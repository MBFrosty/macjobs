package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/registry"
	"github.com/MBFrosty/macjobs/internal/runner"
)

type replay struct {
	t     *testing.T
	data  map[string][]byte
	fail  map[string]bool
	calls []string
}

func (r *replay) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.t.Helper()
	if !runner.Allowed(name, args) {
		r.t.Fatalf("unsafe command: %s %v", name, args)
	}
	k := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, k)
	if r.fail[k] {
		return nil, errors.New("unavailable")
	}
	b, ok := r.data[k]
	if !ok {
		r.t.Fatalf("unexpected command: %s", k)
	}
	return b, nil
}
func read(t *testing.T, name string) []byte {
	t.Helper()
	b, e := os.ReadFile("../../testdata/" + name)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func fixtureCollector(t *testing.T) (*Collector, *replay, registry.Registry, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "com.example.worker.plist")
	log := filepath.Join(dir, "example.log")
	p := strings.ReplaceAll(string(read(t, "worker.plist")), "/home/example/Library/Logs/example.log", log)
	if e := os.WriteFile(path, []byte(p), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(log, []byte("example\n"), 0600); e != nil {
		t.Fatal(e)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if e := os.Chtimes(log, now, now); e != nil {
		t.Fatal(e)
	}
	domain := []byte("gui/501 = {\n\tservices = {\n\t\t1234 0 com.example.worker\n\t\t0 0 com.apple.example\n\t}\n}\n")
	state := strings.ReplaceAll(string(read(t, "launchctl-running.txt")), "/home/example/Library/LaunchAgents/com.example.worker.plist", path)
	r := &replay{t: t, fail: map[string]bool{}, data: map[string][]byte{
		"launchctl print gui/501": domain, "launchctl print system": []byte("system = {\n\tservices = {\n\t}\n}\n"),
		"launchctl print-disabled gui/501": read(t, "launchctl-disabled.txt"), "launchctl print-disabled system": []byte("disabled services = {\n}\n"),
		"launchctl print gui/501/com.example.worker": []byte(state), "launchctl print gui/501/com.apple.example": []byte("job = {\n\tstate = not running\n}\n"),
		"crontab -l": []byte(""), "brew services list --json": read(t, "brew.json"), "tailscale serve status --json": read(t, "tailscale-status.json"), "tailscale serve get-config --all": read(t, "tailscale-get-config.json"),
		"lsof -nP -iTCP -sTCP:LISTEN": read(t, "lsof.txt"), "lsof -nP -iUDP": []byte(""), "ps -axo pid=,ppid=,comm=": read(t, "ps-tree.txt"), "ps -axo pid=,comm=": read(t, "ps.txt"),
	}}
	c := &Collector{Runner: r, Home: dir, UID: 501, TailscalePath: "tailscale", PlistDirs: map[string]string{dir: "launchd"}, Now: func() time.Time { return now }}
	reg := registry.Empty()
	reg.Entries = []registry.Entry{{ID: "com.example.worker", Owner: "team", Purpose: "Example worker", Port: 8080}, {ID: "com.example.orphan", Owner: "unknown"}, {ID: "example-process", Owner: "team", Match: &registry.Match{Name: "example", Port: 8080}}}
	return c, r, reg, dir
}
func TestGoldenInventory(t *testing.T) {
	c, _, reg, dir := fixtureCollector(t)
	result := c.Collect(context.Background(), reg, false)
	b, e := json.MarshalIndent(result.Report, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	b = []byte(strings.ReplaceAll(string(b), dir, "/example") + "\n")
	path := "../../testdata/inventory.golden.json"
	if os.Getenv("MACJOBS_UPDATE_GOLDEN") == "1" {
		if e := os.WriteFile(path, b, 0644); e != nil {
			t.Fatal(e)
		}
	}
	want, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if string(b) != string(want) {
		t.Fatalf("inventory JSON changed:\n%s", b)
	}
}
func TestUnavailableDomainDoesNotInventOrphans(t *testing.T) {
	c, r, reg, _ := fixtureCollector(t)
	r.fail["launchctl print gui/501"] = true
	r.fail["launchctl list"] = true
	result := c.Collect(context.Background(), reg, false)
	for _, i := range result.Report.Items {
		if i.ID == "com.example.orphan" && i.Health != "unknown" {
			t.Fatal(i)
		}
	}
}
func TestListFallbackAndAll(t *testing.T) {
	c, r, reg, _ := fixtureCollector(t)
	r.fail["launchctl print gui/501"] = true
	r.data["launchctl list"] = []byte("PID Status Label\n1234 0 com.example.worker\n")
	result := c.Collect(context.Background(), reg, false)
	if len(result.Report.Warnings) == 0 {
		t.Fatal("missing fallback warning")
	}
	delete(r.fail, "launchctl print gui/501")
	result = c.Collect(context.Background(), reg, true)
	found := false
	for _, i := range result.Report.Items {
		if i.ID == "com.apple.example" {
			found = true
		}
	}
	if !found {
		t.Fatal("--all omitted Apple job")
	}
}
func TestBrewDeduplication(t *testing.T) {
	c, r, reg, dir := fixtureCollector(t)
	r.data["launchctl print gui/501"] = []byte("gui/501 = {\n\tservices = {\n\t\t1234 0 homebrew.mxcl.example-service-0\n\t}\n}\n")
	r.data["launchctl print gui/501/homebrew.mxcl.example-service-0"] = []byte("job = {\n\tstate = running\n\tpid = 1234\n}\n")
	os.Remove(filepath.Join(dir, "com.example.worker.plist"))
	result := c.Collect(context.Background(), reg, false)
	n := 0
	for _, i := range result.Report.Items {
		if i.ID == "homebrew.mxcl.example-service-0" {
			n++
			if i.Kind != "launchd" || i.Owner != "homebrew" {
				t.Fatal(i)
			}
		}
	}
	if n != 1 {
		t.Fatalf("dedupe got %d", n)
	}
}
func TestSubmittedJobsHaveNoMissingPlist(t *testing.T) {
	c, r, reg, _ := fixtureCollector(t)
	r.data["launchctl print gui/501/com.example.worker"] = []byte("job = {\n\tpath = (submitted by example.1234)\n\tstate = running\n\tpid = 1234\n}\n")
	result := c.Collect(context.Background(), reg, false)
	for _, i := range result.Report.Items {
		if i.ID == "com.example.worker" && (i.PlistMissing || i.PlistPath != "") {
			t.Fatal(i)
		}
	}
}
func TestBenignEnvironmentValuesDoNotChangeIDs(t *testing.T) {
	c, _, reg, dir := fixtureCollector(t)
	path := filepath.Join(dir, "com.example.worker.plist")
	p := string(read(t, "worker.plist"))
	p = strings.ReplaceAll(p, "<key>EXAMPLE_VALUE</key>", "<key>MODE</key>")
	p = strings.ReplaceAll(p, "<string>[REDACTED]</string>", "<string>3</string>")
	os.WriteFile(path, []byte(p), 0600)
	result := c.Collect(context.Background(), reg, false)
	b, e := json.Marshal(result.Redactor.Value(map[string]any{"id": "service3", "owner": "team3", "port": 443}, true))
	if e != nil || !strings.Contains(string(b), "service3") || !strings.Contains(string(b), "team3") {
		t.Fatal(string(b), e)
	}
}
func TestMissingRegisteredBrewService(t *testing.T) {
	c, _, reg, _ := fixtureCollector(t)
	reg.Entries = append(reg.Entries, registry.Entry{ID: "homebrew.mxcl.missing", Kind: "brew", Owner: "unknown"})
	result := c.Collect(context.Background(), reg, false)
	for _, i := range result.Report.Items {
		if i.ID == "homebrew.mxcl.missing" && i.Health != "fail" {
			t.Fatal(i)
		}
	}
}
func TestSystemHomebrewDeduplication(t *testing.T) {
	c, r, reg, _ := fixtureCollector(t)
	r.data["launchctl print system"] = []byte("system = {\n\tservices = {\n\t\t1234 0 homebrew.mxcl.example-service-0\n\t}\n}\n")
	r.data["launchctl print system/homebrew.mxcl.example-service-0"] = []byte("job = {\n\tstate = running\n\tpid = 1234\n}\n")
	result := c.Collect(context.Background(), reg, false)
	for _, i := range result.Report.Items {
		if i.ID == "homebrew.mxcl.example-service-0" {
			t.Fatal("duplicate user brew item")
		}
		if i.ID == "system:homebrew.mxcl.example-service-0" && i.Owner != "homebrew" {
			t.Fatal(i)
		}
	}
}

func TestPortSnapshotFailureIsUnknown(t *testing.T) {
	c, r, reg, _ := fixtureCollector(t)
	r.fail["lsof -nP -iTCP -sTCP:LISTEN"] = true
	r.fail["lsof -nP -iUDP"] = true
	r.fail["ps -axo pid=,ppid=,comm="] = true
	result := c.Collect(context.Background(), reg, false)
	if !strings.Contains(strings.Join(result.Report.Warnings, " "), "listening ports unavailable") || !strings.Contains(strings.Join(result.Report.Warnings, " "), "process tree unavailable") {
		t.Fatal(result.Report.Warnings)
	}
	for _, i := range result.Report.Items {
		if i.ID == "com.example.worker" && i.Health == "fail" && strings.Contains(strings.Join(i.Notes, " "), "registered port is not listening") {
			t.Fatal(i)
		}
	}
}

func TestPortsAttributionAndJSON(t *testing.T) {
	c, r, reg, dir := fixtureCollector(t)
	r.data["lsof -nP -iTCP -sTCP:LISTEN"] = read(t, "lsof-ports-tcp.txt")
	r.data["lsof -nP -iUDP"] = read(t, "lsof-ports-udp.txt")
	plist := filepath.Join(dir, "com.example.web.plist")
	if e := os.WriteFile(plist, []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>com.example.web</string>
<key>ProgramArguments</key><array><string>/usr/bin/web</string><string>--port</string><string>9090</string></array>
<key>KeepAlive</key><true/>
<key>Sockets</key><dict><key>HTTP</key><dict><key>SockServiceName</key><string>9092</string></dict></dict>
</dict></plist>`), 0600); e != nil {
		t.Fatal(e)
	}
	c.PlistDirs[dir] = "launchd"
	r.data["launchctl print gui/501"] = []byte("gui/501 = {\n\tservices = {\n\t\t1234 0 com.example.worker\n\t\t2001 0 com.example.web\n\t}\n}\n")
	r.data["launchctl print gui/501/com.example.web"] = []byte("job = {\n\tpath = " + plist + "\n\tstate = running\n\tpid = 2001\n}\n")
	reg.Entries = append(reg.Entries, registry.Entry{ID: "com.example.web", Owner: "team", Ports: []int{9090}})
	result := c.Collect(context.Background(), reg, false)
	found := map[string]model.Listener{}
	for _, l := range result.Listeners {
		found[l.Proto+":"+l.Command+":"+itoa(l.Port)] = l
	}
	if found["tcp:web:9090"].JobID != "com.example.web" || found["udp:child:5354"].JobID != "com.example.web" || found["udp:child:5354"].Via != "ppid" {
		t.Fatal(result.Listeners)
	}
	if found["tcp:other:9100"].JobID != "" {
		t.Fatal("unattributed assigned", found["tcp:other:9100"])
	}
	if !strings.Contains(strings.Join(result.Report.Warnings, " "), "listeners have no known job") {
		t.Fatal(result.Report.Warnings)
	}
	for _, i := range result.Report.Items {
		if i.ID == "com.example.web" {
			sources := map[string]bool{}
			for _, p := range i.Ports {
				sources[p.Source] = true
				if p.Port == 9090 && p.Source == "declared" && !p.Listening {
					t.Fatal(p)
				}
			}
			if !sources["declared"] || !sources["inferred"] || !sources["socket"] || !sources["observed"] {
				t.Fatal(i.Ports)
			}
		}
		if i.ID == "tailscale:example-host.example.test:443/" {
			front, back := false, false
			for _, p := range i.Ports {
				if p.Source == "tailscale" && p.Port == 443 {
					front = true
				}
				if p.Source == "tailscale" && p.Port == 8080 && p.Listening {
					back = true
				}
			}
			if !front || !back {
				t.Fatal(i.Ports)
			}
		}
	}
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func TestRegistryTagsMergeEveryKind(t *testing.T) {
	c, _, reg, _ := fixtureCollector(t)
	live := c.Collect(context.Background(), reg, false)
	kinds := map[string]bool{}
	for _, i := range live.Report.Items {
		kinds[i.Kind] = true
		found := false
		for n := range reg.Entries {
			if reg.Entries[n].ID == i.ID {
				reg.Entries[n].Tags = []string{"homelab"}
				found = true
			}
		}
		if !found {
			reg.Entries = append(reg.Entries, registry.Entry{ID: i.ID, Kind: i.Kind, Owner: "team", Tags: []string{"homelab"}})
		}
	}
	reg.Entries = append(reg.Entries, registry.Entry{ID: "system:com.example.absent", Kind: "system", Tags: []string{"homelab"}})
	result := c.Collect(context.Background(), reg, false)
	for _, i := range result.Report.Items {
		kinds[i.Kind] = true
		if len(i.Tags) != 1 || i.Tags[0] != "homelab" {
			t.Fatal(i)
		}
	}
	for _, kind := range []string{"launchd", "system", "brew", "tailscale", "process"} {
		if !kinds[kind] {
			t.Fatal("missing kind", kind)
		}
	}
}
