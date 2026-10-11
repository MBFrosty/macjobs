package main

import "testing"

func TestVersionString(t *testing.T) {
	tests := []struct {
		name    string
		version string
		commit  string
		date    string
		want    string
	}{
		{name: "version only", version: "0.1.0", want: "0.1.0"},
		{name: "dev default", version: "dev", want: "dev"},
		{name: "commit and date", version: "1.2.3", commit: "abcdef123456", date: "2026-10-11", want: "1.2.3 (commit abcdef1, 2026-10-11)"},
		{name: "commit only", version: "1.0.0", commit: "deadbeef", want: "1.0.0 (commit deadbee)"},
		{name: "date only", version: "2.0.0", date: "2026-01-01", want: "2.0.0 (2026-01-01)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := VersionString(tt.version, tt.commit, tt.date); got != tt.want {
				t.Fatalf("VersionString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveVersion(t *testing.T) {
	if got := resolveVersion("1.0.0"); got != "1.0.0" {
		t.Fatalf("resolveVersion(release) = %q, want 1.0.0", got)
	}
	if got := resolveVersion("dev"); got != "dev" {
		t.Fatalf("resolveVersion(dev) in test build = %q, want dev", got)
	}
}
