package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStableSelfPathCaskroomPrefersHomebrewLink(t *testing.T) {
	resolved, stable := linkedCaskroom(t)
	got, err := stableSelfPath(constPath(resolved), identityEval, remapStat(t, "/opt/homebrew/bin/macjobs", stable), "")
	if err != nil || got != "/opt/homebrew/bin/macjobs" {
		t.Fatal(got, err)
	}
}

func TestStableSelfPathCellarPrefersUsrLocalLink(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Cellar", "macjobs", "0.1.0", "bin")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	resolved := filepath.Join(dir, "macjobs")
	if err := os.WriteFile(resolved, []byte("bin"), 0700); err != nil {
		t.Fatal(err)
	}
	stableDir := filepath.Join(root, "usr", "local", "bin")
	if err := os.MkdirAll(stableDir, 0700); err != nil {
		t.Fatal(err)
	}
	stable := filepath.Join(stableDir, "macjobs")
	linkSame(t, resolved, stable)
	got, err := stableSelfPath(constPath(resolved), identityEval, remapStat(t, "/usr/local/bin/macjobs", stable), "")
	if err != nil || got != "/usr/local/bin/macjobs" {
		t.Fatal(got, err)
	}
}

func TestStableSelfPathPlainPathUnchanged(t *testing.T) {
	root := t.TempDir()
	resolved := filepath.Join(root, "bin", "macjobs")
	if err := os.MkdirAll(filepath.Dir(resolved), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resolved, []byte("bin"), 0700); err != nil {
		t.Fatal(err)
	}
	stableDir := filepath.Join(root, "homebrew")
	if err := os.MkdirAll(stableDir, 0700); err != nil {
		t.Fatal(err)
	}
	stable := filepath.Join(stableDir, "macjobs")
	linkSame(t, resolved, stable)
	got, err := stableSelfPath(constPath(resolved), identityEval, remapStat(t, "/opt/homebrew/bin/macjobs", stable), "")
	if err != nil || got != resolved {
		t.Fatal(got, err)
	}
}

func TestStableSelfPathResolvesSymlinkThenPrefersStable(t *testing.T) {
	resolved, stable := linkedCaskroom(t)
	link := filepath.Join(t.TempDir(), "mj")
	if err := os.Symlink(resolved, link); err != nil {
		t.Fatal(err)
	}
	got, err := stableSelfPath(constPath(link), filepath.EvalSymlinks, remapStat(t, "/opt/homebrew/bin/macjobs", stable), "")
	if err != nil || got != "/opt/homebrew/bin/macjobs" {
		t.Fatal(got, err)
	}
}

func TestStableSelfPathNoStableLinkFallsBack(t *testing.T) {
	resolved, _ := linkedCaskroom(t)
	otherDir := filepath.Join(t.TempDir(), "other")
	if err := os.MkdirAll(otherDir, 0700); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(otherDir, "macjobs")
	if err := os.WriteFile(other, []byte("other"), 0700); err != nil {
		t.Fatal(err)
	}
	got, err := stableSelfPath(constPath(resolved), identityEval, os.Stat, otherDir)
	if err != nil || got != resolved {
		t.Fatal(got, err)
	}
}

func TestStableSelfPathPATHCandidate(t *testing.T) {
	resolved, stable := linkedCaskroom(t)
	got, err := stableSelfPath(constPath(resolved), identityEval, os.Stat, filepath.Dir(stable))
	if err != nil || got != stable {
		t.Fatal(got, err)
	}
}

func TestStableSelfPathExecutableError(t *testing.T) {
	_, err := stableSelfPath(func() (string, error) { return "", errors.New("missing") }, identityEval, os.Stat, "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func linkedCaskroom(t *testing.T) (resolved, stable string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "Caskroom", "macjobs", "0.1.0")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	resolved = filepath.Join(dir, "macjobs")
	if err := os.WriteFile(resolved, []byte("bin"), 0700); err != nil {
		t.Fatal(err)
	}
	stableDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(stableDir, 0700); err != nil {
		t.Fatal(err)
	}
	stable = filepath.Join(stableDir, "macjobs")
	linkSame(t, resolved, stable)
	return resolved, stable
}

func linkSame(t *testing.T, target, name string) {
	t.Helper()
	if err := os.Link(target, name); err != nil {
		if err := os.Symlink(target, name); err != nil {
			t.Fatal(err)
		}
	}
}

func constPath(path string) func() (string, error) {
	return func() (string, error) { return path, nil }
}

func identityEval(path string) (string, error) { return path, nil }

func remapStat(t *testing.T, alias, real string) func(string) (os.FileInfo, error) {
	t.Helper()
	return func(path string) (os.FileInfo, error) {
		if path == alias {
			return os.Stat(real)
		}
		return os.Stat(path)
	}
}
