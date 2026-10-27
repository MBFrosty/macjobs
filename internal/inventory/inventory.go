package inventory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/MBFrosty/macjobs/internal/brew"
	"github.com/MBFrosty/macjobs/internal/health"
	"github.com/MBFrosty/macjobs/internal/launchd"
	"github.com/MBFrosty/macjobs/internal/model"
	jobplist "github.com/MBFrosty/macjobs/internal/plist"
	"github.com/MBFrosty/macjobs/internal/procs"
	"github.com/MBFrosty/macjobs/internal/redact"
	"github.com/MBFrosty/macjobs/internal/registry"
	"github.com/MBFrosty/macjobs/internal/runner"
	"github.com/MBFrosty/macjobs/internal/tailscale"
)

type Collector struct {
	Runner        runner.Runner
	Home          string
	UID           int
	PlistDirs     map[string]string
	TailscalePath string
	SystemCrontab string
	Now           func() time.Time
}
type Result struct {
	Report      model.Report
	Redactor    redact.Redactor
	LogRedactor redact.Redactor
	Ports       procs.Snapshot
	Listeners   []model.Listener
	routes      map[string]tailscale.Route
}

func New() (*Collector, error) {
	if runtime.GOOS != "darwin" {
		return nil, errors.New("live inspection requires macOS")
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return nil, errors.New("cannot resolve home directory")
	}
	ts := "tailscale"
	if _, e := exec.LookPath(ts); e != nil {
		ts = "/Applications/Tailscale.app/Contents/MacOS/Tailscale"
	}
	return &Collector{Runner: runner.OS{}, Home: home, UID: os.Getuid(), TailscalePath: ts, SystemCrontab: "/etc/crontab", Now: time.Now, PlistDirs: map[string]string{filepath.Join(home, "Library", "LaunchAgents"): "launchd", "/Library/LaunchAgents": "launchd", "/Library/LaunchDaemons": "system"}}, nil
}
func (c *Collector) Collect(ctx context.Context, reg registry.Registry, all bool) Result {
	result := Result{Report: model.NewReport(), routes: map[string]tailscale.Route{}}
	items := map[string]*model.Item{}
	warn := func(s string) { result.Report.Warnings = append(result.Report.Warnings, s) }
	domains := map[string]bool{}
	for _, kind := range []string{"launchd", "system"} {
		domain := "system"
		if kind == "launchd" {
			domain = fmt.Sprintf("gui/%d", c.UID)
		}
		b, e := c.Runner.Run(ctx, "launchctl", "print", domain)
		rows := launchd.ParseDomain(b)
		known := e == nil && strings.Contains(string(b), "services = {")
		if !known && kind == "launchd" {
			b, e = c.Runner.Run(ctx, "launchctl", "list")
			rows = launchd.ParseList(b)
			known = e == nil
			if known {
				warn("user domain unavailable; using launchctl list fallback")
			}
		}
		domains[kind] = known
		if !known {
			warn(kind + " domain unavailable")
		}
		overrides := map[string]bool{}
		if b, e := c.Runner.Run(ctx, "launchctl", "print-disabled", domain); e == nil {
			overrides = launchd.ParseDisabled(b)
		} else {
			warn(kind + " disabled overrides unavailable")
		}
		for _, row := range rows {
			if !all && strings.HasPrefix(row.Label, "com.apple.") {
				continue
			}
			id := row.Label
			if kind == "system" {
				id = "system:" + id
			}
			item := &model.Item{ID: id, Label: row.Label, Kind: kind, Domain: domain, Owner: "unknown", State: "not running", PID: row.PID, LastExit: row.LastExit, ReadOnly: true, Live: true, Observed: known, Disabled: overrides[row.Label]}
			if row.PID > 0 {
				item.State = "running"
			}
			if item.LastExit != nil && *item.LastExit < 0 {
				item.Signal = fmt.Sprint(-*item.LastExit)
				item.LastExit = nil
			}
			if b, e := c.Runner.Run(ctx, "launchctl", "print", domain+"/"+row.Label); e == nil {
				state := launchd.ParsePrint(b)
				item.PlistPath = state.Path
				if state.State != "" {
					item.State = state.State
					item.PID = state.PID
					item.Runs = state.Runs
					item.Signal = state.Signal
					item.LastExit = state.LastExit
				} else {
					warn("some " + kind + " service details could not be parsed")
				}
			} else {
				warn("some " + kind + " service details unavailable")
			}

			items[id] = item
		}
	}
	// Read local plist metadata, including installed jobs absent from the live domain.
	dirs := make([]string, 0, len(c.PlistDirs))
	for dir := range c.PlistDirs {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		kind := c.PlistDirs[dir]
		entries, e := os.ReadDir(dir)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			warn(kind + " plist directory unavailable")
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".plist") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			b, e := os.ReadFile(path)
			if e != nil {
				warn("some plist files unreadable")
				continue
			}
			p, e := jobplist.Decode(b)
			if e != nil {
				warn("some plist files invalid")
				continue
			}
			label := jobplist.String(p, "Label")
			if label == "" || (!all && strings.HasPrefix(label, "com.apple.")) {
				continue
			}
			id := label
			if kind == "system" {
				id = "system:" + id
			}
			item, ok := items[id]
			if !ok {
				item = &model.Item{ID: id, Label: label, Kind: kind, Owner: "unknown", State: "not loaded", ReadOnly: true, Observed: domains[kind]}
				items[id] = item
			}
			if item.PlistPath == "" {
				item.PlistPath = path
			}
		}
	}
	for _, item := range items {
		c.readPlist(item, &result)
	}
	if b, e := c.Runner.Run(ctx, "brew", "services", "list", "--json"); e == nil {
		services, e := brew.Parse(b)
		if e != nil {
			warn("Homebrew JSON could not be parsed")
		} else {
			domains["brew"] = true
			for _, s := range services {
				id := "homebrew.mxcl." + s.Name
				item, exists := items[id]
				if !exists {
					item, exists = items["system:"+id]
				}
				if exists {
					item.BrewName = s.Name
					if item.Owner == "unknown" {
						item.Owner = "homebrew"
					}
					continue
				}
				item = &model.Item{ID: id, Label: id, Kind: "brew", BrewName: s.Name, Owner: "homebrew", State: s.Status, LastExit: s.ExitCode, ReadOnly: true, Live: true, Observed: true, PlistPath: s.File}
				// A service with no installed plist is a known, inactive Homebrew service.
				if s.Status == "none" || s.Status == "stopped" {
					item.PlistPath = ""
				}
				c.readPlist(item, &result)
				items[id] = item
			}
		}
	} else {
		warn("Homebrew services unavailable")
	}
	cronKnown := c.collectCron(ctx, items, &result)
	tsKnown := false
	// Status holds node routes; get-config may hold additional service routes.
	for _, args := range [][]string{{"serve", "status", "--json"}, {"serve", "get-config", "--all"}} {
		b, e := c.Runner.Run(ctx, c.TailscalePath, args...)
		if e != nil {
			warn("a Tailscale configuration source unavailable")
			continue
		}
		config, e := tailscale.Parse(b)
		if e != nil {
			warn("Tailscale JSON could not be parsed")
			continue
		}
		tsKnown = true
		for _, route := range tailscale.Routes(config) {
			port := 0
			if route.Local {
				port = route.Port
			}
			items[route.ID] = &model.Item{ID: route.ID, Kind: "tailscale", Owner: "unknown", State: "configured", Target: route.Target, Port: port, Funnel: route.Funnel, ReadOnly: true, Live: true, Observed: true}
			result.routes[route.ID] = route
		}
	}
	if b, e := c.Runner.Run(ctx, "lsof", "-nP", "-iTCP", "-sTCP:LISTEN"); e == nil {
		result.Ports.PortsKnown = true
		result.Ports.Listeners = procs.ParseLsof(b)
	} else {
		var exit *exec.ExitError
		if errors.As(e, &exit) && exit.ExitCode() == 1 && len(b) == 0 {
			result.Ports.PortsKnown = true
		} else {
			warn("listening ports unavailable")
		}
	}
	if b, e := c.Runner.Run(ctx, "lsof", "-nP", "-iUDP"); e == nil {
		result.Ports.Listeners = procs.MergeListeners(result.Ports.Listeners, procs.ParseLsof(b))
	} else {
		var exit *exec.ExitError
		if errors.As(e, &exit) && exit.ExitCode() == 1 && len(b) == 0 {
			// no unconnected UDP sockets
		} else {
			warn("UDP sockets unavailable")
		}
	}
	if b, e := c.Runner.Run(ctx, "ps", "-axo", "pid=,ppid=,comm="); e == nil {
		result.Ports.ApplyTree(procs.ParsePSTree(b))
	} else {
		warn("process tree unavailable")
		needsProcesses := false
		for _, e := range reg.Entries {
			if e.Match != nil && (e.Match.Name != "" || e.Match.Regex != "") {
				needsProcesses = true
			}
		}
		if needsProcesses {
			if b, e := c.Runner.Run(ctx, "ps", "-axo", "pid=,comm="); e == nil {
				result.Ports.ProcessesKnown = true
				result.Ports.Processes = procs.ParsePS(b)
			} else {
				warn("process snapshot unavailable")
			}
		}
	}
	for _, e := range reg.Entries {
		if !all && (strings.HasPrefix(e.ID, "com.apple.") || strings.HasPrefix(e.ID, "system:com.apple.")) {
			continue
		}
		item, ok := items[e.ID]
		if !ok {
			kind := e.Kind
			if kind == "" {
				kind = "launchd"
			}
			if e.Match != nil {
				kind = "process"
			}
			known := domains[kind]
			if kind == "tailscale" {
				known = tsKnown
			}
			if kind == "cron" {
				source := "user"
				if strings.HasPrefix(e.ID, "cron:system:") {
					source = "system"
				}
				known = cronKnown[source]
			}
			item = &model.Item{ID: e.ID, Label: e.ID, Kind: kind, State: "absent", ReadOnly: true, Observed: known}
			items[e.ID] = item
			if kind == "process" && e.Match != nil {
				c.match(item, e.Match, result.Ports)
			}
		}
		item.Registered = true
		item.Owner = e.Owner
		if item.Owner == "" {
			item.Owner = "unknown"
		}
		item.Tags = append([]string(nil), e.Tags...)
		item.Purpose = e.Purpose
		item.Managed = e.Managed
		item.CreatedByTool = e.CreatedByTool
		if e.Port > 0 {
			item.Port = e.Port
		}
		if e.Log != "" {
			item.Logs = appendUnique(item.Logs, c.expand(e.Log))
			c.logTimestamp(item)
		}
	}
	for _, item := range items {
		var entry *registry.Entry
		for n := range reg.Entries {
			if reg.Entries[n].ID == item.ID {
				entry = &reg.Entries[n]
				break
			}
		}
		var route *tailscale.Route
		if r, ok := result.routes[item.ID]; ok {
			route = &r
		}
		attachExpected(item, entry, route)
	}
	c.collectExecutables(ctx, &result.Ports)
	result.Listeners = attributeListeners(items, result.Ports)
	for _, item := range items {
		markListening(item, result.Ports)
	}
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	keys := make([]string, 0, len(items))
	for k := range items {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		i := items[k]
		health.Evaluate(i, result.Ports, now)
		result.Report.Items = append(result.Report.Items, *i)
	}
	result.Report.Warnings = append(result.Report.Warnings, health.PortConflicts(result.Report.Items, result.Ports, result.Listeners)...)
	sort.Strings(result.Report.Warnings)
	result.Report.Warnings = unique(result.Report.Warnings)
	return result
}
func (c *Collector) expand(s string) string {
	if strings.HasPrefix(s, "~/") {
		return filepath.Join(c.Home, s[2:])
	}
	return s
}
func (c *Collector) readPlist(item *model.Item, r *Result) {
	if item.PlistPath == "" {
		return
	}
	item.PlistPath = c.expand(item.PlistPath)
	if !filepath.IsAbs(item.PlistPath) {
		item.PlistPath = ""
		return
	}
	b, e := os.ReadFile(item.PlistPath)
	if os.IsNotExist(e) {
		item.PlistMissing = true
		return
	}
	if e != nil {
		item.PlistUnreadable = true
		return
	}
	p, e := jobplist.Decode(b)
	if e != nil {
		item.PlistUnreadable = true
		return
	}
	item.Plist = p
	item.KeepAlive = jobplist.KeepAlive(p)
	item.Schedule, _ = jobplist.Schedule(p)
	item.Interval = jobplist.NominalInterval(p)
	for _, key := range []string{"StandardOutPath", "StandardErrorPath"} {
		if s := jobplist.String(p, key); s != "" {
			item.Logs = appendUnique(item.Logs, c.expand(s))
		}
	}
	c.logTimestamp(item)
	r.Redactor.Values = append(r.Redactor.Values, redact.EnvironmentValues(p)...)
	r.LogRedactor.Values = append(r.LogRedactor.Values, redact.AllEnvironmentValues(p)...)
}
func (c *Collector) logTimestamp(i *model.Item) {
	for _, path := range i.Logs {
		if s, e := os.Stat(path); e == nil {
			t := s.ModTime().UTC()
			if i.LastRun == nil || t.After(*i.LastRun) {
				i.LastRun = &t
			}
		}
	}
}
func (c *Collector) match(i *model.Item, m *registry.Match, p procs.Snapshot) {
	i.Observed = true
	if m.Port > 0 && !p.PortsKnown {
		i.Observed = false
	}
	if (m.Name != "" || m.Regex != "") && !p.ProcessesKnown {
		i.Observed = false
	}
	if !i.Observed {
		return
	}
	if m.Name == "" && m.Regex == "" {
		i.PID, i.Live = p.Listening(m.Port, false)
	} else {
		var re *regexp.Regexp
		if m.Regex != "" {
			re, _ = regexp.Compile(m.Regex)
		}
		for _, proc := range p.Processes {
			if m.Name != "" && !procs.NameMatches(proc.Name, m.Name) {
				continue
			}
			if re != nil && !re.MatchString(proc.Name) {
				continue
			}
			if m.Port > 0 {
				matchPort := false
				for _, l := range p.Listeners {
					if l.PID == proc.PID && l.Port == m.Port {
						matchPort = true
					}
				}
				if !matchPort {
					continue
				}
			}
			i.PID = proc.PID
			i.Live = true
			break
		}
	}
	i.Port = m.Port
	if i.Live {
		i.State = "running"
	} else {
		i.State = "not running"
	}
}
func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}
func unique(s []string) []string {
	out := []string{}
	for _, v := range s {
		out = appendUnique(out, v)
	}
	return out
}
