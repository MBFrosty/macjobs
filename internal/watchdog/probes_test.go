package watchdog

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type resolverFunc func(context.Context, string) ([]net.IPAddr, error)

func (f resolverFunc) LookupIPAddr(ctx context.Context, name string) ([]net.IPAddr, error) {
	return f(ctx, name)
}
func TestTCPPeerFixtures(t *testing.T) {
	for _, tc := range []struct{ name, file, peer, want string }{
		{"loopback", "lsof-loopback.txt", "127.0.0.1:443", "no"},
		{"any", "lsof-any.txt", "203.0.113.42:443", "no"},
		{"wrong-port", "lsof-peer.txt", "203.0.113.42:80", "no"},
		{"relay", "lsof-peer.txt", "203.0.113.42:443", "yes"},
		{"port-pattern", "lsof-peer.txt", "203.0.113.42:*", "yes"},
		{"ipv6", "lsof-peer.txt", "[2001:db8::42]:8443", "yes"},
		{"resolved", "lsof-peer.txt", "relay.example.invalid:443", "yes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, f, _ := setup(t)
			f.tcp["43"] = fixture(t, tc.file)
			s.Resolver = resolverFunc(func(_ context.Context, host string) ([]net.IPAddr, error) {
				if host != "relay.example.invalid" {
					t.Fatal(host)
				}
				return []net.IPAddr{{IP: net.ParseIP("203.0.113.42")}}, nil
			})
			p := tcpPeerProbe{service: s, peers: []string{tc.peer}}
			r := p.Check(context.Background(), ProbeInput{PIDs: []int{42, 43}})
			if r.Connected != tc.want {
				t.Fatal(r)
			}
		})
	}
}
func TestTCPUnavailableAndHalfOpen(t *testing.T) {
	s, _, f, _ := setup(t)
	p := tcpPeerProbe{service: s, peers: []string{"203.0.113.42:443"}}
	f.tcpError = errors.New("password=private-value")
	r := p.Check(context.Background(), ProbeInput{PIDs: []int{42}})
	if r.Connected != "unknown" || strings.Contains(r.Reason, "private-value") {
		t.Fatal(r)
	}
	f.tcpError = nil
	f.tcp["42"] = fixture(t, "lsof-peer.txt")
	r = p.Check(context.Background(), ProbeInput{PIDs: []int{42}})
	if connected, _, reason := combine([]ProbeResult{r}); connected != "unknown" || reason != "tcp-peer-needs-freshness" {
		t.Fatal(connected, reason)
	}
	s.Resolver = resolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
		return nil, errors.New("private resolver failure")
	})
	p.service = s
	p.peers = []string{"relay.example.invalid:443"}
	if r := p.Check(context.Background(), ProbeInput{PIDs: []int{42}}); r.Connected != "unknown" {
		t.Fatal(r)
	}
}
func TestLogEvidence(t *testing.T) {
	for _, tc := range []struct{ name, content, want, reason string }{
		{"fresh", "2026-10-10T18:29:59-07:00 link=connected\n", "yes", "log-connected-fresh"},
		{"heartbeat", "[2026-10-10T18:29:59.123-07:00] heartbeat\n", "yes", "log-connected-fresh"},
		{"stale", "2026-10-10T18:19:59-07:00 link=connected\n", "no", "log-stale"},
		{"boundary", "2026-10-10T18:20:00-07:00 link=connected\n", "yes", "log-connected-fresh"},
		{"disconnect", "2026-10-10T18:29:00-07:00 link=connected\n2026-10-10T18:29:01-07:00 link=disconnected password=private-value\n", "no", "log-disconnected"},
		{"newest-wins", "2026-10-10T18:29:00-07:00 link=disconnected\n2026-10-10T18:29:01-07:00 link=connected\n", "yes", "log-connected-fresh"},
		{"unrelated-writes", "2026-10-10T18:19:00-07:00 link=connected\n2026-10-10T18:29:59-07:00 telemetry uploaded\n", "no", "log-stale"},
		{"no-timestamp", "link=connected\n", "unknown", "log-timestamp-invalid"},
		{"future", "2026-10-10T18:30:01-07:00 link=connected\n", "unknown", "log-timestamp-invalid"},
		{"partial", "2026-10-10T18:29:59-07:00 link=connected", "unknown", "log-record-incomplete"},
		{"no-signal", "2026-10-10T18:29:59-07:00 telemetry uploaded\n", "unknown", "log-no-state"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, o, _, _ := setup(t)
			o.Probes = ProbeOptions{LogPath: filepath.Join(s.Home, "app.log"), ConnectedRegex: `link=connected|heartbeat`, DisconnectedRegex: `link=disconnected`}
			o.Interval = 10 * time.Minute
			if err := os.WriteFile(o.Probes.LogPath, []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			if err := o.Probes.Validate(o.Interval); err != nil {
				t.Fatal(err)
			}
			r := s.probes(o)[0].Check(context.Background(), ProbeInput{Now: s.Now()})
			if r.Connected != tc.want || r.Reason != tc.reason || strings.Contains(r.Reason, "private-value") {
				t.Fatal(r)
			}
		})
	}
}
func TestLogAgeOverrideAndUnavailable(t *testing.T) {
	s, o, _, _ := setup(t)
	o.Probes = ProbeOptions{LogPath: filepath.Join(s.Home, "app.log"), ConnectedRegex: "connected", DisconnectedRegex: "disconnected", MaxAge: time.Second}
	o.Interval = time.Minute
	p := s.probes(o)[0]
	if r := p.Check(context.Background(), ProbeInput{Now: s.Now()}); r.Connected != "unknown" {
		t.Fatal(r)
	}
	if err := os.WriteFile(o.Probes.LogPath, []byte("2026-10-10T18:29:58-07:00 connected\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if r := p.Check(context.Background(), ProbeInput{Now: s.Now()}); r.Connected != "no" {
		t.Fatal(r)
	}
	// Read only a bounded tail of large files, discarding its first partial record.
	if err := os.WriteFile(o.Probes.LogPath, []byte(strings.Repeat("x", 1024*1024+100)+"\n2026-10-10T18:29:59-07:00 connected\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if r := p.Check(context.Background(), ProbeInput{Now: s.Now()}); r.Connected != "yes" {
		t.Fatal(r)
	}
}

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }
func TestHTTPState(t *testing.T) {
	for _, tc := range []struct {
		name, body, want, reason string
		code                     int
		down                     bool
	}{
		{"fresh", `{"connected":true,"timestamp":"2026-10-10T18:29:59-07:00"}`, "yes", "http-connected-fresh", 200, false},
		{"stale", `{"connected":true,"timestamp":"2026-10-10T18:19:59-07:00"}`, "no", "http-stale", 200, false},
		{"disconnected", `{"connected":false}`, "no", "http-disconnected", 200, false},
		{"missing-timestamp", `{"connected":true}`, "unknown", "http-timestamp-invalid", 200, false},
		{"future", `{"connected":true,"timestamp":"2026-10-10T18:30:01-07:00"}`, "unknown", "http-timestamp-invalid", 200, false},
		{"health-only", `{"ok":true}`, "unknown", "http-state-invalid", 200, false},
		{"oversized", `{"connected":true,"timestamp":"2026-10-10T18:29:59-07:00"}` + strings.Repeat(" ", 65536), "unknown", "http-state-invalid", 200, false},
		{"junk", `{"connected":true} garbage`, "unknown", "http-state-invalid", 200, false},
		{"down", "", "no", "http-unavailable", 0, true},
		{"server-error", "password=private-value", "no", "http-status-unavailable", 503, false},
		{"redirect", "", "no", "http-status-unavailable", 302, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, o, _, _ := setup(t)
			o.Probes = ProbeOptions{HTTPURL: "http://127.0.0.1:12345/status"}
			o.Interval = 10 * time.Minute
			s.HTTPClient = doerFunc(func(req *http.Request) (*http.Response, error) {
				if req.Method != "GET" || !loopbackURL(req.URL.String()) || req.Context().Err() != nil {
					t.Fatal(req)
				}
				if tc.down {
					return nil, errors.New("password=private-value")
				}
				return &http.Response{StatusCode: tc.code, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			r := s.probes(o)[0].Check(context.Background(), ProbeInput{Now: s.Now()})
			if r.Connected != tc.want || r.Reason != tc.reason || strings.Contains(r.Reason, "private-value") {
				t.Fatal(r)
			}
		})
	}
}
func TestProbeCombination(t *testing.T) {
	for _, tc := range []struct {
		states []ProbeResult
		want   string
	}{
		{nil, "unknown"},
		{[]ProbeResult{{"tcp-peer", "yes", "present"}}, "unknown"},
		{[]ProbeResult{{"tcp-peer", "yes", "present"}, {"log", "yes", "fresh"}}, "yes"},
		{[]ProbeResult{{"log", "yes", "fresh"}, {"http", "no", "down"}}, "no"},
		{[]ProbeResult{{"log", "unknown", "unavailable"}, {"http", "no", "down"}}, "no"},
		{[]ProbeResult{{"log", "yes", "fresh"}, {"http", "unknown", "unavailable"}}, "unknown"},
		{[]ProbeResult{{"log", "yes", "fresh"}}, "yes"},
	} {
		if got, _, _ := combine(tc.states); got != tc.want {
			t.Fatal(tc, got)
		}
	}
}
func TestProbeValidation(t *testing.T) {
	for _, o := range []ProbeOptions{
		{TCPPeers: []string{"example.invalid"}}, {TCPPeers: []string{"example.invalid:0"}}, {TCPPeers: []string{"example.invalid:65536"}}, {TCPPeers: []string{"https://example.invalid:443"}},
		{LogPath: "app.log", ConnectedRegex: "up", DisconnectedRegex: "down"}, {LogPath: "/tmp/app.log"}, {LogPath: "/tmp/app.log", ConnectedRegex: "[", DisconnectedRegex: "down"},
		{HTTPURL: "https://127.0.0.1/status"}, {HTTPURL: "http://localhost/status"}, {HTTPURL: "http://example.invalid/status"}, {HTTPURL: "http://127.0.0.1.example.invalid/status"}, {HTTPURL: "http://user:password@127.0.0.1/status"}, {HTTPURL: "http://127.0.0.1:65536/status"}, {MaxAge: -time.Second},
	} {
		if err := o.Validate(time.Minute); err == nil {
			t.Fatal("invalid accepted", o)
		}
	}
	for _, url := range []string{"http://127.0.0.1/status", "http://[::1]:12345/status"} {
		if err := (ProbeOptions{HTTPURL: url}).Validate(time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	if err := (ProbeOptions{}).Validate(-time.Second); err == nil {
		t.Fatal("negative interval")
	}
}
func TestJSONProbesAdditiveContract(t *testing.T) {
	s, o, f, bundle := setup(t)
	f.ps = "42 " + filepath.Join(bundle, "Contents", "MacOS", "Example Main") + "\n"
	s.Probes = []Probe{fixedProbe{"log", "yes", "fresh"}}
	r, err := s.Check(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(b, &document); err != nil {
		t.Fatal(err)
	}
	if document["schemaVersion"] != float64(1) || len(document["probes"].([]any)) != 1 || document["connected"] != "yes" {
		t.Fatal(string(b))
	}
	for _, key := range []string{"app", "running", "relaunched", "connected", "signal", "timestamp"} {
		if _, ok := document[key]; !ok {
			t.Fatal("missing legacy field", key)
		}
	}
}

func TestProbeOutputAliasesRejected(t *testing.T) {
	for _, kind := range []string{"same-path", "symlink", "hard-link", "state", "state-lock"} {
		t.Run(kind, func(t *testing.T) {
			s, o, f, bundle := setup(t)
			f.ps = "42 " + filepath.Join(bundle, "Contents", "MacOS", "Example Main") + "\n"
			o.State = filepath.Join(s.Home, "state.json")
			o.Log = filepath.Join(s.Home, "watchdog.log")
			source := o.Log
			if kind == "state" {
				source = o.State
			}
			if kind == "state-lock" {
				source = o.State + ".lock"
			}
			if err := os.WriteFile(source, []byte("2026-10-10T18:29:59-07:00 connected\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if kind == "symlink" || kind == "hard-link" {
				alias := filepath.Join(s.Home, "source.log")
				link := os.Symlink
				if kind == "hard-link" {
					link = os.Link
				}
				if err := link(source, alias); err != nil {
					t.Fatal(err)
				}
				source = alias
			}
			o.Probes = ProbeOptions{LogPath: source, ConnectedRegex: "connected", DisconnectedRegex: "disconnected"}
			if _, err := s.Check(context.Background(), o); err == nil || f.opens != 0 || f.quits != 0 {
				t.Fatal("source alias accepted", err)
			}
		})
	}
}
func TestHTTPTransportIsLocalAndDoesNotRedirect(t *testing.T) {
	s, o, _, _ := setup(t)
	o.Interval = time.Minute
	o.Probes = ProbeOptions{HTTPURL: "http://127.0.0.1:12345/status"}
	p := s.probes(o)[0].(httpProbe)
	client := p.client.(*http.Client)
	transport := client.Transport.(*http.Transport)
	if transport.Proxy != nil || client.CheckRedirect(&http.Request{}, nil) != http.ErrUseLastResponse || client.Timeout != 5*time.Second {
		t.Fatal("unsafe HTTP transport")
	}
}
