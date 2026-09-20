package cli

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"vigil/internal/spike"
)

func spikeCommand() *cobra.Command {
	var opt spike.Options
	cmd := &cobra.Command{Use: "spike", Short: "Probe a prepared harness or explicitly run its disposable fixture", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		opt.Output = cmd.OutOrStdout()
		_, err := spike.Run(ctx, opt)
		return err
	}}
	cmd.Flags().StringVar(&opt.Manifest, "manifest", "", "Prepared launch.json path")
	cmd.Flags().StringVar(&opt.Harness, "harness", "", "Harness: codex or hermes")
	cmd.Flags().BoolVar(&opt.Live, "live", false, "Start one model-backed fixture turn (default: metadata only)")
	cmd.Flags().StringVar(&opt.KeyFile, "llama-key-file", "", "Private local API key file; otherwise use VIGIL_LLAMA_API_KEY or matching OPENAI exports")
	cmd.Flags().StringVar(&opt.Scenario, "scenario", "", "Lifecycle experiment: resume, interrupt, child, loss, clarify, approval-allow, approval-deny")
	_ = cmd.MarkFlagRequired("manifest")
	_ = cmd.MarkFlagRequired("harness")
	return cmd
}
