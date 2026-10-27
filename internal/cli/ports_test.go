package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MBFrosty/macjobs/internal/inventory"
	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/redact"
	"github.com/MBFrosty/macjobs/internal/registry"
)

func portedReport() model.Report {
	r := model.NewReport()
	r.Items = []model.Item{
		{ID: "com.example.web", Kind: "launchd", Owner: "team", Health: "ok", Tags: []string{"homelab"}, Port: 8080, Ports: []model.PortBinding{{Proto: "tcp", Port: 8080, Source: model.PortObserved, Listening: true}, {Proto: "tcp", Port: 9090, Source: model.PortDeclared}}},
		{ID: "com.example.dns", Kind: "process", Owner: "team", Health: "warn", Ports: []model.PortBinding{{Proto: "udp", Port: 5353, Source: model.PortObserved, Listening: true}}},
		{ID: "tailscale:example:443/", Kind: "tailscale", Owner: "unknown", Health: "ok", Port: 8080, Ports: []model.PortBinding{{Proto: "tcp", Port: 8080, Source: model.PortTailscale, Listening: true}, {Proto: "tcp", Port: 443, Source: model.PortTailscale}}},
		{ID: "com.example.none", Kind: "launchd", Owner: "team", Health: "ok"},
	}
	return r
}

func TestFormatPortsAndFilter(t *testing.T) {
	if got := FormatPorts(nil); got != "-" {
		t.Fatal(got)
	}
	if got := FormatPorts(portedReport().Items[0].Ports); got != "8080,!9090" {
		t.Fatal(got)
	}
	many := []model.PortBinding{}
	for _, port := range []int{1, 2, 3, 4, 5} {
		many = append(many, model.PortBinding{Proto: "tcp", Port: port, Source: model.PortObserved, Listening: true})
	}
	if got := FormatPorts(many); got != "1,2,3,4,+1" {
		t.Fatal(got)
	}
	r := portedReport()
	got := Filter(r, "", "", false, nil, []int{8080})
	if len(got.Items) != 2 {
		t.Fatal(got)
	}
	got = Filter(r, "launchd", "team", false, []string{"homelab"}, []int{8080, 5353})
	if len(got.Items) != 1 || got.Items[0].ID != "com.example.web" {
		t.Fatal(got)
	}
	out := &bytes.Buffer{}
	if err := Table(out, r, redact.Redactor{}); err != nil {
		t.Fatal(err)
	}
	header := strings.Fields(strings.Split(out.String(), "\n")[0])
	if !reflect.DeepEqual(header[5:7], []string{"PID", "PORTS"}) || !strings.Contains(out.String(), "8080,!9090") || !strings.Contains(out.String(), "5353/udp") {
		t.Fatal(out.String())
	}
}

func TestListPortFlagAndPortsCommand(t *testing.T) {
	listeners := []model.Listener{
		{Proto: "tcp", Host: "127.0.0.1", Port: 8080, PID: 1234, Command: "example", JobID: "com.example.web", ExpectedBy: []string{"com.example.web"}},
		{Proto: "udp", Host: "*", Port: 5353, PID: 9, Command: "mdns", ExpectedBy: []string{}},
	}
	collect := func(context.Context, registry.Registry, bool) (inventory.Result, error) {
		return inventory.Result{Report: portedReport(), Listeners: listeners}, nil
	}
	run := func(args ...string) (string, error) {
		cmd := New("test", collect)
		out := &bytes.Buffer{}
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs(append([]string{"--registry", filepath.Join(t.TempDir(), "missing")}, args...))
		err := cmd.Execute()
		return out.String(), err
	}
	s, err := run("list", "--json", "--port", "8080,5353")
	if err != nil {
		t.Fatal(err)
	}
	var report model.Report
	if err = json.Unmarshal([]byte(s), &report); err != nil || report.SchemaVersion != 1 || len(report.Items) != 3 {
		t.Fatal(s, err)
	}
	s, err = run("list", "--json", "--port", "8080", "--kind", "tailscale")
	if err != nil || !strings.Contains(s, "tailscale:example:443/") {
		t.Fatal(s, err)
	}
	if _, err = run("list", "--port", "0"); err == nil {
		t.Fatal("accepted invalid port")
	}
	s, err = run("ports", "--json")
	if err != nil {
		t.Fatal(s, err)
	}
	var payload portsReport
	if err = json.Unmarshal([]byte(s), &payload); err != nil || payload.SchemaVersion != 1 || len(payload.Listeners) != 2 {
		t.Fatal(s, err)
	}
	path := filepath.Join("..", "..", "testdata", "ports.golden.json")
	if os.Getenv("MACJOBS_UPDATE_GOLDEN") == "1" {
		if e := os.WriteFile(path, []byte(s), 0644); e != nil {
			t.Fatal(e)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil || s != string(want) {
		t.Fatalf("ports JSON changed:\n%s\n%s", s, err)
	}
	table, err := run("ports")
	if err != nil || !strings.Contains(table, "PROTO") || !strings.Contains(table, "com.example.web") || !strings.Contains(table, "unattributed") {
		t.Fatal(table, err)
	}
	tablePath := filepath.Join("..", "..", "testdata", "ports.golden.txt")
	if os.Getenv("MACJOBS_UPDATE_GOLDEN") == "1" {
		if e := os.WriteFile(tablePath, []byte(table), 0644); e != nil {
			t.Fatal(e)
		}
	}
	wantTable, err := os.ReadFile(tablePath)
	if err != nil || table != string(wantTable) {
		t.Fatalf("ports table changed:\n%s\n%s", table, err)
	}
	only, err := run("ports", "--unattributed", "--json")
	if err != nil {
		t.Fatal(only, err)
	}
	payload = portsReport{}
	if err = json.Unmarshal([]byte(only), &payload); err != nil || len(payload.Listeners) != 1 || payload.Listeners[0].Command != "mdns" || payload.Listeners[0].JobID != "" {
		t.Fatal(only, err, payload)
	}
}

func TestApplePortsFilters(t *testing.T) {
	for _, all := range []bool{false, true} {
		for _, unattributed := range []bool{false, true} {
			for _, jsonOut := range []bool{false, true} {
				root := New("test", func(context.Context, registry.Registry, bool) (inventory.Result, error) {
					return inventory.Result{Report: model.NewReport(), Listeners: []model.Listener{
						{Proto: "tcp", Port: 5000, Command: "AppleService", Apple: true},
						{Proto: "tcp", Port: 8080, Command: "ThirdParty"},
						{Proto: "tcp", Port: 9000, Command: "KnownJob", JobID: "com.example.web"},
					}}, nil
				})
				args := []string{"ports", "--registry", filepath.Join(t.TempDir(), "missing.yaml")}
				if all {
					args = append(args, "--all")
				}
				if unattributed {
					args = append(args, "--unattributed")
				}
				if jsonOut {
					args = append(args, "--json")
				}
				var out bytes.Buffer
				root.SetOut(&out)
				root.SetArgs(args)
				if err := root.Execute(); err != nil {
					t.Fatal(err)
				}
				s := out.String()
				if strings.Contains(s, "AppleService") != (all && !unattributed) || !strings.Contains(s, "ThirdParty") || strings.Contains(s, "KnownJob") == unattributed {
					t.Fatal(args, s)
				}
				if all && !unattributed {
					if jsonOut && !strings.Contains(s, `"apple": true`) {
						t.Fatal(s)
					}
					if !jsonOut && !strings.Contains(s, "Apple") {
						t.Fatal(s)
					}
				}
				if jsonOut {
					var report portsReport
					if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.SchemaVersion != 1 {
						t.Fatal(s, err)
					}
				}
			}
		}
	}
}
