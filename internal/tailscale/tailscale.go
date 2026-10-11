package tailscale

import (
	"encoding/json"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

type Handler struct {
	Proxy string `json:"Proxy"`
	Path  string `json:"Path"`
	Text  string `json:"Text"`
}
type TCPHandler struct {
	TCPForward string `json:"TCPForward"`
}
type Config struct {
	TCP map[string]TCPHandler `json:"TCP"`
	Web map[string]struct {
		Handlers map[string]Handler `json:"Handlers"`
	} `json:"Web"`
	AllowFunnel map[string]bool   `json:"AllowFunnel"`
	Services    map[string]Config `json:"Services"`
	Endpoints   map[string]string `json:"endpoints"`
	Version     string            `json:"version"`
}
type Route struct {
	ID, Target string
	Port       int
	FrontPort  int
	Funnel     bool
	Local      bool
}

func Parse(b []byte) (Config, error) { var c Config; err := json.Unmarshal(b, &c); return c, err }
func Routes(c Config) []Route        { return routes(c, "") }
func routes(c Config, prefix string) []Route {
	out := []Route{}
	for endpoint, target := range c.Endpoints {
		port, local := LocalPort(target)
		out = append(out, Route{ID: "tailscale:" + prefix + endpoint, Target: target, Port: port, FrontPort: frontPort(endpoint), Local: local})
	}
	for host, w := range c.Web {
		for mount, h := range w.Handlers {
			target := h.Proxy
			if target == "" {
				if h.Path != "" {
					target = "file:" + h.Path
				} else {
					target = "static content"
				}
			}
			port, local := LocalPort(target)
			id := "tailscale:" + prefix + host + mount
			if prefix != "" && mount == "/" {
				if _, port, e := net.SplitHostPort(host); e == nil {
					id = "tailscale:" + prefix + "tcp:" + port
				}
			}
			out = append(out, Route{ID: id, Target: target, Port: port, FrontPort: frontPort(host), Funnel: c.AllowFunnel[host], Local: local})
		}
	}
	for port, h := range c.TCP {
		if h.TCPForward != "" {
			p, local := LocalPort(h.TCPForward)
			out = append(out, Route{ID: "tailscale:" + prefix + "tcp:" + port, Target: h.TCPForward, Port: p, FrontPort: frontPort(port), Local: local})
		}
	}
	for name, service := range c.Services {
		out = append(out, routes(service, prefix+name+"/")...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func LocalPort(target string) (int, bool) {
	s := target
	if strings.Contains(s, "://") {
		u, e := url.Parse(s)
		if e != nil {
			return 0, false
		}
		s = u.Host
		if u.Port() == "" {
			if u.Scheme == "http" {
				s = net.JoinHostPort(u.Hostname(), "80")
			} else if u.Scheme == "https" {
				s = net.JoinHostPort(u.Hostname(), "443")
			}
		}
	}
	h, p, e := net.SplitHostPort(s)
	if e != nil {
		return 0, false
	}
	ip := net.ParseIP(h)
	local := h == "localhost" || (ip != nil && ip.IsLoopback())
	n, e := strconv.Atoi(p)
	return n, local && e == nil && n > 0 && n <= 65535
}

func frontPort(key string) int {
	if n, err := strconv.Atoi(key); err == nil && n > 0 && n <= 65535 {
		return n
	}
	if _, p, err := net.SplitHostPort(key); err == nil {
		if n, err := strconv.Atoi(p); err == nil && n > 0 && n <= 65535 {
			return n
		}
	}
	if i := strings.LastIndex(key, ":"); i >= 0 {
		if n, err := strconv.Atoi(key[i+1:]); err == nil && n > 0 && n <= 65535 {
			return n
		}
	}
	return 0
}
