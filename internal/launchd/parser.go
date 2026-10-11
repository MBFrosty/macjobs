package launchd

import (
	"bufio"
	"strconv"
	"strings"
)

type State struct {
	State, Path, Signal string
	PID, Runs           int
	LastExit            *int
}

// ParsePrint only accepts direct fields of the service block, never nested environment fields.
func ParsePrint(b []byte) State {
	var s State
	depth := 0
	scan := bufio.NewScanner(strings.NewReader(string(b)))
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if depth == 1 {
			k, v, ok := strings.Cut(line, " = ")
			if ok {
				v = strings.Trim(v, "\"")
				switch k {
				case "state":
					s.State = v
				case "path":
					s.Path = v
				case "pid":
					s.PID, _ = strconv.Atoi(v)
				case "runs":
					s.Runs, _ = strconv.Atoi(v)
				case "last exit code":
					if n, e := strconv.Atoi(v); e == nil {
						s.LastExit = &n
					}
				case "last terminating signal":
					s.Signal = v
				}
			}
		}
		if strings.HasSuffix(line, "{") {
			depth++
		}
		if line == "}" {
			depth--
		}
	}
	return s
}

type Row struct {
	Label    string
	PID      int
	LastExit *int
}

func ParseList(b []byte) []Row {
	var rows []Row
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 || f[0] == "PID" {
			continue
		}
		r := Row{Label: f[2]}
		r.PID, _ = strconv.Atoi(f[0])
		if n, e := strconv.Atoi(f[1]); e == nil {
			r.LastExit = &n
		}
		rows = append(rows, r)
	}
	return rows
}
func ParseDomain(b []byte) []Row {
	var rows []Row
	inside := false
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "services = {" {
			inside = true
			continue
		}
		if !inside {
			continue
		}
		if strings.TrimSpace(line) == "}" {
			break
		}
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		pid, e := strconv.Atoi(f[0])
		if e != nil {
			continue
		}
		r := Row{Label: f[len(f)-1], PID: pid}
		if n, e := strconv.Atoi(f[1]); e == nil {
			r.LastExit = &n
		}
		rows = append(rows, r)
	}
	return rows
}
func ParseDisabled(b []byte) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), " => ")
		if !ok {
			k, v, ok = strings.Cut(strings.TrimSpace(line), " = ")
		}
		if ok && (v == "true" || v == "false" || v == "enabled" || v == "disabled") {
			out[strings.Trim(k, "\"")] = v == "true" || v == "disabled"
		}
	}
	return out
}
