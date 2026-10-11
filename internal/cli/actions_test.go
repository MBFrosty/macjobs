package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MBFrosty/macjobs/internal/actions"
	"github.com/MBFrosty/macjobs/internal/inventory"
	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/registry"
	"github.com/spf13/cobra"
)

type forbiddenRunner struct{ t *testing.T }

func (f forbiddenRunner) Run(context.Context, string, ...string) ([]byte, error) {
	f.t.Fatal("dry run or refusal called Runner")
	return nil, errors.New("unexpected subprocess")
}

func TestActionJSONGolden(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "com.example.worker.plist")
	if err := os.WriteFile(path, []byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>Label</key><string>com.example.worker</string></dict></plist>`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		args    []string
		failure bool
	}{
		{"start", []string{"start", "com.example.worker", "--dry-run"}, false},
		{"stop", []string{"stop", "com.example.worker", "--dry-run"}, false},
		{"restart", []string{"restart", "com.example.worker", "--dry-run"}, false},
		{"enable", []string{"enable", "com.example.worker", "--dry-run"}, false},
		{"disable", []string{"disable", "com.example.worker", "--dry-run"}, false},
		{"edit", []string{"edit", "com.example.worker", "--dry-run"}, false},
		{"remove", []string{"remove", "com.example.worker", "--dry-run"}, false},
		{"foreign", []string{"restart", "com.example.worker"}, true},
		{"no-tty", []string{"restart", "com.example.worker", "--yes"}, true},
		{"missing", []string{"restart", "missing"}, true},
		{"arguments", []string{"restart"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			collect := func(context.Context, registry.Registry, bool) (inventory.Result, error) {
				report := model.NewReport()
				report.Items = []model.Item{{ID: "com.example.worker", Label: "com.example.worker", Kind: "launchd", PlistPath: path, Live: tc.name != "start" && tc.name != "enable", Plist: map[string]any{"EnvironmentVariables": map[string]any{"MODE": "private-value"}}}}
				return inventory.Result{Report: report}, nil
			}
			factory := func(_ *cobra.Command, path string) (actions.Service, error) {
				return actions.Service{Runner: forbiddenRunner{t}, Home: home, UID: 501, PID: 100, RegistryPath: path, Editor: []string{"example-editor", "--wait"}, Now: func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }}, nil
			}
			cmd := NewWithActions("test", collect, factory)
			out := &bytes.Buffer{}
			cmd.SetOut(out)
			cmd.SetErr(out)
			cmd.SetArgs(append([]string{"--registry", filepath.Join(home, "registry.yaml"), "--json"}, tc.args...))
			err := cmd.Execute()
			if tc.failure != errors.Is(err, ErrActionReported) {
				t.Fatal(out.String(), err)
			}
			var report actions.Report
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatal(out.String(), err)
			}
			normalized := strings.ReplaceAll(out.String(), home, "<home>")
			golden := filepath.Join("..", "..", "testdata", "action-"+tc.name+".golden.json")
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(golden, []byte(normalized), 0600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil || string(want) != normalized {
				t.Fatalf("JSON differs from %s\n%s\n%v", golden, normalized, err)
			}
		})
	}
}
func TestActionRedactsAllEnvironmentValues(t *testing.T) {
	collect := func(context.Context, registry.Registry, bool) (inventory.Result, error) {
		report := model.NewReport()
		report.Items = []model.Item{{ID: "com.example.private-value", Label: "com.example.private-value", Kind: "launchd", Live: true, Plist: map[string]any{"EnvironmentVariables": map[string]any{"MODE": "private-value"}}}}
		return inventory.Result{Report: report}, nil
	}
	cmd := NewWithActions("test", collect, func(*cobra.Command, string) (actions.Service, error) {
		return actions.Service{Runner: forbiddenRunner{t}, UID: 501}, nil
	})
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetArgs([]string{"--registry", filepath.Join(t.TempDir(), "missing"), "restart", "com.example.private-value", "--dry-run", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "private-value") || !strings.Contains(out.String(), "[REDACTED]") {
		t.Fatal(out.String())
	}
}
