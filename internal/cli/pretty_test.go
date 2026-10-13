package cli

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/redact"
	"github.com/charmbracelet/lipgloss"
)

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func exampleReport() model.Report {
	r := model.NewReport()
	r.Items = []model.Item{
		{ID: "com.example.worker", Kind: "launchd", Owner: "team", Tags: []string{"homelab"}, State: "running", Health: "warn", Purpose: "Example worker", Ports: []model.PortBinding{{Proto: "tcp", Port: 8080, Source: model.PortObserved, Listening: true}}, Notes: []string{"log is stale"}},
		{ID: "homebrew.mxcl.postgresql@16", Kind: "brew", Owner: "data", Tags: []string{"db"}, State: "running", Health: "ok", Purpose: "Local database", Ports: []model.PortBinding{{Proto: "tcp", Port: 5432, Source: model.PortObserved, Listening: true}}},
		{ID: "tailscale:https:443", Kind: "tailscale", Owner: "ops", Tags: []string{"docs"}, State: "serving", Health: "ok", Purpose: "Docs preview", Ports: []model.PortBinding{{Proto: "tcp", Port: 443, Source: model.PortTailscale, Listening: true}}},
		{ID: "cron:user:9f3a1c20", Kind: "cron", Owner: "ops", Tags: []string{"nightly"}, State: "scheduled", Health: "ok", Purpose: "Nightly backup"},
		{ID: "com.example.sync", Kind: "launchd", Owner: "team", Tags: []string{"sync"}, State: "running", Health: "ok", Purpose: "Photo sync"},
		{ID: "example-process", Kind: "process", Owner: "dev", Tags: []string{"dev"}, State: "running", Health: "warn", Purpose: "Dev server", Ports: []model.PortBinding{{Proto: "tcp", Port: 8080, Source: model.PortDeclared}}, Notes: []string{"registered port is not listening"}},
		{ID: "com.example.backup", Kind: "launchd", Owner: "ops", Tags: []string{"nightly"}, State: "stopped", Health: "fail", Purpose: "Offsite backup", Notes: []string{"registry entry has no live job"}},
		{ID: "homebrew.mxcl.redis", Kind: "brew", Owner: "data", Tags: []string{"cache"}, State: "running", Health: "ok", Purpose: "Cache", Ports: []model.PortBinding{{Proto: "tcp", Port: 6379, Source: model.PortObserved, Listening: true}}},
	}
	r.Warnings = []string{"example collector warning"}
	return r
}

func TestPrettyListColorAndLayout(t *testing.T) {
	r := exampleReport()
	colored := &bytes.Buffer{}
	if err := writePretty(colored, r, redact.Redactor{}, true, false); err != nil {
		t.Fatal(err)
	}
	got := colored.String()
	if !strings.Contains(got, "\x1b") {
		t.Fatal("expected ANSI color")
	}
	plain := ansiEscape.ReplaceAllString(got, "")
	if !strings.Contains(plain, "● com.example.worker") || !strings.Contains(plain, "(homelab)") {
		t.Fatal(plain)
	}
	if !strings.Contains(plain, "● warn") || !strings.Contains(plain, "● ok") || !strings.Contains(plain, "● fail") {
		t.Fatal(plain)
	}
	header := strings.Fields(strings.Split(plain, "\n")[0])
	if strings.Join(header, " ") != "ID KIND OWNER TAGS STATE PORTS HEALTH PURPOSE" {
		t.Fatal(header)
	}
	if strings.Contains(plain, "LAST RUN") || strings.Contains(plain, "PID") {
		t.Fatal("pretty list kept the piped columns")
	}
	if strings.Contains(plain, "log is stale") {
		t.Fatal("list pretty included doctor findings")
	}
	if !strings.Contains(plain, "Warning: example collector warning") {
		t.Fatal(plain)
	}
	for _, kind := range []string{"launchd", "brew", "tailscale", "cron", "process"} {
		if !strings.Contains(plain, kind) {
			t.Fatal("missing kind", kind, plain)
		}
	}
	if lipgloss.Width(strings.Split(got, "\n")[1]) < lipgloss.Width("● com.example.worker") {
		t.Fatal("lipgloss.Width did not measure the colored row")
	}
}

func TestPrettyNoColorOmitsANSI(t *testing.T) {
	out := &bytes.Buffer{}
	if err := writePretty(out, exampleReport(), redact.Redactor{}, false, false); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if strings.Contains(got, "\x1b") {
		t.Fatal(got)
	}
	if !strings.Contains(got, "● com.example.worker") || !strings.Contains(got, "(homelab)") || !strings.Contains(got, "● ok") {
		t.Fatal(got)
	}
}

func TestPrettyDoctorIncludesFindings(t *testing.T) {
	out := &bytes.Buffer{}
	if err := writePretty(out, exampleReport(), redact.Redactor{}, true, true); err != nil {
		t.Fatal(err)
	}
	plain := ansiEscape.ReplaceAllString(out.String(), "")
	if !strings.Contains(plain, "registry entry has no live job") || !strings.Contains(plain, "registered port is not listening") {
		t.Fatal(plain)
	}
	if !strings.Contains(plain, "!8080") {
		t.Fatal(plain)
	}
}

func TestPrettyShowHeader(t *testing.T) {
	item := exampleReport().Items[0]
	colored := &bytes.Buffer{}
	if err := writeShowHeader(colored, item, redact.Redactor{}, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(colored.String(), "\x1b") {
		t.Fatal(colored.String())
	}
	plainOut := &bytes.Buffer{}
	if err := writeShowHeader(plainOut, item, redact.Redactor{}, false); err != nil {
		t.Fatal(err)
	}
	got := plainOut.String()
	if strings.Contains(got, "\x1b") {
		t.Fatal(got)
	}
	if !strings.Contains(got, "● com.example.worker") || !strings.Contains(got, "launchd") || !strings.Contains(got, "● warn") {
		t.Fatal(got)
	}
}

func TestJSONSchemaUnchanged(t *testing.T) {
	s, err := commands(t, "list", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s, "\x1b") || strings.Contains(s, "●") {
		t.Fatal(s)
	}
	var r model.Report
	if err := json.Unmarshal([]byte(s), &r); err != nil || r.SchemaVersion != 1 {
		t.Fatal(s, err)
	}
	s, err = commands(t, "doctor", "--json")
	if err == nil {
		t.Fatal("doctor should fail on fixture")
	}
	if strings.Contains(s, "\x1b") {
		t.Fatal(s)
	}
	if err := json.Unmarshal([]byte(s), &r); err != nil || r.SchemaVersion != 1 {
		t.Fatal(s, err)
	}
}
