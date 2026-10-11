package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func resolveSelfPath() (string, error) {
	return stableSelfPath(os.Executable, filepath.EvalSymlinks, os.Stat, os.Getenv("PATH"))
}

func stableSelfPath(executable func() (string, error), evalSymlinks func(string) (string, error), stat func(string) (os.FileInfo, error), pathEnv string) (string, error) {
	raw, err := executable()
	if err != nil {
		return "", errors.New("cannot resolve installed executable")
	}
	resolved, err := evalSymlinks(raw)
	if err != nil {
		return "", errors.New("cannot resolve executable symlink")
	}
	if !strings.Contains(resolved, "/Caskroom/") && !strings.Contains(resolved, "/Cellar/") {
		return resolved, nil
	}
	info, err := stat(resolved)
	if err != nil {
		return resolved, nil
	}
	candidates := []string{"/opt/homebrew/bin/macjobs", "/usr/local/bin/macjobs"}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		candidates = append(candidates, filepath.Join(dir, "macjobs"))
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		other, err := stat(candidate)
		if err != nil {
			continue
		}
		if os.SameFile(info, other) {
			return candidate, nil
		}
	}
	return resolved, nil
}
