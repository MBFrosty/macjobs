package procs

import (
	"net"
	"path/filepath"
	"strconv"
	"strings"
)

type Process struct {
	PID  int
	Name string
}
type Listener struct {
	Proto, Host, Command string
	PID, Port            int
}
type Node struct {
	PID, PPID int
	Command   string
}
type Snapshot struct {
	Processes                  []Process
	Listeners                  []Listener
	Executables                map[int]string
	Parents                    map[int]int
	PortsKnown, ProcessesKnown bool
	TreeKnown                  bool
}

func ParsePS(b []byte) []Process {
	out := []Process{}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		n, e := strconv.Atoi(f[0])
		if e == nil {
			out = append(out, Process{PID: n, Name: strings.Join(f[1:], " ")})
		}
	}
	return out
}

func ParsePSTree(b []byte) []Node {
	out := []Node{}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		pid, e := strconv.Atoi(f[0])
		if e != nil {
			continue
		}
		ppid, e := strconv.Atoi(f[1])
		if e != nil {
			continue
		}
		out = append(out, Node{PID: pid, PPID: ppid, Command: strings.Join(f[2:], " ")})
	}
	return out
}

func (s *Snapshot) ApplyTree(nodes []Node) {
	s.TreeKnown = true
	s.ProcessesKnown = true
	s.Parents = map[int]int{}
	s.Processes = make([]Process, 0, len(nodes))
	for _, n := range nodes {
		s.Parents[n.PID] = n.PPID
		s.Processes = append(s.Processes, Process{PID: n.PID, Name: n.Command})
	}
}

func ParseLsof(b []byte) []Listener {
	out := []Listener{}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[0] == "COMMAND" {
			continue
		}
		pid, e := strconv.Atoi(f[1])
		if e != nil {
			continue
		}
		for i, v := range f {
			proto := strings.ToLower(v)
			if (proto != "tcp" && proto != "udp") || i+1 >= len(f) {
				continue
			}
			name := f[i+1]
			if proto == "udp" && strings.Contains(name, "->") {
				continue
			}
			h, p, e := net.SplitHostPort(name)
			if e != nil {
				h, p, _ = strings.Cut(name, ":")
			}
			n, e := strconv.Atoi(p)
			if e == nil {
				out = append(out, Listener{Proto: proto, Host: h, Command: f[0], PID: pid, Port: n})
			}
		}
	}
	return DedupeListeners(out)
}

func DedupeListeners(in []Listener) []Listener {
	out := []Listener{}
	seen := map[string]bool{}
	for _, l := range in {
		key := l.Proto + "\x00" + l.Host + "\x00" + strconv.Itoa(l.Port) + "\x00" + strconv.Itoa(l.PID)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, l)
	}
	return out
}

func MergeListeners(a, b []Listener) []Listener {
	return DedupeListeners(append(append([]Listener{}, a...), b...))
}

func (s Snapshot) Listening(port int, local bool) (int, bool) {
	return s.ListeningProto(port, "", local)
}

func (s Snapshot) ListeningProto(port int, proto string, local bool) (int, bool) {
	for _, l := range s.Listeners {
		if l.Port != port {
			continue
		}
		if proto != "" && l.Proto != "" && l.Proto != proto {
			continue
		}
		if !local || l.Host == "*" || l.Host == "0.0.0.0" || l.Host == "::" || l.Host == "localhost" {
			return l.PID, true
		}
		if ip := net.ParseIP(l.Host); ip != nil && ip.IsLoopback() {
			return l.PID, true
		}
	}
	return 0, false
}

func (s Snapshot) Owner(pid int, itemPIDs map[int]string) (string, string) {
	if id, ok := itemPIDs[pid]; ok {
		return id, ""
	}
	if !s.TreeKnown || len(s.Parents) == 0 {
		return "", ""
	}
	seen := map[int]bool{}
	cur := pid
	for depth := 0; depth < 32; depth++ {
		if seen[cur] {
			break
		}
		seen[cur] = true
		parent, ok := s.Parents[cur]
		if !ok || parent <= 0 {
			break
		}
		if id, ok := itemPIDs[parent]; ok {
			return id, "ppid"
		}
		cur = parent
	}
	return "", ""
}

func NameMatches(process, wanted string) bool {
	return process == wanted || filepath.Base(process) == wanted
}

// ParseExecutables retains only the first text mapping for each PID: the executable,
// not subsequent shared libraries. Missing evidence must not imply Apple ownership.
func ParseExecutables(b []byte) map[int]string {
	out := map[int]string{}
	pid := 0
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "p") {
			pid, _ = strconv.Atoi(line[1:])
		}
		if pid > 0 && strings.HasPrefix(line, "n") {
			if _, seen := out[pid]; !seen {
				out[pid] = line[1:]
			}
		}
	}
	return out
}

// AppleExecutable is a location heuristic, not signature verification. Match
// directory boundaries after cleaning so lookalike prefixes cannot hide apps.
func AppleExecutable(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	path = filepath.Clean(path)
	for _, root := range []string{"/System/", "/usr/libexec/", "/usr/sbin/", "/Library/Apple/"} {
		if strings.HasPrefix(path, root) {
			return true
		}
	}
	return false
}
