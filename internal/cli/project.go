package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"vigil/internal/artifacts"
	"vigil/internal/boundary"
	"vigil/internal/coordinator"
	"vigil/internal/core"
	"vigil/internal/store"
	"vigil/internal/supervisor"
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
	root.AddCommand(&cobra.Command{Use: "discover PROJECT_ID", Short: "Inventory nested Git roots without enrolling repositories or changing branches", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := e.DiscoverRepositories(cmd.Context())
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
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
	root.AddCommand(&cobra.Command{Use: "repository PROJECT_ID REPOSITORY_ID", Short: "Inspect the latest explicit repository enrollment and baseline", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			r, err := e.Repository(cmd.Context(), args[1])
			if err != nil {
				return err
			}
			return printJSON(cmd, r)
		})
	}})
	var prepareCommand string
	var prepareRevision int
	prepare := &cobra.Command{Use: "prepare-repository PROJECT_ID REPOSITORY_ID", Short: "Journal and prepare an explicitly enrolled clean plan branch", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if prepareCommand == "" || prepareRevision < 1 {
			return errors.New("--command-id and --expected-revision required")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			r, err := e.PrepareRepository(cmd.Context(), prepareCommand, args[1], prepareRevision)
			if err != nil {
				return err
			}
			return printJSON(cmd, r)
		})
	}}
	prepare.Flags().StringVar(&prepareCommand, "command-id", "", "Unique replay-safe command identifier")
	prepare.Flags().IntVar(&prepareRevision, "expected-revision", 0, "Expected project revision")
	root.AddCommand(prepare)
	var executionCommand string
	var executionRevision int
	var executionWall int64
	var syntheticFixture bool
	executionPrepare := &cobra.Command{Use: "execution-prepare PROJECT_ID TASK_ID", Short: "Persist an execution attempt without launching it", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if !syntheticFixture {
			return errors.New("only --synthetic-fixture is available; production dispatch remains qualification-gated")
		}
		if executionCommand == "" || executionRevision < 1 {
			return errors.New("--command-id and --expected-revision required")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			r, err := supervisor.Prepare(cmd.Context(), e, supervisor.PrepareRequest{CommandID: executionCommand, ExpectedProjectRevision: executionRevision, TaskID: args[1], RuntimeKind: "synthetic", WallLimitMS: executionWall})
			if err != nil {
				return err
			}
			return printJSON(cmd, r)
		})
	}}
	executionPrepare.Flags().StringVar(&executionCommand, "command-id", "", "Unique replay-safe preparation command")
	executionPrepare.Flags().IntVar(&executionRevision, "expected-revision", 0, "Expected project revision")
	executionPrepare.Flags().Int64Var(&executionWall, "wall-limit-ms", 60000, "Absolute fixture attempt wall limit")
	executionPrepare.Flags().BoolVar(&syntheticFixture, "synthetic-fixture", false, "Use the deterministic disposable-fixture driver")
	root.AddCommand(executionPrepare)
	var fixtureRepository, fixturePath, fixtureContent, fixturePrompt, startCommand string
	executionStart := &cobra.Command{Use: "execution-start PROJECT_ID RUN_ID", Short: "Explicitly start a prepared disposable synthetic execution", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if !syntheticFixture {
			return errors.New("--synthetic-fixture required; no production or spike fallback is available")
		}
		if startCommand == "" || fixtureRepository == "" || fixturePath == "" || fixturePrompt == "" {
			return errors.New("--command-id, --repository, --path and --prompt required")
		}
		m, err := manager(cmd, stateDir)
		if err != nil {
			return err
		}
		defer m.Close()
		e, err := m.Open(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		defer e.DB.Close()
		prepared, err := supervisor.LoadPrepared(cmd.Context(), e, args[1])
		if err != nil {
			return err
		}
		if prepared.RuntimeKind != "synthetic" {
			return errors.New("this CLI build has no production execution driver")
		}
		var repositoryRoot string
		for _, repository := range prepared.Repositories {
			if repository.ID == fixtureRepository {
				repositoryRoot = repository.Root
			}
		}
		if repositoryRoot == "" {
			return errors.New("fixture repository is not enrolled in this run")
		}
		owner, err := m.Coordinator.Register(cmd.Context())
		if err != nil {
			return err
		}
		defer owner.Close()
		reservation, err := e.ReserveResources(cmd.Context(), owner, "run-resources-"+prepared.RunID, prepared.RunID, prepared.EndpointID)
		if err != nil {
			return err
		}
		driver := &supervisor.FixtureDriver{RepositoryID: fixtureRepository, Root: repositoryRoot, RelativePath: fixturePath, Content: []byte(fixtureContent)}
		runner := supervisor.Runner{Engine: e, Driver: driver, StartCommandID: startCommand}
		result, err := runner.Run(cmd.Context(), prepared, reservation, fixturePrompt)
		if err != nil {
			return err
		}
		if err := owner.FinishTicket(cmd.Context(), reservation.Ticket, "contained_stopped"); err != nil {
			return err
		}
		for _, claim := range reservation.Claims {
			if err := owner.Release(cmd.Context(), claim, "contained_stopped"); err != nil {
				return err
			}
		}
		return printJSON(cmd, map[string]any{"run": prepared.RunID, "result": result, "task_state": "checking", "accepted": false})
	}}
	executionStart.Flags().BoolVar(&syntheticFixture, "synthetic-fixture", false, "Use only the deterministic disposable-fixture driver")
	executionStart.Flags().StringVar(&startCommand, "command-id", "", "Unique replay-safe start command")
	executionStart.Flags().StringVar(&fixtureRepository, "repository", "", "Enrolled repository ID")
	executionStart.Flags().StringVar(&fixturePath, "path", "", "Repository-relative path to write")
	executionStart.Flags().StringVar(&fixtureContent, "content", "persisted fixture execution\n", "Bounded fixture file content")
	executionStart.Flags().StringVar(&fixturePrompt, "prompt", "", "Recorded bounded fixture instruction")
	root.AddCommand(executionStart)
	executionInspect := &cobra.Command{Use: "execution-inspect PROJECT_ID RUN_ID", Short: "Inspect persisted execution, uncertainty and allowed next commands", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			prepared, err := supervisor.LoadPrepared(cmd.Context(), e, args[1])
			if err != nil {
				return err
			}
			runner := supervisor.Runner{Engine: e}
			view, err := runner.Inspect(cmd.Context(), prepared)
			if err != nil {
				return err
			}
			return printJSON(cmd, view)
		})
	}}
	root.AddCommand(executionInspect)
	var reconcileRepository, reconcilePath, reconcileCommand string
	executionReconcile := &cobra.Command{Use: "execution-reconcile PROJECT_ID RUN_ID", Short: "Reconcile a disposable synthetic execution without replaying submission", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if !syntheticFixture {
			return errors.New("--synthetic-fixture required; production reconciliation needs its qualified runtime driver")
		}
		if reconcileCommand == "" || reconcileRepository == "" || reconcilePath == "" {
			return errors.New("--command-id, --repository and --path required")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			prepared, err := supervisor.LoadPrepared(cmd.Context(), e, args[1])
			if err != nil {
				return err
			}
			if prepared.RuntimeKind != "synthetic" {
				return errors.New("synthetic reconciliation cannot inspect a production runtime")
			}
			var root string
			for _, repository := range prepared.Repositories {
				if repository.ID == reconcileRepository {
					root = repository.Root
				}
			}
			if root == "" {
				return errors.New("repository is not part of the run")
			}
			driver := &supervisor.FixtureDriver{RepositoryID: reconcileRepository, Root: root, RelativePath: reconcilePath}
			runner := supervisor.Runner{Engine: e, Driver: driver, ReconcileCommandID: reconcileCommand}
			view, err := runner.Inspect(cmd.Context(), prepared)
			if err != nil {
				return err
			}
			observation := supervisor.Observation{Exists: view.Effects["runtime_create"] != "", Started: view.Effects["runtime_start"] != "", Attached: view.Effects["runtime_attach"] != "", NativeSessionID: view.NativeSessionID, NativeTurnID: view.NativeTurnID, SubmissionState: view.SubmissionState, WriterState: view.WriterState}
			path := filepath.Join(root, filepath.FromSlash(reconcilePath))
			if info, statErr := os.Lstat(path); statErr == nil && info.Mode().IsRegular() && view.SubmissionState == "delivered" {
				observation.Terminal, observation.Outcome, observation.WriterState = true, "completed", "contained_stopped"
				observation.Result, _ = json.Marshal(supervisor.Result{SchemaVersion: 1, Status: "completed", Summary: "reconciled deterministic fixture edit", ChangedPaths: []string{reconcileRepository + ":" + filepath.ToSlash(reconcilePath)}})
			}
			driver.Restore(observation)
			view, err = runner.Reconcile(cmd.Context(), prepared)
			if err != nil {
				return err
			}
			return printJSON(cmd, view)
		})
	}}
	executionReconcile.Flags().BoolVar(&syntheticFixture, "synthetic-fixture", false, "Use only the deterministic disposable-fixture reconciler")
	executionReconcile.Flags().StringVar(&reconcileCommand, "command-id", "", "Unique replay-safe reconciliation command")
	executionReconcile.Flags().StringVar(&reconcileRepository, "repository", "", "Enrolled repository ID")
	executionReconcile.Flags().StringVar(&reconcilePath, "path", "", "Expected repository-relative fixture path")
	root.AddCommand(executionReconcile)
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
