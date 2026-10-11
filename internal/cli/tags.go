package cli

import (
	"github.com/MBFrosty/macjobs/internal/actions"
	"github.com/MBFrosty/macjobs/internal/inventory"
	"github.com/MBFrosty/macjobs/internal/model"
	"github.com/MBFrosty/macjobs/internal/redact"
	"github.com/MBFrosty/macjobs/internal/registry"
	"github.com/spf13/cobra"
)

func addTags(root *cobra.Command, snapshot func(*cobra.Command) (inventory.Result, error), resolve func() (string, error), jsonOut *bool, factory ActionFactory) {
	for _, action := range []string{"tag", "untag"} {
		var dryRun bool
		cmd := &cobra.Command{Use: action + " <id> <tag>...", Short: "Update tags in registry metadata only", RunE: func(cmd *cobra.Command, args []string) error {
			report := actions.Report{SchemaVersion: model.SchemaVersion, Action: cmd.Name(), DryRun: dryRun, Result: "refused", Ops: []actions.Operation{}}
			if len(args) > 0 {
				report.ID = args[0]
			}
			red := redact.Redactor{}
			emit := func(err error) error {
				if e := WriteJSON(cmd.OutOrStdout(), report, red); e != nil {
					return e
				}
				if err != nil {
					return ErrActionReported
				}
				return nil
			}
			fail := func(code, message string) error { return emit(actions.Failure(&report, code, message)) }
			if len(args) < 2 {
				return fail("invalid_arguments", "a job ID and at least one tag are required")
			}
			tags, err := registry.NormalizeTags(args[1:])
			if err != nil {
				return fail("invalid_tags", err.Error())
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
			service, err := factory(cmd, path)
			if err != nil {
				return fail("configuration_error", "cannot configure tag operation")
			}
			if cmd.Name() == "untag" {
				report, err = service.Untag(cmd.Context(), item, tags, dryRun)
			} else {
				report, err = service.Tag(cmd.Context(), item, tags, dryRun)
			}
			return emit(err)
		}}
		cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview registry metadata without writing files; inventory inspection is read-only")
		root.AddCommand(cmd)
	}
}
