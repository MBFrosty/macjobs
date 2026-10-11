package plist

import (
	codec "howett.net/plist"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestRoundTripXMLAndBinary(t *testing.T) {
	b, e := os.ReadFile("../../testdata/worker.plist")
	if e != nil {
		t.Fatal(e)
	}
	p, e := Decode(b)
	if e != nil {
		t.Fatal(e)
	}
	for _, format := range []int{codec.XMLFormat, codec.BinaryFormat} {
		b, e := Encode(p, format)
		if e != nil {
			t.Fatal(e)
		}
		got, e := Decode(b)
		if e != nil || !reflect.DeepEqual(p, got) {
			t.Fatalf("round trip format %d: %v", format, e)
		}
	}
}
func TestSchedule(t *testing.T) {
	for _, tc := range []struct {
		p        map[string]any
		want     string
		interval time.Duration
	}{
		{map[string]any{"StartInterval": uint64(600)}, "every 10m0s", 10 * time.Minute},
		{map[string]any{"KeepAlive": true}, "keep alive", 0},
		{map[string]any{"KeepAlive": map[string]any{"SuccessfulExit": false}}, "keep alive", 0},
		{map[string]any{"RunAtLoad": true}, "at load", 0},
		{map[string]any{}, "on demand", 0},
		{map[string]any{"StartCalendarInterval": map[string]any{"Weekday": uint64(0), "Hour": uint64(3), "Minute": uint64(0)}}, "Sun 03:00", 0},
		{map[string]any{"StartCalendarInterval": []any{map[string]any{"Minute": uint64(15)}, map[string]any{"Hour": uint64(10), "Minute": uint64(30)}}}, "*:15; 10:30", 0},
		{map[string]any{"StartCalendarInterval": map[string]any{"Month": uint64(2), "Day": uint64(1)}}, "month 2 day 1 *:*", 0},
	} {
		s, d := Schedule(tc.p)
		if s != tc.want || d != tc.interval {
			t.Fatalf("schedule got %q %v want %q %v", s, d, tc.want, tc.interval)
		}
	}
}
func TestSocketsAndArgPorts(t *testing.T) {
	p := map[string]any{
		"Sockets": map[string]any{
			"HTTP": map[string]any{"SockServiceName": "8080", "SockType": "stream", "SockNodeName": "127.0.0.1"},
			"DNS":  map[string]any{"SockServiceName": "domain", "SockType": "dgram"},
			"Many": []any{map[string]any{"SockServiceName": uint64(9090)}},
		},
		"ProgramArguments": []any{"/usr/bin/example", "--port", "8081", "--port=8082", "-p", "8083", "--http-port", "8084", "--listen", "127.0.0.1:8085", "--bind=:8086", "--addr", "[::1]:8087", "-plist", "ignored"},
	}
	sockets := Sockets(p)
	if len(sockets) != 3 {
		t.Fatal(sockets)
	}
	found := map[int]PortSpec{}
	for _, s := range sockets {
		found[s.Port] = s
	}
	if found[8080].Proto != "tcp" || found[8080].Source != "socket" || found[8080].Host != "127.0.0.1" {
		t.Fatal(found[8080])
	}
	if found[53].Proto != "udp" || found[9090].Proto != "tcp" {
		t.Fatal(found)
	}
	args := ArgPorts(p)
	ports := []int{}
	for _, s := range args {
		if s.Source != "inferred" || s.Proto != "tcp" {
			t.Fatal(s)
		}
		ports = append(ports, s.Port)
	}
	if !reflect.DeepEqual(ports, []int{8081, 8082, 8083, 8084, 8085, 8086, 8087}) {
		t.Fatal(ports)
	}
	if specs := ArgPorts(map[string]any{"ProgramArguments": []any{"/usr/bin/example", "-p"}}); len(specs) != 0 {
		t.Fatal(specs)
	}
}
func TestCalendarCadence(t *testing.T) {
	for _, tc := range []struct {
		p    map[string]any
		want time.Duration
	}{{map[string]any{"StartCalendarInterval": map[string]any{"Weekday": uint64(0), "Hour": uint64(3), "Minute": uint64(0)}}, 7 * 24 * time.Hour}, {map[string]any{"StartCalendarInterval": map[string]any{"Hour": uint64(3)}}, 24 * time.Hour}, {map[string]any{"StartCalendarInterval": map[string]any{"Minute": uint64(3)}}, time.Hour}} {
		if got := NominalInterval(tc.p); got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
}
