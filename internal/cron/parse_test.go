package cron

import (
	"reflect"
	"strings"
	"testing"
)

func TestSchedules(t *testing.T) {
	for _, schedule := range []string{"* * * * *", "*/15 0-23/2 1,15 jan,DEC sun-sat", "0,30 2 1-31/3 1-12 0,7", "5/10 23 31 12 7", "@reboot", "@yearly", "@annually", "@monthly", "@weekly", "@daily", "@midnight", "@hourly"} {
		for _, source := range []string{"user", "system"} {
			t.Run(schedule+source, func(t *testing.T) {
				user := ""
				if source == "system" {
					user = "root "
				}
				r := Parse([]byte(schedule+" "+user+"/bin/echo  hello"), source)
				if len(r.Entries) != 1 {
					t.Fatal(r)
				}
				e := r.Entries[0]
				if e.Malformed || e.Schedule != schedule || e.Command != "/bin/echo  hello" || e.Program != "/bin/echo" || source == "system" && e.RunAs != "root" {
					t.Fatal(e)
				}
			})
		}
	}
}
func TestMalformed(t *testing.T) {
	for _, line := range []string{"* * * *", "60 * * * * /bin/echo", "* 24 * * * echo", "* * 0 * * echo", "* * * 13 * echo", "* * * * 8 echo", "* * * * xyz echo", "@sometimes echo", "@daily", "*/0 * * * * echo", "*/-1 * * * * echo", "1-0 * * * * echo", "*,,1 * * * * echo", "*/x * * * * echo", "1-2-3 * * * * echo", "* * * * *", "* * * janx * echo"} {
		t.Run(line, func(t *testing.T) {
			r := Parse([]byte("# comment\n\n"+line), "user")
			if len(r.Entries) != 1 {
				t.Fatal(r)
			}
			e := r.Entries[0]
			if !e.Malformed || e.ID != "cron:user:invalid-L3" || e.Command != "" || e.Schedule != "" {
				t.Fatal(e)
			}
		})
	}
	for _, line := range []string{"@daily /bin/echo", "* * * * * -root /bin/echo", "* * * * * root"} {
		if e := Parse([]byte(line), "system").Entries[0]; !e.Malformed {
			t.Fatal(e)
		}
	}
}
func TestEnvironmentAndShellWords(t *testing.T) {
	r := Parse([]byte("# comment\r\n\r\nSHELL = /bin/sh\r\nMODE='private value'\r\n@daily A=short B=\"two words\" '/Users/alice/bin/task name'  hello%input\r\nMODE=changed\r\n@hourly /bin/echo $MODE\r\n"), "user")
	if len(r.Entries) != 2 {
		t.Fatal(r)
	}
	e := r.Entries[0]
	if e.Line != 5 || e.Program != "/Users/alice/bin/task name" || !e.Percent || e.Env["MODE"] != "private value" || e.Env["A"] != "short" || e.Env["B"] != "two words" {
		t.Fatal(e)
	}
	if r.Entries[1].Env["MODE"] != "changed" {
		t.Fatal(r)
	}
	if !reflect.DeepEqual(r.Values, []string{"/bin/sh", "private value", "short", "two words", "changed"}) {
		t.Fatal(r.Values)
	}
	for _, command := range []string{"echo hello", "/bin/$PROGRAM x", "A=x /bin/$(echo task)", "A='unterminated"} {
		e := Parse([]byte("@daily "+command), "user").Entries[0]
		if strings.Contains(command, "$") && e.Program != "" {
			t.Fatal(e)
		}
	}
}
func TestStableIDsAndDuplicates(t *testing.T) {
	a := Parse([]byte("0  1 * * * /bin/echo  hello\n@daily /bin/echo world\n0 1 * * * /bin/echo  hello\n0 1 * * * /bin/echo  hello"), "user")
	b := Parse([]byte("# added\n@daily /bin/echo world\n\n0 1 * * * /bin/echo  hello"), "user")
	if a.Entries[0].ID != b.Entries[1].ID || a.Entries[1].ID != b.Entries[0].ID || a.Entries[2].ID != a.Entries[0].ID+"-2" || a.Entries[3].ID != a.Entries[0].ID+"-3" {
		t.Fatal(a, b)
	}
	if Parse([]byte("0 1 * * * root /bin/echo  hello"), "system").Entries[0].ID == a.Entries[0].ID {
		t.Fatal("sources share IDs")
	}
}
