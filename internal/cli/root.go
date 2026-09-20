package cli

import (
	"fmt"

	"agent-control/internal/storage"
	"agent-control/internal/tui"
	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	var database string
	root := &cobra.Command{
		Use:           "agent-control",
		Short:         "A control panel for development agents",
		Version:       "0.1.0-dev",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&database, "db", ":memory:", "SQLite database path (defaults to a temporary in-memory database)")
	root.AddCommand(&cobra.Command{
		Use:   "hello",
		Short: "Print a greeting and verify SQLite",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			version, err := storage.Version(cmd.Context(), database)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Hello, world!\nagent-control is ready.\nSQLite %s connected.\n", version)
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
