package main

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// VersionString formats the version line passed to cobra (Name + "version " + Version).
func VersionString(version, commit, date string) string {
	v := strings.TrimSpace(version)
	if v == "" {
		v = "dev"
	}
	commit = strings.TrimSpace(commit)
	date = strings.TrimSpace(date)
	if commit == "" && date == "" {
		return v
	}
	var extras []string
	if commit != "" {
		extras = append(extras, "commit "+shortCommit(commit))
	}
	if date != "" {
		extras = append(extras, date)
	}
	return fmt.Sprintf("%s (%s)", v, strings.Join(extras, ", "))
}

func shortCommit(commit string) string {
	if len(commit) <= 7 {
		return commit
	}
	return commit[:7]
}

func resolveVersion(version string) string {
	if version != "dev" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return version
	}
	return info.Main.Version
}
