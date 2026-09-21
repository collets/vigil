package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"vigil/internal/artifacts"
	"vigil/internal/boundary"
	"vigil/internal/checkpoint"
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
	for _, action := range []string{"pause", "continue"} {
		action := action
		var controlCommand string
		var controlRevision int
		control := &cobra.Command{Use: action + " PROJECT_ID", Short: map[string]string{"pause": "Durably prohibit future dispatch", "continue": "Explicitly recheck and re-enable dispatch"}[action], Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if controlCommand == "" || controlRevision < 1 {
				return errors.New("--command-id and --expected-revision required")
			}
			return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
				var result core.DispatchControl
				var err error
				if action == "pause" {
					result, err = e.Pause(cmd.Context(), controlCommand, controlRevision)
				} else {
					result, err = e.Continue(cmd.Context(), controlCommand, controlRevision)
				}
				if err != nil {
					return err
				}
				return printJSON(cmd, result)
			})
		}}
		control.Flags().StringVar(&controlCommand, "command-id", "", "Unique replay-safe control command")
		control.Flags().IntVar(&controlRevision, "expected-revision", 0, "Expected project revision")
		root.AddCommand(control)
	}
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
		if startCommand == "" || fixtureRepository == "" || fixturePath == "" {
			return errors.New("--command-id, --repository and --path required")
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
		if prepared.ExactResume && fixturePrompt != "" {
			return errors.New("exact resume refuses --prompt because it must not submit replacement instructions")
		}
		if !prepared.ExactResume && fixturePrompt == "" {
			return errors.New("--prompt required for a new synthetic attempt")
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
		runner := supervisor.Runner{Engine: e, Owner: owner, Driver: driver, StartCommandID: startCommand}
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
	var stopCommand, stopRepository, stopPath string
	var stopInterruptMS, stopTerminateMS int64
	executionStop := &cobra.Command{Use: "execution-stop PROJECT_ID RUN_ID", Short: "Durably stop a disposable execution and retire its requests", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if !syntheticFixture {
			return errors.New("--synthetic-fixture required; production stop needs its qualified runtime driver")
		}
		if stopCommand == "" || stopRepository == "" || stopPath == "" {
			return errors.New("--command-id, --repository and --path required")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			prepared, err := supervisor.LoadPrepared(cmd.Context(), e, args[1])
			if err != nil {
				return err
			}
			if prepared.RuntimeKind != "synthetic" {
				return errors.New("synthetic stop cannot control a production runtime")
			}
			var repositoryRoot string
			for _, repository := range prepared.Repositories {
				if repository.ID == stopRepository {
					repositoryRoot = repository.Root
				}
			}
			if repositoryRoot == "" {
				return errors.New("repository is not part of the run")
			}
			driver := &supervisor.FixtureDriver{RepositoryID: stopRepository, Root: repositoryRoot, RelativePath: stopPath}
			runner := supervisor.Runner{Engine: e, Driver: driver}
			receipt, err := runner.Stop(cmd.Context(), prepared, stopCommand, time.Duration(stopInterruptMS)*time.Millisecond, time.Duration(stopTerminateMS)*time.Millisecond)
			if err != nil {
				return err
			}
			return printJSON(cmd, receipt)
		})
	}}
	executionStop.Flags().BoolVar(&syntheticFixture, "synthetic-fixture", false, "Use only the deterministic disposable-fixture controller")
	executionStop.Flags().StringVar(&stopCommand, "command-id", "", "Unique replay-safe stop command")
	executionStop.Flags().StringVar(&stopRepository, "repository", "", "Enrolled repository ID")
	executionStop.Flags().StringVar(&stopPath, "path", "", "Repository-relative fixture path")
	executionStop.Flags().Int64Var(&stopInterruptMS, "interrupt-grace-ms", 100, "Bounded native interrupt grace")
	executionStop.Flags().Int64Var(&stopTerminateMS, "terminate-grace-ms", 5000, "Bounded boundary termination grace")
	root.AddCommand(executionStop)
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

	var recoveryCommand, recoveryMode, historyState, historyClass string
	var recoveryRevision int
	var historyAutomatic bool
	recoveryChoose := &cobra.Command{Use: "execution-recovery-choose PROJECT_ID RUN_ID", Short: "Record an explicit synthetic exact-resume, reconstruction, or blocked recovery choice", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if !syntheticFixture {
			return errors.New("--synthetic-fixture required; production history inspection is not implemented")
		}
		if recoveryCommand == "" || recoveryRevision < 1 || recoveryMode == "" {
			return errors.New("--command-id, --expected-revision and --mode required")
		}
		if historyState != "readable" && historyState != "missing" && historyState != "corrupt" && historyState != "unsupported" {
			return errors.New("--history-state must be readable, missing, corrupt, or unsupported")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			prepared, err := supervisor.LoadPrepared(cmd.Context(), e, args[1])
			if err != nil {
				return err
			}
			if prepared.RuntimeKind != "synthetic" {
				return errors.New("synthetic history evidence cannot qualify a production run")
			}
			history := supervisor.HistoryObservation{State: historyState, RecoveryClass: historyClass, AutomaticWork: historyAutomatic, Qualification: "synthetic"}
			if historyState == "readable" {
				if err := e.DB.SQL.QueryRowContext(cmd.Context(), `SELECT native_home_ref,coalesce(durable_id,''),profile_digest,workspace_identity,generation FROM sessions WHERE run_id=? ORDER BY rowid DESC LIMIT 1`, args[1]).Scan(&history.NativeHomeRef, &history.NativeSessionID, &history.ProfileDigest, &history.WorkspaceIdentity, &history.TransportGeneration); err != nil {
					return fmt.Errorf("readable synthetic history requires a recorded native session: %w", err)
				}
			}
			checkpointManager, err := checkpoint.NewManager(e)
			if err != nil {
				return err
			}
			verifier := supervisor.CheckpointVerifierFunc(func(ctx context.Context, checkpointID string) error {
				_, verifyErr := checkpointManager.VerifySet(ctx, checkpointID)
				return verifyErr
			})
			runner := supervisor.Runner{Engine: e}
			receipt, err := runner.ChooseRecovery(cmd.Context(), supervisor.RecoveryChoiceRequest{CommandID: recoveryCommand, ExpectedRevision: recoveryRevision, RunID: args[1], Mode: recoveryMode}, supervisor.SyntheticHistoryInspector{Observation: history, Verifier: verifier})
			if err != nil {
				return err
			}
			return printJSON(cmd, receipt)
		})
	}}
	recoveryChoose.Flags().BoolVar(&syntheticFixture, "synthetic-fixture", false, "Use only explicit disposable-fixture history evidence")
	recoveryChoose.Flags().StringVar(&recoveryCommand, "command-id", "", "Unique replay-safe recovery choice")
	recoveryChoose.Flags().IntVar(&recoveryRevision, "expected-revision", 0, "Expected project revision")
	recoveryChoose.Flags().StringVar(&recoveryMode, "mode", "", "exact_resume, fresh_context, or remain_blocked")
	recoveryChoose.Flags().StringVar(&historyState, "history-state", "missing", "Observed synthetic native history state")
	recoveryChoose.Flags().StringVar(&historyClass, "history-class", "interrupted", "Observed synthetic recovery class")
	recoveryChoose.Flags().BoolVar(&historyAutomatic, "history-automatic-work", false, "Record automatic or queued native work")
	root.AddCommand(recoveryChoose)

	var exactCommand string
	var exactRevision int
	exactPrepare := &cobra.Command{Use: "execution-resume-prepare PROJECT_ID CHOICE_ID", Short: "Prepare one eligible exact native resume without a replacement prompt", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if exactCommand == "" || exactRevision < 1 {
			return errors.New("--command-id and --expected-revision required")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			prepared, err := (&supervisor.Runner{Engine: e}).PrepareExactResume(cmd.Context(), supervisor.ExactResumeRequest{CommandID: exactCommand, ExpectedRevision: exactRevision, ChoiceID: args[1]})
			if err != nil {
				return err
			}
			return printJSON(cmd, prepared)
		})
	}}
	exactPrepare.Flags().StringVar(&exactCommand, "command-id", "", "Unique replay-safe exact-resume preparation")
	exactPrepare.Flags().IntVar(&exactRevision, "expected-revision", 0, "Expected project revision")
	root.AddCommand(exactPrepare)

	var followupCommand, followupKind, followupChoice string
	var followupRevision int
	var followupWall int64
	followupPrepare := &cobra.Command{Use: "execution-followup-prepare PROJECT_ID SOURCE_RUN_ID", Short: "Prepare a distinct repair, infrastructure retry, or fresh-context attempt", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if followupCommand == "" || followupRevision < 1 || followupKind == "" {
			return errors.New("--command-id, --expected-revision and --kind required")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			var verifier supervisor.CheckpointVerifier
			if followupKind == "fresh_context" {
				checkpointManager, err := checkpoint.NewManager(e)
				if err != nil {
					return err
				}
				verifier = supervisor.CheckpointVerifierFunc(func(ctx context.Context, checkpointID string) error {
					_, verifyErr := checkpointManager.VerifySet(ctx, checkpointID)
					return verifyErr
				})
			}
			prepared, err := supervisor.PrepareFollowup(cmd.Context(), e, supervisor.FollowupRequest{CommandID: followupCommand, ExpectedRevision: followupRevision, SourceRunID: args[1], Kind: followupKind, ChoiceID: followupChoice, WallLimitMS: followupWall, Verifier: verifier})
			if err != nil {
				return err
			}
			return printJSON(cmd, prepared)
		})
	}}
	followupPrepare.Flags().StringVar(&followupCommand, "command-id", "", "Unique replay-safe follow-up preparation")
	followupPrepare.Flags().IntVar(&followupRevision, "expected-revision", 0, "Expected project revision")
	followupPrepare.Flags().StringVar(&followupKind, "kind", "", "repair, infrastructure, or fresh_context")
	followupPrepare.Flags().StringVar(&followupChoice, "choice-id", "", "Eligible fresh-context recovery choice")
	followupPrepare.Flags().Int64Var(&followupWall, "wall-limit-ms", 60000, "Absolute wall limit for the new attempt")
	root.AddCommand(followupPrepare)

	var checkpointCommand string
	var checkpointRevision int
	checkpointSave := &cobra.Command{Use: "checkpoint-save PROJECT_ID RUN_ID", Short: "Save and verify every participating repository without clearing work", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if checkpointCommand == "" || checkpointRevision < 1 {
			return errors.New("--command-id and --expected-revision required")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			prepared, err := supervisor.LoadPrepared(cmd.Context(), e, args[1])
			if err != nil {
				return err
			}
			var repositories []checkpoint.RepositorySpec
			for _, repository := range prepared.Repositories {
				repositories = append(repositories, checkpoint.RepositorySpec{ID: repository.ID, Root: repository.Root, Identity: repository.Identity, Exclusions: repository.Baseline.Exclusions, UntrackedScope: prepared.Task.Scope})
			}
			manager, err := checkpoint.NewManager(e)
			if err != nil {
				return err
			}
			receipt, err := manager.Save(cmd.Context(), checkpoint.SaveRequest{CommandID: checkpointCommand, ExpectedRevision: checkpointRevision, RunID: args[1], Repositories: repositories})
			if err != nil {
				return err
			}
			return printJSON(cmd, receipt)
		})
	}}
	checkpointSave.Flags().StringVar(&checkpointCommand, "command-id", "", "Unique replay-safe save command")
	checkpointSave.Flags().IntVar(&checkpointRevision, "expected-revision", 0, "Expected project revision")
	root.AddCommand(checkpointSave)

	var clearCommand string
	var clearRevision int
	checkpointClear := &cobra.Command{Use: "checkpoint-clear PROJECT_ID CHECKPOINT_ID BASELINE_CHECKPOINT_ID", Short: "Clear only result paths proven agent-owned after full-set verification", Args: cobra.ExactArgs(3), RunE: func(cmd *cobra.Command, args []string) error {
		if clearCommand == "" || clearRevision < 1 {
			return errors.New("--command-id and --expected-revision required")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			var runID string
			if err := e.DB.SQL.QueryRowContext(cmd.Context(), "SELECT run_id FROM checkpoint_sets WHERE id=?", args[1]).Scan(&runID); err != nil {
				return err
			}
			result, err := (&supervisor.Runner{Engine: e}).Result(cmd.Context(), runID)
			if err != nil {
				return errors.New("clear requires a validated execution result naming agent-owned paths")
			}
			owned := map[string][]string{}
			for _, changed := range result.ChangedPaths {
				parts := strings.SplitN(changed, ":", 2)
				if len(parts) != 2 {
					return errors.New("validated result contains an invalid changed path")
				}
				owned[parts[0]] = append(owned[parts[0]], parts[1])
			}
			manager, err := checkpoint.NewManager(e)
			if err != nil {
				return err
			}
			receipt, err := manager.Clear(cmd.Context(), checkpoint.ClearRequest{CommandID: clearCommand, ExpectedRevision: clearRevision, CheckpointID: args[1], BaselineCheckpointID: args[2], OwnedPaths: owned})
			if err != nil {
				return err
			}
			return printJSON(cmd, receipt)
		})
	}}
	checkpointClear.Flags().StringVar(&clearCommand, "command-id", "", "Unique replay-safe clear command")
	checkpointClear.Flags().IntVar(&clearRevision, "expected-revision", 0, "Expected project revision")
	root.AddCommand(checkpointClear)

	var restoreCommand string
	var restoreRevision int
	checkpointRestore := &cobra.Command{Use: "checkpoint-restore PROJECT_ID CHECKPOINT_ID BASELINE_CHECKPOINT_ID DESTINATION_CHECKPOINT_ID", Short: "Apply an explicitly approved restore bound to a verified destination snapshot", Args: cobra.ExactArgs(4), RunE: func(cmd *cobra.Command, args []string) error {
		if restoreCommand == "" || restoreRevision < 1 {
			return errors.New("--command-id and --expected-revision required")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			manager, err := checkpoint.NewManager(e)
			if err != nil {
				return err
			}
			receipt, err := manager.Restore(cmd.Context(), checkpoint.RestoreRequest{CommandID: restoreCommand, ExpectedRevision: restoreRevision, CheckpointID: args[1], BaselineCheckpointID: args[2], DestinationCheckpointID: args[3]})
			if err != nil {
				return err
			}
			return printJSON(cmd, receipt)
		})
	}}
	checkpointRestore.Flags().StringVar(&restoreCommand, "command-id", "", "Unique human approval and replay-safe restore command")
	checkpointRestore.Flags().IntVar(&restoreRevision, "expected-revision", 0, "Expected project revision")
	root.AddCommand(checkpointRestore)
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
