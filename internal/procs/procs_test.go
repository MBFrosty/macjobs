package procs

import (
	"os"
	"reflect"
	"testing"
)

func TestSnapshots(t *testing.T) {
	b, e := os.ReadFile("../../testdata/lsof.txt")
	if e != nil {
		t.Fatal(e)
	}
	s := Snapshot{Listeners: ParseLsof(b), PortsKnown: true}
	if pid, ok := s.Listening(8080, true); !ok || pid != 1234 {
		t.Fatal(s)
	}
	if _, ok := s.Listening(9999, false); ok {
		t.Fatal("unexpected port")
	}
	if len(s.Listeners) != 1 || s.Listeners[0].Proto != "tcp" || s.Listeners[0].Command != "example" {
		t.Fatal(s.Listeners)
	}
	b, e = os.ReadFile("../../testdata/ps.txt")
	if e != nil {
		t.Fatal(e)
	}
	p := ParsePS(b)
	if len(p) != 2 || !NameMatches(p[0].Name, "example") || p[1].Name != "/Applications/Example App.app/Contents/MacOS/Example App" {
		t.Fatal(p)
	}
}
func TestRemoteBindDoesNotCoverLoopback(t *testing.T) {
	s := Snapshot{Listeners: []Listener{{PID: 1, Port: 8080, Host: "192.0.2.1"}}}
	if _, ok := s.Listening(8080, true); ok {
		t.Fatal("remote-only bind treated as loopback")
	}
}

func TestParseLsofTCPAndUDP(t *testing.T) {
	tcp, e := os.ReadFile("../../testdata/lsof-ports-tcp.txt")
	if e != nil {
		t.Fatal(e)
	}
	udp, e := os.ReadFile("../../testdata/lsof-ports-udp.txt")
	if e != nil {
		t.Fatal(e)
	}
	got := MergeListeners(ParseLsof(tcp), ParseLsof(udp))
	want := []Listener{
		{Proto: "tcp", Host: "127.0.0.1", Command: "example", PID: 1234, Port: 8080},
		{Proto: "tcp", Host: "*", Command: "web", PID: 2001, Port: 9090},
		{Proto: "tcp", Host: "::1", Command: "loop", PID: 2002, Port: 9091},
		{Proto: "tcp", Host: "127.0.0.1", Command: "other", PID: 3001, Port: 9100},
		{Proto: "udp", Host: "*", Command: "mdns", PID: 4001, Port: 5353},
		{Proto: "udp", Host: "127.0.0.1", Command: "child", PID: 2003, Port: 5354},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
	s := Snapshot{Listeners: got}
	if _, ok := s.ListeningProto(5300, "udp", false); ok {
		t.Fatal("connected UDP kept")
	}
}

func TestParsePSTreeAndOwner(t *testing.T) {
	b, e := os.ReadFile("../../testdata/ps-tree.txt")
	if e != nil {
		t.Fatal(e)
	}
	nodes := ParsePSTree(b)
	if nodes[3].Command != "Example App Helper" || nodes[4].Command != "/Applications/Example App.app/Contents/MacOS/Example App" {
		t.Fatal(nodes)
	}
	s := Snapshot{}
	s.ApplyTree(nodes)
	itemPIDs := map[int]string{1234: "com.example.worker", 2001: "com.example.web"}
	if id, via := s.Owner(1234, itemPIDs); id != "com.example.worker" || via != "" {
		t.Fatal(id, via)
	}
	if id, via := s.Owner(2003, itemPIDs); id != "com.example.web" || via != "ppid" {
		t.Fatal(id, via)
	}
	if id, via := s.Owner(3001, itemPIDs); id != "" || via != "" {
		t.Fatal(id, via)
	}
	if id, via := s.Owner(500, itemPIDs); id != "" || via != "" {
		t.Fatal("cycle walked off", id, via)
	}
	bare := Snapshot{}
	if id, via := bare.Owner(2003, itemPIDs); id != "" || via != "" {
		t.Fatal("tree-less ancestor walk", id, via)
	}
}
