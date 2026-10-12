package actions

import (
	"context"
	"os"
	"testing"

	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/registry"
	"github.com/MBFrosty/macjobs/internal/templates"
)

func TestCronLifecycleRefusal(t *testing.T) {
	for _, action := range []string{"start", "stop", "restart", "enable", "disable", "edit", "remove", "create"} {
		for _, dry := range []bool{false, true} {
			for _, yes := range []bool{false, true} {
				t.Run(action, func(t *testing.T) {
					f := &fakeRunner{}
					s := Service{Runner: f}
					i := model.Item{ID: "cron:user:12345678", Kind: "cron", Label: "@daily /bin/echo", Managed: true, CreatedByTool: true, Registered: true}
					r, err := s.Execute(context.Background(), action, i, Options{DryRun: dry, Yes: yes})
					if err == nil || r.Result != "refused" || r.Error.Code != "display_only" || r.Error.Message != "cron items are read-only in macjobs" || len(r.Ops) != 0 || len(f.calls) != 0 {
						t.Fatal(r, err, f.calls)
					}
				})
			}
		}
	}
	for _, dry := range []bool{false, true} {
		s := Service{}
		r, err := s.Create(context.Background(), CreateOptions{Name: "example", Template: templates.Options{Template: "cron"}, DryRun: dry})
		if err == nil || r.Error.Code != "display_only" || len(r.Ops) != 0 {
			t.Fatal(r, err)
		}
		a, err := s.Adopt(context.Background(), model.Item{ID: "cron:user:12345678", Kind: "cron"}, AdoptOptions{DryRun: dry})
		if err == nil || a.Error.Code != "display_only" || len(a.Ops) != 0 {
			t.Fatal(a, err)
		}
	}
}
func TestCronTagsAreMetadataOnly(t *testing.T) {
	s, _, f := setup(t)
	i := model.Item{ID: "cron:user:12345678", Kind: "cron", ReadOnly: true}
	before, _ := os.ReadFile(s.RegistryPath)
	if _, err := s.Tag(context.Background(), i, []string{"nightly"}, true); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(s.RegistryPath)
	if string(before) != string(after) {
		t.Fatal("preview wrote registry")
	}
	if _, err := s.Tag(context.Background(), i, []string{"nightly"}, false); err != nil {
		t.Fatal(err)
	}
	reg, err := registry.Load(s.RegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range reg.Entries {
		if e.ID == i.ID {
			found = true
			if e.Kind != "cron" || e.Managed || e.CreatedByTool || len(e.Tags) != 1 {
				t.Fatal(e)
			}
		}
	}
	if !found {
		t.Fatal("cron tag missing")
	}
	if _, err := s.Untag(context.Background(), i, []string{"nightly"}, false); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 0 {
		t.Fatal("metadata called external runner", f.calls)
	}
}
