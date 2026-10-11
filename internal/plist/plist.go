package plist

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	codec "howett.net/plist"
)

type PortSpec struct {
	Proto, Host, Source string
	Port                int
}

func Decode(b []byte) (map[string]any, error) {
	var p map[string]any
	_, err := codec.Unmarshal(b, &p)
	return p, err
}
func Encode(p map[string]any, format int) ([]byte, error) { return codec.Marshal(p, format) }
func Number(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case uint64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}
func String(p map[string]any, k string) string { s, _ := p[k].(string); return s }
func KeepAlive(p map[string]any) bool {
	switch v := p["KeepAlive"].(type) {
	case bool:
		return v
	case map[string]any:
		return len(v) > 0
	}
	return false
}
func Schedule(p map[string]any) (string, time.Duration) {
	if n := Number(p["StartInterval"]); n > 0 {
		d := time.Duration(n) * time.Second
		return "every " + d.String(), d
	}
	if v, ok := p["StartCalendarInterval"]; ok {
		var parts []string
		switch x := v.(type) {
		case map[string]any:
			parts = append(parts, calendar(x))
		case []any:
			for _, e := range x {
				if m, ok := e.(map[string]any); ok {
					parts = append(parts, calendar(m))
				}
			}
		}
		return strings.Join(parts, "; "), 0
	}
	if KeepAlive(p) {
		return "keep alive", 0
	}
	if b, _ := p["RunAtLoad"].(bool); b {
		return "at load", 0
	}
	return "on demand", 0
}
func calendar(m map[string]any) string {
	weekdays := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	parts := []string{}
	if v, ok := m["Weekday"]; ok {
		n := Number(v)
		if n >= 0 && n < len(weekdays) {
			parts = append(parts, weekdays[n])
		}
	}
	if v, ok := m["Month"]; ok {
		parts = append(parts, fmt.Sprintf("month %d", Number(v)))
	}
	if v, ok := m["Day"]; ok {
		parts = append(parts, fmt.Sprintf("day %d", Number(v)))
	}
	h, min := "*", "*"
	if v, ok := m["Hour"]; ok {
		h = fmt.Sprintf("%02d", Number(v))
	}
	if v, ok := m["Minute"]; ok {
		min = fmt.Sprintf("%02d", Number(v))
	}
	parts = append(parts, h+":"+min)
	return strings.Join(parts, " ")
}

// NominalInterval gives a conservative calendar cadence for log-staleness checks.
// Multiple calendar entries use the longest cadence to avoid premature warnings.
func NominalInterval(p map[string]any) time.Duration {
	if n := Number(p["StartInterval"]); n > 0 {
		return time.Duration(n) * time.Second
	}
	var calendars []map[string]any
	switch v := p["StartCalendarInterval"].(type) {
	case map[string]any:
		calendars = append(calendars, v)
	case []any:
		for _, v := range v {
			if m, ok := v.(map[string]any); ok {
				calendars = append(calendars, m)
			}
		}
	}
	longest := time.Duration(0)
	for _, m := range calendars {
		d := time.Minute
		if _, ok := m["Minute"]; ok {
			d = time.Hour
		}
		if _, ok := m["Hour"]; ok {
			d = 24 * time.Hour
		}
		if _, ok := m["Weekday"]; ok {
			d = 7 * 24 * time.Hour
		}
		if _, ok := m["Day"]; ok {
			d = 31 * 24 * time.Hour
		}
		if _, ok := m["Month"]; ok {
			d = 366 * 24 * time.Hour
		}
		if d > longest {
			longest = d
		}
	}
	return longest
}

func Sockets(p map[string]any) []PortSpec {
	if p == nil {
		return nil
	}
	raw, ok := p["Sockets"]
	if !ok {
		return nil
	}
	out := []PortSpec{}
	switch m := raw.(type) {
	case map[string]any:
		for _, v := range m {
			out = append(out, socketValue(v)...)
		}
	}
	return out
}

func ArgPorts(p map[string]any) []PortSpec {
	if p == nil {
		return nil
	}
	args := stringList(p["ProgramArguments"])
	if len(args) == 0 {
		if s := String(p, "Program"); s != "" {
			args = []string{s}
		}
	}
	out := []PortSpec{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := ""
		if i+1 < len(args) {
			next = args[i+1]
		}
		switch {
		case a == "--port" || a == "-p" || a == "--http-port":
			if n, ok := parsePort(next); ok {
				out = append(out, PortSpec{Proto: "tcp", Port: n, Source: "inferred"})
				i++
			}
		case strings.HasPrefix(a, "--port="):
			if n, ok := parsePort(strings.TrimPrefix(a, "--port=")); ok {
				out = append(out, PortSpec{Proto: "tcp", Port: n, Source: "inferred"})
			}
		case strings.HasPrefix(a, "--http-port="):
			if n, ok := parsePort(strings.TrimPrefix(a, "--http-port=")); ok {
				out = append(out, PortSpec{Proto: "tcp", Port: n, Source: "inferred"})
			}
		case a == "--listen" || a == "--bind" || a == "--addr":
			if n, ok := parseListen(next); ok {
				out = append(out, PortSpec{Proto: "tcp", Port: n, Source: "inferred"})
				i++
			}
		case strings.HasPrefix(a, "--listen="):
			if n, ok := parseListen(strings.TrimPrefix(a, "--listen=")); ok {
				out = append(out, PortSpec{Proto: "tcp", Port: n, Source: "inferred"})
			}
		case strings.HasPrefix(a, "--bind="):
			if n, ok := parseListen(strings.TrimPrefix(a, "--bind=")); ok {
				out = append(out, PortSpec{Proto: "tcp", Port: n, Source: "inferred"})
			}
		case strings.HasPrefix(a, "--addr="):
			if n, ok := parseListen(strings.TrimPrefix(a, "--addr=")); ok {
				out = append(out, PortSpec{Proto: "tcp", Port: n, Source: "inferred"})
			}
		}
	}
	return out
}

func socketValue(v any) []PortSpec {
	switch x := v.(type) {
	case map[string]any:
		if spec, ok := socketDict(x); ok {
			return []PortSpec{spec}
		}
	case []any:
		out := []PortSpec{}
		for _, e := range x {
			if m, ok := e.(map[string]any); ok {
				if spec, ok := socketDict(m); ok {
					out = append(out, spec)
				}
			}
		}
		return out
	}
	return nil
}

func socketDict(m map[string]any) (PortSpec, bool) {
	proto := "tcp"
	if t, _ := m["SockType"].(string); strings.EqualFold(t, "dgram") {
		proto = "udp"
	}
	name := ""
	switch v := m["SockServiceName"].(type) {
	case string:
		name = v
	default:
		if n := Number(v); n > 0 {
			name = strconv.Itoa(n)
		}
	}
	if name == "" {
		return PortSpec{}, false
	}
	port := 0
	if n, err := strconv.Atoi(name); err == nil {
		port = n
	} else if n, err := net.LookupPort(proto, name); err == nil {
		port = n
	}
	if port < 1 || port > 65535 {
		return PortSpec{}, false
	}
	host, _ := m["SockNodeName"].(string)
	return PortSpec{Proto: proto, Port: port, Host: host, Source: "socket"}, true
}

func stringList(v any) []string {
	switch x := v.(type) {
	case []any:
		out := []string{}
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return append([]string{}, x...)
	}
	return nil
}

func parsePort(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil && n > 0 && n <= 65535
}

func parseListen(s string) (int, bool) {
	if strings.HasPrefix(s, ":") {
		return parsePort(s[1:])
	}
	_, p, err := net.SplitHostPort(s)
	if err != nil {
		return 0, false
	}
	return parsePort(p)
}
