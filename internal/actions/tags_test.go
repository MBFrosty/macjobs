package actions

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/registry"
)

func TestTagsAllKindsAndIdempotence(t *testing.T) {
	for _, i := range []model.Item{
		{ID: "com.example.worker", Label: "com.example.worker", Kind: "launchd"},
		{ID: "system:com.example.worker", Label: "com.example.worker", Kind: "system"},
		{ID: "homebrew.mxcl.example", Kind: "brew"},
		{ID: "tailscale:example.invalid:443/", Kind: "tailscale"},
		{ID: "example-process", Kind: "process"},
	} {
		t.Run(i.Kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "registry.yaml")
			s := Service{RegistryPath: path}
			r, err := s.Tag(context.Background(), i, []string{"Z", "homelab", "z"}, false)
			if err != nil || r.Result != "completed" || len(r.Ops) != 1 || r.Ops[0].Command != "registry-tag" {
				t.Fatal(r, err)
			}
			reg, err := registry.Load(path)
			if err != nil || len(reg.Entries) != 1 {
				t.Fatal(reg, err)
			}
			e := reg.Entries[0]
			if e.Kind != i.Kind || e.Owner != "unknown" || e.Managed || e.CreatedByTool || !reflect.DeepEqual(e.Tags, []string{"homelab", "z"}) {
				t.Fatal(e)
			}
			before, _ := os.ReadFile(path)
			r, err = s.Tag(context.Background(), i, []string{"homelab"}, false)
			after, _ := os.ReadFile(path)
			if err != nil || r.Result != "unchanged" || len(r.Ops) != 0 || string(before) != string(after) {
				t.Fatal(r, err)
			}
			r, err = s.Untag(context.Background(), i, []string{"missing"}, false)
			after, _ = os.ReadFile(path)
			if err != nil || r.Result != "unchanged" || string(before) != string(after) {
				t.Fatal(r, err)
			}
			r, err = s.Untag(context.Background(), i, []string{"Z"}, false)
			if err != nil || r.Ops[0].Command != "registry-untag" {
				t.Fatal(r, err)
			}
			reg, _ = registry.Load(path)
			if !reflect.DeepEqual(reg.Entries[0].Tags, []string{"homelab"}) {
				t.Fatal(reg)
			}
			if _, err := s.Untag(context.Background(), i, []string{"homelab"}, false); err != nil {
				t.Fatal(err)
			}
			reg, _ = registry.Load(path)
			if len(reg.Entries) != 1 || len(reg.Entries[0].Tags) != 0 {
				t.Fatal(reg)
			}
		})
	}
}

func TestTagsPreserveMetadataAndSafety(t *testing.T) {
	s, i, f := setup(t)
	reg, _ := registry.Load(s.RegistryPath)
	reg.Entries[0].Purpose = "Example worker"
	reg.Entries[0].Match = &registry.Match{Name: "example"}
	if err := registry.Save(s.RegistryPath, reg, true); err != nil {
		t.Fatal(err)
	}
	original := reg.Entries[0]
	original.Tags = nil
	if _, err := s.Tag(context.Background(), i, []string{"homelab"}, false); err != nil {
		t.Fatal(err)
	}
	got, _ := registry.Load(s.RegistryPath)
	e := got.Entries[0]
	e.Tags = nil
	if !reflect.DeepEqual(e, original) || !reflect.DeepEqual(got.Entries[1], reg.Entries[1]) || len(f.calls) != 0 {
		t.Fatal("metadata or service changed", got, f.calls)
	}
	// Metadata registration never grants a foreign job lifecycle authority.
	foreign := model.Item{ID: "com.example.foreign", Label: "com.example.foreign", Kind: "launchd"}
	if _, err := s.Tag(context.Background(), foreign, []string{"homelab"}, false); err != nil {
		t.Fatal(err)
	}
	got, _ = registry.Load(s.RegistryPath)
	e = got.Entries[2]
	foreign.Registered, foreign.Managed, foreign.CreatedByTool = true, e.Managed, e.CreatedByTool
	r, err := s.Execute(context.Background(), "stop", foreign, Options{})
	if err == nil || r.Error.Code != "foreign_confirmation" || len(f.calls) != 0 {
		t.Fatal(r, err, f.calls)
	}
}

func TestTagsDryRunInvalidAndLock(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "tag", true: "untag"}[remove], func(t *testing.T) {
			dir := t.TempDir()
			s := Service{RegistryPath: filepath.Join(dir, "nested", "registry.yaml")}
			i := model.Item{ID: "example-process", Kind: "process"}
			run := s.Tag
			if remove {
				run = s.Untag
			}
			r, err := run(context.Background(), i, []string{"homelab"}, true)
			if err != nil || (r.Result != "planned" && r.Result != "unchanged") {
				t.Fatal(r, err)
			}
			files, _ := os.ReadDir(dir)
			if len(files) != 0 {
				t.Fatal("dry-run wrote files")
			}
			if _, err := run(context.Background(), i, []string{"bad/tag"}, false); err == nil {
				t.Fatal("invalid tag accepted")
			}
			files, _ = os.ReadDir(dir)
			if len(files) != 0 {
				t.Fatal("invalid tag wrote files")
			}
			for _, apple := range []model.Item{{ID: "com.apple.example", Kind: "launchd"}, {ID: "system:com.apple.example", Kind: "system"}, {ID: "other", Label: "com.apple.example", Kind: "launchd"}} {
				if _, err := run(context.Background(), apple, []string{"homelab"}, false); err == nil {
					t.Fatal("Apple tag accepted")
				}
			}
			unlock, err := registry.Lock(s.RegistryPath)
			if err != nil {
				t.Fatal(err)
			}
			r, err = run(context.Background(), i, []string{"homelab"}, false)
			unlock()
			if err == nil || r.Error.Code != "registry_busy" {
				t.Fatal(r, err)
			}
			if _, err := os.Lstat(s.RegistryPath); !os.IsNotExist(err) {
				t.Fatal("busy operation wrote registry")
			}
		})
	}
}

func TestUntagMissingDoesNotCreateEntry(t *testing.T) {
	s := Service{RegistryPath: filepath.Join(t.TempDir(), "registry.yaml")}
	r, err := s.Untag(context.Background(), model.Item{ID: "example-process", Kind: "process"}, []string{"homelab"}, false)
	if err != nil || r.Result != "unchanged" {
		t.Fatal(r, err)
	}
	if _, err := os.Lstat(s.RegistryPath); !os.IsNotExist(err) {
		t.Fatal("no-op created registry")
	}
}

func TestTagsExistingDryRunPreservesRegistry(t *testing.T) {
	s, i, f := setup(t)
	if _, err := s.Tag(context.Background(), i, []string{"existing"}, false); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(s.RegistryPath)
	beforeFiles, _ := os.ReadDir(filepath.Dir(s.RegistryPath))
	r, err := s.Tag(context.Background(), i, []string{"homelab"}, true)
	if err != nil || !reflect.DeepEqual(r.Tags, []string{"existing", "homelab"}) {
		t.Fatal(r, err)
	}
	r, err = s.Untag(context.Background(), i, []string{"existing"}, true)
	if err != nil || r.Result != "planned" || len(r.Tags) != 0 {
		t.Fatal(r, err)
	}
	after, _ := os.ReadFile(s.RegistryPath)
	afterFiles, _ := os.ReadDir(filepath.Dir(s.RegistryPath))
	if string(before) != string(after) || len(beforeFiles) != len(afterFiles) || len(f.calls) != 0 {
		t.Fatal("preview changed registry or services")
	}
}
