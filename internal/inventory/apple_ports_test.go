package inventory

import (
	"context"
	"strings"
	"testing"

	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/procs"
	"github.com/MBFrosty/macjobs/internal/registry"
)

func TestAppleListenerInventory(t *testing.T) {
	for _, all := range []bool{false, true} {
		for _, evidence := range []bool{false, true} {
			c, replay, _, _ := fixtureCollector(t)
			replay.data["lsof -nP -iTCP -sTCP:LISTEN"] = []byte("ControlCenter 70 user 1u IPv4 0 0 TCP *:5000 (LISTEN)\nExample 80 user 1u IPv4 0 0 TCP *:8080 (LISTEN)\n")
			// A process name/argv[0] alone must never establish Apple ownership.
			replay.data["ps -axo pid=,ppid=,comm="] = []byte("70 1 /System/Library/CoreServices/ControlCenter.app/Contents/MacOS/ControlCenter\n80 1 /usr/libexec/spoof\n")
			if evidence {
				replay.data["lsof -nP -a -p 70,80 -d txt -Fpn"] = []byte("p70\nn/System/Library/CoreServices/ControlCenter.app/Contents/MacOS/ControlCenter\np80\nn/Applications/Example.app/Contents/MacOS/Example\nn/System/Library/Frameworks/Example.framework/Example\n")
			} else {
				replay.fail["lsof -nP -a -p 70,80 -d txt -Fpn"] = true
			}
			result := c.Collect(context.Background(), registry.Empty(), all)
			if len(result.Listeners) != 2 || result.Listeners[0].Apple != evidence || result.Listeners[1].Apple {
				t.Fatalf("all=%v evidence=%v listeners=%+v", all, evidence, result.Listeners)
			}
			count := "2 listeners have no known job"
			if evidence {
				count = "1 listeners have no known job"
			}
			if !strings.Contains(strings.Join(result.Report.Warnings, "\n"), count) {
				t.Fatal(result.Report.Warnings)
			}
		}
	}
}

func TestAppleJobLabelClassifiesListener(t *testing.T) {
	items := map[string]*model.Item{"com.apple.example": {ID: "com.apple.example", Label: "com.apple.example", PID: 42}}
	snap := procs.Snapshot{Listeners: []procs.Listener{{PID: 42, Proto: "udp", Port: 5353}}}
	listeners := attributeListeners(items, snap)
	if len(listeners) != 1 || !listeners[0].Apple || listeners[0].JobID != "com.apple.example" {
		t.Fatal(listeners)
	}
}
