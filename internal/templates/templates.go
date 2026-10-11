// Package templates renders user-agent plists without executing commands.
package templates

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	jobplist "github.com/MBFrosty/macjobs/internal/plist"
	"github.com/MBFrosty/macjobs/internal/watchdog"
	codec "howett.net/plist"
)

type Options struct {
	Template, Label, Log, App, Binary string
	Every                             time.Duration
	At                                []string
	Command                           []string
	Probes                            watchdog.ProbeOptions
	RestartOnDisconnect               bool
}

var clock = regexp.MustCompile(`^(?:(Sun|Mon|Tue|Wed|Thu|Fri|Sat) )?([0-9]{2}):([0-9]{2})$`)

func Render(o Options) ([]byte, error) {
	p := map[string]any{"Label": o.Label, "StandardOutPath": o.Log, "StandardErrorPath": o.Log}
	args := o.Command
	if o.Template == "app-watchdog" {
		if strings.TrimSpace(o.App) == "" || strings.HasPrefix(o.App, "-") || strings.ContainsAny(o.App, "/\x00\r\n") || len(args) != 0 || !filepath.IsAbs(o.Binary) {
			return nil, errors.New("app-watchdog requires an app name, an absolute binary path, and no command")
		}
		args = []string{o.Binary, "watchdog", "check", "--app", o.App, "--log", o.Log}
		if err := o.Probes.Validate(o.Every); err != nil {
			return nil, err
		}
		if err := o.Probes.ValidateOutput(o.Log); err != nil {
			return nil, err
		}
		args = append(args, "--interval", o.Every.String())
		args = append(args, o.Probes.Args()...)
		if o.RestartOnDisconnect {
			args = append(args, "--restart-on-disconnect")
		}
		p["RunAtLoad"] = true
	}
	if len(args) == 0 || !filepath.IsAbs(args[0]) {
		return nil, errors.New("an absolute command executable is required after --")
	}
	for _, arg := range args {
		if strings.ContainsRune(arg, 0) {
			return nil, errors.New("command arguments cannot contain NUL")
		}
	}
	p["ProgramArguments"] = args
	switch o.Template {
	case "interval", "app-watchdog":
		if o.Every < time.Second || o.Every%time.Second != 0 || o.Every/time.Second > 2147483647 || len(o.At) != 0 {
			return nil, errors.New("--every must be a positive whole number of seconds; --at is calendar-only")
		}
		p["StartInterval"] = int(o.Every / time.Second)
	case "calendar":
		if len(o.At) == 0 || o.Every != 0 {
			return nil, errors.New("calendar requires --at and does not accept --every")
		}
		entries := []map[string]any{}
		for _, at := range o.At {
			m := clock.FindStringSubmatch(at)
			if m == nil {
				return nil, errors.New("--at must be HH:MM or Day HH:MM (for example Sun 03:00)")
			}
			h, _ := strconv.Atoi(m[2])
			minute, _ := strconv.Atoi(m[3])
			if h > 23 || minute > 59 {
				return nil, errors.New("calendar time is out of range")
			}
			entry := map[string]any{"Hour": h, "Minute": minute}
			if m[1] != "" {
				for i, day := range []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"} {
					if day == m[1] {
						entry["Weekday"] = i
					}
				}
			}
			entries = append(entries, entry)
		}
		if len(entries) == 1 {
			p["StartCalendarInterval"] = entries[0]
		} else {
			p["StartCalendarInterval"] = entries
		}
	case "keepalive":
		if o.Every != 0 || len(o.At) != 0 {
			return nil, errors.New("keepalive does not accept schedule flags")
		}
		p["KeepAlive"], p["RunAtLoad"] = true, true
	default:
		return nil, fmt.Errorf("unknown template; choose interval, calendar, keepalive, or app-watchdog")
	}
	if o.Template != "app-watchdog" && (o.App != "" || o.Probes.Configured() || o.RestartOnDisconnect) {
		return nil, errors.New("app and connection probe flags are app-watchdog-only")
	}
	return jobplist.Encode(p, codec.XMLFormat)
}
