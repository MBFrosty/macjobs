package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicSaveAndNoOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "registry.yaml")
	r := Empty()
	r.Entries = append(r.Entries, Entry{ID: "com.example.worker", Owner: "team", Match: &Match{Name: "example"}})
	if e := Save(path, r, false); e != nil {
		t.Fatal(e)
	}
	s, e := os.Stat(path)
	if e != nil || s.Mode().Perm() != 0600 {
		t.Fatal("registry permissions")
	}
	got, e := Load(path)
	if e != nil || got.Entries[0].Owner != "team" {
		t.Fatal(e, got)
	}
	r.Entries[0].Owner = "other"
	if e = Save(path, r, false); e == nil {
		t.Fatal("overwrote registry")
	}
	got, _ = Load(path)
	if got.Entries[0].Owner != "team" {
		t.Fatal("existing registry changed")
	}
	if e = Save(path, r, true); e != nil {
		t.Fatal(e)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatal("temporary registry file leaked")
	}
}
func TestValidation(t *testing.T) {
	for _, s := range []string{"schemaVersion: 2\nentries: []\n", "schemaVersion: 1\nentries:\n - id: same\n - id: same\n", "schemaVersion: 1\nunknown: value\nentries: []\n", "schemaVersion: 1\nentries: []\n---\nextra: value\n", "schemaVersion: 1\nentries:\n - id: example\n   match:\n     regex: '['\n"} {
		path := filepath.Join(t.TempDir(), "registry.yaml")
		os.WriteFile(path, []byte(s), 0600)
		if _, e := Load(path); e == nil {
			t.Fatalf("accepted invalid registry: %s", s)
		}
	}
}
func TestPortsRoundTripAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.yaml")
	r := Empty()
	r.Entries = []Entry{{ID: "com.example.web", Owner: "team", Port: 8080, Ports: []int{8080, 9090, 9090}}}
	if e := Save(path, r, false); e != nil {
		t.Fatal(e)
	}
	if len(r.Entries[0].Ports) != 3 {
		t.Fatal("save mutated caller", r.Entries[0].Ports)
	}
	got, e := Load(path)
	if e != nil || got.Entries[0].Port != 8080 || len(got.Entries[0].Ports) != 2 || got.Entries[0].Ports[0] != 8080 || got.Entries[0].Ports[1] != 9090 {
		t.Fatal(got, e)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "ports:") {
		t.Fatal(string(b))
	}
	for _, ports := range [][]int{{0}, {-1}, {65536}} {
		r.Entries[0].Ports = ports
		if Validate(r) == nil {
			t.Fatal(ports)
		}
	}
	plain := filepath.Join(t.TempDir(), "plain.yaml")
	if e := os.WriteFile(plain, []byte("schemaVersion: 1\nentries:\n - id: com.example.web\n   owner: team\n   port: 8080\n"), 0600); e != nil {
		t.Fatal(e)
	}
	got, e = Load(plain)
	if e != nil || got.Entries[0].Port != 8080 || got.Entries[0].Ports != nil {
		t.Fatal("untagged ports changed", got, e)
	}
}

func TestMissingRegistry(t *testing.T) {
	r, e := Load(filepath.Join(t.TempDir(), "missing"))
	if e != nil || r.SchemaVersion != 1 || len(r.Entries) != 0 {
		t.Fatal(r, e)
	}
}

func TestManagedLabelPrefix(t *testing.T) {
	r := Empty()
	if r.ManagedLabelPrefix() != "local.macjobs." {
		t.Fatal(r)
	}
	r.LabelPrefix = "local.example.jobs."
	path := filepath.Join(t.TempDir(), "registry.yaml")
	if err := Save(path, r, false); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got.ManagedLabelPrefix() != r.LabelPrefix {
		t.Fatal(got, err)
	}
	for _, prefix := range []string{"no-dot", "../example.", "space example.", "-example."} {
		r.LabelPrefix = prefix
		if Validate(r) == nil {
			t.Fatal(prefix)
		}
	}
}
