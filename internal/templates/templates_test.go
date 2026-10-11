package templates

import (
	"fmt"
	jobplist "github.com/MBFrosty/macjobs/internal/plist"
	"github.com/MBFrosty/macjobs/internal/watchdog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGoldenPlists(t *testing.T) {
	for _, kind := range []string{"interval", "calendar", "keepalive", "app-watchdog"} {
		t.Run(kind, func(t *testing.T) {
			o := Options{Template: kind, Label: "local.macjobs.example", Log: "/home/example/Library/Logs/macjobs/example.log", Command: []string{"/bin/echo", "example"}}
			switch kind {
			case "interval":
				o.Every = 10 * time.Minute
			case "calendar":
				o.At = []string{"Sun 03:00", "Wed 15:30"}
			case "app-watchdog":
				o.Every = 10 * time.Minute
				o.App = "Example App"
				o.Binary = "/home/example/.local/bin/macjobs"
				o.Command = nil
			}
			b, err := Render(o)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("..", "..", "testdata", "template-"+kind+".golden.plist")
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(path, b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil || string(want) != string(b) {
				t.Fatalf("golden differs: %v\n%s", err, b)
			}
		})
	}
}
func TestInvalidOptions(t *testing.T) {
	for _, o := range []Options{
		{Template: "interval", Command: []string{"/bin/echo"}, Every: 500 * time.Millisecond},
		{Template: "interval", Command: []string{"echo"}, Every: time.Minute},
		{Template: "calendar", Command: []string{"/bin/echo"}, At: []string{"Sun 25:00"}},
		{Template: "calendar", Command: []string{"/bin/echo"}, At: []string{"not a time"}},
		{Template: "calendar", Command: []string{"/bin/echo"}},
		{Template: "keepalive", Command: []string{"/bin/echo"}, Every: time.Minute},
		{Template: "app-watchdog", App: "--background", Binary: "/bin/macjobs", Every: time.Minute},
		{Template: "app-watchdog", App: "Example App", Binary: "macjobs", Every: time.Minute},
		{Template: "interval", App: "Example App", Command: []string{"/bin/echo"}, Every: time.Minute},
		{Template: "unknown", Command: []string{"/bin/echo"}},
	} {
		if _, err := Render(o); err == nil {
			t.Fatal("accepted invalid options", o)
		}
	}
}

func TestWatchdogProbeArguments(t *testing.T) {
	o := Options{Template: "app-watchdog", Label: "local.macjobs.example", App: "Example App", Log: "/tmp/watchdog.log", Binary: "/bin/macjobs", Every: time.Minute, Probes: watchdog.ProbeOptions{TCPPeers: []string{"relay.example.invalid:443", "[2001:db8::42]:*"}, LogPath: "/tmp/example-app.log", ConnectedRegex: `link=connected`, DisconnectedRegex: `link=disconnected`, HTTPURL: "http://127.0.0.1:12345/status", MaxAge: 30 * time.Second}}
	b, err := Render(o)
	if err != nil {
		t.Fatal(err)
	}
	p, err := jobplist.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	args := p["ProgramArguments"].([]any)
	want := append([]string{o.Binary, "watchdog", "check", "--app", o.App, "--log", o.Log, "--interval", "1m0s"}, o.Probes.Args()...)
	if fmt.Sprint(args) != fmt.Sprint(want) {
		t.Fatal(args, want)
	}
	o.RestartOnDisconnect = true
	b, err = Render(o)
	if err != nil {
		t.Fatal(err)
	}
	p, _ = jobplist.Decode(b)
	args = p["ProgramArguments"].([]any)
	if args[len(args)-1] != "--restart-on-disconnect" {
		t.Fatal(args)
	}
	o.Template = "interval"
	o.Command = []string{"/bin/echo"}
	if _, err := Render(o); err == nil {
		t.Fatal("probe options accepted outside watchdog")
	}
}
