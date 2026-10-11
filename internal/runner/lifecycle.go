package runner

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Lifecycle is used only after the action layer has checked ownership and ancestry.
// Editor subprocess output is discarded because it can contain plist secrets.
type Lifecycle struct {
	Editor []string
	Input  io.Reader
}

var labelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
var formulaPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@+_.-]*(/[A-Za-z0-9][A-Za-z0-9@+_.-]*)*$`)

func userDomain(s string) bool {
	p := strings.Split(s, "/")
	return len(p) == 2 && p[0] == "gui" && positive(p[1])
}
func userTarget(s string) bool {
	p := strings.Split(s, "/")
	return len(p) == 3 && userDomain(strings.Join(p[:2], "/")) && labelPattern.MatchString(p[2]) && !strings.HasPrefix(p[2], "com.apple.")
}
func LifecycleAllowed(name string, args []string) bool {
	switch filepath.Base(name) {
	case "launchctl":
		if len(args) == 2 && (args[0] == "bootout" || args[0] == "enable" || args[0] == "disable" || args[0] == "kickstart") {
			return userTarget(args[1])
		}
		if len(args) == 3 && args[0] == "kickstart" && args[1] == "-k" {
			return userTarget(args[2])
		}
		if len(args) == 3 && args[0] == "bootstrap" && userDomain(args[1]) {
			p := filepath.Clean(args[2])
			return filepath.IsAbs(p) && filepath.Base(filepath.Dir(p)) == "LaunchAgents" && strings.HasSuffix(p, ".plist") && !strings.HasPrefix(p, "/System/") && !strings.HasPrefix(filepath.Base(p), "com.apple.")
		}
	case "brew":
		return len(args) == 3 && args[0] == "services" && (args[1] == "start" || args[1] == "stop" || args[1] == "restart") && formulaPattern.MatchString(args[2])
	case "plutil":
		return len(args) == 2 && args[0] == "-lint" && filepath.IsAbs(args[1]) && strings.HasSuffix(args[1], ".plist")
	}
	return false
}
func (r Lifecycle) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	editor := len(r.Editor) > 0 && name == r.Editor[0] && len(args) == len(r.Editor) && filepath.IsAbs(args[len(args)-1]) && strings.HasSuffix(args[len(args)-1], ".plist")
	if editor {
		for i, a := range r.Editor[1:] {
			if args[i] != a {
				editor = false
			}
		}
	}
	if editor && filepath.Base(name) == "sudo" {
		return nil, errors.New("privilege escalation is refused")
	}
	if !editor && !Allowed(name, args) && !LifecycleAllowed(name, args) {
		return nil, errors.New("command is outside the lifecycle allowlist")
	}
	if editor {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Stdin = r.Input
		return nil, cmd.Run()
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}
