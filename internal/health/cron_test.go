package health

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/procs"
)

func TestCronHealth(t *testing.T) {
	dir := t.TempDir()
	exists := filepath.Join(dir, "exists")
	if err := os.WriteFile(exists, []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, program, health       string
		managed, malformed, percent bool
	}{
		{"foreign missing", filepath.Join(dir, "missing"), "warn", false, false, false},
		{"managed missing", filepath.Join(dir, "missing"), "fail", true, false, false},
		{"existing", exists, "ok", true, false, false},
		{"relative", "task", "ok", true, false, false},
		{"malformed", "", "warn", false, true, false},
		{"percent", "", "warn", false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := model.Item{Kind: "cron", Observed: true, Live: true, Managed: tc.managed, CronProgram: tc.program, CronMalformed: tc.malformed, CronPercent: tc.percent, Line: 5}
			Evaluate(&i, procs.Snapshot{}, time.Now())
			if i.Health != tc.health {
				t.Fatal(i)
			}
			if tc.malformed && !strings.Contains(strings.Join(i.Notes, " "), "malformed cron line 5") {
				t.Fatal(i)
			}
		})
	}
}
