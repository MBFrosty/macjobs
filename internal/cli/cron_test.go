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

	"github.com/MBFrosty/macjobs/internal/actions"
	"github.com/MBFrosty/macjobs/internal/cron"
	"github.com/MBFrosty/macjobs/internal/inventory"
	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/registry"
	"github.com/MBFrosty/macjobs/internal/runner"
	"github.com/spf13/cobra"
)

type cronFixtureRunner struct {
	t    *testing.T
	user []byte
}

func (f cronFixtureRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.t.Helper()
	if !runner.Allowed(name, args) {
		f.t.Fatal("unsafe inventory command", name, args)
	}
	switch name {
	case "crontab":
		return f.user, nil
	case "launchctl":
		if args[0] == "print" {
			return []byte("domain = {\n services = {\n }\n}\n"), nil
		}
		return []byte("disabled services = {\n}\n"), nil
	case "brew":
		return []byte("[]"), nil
	case "tailscale":
		return []byte("{}"), nil
	default:
		return nil, nil
	}
}
func cronFixture(t *testing.T) (Collect, string, string) {
	t.Helper()
	data, err := os.ReadFile("../../testdata/crontab-user.txt")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	system := filepath.Join(dir, "crontab")
	systemData, err := os.ReadFile("../../testdata/crontab-system.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(system, systemData, 0600); err != nil {
		t.Fatal(err)
	}
	id := cron.Parse(data, "user").Entries[0].ID
	reg := registry.Empty()
	reg.Entries = []registry.Entry{{ID: id, Kind: "cron", Owner: "ops", Tags: []string{"nightly"}}}
	path := filepath.Join(dir, "registry.yaml")
	if err := registry.Save(path, reg, false); err != nil {
		t.Fatal(err)
	}
	c := &inventory.Collector{Runner: cronFixtureRunner{t, data}, Home: filepath.Join("/Users", "alice"), UID: 501, TailscalePath: "tailscale", SystemCrontab: system}
	return func(ctx context.Context, r registry.Registry, all bool) (inventory.Result, error) {
		return c.Collect(ctx, r, all), nil
	}, path, id
}
func cronCommand(t *testing.T, collect Collect, path string, args ...string) (string, error) {
	t.Helper()
	factory := func(_ *cobra.Command, path string) (actions.Service, error) {
		return actions.Service{Runner: forbiddenRunner{t}, RegistryPath: path}, nil
	}
	cmd := NewWithActions("test", collect, factory)
	b := &bytes.Buffer{}
	cmd.SetOut(b)
	cmd.SetErr(b)
	cmd.SetArgs(append([]string{"--registry", path}, args...))
	err := cmd.Execute()
	// A regression must never expose any invented secret, including malformed text.
	for _, secret := range []string{"fixture-env-value", "fixture-cron-secret", "fixture-flag-secret", "fixture-url-secret", "fixture-basic-secret", "fixture-bearer-secret", "fixture-malformed-secret", "INLINE=short"} {
		if strings.Contains(b.String(), secret) {
			t.Fatalf("secret leaked for %v", args)
		}
	}
	return b.String(), err
}
func cronGolden(t *testing.T, name, got string) {
	t.Helper()
	path := "../../testdata/" + name
	if os.Getenv("MACJOBS_UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch %s:\n%s", name, got)
	}
}
func TestCronCLI(t *testing.T) {
	collect, path, id := cronFixture(t)
	got, err := cronCommand(t, collect, path, "list", "--kind", "cron", "--json")
	if err != nil {
		t.Fatal(err)
	}
	cronGolden(t, "cron.golden.json", got)
	var report model.Report
	if err := json.Unmarshal([]byte(got), &report); err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != 1 || len(report.Items) != 10 || len(report.Warnings) != 0 {
		t.Fatal(report)
	}
	got, err = cronCommand(t, collect, path, "show", id, "--json", "--show-env")
	if err != nil {
		t.Fatal(err)
	}
	cronGolden(t, "cron-show.golden.json", got)
	var show map[string]any
	if err := json.Unmarshal([]byte(got), &show); err != nil {
		t.Fatal(err)
	}
	if show["plist"] != nil || show["entry"] == nil {
		t.Fatal(show)
	}
	got, err = cronCommand(t, collect, path, "list", "--kind", "cron", "--owner", "ops", "--tag", "nightly", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(got), &report); err != nil || len(report.Items) != 1 || report.Items[0].ID != id {
		t.Fatal(got, err)
	}
	for _, args := range [][]string{{"list", "--kind", "cron"}, {"show", id}, {"doctor"}, {"doctor", "--json"}} {
		if _, err := cronCommand(t, collect, path, args...); err != nil {
			t.Fatal(args, err)
		}
	}
	if _, err := cronCommand(t, collect, path, "list", "--kind", "bogus"); err == nil {
		t.Fatal("invalid kind accepted")
	}
}
func TestCronDoctorManagedMissingProgram(t *testing.T) {
	collect, path, _ := cronFixture(t)
	reg, err := registry.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../testdata/crontab-user.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range cron.Parse(data, "user").Entries {
		if strings.Contains(e.Command, "missing-macjobs") {
			reg.Entries = append(reg.Entries, registry.Entry{ID: e.ID, Kind: "cron", Managed: true})
		}
	}
	if err := registry.Save(path, reg, true); err != nil {
		t.Fatal(err)
	}
	got, err := cronCommand(t, collect, path, "doctor", "--json")
	if !errors.Is(err, ErrUnhealthy) || !strings.Contains(got, `"health": "fail"`) {
		t.Fatal(got, err)
	}
}
func TestCronCLIRefusesActions(t *testing.T) {
	collect, path, id := cronFixture(t)
	for _, verb := range []string{"start", "stop", "restart", "enable", "disable", "remove", "edit"} {
		for _, flags := range [][]string{nil, {"--dry-run"}, {"--yes"}, {"--dry-run", "--yes"}} {
			args := append([]string{verb, id, "--json"}, flags...)
			got, err := cronCommand(t, collect, path, args...)
			var report actions.Report
			if e := json.Unmarshal([]byte(got), &report); e != nil {
				t.Fatal(got, e)
			}
			if !errors.Is(err, ErrActionReported) || report.Result != "refused" || report.Error.Code != "display_only" || len(report.Ops) != 0 {
				t.Fatal(got, err)
			}
		}
	}
	for _, dry := range [][]string{nil, {"--dry-run"}} {
		got, err := cronCommand(t, collect, path, append([]string{"create", "cron", "--name", "example", "--json"}, dry...)...)
		if !errors.Is(err, ErrActionReported) || !strings.Contains(got, "cron items are read-only in macjobs") {
			t.Fatal(got, err)
		}
	}
}
