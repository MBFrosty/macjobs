package launchd

import (
	"os"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, e := os.ReadFile("../../testdata/" + name)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestPrintFixtures(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		pid         int
		signal      bool
		exit        bool
	}{{"running", "running", 1234, false, false}, {"scheduled", "not running", 0, false, true}, {"signal", "running", 1234, true, false}, {"never-exited", "not running", 0, false, false}} {
		t.Run(tc.name, func(t *testing.T) {
			s := ParsePrint(fixture(t, "launchctl-"+tc.name+".txt"))
			if s.State != tc.state || s.PID != tc.pid || (s.Signal != "") != tc.signal || (s.LastExit != nil) != tc.exit {
				t.Fatalf("unexpected state: %+v", s)
			}
		})
	}
}
func TestTopLevelOnly(t *testing.T) {
	s := ParsePrint([]byte("job = {\n\tstate = waiting\n\tenvironment = {\n\t\tstate = running\n\t\tpid = 999\n\t}\n\tunknown = value = another\n\tlast exit code = 7\n}\n"))
	if s.State != "waiting" || s.PID != 0 || s.LastExit == nil || *s.LastExit != 7 {
		t.Fatalf("nested fields affected state: %+v", s)
	}
}
func TestListAndDomain(t *testing.T) {
	for _, rows := range [][]Row{ParseList(fixture(t, "launchctl-list.txt")), ParseDomain(fixture(t, "launchctl-domain.txt"))} {
		if len(rows) != 4 {
			t.Fatalf("got %d rows", len(rows))
		}
		for _, r := range rows {
			if r.Label == "" {
				t.Fatal("empty label")
			}
		}
	}
	s := ParseList([]byte("PID Status Label\n- -9 com.example.killed\n"))
	if len(s) != 1 || *s[0].LastExit != -9 {
		t.Fatal(s)
	}
}
func TestDisabled(t *testing.T) {
	d := ParseDisabled(fixture(t, "launchctl-disabled.txt"))
	if !d["com.example.disabled"] || d["com.example.worker"] {
		t.Fatal(d)
	}
}
