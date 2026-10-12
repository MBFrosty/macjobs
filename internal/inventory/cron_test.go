package inventory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MBFrosty/macjobs/internal/cron"
	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/registry"
)

type cronExit int

func (e cronExit) Error() string { return "private subprocess detail" }
func (e cronExit) ExitCode() int { return int(e) }

type cronReplay struct {
	t    *testing.T
	data []byte
	err  error
}

func (r cronReplay) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name != "crontab" || len(args) != 1 || args[0] != "-l" {
		r.t.Fatal("unexpected command", name, args)
	}
	return r.data, r.err
}
func TestCronCollection(t *testing.T) {
	c, r, reg, dir := fixtureCollector(t)
	r.data["crontab -l"] = read(t, "crontab-user.txt")
	c.SystemCrontab = filepath.Join(dir, "crontab")
	if err := os.WriteFile(c.SystemCrontab, read(t, "crontab-system.txt"), 0600); err != nil {
		t.Fatal(err)
	}
	userEntries := cron.Parse(r.data["crontab -l"], "user").Entries
	reg.Entries = append(reg.Entries, registry.Entry{ID: userEntries[0].ID, Kind: "cron", Owner: "ops", Tags: []string{"nightly"}}, registry.Entry{ID: "cron:system:orphan", Kind: "cron"})
	result := c.Collect(context.Background(), reg, false)
	count := 0
	for _, i := range result.Report.Items {
		if i.Kind != "cron" {
			continue
		}
		count++
		if i.ID == userEntries[0].ID && (i.Owner != "ops" || len(i.Tags) != 1 || i.Health != "ok") {
			t.Fatal(i)
		}
		if i.ID == "cron:system:orphan" {
			if i.Health != "fail" || i.State != "absent" {
				t.Fatal(i)
			}
			continue
		}
		if !i.ReadOnly || !i.Live || !i.Observed || i.State != "scheduled" || i.Line == 0 {
			t.Fatal(i)
		}
		if i.Source == "system" && i.RunAs == "" {
			t.Fatal(i)
		}
		if i.CronEntry.Env["SHELL"] != "" && i.CronEntry.Env["SHELL"] != "[REDACTED]" {
			t.Fatal(i.CronEntry)
		}
		if i.CronMalformed && (i.Health != "warn" || i.Command != "") {
			t.Fatal(i)
		}
	}
	if count != 11 {
		t.Fatal(count)
	}
}
func TestCronAvailability(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
		err  error
		warn bool
	}{
		{"empty", "", nil, false}, {"no-crontab", "", cronExit(1), false}, {"other-exit", "", cronExit(2), true}, {"exit-one-output", "partial secret", cronExit(1), true}, {"unavailable", "", errors.New("secret diagnostic"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Collector{Runner: cronReplay{t, []byte(tc.data), tc.err}}
			result := Result{Report: model.NewReport()}
			items := map[string]*model.Item{}
			known := c.collectCron(context.Background(), items, &result)
			if known["user"] == tc.warn || (len(result.Report.Warnings) > 0) != tc.warn || len(items) != 0 {
				t.Fatal(known, items, result.Report.Warnings)
			}
			if strings.Contains(strings.Join(result.Report.Warnings, " "), "secret") {
				t.Fatal("raw failure exposed")
			}
		})
	}
}
func TestSystemCronAvailability(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crontab")
	c := Collector{Runner: cronReplay{t, nil, nil}, SystemCrontab: path}
	check := func(warn, known bool) {
		t.Helper()
		r := Result{Report: model.NewReport()}
		k := c.collectCron(context.Background(), map[string]*model.Item{}, &r)
		if (len(r.Report.Warnings) > 0) != warn || k["system"] != known {
			t.Fatal(k, r.Report.Warnings)
		}
	}
	check(false, true) // missing is empty, not unavailable
	if err := os.WriteFile(path, []byte("@daily root /bin/echo safe\n"), 0600); err != nil {
		t.Fatal(err)
	}
	check(false, true)
	if err := os.Chmod(path, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0600) })
	if os.Getuid() != 0 {
		check(true, false)
	}
	c.SystemCrontab = filepath.Dir(path) // deterministic read failure, including root
	check(true, false)
}
func TestUnavailableCronRegistryEntries(t *testing.T) {
	c, r, reg, dir := fixtureCollector(t)
	r.fail["crontab -l"] = true
	c.SystemCrontab = dir
	reg.Entries = []registry.Entry{{ID: "cron:user:orphan", Kind: "cron"}, {ID: "cron:system:orphan", Kind: "cron"}}
	for _, i := range c.Collect(context.Background(), reg, false).Report.Items {
		if i.Kind == "cron" && i.Health != "unknown" {
			t.Fatal(i)
		}
	}
}
