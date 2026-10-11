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

	"github.com/MBFrosty/macjobs/internal/actions"
	"github.com/MBFrosty/macjobs/internal/inventory"
	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/redact"
	"github.com/MBFrosty/macjobs/internal/registry"
	"github.com/spf13/cobra"
)

func taggedReport() model.Report {
	r := model.NewReport()
	r.Items = []model.Item{
		{ID: "com.example.a", Kind: "launchd", Owner: "team", Health: "warn", Tags: []string{"a", "homelab"}, Notes: []string{}},
		{ID: "com.example.b", Kind: "brew", Owner: "team", Health: "fail", Tags: []string{"b"}, Notes: []string{}},
		{ID: "com.example.c", Kind: "launchd", Owner: "other", Health: "unknown", Tags: []string{"b"}, Notes: []string{}},
		{ID: "com.example.d", Kind: "launchd", Owner: "team", Health: "ok", Tags: []string{"b"}, Notes: []string{}},
		{ID: "com.example.empty", Kind: "process", Owner: "unknown", Health: "unknown", Notes: []string{}},
	}
	return r
}

func TestFilterTagsAndTable(t *testing.T) {
	r := taggedReport()
	got := Filter(r, "launchd", "team", true, []string{"a", "b"}, nil)
	if len(got.Items) != 1 || got.Items[0].ID != "com.example.a" {
		t.Fatal(got)
	}
	got = Filter(r, "", "", false, []string{"a", "b"}, nil)
	if len(got.Items) != 4 {
		t.Fatal(got)
	}
	if len(Filter(r, "", "", false, []string{"missing"}, nil).Items) != 0 || len(Filter(r, "", "", false, nil, nil).Items) != 5 {
		t.Fatal("tag filtering")
	}
	out := &bytes.Buffer{}
	if err := Table(out, r, redact.Redactor{}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out.String(), "\n")
	header := strings.Fields(lines[0])
	if !reflect.DeepEqual(header[:5], []string{"ID", "KIND", "OWNER", "TAGS", "STATE"}) || strings.Fields(lines[1])[3] != "a,homelab" || strings.Fields(lines[5])[3] != "-" {
		t.Fatal(out.String())
	}
	b, err := json.Marshal(r)
	if err != nil || !strings.Contains(string(b), `"tags":["a","homelab"]`) {
		t.Fatal(string(b), err)
	}
	var raw struct {
		SchemaVersion int
		Items         []map[string]any
	}
	if err := json.Unmarshal(b, &raw); err != nil || raw.SchemaVersion != 1 {
		t.Fatal(err)
	}
	if _, ok := raw.Items[4]["tags"]; ok {
		t.Fatal("empty tags not omitted")
	}
}

func TestListTagFlagForms(t *testing.T) {
	for _, flags := range [][]string{{"--tag", "homelab"}, {"--tag", "a,b"}, {"--tag", "a", "--tag", "b"}, {"--tag", "A,b"}, {"--tag", "missing"}, {"--tag", "bad/tag"}} {
		calls := 0
		collect := func(context.Context, registry.Registry, bool) (inventory.Result, error) {
			calls++
			return inventory.Result{Report: taggedReport()}, nil
		}
		cmd := New("test", collect)
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetArgs(append([]string{"--registry", filepath.Join(t.TempDir(), "missing"), "list", "--json"}, flags...))
		err := cmd.Execute()
		if flags[1] == "bad/tag" {
			if err == nil || calls != 0 {
				t.Fatal("invalid filter collected", err, calls)
			}
			continue
		}
		var got model.Report
		if err != nil || json.Unmarshal(out.Bytes(), &got) != nil {
			t.Fatal(out.String(), err)
		}
		want := 4
		if flags[1] == "homelab" {
			want = 1
		}
		if flags[1] == "missing" {
			want = 0
		}
		if len(got.Items) != want {
			t.Fatal(flags, out.String())
		}
	}
}

func TestTagCommandsFixtures(t *testing.T) {
	for _, kind := range []string{"launchd", "system", "brew", "tailscale", "process"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "registry.yaml")
			collect := func(_ context.Context, reg registry.Registry, _ bool) (inventory.Result, error) {
				r := model.NewReport()
				i := model.Item{ID: "com.example.worker", Kind: kind}
				for _, e := range reg.Entries {
					if e.ID == i.ID {
						i.Tags = e.Tags
					}
				}
				r.Items = append(r.Items, i)
				return inventory.Result{Report: r}, nil
			}
			factory := func(_ *cobra.Command, p string) (actions.Service, error) {
				return actions.Service{RegistryPath: p, Runner: forbiddenRunner{t}}, nil
			}
			run := func(args ...string) (actions.Report, error) {
				cmd := NewWithActions("test", collect, factory)
				out := &bytes.Buffer{}
				cmd.SetOut(out)
				cmd.SetArgs(append([]string{"--registry", path, "--json"}, args...))
				err := cmd.Execute()
				var r actions.Report
				if e := json.Unmarshal(out.Bytes(), &r); e != nil {
					t.Fatal(out.String(), e)
				}
				return r, err
			}
			r, err := run("tag", "com.example.worker", "Z", "homelab", "--dry-run")
			if err != nil || r.SchemaVersion != 1 || r.Result != "planned" || !reflect.DeepEqual(r.Tags, []string{"homelab", "z"}) {
				t.Fatal(r, err)
			}
			files, _ := os.ReadDir(dir)
			if len(files) != 0 {
				t.Fatal("dry-run wrote files")
			}
			if _, err = run("tag", "com.example.worker", "Z", "homelab"); err != nil {
				t.Fatal(err)
			}
			r, err = run("untag", "com.example.worker", "z")
			if err != nil || !reflect.DeepEqual(r.Tags, []string{"homelab"}) {
				t.Fatal(r, err)
			}
			r, err = run("untag", "com.example.worker", "missing")
			if err != nil || r.Result != "unchanged" {
				t.Fatal(r, err)
			}
			r, err = run("tag", "missing", "homelab")
			if !errors.Is(err, ErrActionReported) || r.Error.Code != "not_found" {
				t.Fatal(r, err)
			}
		})
	}
}

func TestTagCLIInvalidInputAndRedaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.yaml")
	collect := func(context.Context, registry.Registry, bool) (inventory.Result, error) {
		t.Fatal("invalid arguments collected")
		return inventory.Result{}, nil
	}
	for _, args := range [][]string{{"tag"}, {"untag", "com.example.worker"}, {"tag", "com.example.worker", "password=private"}} {
		cmd := New("test", collect)
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetArgs(append([]string{"--json", "--registry", path}, args...))
		if !errors.Is(cmd.Execute(), ErrActionReported) || strings.Contains(out.String(), "password=private") {
			t.Fatal(out.String())
		}
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("invalid CLI wrote registry")
	}
}
