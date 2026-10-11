package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MBFrosty/macjobs/internal/actions"
	"github.com/MBFrosty/macjobs/internal/inventory"
	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/registry"
	"github.com/MBFrosty/macjobs/internal/watchdog"
	"github.com/spf13/cobra"
)

func TestCreationCLIWithoutExternalCommands(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "com.example.worker.plist"), []byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>Label</key><string>com.example.worker</string></dict></plist>`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"create", "interval", "--name", "example", "--every", "10m", "--tag", "HOMELAB,z", "--tag", "homelab", "--dry-run", "--", "/bin/echo", "example"},
		{"create", "calendar", "--name", "example", "--at", "Sun 03:00", "--at", "Wed 15:30", "--dry-run", "--", "/bin/echo", "example"},
		{"create", "keepalive", "--name", "example", "--dry-run", "--", "/bin/sleep", "3600"},
		{"create", "app-watchdog", "--name", "example", "--app", "Example App", "--every", "10m", "--dry-run"},
		{"create", "app-watchdog", "--name", "example", "--app", "Example App", "--every", "10m", "--probe-log", "~/app.log", "--connected-regex", "link=connected", "--disconnected-regex", "link=disconnected", "--tcp-peer", "relay.example.invalid:443", "--probe-http", "http://127.0.0.1:12345/status", "--max-age", "30s", "--restart-on-disconnect", "--dry-run"},
		{"adopt", "com.example.worker", "--owner", "team", "--purpose", "Example worker", "--tag", "homelab", "--dry-run"},
	} {
		collect := func(context.Context, registry.Registry, bool) (inventory.Result, error) {
			t.Fatal("dry-run collected host inventory")
			return inventory.Result{}, errors.New("unexpected collection")
		}
		factory := func(_ *cobra.Command, path string) (actions.Service, error) {
			return actions.Service{Home: home, UID: 501, RegistryPath: path, Runner: forbiddenRunner{t}}, nil
		}
		cmd := NewWithActions("test", collect, factory)
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetArgs(append([]string{"--json", "--registry", filepath.Join(home, "registry.yaml")}, args...))
		if err := cmd.Execute(); err != nil {
			t.Fatal(out.String(), err)
		}
		var report actions.CreateReport
		if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.SchemaVersion != 1 || report.Result != "planned" {
			t.Fatal(out.String(), err)
		}
		if strings.Contains(strings.Join(args, " "), "--tag") && (len(report.Tags) == 0 || report.Tags[0] != "homelab") {
			t.Fatal("missing preview tags", report)
		}
		if strings.Contains(strings.Join(args, " "), "--probe-log") {
			for _, expected := range []string{"--probe-log", filepath.Join(home, "app.log"), "--tcp-peer", "relay.example.invalid:443", "--interval", "--max-age", "--restart-on-disconnect"} {
				if !strings.Contains(report.Plist, expected) {
					t.Fatal("missing generated arg", expected, report.Plist)
				}
			}
		}
		if _, err := os.Lstat(filepath.Join(home, "registry.yaml")); !os.IsNotExist(err) {
			t.Fatal("preview created registry")
		}
	}
}
func TestWatchdogCLIAppendsWithoutDuplicatingStdout(t *testing.T) {
	home := t.TempDir()
	log := filepath.Join(home, "watchdog.log")
	watch := func() (watchdog.Service, error) {
		return watchdog.Service{Runner: runningReplay{}, Home: home, Applications: []string{}, Now: func() time.Time { return time.Date(2026, 10, 10, 18, 30, 0, 0, time.UTC) }}, nil
	}
	cmd := NewWithServices("test", nil, nil, watch)
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetArgs([]string{"watchdog", "check", "--app", "Example App", "--log", log})
	if err := cmd.Execute(); err != nil || out.Len() != 0 {
		t.Fatal(out.String(), err)
	}
	b, err := os.ReadFile(log)
	if err != nil || strings.Count(string(b), "\n") != 1 || !strings.Contains(string(b), "relaunched=no") {
		t.Fatal(string(b), err)
	}
}

type runningReplay struct{}

func (runningReplay) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name == "pgrep" {
		return []byte("42\n"), nil
	}
	if name == "lsof" {
		return []byte("p42\nnlocalhost:6000->localhost:443\n"), nil
	}
	return nil, errors.New("unexpected command")
}

func TestWatchdogCLIProbeFlags(t *testing.T) {
	home := t.TempDir()
	source := filepath.Join(home, "app.log")
	if err := os.WriteFile(source, []byte("2026-10-10T18:29:59Z link=connected\n"), 0600); err != nil {
		t.Fatal(err)
	}
	watch := func() (watchdog.Service, error) {
		return watchdog.Service{Runner: runningReplay{}, Home: home, Applications: []string{}, Now: func() time.Time { return time.Date(2026, 10, 10, 18, 30, 0, 0, time.UTC) }}, nil
	}
	cmd := NewWithServices("test", nil, nil, watch)
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetArgs([]string{"watchdog", "check", "--app", "Example App", "--log", filepath.Join(home, "watchdog.log"), "--probe-log", "~/app.log", "--connected-regex", "link=connected", "--disconnected-regex", "link=disconnected", "--interval", "1m", "--max-age", "30s", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(out.String(), err)
	}
	var result watchdog.Result
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Connected != "yes" || len(result.Probes) != 1 || result.Probes[0].Probe != "log" {
		t.Fatal(result)
	}
}

func TestCreateAdoptTagFlagsPersist(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "registry.yaml")
	plist := filepath.Join(dir, "com.example.worker.plist")
	if err := os.WriteFile(plist, []byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>Label</key><string>com.example.worker</string></dict></plist>`), 0600); err != nil {
		t.Fatal(err)
	}
	collect := func(context.Context, registry.Registry, bool) (inventory.Result, error) {
		report := model.NewReport()
		report.Items = append(report.Items, model.Item{ID: "com.example.worker", Label: "com.example.worker", Kind: "launchd", Domain: "gui/501", PlistPath: plist})
		return inventory.Result{Report: report}, nil
	}
	factory := func(_ *cobra.Command, p string) (actions.Service, error) {
		return actions.Service{Home: home, UID: 501, RegistryPath: p, Runner: creationReplay{}}, nil
	}
	run := func(args ...string) error {
		cmd := NewWithActions("test", collect, factory)
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetArgs(append([]string{"--registry", path, "--json"}, args...))
		return cmd.Execute()
	}
	if err := run("create", "interval", "--name", "example", "--every", "10m", "--tag", "HOMELAB,z", "--tag", "homelab", "--", "/bin/echo", "example"); err != nil {
		t.Fatal(err)
	}
	if err := run("adopt", "com.example.worker", "--tag", "a", "--tag", "HOMELAB"); err != nil {
		t.Fatal(err)
	}
	if err := run("adopt", "com.example.worker", "--tag", "b"); err != nil {
		t.Fatal(err)
	}
	reg, err := registry.Load(path)
	if err != nil || len(reg.Entries) != 2 || !reflect.DeepEqual(reg.Entries[0].Tags, []string{"homelab", "z"}) || !reflect.DeepEqual(reg.Entries[1].Tags, []string{"a", "b", "homelab"}) {
		t.Fatal(reg, err)
	}
	before, _ := os.ReadFile(path)
	for _, args := range [][]string{
		{"create", "interval", "--name", "bad", "--every", "10m", "--tag", "bad/tag", "--", "/bin/echo", "example"},
		{"adopt", "com.example.worker", "--tag", "bad/tag"},
	} {
		if err := run(args...); err == nil {
			t.Fatal("accepted invalid tags")
		}
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("invalid tags changed registry")
	}
}

type creationReplay struct{}

func (creationReplay) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name == "launchctl" && args[0] == "print" {
		return []byte("gui/501 = {\n services = {\n }\n}\n"), nil
	}
	if name == "plutil" || name == "launchctl" && (args[0] == "print-disabled" || args[0] == "enable" || args[0] == "bootstrap") {
		return nil, nil
	}
	return nil, errors.New("unexpected replay command")
}
