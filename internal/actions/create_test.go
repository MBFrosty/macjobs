package actions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/registry"
	"github.com/MBFrosty/macjobs/internal/templates"
)

func createOptions() CreateOptions {
	return CreateOptions{Name: "test", Owner: "team", Purpose: "Example worker", Template: templates.Options{Template: "interval", Every: time.Minute, Command: []string{"/bin/echo", "example"}}}
}

func TestCreateDryRunAndRoundTrip(t *testing.T) {
	s, _, f := setup(t)
	o := createOptions()
	o.DryRun = true
	before, _ := os.ReadFile(s.RegistryPath)
	r, err := s.Create(context.Background(), o)
	after, _ := os.ReadFile(s.RegistryPath)
	if err != nil || r.Result != "planned" || len(f.calls) != 0 || !reflect.DeepEqual(before, after) {
		t.Fatal(r, err, f.calls)
	}
	path := filepath.Join(s.Home, "Library", "LaunchAgents", r.Label+".plist")
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("dry-run wrote plist")
	}
	o.DryRun = false
	r, err = s.Create(context.Background(), o)
	if err != nil || r.Result != "completed" {
		t.Fatal(r, err)
	}
	reg, err := registry.Load(s.RegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	entry := reg.Entries[len(reg.Entries)-1]
	if !entry.Managed || !entry.CreatedByTool || entry.CreatedBy != "macjobs" || entry.Template != "interval" || entry.Owner != "team" {
		t.Fatal(entry)
	}
	if err := os.WriteFile(entry.Log, []byte("example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	i := model.Item{ID: r.ID, Label: r.Label, Kind: "launchd", Domain: "gui/501", PlistPath: path, Registered: true, Managed: true, CreatedByTool: true, Live: true}
	removed, err := s.Execute(context.Background(), "remove", i, Options{PurgeLog: true})
	if err != nil {
		t.Fatal(removed, err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("plist retained")
	}
	if _, err := os.Lstat(entry.Log); !os.IsNotExist(err) {
		t.Fatal("log retained")
	}
	reg, _ = registry.Load(s.RegistryPath)
	if len(reg.Entries) != 2 {
		t.Fatal("entry retained", reg)
	}
	trash, _ := os.ReadDir(filepath.Join(s.Home, ".config", "macjobs", "trash"))
	if len(trash) != 1 {
		t.Fatal("recovery plist missing")
	}
}

func TestCreateRollback(t *testing.T) {
	for _, stage := range []string{"plutil", "enable", "bootstrap"} {
		t.Run(stage, func(t *testing.T) {
			s, _, f := setup(t)
			before, _ := os.ReadFile(s.RegistryPath)
			f.hook = func(name string, args []string) error {
				if name == stage || (name == "launchctl" && args[0] == stage) {
					return errors.New("private subprocess error")
				}
				return nil
			}
			r, err := s.Create(context.Background(), createOptions())
			if err == nil || r.Result != "failed" || strings.Contains(r.Error.Message, "private subprocess") {
				t.Fatal(r, err)
			}
			after, _ := os.ReadFile(s.RegistryPath)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("registry changed after rollback")
			}
			if _, err := os.Lstat(filepath.Join(s.Home, "Library", "LaunchAgents", r.Label+".plist")); !os.IsNotExist(err) {
				t.Fatal("failed creation retained plist")
			}
		})
	}
}

func TestCreateExistsRefusal(t *testing.T) {
	for _, where := range []string{"plist", "registry", "loaded"} {
		s, _, f := setup(t)
		label := "local.example.test"
		switch where {
		case "plist":
			if err := os.WriteFile(filepath.Join(s.Home, "Library", "LaunchAgents", label+".plist"), []byte("existing"), 0600); err != nil {
				t.Fatal(err)
			}
		case "registry":
			reg, _ := registry.Load(s.RegistryPath)
			reg.Entries = append(reg.Entries, registry.Entry{ID: label})
			if err := registry.Save(s.RegistryPath, reg, true); err != nil {
				t.Fatal(err)
			}
		case "loaded":
			f.hook = func(name string, args []string) error { return nil }
			s.Runner = domainRunner{f, label}
		}
		r, err := s.Create(context.Background(), createOptions())
		if err == nil || r.Error.Code != "already_exists" {
			t.Fatal(where, r, err)
		}
		for _, call := range f.calls {
			if call.Command != "launchctl" || call.Args[0] != "print" {
				t.Fatal("exists refusal mutated", call)
			}
		}
	}
}

type domainRunner struct {
	*fakeRunner
	label string
}

func (f domainRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	b, err := f.fakeRunner.Run(ctx, name, args...)
	if name == "launchctl" && args[0] == "print" && strings.Count(args[1], "/") == 1 {
		return []byte("gui/501 = {\n services = {\n 0 0 " + f.label + "\n }\n}\n"), nil
	}
	return b, err
}

func TestAdopt(t *testing.T) {
	for _, asCreated := range []bool{false, true} {
		s, i, f := setup(t)
		reg, _ := registry.Load(s.RegistryPath)
		reg.Entries[0].CreatedByTool = false
		reg.Entries[0].Managed = false
		if err := registry.Save(s.RegistryPath, reg, true); err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(s.RegistryPath)
		if _, err := s.Adopt(context.Background(), i, AdoptOptions{Owner: "team", DryRun: true, AsCreated: asCreated}); err != nil {
			t.Fatal(err)
		}
		after, _ := os.ReadFile(s.RegistryPath)
		if !reflect.DeepEqual(before, after) || len(f.calls) != 0 {
			t.Fatal("adoption dry-run changed state")
		}
		r, err := s.Adopt(context.Background(), i, AdoptOptions{Owner: "team", Purpose: "Example worker", AsCreated: asCreated})
		if err != nil || r.Result != "completed" {
			t.Fatal(r, err)
		}
		reg, _ = registry.Load(s.RegistryPath)
		entry := reg.Entries[0]
		if !entry.Managed || entry.CreatedByTool != asCreated || entry.AdoptedAt == "" || entry.Owner != "team" {
			t.Fatal(entry)
		}
		for _, protection := range []string{"apple", "system", "wrong-domain"} {
			blocked := i
			switch protection {
			case "apple":
				blocked.Label = "com.apple.example"
			case "system":
				blocked.Kind = "system"
			case "wrong-domain":
				blocked.Domain = "gui/502"
			}
			if _, err := s.Adopt(context.Background(), blocked, AdoptOptions{DryRun: true}); err == nil {
				t.Fatal("adopted protected job")
			}
		}
	}
}

func TestCreateUsesLabelPrefix(t *testing.T) {
	s, _, _ := setup(t)
	o := createOptions()
	o.DryRun = true
	r, err := s.Create(context.Background(), o)
	if err != nil || r.ID != "local.example.test" {
		t.Fatal(r, err)
	}
	reg, err := registry.Load(s.RegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	reg.LabelPrefix = ""
	if err := registry.Save(s.RegistryPath, reg, true); err != nil {
		t.Fatal(err)
	}
	r, err = s.Create(context.Background(), o)
	if err != nil || r.ID != "local.macjobs.test" {
		t.Fatal(r, err)
	}
}

func TestCreateOutputRedactsCommandSecrets(t *testing.T) {
	s, _, _ := setup(t)
	o := createOptions()
	o.DryRun = true
	o.Template.Command = []string{"/bin/echo", "--password", "private-value"}
	r, err := s.Create(context.Background(), o)
	if err != nil || strings.Contains(r.Plist, "private-value") || !strings.Contains(r.Plist, "[REDACTED]") {
		t.Fatal(r, err)
	}
}

func TestCreateAndAdoptTags(t *testing.T) {
	s, i, f := setup(t)
	reg, _ := registry.Load(s.RegistryPath)
	reg.Entries[0].Tags = []string{"existing"}
	reg.Entries[1].Tags = []string{"other"}
	if err := registry.Save(s.RegistryPath, reg, true); err != nil {
		t.Fatal(err)
	}
	o := createOptions()
	o.Tags = []string{"Z", "homelab", "z"}
	o.DryRun = true
	r, err := s.Create(context.Background(), o)
	if err != nil || !reflect.DeepEqual(r.Tags, []string{"homelab", "z"}) {
		t.Fatal(r, err)
	}
	o.DryRun = false
	if _, err = s.Create(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	reg, _ = registry.Load(s.RegistryPath)
	if !reflect.DeepEqual(reg.Entries[2].Tags, r.Tags) || !reflect.DeepEqual(reg.Entries[1].Tags, []string{"other"}) {
		t.Fatal(reg)
	}
	f.calls = nil
	preview, err := s.Adopt(context.Background(), i, AdoptOptions{Tags: []string{"HOMELAB"}, DryRun: true})
	if err != nil || !reflect.DeepEqual(preview.Tags, []string{"existing", "homelab"}) || len(f.calls) != 0 {
		t.Fatal(preview, err)
	}
	if _, err = s.Adopt(context.Background(), i, AdoptOptions{Tags: []string{"HOMELAB"}}); err != nil {
		t.Fatal(err)
	}
	reg, _ = registry.Load(s.RegistryPath)
	if !reflect.DeepEqual(reg.Entries[0].Tags, preview.Tags) {
		t.Fatal(reg)
	}
	before, _ := os.ReadFile(s.RegistryPath)
	o.Name = "invalid"
	o.Tags = []string{"bad/tag"}
	if _, err = s.Create(context.Background(), o); err == nil {
		t.Fatal("created invalid tags")
	}
	if _, err = s.Adopt(context.Background(), i, AdoptOptions{Tags: []string{"bad/tag"}}); err == nil {
		t.Fatal("adopted invalid tags")
	}
	after, _ := os.ReadFile(s.RegistryPath)
	if string(before) != string(after) || len(f.calls) != 0 {
		t.Fatal("invalid tags changed state")
	}
	if _, err := os.Lstat(filepath.Join(s.Home, "Library", "LaunchAgents", "local.example.invalid.plist")); !os.IsNotExist(err) {
		t.Fatal("invalid create wrote plist")
	}
}
