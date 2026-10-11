package runner

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func appName(s string) bool {
	return strings.TrimSpace(s) != "" && !strings.HasPrefix(s, "-") && !strings.ContainsAny(s, "/\x00\r\n")
}

func WatchdogAllowed(name string, args []string) bool {
	switch filepath.Base(name) {
	case "open":
		return len(args) == 2 && args[0] == "-a" && appName(args[1])
	case "pgrep":
		return len(args) == 2 && args[0] == "-x" && appName(args[1])
	case "lsof":
		return len(args) == 7 && args[0] == "-nP" && args[1] == "-a" && args[2] == "-p" && positive(args[3]) && args[4] == "-iTCP" && args[5] == "-sTCP:ESTABLISHED" && args[6] == "-Fn"
	case "ps":
		return len(args) == 2 && args[0] == "-axo" && args[1] == "pid=,comm="
	}
	return false
}

// WatchdogQuitScript constructs the sole permitted clean quit operation.
func WatchdogQuitScript(app string) string {
	quoted := `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(app) + `"`
	return "if application " + quoted + " is running then\n tell application " + quoted + " to quit\nend if"
}

// Watchdog cannot mutate services. QuitApp is set only for opt-in disconnect recovery.
type Watchdog struct{ QuitApp string }

func (w Watchdog) Allowed(name string, args []string) bool {
	quit := filepath.Base(name) == "osascript" && appName(w.QuitApp) && len(args) == 2 && args[0] == "-e" && args[1] == WatchdogQuitScript(w.QuitApp)
	return WatchdogAllowed(name, args) || quit
}

func (w Watchdog) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if !w.Allowed(name, args) {
		return nil, errors.New("command is outside the watchdog allowlist")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}
