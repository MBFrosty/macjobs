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
