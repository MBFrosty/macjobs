package actions

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/MBFrosty/macjobs/internal/launchd"
	"github.com/MBFrosty/macjobs/internal/model"
	jobplist "github.com/MBFrosty/macjobs/internal/plist"
)

func (s Service) edit(ctx context.Context, i model.Item, r Report) (Report, error) {
	fail := func(code, message string) (Report, error) {
		r.Result = "failed"
		r.Error = &ActionError{Code: code, Message: message}
		return r, r.Error
	}
	original, info, err := s.plist(i)
	if err != nil {
		return fail("invalid_plist", err.Error())
	}
	tmp := r.Ops[0].Args[1]
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fail("edit_failed", "cannot create private edit copy")
	}
	defer os.Remove(tmp)
	_, err = f.Write(original)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return fail("edit_failed", "cannot write edit copy")
	}
	editor := r.Ops[1]
	if _, err := s.Runner.Run(ctx, editor.Command, editor.Args...); err != nil {
		return fail("editor_failed", "editor failed; original plist was retained")
	}
	tmpInfo, err := os.Lstat(tmp)
	if err != nil || !tmpInfo.Mode().IsRegular() {
		return fail("invalid_edit", "edited copy must remain a regular file; original plist was retained")
	}
	if _, err := s.Runner.Run(ctx, "plutil", "-lint", tmp); err != nil {
		return fail("invalid_edit", "plist validation failed; original plist was retained")
	}
	edited, err := os.ReadFile(tmp)
	if err != nil {
		return fail("edit_failed", "cannot read edit copy; original plist was retained")
	}
	p, err := jobplist.Decode(edited)
	if err != nil || jobplist.String(p, "Label") != i.Label {
		return fail("invalid_edit", "edited plist must keep the original label; original plist was retained")
	}
	if bytes.Equal(original, edited) {
		r.Result = "unchanged"
		return r, nil
	}
	// An editor can stay open for hours: inspect current state and ancestry again before reload.
	target := r.Ops[len(r.Ops)-2].Args[1] + "/" + i.Label
	b, err := s.Runner.Run(ctx, "launchctl", "print", target)
	if err == nil {
		state := launchd.ParsePrint(b)
		if state.State == "" {
			return fail("state_unavailable", "cannot verify job state before reload; original plist was retained")
		}
		if err := s.CheckAncestor(ctx, state.PID); err != nil {
			return fail("self_harm", err.Error())
		}
		i.Live = true
	} else {
		b, err := s.Runner.Run(ctx, "launchctl", "print", r.Ops[len(r.Ops)-2].Args[1])
		if err != nil || !bytes.Contains(b, []byte("services = {")) {
			return fail("state_unavailable", "cannot verify gui domain before reload; original plist was retained")
		}
		for _, row := range launchd.ParseDomain(b) {
			if row.Label == i.Label {
				return fail("state_unavailable", "job details unavailable before reload; original plist was retained")
			}
		}
		i.Live = false
	}
	current, currentInfo, err := s.plist(i)
	if err != nil || !os.SameFile(info, currentInfo) || !bytes.Equal(original, current) {
		return fail("concurrent_edit", "original plist changed while editing; edit copy was not published")
	}
	// Sync the replacement before the atomic rename; preserve the original mode.
	f, err = os.OpenFile(tmp, os.O_WRONLY, 0)
	if err != nil {
		return fail("edit_failed", "cannot open validated edit copy")
	}
	err = f.Chmod(info.Mode().Perm())
	if err == nil {
		err = f.Sync()
	}
	closeErr = f.Close()
	if err != nil || closeErr != nil {
		return fail("edit_failed", "cannot persist validated edit copy")
	}
	if err := os.Rename(tmp, i.PlistPath); err != nil {
		return fail("edit_failed", "cannot replace original plist")
	}
	if i.Live {
		if err := s.run(ctx, Operation{Command: "launchctl", Args: []string{"bootout", target}}); err != nil {
			return fail("reload_failed", "plist was replaced but bootout failed; inspect the job before retrying")
		}
	}
	domain := r.Ops[len(r.Ops)-2].Args[1]
	if err := s.run(ctx, Operation{Command: "launchctl", Args: []string{"bootstrap", domain, i.PlistPath}}); err != nil {
		return fail("reload_failed", "plist was replaced but bootstrap failed; inspect the job before retrying")
	}
	r.Result = "completed"
	return r, nil
}

// ParseEditor accepts an executable and quoted arguments, without shell expansion.
func ParseEditor(value string) ([]string, error) {
	var args []string
	var word []rune
	var quote rune
	escaped, started := false, false
	for _, c := range value {
		if escaped {
			word = append(word, c)
			escaped = false
			started = true
			continue
		}
		if c == '\\' && quote != '\'' {
			escaped = true
			started = true
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				word = append(word, c)
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			started = true
			continue
		}
		if c == ' ' || c == '\t' || c == '\n' {
			if started {
				args = append(args, string(word))
				word = nil
				started = false
			}
			continue
		}
		word = append(word, c)
		started = true
	}
	if quote != 0 || escaped {
		return nil, errors.New("EDITOR contains an unfinished quote or escape")
	}
	if started {
		args = append(args, string(word))
	}
	if len(args) == 0 || args[0] == "" {
		return nil, errors.New("set EDITOR to an executable with optional arguments")
	}
	if filepath.Base(args[0]) == "sudo" {
		return nil, errors.New("privilege escalation is refused")
	}
	return args, nil
}
