package actions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/MBFrosty/macjobs/internal/launchd"
	"github.com/MBFrosty/macjobs/internal/model"
	jobplist "github.com/MBFrosty/macjobs/internal/plist"
	"github.com/MBFrosty/macjobs/internal/registry"
	"github.com/MBFrosty/macjobs/internal/runner"
)

type Operation struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	When    string   `json:"when,omitempty"`
}
type ActionError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *ActionError) Error() string { return e.Message }

type Report struct {
	Tags          []string     `json:"tags,omitempty"`
	SchemaVersion int          `json:"schemaVersion"`
	Action        string       `json:"action"`
	ID            string       `json:"id"`
	Label         string       `json:"label"`
	Kind          string       `json:"kind"`
	DryRun        bool         `json:"dryRun"`
	Ops           []Operation  `json:"ops"`
	Result        string       `json:"result"`
	Note          string       `json:"note,omitempty"`
	Error         *ActionError `json:"error"`
}

// ForeignDryRunNote tells callers a planned foreign-job change will be refused without confirmation.
const ForeignDryRunNote = "foreign job: a real run requires --yes plus typed confirmation of the label on a TTY"

type Options struct{ DryRun, Yes, PurgeLog bool }
type Service struct {
	Runner       runner.Runner
	Home         string
	UID, PID     int
	RegistryPath string
	Now          func() time.Time
	Editor       []string
	TTY          bool
	Confirm      func(string) (string, error)
}

var safeLabel = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

func Failure(r *Report, code, message string) error {
	r.Result = "refused"
	r.Error = &ActionError{Code: code, Message: message}
	return r.Error
}
func destructive(action string) bool {
	return action == "stop" || action == "restart" || action == "disable" || action == "remove" || action == "edit"
}
func (s Service) CheckAncestor(ctx context.Context, pid int) error {
	if pid <= 0 {
		return nil
	}
	current := s.PID
	seen := map[int]bool{}
	for current > 0 {
		if current == pid {
			return errors.New("job contains the current process in its process tree; interrupting it would terminate this session")
		}
		if seen[current] {
			return errors.New("cannot verify process ancestry; action refused")
		}
		seen[current] = true
		if current == 1 {
			return nil
		}
		b, err := s.Runner.Run(ctx, "ps", "-o", "ppid=", "-p", strconv.Itoa(current))
		parent, parseErr := strconv.Atoi(strings.TrimSpace(string(b)))
		if err != nil || parseErr != nil || parent < 0 || parent == current {
			return errors.New("cannot verify process ancestry; action refused")
		}
		current = parent
	}
	return nil
}
func (s Service) protected(i model.Item) bool {
	p := filepath.Clean(i.PlistPath)
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return i.Kind == "system" || strings.HasPrefix(i.ID, "system:") || strings.HasPrefix(i.Domain, "system") || strings.HasPrefix(i.Label, "com.apple.") || p == "/System" || strings.HasPrefix(p, "/System/") || p == "/Library/LaunchDaemons" || strings.HasPrefix(p, "/Library/LaunchDaemons/")
}
func (s Service) plist(i model.Item) ([]byte, os.FileInfo, error) {
	p := filepath.Clean(i.PlistPath)
	if !filepath.IsAbs(p) || (filepath.Dir(p) != filepath.Join(s.Home, "Library", "LaunchAgents") && filepath.Dir(p) != "/Library/LaunchAgents") {
		return nil, nil, errors.New("a plist in a user LaunchAgents directory is required")
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil || resolved != p {
		return nil, nil, errors.New("plist must exist and must not use symbolic links")
	}
	info, err := os.Lstat(p)
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil, errors.New("plist must be a regular file")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, nil, errors.New("cannot read plist")
	}
	pmap, err := jobplist.Decode(b)
	if err != nil || jobplist.String(pmap, "Label") != i.Label {
		return nil, nil, errors.New("plist label does not match the selected job")
	}
	return b, info, nil
}
func (s Service) Execute(ctx context.Context, action string, i model.Item, opts Options) (Report, error) {
	r := Report{SchemaVersion: model.SchemaVersion, Action: action, ID: i.ID, Label: i.Label, Kind: i.Kind, DryRun: opts.DryRun, Ops: []Operation{}, Result: "planned"}
	refuse := func(code, message string) (Report, error) { err := Failure(&r, code, message); return r, err }
	if !safeLabel.MatchString(i.Label) && i.Kind != "tailscale" {
		return refuse("invalid_label", "job label is invalid")
	}
	if s.protected(i) {
		return refuse("protected_job", "system daemons, Apple jobs, and System paths cannot be changed")
	}
	if i.Kind == "tailscale" {
		return refuse("display_only", "Tailscale items are display-only; lifecycle actions are refused")
	}
	if i.Kind != "launchd" && i.Kind != "brew" {
		return refuse("unsupported_kind", "lifecycle actions require a user agent or Homebrew service")
	}
	domain := fmt.Sprintf("gui/%d", s.UID)
	if i.Domain != "" && i.Domain != domain {
		return refuse("wrong_domain", "job does not belong to the current user's gui domain")
	}
	switch action {
	case "start", "stop", "restart", "enable", "disable", "edit", "remove":
	default:
		return refuse("invalid_action", "unknown lifecycle action")
	}
	isBrew := i.Kind == "brew" || i.BrewName != ""
	if isBrew && action != "start" && action != "stop" && action != "restart" {
		return refuse("unsupported_action", "Homebrew services support start, stop, and restart only")
	}
	if opts.DryRun && destructive(action) && (!i.Registered || !i.Managed || !i.CreatedByTool) {
		r.Note = ForeignDryRunNote
	}
	if !opts.DryRun && destructive(action) {
		// Check ancestry before confirmation, and again against fresh live state before mutation.
		if err := s.CheckAncestor(ctx, i.PID); err != nil {
			return refuse("self_harm", err.Error())
		}
		if !i.Registered || !i.Managed || !i.CreatedByTool {
			if !opts.Yes {
				return refuse("foreign_confirmation", "foreign jobs require --yes and interactive confirmation by typing the label")
			}
			if !s.TTY || s.Confirm == nil {
				return refuse("tty_required", "foreign jobs require a TTY even with --yes; no action was taken")
			}
			text, err := s.Confirm(i.Label)
			if err != nil || strings.TrimSpace(text) != i.Label {
				return refuse("confirmation_mismatch", "confirmation did not match the job label; no action was taken")
			}
		}
	}
	target := domain + "/" + i.Label
	op := func(command string, args ...string) { r.Ops = append(r.Ops, Operation{Command: command, Args: args}) }
	if !opts.DryRun {
		b, err := s.Runner.Run(ctx, "launchctl", "print", target)
		if err == nil {
			state := launchd.ParsePrint(b)
			if state.State == "" {
				return refuse("state_unavailable", "cannot verify current job state; no action was taken")
			}
			i.Live, i.PID = true, state.PID
		} else {
			// A failed print may be an inspection failure, not an unloaded service.
			b, listErr := s.Runner.Run(ctx, "launchctl", "print", domain)
			if listErr != nil || !strings.Contains(string(b), "services = {") {
				return refuse("state_unavailable", "cannot verify current gui domain; no action was taken")
			}
			i.Live, i.PID = false, 0
			for _, row := range launchd.ParseDomain(b) {
				if row.Label == i.Label {
					return refuse("state_unavailable", "job is loaded but its current state is unavailable; no action was taken")
				}
			}
		}
		if destructive(action) {
			if err := s.CheckAncestor(ctx, i.PID); err != nil {
				return refuse("self_harm", err.Error())
			}
		}
	}
	if isBrew {
		name := i.BrewName
		if name == "" {
			name = strings.TrimPrefix(i.Label, "homebrew.mxcl.")
		}
		if !runner.LifecycleAllowed("brew", []string{"services", action, name}) {
			return refuse("invalid_service", "Homebrew service name is invalid")
		}
		op("brew", "services", action, name)
	} else {
		needsPlist := action == "edit" || action == "remove" || ((action == "start" || action == "restart" || action == "enable") && !i.Live)
		if needsPlist {
			if _, _, err := s.plist(i); err != nil {
				return refuse("invalid_plist", err.Error())
			}
		}
		now := time.Now()
		if s.Now != nil {
			now = s.Now()
		}
		switch action {
		case "start", "restart":
			if !i.Live {
				op("launchctl", "bootstrap", domain, i.PlistPath)
			}
			if action == "restart" {
				op("launchctl", "kickstart", "-k", target)
			} else {
				op("launchctl", "kickstart", target)
			}
		case "stop":
			if i.Live {
				op("launchctl", "bootout", target)
			}
		case "disable":
			op("launchctl", "disable", target)
			if i.Live {
				op("launchctl", "bootout", target)
			}
		case "enable":
			op("launchctl", "enable", target)
			if !i.Live {
				op("launchctl", "bootstrap", domain, i.PlistPath)
			}
		case "remove":
			if opts.PurgeLog && (!i.Registered || !i.CreatedByTool) {
				return refuse("foreign_log", "log purging requires tool-created provenance")
			}
			var purge string
			if opts.PurgeLog {
				reg, err := registry.Load(s.RegistryPath)
				if err != nil {
					return refuse("registry_unavailable", "cannot read registry for log purging")
				}
				for _, entry := range reg.Entries {
					if entry.ID == i.ID {
						purge = entry.Log
					}
				}
				if purge == "" || !filepath.IsAbs(purge) || purge == i.PlistPath || purge == s.RegistryPath {
					return refuse("invalid_log", "a distinct absolute registry log path is required")
				}
				for _, entry := range reg.Entries {
					if entry.ID != i.ID && entry.Log == purge {
						return refuse("shared_log", "log is shared with another registry entry")
					}
				}
				if info, err := os.Lstat(purge); err == nil && !info.Mode().IsRegular() {
					return refuse("invalid_log", "log must be a regular file")
				} else if err != nil && !os.IsNotExist(err) {
					return refuse("invalid_log", "cannot inspect log")
				}
			}
			if i.Live {
				op("launchctl", "bootout", target)
			}
			trash := filepath.Join(s.Home, ".config", "macjobs", "trash", i.Label+"-"+now.UTC().Format("20060102T150405.000000000Z")+".plist")
			op("mkdir", "0700", filepath.Dir(trash))
			op("move", i.PlistPath, trash)
			if purge != "" {
				op("purge-log", purge)
			}
			if i.Registered {
				op("registry-remove", s.RegistryPath, i.ID)
			}
		case "edit":
			if len(s.Editor) == 0 {
				return refuse("editor_required", "set EDITOR to an executable with optional arguments")
			}
			if filepath.Base(s.Editor[0]) == "sudo" {
				return refuse("protected_editor", "privilege escalation is refused")
			}
			tmp := filepath.Join(filepath.Dir(i.PlistPath), ".macjobs-edit-"+i.Label+"-"+strconv.FormatInt(now.UnixNano(), 10)+".plist")
			op("copy", i.PlistPath, tmp)
			op(s.Editor[0], append(append([]string{}, s.Editor[1:]...), tmp)...)
			op("plutil", "-lint", tmp)
			op("atomic-replace", tmp, i.PlistPath)
			r.Ops[len(r.Ops)-1].When = "changed and valid"
			op("launchctl", "bootout", target)
			r.Ops[len(r.Ops)-1].When = "changed and valid and loaded"
			op("launchctl", "bootstrap", domain, i.PlistPath)
			r.Ops[len(r.Ops)-1].When = "changed and valid"
			op("remove-temp", tmp)
		}
	}
	if opts.DryRun {
		return r, nil
	}
	if action == "edit" {
		return s.edit(ctx, i, r)
	}
	for _, operation := range r.Ops {
		if err := s.run(ctx, operation); err != nil {
			r.Result = "failed"
			r.Error = &ActionError{Code: "operation_failed", Message: err.Error()}
			return r, r.Error
		}
	}
	r.Result = "completed"
	if len(r.Ops) == 0 {
		r.Result = "unchanged"
	}
	return r, nil
}
func (s Service) run(ctx context.Context, op Operation) error {
	switch op.Command {
	case "purge-log":
		if err := os.Remove(op.Args[0]); err != nil && !os.IsNotExist(err) {
			return errors.New("cannot purge log")
		}
	case "mkdir":
		if err := os.MkdirAll(op.Args[1], 0700); err != nil {
			return errors.New("cannot create private trash directory")
		}
	case "move":
		// Link then unlink prevents replacement of an existing recovery file.
		if err := os.Link(op.Args[0], op.Args[1]); err != nil {
			return errors.New("cannot move plist to trash; original retained")
		}
		if err := os.Remove(op.Args[0]); err != nil {
			_ = os.Remove(op.Args[1])
			return errors.New("cannot retire original plist")
		}
	case "registry-remove":
		unlock, err := registry.Lock(op.Args[0])
		if err != nil {
			return err
		}
		defer unlock()
		reg, err := registry.Load(op.Args[0])
		if err != nil {
			return err
		}
		entries := []registry.Entry{}
		for _, e := range reg.Entries {
			if e.ID != op.Args[1] {
				entries = append(entries, e)
			}
		}
		reg.Entries = entries
		if err := registry.Save(op.Args[0], reg, true); err != nil {
			return err
		}
	default:
		if _, err := s.Runner.Run(ctx, op.Command, op.Args...); err != nil {
			return errors.New("external operation failed; subprocess output suppressed")
		}
	}
	return nil
}
