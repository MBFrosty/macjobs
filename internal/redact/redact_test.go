package redact

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCredentialsAndKnownValues(t *testing.T) {
	r := Redactor{Values: []string{"private-value"}}
	for _, input := range []string{`Authorization: Bearer hidden-value`, `token=hidden-value`, `password: "hidden-value"`, `https://user:hidden-value@example.org/`, `--api-key hidden-value`, `cookie: session=hidden-value`, `private-value`, `Bearer hidden-value`} {
		s := r.Text(input)
		if strings.Contains(s, "hidden-value") || strings.Contains(s, "private-value") {
			t.Fatalf("redaction missed credential in %q", input)
		}
	}
}
func TestPlistRedaction(t *testing.T) {
	p := map[string]any{"EnvironmentVariables": map[string]any{"MODE": "example", "API_TOKEN": "hidden-value"}, "ProgramArguments": []any{"example", "--token", "hidden-value"}}
	for _, show := range []bool{false, true} {
		b, e := json.Marshal((Redactor{}).Value(p, show))
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(string(b), "hidden-value") {
			t.Fatal("credential exposed")
		}
		if strings.Contains(string(b), "example\"}") && !show {
			t.Fatal("environment exposed")
		}
		env := (Redactor{}).Value(p, show).(map[string]any)["EnvironmentVariables"].(map[string]any)
		if !show && env["MODE"] != Hidden {
			t.Fatal(env)
		}
		if show && env["MODE"] != "example" {
			t.Fatal(env)
		}
	}
}

func TestEnvironmentValueScrubbing(t *testing.T) {
	plist := map[string]any{"EnvironmentVariables": map[string]any{
		"HOME": "/Users/snowman", "PORT": "48", "N": "3", "WORKERS": "0", "DOCKER_PID": "4843",
		"API_TOKEN": "s3cr3t-token-value", "DB_PASSWORD": "hunter2-long", "SHORT_KEY": "abc",
		"SESSION_ID": "sess-12345678",
	}}
	r := Redactor{Values: AllEnvironmentValues(plist)}
	cases := []struct{ name, in, want string }{
		{"docker label", "application.com.docker.docker.48434731.48435328", "application.com.docker.docker.48434731.48435328"},
		{"timestamp", "2026-10-11T20:40:05-07:00 START weekly cleanup", "2026-10-11T20:40:05-07:00 START weekly cleanup"},
		{"home path", "/Users/snowman/Library/Logs/x.log", "/Users/snowman/Library/Logs/x.log"},
		{"system word", "system daemons", "system daemons"},
		{"short sensitive value kept", "abc and abcdef", "abc and abcdef"},
		{"token value", "auth s3cr3t-token-value done", "auth " + Hidden + " done"},
		{"password value in path", "/tmp/hunter2-long/x", "/tmp/" + Hidden + "/x"},
		{"session value", "id=sess-12345678", "id=" + Hidden},
		{"embedded substring kept", "xs3cr3t-token-valuey", "xs3cr3t-token-valuey"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := r.Text(c.in); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestAllEnvironmentValuesFilters(t *testing.T) {
	got := AllEnvironmentValues(map[string]any{"EnvironmentVariables": map[string]any{"HOME": "/Users/snowman", "GH_TOKEN": "abcdefghij", "AUTH": "short"}})
	if len(got) != 1 || got[0] != "abcdefghij" {
		t.Fatalf("got %v", got)
	}
}
