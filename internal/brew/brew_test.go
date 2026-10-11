package brew

import (
	"os"
	"testing"
)

func TestCapturedJSON(t *testing.T) {
	b, e := os.ReadFile("../../testdata/brew.json")
	if e != nil {
		t.Fatal(e)
	}
	s, e := Parse(b)
	if e != nil || len(s) != 2 || s[0].Status != "none" {
		t.Fatalf("parse: %v %+v", e, s)
	}
	s, e = Parse([]byte(`[{"name":"example","status":"error","exit_code":2,"new_field":true}]`))
	if e != nil || s[0].ExitCode == nil || *s[0].ExitCode != 2 {
		t.Fatal("exit code missing")
	}
	if _, e = Parse([]byte(`invalid`)); e == nil {
		t.Fatal("accepted invalid JSON")
	}
}
