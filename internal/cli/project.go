package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"vigil/internal/artifacts"
	"vigil/internal/boundary"
	"vigil/internal/coordinator"
	"vigil/internal/core"
	"vigil/internal/store"
)

func printJSON(cmd *cobra.Command, value any) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
func manager(cmd *cobra.Command, stateDir *string) (*core.Manager, error) {
	dir := *stateDir
	if dir == "" {
		var err error
		dir, err = core.DefaultStateDir()
		if err != nil {
			return nil, err
		}
	}
	return core.OpenManager(cmd.Context(), dir)
}
func withProject(cmd *cobra.Command, stateDir *string, id string, fn func(*core.Engine) error) error {
	m, err := manager(cmd, stateDir)
	if err != nil {
		return err
	}
	defer m.Close()
	e, err := m.Open(cmd.Context(), id)
	if err != nil {
		return err
	}
	defer e.DB.Close()
	return fn(e)
}
func projectCommand(stateDir *string) *cobra.Command {
	root := &cobra.Command{Use: "project", Short: "Persist project definitions, policy, decisions and evidence"}
	root.AddCommand(&cobra.Command{Use: "init ROOT", Short: "Register or reopen a project with private state outside its checkout", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		m, err := manager(cmd, stateDir)
		if err != nil {
			return err
		}
		defer m.Close()
		p, err := m.Init(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return printJSON(cmd, p)
	}})
	root.AddCommand(&cobra.Command{Use: "list", Short: "List registered projects", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		m, err := manager(cmd, stateDir)
		if err != nil {
			return err
		}
		defer m.Close()
		p, err := m.List(cmd.Context())
		if err != nil {
			return err
		}
		return printJSON(cmd, p)
	}})
	root.AddCommand(&cobra.Command{Use: "status PROJECT_ID", Short: "Show persisted task readiness and execution blockers", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			r, err := e.Readiness(cmd.Context())
			if err != nil {
				return err
			}
			return printJSON(cmd, r)
		})
	}})
	root.AddCommand(&cobra.Command{Use: "reservation PROJECT_ID OPERATION_ID", Short: "Inspect a persisted core resource reservation without acquiring or releasing anything", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			r, err := e.Reservation(cmd.Context(), args[1])
			if err != nil {
				return err
			}
			return printJSON(cmd, r)
		})
	}})
	var file string
	apply := &cobra.Command{Use: "apply PROJECT_ID --file COMMAND.json", Short: "Apply one versioned human command atomically (never launches a model)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if file == "" {
			return errors.New("--file required")
		}
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, store.MaxDocument+1))
		if err != nil {
			return err
		}
		var input core.Envelope
		if err = store.Decode(b, &input); err != nil {
			return err
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			r, err := e.Apply(cmd.Context(), core.Human, input)
			if err != nil {
				return err
			}
			return printJSON(cmd, r)
		})
	}}
	apply.Flags().StringVar(&file, "file", "", "Closed JSON command envelope; no credential values")
	root.AddCommand(apply)
	root.AddCommand(&cobra.Command{Use: "inbox PROJECT_ID", Short: "Show pending human decisions", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			r, err := e.Inbox(cmd.Context())
			if err != nil {
				return err
			}
			return printJSON(cmd, r)
		})
	}})
	var after int64
	events := &cobra.Command{Use: "events PROJECT_ID", Short: "Read up to 100 persisted events after a sequence", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			r, err := e.Events(cmd.Context(), after)
			if err != nil {
				return err
			}
			return printJSON(cmd, r)
		})
	}}
	events.Flags().Int64Var(&after, "after", 0, "Last sequence already read")
	root.AddCommand(events)
	root.AddCommand(&cobra.Command{Use: "artifacts PROJECT_ID", Short: "Inspect orphaned or corrupt artifacts without deleting evidence", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			r, err := artifacts.New(e.DB)
			if err != nil {
				return err
			}
			result, err := r.Inspect(cmd.Context())
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}})
	var commandID, kind string
	artifact := &cobra.Command{Use: "artifact PROJECT_ID FILE", Short: "Publish an explicit durable evidence file (maximum 16 MiB)", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if commandID == "" {
			return errors.New("--command-id required for replay-safe publication")
		}
		f, err := os.Open(args[1])
		if err != nil {
			return err
		}
		defer f.Close()
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			r, err := artifacts.New(e.DB)
			if err != nil {
				return err
			}
			result, err := r.Put(cmd.Context(), commandID, kind, "durable", f)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}}
	artifact.Flags().StringVar(&commandID, "command-id", "", "Unique command identifier")
	artifact.Flags().StringVar(&kind, "kind", "evidence", "Evidence kind")
	root.AddCommand(artifact)
	return root
}
func resourceCommand(stateDir *string) *cobra.Command {
	root := &cobra.Command{Use: "resources", Short: "Inspect host ownership, endpoint capacity and crash quarantine"}
	root.AddCommand(&cobra.Command{Use: "status", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		m, err := manager(cmd, stateDir)
		if err != nil {
			return err
		}
		defer m.Close()
		if err = m.Coordinator.Reap(cmd.Context()); err != nil {
			return err
		}
		result, err := m.Coordinator.Status(cmd.Context())
		if err != nil {
			return err
		}
		return printJSON(cmd, result)
	}})
	var capacity int
	var singleHost bool
	endpoint := &cobra.Command{Use: "endpoint ID URL [ALIASES...]", Short: "Register a physical inference resource and explicit URL aliases", Args: cobra.MinimumNArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if !singleHost {
			return errors.New("--single-host required: this host must be the sole Vigil capacity authority; other hosts/clients are not coordinated")
		}
		m, err := manager(cmd, stateDir)
		if err != nil {
			return err
		}
		defer m.Close()
		if err = m.Coordinator.Endpoint(cmd.Context(), args[0], args[1:], capacity, coordinator.Host()); err != nil {
			return err
		}
		return printJSON(cmd, map[string]any{"endpoint_id": args[0], "capacity": capacity, "host_authority": coordinator.Host()})
	}}
	endpoint.Flags().IntVar(&capacity, "capacity", 1, "Maximum simultaneous owned inferences")
	endpoint.Flags().BoolVar(&singleHost, "single-host", false, "Explicitly select this host as the sole capacity authority")
	root.AddCommand(endpoint)
	var observation string
	reconcile := &cobra.Command{Use: "reconcile OWNER_ID", Short: "Record explicit human recovery evidence and release a dead owner's quarantine", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		m, err := manager(cmd, stateDir)
		if err != nil {
			return err
		}
		defer m.Close()
		if err = m.Coordinator.Reconcile(cmd.Context(), args[0], observation); err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "Recovery observation recorded; dead owner's resources released.")
		return err
	}}
	reconcile.Flags().StringVar(&observation, "observation", "", "Observed proof that writers and inference stopped; required")
	root.AddCommand(reconcile)
	return root
}
func doctorCommand() *cobra.Command {
	return &cobra.Command{Use: "doctor", Short: "Check execution-boundary prerequisites without installing or launching agents", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error { return printJSON(cmd, boundary.Inspect(cmd.Context())) }}
}
