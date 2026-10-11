package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/MBFrosty/macjobs/internal/actions"
	"github.com/MBFrosty/macjobs/internal/inventory"
	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/redact"
	"github.com/MBFrosty/macjobs/internal/runner"
	"github.com/MBFrosty/macjobs/internal/watchdog"
	"github.com/spf13/cobra"
)

type WatchdogFactory func() (watchdog.Service, error)

func DefaultWatchdog() (watchdog.Service, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return watchdog.Service{}, errors.New("cannot resolve home directory")
	}
	return watchdog.Service{Runner: runner.Watchdog{}, Home: home, Now: time.Now}, nil
}
func expandHome(path, home string) string {
	if len(path) >= 2 && path[:2] == "~/" {
		return filepath.Join(home, path[2:])
	}
	return path
}

func emitCreation(cmd *cobra.Command, v any, jsonOut bool, err error) error {
	if jsonOut {
		if e := WriteJSON(cmd.OutOrStdout(), v, redact.Redactor{}); e != nil {
			return e
		}
		if err != nil {
			return ErrActionReported
		}
		return nil
	}
	// A single redacted JSON document keeps plist XML and command arguments safely quoted.
	if e := WriteJSON(cmd.OutOrStdout(), v, redact.Redactor{}); e != nil {
		return e
	}
	if err != nil {
		return ErrActionReported
	}
	return nil
}
func addCreation(root *cobra.Command, snapshot func(*cobra.Command) (inventory.Result, error), resolve func() (string, error), jsonOut *bool, factory ActionFactory, watchFactory WatchdogFactory) {
	var o actions.CreateOptions
	create := &cobra.Command{Use: "create <template> --name <name> [flags] [-- <command...>]", Short: "Create a managed user agent from a template", RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return errors.New("one template is required")
		}
		dash := cmd.ArgsLenAtDash()
		if (len(args) > 1 && dash != 1) || (len(args) == 1 && dash > 1) {
			return errors.New("command arguments must follow --")
		}
		o.Template.Template, o.Template.Command = args[0], args[1:]
		path, err := resolve()
		if err != nil {
			return err
		}
		service, err := factory(cmd, path)
		if err != nil {
			return err
		}
		binary, err := resolveSelfPath()
		if err != nil {
			return err
		}
		o.Binary = binary
		report, err := service.Create(cmd.Context(), o)
		return emitCreation(cmd, report, *jsonOut, err)
	}}
	create.Flags().StringSliceVar(&o.Tags, "tag", nil, "Registry tags; repeat or use a comma list")
	create.Flags().StringVar(&o.Name, "name", "", "Job name appended to the configured label prefix")
	create.Flags().StringVar(&o.Owner, "owner", "unknown", "Registry owner")
	create.Flags().StringVar(&o.Purpose, "purpose", "", "Registry purpose")
	create.Flags().StringVar(&o.Template.Log, "log", "", "Log path (default: ~/Library/Logs/macjobs/<name>.log)")
	create.Flags().StringVar(&o.Template.App, "app", "", "Desktop app name for app-watchdog")
	create.Flags().DurationVar(&o.Template.Every, "every", 0, "Interval, for example 10m")
	create.Flags().StringArrayVar(&o.Template.At, "at", nil, "Calendar time: Sun 03:00 or 03:00; repeat for multiple times")
	probeFlags(create, &o.Template.Probes, &o.Template.RestartOnDisconnect)
	create.Flags().BoolVar(&o.DryRun, "dry-run", false, "Print the plist and operations without executing commands or writing files")
	root.AddCommand(create)
	var adoptOptions actions.AdoptOptions
	adopt := &cobra.Command{Use: "adopt <label>", Short: "Record management metadata for an existing user agent", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		path, err := resolve()
		if err != nil {
			return err
		}
		service, err := factory(cmd, path)
		if err != nil {
			return err
		}
		var item model.Item
		if adoptOptions.DryRun {
			// Read the user plist directly so a preview executes no external commands.
			item = model.Item{ID: args[0], Label: args[0], Kind: "launchd", Domain: fmt.Sprintf("gui/%d", service.UID)}
			for _, dir := range []string{filepath.Join(service.Home, "Library", "LaunchAgents"), "/Library/LaunchAgents"} {
				candidate := filepath.Join(dir, args[0]+".plist")
				if _, err := os.Lstat(candidate); err == nil {
					item.PlistPath = candidate
					break
				}
			}
		} else {
			result, err := snapshot(cmd)
			if err != nil {
				return err
			}
			item, err = Find(result.Report, args[0])
			if err != nil {
				return err
			}
		}
		report, err := service.Adopt(cmd.Context(), item, adoptOptions)
		// Adoption output contains metadata operations only; inventory values stay private.
		return emitCreation(cmd, report, *jsonOut, err)
	}}
	adopt.Flags().StringSliceVar(&adoptOptions.Tags, "tag", nil, "Merge registry tags; repeat or use a comma list")
	adopt.Flags().StringVar(&adoptOptions.Owner, "owner", "", "Set registry owner")
	adopt.Flags().StringVar(&adoptOptions.Purpose, "purpose", "", "Set registry purpose")
	adopt.Flags().BoolVar(&adoptOptions.DryRun, "dry-run", false, "Preview registry changes without writing")
	adopt.Flags().BoolVar(&adoptOptions.AsCreated, "as-created", false, "Assert tool-created provenance, bypassing foreign-job confirmation later")
	root.AddCommand(adopt)
	var checkOptions watchdog.Options
	watch := &cobra.Command{Use: "watchdog", Short: "Check desktop app liveness"}
	check := &cobra.Command{Use: "check", Short: "Log one app check and rate-limit reopening a missing app", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		service, err := watchFactory()
		if err != nil {
			return err
		}
		checkOptions.Log = expandHome(checkOptions.Log, service.Home)
		checkOptions.State = expandHome(checkOptions.State, service.Home)
		checkOptions.Probes.LogPath = expandHome(checkOptions.Probes.LogPath, service.Home)
		result, err := service.Check(cmd.Context(), checkOptions)
		if *jsonOut {
			return emitCreation(cmd, result, true, err)
		}
		// The log receives the only line; launchd stdout may point to that same file.
		if err != nil {
			return fmt.Errorf("watchdog check failed: %s", err.Error())
		}
		return nil
	}}
	probeFlags(check, &checkOptions.Probes, &checkOptions.RestartOnDisconnect)
	check.Flags().DurationVar(&checkOptions.Interval, "interval", 10*time.Minute, "Check interval and default probe max-age")
	check.Flags().StringVar(&checkOptions.App, "app", "", "Desktop app name")
	check.Flags().StringVar(&checkOptions.Log, "log", "", "Absolute log path")
	check.Flags().StringVar(&checkOptions.State, "state", "", "State path (default: ~/.config/macjobs/state/<app>.json)")
	check.Flags().DurationVar(&checkOptions.RelaunchInterval, "relaunch-interval", 10*time.Minute, "Minimum time between reopen attempts")
	watch.AddCommand(check)
	root.AddCommand(watch)
}

func probeFlags(cmd *cobra.Command, p *watchdog.ProbeOptions, restart *bool) {
	cmd.Flags().StringArrayVar(&p.TCPPeers, "tcp-peer", nil, "Allowed relay host:port; repeat for alternatives; port may be * (requires fresh app-level evidence)")
	cmd.Flags().StringVar(&p.LogPath, "probe-log", "", "App connection log path with RFC 3339 timestamped state lines")
	cmd.Flags().StringVar(&p.ConnectedRegex, "connected-regex", "", "Regex matching app connected/heartbeat log lines")
	cmd.Flags().StringVar(&p.DisconnectedRegex, "disconnected-regex", "", "Regex matching app disconnected log lines")
	cmd.Flags().StringVar(&p.HTTPURL, "probe-http", "", "Loopback http status URL returning connected boolean and RFC 3339 timestamp")
	cmd.Flags().DurationVar(&p.MaxAge, "max-age", 0, "Maximum age of app-level evidence (default: check interval)")
	cmd.Flags().BoolVar(restart, "restart-on-disconnect", false, "Opt in to rate-limited clean quit/reopen only after connected=no")
}
