package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"vigil/internal/core"
	"vigil/internal/storage"
	"vigil/internal/tui"
)

func NewCommand() *cobra.Command {
	var database string
	var stateDir string
	root := &cobra.Command{
		Use:           "vigil",
		Short:         "A control panel for development agents",
		Version:       "0.1.0-dev",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&database, "db", ":memory:", "SQLite database path (defaults to a temporary in-memory database)")
	root.PersistentFlags().StringVar(&stateDir, "state-dir", "", "Private application state directory (defaults to XDG_STATE_HOME/vigil)")
	root.AddCommand(projectCommand(&stateDir), resourceCommand(&stateDir), doctorCommand())
	root.AddCommand(spikeCommand())
	root.AddCommand(&cobra.Command{
		Use:   "hello",
		Short: "Print a greeting and verify SQLite",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			version, err := storage.Version(cmd.Context(), database)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Hello, world!\nVigil is ready.\nSQLite %s connected.\n", version)
			return err
		},
	})
	root.AddCommand(&cobra.Command{
		Use:   "dashboard PROJECT_ID",
		Short: "Inspect persisted readiness, tasks, inbox and history",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withProject(cmd, &stateDir, args[0], func(e *core.Engine) error {
				return tui.RunProject(cmd.Context(), e, cmd.InOrStdin(), cmd.OutOrStdout())
			})
		},
	})
	return root
}
