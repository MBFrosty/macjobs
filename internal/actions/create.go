package actions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MBFrosty/macjobs/internal/launchd"
	"github.com/MBFrosty/macjobs/internal/model"
	jobplist "github.com/MBFrosty/macjobs/internal/plist"
	"github.com/MBFrosty/macjobs/internal/redact"
	"github.com/MBFrosty/macjobs/internal/registry"
	"github.com/MBFrosty/macjobs/internal/templates"
	codec "howett.net/plist"
)

type CreateOptions struct {
	Tags                         []string
	Name, Owner, Purpose, Binary string
	Template                     templates.Options
	DryRun                       bool
}
type CreateReport struct {
	Report
	Plist string `json:"plist"`
}

func (s Service) Create(ctx context.Context, o CreateOptions) (CreateReport, error) {
	r := CreateReport{Report: Report{SchemaVersion: model.SchemaVersion, Action: "create", Kind: "launchd", DryRun: o.DryRun, Result: "planned", Ops: []Operation{}}}
	fail := func(code, message string) (CreateReport, error) {
		err := Failure(&r.Report, code, message)
		return r, err
	}
	tags, tagErr := registry.NormalizeTags(o.Tags)
	if tagErr != nil {
		return fail("invalid_tags", tagErr.Error())
	}
	r.Tags = tags
	if !safeLabel.MatchString(o.Name) {
		return fail("invalid_name", "name must contain only letters, digits, dots, underscores, or hyphens")
	}
	if !o.DryRun {
		unlock, err := registry.Lock(s.RegistryPath)
		if err != nil {
			return fail("registry_busy", err.Error())
		}
		defer unlock()
	}
	reg, err := registry.Load(s.RegistryPath)
	if err != nil {
		return fail("registry_unavailable", err.Error())
	}
	label := reg.ManagedLabelPrefix() + o.Name
	r.ID, r.Label = label, label
	if strings.HasPrefix(label, "com.apple.") || s.UID <= 0 {
		return fail("protected_job", "creation requires a non-Apple label and a user gui domain")
	}
	for _, entry := range reg.Entries {
		if entry.ID == label {
			return fail("already_exists", "registry label already exists")
		}
	}
	path := filepath.Join(s.Home, "Library", "LaunchAgents", label+".plist")
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return fail("already_exists", "plist already exists or cannot be inspected")
	}
	if o.Template.Log == "" {
		o.Template.Log = filepath.Join(s.Home, "Library", "Logs", "macjobs", o.Name+".log")
	}
	if strings.HasPrefix(o.Template.Log, "~/") {
		o.Template.Log = filepath.Join(s.Home, o.Template.Log[2:])
	}
	if !filepath.IsAbs(o.Template.Log) || filepath.Clean(o.Template.Log) == path || filepath.Clean(o.Template.Log) == filepath.Clean(s.RegistryPath) {
		return fail("invalid_log", "log requires a distinct absolute path")
	}
	if strings.HasPrefix(o.Template.Probes.LogPath, "~/") {
		o.Template.Probes.LogPath = filepath.Join(s.Home, o.Template.Probes.LogPath[2:])
	}
	o.Template.Label, o.Template.Binary = label, o.Binary
	b, err := templates.Render(o.Template)
	if err != nil {
		return fail("invalid_template", err.Error())
	}
	decoded, _ := jobplist.Decode(b)
	redacted, _ := jobplist.Encode((redact.Redactor{}).Value(decoded, false).(map[string]any), codec.XMLFormat)
	r.Plist = string(redacted)
	domain := fmt.Sprintf("gui/%d", s.UID)
	target := domain + "/" + label
	r.Ops = []Operation{{Command: "write-plist", Args: []string{path}}, {Command: "plutil", Args: []string{"-lint", path}}, {Command: "registry-add", Args: []string{s.RegistryPath, label}}, {Command: "launchctl", Args: []string{"enable", target}}, {Command: "launchctl", Args: []string{"bootstrap", domain, path}}}
	if o.DryRun {
		return r, nil
	}
	// Verify absence in an available domain rather than treating a failed print as absence.
	bDomain, err := s.Runner.Run(ctx, "launchctl", "print", domain)
	if err != nil || !strings.Contains(string(bDomain), "services = {") {
		return fail("state_unavailable", "cannot verify user domain; no changes made")
	}
	for _, row := range launchd.ParseDomain(bDomain) {
		if row.Label == label {
			return fail("already_exists", "launchd label already exists")
		}
	}
	bDisabled, err := s.Runner.Run(ctx, "launchctl", "print-disabled", domain)
	if err != nil {
		return fail("state_unavailable", "cannot verify disabled overrides; no changes made")
	}
	wasDisabled := launchd.ParseDisabled(bDisabled)[label]
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fail("write_failed", "cannot create LaunchAgents directory")
	}
	if resolved, err := filepath.EvalSymlinks(filepath.Dir(path)); err != nil || resolved != filepath.Dir(path) {
		return fail("invalid_path", "LaunchAgents directory must not use symbolic links")
	}
	if err := os.MkdirAll(filepath.Dir(o.Template.Log), 0700); err != nil {
		return fail("write_failed", "cannot create log directory")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fail("write_failed", "cannot create plist without overwriting")
	}
	_, writeErr := f.Write(b)
	syncErr := f.Sync()
	closeErr := f.Close()
	registered, enabled := false, false
	original := reg
	_, originalErr := os.Lstat(s.RegistryPath)
	rollback := func(message string) (CreateReport, error) {
		cleanupOK := true
		if enabled && wasDisabled {
			if _, e := s.Runner.Run(ctx, "launchctl", "disable", target); e != nil {
				cleanupOK = false
			}
		}
		if registered {
			var e error
			if os.IsNotExist(originalErr) {
				e = os.Remove(s.RegistryPath)
			} else {
				e = registry.Save(s.RegistryPath, original, true)
			}
			if e != nil {
				cleanupOK = false
			}
		}
		if e := os.Remove(path); e != nil {
			cleanupOK = false
		}
		if cleanupOK {
			message += "; plist and registry entry rolled back"
		} else {
			message += "; rollback incomplete, inspect the job and registry"
		}
		r.Result = "failed"
		r.Error = &ActionError{Code: "create_failed", Message: message}
		return r, r.Error
	}
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return rollback("cannot persist plist")
	}
	if _, err := s.Runner.Run(ctx, "plutil", "-lint", path); err != nil {
		return rollback("plist validation failed")
	}
	owner := o.Owner
	if strings.TrimSpace(owner) == "" {
		owner = "unknown"
	}
	reg.Entries = append(reg.Entries, registry.Entry{ID: label, Kind: "launchd", Tags: tags, Owner: owner, Purpose: o.Purpose, Managed: true, CreatedByTool: true, CreatedBy: "macjobs", Template: o.Template.Template, Log: o.Template.Log})
	if err := registry.Save(s.RegistryPath, reg, true); err != nil {
		return rollback("cannot save registry")
	}
	registered = true
	if _, err := s.Runner.Run(ctx, "launchctl", "enable", target); err != nil {
		return rollback("cannot enable user agent")
	}
	enabled = true
	if _, err := s.Runner.Run(ctx, "launchctl", "bootstrap", domain, path); err != nil {
		return rollback("cannot bootstrap user agent")
	}
	r.Result = "completed"
	return r, nil
}

type AdoptOptions struct {
	Tags              []string
	Owner, Purpose    string
	DryRun, AsCreated bool
}

func (s Service) Adopt(ctx context.Context, i model.Item, o AdoptOptions) (Report, error) {
	r := Report{SchemaVersion: model.SchemaVersion, Action: "adopt", ID: i.ID, Label: i.Label, Kind: i.Kind, DryRun: o.DryRun, Result: "planned", Ops: []Operation{}}
	fail := func(code, message string) (Report, error) { err := Failure(&r, code, message); return r, err }
	tags, tagErr := registry.NormalizeTags(o.Tags)
	if tagErr != nil {
		return fail("invalid_tags", tagErr.Error())
	}
	if s.protected(i) || !safeLabel.MatchString(i.Label) || i.ID != i.Label || i.Kind != "launchd" || i.BrewName != "" || (i.Domain != "" && i.Domain != fmt.Sprintf("gui/%d", s.UID)) {
		return fail("protected_job", "adoption requires a non-Apple user agent in the current gui domain")
	}
	if _, _, err := s.plist(i); err != nil {
		return fail("invalid_plist", err.Error())
	}
	if !o.DryRun {
		unlock, err := registry.Lock(s.RegistryPath)
		if err != nil {
			return fail("registry_busy", err.Error())
		}
		defer unlock()
	}
	reg, err := registry.Load(s.RegistryPath)
	if err != nil {
		return fail("registry_unavailable", err.Error())
	}
	index := -1
	for n, entry := range reg.Entries {
		if entry.ID == i.ID {
			index = n
		}
	}
	entry := registry.Entry{ID: i.ID, Kind: "launchd", Owner: "unknown"}
	if index >= 0 {
		entry = reg.Entries[index]
	}
	entry.Tags, _ = registry.NormalizeTags(append(append([]string(nil), entry.Tags...), tags...))
	r.Tags = entry.Tags
	entry.Managed = true
	if o.AsCreated {
		entry.CreatedByTool = true
	}
	if o.Owner != "" {
		entry.Owner = o.Owner
	}
	if o.Purpose != "" {
		entry.Purpose = o.Purpose
	}
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	entry.AdoptedAt = now.Format(time.RFC3339)
	if index >= 0 {
		reg.Entries[index] = entry
	} else {
		reg.Entries = append(reg.Entries, entry)
	}
	r.Ops = append(r.Ops, Operation{Command: "registry-adopt", Args: []string{s.RegistryPath, i.ID}})
	if o.DryRun {
		return r, nil
	}
	if err := registry.Save(s.RegistryPath, reg, true); err != nil {
		r.Result = "failed"
		r.Error = &ActionError{Code: "adopt_failed", Message: errors.New("cannot save adoption metadata").Error()}
		return r, r.Error
	}
	r.Result = "completed"
	return r, nil
}
