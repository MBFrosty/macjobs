package actions

import (
	"context"
	"strings"

	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/registry"
)

func (s Service) Tag(ctx context.Context, item model.Item, tags []string, dryRun bool) (Report, error) {
	return s.updateTags(ctx, item, tags, dryRun, false)
}

func (s Service) Untag(ctx context.Context, item model.Item, tags []string, dryRun bool) (Report, error) {
	return s.updateTags(ctx, item, tags, dryRun, true)
}

// Tags change registry metadata only; they never grant lifecycle authority.
func (s Service) updateTags(ctx context.Context, i model.Item, tags []string, dryRun, remove bool) (Report, error) {
	action := "tag"
	if remove {
		action = "untag"
	}
	r := Report{SchemaVersion: model.SchemaVersion, Action: action, ID: i.ID, Label: i.Label, Kind: i.Kind, DryRun: dryRun, Result: "planned", Ops: []Operation{}}
	fail := func(code, message string) (Report, error) { err := Failure(&r, code, message); return r, err }
	normalized, err := registry.NormalizeTags(tags)
	if err != nil {
		return fail("invalid_tags", err.Error())
	}
	if len(normalized) == 0 {
		return fail("invalid_tags", "at least one tag is required")
	}
	if strings.HasPrefix(i.ID, "com.apple.") || strings.HasPrefix(i.ID, "system:com.apple.") || strings.HasPrefix(i.Label, "com.apple.") {
		return fail("protected_job", "Apple jobs cannot be tagged")
	}
	if strings.TrimSpace(i.ID) == "" {
		return fail("invalid_id", "a job ID is required")
	}
	switch i.Kind {
	case "launchd", "system", "brew", "tailscale", "process", "cron":
	default:
		return fail("unsupported_kind", "unknown job kind")
	}
	if err := ctx.Err(); err != nil {
		return fail("cancelled", "tag operation cancelled")
	}
	if !dryRun {
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
	entry := registry.Entry{ID: i.ID, Kind: i.Kind, Owner: "unknown"}
	for n, candidate := range reg.Entries {
		if candidate.ID == i.ID {
			index, entry = n, candidate
			break
		}
	}
	before := strings.Join(entry.Tags, ",")
	if remove {
		removed := map[string]bool{}
		for _, tag := range normalized {
			removed[tag] = true
		}
		remaining := []string{}
		for _, tag := range entry.Tags {
			if !removed[tag] {
				remaining = append(remaining, tag)
			}
		}
		entry.Tags = remaining
	} else {
		entry.Tags, _ = registry.NormalizeTags(append(append([]string(nil), entry.Tags...), normalized...))
	}
	r.Tags = entry.Tags
	if before == strings.Join(entry.Tags, ",") {
		r.Result = "unchanged"
		return r, nil
	}
	r.Ops = append(r.Ops, Operation{Command: "registry-" + action, Args: append([]string{s.RegistryPath, i.ID}, normalized...)})
	if dryRun {
		return r, nil
	}
	if index >= 0 {
		reg.Entries[index] = entry
	} else {
		reg.Entries = append(reg.Entries, entry)
	}
	if err := registry.Save(s.RegistryPath, reg, true); err != nil {
		r.Result = "failed"
		r.Error = &ActionError{Code: "tag_write_failed", Message: "cannot save tag metadata"}
		return r, r.Error
	}
	r.Result = "completed"
	return r, nil
}
