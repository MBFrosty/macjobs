package watchdog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ProbeOptions has no machine-specific defaults. MaxAge defaults to the check interval.
type ProbeOptions struct {
	TCPPeers                                            []string
	LogPath, ConnectedRegex, DisconnectedRegex, HTTPURL string
	MaxAge                                              time.Duration
}

type ProbeResult struct {
	Probe     string `json:"probe"`
	Connected string `json:"connected"`
	Reason    string `json:"reason"`
}

type ProbeInput struct {
	PIDs []int
	Now  time.Time
}

// Probe implementations return bounded, non-sensitive reasons, never source content.
type Probe interface {
	Name() string
	Check(context.Context, ProbeInput) ProbeResult
}

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}
type IPResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type peer struct{ host, port string }

func parsePeer(value string) (peer, error) {
	host, port, err := net.SplitHostPort(value)
	if err != nil || host == "" || strings.ContainsAny(host, "/\\ \t\r\n\x00") {
		return peer{}, errors.New("TCP peers require a host and numeric port (or *)")
	}
	if net.ParseIP(host) == nil && !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]*$`).MatchString(host) {
		return peer{}, errors.New("TCP peer host must be an IP address or DNS name")
	}
	if port != "*" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != port {
			return peer{}, errors.New("TCP peer port must be 1-65535 or *")
		}
	}
	return peer{host, port}, nil
}
func loopbackURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	if u.Port() != "" {
		n, err := strconv.Atoi(u.Port())
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	return true
}
func (o ProbeOptions) Configured() bool {
	return len(o.TCPPeers) > 0 || o.LogPath != "" || o.ConnectedRegex != "" || o.DisconnectedRegex != "" || o.HTTPURL != "" || o.MaxAge != 0
}
func (o ProbeOptions) Validate(interval time.Duration) error {
	if interval <= 0 || o.MaxAge < 0 {
		return errors.New("check interval must be positive and max-age cannot be negative")
	}
	for _, value := range o.TCPPeers {
		if _, err := parsePeer(value); err != nil {
			return err
		}
	}
	if o.LogPath != "" || o.ConnectedRegex != "" || o.DisconnectedRegex != "" {
		if !filepath.IsAbs(o.LogPath) || o.ConnectedRegex == "" || o.DisconnectedRegex == "" {
			return errors.New("log probe requires an absolute path and both connection regexes")
		}
		for _, value := range []string{o.ConnectedRegex, o.DisconnectedRegex} {
			if _, err := regexp.Compile(value); err != nil {
				return errors.New("invalid connection regex")
			}
		}
	}
	if o.HTTPURL != "" && !loopbackURL(o.HTTPURL) {
		return errors.New("HTTP probe requires an http URL with a literal loopback IP and no credentials or fragment")
	}
	return nil
}
func (s Service) probes(o Options) []Probe {
	age := o.Probes.MaxAge
	if age == 0 {
		age = o.Interval
	}
	probes := []Probe{}
	if len(o.Probes.TCPPeers) > 0 {
		probes = append(probes, tcpPeerProbe{service: s, peers: o.Probes.TCPPeers})
	}
	if o.Probes.LogPath != "" {
		probes = append(probes, logProbe{path: o.Probes.LogPath, yes: regexp.MustCompile(o.Probes.ConnectedRegex), no: regexp.MustCompile(o.Probes.DisconnectedRegex), maxAge: age})
	}
	if o.Probes.HTTPURL != "" {
		client := s.HTTPClient
		if client == nil {
			client = &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true, MaxResponseHeaderBytes: 65536}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		}
		probes = append(probes, httpProbe{url: o.Probes.HTTPURL, client: client, maxAge: age})
	}
	return probes
}
func probeResult(name, connected, reason string) ProbeResult {
	return ProbeResult{name, connected, reason}
}

type tcpPeerProbe struct {
	service Service
	peers   []string
}

func (tcpPeerProbe) Name() string { return "tcp-peer" }
func (p tcpPeerProbe) Check(ctx context.Context, in ProbeInput) ProbeResult {
	resolver := p.service.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	targets := map[string]map[string]bool{}
	known := true
	for _, value := range p.peers {
		peer, _ := parsePeer(value)
		ips := []net.IPAddr{}
		if ip := net.ParseIP(peer.host); ip != nil {
			ips = append(ips, net.IPAddr{IP: ip})
		} else {
			lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			var err error
			ips, err = resolver.LookupIPAddr(lookupCtx, peer.host)
			cancel()
			if err != nil || len(ips) == 0 {
				known = false
				continue
			}
		}
		for _, addr := range ips {
			if addr.IP.IsLoopback() {
				continue
			}
			key := addr.IP.String()
			if targets[key] == nil {
				targets[key] = map[string]bool{}
			}
			targets[key][peer.port] = true
		}
	}
	for _, pid := range in.PIDs {
		b, err := p.service.Runner.Run(ctx, "lsof", "-nP", "-a", "-p", strconv.Itoa(pid), "-iTCP", "-sTCP:ESTABLISHED", "-Fn")
		if err != nil {
			if !noMatch(err) {
				known = false
			}
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if !strings.HasPrefix(line, "n") || !strings.Contains(line, "->") {
				continue
			}
			ends := strings.SplitN(line[1:], "->", 2)
			host, port, err := net.SplitHostPort(ends[1])
			ip := net.ParseIP(host)
			if err != nil || ip == nil {
				known = false
				continue
			}
			if ip.IsLoopback() {
				continue
			}
			ports := targets[ip.String()]
			if ports[port] || ports["*"] {
				return probeResult(p.Name(), "yes", "tcp-peer-present")
			}
		}
	}
	if !known {
		return probeResult(p.Name(), "unknown", "tcp-inspection-unavailable")
	}
	return probeResult(p.Name(), "no", "tcp-peer-absent")
}

type logProbe struct {
	path    string
	yes, no *regexp.Regexp
	maxAge  time.Duration
}

func (logProbe) Name() string { return "log" }

var eventTime = regexp.MustCompile(`^\[?(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2}))\]?(?:\s|$)`)

func fresh(at, now time.Time, maxAge time.Duration) string {
	if at.IsZero() {
		return "unknown"
	}
	age := now.Sub(at)
	if age < 0 {
		return "unknown"
	}
	if age > maxAge {
		return "no"
	}
	return "yes"
}
func (p logProbe) Check(_ context.Context, in ProbeInput) ProbeResult {
	// Refuse FIFOs/devices before opening; a source probe must never block on them.
	info, err := os.Stat(p.path)
	if err != nil || !info.Mode().IsRegular() {
		return probeResult(p.Name(), "unknown", "log-unavailable")
	}
	f, err := os.OpenFile(p.path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return probeResult(p.Name(), "unknown", "log-unavailable")
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return probeResult(p.Name(), "unknown", "log-unavailable")
	}
	const limit = 1024 * 1024
	skip := info.Size() > limit
	if skip {
		if _, err = f.Seek(-limit, io.SeekEnd); err != nil {
			return probeResult(p.Name(), "unknown", "log-unavailable")
		}
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || len(b) > limit {
		return probeResult(p.Name(), "unknown", "log-read-unavailable")
	}
	if skip {
		if pos := bytes.IndexByte(b, '\n'); pos >= 0 {
			b = b[pos+1:]
		} else {
			return probeResult(p.Name(), "unknown", "log-no-state")
		}
	}
	if len(b) > 0 && b[len(b)-1] != '\n' {
		return probeResult(p.Name(), "unknown", "log-record-incomplete")
	}
	result := probeResult(p.Name(), "unknown", "log-no-state")
	for _, line := range strings.Split(string(b), "\n") {
		yes, no := p.yes.MatchString(line), p.no.MatchString(line)
		if !yes && !no {
			continue
		}
		if no {
			result = probeResult(p.Name(), "no", "log-disconnected")
			continue
		}
		m := eventTime.FindStringSubmatch(line)
		if m == nil {
			result = probeResult(p.Name(), "unknown", "log-timestamp-invalid")
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, m[1])
		if err != nil {
			result = probeResult(p.Name(), "unknown", "log-timestamp-invalid")
			continue
		}
		switch fresh(at, in.Now, p.maxAge) {
		case "yes":
			result = probeResult(p.Name(), "yes", "log-connected-fresh")
		case "no":
			result = probeResult(p.Name(), "no", "log-stale")
		default:
			result = probeResult(p.Name(), "unknown", "log-timestamp-invalid")
		}
	}
	return result
}

type httpProbe struct {
	url    string
	client HTTPDoer
	maxAge time.Duration
}

func (httpProbe) Name() string { return "http" }
func (p httpProbe) Check(ctx context.Context, in ProbeInput) ProbeResult {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url, nil)
	if err != nil {
		return probeResult(p.Name(), "unknown", "http-invalid")
	}
	response, err := p.client.Do(req)
	if err != nil {
		return probeResult(p.Name(), "no", "http-unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return probeResult(p.Name(), "no", "http-status-unavailable")
	}
	var status struct {
		Connected *bool  `json:"connected"`
		Timestamp string `json:"timestamp"`
	}
	b, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(b) > 65536 {
		return probeResult(p.Name(), "unknown", "http-state-invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	if decoder.Decode(&status) != nil || status.Connected == nil {
		return probeResult(p.Name(), "unknown", "http-state-invalid")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return probeResult(p.Name(), "unknown", "http-state-invalid")
	}
	if !*status.Connected {
		return probeResult(p.Name(), "no", "http-disconnected")
	}
	at, err := time.Parse(time.RFC3339Nano, status.Timestamp)
	if err != nil {
		return probeResult(p.Name(), "unknown", "http-timestamp-invalid")
	}
	switch fresh(at, in.Now, p.maxAge) {
	case "yes":
		return probeResult(p.Name(), "yes", "http-connected-fresh")
	case "no":
		return probeResult(p.Name(), "no", "http-stale")
	default:
		return probeResult(p.Name(), "unknown", "http-timestamp-invalid")
	}
}

// A TCP socket is corroboration only: half-open sockets cannot prove a live link.
func combine(results []ProbeResult) (connected, probe, reason string) {
	if len(results) == 0 {
		return "unknown", "tcp-any", "no-specific-probe"
	}
	connected = "yes"
	probe = "all"
	reason = "all-probes-connected"
	appLevel := false
	for _, r := range results {
		if r.Probe == "log" || r.Probe == "http" {
			appLevel = true
		}
		if r.Connected == "no" {
			return "no", r.Probe, r.Reason
		}
		if r.Connected != "yes" && connected == "yes" {
			connected, probe, reason = "unknown", r.Probe, r.Reason
		}
	}
	if connected == "yes" && !appLevel {
		return "unknown", "tcp-peer", "tcp-peer-needs-freshness"
	}
	return
}

// Args serializes only explicitly configured probes for a generated LaunchAgent.
func (o ProbeOptions) Args() []string {
	args := []string{}
	for _, v := range o.TCPPeers {
		args = append(args, "--tcp-peer", v)
	}
	for _, pair := range [][2]string{{"--probe-log", o.LogPath}, {"--connected-regex", o.ConnectedRegex}, {"--disconnected-regex", o.DisconnectedRegex}, {"--probe-http", o.HTTPURL}} {
		if pair[1] != "" {
			args = append(args, pair[0], pair[1])
		}
	}
	if o.MaxAge != 0 {
		args = append(args, "--max-age", o.MaxAge.String())
	}
	return args
}

// ValidateOutput prevents the watchdog from treating its own writes as app evidence.
func (o ProbeOptions) ValidateOutput(paths ...string) error {
	if o.LogPath == "" {
		return nil
	}
	source, _ := os.Stat(o.LogPath)
	for _, path := range paths {
		target, _ := os.Stat(path)
		a, ae := filepath.EvalSymlinks(o.LogPath)
		b, be := filepath.EvalSymlinks(path)
		if filepath.Clean(o.LogPath) == filepath.Clean(path) || (ae == nil && be == nil && a == b) || (source != nil && target != nil && os.SameFile(source, target)) {
			return errors.New("probe source must be distinct from watchdog log and state files")
		}
	}
	return nil
}
