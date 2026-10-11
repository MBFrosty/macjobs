package watchdog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	jobplist "github.com/MBFrosty/macjobs/internal/plist"
	"github.com/MBFrosty/macjobs/internal/runner"
	codec "howett.net/plist"
)

type exitError int

func (e exitError) Error() string { return "sanitized exit" }
func (e exitError) ExitCode() int { return int(e) }

type fake struct {
	t         *testing.T
	ps        string
	pgrep     []byte
	tcp       map[string][]byte
	tcpError  error
	opens     int
	psError   error
	openError error
	quits     int
	quitError error
}

func (f *fake) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if !runner.WatchdogAllowed(name, args) && !(name == "osascript" && len(args) == 2 && args[0] == "-e" && args[1] == runner.WatchdogQuitScript("Example App")) {
		f.t.Fatalf("outside allowlist: %s %v", name, args)
	}
	switch name {
	case "ps":
		return []byte(f.ps), f.psError
	case "pgrep":
		if f.pgrep == nil {
			return nil, exitError(1)
		}
		return f.pgrep, nil
	case "lsof":
		if f.tcpError != nil {
			return nil, f.tcpError
		}
		if b, ok := f.tcp[args[3]]; ok {
			return b, nil
		}
		return nil, exitError(1)
	case "osascript":
		f.quits++
		if f.quitError == nil {
			f.ps = ""
			f.pgrep = nil
		}
		return nil, f.quitError
	case "open":
		f.opens++
		return nil, f.openError
	}
	f.t.Fatal("unexpected command")
	return nil, errors.New("missing replay")
}
func setup(t *testing.T) (Service, Options, *fake, string) {
	t.Helper()
	home := t.TempDir()
	apps := filepath.Join(home, "Applications")
	bundle := filepath.Join(apps, "Example App.app")
	if err := os.MkdirAll(filepath.Join(bundle, "Contents"), 0700); err != nil {
		t.Fatal(err)
	}
	b, err := jobplist.Encode(map[string]any{"CFBundleExecutable": "Example Main", "CFBundleIdentifier": "com.example.app"}, codec.XMLFormat)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), b, 0600); err != nil {
		t.Fatal(err)
	}
	f := &fake{t: t, tcp: map[string][]byte{}}
	s := Service{Runner: f, Home: home, Applications: []string{apps}, Now: func() time.Time { return time.Date(2026, 10, 10, 18, 30, 0, 0, time.FixedZone("local", -7*60*60)) }}
	o := Options{App: "Example App", Log: filepath.Join(home, "logs", "watchdog.log"), RelaunchInterval: 10 * time.Minute}
	return s, o, f, bundle
}

type fixedProbe ProbeResult

func (p fixedProbe) Name() string                                  { return p.Probe }
func (p fixedProbe) Check(context.Context, ProbeInput) ProbeResult { return ProbeResult(p) }
func TestRunningNeverRelaunchesWithoutOptIn(t *testing.T) {
	for _, connected := range []string{"yes", "no", "unknown"} {
		s, o, f, bundle := setup(t)
		f.ps = "42 " + filepath.Join(bundle, "Contents", "MacOS", "Example Main") + "\n43 " + filepath.Join(bundle, "Contents", "Frameworks", "Example Helper") + "\n"
		s.Probes = []Probe{fixedProbe{Probe: "log", Connected: connected, Reason: "example-state"}}
		r, err := s.Check(context.Background(), o)
		if err != nil || r.Running != "yes" || r.Relaunched || r.Connected != connected || f.opens != 0 || f.quits != 0 {
			t.Fatal(r, err)
		}
		if _, err := os.Stat(filepath.Join(s.Home, ".config", "macjobs", "state", o.App+".json")); !os.IsNotExist(err) {
			t.Fatal("running check created state")
		}
		b, _ := os.ReadFile(o.Log)
		if strings.Count(string(b), "\n") != 1 || !strings.Contains(string(b), "probe=") || !strings.Contains(string(b), "reason=") {
			t.Fatal(string(b))
		}
	}
}
func TestNoProbeNeverClaimsConnected(t *testing.T) {
	s, o, f, bundle := setup(t)
	f.ps = "42 " + filepath.Join(bundle, "Contents", "MacOS", "Example Main") + "\n"
	f.tcp["42"] = []byte("p42\nn127.0.0.1:4000->127.0.0.1:443\nn192.0.2.1:4001->203.0.113.1:443\n")
	o.RestartOnDisconnect = true
	r, err := s.Check(context.Background(), o)
	if err != nil || r.Connected != "unknown" || r.Signal != "bundle-executable+tcp-any" || r.Probe != "tcp-any" || f.opens != 0 || f.quits != 0 {
		t.Fatal(r, err)
	}
}
func TestAbsentRateLimit(t *testing.T) {
	s, o, f, bundle := setup(t)
	// A helper alone must never hide the absence of the main executable.
	f.ps = "43 " + filepath.Join(bundle, "Contents", "Frameworks", "Example Helper") + "\n"
	r, err := s.Check(context.Background(), o)
	if err != nil || r.Running != "no" || !r.Relaunched || f.opens != 1 {
		t.Fatal(r, err)
	}
	r, err = s.Check(context.Background(), o)
	if err != nil || r.Relaunched || r.Reason != "relaunch-suppressed-rate-limit" || f.opens != 1 {
		t.Fatal(r, err)
	}
	first := s.Now()
	s.Now = func() time.Time { return first.Add(10 * time.Minute) }
	r, err = s.Check(context.Background(), o)
	if err != nil || !r.Relaunched || f.opens != 2 {
		t.Fatal(r, err)
	}
	b, _ := os.ReadFile(o.Log)
	if strings.Count(string(b), "\n") != 3 {
		t.Fatal("not exactly one line per check", string(b))
	}
}
func TestFailedOpenAlsoRateLimited(t *testing.T) {
	s, o, f, _ := setup(t)
	f.openError = errors.New("private subprocess failure")
	r, err := s.Check(context.Background(), o)
	if err == nil || r.Relaunched || r.Reason != "relaunch-failed" || strings.Contains(err.Error(), "private") {
		t.Fatal(r, err)
	}
	r, err = s.Check(context.Background(), o)
	if err != nil || r.Reason != "relaunch-suppressed-rate-limit" || f.opens != 1 {
		t.Fatal(r, err)
	}
}
func TestUnknownLivenessNeverOpens(t *testing.T) {
	s, o, f, _ := setup(t)
	f.psError = errors.New("private inspection failure")
	r, err := s.Check(context.Background(), o)
	if err == nil || f.opens != 0 || r.Running != "unknown" {
		t.Fatal(r, err)
	}
	b, _ := os.ReadFile(o.Log)
	if strings.Count(string(b), "\n") != 1 || strings.Contains(string(b), "private") {
		t.Fatal(string(b))
	}
}
func TestExactNameFallback(t *testing.T) {
	s, o, f, _ := setup(t)
	o.App = "Example Other"
	f.pgrep = []byte("42\n")
	r, err := s.Check(context.Background(), o)
	if err != nil || r.Running != "yes" || r.Signal != "exact-name+tcp-any" || f.opens != 0 {
		t.Fatal(r, err)
	}
}
func TestInvalidStateRefusesOpen(t *testing.T) {
	s, o, f, _ := setup(t)
	o.State = filepath.Join(s.Home, "state.json")
	if err := os.WriteFile(o.State, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Check(context.Background(), o); err == nil || f.opens != 0 {
		t.Fatal("invalid state relaunched")
	}
}
func TestLineEscapesAndRedacts(t *testing.T) {
	r := Result{App: "Example\npassword=private-value", Running: "yes", Connected: "unknown", Signal: "tcp-established", Timestamp: "2026-10-10T18:30:00-07:00"}
	line := r.Line()
	if strings.Contains(line, "\n") || strings.Contains(line, "private-value") || !strings.Contains(line, "[REDACTED]") {
		t.Fatal(line)
	}
}

type openGate struct {
	*fake
	started, release chan struct{}
}

func (f openGate) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	b, err := f.fake.Run(ctx, name, args...)
	if name == "open" {
		close(f.started)
		<-f.release
	}
	return b, err
}
func TestOverlappingChecksOnlyOpenOnce(t *testing.T) {
	s, o, f, _ := setup(t)
	gate := openGate{fake: f, started: make(chan struct{}), release: make(chan struct{})}
	s.Runner = gate
	done := make(chan error, 1)
	go func() { _, err := s.Check(context.Background(), o); done <- err }()
	<-gate.started
	r, err := s.Check(context.Background(), o)
	close(gate.release)
	firstErr := <-done
	if err != nil || firstErr != nil || r.Relaunched || r.Reason != "relaunch-suppressed-busy" || f.opens != 1 {
		t.Fatal(r, err, firstErr, f.opens)
	}
	b, _ := os.ReadFile(o.Log)
	if strings.Count(string(b), "\n") != 2 {
		t.Fatal("concurrent checks did not each append one line", string(b))
	}
}

func TestDisconnectRestartOptInAndRateLimit(t *testing.T) {
	s, o, f, bundle := setup(t)
	running := "42 " + filepath.Join(bundle, "Contents", "MacOS", "Example Main") + "\n"
	f.ps = running
	o.RestartOnDisconnect = true
	s.Probes = []Probe{fixedProbe{Probe: "log", Connected: "no", Reason: "log-disconnected"}}
	r, err := s.Check(context.Background(), o)
	if err != nil || !r.Relaunched || f.quits != 1 || f.opens != 1 || r.Connected != "no" {
		t.Fatal(r, err)
	}
	f.ps = running
	r, err = s.Check(context.Background(), o)
	if err != nil || r.Relaunched || f.quits != 1 || f.opens != 1 || r.Reason != "relaunch-suppressed-rate-limit" {
		t.Fatal(r, err)
	}
}
func TestRestartFailureAndUnknown(t *testing.T) {
	for _, tc := range []struct {
		state   string
		failure bool
	}{{"unknown", false}, {"yes", false}, {"no", true}} {
		s, o, f, bundle := setup(t)
		f.ps = "42 " + filepath.Join(bundle, "Contents", "MacOS", "Example Main") + "\n"
		o.RestartOnDisconnect = true
		s.Probes = []Probe{fixedProbe{Probe: "log", Connected: tc.state, Reason: "example-state"}}
		if tc.failure {
			f.quitError = errors.New("password=private-value")
		}
		r, err := s.Check(context.Background(), o)
		if f.opens != 0 || r.Relaunched {
			t.Fatal(r, err)
		}
		if tc.failure {
			if err == nil || strings.Contains(err.Error(), "private-value") || f.quits != 1 || r.Reason != "restart-quit-failed" {
				t.Fatal(r, err)
			}
		} else if err != nil || f.quits != 0 {
			t.Fatal(r, err)
		}
	}
}

type quitGate struct {
	*fake
	started, release chan struct{}
}

func (g quitGate) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	b, err := g.fake.Run(ctx, name, args...)
	if name == "osascript" {
		close(g.started)
		<-g.release
	}
	return b, err
}
func TestOverlappingDisconnectRecoveryOnlyQuitsOnce(t *testing.T) {
	s, o, f, bundle := setup(t)
	f.ps = "42 " + filepath.Join(bundle, "Contents", "MacOS", "Example Main") + "\n"
	o.RestartOnDisconnect = true
	s.Probes = []Probe{fixedProbe{"log", "no", "log-disconnected"}}
	gate := quitGate{f, make(chan struct{}), make(chan struct{})}
	s.Runner = gate
	done := make(chan error, 1)
	go func() { _, err := s.Check(context.Background(), o); done <- err }()
	<-gate.started
	r, err := s.Check(context.Background(), o)
	close(gate.release)
	firstErr := <-done
	if err != nil || firstErr != nil || r.Relaunched || r.Reason != "relaunch-suppressed-busy" || f.quits != 1 || f.opens != 1 {
		t.Fatal(r, err, firstErr, f.quits, f.opens)
	}
}
func TestDisconnectRestartInvalidStateRefusesQuit(t *testing.T) {
	s, o, f, bundle := setup(t)
	f.ps = "42 " + filepath.Join(bundle, "Contents", "MacOS", "Example Main") + "\n"
	o.RestartOnDisconnect = true
	o.State = filepath.Join(s.Home, "state.json")
	if err := os.WriteFile(o.State, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	s.Probes = []Probe{fixedProbe{"log", "no", "log-disconnected"}}
	if _, err := s.Check(context.Background(), o); err == nil || f.quits != 0 || f.opens != 0 {
		t.Fatal("invalid state permitted quit", err)
	}
}
