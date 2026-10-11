// Package watchdog checks app liveness and logs a conservative connection signal.
package watchdog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	jobplist "github.com/MBFrosty/macjobs/internal/plist"
	"github.com/MBFrosty/macjobs/internal/procs"
	"github.com/MBFrosty/macjobs/internal/redact"
	"github.com/MBFrosty/macjobs/internal/runner"
)

type Options struct {
	App, Log, State     string
	RelaunchInterval    time.Duration
	Interval            time.Duration
	Probes              ProbeOptions
	RestartOnDisconnect bool
}
type Service struct {
	Runner       runner.Runner
	Home         string
	Now          func() time.Time
	Applications []string
	HTTPClient   HTTPDoer
	Resolver     IPResolver
	Probes       []Probe
}
type Result struct {
	SchemaVersion int           `json:"schemaVersion"`
	App           string        `json:"app"`
	Running       string        `json:"running"`
	Relaunched    bool          `json:"relaunched"`
	Connected     string        `json:"connected"`
	Signal        string        `json:"signal"`
	Reason        string        `json:"reason,omitempty"`
	Timestamp     string        `json:"timestamp"`
	Probe         string        `json:"probe"`
	Probes        []ProbeResult `json:"probes"`
}
type state struct {
	LastAttempt time.Time `json:"lastAttempt"`
}

func noMatch(err error) bool {
	var status interface{ ExitCode() int }
	return errors.As(err, &status) && status.ExitCode() == 1
}
func (s Service) processes(ctx context.Context, app string) ([]int, string, error) {
	dirs := s.Applications
	if dirs == nil {
		dirs = []string{"/Applications", filepath.Join(s.Home, "Applications")}
	}
	for _, dir := range dirs {
		bundle := filepath.Join(dir, app+".app")
		b, err := os.ReadFile(filepath.Join(bundle, "Contents", "Info.plist"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, "bundle-executable", errors.New("cannot read app bundle metadata")
		}
		p, err := jobplist.Decode(b)
		executable := jobplist.String(p, "CFBundleExecutable")
		if err != nil || executable == "" || filepath.Base(executable) != executable {
			return nil, "bundle-executable", errors.New("app executable metadata is invalid")
		}
		main := filepath.Join(bundle, "Contents", "MacOS", executable)
		b, err = s.Runner.Run(ctx, "ps", "-axo", "pid=,comm=")
		if err != nil {
			return nil, "bundle-executable", errors.New("app process inspection unavailable")
		}
		mainRunning := false
		pids := []int{}
		for _, process := range procs.ParsePS(b) {
			if process.Name == main {
				mainRunning = true
			}
			if process.PID > 0 && strings.HasPrefix(process.Name, bundle+string(filepath.Separator)) {
				pids = append(pids, process.PID)
			}
		}
		if !mainRunning {
			pids = nil
		}
		return pids, "bundle-executable", nil
	}
	b, err := s.Runner.Run(ctx, "pgrep", "-x", app)
	if noMatch(err) {
		return nil, "exact-name", nil
	}
	if err != nil {
		return nil, "exact-name", errors.New("app process inspection unavailable")
	}
	pids := []int{}
	for _, field := range strings.Fields(string(b)) {
		pid, err := strconv.Atoi(field)
		if err != nil || pid <= 0 {
			return nil, "exact-name", errors.New("invalid process response")
		}
		pids = append(pids, pid)
	}
	return pids, "exact-name", nil
}

func (s Service) relaunch(ctx context.Context, o Options, now time.Time, r *Result, disconnect bool) error {
	if err := os.MkdirAll(filepath.Dir(o.State), 0700); err != nil {
		return errors.New("cannot create watchdog state directory")
	}
	lock, err := os.OpenFile(o.State+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return errors.New("cannot open watchdog state lock")
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		r.Reason = "relaunch-suppressed-busy"
		return nil
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	var previous state
	b, err := os.ReadFile(o.State)
	if err == nil {
		if json.Unmarshal(b, &previous) != nil || previous.LastAttempt.IsZero() {
			return errors.New("invalid watchdog state; relaunch refused")
		}
	} else if !os.IsNotExist(err) {
		return errors.New("cannot read watchdog state; relaunch refused")
	}
	if !previous.LastAttempt.IsZero() && now.Sub(previous.LastAttempt) < o.RelaunchInterval {
		r.Reason = "relaunch-suppressed-rate-limit"
		return nil
	}
	// Persist before quit/open so failed attempts and concurrent checks cannot cause a loop.
	b, _ = json.Marshal(state{LastAttempt: now})
	f, err := os.CreateTemp(filepath.Dir(o.State), ".watchdog-*")
	if err != nil {
		return errors.New("cannot create watchdog state")
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err != nil || syncErr != nil || closeErr != nil || os.Rename(f.Name(), o.State) != nil {
		return errors.New("cannot save watchdog state; relaunch refused")
	}
	if disconnect {
		if !o.RestartOnDisconnect || r.Connected != "no" {
			return errors.New("disconnect restart requires explicit opt-in and a negative probe")
		}
		if _, err := s.Runner.Run(ctx, "osascript", "-e", runner.WatchdogQuitScript(o.App)); err != nil {
			r.Reason = "restart-quit-failed"
			return errors.New("cannot cleanly quit app; subprocess output suppressed")
		}
		// Never open until the main process has actually exited. No forced termination.
		waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		for {
			pids, _, err := s.processes(waitCtx, o.App)
			if err != nil {
				r.Reason = "restart-exit-unavailable"
				return errors.New("cannot verify app exit; reopen refused")
			}
			if len(pids) == 0 {
				break
			}
			select {
			case <-waitCtx.Done():
				r.Reason = "restart-quit-pending"
				return errors.New("app did not exit; reopen refused")
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	if _, err := s.Runner.Run(ctx, "open", "-a", o.App); err != nil {
		r.Reason = "relaunch-failed"
		return errors.New("cannot reopen app; subprocess output suppressed")
	}
	r.Relaunched = true
	return nil
}

func (r Result) Line() string {
	launched := "no"
	if r.Relaunched {
		launched = "yes"
	}
	line := fmt.Sprintf("%s app=%s running=%s relaunched=%s connected=%s signal=%s", r.Timestamp, strconv.Quote(redact.Redactor{}.Text(r.App)), r.Running, launched, r.Connected, r.Signal)
	if r.Reason != "" {
		line += " reason=" + r.Reason
	}
	line += " probe=" + r.Probe
	return line
}
func (s Service) Check(ctx context.Context, o Options) (Result, error) {
	r := Result{SchemaVersion: 1, App: o.App, Running: "unknown", Connected: "unknown", Signal: "tcp-any", Probe: "tcp-any", Probes: []ProbeResult{}}
	if !runner.WatchdogAllowed("open", []string{"-a", o.App}) || !filepath.IsAbs(o.Log) || o.RelaunchInterval <= 0 {
		return r, errors.New("an app name, absolute log path, and positive relaunch interval are required")
	}
	if o.Interval == 0 {
		o.Interval = 10 * time.Minute
	}
	if err := o.Probes.Validate(o.Interval); err != nil {
		return r, err
	}
	if w, ok := s.Runner.(runner.Watchdog); ok && o.RestartOnDisconnect {
		w.QuitApp = o.App
		s.Runner = w
	}
	if o.State == "" {
		o.State = filepath.Join(s.Home, ".config", "macjobs", "state", o.App+".json")
	}
	if !filepath.IsAbs(o.State) || filepath.Clean(o.State) == filepath.Clean(o.Log) || filepath.Clean(o.State)+".lock" == filepath.Clean(o.Log) {
		return r, errors.New("state requires an absolute path distinct from the log")
	}
	if err := o.Probes.ValidateOutput(o.Log, o.State, o.State+".lock"); err != nil {
		return r, err
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	r.Timestamp = now.Format(time.RFC3339)
	probes := s.probes(o)
	if s.Probes != nil {
		probes = s.Probes
	}
	pids, method, checkErr := s.processes(ctx, o.App)
	r.Signal = method + "+tcp-any"
	if len(probes) > 0 {
		names := []string{}
		for _, probe := range probes {
			names = append(names, probe.Name())
		}
		r.Signal = method + "+" + strings.Join(names, ",")
	}
	if checkErr == nil {
		if len(pids) > 0 {
			r.Running = "yes"
			for _, probe := range probes {
				r.Probes = append(r.Probes, probe.Check(ctx, ProbeInput{PIDs: pids, Now: now}))
			}
			r.Connected, r.Probe, r.Reason = combine(r.Probes)
			if o.RestartOnDisconnect && r.Connected == "no" {
				checkErr = s.relaunch(ctx, o, now, &r, true)
			}
		} else {
			r.Running = "no"
			r.Reason = "app-not-running"
			checkErr = s.relaunch(ctx, o, now, &r, false)
		}
	}
	if r.Running != "yes" {
		for _, probe := range probes {
			r.Probes = append(r.Probes, probeResult(probe.Name(), "unknown", "process-not-running-or-unavailable"))
		}
		r.Probe = "liveness"
	}
	if checkErr != nil && r.Reason == "" {
		r.Reason = "check-unavailable"
	}
	if err := os.MkdirAll(filepath.Dir(o.Log), 0700); err != nil {
		return r, errors.New("cannot create watchdog log directory")
	}
	f, err := os.OpenFile(o.Log, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err != nil {
		return r, errors.New("cannot open watchdog log")
	}
	_, writeErr := f.WriteString(r.Line() + "\n")
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		return r, errors.New("cannot append watchdog log")
	}
	return r, checkErr
}
