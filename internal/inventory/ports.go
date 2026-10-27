package inventory

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/MBFrosty/macjobs/internal/model"
	jobplist "github.com/MBFrosty/macjobs/internal/plist"
	"github.com/MBFrosty/macjobs/internal/procs"
	"github.com/MBFrosty/macjobs/internal/registry"
	"github.com/MBFrosty/macjobs/internal/tailscale"
)

func attachExpected(item *model.Item, e *registry.Entry, route *tailscale.Route) {
	if e != nil {
		ports := append([]int{}, e.Ports...)
		if e.Port > 0 {
			ports = append(ports, e.Port)
		}
		if e.Match != nil && e.Match.Port > 0 {
			ports = append(ports, e.Match.Port)
		}
		if normalized, err := registry.NormalizePorts(ports); err == nil {
			for _, port := range normalized {
				item.Ports = appendPort(item.Ports, model.PortBinding{Proto: "tcp", Port: port, Source: model.PortDeclared})
			}
		}
	}
	if item.Plist != nil {
		for _, spec := range jobplist.Sockets(item.Plist) {
			item.Ports = appendPort(item.Ports, model.PortBinding{Proto: spec.Proto, Port: spec.Port, Host: spec.Host, Source: model.PortSocket})
		}
		for _, spec := range jobplist.ArgPorts(item.Plist) {
			item.Ports = appendPort(item.Ports, model.PortBinding{Proto: spec.Proto, Port: spec.Port, Source: model.PortInferred})
		}
	}
	if route != nil {
		if route.Port > 0 {
			item.Ports = appendPort(item.Ports, model.PortBinding{Proto: "tcp", Port: route.Port, Source: model.PortTailscale})
		}
		if route.FrontPort > 0 && route.FrontPort != route.Port {
			item.Ports = appendPort(item.Ports, model.PortBinding{Proto: "tcp", Port: route.FrontPort, Source: model.PortTailscale})
		}
	} else if item.Kind == "tailscale" && item.Port > 0 {
		item.Ports = appendPort(item.Ports, model.PortBinding{Proto: "tcp", Port: item.Port, Source: model.PortTailscale})
	}
}

func attributeListeners(items map[string]*model.Item, snap procs.Snapshot) []model.Listener {
	ids := make([]string, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	itemPIDs := map[int]string{}
	for _, id := range ids {
		if pid := items[id].PID; pid > 0 {
			if _, ok := itemPIDs[pid]; !ok {
				itemPIDs[pid] = id
			}
		}
	}
	out := []model.Listener{}
	for _, l := range snap.Listeners {
		job, via := snap.Owner(l.PID, itemPIDs)
		expectedBy := []string{}
		for _, id := range ids {
			if expects(items[id], l.Proto, l.Port) {
				expectedBy = append(expectedBy, id)
			}
		}
		view := model.Listener{Proto: l.Proto, Host: l.Host, Port: l.Port, PID: l.PID, Command: l.Command, JobID: job, Via: via, ExpectedBy: expectedBy}
		view.Apple = procs.AppleExecutable(snap.Executables[l.PID])
		if job != "" && strings.HasPrefix(items[job].Label, "com.apple.") {
			view.Apple = true
		}
		if job != "" {
			items[job].Ports = appendPort(items[job].Ports, model.PortBinding{Proto: l.Proto, Port: l.Port, Host: l.Host, PID: l.PID, Source: model.PortObserved, Listening: true, Via: via})
		}
		out = append(out, view)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Proto != out[j].Proto {
			return out[i].Proto < out[j].Proto
		}
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		if out[i].Host != out[j].Host {
			return out[i].Host < out[j].Host
		}
		return out[i].PID < out[j].PID
	})
	return out
}

func markListening(item *model.Item, snap procs.Snapshot) {
	for n, b := range item.Ports {
		if b.Source == model.PortObserved {
			item.Ports[n].Listening = true
			continue
		}
		local := item.Kind == "tailscale" && b.Port == item.Port
		if pid, ok := snap.ListeningProto(b.Port, b.Proto, local); ok {
			item.Ports[n].Listening = true
			if item.Ports[n].PID == 0 {
				item.Ports[n].PID = pid
			}
			if item.Ports[n].Host == "" {
				item.Ports[n].Host = listenerHost(snap, b.Port, b.Proto, pid)
			}
		}
	}
}

func expects(item *model.Item, proto string, port int) bool {
	for _, b := range item.Ports {
		if b.Source == model.PortObserved {
			continue
		}
		if b.Port == port && (b.Proto == "" || proto == "" || b.Proto == proto) {
			return true
		}
	}
	return item.Port == port
}

func appendPort(ports []model.PortBinding, next model.PortBinding) []model.PortBinding {
	if next.Proto == "" {
		next.Proto = "tcp"
	}
	for _, existing := range ports {
		if existing.Source == next.Source && existing.Proto == next.Proto && existing.Port == next.Port && existing.Host == next.Host && existing.PID == next.PID && existing.Via == next.Via {
			return ports
		}
	}
	return append(ports, next)
}

func listenerHost(snap procs.Snapshot, port int, proto string, pid int) string {
	for _, l := range snap.Listeners {
		if l.Port == port && l.PID == pid && (proto == "" || l.Proto == "" || l.Proto == proto) {
			return l.Host
		}
	}
	return ""
}

// Read executable mappings only for visible listening PIDs, in bounded batches.
func (c *Collector) collectExecutables(ctx context.Context, snap *procs.Snapshot) {
	pids := []int{}
	seen := map[int]bool{}
	for _, l := range snap.Listeners {
		if l.PID > 0 && !seen[l.PID] {
			pids = append(pids, l.PID)
			seen[l.PID] = true
		}
	}
	sort.Ints(pids)
	snap.Executables = map[int]string{}
	for start := 0; start < len(pids); start += 64 {
		end := start + 64
		if end > len(pids) {
			end = len(pids)
		}
		ids := []string{}
		for _, pid := range pids[start:end] {
			ids = append(ids, strconv.Itoa(pid))
		}
		b, _ := c.Runner.Run(ctx, "lsof", "-nP", "-a", "-p", strings.Join(ids, ","), "-d", "txt", "-Fpn")
		// lsof can return partial output when a selected process exits. Retain only
		// observed mappings; an absent or unreadable executable stays unclassified.
		for pid, path := range procs.ParseExecutables(b) {
			snap.Executables[pid] = path
		}
	}
}
