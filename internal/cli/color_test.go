package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MBFrosty/macjobs/internal/model"
)

func TestColorEnabled(t *testing.T) {
	cases := []struct {
		tty, flag bool
		env       string
		want      bool
	}{
		{true, false, "", true},
		{false, false, "", false},
		{true, true, "", false},
		{true, false, "1", false},
		{true, false, "0", false},
		{true, false, "true", false},
		{false, true, "1", false},
		{false, false, "1", false},
		{true, true, "1", false},
	}
	for _, tc := range cases {
		if got := ColorEnabled(tc.tty, tc.flag, tc.env); got != tc.want {
			t.Fatalf("ColorEnabled(tty=%v, flag=%v, env=%q)=%v, want %v", tc.tty, tc.flag, tc.env, got, tc.want)
		}
	}
}

func TestNoColorFlagKeepsJSONAndPlainTable(t *testing.T) {
	plain, err := commands(t, "list")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain, "\x1b") {
		t.Fatal("piped list used color")
	}
	if !strings.Contains(plain, "PID") || !strings.Contains(plain, "LAST RUN*") {
		t.Fatal(plain)
	}
	disabled, err := commands(t, "--no-color", "list")
	if err != nil {
		t.Fatal(err)
	}
	if disabled != plain {
		t.Fatal("piped --no-color changed table output")
	}
	s, err := commands(t, "--no-color", "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s, "\x1b") {
		t.Fatal(s)
	}
	var r model.Report
	if err := json.Unmarshal([]byte(s), &r); err != nil || r.SchemaVersion != 1 || len(r.Items) == 0 {
		t.Fatal(s, err)
	}
}

func TestHelpListsNoColor(t *testing.T) {
	s, err := commands(t, "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, "--no-color") {
		t.Fatal(s)
	}
}
