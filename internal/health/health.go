package health

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/procs"
)

func Evaluate(i *model.Item, p procs.Snapshot, now time.Time) {
	i.Health = "ok"
	i.Notes = []string{}
	if !i.Observed {
		add(i, "unknown", "live state unavailable")
	} else if !i.Live && i.Registered {
		add(i, "fail", "registry entry has no live job")
	}
	if i.PlistMissing {
		add(i, "fail", "plist is missing")
	}
	if i.PlistUnreadable {
		add(i, "unknown", "plist could not be read")
	}
	programFindings(i)
	cronFindings(i)
	if i.Disabled {
		add(i, "warn", "disabled by launchd override")
	}
	if i.KeepAlive && i.PID == 0 && i.State != "started" && !i.Disabled && i.Observed {
		add(i, "fail", "KeepAlive job is not running")
	}
	if i.LastExit != nil && *i.LastExit != 0 {
		add(i, "fail", fmt.Sprintf("nonzero last exit: %d", *i.LastExit))
	}
	if i.Signal != "" {
		add(i, "fail", "last run terminated by signal: "+i.Signal)
	}
	if i.Runs >= 5 && (i.Signal != "" || (i.LastExit != nil && *i.LastExit != 0)) && i.PID == 0 {
		add(i, "fail", "possible crash loop (repeated runs with unsuccessful last termination)")
	}
	if i.Interval > 0 && len(i.Logs) > 0 {
		if i.LastRun == nil {
			add(i, "unknown", "scheduled job has no readable log timestamp")
		} else if now.Sub(*i.LastRun) > 2*i.Interval {
			add(i, "warn", "scheduled job log is stale")
		}
	}
	portFindings(i, p)
	if i.Kind == "brew" && (i.State == "error" || i.State == "unknown") {
		add(i, "warn", "Homebrew reports "+i.State)
	}
}

func PortConflicts(items []model.Item, p procs.Snapshot, listeners []model.Listener) []string {
	expected := map[string][]string{}
	for i := range items {
		seen := map[string]bool{}
		for _, b := range expectedBindings(items[i]) {
			if b.Source != model.PortDeclared && b.Source != model.PortSocket && b.Source != model.PortInferred {
				continue
			}
			key := protoKey(b.Proto, b.Port)
			if seen[key] {
				continue
			}
			seen[key] = true
			expected[key] = append(expected[key], items[i].ID)
		}
	}
	for key, ids := range expected {
		if len(ids) < 2 {
			continue
		}
		sort.Strings(ids)
		note := fmt.Sprintf("multiple jobs expect %s: %s", displayKey(key), strings.Join(ids, ", "))
		for i := range items {
			if contains(ids, items[i].ID) {
				add(&items[i], "warn", note)
			}
		}
	}
	for i := range items {
		for _, b := range expectedBindings(items[i]) {
			if b.Source == model.PortTailscale && b.Port != items[i].Port {
				continue
			}
			for _, l := range listeners {
				if l.Port != b.Port || proto(l.Proto) != proto(b.Proto) {
					continue
				}
				if l.JobID == items[i].ID || items[i].PID > 0 && l.PID == items[i].PID {
					continue
				}
				if l.JobID != "" && expectsPort(items, l.JobID, b.Port, b.Proto) {
					continue
				}
				if l.JobID != "" {
					add(&items[i], "warn", fmt.Sprintf("port %s held by %s", FormatPort(b.Proto, b.Port), l.JobID))
				} else if l.PID > 0 {
					add(&items[i], "warn", fmt.Sprintf("port %s held by unattributed pid %d", FormatPort(b.Proto, b.Port), l.PID))
				}
			}
		}
		if items[i].Kind == "tailscale" && items[i].Port > 0 && p.PortsKnown {
			if _, ok := p.Listening(items[i].Port, true); !ok {
				owners := []string{}
				for _, o := range items {
					if o.ID == items[i].ID {
						continue
					}
					if expectsPort(items, o.ID, items[i].Port, "tcp") {
						owners = append(owners, o.ID)
					}
				}
				if len(owners) > 0 {
					sort.Strings(owners)
					rewriteNote(&items[i], "serve route points to a dead local port", "serve route points to a dead local port (backend expected by "+strings.Join(owners, ", ")+")")
				}
			}
		}
	}
	n := 0
	for _, l := range listeners {
		if l.JobID == "" {
			n++
		}
	}
	if n == 0 {
		return nil
	}
	return []string{fmt.Sprintf("%d listeners have no known job (see macjobs ports)", n)}
}

func programFindings(i *model.Item) {
	if i.Kind != "launchd" {
		return
	}
	if strings.HasPrefix(i.Label, "com.apple.") || strings.HasPrefix(i.ID, "com.apple.") {
		return
	}
	missingProgram(i, programPath(i.Plist))
}

func missingProgram(i *model.Item, program string) {
	if program == "" || !filepath.IsAbs(program) {
		return
	}
	_, err := os.Stat(program)
	if err == nil || !os.IsNotExist(err) {
		return
	}
	note := "program not found: " + program
	if i.Managed {
		add(i, "fail", note)
		return
	}
	add(i, "warn", note)
}

func programPath(p map[string]any) string {
	if p == nil {
		return ""
	}
	switch args := p["ProgramArguments"].(type) {
	case []any:
		if len(args) > 0 {
			if s, ok := args[0].(string); ok {
				return s
			}
		}
	case []string:
		if len(args) > 0 {
			return args[0]
		}
	}
	s, _ := p["Program"].(string)
	return s
}

func portFindings(i *model.Item, p procs.Snapshot) {
	bindings := expectedBindings(*i)
	if len(bindings) == 0 {
		return
	}
	if !p.PortsKnown {
		add(i, "unknown", "listening ports unavailable")
		return
	}
	socketActivated := false
	for _, b := range i.Ports {
		if b.Source == model.PortSocket {
			socketActivated = true
		}
	}
	seen := map[string]bool{}
	for _, b := range bindings {
		key := b.Source + "/" + proto(b.Proto) + "/" + strconv.Itoa(b.Port)
		if seen[key] {
			continue
		}
		seen[key] = true
		if listening(i, b, p) {
			continue
		}
		if b.Source == model.PortTailscale && b.Port == i.Port {
			add(i, "fail", "serve route points to a dead local port")
			continue
		}
		if b.Source == model.PortTailscale {
			continue
		}
		if b.Source == model.PortInferred {
			add(i, "warn", "inferred port "+FormatPort(b.Proto, b.Port)+" is not listening")
			continue
		}
		if !activeJob(i) {
			continue
		}
		if i.Kind == "system" || socketActivated || b.Source == model.PortSocket {
			add(i, "unknown", "listener owner not visible without root")
			continue
		}
		if b.Source == model.PortDeclared {
			add(i, "fail", "registered port is not listening")
		}
	}
}

func expectedBindings(i model.Item) []model.PortBinding {
	if len(i.Ports) > 0 {
		out := []model.PortBinding{}
		for _, b := range i.Ports {
			if b.Source != model.PortObserved {
				out = append(out, b)
			}
		}
		return out
	}
	if i.Port > 0 {
		src := model.PortDeclared
		if i.Kind == "tailscale" {
			src = model.PortTailscale
		}
		return []model.PortBinding{{Proto: "tcp", Port: i.Port, Source: src}}
	}
	return nil
}

func listening(i *model.Item, b model.PortBinding, p procs.Snapshot) bool {
	if b.Listening {
		return true
	}
	local := i.Kind == "tailscale" && b.Port == i.Port
	_, ok := p.ListeningProto(b.Port, proto(b.Proto), local)
	return ok
}

func activeJob(i *model.Item) bool {
	return i.KeepAlive || i.PID > 0 || i.State == "running" || i.State == "started"
}

func add(i *model.Item, level, note string) {
	severity := map[string]int{"ok": 0, "unknown": 1, "warn": 2, "fail": 3}
	if severity[level] > severity[i.Health] {
		i.Health = level
	}
	for _, existing := range i.Notes {
		if existing == note {
			return
		}
	}
	i.Notes = append(i.Notes, note)
}

func rewriteNote(i *model.Item, old, next string) {
	for n, note := range i.Notes {
		if note == old {
			i.Notes[n] = next
		}
	}
}

func expectsPort(items []model.Item, id string, port int, protoName string) bool {
	for _, i := range items {
		if i.ID != id {
			continue
		}
		for _, b := range expectedBindings(i) {
			if b.Port == port && proto(b.Proto) == proto(protoName) {
				return true
			}
		}
	}
	return false
}

func proto(s string) string {
	if s == "" {
		return "tcp"
	}
	return s
}

func protoKey(protoName string, port int) string {
	return proto(protoName) + "/" + strconv.Itoa(port)
}

func displayKey(key string) string {
	protoName, port, _ := strings.Cut(key, "/")
	n, _ := strconv.Atoi(port)
	return FormatPort(protoName, n)
}

func contains(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func FormatPort(protoName string, port int) string {
	if proto(protoName) == "udp" {
		return strconv.Itoa(port) + "/udp"
	}
	return strconv.Itoa(port)
}

func cronFindings(i *model.Item) {
	if i.Kind != "cron" {
		return
	}
	if i.CronMalformed {
		add(i, "warn", fmt.Sprintf("malformed cron line %d", i.Line))
		return
	}
	if i.CronPercent {
		add(i, "warn", "cron % splits command and stdin (unless escaped)")
	}
	missingProgram(i, i.CronProgram)
}
