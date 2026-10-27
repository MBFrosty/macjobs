package runner

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Runner is the only external-command boundary. Implementations must not log raw output.
type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}
type OS struct{}

func (OS) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if !Allowed(name, args) {
		return nil, errors.New("command is outside the read-only allowlist")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	// stderr may include credentials or environment values; deliberately discard it.
	return cmd.Output()
}
func Allowed(name string, args []string) bool {
	a := strings.Join(args, " ")
	switch filepath.Base(name) {
	case "crontab":
		return len(args) == 1 && args[0] == "-l"
	case "launchctl":
		if len(args) == 1 && args[0] == "list" {
			return true
		}
		return len(args) == 2 && (args[0] == "print" || args[0] == "print-disabled") && (strings.HasPrefix(args[1], "gui/") || args[1] == "system" || strings.HasPrefix(args[1], "system/"))
	case "brew":
		return a == "services list --json"
	case "tailscale", "Tailscale":
		return a == "serve status --json" || a == "serve get-config --all"
	case "lsof":
		if len(args) == 7 && args[0] == "-nP" && args[1] == "-a" && args[2] == "-p" && args[4] == "-d" && args[5] == "txt" && args[6] == "-Fpn" {
			ids := strings.Split(args[3], ",")
			if len(ids) > 64 {
				return false
			}
			for _, id := range ids {
				if !positive(id) {
					return false
				}
			}
			return true
		}
		return a == "-nP -iTCP -sTCP:LISTEN" || a == "-nP -iUDP"
	case "ps":
		return a == "-axo pid=,comm=" || a == "-axo pid=,ppid=,comm=" || (len(args) == 4 && args[0] == "-o" && args[1] == "ppid=" && args[2] == "-p" && positive(args[3]))
	}
	return false
}

func positive(s string) bool {
	n, e := strconv.Atoi(s)
	return e == nil && n > 0 && strconv.Itoa(n) == s
}
