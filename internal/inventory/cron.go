package inventory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/MBFrosty/macjobs/internal/cron"
	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/redact"
)

func (c *Collector) collectCron(ctx context.Context, items map[string]*model.Item, result *Result) map[string]bool {
	known := map[string]bool{}
	parsed := map[string]cron.Result{}
	b, err := c.Runner.Run(ctx, "crontab", "-l")
	var exit interface{ ExitCode() int }
	if err == nil || errors.As(err, &exit) && exit.ExitCode() == 1 && len(b) == 0 {
		known["user"] = true
		parsed["user"] = cron.Parse(b, "user")
	} else {
		result.Report.Warnings = append(result.Report.Warnings, "user crontab unavailable (may need Full Disk Access)")
	}
	// New supplies /etc/crontab; tests inject a temporary path. Empty skips I/O.
	if c.SystemCrontab != "" {
		b, err := os.ReadFile(c.SystemCrontab)
		if err == nil {
			known["system"] = true
			parsed["system"] = cron.Parse(b, "system")
		} else if os.IsNotExist(err) {
			known["system"] = true
		} else {
			result.Report.Warnings = append(result.Report.Warnings, "system crontab unreadable without root; skipped")
		}
	}
	for _, source := range []string{"user", "system"} {
		values := redact.CronValues(parsed[source].Values)
		result.Redactor.Values = append(result.Redactor.Values, values...)
		result.LogRedactor.Values = append(result.LogRedactor.Values, values...)
	}
	for _, source := range []string{"user", "system"} {
		for _, e := range parsed[source].Entries {
			command := result.Redactor.CronText(e.Command)
			env := map[string]string{}
			for key := range e.Env {
				env[key] = redact.Hidden
			}
			label := e.Schedule + " " + command
			if e.Malformed {
				label = fmt.Sprintf("malformed cron line %d", e.Line)
			}
			chars := []rune(label)
			if len(chars) > 100 {
				label = string(chars[:100]) + "…"
			}
			items[e.ID] = &model.Item{ID: e.ID, Kind: "cron", Source: source, Line: e.Line, RunAs: e.RunAs, Label: strings.TrimSpace(label), Owner: "unknown", State: "scheduled", Schedule: e.Schedule, Command: command, ReadOnly: true, Live: true, Observed: true, CronProgram: e.Program, CronMalformed: e.Malformed, CronPercent: e.Percent, CronEntry: &model.CronEntry{Schedule: e.Schedule, Command: command, Env: env}}
		}
	}
	return known
}
