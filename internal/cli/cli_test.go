package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MBFrosty/macjobs/internal/inventory"
	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/redact"
	"github.com/MBFrosty/macjobs/internal/registry"
)

func commands(t *testing.T, args ...string) (string, error) {
	t.Helper()
	r := model.NewReport()
	r.Items = []model.Item{{ID: "com.example.worker", Kind: "launchd", Label: "com.example.worker", Owner: "team", Health: "ok", State: "running", Live: true, ReadOnly: true, Notes: []string{}, Plist: map[string]any{"EnvironmentVariables": map[string]any{"MODE": "example", "API_TOKEN": "hidden-value"}}}, {ID: "com.example.orphan", Owner: "unknown", Kind: "launchd", State: "absent", Health: "fail", ReadOnly: true, Notes: []string{"registry entry has no live job"}}}
	collect := func(context.Context, registry.Registry, bool) (inventory.Result, error) {
		return inventory.Result{Report: r}, nil
	}
	cmd := New("test", collect)
	b := &bytes.Buffer{}
	cmd.SetOut(b)
	cmd.SetErr(b)
	cmd.SetArgs(append([]string{"--registry", filepath.Join(t.TempDir(), "missing")}, args...))
	e := cmd.Execute()
	return b.String(), e
}
func TestRootHasNoMJAlias(t *testing.T) {
	cmd := New("test", func(context.Context, registry.Registry, bool) (inventory.Result, error) {
		return inventory.Result{Report: model.NewReport()}, nil
	})
	if len(cmd.Aliases) != 0 {
		t.Fatal(cmd.Aliases)
	}
	b := &bytes.Buffer{}
	cmd.SetOut(b)
	cmd.SetErr(b)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "Aliases") || strings.Contains(b.String(), "mj") {
		t.Fatal(b.String())
	}
}

func TestJSONAndFilters(t *testing.T) {
	s, e := commands(t, "list", "--json", "--owner", "team", "--kind", "launchd")
	if e != nil {
		t.Fatal(e)
	}
	var r model.Report
	if e = json.Unmarshal([]byte(s), &r); e != nil || r.SchemaVersion != 1 || len(r.Items) != 1 {
		t.Fatal(s, e)
	}
	s, e = commands(t, "list", "--json", "--unhealthy")
	if e != nil {
		t.Fatal(e)
	}
	json.Unmarshal([]byte(s), &r)
	if len(r.Items) != 1 || r.Items[0].Health != "fail" {
		t.Fatal(s)
	}
}
func TestDoctorFailsAfterReporting(t *testing.T) {
	s, e := commands(t, "doctor", "--json")
	if !errors.Is(e, ErrUnhealthy) || !strings.Contains(s, "com.example.orphan") {
		t.Fatal(s, e)
	}
}

func TestDoctorRedactsMissingProgramPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret-program")
	r := model.NewReport()
	r.Items = []model.Item{{ID: "com.example.worker", Kind: "launchd", Health: "fail", Notes: []string{"program not found: " + path}}}
	collect := func(context.Context, registry.Registry, bool) (inventory.Result, error) {
		return inventory.Result{Report: r, Redactor: redact.Redactor{Values: []string{path}}}, nil
	}
	cmd := New("test", collect)
	b := &bytes.Buffer{}
	cmd.SetOut(b)
	cmd.SetArgs([]string{"--registry", filepath.Join(t.TempDir(), "missing"), "doctor", "--json"})
	e := cmd.Execute()
	if !errors.Is(e, ErrUnhealthy) || strings.Contains(b.String(), path) || !strings.Contains(b.String(), redact.Hidden) {
		t.Fatal(b.String(), e)
	}
}
func TestShowAlwaysRedactsEnvironment(t *testing.T) {
	for _, args := range [][]string{{"show", "com.example.worker"}, {"show", "com.example.worker", "--show-env"}} {
		s, e := commands(t, args...)
		if e != nil || strings.Contains(s, "hidden-value") {
			t.Fatal(s, e)
		}
		var payload struct {
			Plist map[string]any `json:"plist"`
		}
		if err := json.Unmarshal([]byte(s), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Plist["EnvironmentVariables"].(map[string]any)["MODE"] != redact.Hidden {
			t.Fatal("environment value exposed", s)
		}
	}
}
func TestRegistryInitScopeAndForce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.yaml")
	s, e := commands(t, "--registry", path, "registry", "init")
	if e != nil {
		t.Fatal(s, e)
	}
	r, e := registry.Load(path)
	if e != nil || len(r.Entries) != 1 || r.Entries[0].Managed || r.Entries[0].CreatedByTool || r.Entries[0].Owner != "unknown" {
		t.Fatal(r, e)
	}
	if _, e = commands(t, "--registry", path, "registry", "init"); e == nil {
		t.Fatal("overwrote existing file")
	}
	if _, e = commands(t, "--registry", path, "registry", "init", "--force"); e != nil {
		t.Fatal(e)
	}
}
func TestTailAndRedactedLogs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "example.log")
	os.WriteFile(path, []byte("first\nAuthorization: Bearer hidden-value\nlast\n"), 0600)
	f, e := os.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	lines, offset, e := Tail(f, 2)
	f.Close()
	if e != nil || len(lines) != 2 || lines[1] != "last" || offset == 0 {
		t.Fatal(lines, offset, e)
	}
	b := &bytes.Buffer{}
	e = Logs(context.Background(), b, model.Item{ID: "example", Logs: []string{path}}, 2, false, true, redact.Redactor{})
	if e != nil || strings.Contains(b.String(), "hidden-value") || !strings.Contains(b.String(), `"schemaVersion":1`) {
		t.Fatal(b.String(), e)
	}
}
func TestUnknownJobAndCommands(t *testing.T) {
	for _, args := range [][]string{{"show", "missing"}, {"start", "example"}, {"logs", "com.example.worker", "-n", "-1"}} {
		if _, e := commands(t, args...); e == nil {
			t.Fatal("accepted", args)
		}
	}
}

type observedWriter struct {
	bytes.Buffer
	opened, redacted     chan struct{}
	openOnce, redactOnce sync.Once
}

func (w *observedWriter) Write(b []byte) (int, error) {
	if bytes.Contains(b, []byte("==>")) {
		w.openOnce.Do(func() { close(w.opened) })
	}
	if bytes.Contains(b, []byte("[REDACTED]")) {
		w.redactOnce.Do(func() { close(w.redacted) })
	}
	return w.Buffer.Write(b)
}
func TestFollowKeepsPartialCredentialsTogether(t *testing.T) {
	path := filepath.Join(t.TempDir(), "example.log")
	os.WriteFile(path, []byte("Authorization: Bearer hidden-"), 0600)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &observedWriter{opened: make(chan struct{}), redacted: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- Logs(ctx, w, model.Item{ID: "example", Logs: []string{path}}, 1, true, false, redact.Redactor{})
	}()
	select {
	case <-w.opened:
	case <-time.After(3 * time.Second):
		t.Fatal("follow did not initialize")
	}
	f, e := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	f.WriteString("value\n")
	f.Close()
	select {
	case <-w.redacted:
	case <-time.After(3 * time.Second):
		t.Fatal("follow did not emit redacted line")
	}
	cancel()
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if strings.Contains(w.String(), "hidden-") || strings.Contains(w.String(), "value") {
		t.Fatal("partial credential exposed")
	}
}
