package cli

import (
	"fmt"

	"github.com/spf13/cobra"
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
		Use:   "dashboard",
		Short: "Open the hello-world terminal dashboard",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return tui.Run(cmd.Context(), database, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	})
	return root
}
