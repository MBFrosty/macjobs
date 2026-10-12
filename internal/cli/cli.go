package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/MBFrosty/macjobs/internal/inventory"
	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/redact"
	"github.com/MBFrosty/macjobs/internal/registry"
	"github.com/spf13/cobra"
)

var ErrUnhealthy = errors.New("health checks failed")

type Collect func(context.Context, registry.Registry, bool) (inventory.Result, error)

func New(version string, collect Collect) *cobra.Command {
	return NewWithActions(version, collect, DefaultActions)
}
func NewWithActions(version string, collect Collect, factory ActionFactory) *cobra.Command {
	return NewWithServices(version, collect, factory, DefaultWatchdog)
}
func NewWithServices(version string, collect Collect, factory ActionFactory, watchdogFactory WatchdogFactory) *cobra.Command {
	var jsonOut, all bool
	var path string
	root := &cobra.Command{Use: "macjobs", Short: "Inspect and manage macOS user background jobs", Version: version, SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().BoolVar(&jsonOut, "json", false, "Output schemaVersion 1 JSON")
	root.PersistentFlags().BoolVar(&all, "all", false, "Include Apple jobs")
	root.PersistentFlags().StringVar(&path, "registry", "", "Registry file (default: ~/.config/macjobs/registry.yaml)")
	resolve := func() (string, error) {
		if path != "" {
			return path, nil
		}
		return registry.DefaultPath()
	}
	snapshot := func(cmd *cobra.Command) (inventory.Result, error) {
		p, e := resolve()
		if e != nil {
			return inventory.Result{}, e
		}
		r, e := registry.Load(p)
		if e != nil {
			return inventory.Result{}, e
		}
		return collect(cmd.Context(), r, all)
	}
	write := func(cmd *cobra.Command, v any, r redact.Redactor) error { return WriteJSON(cmd.OutOrStdout(), v, r) }
	var tags []string
	var ports []int
	var kind, owner string
	var unhealthy bool
	list := &cobra.Command{Use: "list", Short: "List jobs, routes, and selected processes", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		normalized, e := registry.NormalizeTags(tags)
		if e != nil {
			return e
		}
		if e := validatePorts(ports); e != nil {
			return e
		}
		if e := validateKind(kind); e != nil {
			return e
		}
		result, e := snapshot(cmd)
		if e != nil {
			return e
		}
		report := Filter(result.Report, kind, owner, unhealthy, normalized, ports)
		if jsonOut {
			return write(cmd, report, result.Redactor)
		}
		return Table(cmd.OutOrStdout(), report, result.Redactor)
	}}
	list.Flags().StringVar(&kind, "kind", "", "Filter: launchd, system, brew, tailscale, process")
	list.Flags().StringVar(&owner, "owner", "", "Filter by registry owner")
	list.Flags().BoolVar(&unhealthy, "unhealthy", false, "Show warn, fail, and unknown health")
	list.Flags().StringSliceVar(&tags, "tag", nil, "Filter by any tag; repeat or use a comma list")
	list.Flags().IntSliceVar(&ports, "port", nil, "Filter by any observed or expected port; repeat or use a comma list")
	root.AddCommand(list)
	var unattributed bool
	portsCmd := &cobra.Command{Use: "ports", Short: "List TCP listeners and unconnected UDP sockets", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		result, e := snapshot(cmd)
		if e != nil {
			return e
		}
		listeners := result.Listeners
		if unattributed {
			filtered := []model.Listener{}
			for _, l := range listeners {
				if l.JobID == "" {
					filtered = append(filtered, l)
				}
			}
			listeners = filtered
		}
		if jsonOut {
			return write(cmd, portsReport{SchemaVersion: model.SchemaVersion, Listeners: listeners, Warnings: result.Report.Warnings}, result.Redactor)
		}
		result.Listeners = listeners
		return writePorts(cmd.OutOrStdout(), result, false, result.Redactor)
	}}
	portsCmd.Flags().BoolVar(&unattributed, "unattributed", false, "Show only listeners with no known job")
	root.AddCommand(portsCmd)
	var showEnv bool
	show := &cobra.Command{Use: "show <id>", Short: "Show live state and redacted plist metadata", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		result, e := snapshot(cmd)
		if e != nil {
			return e
		}
		item, e := Find(result.Report, args[0])
		if e != nil {
			return e
		}
		r := result.Redactor
		// Environment values remain private in all output.

		payload := struct {
			SchemaVersion int        `json:"schemaVersion"`
			Item          model.Item `json:"item"`
			Plist         any        `json:"plist"`
			Warnings      []string   `json:"warnings"`
		}{model.SchemaVersion, item, r.Value(item.Plist, false), result.Report.Warnings}
		// JSON is also the human-readable detail format.
		return write(cmd, payload, r)
	}}
	show.Flags().BoolVar(&showEnv, "show-env", false, "Deprecated: environment values are always redacted")
	root.AddCommand(show)
	doctor := &cobra.Command{Use: "doctor", Short: "Report health findings; never fix jobs", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		result, e := snapshot(cmd)
		if e != nil {
			return e
		}
		if jsonOut {
			e = write(cmd, result.Report, result.Redactor)
		} else {
			e = Table(cmd.OutOrStdout(), result.Report, result.Redactor)
		}
		if e != nil {
			return e
		}
		for _, item := range result.Report.Items {
			if item.Health == "fail" {
				return ErrUnhealthy
			}
		}
		return nil
	}}
	root.AddCommand(doctor)
	var follow bool
	var count int
	logs := &cobra.Command{Use: "logs <id>", Short: "Read configured logs with credentials redacted", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if count < 0 {
			return errors.New("line count must be nonnegative")
		}
		result, e := snapshot(cmd)
		if e != nil {
			return e
		}
		item, e := Find(result.Report, args[0])
		if e != nil {
			return e
		}
		if len(item.Logs) == 0 {
			return errors.New("job has no configured logs")
		}
		return Logs(cmd.Context(), cmd.OutOrStdout(), item, count, follow, jsonOut, result.LogRedactor)
	}}
	logs.Flags().BoolVarP(&follow, "follow", "f", false, "Follow appends, truncation, and rotation")
	logs.Flags().IntVarP(&count, "lines", "n", 50, "Initial lines per log")
	root.AddCommand(logs)
	var force bool
	reg := &cobra.Command{Use: "registry", Short: "Manage local metadata only"}
	init := &cobra.Command{Use: "init", Short: "Discover live non-Apple user jobs into an unmanaged registry", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		p, e := resolve()
		if e != nil {
			return e
		}
		if _, e := os.Lstat(p); e == nil && !force {
			return errors.New("registry already exists; use --force to replace it")
		} else if e != nil && !os.IsNotExist(e) {
			return errors.New("cannot inspect registry")
		}
		result, e := collect(cmd.Context(), registry.Empty(), false)
		if e != nil {
			return e
		}
		r := registry.Empty()
		for _, i := range result.Report.Items {
			if i.Kind == "launchd" && i.Live && !strings.HasPrefix(i.Label, "com.apple.") {
				r.Entries = append(r.Entries, registry.Entry{ID: i.ID, Kind: "launchd", Owner: "unknown"})
			}
		}
		for _, w := range result.Report.Warnings {
			if w == "launchd domain unavailable" {
				return errors.New("user jobs unavailable; registry was not written")
			}
		}
		if e := registry.Save(p, r, force); e != nil {
			return e
		}
		if jsonOut {
			return write(cmd, r, result.Redactor)
		}
		_, e = fmt.Fprintf(cmd.OutOrStdout(), "Wrote %d unmanaged registry entries.\n", len(r.Entries))
		return e
	}}
	init.Flags().BoolVar(&force, "force", false, "Atomically replace an existing registry")
	reg.AddCommand(init)
	root.AddCommand(reg)
	addTags(root, snapshot, resolve, &jsonOut, factory)
	addActions(root, snapshot, resolve, &jsonOut, factory)
	addCreation(root, snapshot, resolve, &jsonOut, factory, watchdogFactory)
	return root
}
func Live(ctx context.Context, r registry.Registry, all bool) (inventory.Result, error) {
	c, e := inventory.New()
	if e != nil {
		return inventory.Result{}, e
	}
	return c.Collect(ctx, r, all), nil
}
func validatePorts(ports []int) error {
	for _, port := range ports {
		if port < 1 || port > 65535 {
			return errors.New("port must be between 1 and 65535")
		}
	}
	return nil
}

func Filter(r model.Report, kind, owner string, unhealthy bool, tags []string, ports []int) model.Report {
	out := model.NewReport()
	out.Warnings = r.Warnings
	for _, i := range r.Items {
		if kind != "" && i.Kind != kind {
			continue
		}
		if owner != "" && i.Owner != owner {
			continue
		}
		if unhealthy && i.Health == "ok" {
			continue
		}
		if len(tags) > 0 {
			matched := false
			for _, wanted := range tags {
				for _, tag := range i.Tags {
					if tag == wanted {
						matched = true
					}
				}
			}
			if !matched {
				continue
			}
		}
		if len(ports) > 0 {
			matched := false
			for _, port := range ports {
				if itemHasPort(i, port) {
					matched = true
				}
			}
			if !matched {
				continue
			}
		}
		out.Items = append(out.Items, i)
	}
	return out
}
func Find(r model.Report, id string) (model.Item, error) {
	for _, i := range r.Items {
		if i.ID == id {
			return i, nil
		}
	}
	return model.Item{}, errors.New("job ID not found (use --all for Apple jobs)")
}
func WriteJSON(w io.Writer, v any, r redact.Redactor) error {
	b, e := json.Marshal(v)
	if e != nil {
		return errors.New("cannot encode output")
	}
	var decoded any
	if e = json.Unmarshal(b, &decoded); e != nil {
		return e
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r.Value(decoded, true))
}
func Table(w io.Writer, r model.Report, red redact.Redactor) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, e := fmt.Fprintln(tw, "ID\tKIND\tOWNER\tTAGS\tSTATE\tPID\tPORTS\tSCHEDULE\tLAST RUN*\tEXIT\tHEALTH\tPURPOSE / FINDINGS"); e != nil {
		return e
	}
	for _, i := range r.Items {
		last := "-"
		if i.LastRun != nil {
			last = i.LastRun.Format(time.RFC3339)
		}
		exit := "-"
		if i.LastExit != nil {
			exit = fmt.Sprint(*i.LastExit)
		}
		if i.Signal != "" {
			exit = i.Signal
		}
		note := i.Purpose
		if len(i.Notes) > 0 {
			note += " " + strings.Join(i.Notes, "; ")
		}
		tagText := "-"
		if len(i.Tags) > 0 {
			tagText = strings.Join(i.Tags, ",")
		}
		values := []string{i.ID, i.Kind, i.Owner, tagText, i.State, fmt.Sprint(i.PID), FormatPorts(i.Ports), i.Schedule, last, exit, i.Health, note}
		for j, v := range values {
			values[j] = strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(red.Text(v))
		}
		if _, e := fmt.Fprintln(tw, strings.Join(values, "\t")); e != nil {
			return e
		}
	}
	if e := tw.Flush(); e != nil {
		return e
	}
	if _, e := fmt.Fprintln(w, "* Last run is inferred from log modification time."); e != nil {
		return e
	}
	for _, warning := range r.Warnings {
		if _, e := fmt.Fprintln(w, "Warning:", red.Text(warning)); e != nil {
			return e
		}
	}
	return nil
}

// ValidKinds are the values accepted by list --kind.
var ValidKinds = []string{"launchd", "system", "brew", "tailscale", "process"}

func validateKind(kind string) error {
	if kind == "" {
		return nil
	}
	for _, k := range ValidKinds {
		if kind == k {
			return nil
		}
	}
	return fmt.Errorf("unknown --kind %q; valid kinds: %s", kind, strings.Join(ValidKinds, ", "))
}
