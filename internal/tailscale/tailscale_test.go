package tailscale

import (
	"os"
	"testing"
)

func TestCapturedConfigs(t *testing.T) {
	for _, name := range []string{"tailscale-status.json", "tailscale-get-config.json"} {
		b, e := os.ReadFile("../../testdata/" + name)
		if e != nil {
			t.Fatal(e)
		}
		c, e := Parse(b)
		if e != nil {
			t.Fatal(e)
		}
		routes := Routes(c)
		if name == "tailscale-status.json" && (len(routes) != 2 || routes[0].Port != 8080 || routes[0].FrontPort != 443 || !routes[1].Funnel || routes[1].Port != 8081 || routes[1].FrontPort != 8443) {
			t.Fatalf("routes %+v", routes)
		}
	}
}
func TestServiceAndTCPRoutes(t *testing.T) {
	c, e := Parse([]byte(`{"Services":{"svc:example":{"TCP":{"9000":{"TCPForward":"127.0.0.1:8082"}},"Web":{"example-host:443":{"Handlers":{"/":{"Proxy":"http://localhost:8083"}}}}}}}`))
	if e != nil {
		t.Fatal(e)
	}
	r := Routes(c)
	if len(r) != 2 || !r[0].Local || !r[1].Local || r[0].FrontPort == 0 || r[1].FrontPort == 0 {
		t.Fatal(r)
	}
}
func TestLocalPort(t *testing.T) {
	for _, tc := range []struct {
		s     string
		port  int
		local bool
	}{{"http://127.0.0.1:8080", 8080, true}, {"http://localhost", 80, true}, {"https://[::1]/", 443, true}, {"127.0.0.1:1234", 1234, true}, {"http://example.org:8080", 8080, false}, {"file:/example", 0, false}, {"http://localhost:99999", 99999, false}} {
		p, l := LocalPort(tc.s)
		if p != tc.port || l != tc.local {
			t.Fatalf("%s: %d %v", tc.s, p, l)
		}
	}
}
func TestVersionedServiceEndpoints(t *testing.T) {
	c, e := Parse([]byte(`{"version":"0.0.1","services":{"svc:example":{"endpoints":{"tcp:443":"http://localhost:8080"}}}}`))
	if e != nil {
		t.Fatal(e)
	}
	r := Routes(c)
	if len(r) != 1 || r[0].ID != "tailscale:svc:example/tcp:443" || r[0].Port != 8080 || r[0].FrontPort != 443 || !r[0].Local {
		t.Fatal(r)
	}
}
