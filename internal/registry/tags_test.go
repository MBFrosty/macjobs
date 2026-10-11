package registry

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeTags(t *testing.T) {
	input := []string{"Z", "homelab", "z", "a.b_c-1", strings.Repeat("a", 32)}
	got, err := NormalizeTags(input)
	if err != nil || !reflect.DeepEqual(got, []string{"a.b_c-1", strings.Repeat("a", 32), "homelab", "z"}) || input[0] != "Z" {
		t.Fatal(got, err, input)
	}
	for _, tag := range []string{"", " a", "a ", "-a", "a/b", "a,b", "é", strings.Repeat("a", 33), "token=private"} {
		if _, err := NormalizeTags([]string{tag}); err == nil || strings.Contains(err.Error(), tag) && tag == "token=private" {
			t.Fatal("accepted or exposed invalid tag")
		}
	}
}

func TestTagsRegistryRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.yaml")
	reg := Empty()
	reg.Entries = []Entry{{ID: "com.example.worker", Tags: []string{"z", "a", "z"}}, {ID: "com.example.other"}}
	if err := Save(path, reg, false); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || !reflect.DeepEqual(got.Entries[0].Tags, []string{"a", "z"}) || len(got.Entries[1].Tags) != 0 {
		t.Fatal(got, err)
	}
	if !reflect.DeepEqual(reg.Entries[0].Tags, []string{"z", "a", "z"}) {
		t.Fatal("save mutated caller")
	}
	before, _ := os.ReadFile(path)
	for _, tag := range []string{"UPPER", "", "-bad", strings.Repeat("a", 33)} {
		reg.Entries[0].Tags = []string{tag}
		if Validate(reg) == nil || Save(path, reg, true) == nil {
			t.Fatal("accepted invalid registry tag")
		}
		after, _ := os.ReadFile(path)
		if string(before) != string(after) {
			t.Fatal("invalid save changed file")
		}
		badPath := filepath.Join(t.TempDir(), "bad.yaml")
		if err := os.WriteFile(badPath, []byte("schemaVersion: 1\nentries:\n - id: com.example.worker\n   tags: ['"+tag+"']\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(badPath); err == nil {
			t.Fatal("loaded invalid tags")
		}
	}
}

func TestUntaggedRegistryKeepsEmptyEntriesAndOmission(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.yaml")
	if err := Save(path, Empty(), false); err != nil {
		t.Fatal(err)
	}
	r, err := Load(path)
	if err != nil || r.Entries == nil {
		t.Fatal("empty entries contract changed", r, err)
	}
	r.Entries = append(r.Entries, Entry{ID: "com.example.worker"})
	if err := Save(path, r, true); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "tags:") {
		t.Fatal("empty tags serialized", string(b))
	}
	r, err = Load(path)
	if err != nil || r.Entries[0].Tags != nil {
		t.Fatal("untagged entry changed", r, err)
	}
}
