package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/MBFrosty/macjobs/internal/actions"
	"github.com/MBFrosty/macjobs/internal/inventory"
	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/redact"
	"github.com/MBFrosty/macjobs/internal/runner"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var ErrActionReported = errors.New("action error already reported")

type ActionFactory func(*cobra.Command, string) (actions.Service, error)

func DefaultActions(cmd *cobra.Command, path string) (actions.Service, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return actions.Service{}, errors.New("cannot resolve home directory")
	}
	editor, editorErr := actions.ParseEditor(os.Getenv("EDITOR"))
	if cmd.Name() == "edit" && editorErr != nil {
		return actions.Service{}, editorErr
	}
	input := cmd.InOrStdin()
	f, ok := input.(*os.File)
	tty := ok && term.IsTerminal(int(f.Fd()))
	return actions.Service{Runner: runner.Lifecycle{Editor: editor, Input: input}, Editor: editor, Home: home, UID: os.Getuid(), PID: os.Getpid(), RegistryPath: path, Now: time.Now, TTY: tty, Confirm: func(label string) (string, error) {
		// The prompt uses stderr so JSON stdout remains one complete document.
		if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "Type %s to confirm: ", redact.Redactor{}.Text(label)); err != nil {
			return "", err
		}
		return bufio.NewReader(input).ReadString('\n')
	}}, nil
}
func addActions(root *cobra.Command, snapshot func(*cobra.Command) (inventory.Result, error), resolve func() (string, error), jsonOut *bool, factory ActionFactory) {
	for _, action := range []string{"start", "stop", "restart", "enable", "disable", "edit", "remove"} {
		var dryRun, yes, purgeLog bool
		cmd := &cobra.Command{Use: action + " <id>", Short: strings.ToUpper(action[:1]) + action[1:] + " a user agent", RunE: func(cmd *cobra.Command, args []string) error {
			report := actions.Report{SchemaVersion: model.SchemaVersion, Action: cmd.Name(), DryRun: dryRun, Ops: []actions.Operation{}, Result: "refused"}
			if len(args) > 0 {
				report.ID = args[0]
			}
			red := redact.Redactor{}
			emit := func(err error) error {
				if *jsonOut {
					if e := WriteJSON(cmd.OutOrStdout(), report, red); e != nil {
						return e
					}
					if err != nil {
						return ErrActionReported
					}
					return nil
				}
				for _, op := range report.Ops {
					words := []string{op.Command}
					for _, arg := range op.Args {
						words = append(words, strconv.Quote(arg))
					}
					text := strings.Join(words, " ")
					if op.When != "" {
						text += " (" + op.When + ")"
					}
					if _, e := fmt.Fprintln(cmd.OutOrStdout(), red.Text(text)); e != nil {
						return e
					}
				}
				if err != nil {
					return errors.New(red.Text(err.Error()))
				}
				if report.Note != "" {
					if _, e := fmt.Fprintln(cmd.OutOrStdout(), "note: "+report.Note); e != nil {
						return e
					}
				}
				_, e := fmt.Fprintln(cmd.OutOrStdout(), report.Result)
				return e
			}
			fail := func(code, message string) error { return emit(actions.Failure(&report, code, message)) }
			if len(args) != 1 {
				return fail("invalid_arguments", "exactly one job ID is required")
			}
			path, err := resolve()
			if err != nil {
				return fail("registry_unavailable", "cannot resolve registry")
			}
			result, err := snapshot(cmd)
			if err != nil {
				return fail("inventory_unavailable", "cannot inspect jobs or registry")
			}
			red = result.LogRedactor
			for _, item := range result.Report.Items {
				red.Values = append(red.Values, redact.AllEnvironmentValues(item.Plist)...)
			}
			item, err := Find(result.Report, args[0])
			if err != nil {
				return fail("not_found", "job ID not found; use list to find its exact ID")
			}
			report.Label, report.Kind = item.Label, item.Kind
			if item.Kind == "cron" || strings.HasPrefix(item.ID, "cron:") {
				return fail("display_only", "cron items are read-only in macjobs")
			}
			service, err := factory(cmd, path)
			if err != nil {
				return fail("configuration_error", err.Error())
			}
			report, err = service.Execute(cmd.Context(), cmd.Name(), item, actions.Options{DryRun: dryRun, Yes: yes, PurgeLog: purgeLog})
			return emit(err)
		}}
		cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show operations without executing commands or changing files")
		cmd.Flags().BoolVar(&yes, "yes", false, "Allow typed TTY confirmation for a foreign job")
		if action == "remove" {
			cmd.Flags().BoolVar(&purgeLog, "purge-log", false, "Delete the tool-created job's unshared registry log")
		}
		root.AddCommand(cmd)
	}
}
