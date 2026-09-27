package cli

import (
	"errors"
	"io"
	"os"
	"strconv"

	"github.com/spf13/cobra"
	"vigil/internal/core"
	"vigil/internal/store"
)

func addFinalizationCommands(root *cobra.Command, stateDir *string) {
	var archiveCommand string
	build := &cobra.Command{Use: "archive-build PROJECT_ID PLAN_ID", Short: "Persist a verified factual archive before any optional narrative", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if archiveCommand == "" {
			return errors.New("--command-id required")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			record, err := e.BuildFactualArchive(cmd.Context(), archiveCommand, args[1])
			if err != nil {
				return err
			}
			return printJSON(cmd, record)
		})
	}}
	build.Flags().StringVar(&archiveCommand, "command-id", "", "Unique replay-safe factual archive command")
	root.AddCommand(build)

	show := &cobra.Command{Use: "archive-show PROJECT_ID PLAN_ID REVISION", Short: "Verify and inspect an exact factual archive revision", Args: cobra.ExactArgs(3), RunE: func(cmd *cobra.Command, args []string) error {
		revision, err := strconv.Atoi(args[2])
		if err != nil || revision < 1 {
			return errors.New("REVISION must be positive")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			record, manifest, err := e.Archive(cmd.Context(), args[1], revision)
			if err != nil {
				return err
			}
			return printJSON(cmd, map[string]any{"record": record, "manifest": manifest})
		})
	}}
	root.AddCommand(show)

	var narrativeFile string
	var fixture bool
	narrative := &cobra.Command{Use: "archive-narrative PROJECT_ID PLAN_ID", Short: "Record one fixture finalization result with exact manifest citations", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if !fixture || narrativeFile == "" {
			return errors.New("--synthetic-fixture and --file required; production narrative dispatch is disabled")
		}
		file, err := os.Open(narrativeFile)
		if err != nil {
			return err
		}
		defer file.Close()
		content, err := io.ReadAll(io.LimitReader(file, store.MaxDocument+1))
		if err != nil {
			return err
		}
		var request core.NarrativeResult
		if err := store.Decode(content, &request); err != nil {
			return err
		}
		if request.PlanID != args[1] {
			return errors.New("narrative plan does not match command target")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			record, err := e.RecordFixtureNarrative(cmd.Context(), request)
			if err != nil {
				return err
			}
			return printJSON(cmd, record)
		})
	}}
	narrative.Flags().StringVar(&narrativeFile, "file", "", "Closed structured narrative-result JSON, at most 64 KiB")
	narrative.Flags().BoolVar(&fixture, "synthetic-fixture", false, "Label this as disposable fixture finalization")
	root.AddCommand(narrative)

	export := &cobra.Command{Use: "archive-export PROJECT_ID PLAN_ID REVISION DESTINATION", Short: "Copy verified archive records into a new portable private directory", Args: cobra.ExactArgs(4), RunE: func(cmd *cobra.Command, args []string) error {
		revision, err := strconv.Atoi(args[2])
		if err != nil || revision < 1 {
			return errors.New("REVISION must be positive")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := e.ExportArchive(cmd.Context(), args[1], revision, args[3])
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}}
	root.AddCommand(export)

	inspect := &cobra.Command{Use: "retention-inspect PROJECT_ID", Short: "Dry-inspect completed-plan transcript expiry candidates", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			candidates, err := e.InspectTranscriptExpiry(cmd.Context(), store.Now())
			if err != nil {
				return err
			}
			return printJSON(cmd, candidates)
		})
	}}
	root.AddCommand(inspect)
	var retentionCommand string
	expire := &cobra.Command{Use: "retention-expire PROJECT_ID", Short: "Expire eligible raw transcripts after the configured completed-plan interval", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if retentionCommand == "" {
			return errors.New("--command-id required")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := e.ExpireTranscripts(cmd.Context(), retentionCommand, store.Now())
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}}
	expire.Flags().StringVar(&retentionCommand, "command-id", "", "Unique replay-safe transcript expiry command")
	root.AddCommand(expire)
}
