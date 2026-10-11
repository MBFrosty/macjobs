package health

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/procs"
	"github.com/MBFrosty/macjobs/internal/redact"
)

func TestFindings(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	old := now.Add(-3 * time.Hour)
	exit := 2
	for _, tc := range []struct {
		name         string
		item         model.Item
		health, note string
	}{
		{"stopped", model.Item{Live: true, Observed: true, KeepAlive: true}, "fail", "KeepAlive"},
		{"orphan", model.Item{Registered: true, Observed: true}, "fail", "no live job"},
		{"missing plist", model.Item{Live: true, Observed: true, PlistMissing: true}, "fail", "plist is missing"},
		{"crash loop", model.Item{Live: true, Observed: true, Runs: 20, LastExit: &exit}, "fail", "crash loop"},
		{"signal", model.Item{Live: true, Observed: true, Signal: "Terminated: 15"}, "fail", "signal"},
		{"stale", model.Item{Live: true, Observed: true, Interval: time.Hour, Logs: []string{"example.log"}, LastRun: &old}, "warn", "stale"},
		{"dead route", model.Item{Live: true, Observed: true, Kind: "tailscale", Port: 8081}, "fail", "dead local port"},
		{"unknown", model.Item{Registered: true}, "unknown", "unavailable"},
		{"disabled", model.Item{Live: true, Observed: true, KeepAlive: true, Disabled: true}, "warn", "disabled"},
		{"installed", model.Item{Observed: true}, "ok", ""},
		{"healthy", model.Item{Live: true, Observed: true, KeepAlive: true, PID: 1234, Port: 8080}, "ok", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := tc.item
			Evaluate(&i, procs.Snapshot{PortsKnown: true, Listeners: []procs.Listener{{PID: 1234, Port: 8080, Host: "127.0.0.1"}}}, now)
			if i.Health != tc.health || !strings.Contains(strings.Join(i.Notes, " "), tc.note) {
				t.Fatal(i.Health, i.Notes)
			}
		})
	}
}
func TestUnavailablePortProbeIsUnknown(t *testing.T) {
	i := model.Item{Live: true, Observed: true, Port: 8080}
	Evaluate(&i, procs.Snapshot{}, time.Now())
	if i.Health != "unknown" {
		t.Fatal(i.Health)
	}
}

func TestDeclaredPortFindings(t *testing.T) {
	now := time.Now()
	snap := procs.Snapshot{PortsKnown: true}
	for _, tc := range []struct {
		name, health, note string
		item               model.Item
	}{
		{"keepalive missing", "fail", "registered port is not listening", model.Item{Live: true, Observed: true, KeepAlive: true, Port: 8080, Ports: []model.PortBinding{{Proto: "tcp", Port: 8080, Source: model.PortDeclared}}}},
		{"running missing", "fail", "registered port is not listening", model.Item{Live: true, Observed: true, State: "running", PID: 9, Port: 8080, Ports: []model.PortBinding{{Proto: "tcp", Port: 8080, Source: model.PortDeclared}}}},
		{"stopped declared", "ok", "", model.Item{Live: true, Observed: true, State: "not running", Port: 8080, Ports: []model.PortBinding{{Proto: "tcp", Port: 8080, Source: model.PortDeclared}}}},
		{"system missing", "unknown", "not visible without root", model.Item{Live: true, Observed: true, Kind: "system", KeepAlive: true, PID: 9, State: "running", Port: 80, Ports: []model.PortBinding{{Proto: "tcp", Port: 80, Source: model.PortDeclared}}}},
		{"socket missing", "unknown", "not visible without root", model.Item{Live: true, Observed: true, KeepAlive: true, PID: 9, State: "running", Ports: []model.PortBinding{{Proto: "tcp", Port: 8080, Source: model.PortSocket}}}},
		{"inferred missing", "warn", "inferred port 8080 is not listening", model.Item{Live: true, Observed: true, State: "running", PID: 9, Ports: []model.PortBinding{{Proto: "tcp", Port: 8080, Source: model.PortInferred}}}},
		{"front skipped", "ok", "", model.Item{Live: true, Observed: true, Kind: "tailscale", Port: 8080, Ports: []model.PortBinding{{Proto: "tcp", Port: 8080, Source: model.PortTailscale, Listening: true}, {Proto: "tcp", Port: 443, Source: model.PortTailscale}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := tc.item
			Evaluate(&i, snap, now)
			if i.Health != tc.health || !strings.Contains(strings.Join(i.Notes, " "), tc.note) {
				t.Fatal(i.Health, i.Notes)
			}
		})
	}
}

func TestPortConflicts(t *testing.T) {
	items := []model.Item{
		{ID: "com.example.web", Kind: "launchd", Port: 8080, Ports: []model.PortBinding{{Proto: "tcp", Port: 8080, Source: model.PortDeclared}}},
		{ID: "com.example.api", Kind: "launchd", Port: 8080, Ports: []model.PortBinding{{Proto: "tcp", Port: 8080, Source: model.PortDeclared}}},
		{ID: "com.example.other", Kind: "process", Ports: []model.PortBinding{{Proto: "tcp", Port: 9090, Source: model.PortDeclared}}},
		{ID: "tailscale:example:443/", Kind: "tailscale", Port: 8081, Ports: []model.PortBinding{{Proto: "tcp", Port: 8081, Source: model.PortTailscale}}},
	}
	listeners := []model.Listener{
		{Proto: "tcp", Port: 8080, PID: 11, JobID: "com.example.web"},
		{Proto: "tcp", Port: 9090, PID: 99},
		{Proto: "udp", Port: 5353, PID: 50},
	}
	warnings := PortConflicts(items, procs.Snapshot{PortsKnown: true}, listeners)
	if items[0].Health != "warn" || !strings.Contains(strings.Join(items[0].Notes, " "), "multiple jobs expect 8080") {
		t.Fatal(items[0])
	}
	if items[2].Health != "warn" || !strings.Contains(strings.Join(items[2].Notes, " "), "held by unattributed pid 99") {
		t.Fatal(items[2])
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "2 listeners have no known job") {
		t.Fatal(warnings)
	}
	dead := model.Item{ID: "tailscale:example:443/", Kind: "tailscale", Live: true, Observed: true, Port: 8081, Ports: []model.PortBinding{{Proto: "tcp", Port: 8081, Source: model.PortTailscale}}}
	backend := model.Item{ID: "com.example.web", Ports: []model.PortBinding{{Proto: "tcp", Port: 8081, Source: model.PortDeclared}}}
	Evaluate(&dead, procs.Snapshot{PortsKnown: true}, time.Now())
	pair := []model.Item{backend, dead}
	PortConflicts(pair, procs.Snapshot{PortsKnown: true}, nil)
	if pair[1].Health != "fail" || !strings.Contains(strings.Join(pair[1].Notes, " "), "backend expected by com.example.web") {
		t.Fatal(pair[1])
	}
}

func TestMissingProgramFindings(t *testing.T) {
	now := time.Now()
	missing := filepath.Join(t.TempDir(), "missing-program")
	present := filepath.Join(t.TempDir(), "present-program")
	if err := os.WriteFile(present, []byte("x"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name         string
		item         model.Item
		health, note string
	}{
		{"managed", model.Item{Kind: "launchd", Live: true, Observed: true, Managed: true, Plist: map[string]any{"ProgramArguments": []any{missing}}}, "fail", "program not found: " + missing},
		{"adopted", model.Item{Kind: "launchd", Live: true, Observed: true, Managed: true, CreatedByTool: false, Plist: map[string]any{"Program": missing}}, "fail", "program not found: " + missing},
		{"foreign", model.Item{Kind: "launchd", Live: true, Observed: true, Plist: map[string]any{"ProgramArguments": []any{missing}}}, "warn", "program not found: " + missing},
		{"system", model.Item{Kind: "system", Live: true, Observed: true, Managed: true, Plist: map[string]any{"ProgramArguments": []any{missing}}}, "ok", ""},
		{"apple", model.Item{Kind: "launchd", Label: "com.apple.example", ID: "com.apple.example", Live: true, Observed: true, Managed: true, Plist: map[string]any{"ProgramArguments": []any{missing}}}, "ok", ""},
		{"present", model.Item{Kind: "launchd", Live: true, Observed: true, Managed: true, Plist: map[string]any{"ProgramArguments": []any{present}}}, "ok", ""},
		{"relative", model.Item{Kind: "launchd", Live: true, Observed: true, Managed: true, Plist: map[string]any{"ProgramArguments": []any{"example-program"}}}, "ok", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := tc.item
			Evaluate(&i, procs.Snapshot{}, now)
			if i.Health != tc.health || !strings.Contains(strings.Join(i.Notes, " "), tc.note) {
				t.Fatal(i.Health, i.Notes)
			}
		})
	}
}

func TestMissingProgramNoteIsRedacted(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "secret-program")
	i := model.Item{Kind: "launchd", Live: true, Observed: true, Managed: true, Plist: map[string]any{"ProgramArguments": []any{missing}}}
	Evaluate(&i, procs.Snapshot{}, time.Now())
	if len(i.Notes) != 1 || !strings.Contains(i.Notes[0], missing) {
		t.Fatal(i.Notes)
	}
	got := (redact.Redactor{Values: []string{missing}}).Text(i.Notes[0])
	if strings.Contains(got, missing) || !strings.Contains(got, redact.Hidden) {
		t.Fatal(got)
	}
}

func TestHeldByOtherJob(t *testing.T) {
	items := []model.Item{
		{ID: "com.example.web", Ports: []model.PortBinding{{Proto: "tcp", Port: 8080, Source: model.PortDeclared}}},
		{ID: "com.example.other", PID: 22},
	}
	PortConflicts(items, procs.Snapshot{PortsKnown: true}, []model.Listener{{Proto: "tcp", Port: 8080, PID: 22, JobID: "com.example.other"}})
	if items[0].Health != "warn" || !strings.Contains(strings.Join(items[0].Notes, " "), "held by com.example.other") {
		t.Fatal(items[0])
	}
}
