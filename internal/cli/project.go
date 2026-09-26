package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"vigil/internal/artifacts"
	"vigil/internal/boundary"
	"vigil/internal/checkpoint"
	"vigil/internal/checks"
	"vigil/internal/coordinator"
	"vigil/internal/core"
	"vigil/internal/mcp"
	"vigil/internal/quality"
	"vigil/internal/review"
	"vigil/internal/spike"
	"vigil/internal/store"
	"vigil/internal/supervisor"
	modeltools "vigil/internal/tools"
)

type fixtureReviewer struct{ raw []byte }

func (r fixtureReviewer) Review(context.Context, review.Manifest) ([]byte, error) {
	return append([]byte(nil), r.raw...), nil
}

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

func resolveQualityTarget(ctx context.Context, e *core.Engine, id string, planWide bool) (quality.Target, error) {
	if planWide {
		return quality.Target{Kind: "plan", PlanID: id}, nil
	}
	var planID string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT plan_id FROM tasks WHERE id=?", id).Scan(&planID); err != nil {
		return quality.Target{}, err
	}
	return quality.Target{Kind: "task", PlanID: planID, TaskID: id}, nil
}

func withRecoveryProject(cmd *cobra.Command, stateDir *string, projectID, checkpointID, commandID string, fn func(*checkpoint.Manager) error) error {
	m, err := manager(cmd, stateDir)
	if err != nil {
		return err
	}
	defer m.Close()
	e, err := m.Open(cmd.Context(), projectID)
	if err != nil {
		return err
	}
	defer e.DB.Close()
	var runID string
	if err := e.DB.SQL.QueryRowContext(cmd.Context(), "SELECT run_id FROM checkpoint_sets WHERE id=?", checkpointID).Scan(&runID); err != nil {
		return err
	}
	prepared, err := supervisor.LoadPrepared(cmd.Context(), e, runID)
	if err != nil {
		return err
	}
	owner, err := m.Coordinator.Register(cmd.Context())
	if err != nil {
		return err
	}
	defer owner.Close()
	reservation, err := e.ReserveResources(cmd.Context(), owner, "checkpoint-recovery-"+store.Digest([]byte(commandID)), runID, prepared.EndpointID)
	if err != nil {
		return err
	}
	checkpointManager, err := checkpoint.NewManager(e)
	if err == nil {
		err = checkpointManager.AuthorizeRecovery(owner, reservation)
	}
	if err == nil {
		err = fn(checkpointManager)
	}
	releaseErr := owner.FinishTicket(context.Background(), reservation.Ticket, "contained_stopped")
	if releaseErr == nil {
		for _, claim := range reservation.Claims {
			if claimErr := owner.Release(context.Background(), claim, "contained_stopped"); claimErr != nil {
				releaseErr = claimErr
				break
			}
		}
	}
	if err != nil {
		return err
	}
	return releaseErr
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
	root.AddCommand(&cobra.Command{Use: "queue-list PROJECT_ID", Short: "Show the authoritative ranked plan queue", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := e.PlanQueue(cmd.Context())
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}})
	var queueCommand string
	var queueRevision, queueRank int
	queue := &cobra.Command{Use: "queue PROJECT_ID PLAN_ID", Short: "Queue one exact accepted plan revision at an explicit rank", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if queueCommand == "" || queueRevision < 1 || queueRank < 0 {
			return errors.New("--command-id, --expected-revision and a nonnegative --rank required")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := e.QueuePlan(cmd.Context(), queueCommand, queueRevision, args[1], queueRank)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}}
	queue.Flags().StringVar(&queueCommand, "command-id", "", "Unique replay-safe queue command")
	queue.Flags().IntVar(&queueRevision, "expected-revision", 0, "Expected project revision")
	queue.Flags().IntVar(&queueRank, "rank", 0, "Nonnegative plan queue rank; ties use stable plan ID")
	root.AddCommand(queue)
	var advanceCommand string
	var advanceRevision int
	advance := &cobra.Command{Use: "advance PROJECT_ID", Short: "Explicitly activate the next queued plan and select its next eligible task", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if advanceCommand == "" || advanceRevision < 1 {
			return errors.New("--command-id and --expected-revision required")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := e.Advance(cmd.Context(), advanceCommand, advanceRevision)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}}
	advance.Flags().StringVar(&advanceCommand, "command-id", "", "Unique replay-safe scheduling command")
	advance.Flags().IntVar(&advanceRevision, "expected-revision", 0, "Expected project revision")
	root.AddCommand(advance)
	var specID, specCommand string
	var specRevision int
	specImport := &cobra.Command{Use: "spec-import PROJECT_ID MARKDOWN_PATH", Short: "Import one bounded owned local Markdown file as an immutable private revision", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if specID == "" || specCommand == "" || specRevision < 1 {
			return errors.New("--id, --command-id and --expected-revision required")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := e.ImportMarkdown(cmd.Context(), specCommand, specRevision, specID, args[1])
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}}
	specImport.Flags().StringVar(&specID, "id", "", "Stable specification identifier")
	specImport.Flags().StringVar(&specCommand, "command-id", "", "Unique replay-safe import command")
	specImport.Flags().IntVar(&specRevision, "expected-revision", 0, "Expected project revision")
	root.AddCommand(specImport)
	root.AddCommand(&cobra.Command{Use: "spec-show PROJECT_ID SPEC_ID REVISION", Short: "Show the exact verified private specification revision and content", Args: cobra.ExactArgs(3), RunE: func(cmd *cobra.Command, args []string) error {
		revision, err := strconv.Atoi(args[2])
		if err != nil || revision < 1 {
			return errors.New("REVISION must be positive")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := e.Specification(cmd.Context(), args[1], revision)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}})
	var proposalFile, proposalCommand string
	var proposalExpected int
	var proposalFixture bool
	proposalCreate := &cobra.Command{Use: "proposal-create PROJECT_ID", Short: "Validate and persist a closed fixture planning proposal without approving it", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if !proposalFixture {
			return errors.New("--synthetic-fixture required; live planning remains qualification-gated")
		}
		if proposalFile == "" || proposalCommand == "" || proposalExpected < 1 {
			return errors.New("--file, --command-id and --expected-revision required")
		}
		f, err := os.Open(proposalFile)
		if err != nil {
			return err
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, store.MaxDocument+1))
		if err != nil {
			return err
		}
		var request core.ProposalRequest
		if err = store.Decode(b, &request); err != nil {
			return err
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := e.CreateFixtureProposal(cmd.Context(), proposalCommand, proposalExpected, request)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}}
	proposalCreate.Flags().StringVar(&proposalFile, "file", "", "Closed proposal JSON file, at most 64 KiB")
	proposalCreate.Flags().StringVar(&proposalCommand, "command-id", "", "Unique replay-safe proposal command")
	proposalCreate.Flags().IntVar(&proposalExpected, "expected-revision", 0, "Expected project revision")
	proposalCreate.Flags().BoolVar(&proposalFixture, "synthetic-fixture", false, "Use deterministic offline proposal input; no model call")
	root.AddCommand(proposalCreate)
	var planningManifest, planningKeyFile, planningCommand, planningProposal, planningPlan, planningSpec, planningProfile string
	var planningExpected, planningSpecRevision, planningProfileRevision int
	var planningActiveMS int64
	var planningLiveLocal bool
	planningRun := &cobra.Command{Use: "planning-run PROJECT_ID", Short: "Run one receipt-backed bounded proposal turn through the prepared local Hermes route", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if !planningLiveLocal {
			return errors.New("--live-local required; planning inference is never implicit")
		}
		if planningManifest == "" || planningCommand == "" || planningProposal == "" || planningPlan == "" || planningSpec == "" || planningProfile == "" || planningExpected < 1 || planningSpecRevision < 1 || planningProfileRevision < 1 || planningActiveMS < 1 || planningActiveMS > core.MaxPlanningAttempt.Milliseconds() {
			return errors.New("manifest, exact identities/revisions and active-limit-ms (1..300000) required")
		}
		provider, err := spike.NewPlanningProvider(cmd.Context(), planningManifest, planningKeyFile)
		if err != nil {
			return err
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := e.RunPlanning(cmd.Context(), core.PlanningRunRequest{CommandID: planningCommand, ExpectedRevision: planningExpected, ProposalID: planningProposal, ExpectedPlanID: planningPlan, SpecificationID: planningSpec, SpecificationRevision: planningSpecRevision, ProfileID: planningProfile, ProfileRevision: planningProfileRevision, ActiveLimit: time.Duration(planningActiveMS) * time.Millisecond}, provider)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}}
	planningRun.Flags().BoolVar(&planningLiveLocal, "live-local", false, "Authorize one local llama turn through the prepared Hermes fixture route")
	planningRun.Flags().StringVar(&planningManifest, "manifest", "", "Prepared qualification manifest")
	planningRun.Flags().StringVar(&planningKeyFile, "key-file", "", "Optional private local llama key file (mode 600)")
	planningRun.Flags().StringVar(&planningCommand, "command-id", "", "Unique replay-safe planning command")
	planningRun.Flags().IntVar(&planningExpected, "expected-revision", 0, "Expected project revision")
	planningRun.Flags().StringVar(&planningProposal, "proposal-id", "", "Stable proposal identifier")
	planningRun.Flags().StringVar(&planningPlan, "plan-id", "", "Server-selected expected plan identifier")
	planningRun.Flags().StringVar(&planningSpec, "spec-id", "", "Exact immutable specification identifier")
	planningRun.Flags().IntVar(&planningSpecRevision, "spec-revision", 0, "Exact immutable specification revision")
	planningRun.Flags().StringVar(&planningProfile, "profile-id", "", "Explicit eligible planning profile identifier")
	planningRun.Flags().IntVar(&planningProfileRevision, "profile-revision", 0, "Explicit current planning profile revision")
	planningRun.Flags().Int64Var(&planningActiveMS, "active-limit-ms", core.MaxPlanningAttempt.Milliseconds(), "Active planning cap, maximum 300000ms")
	root.AddCommand(planningRun)
	var planningReconcileCommand string
	planningReconcile := &cobra.Command{Use: "planning-reconcile PROJECT_ID ATTEMPT_ID", Short: "Explicitly charge a crashed planning attempt as unknown", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if planningReconcileCommand == "" {
			return errors.New("--command-id required")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			if err := e.ReconcilePlanningAttempt(cmd.Context(), planningReconcileCommand, args[1]); err != nil {
				return err
			}
			return printJSON(cmd, map[string]string{"attempt_id": args[1], "state": "unknown"})
		})
	}}
	planningReconcile.Flags().StringVar(&planningReconcileCommand, "command-id", "", "Unique replay-safe human recovery command")
	root.AddCommand(planningReconcile)
	root.AddCommand(&cobra.Command{Use: "tool-server PROJECT_ID SESSION_ID", Short: "Serve bounded MCP tools for one pre-opened injected session over stdio", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			return (&mcp.Server{Handler: &modeltools.Handler{Engine: e}, SessionID: args[1]}).Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
		})
	}})
	var toolQualificationManifest, toolQualificationCommand string
	var toolQualificationLive bool
	toolQualification := &cobra.Command{Use: "tool-qualify PROJECT_ID", Short: "Run one bounded native Hermes MCP isolation qualification", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if !toolQualificationLive || toolQualificationManifest == "" || toolQualificationCommand == "" {
			return errors.New("--live-local, --manifest and --command-id required")
		}
		dir := *stateDir
		if dir == "" {
			var err error
			dir, err = core.DefaultStateDir()
			if err != nil {
				return err
			}
		}
		binary, err := os.Executable()
		if err != nil {
			return err
		}
		binary, err = filepath.Abs(binary)
		if err != nil {
			return err
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := spike.RunHermesToolQualification(cmd.Context(), spike.ToolQualificationRequest{Engine: e, StateDir: dir, VigilBinary: binary, Manifest: toolQualificationManifest, CommandID: toolQualificationCommand})
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}}
	toolQualification.Flags().BoolVar(&toolQualificationLive, "live-local", false, "Authorize one local Hermes tool-integration turn")
	toolQualification.Flags().StringVar(&toolQualificationManifest, "manifest", "", "Prepared Hermes qualification manifest")
	toolQualification.Flags().StringVar(&toolQualificationCommand, "command-id", "", "Unique injected tool-session command")
	root.AddCommand(toolQualification)
	root.AddCommand(&cobra.Command{Use: "proposal-show PROJECT_ID PROPOSAL_ID REVISION", Short: "Show one exact immutable planning proposal revision", Args: cobra.ExactArgs(3), RunE: func(cmd *cobra.Command, args []string) error {
		revision, err := strconv.Atoi(args[2])
		if err != nil || revision < 1 {
			return errors.New("REVISION must be positive")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := e.Proposal(cmd.Context(), args[1], revision)
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}})
	var applyProposalCommand string
	var applyProposalExpected int
	var authorizeCriteria bool
	proposalApply := &cobra.Command{Use: "proposal-apply PROJECT_ID PROPOSAL_ID REVISION", Short: "Human-apply one exact proposal revision atomically", Args: cobra.ExactArgs(3), RunE: func(cmd *cobra.Command, args []string) error {
		revision, err := strconv.Atoi(args[2])
		if err != nil || revision < 1 {
			return errors.New("REVISION must be positive")
		}
		if applyProposalCommand == "" || applyProposalExpected < 1 {
			return errors.New("--command-id and --expected-revision required")
		}
		payload, _ := json.Marshal(map[string]any{"proposal_id": args[1], "proposal_revision": revision, "authorize_criteria_changes": authorizeCriteria})
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			result, err := e.Apply(cmd.Context(), core.Human, core.Envelope{CommandID: applyProposalCommand, ExpectedRevision: applyProposalExpected, Kind: "planning.proposal.apply", Payload: payload})
			if err != nil {
				return err
			}
			return printJSON(cmd, result)
		})
	}}
	proposalApply.Flags().StringVar(&applyProposalCommand, "command-id", "", "Unique replay-safe human approval command")
	proposalApply.Flags().IntVar(&applyProposalExpected, "expected-revision", 0, "Expected project revision")
	proposalApply.Flags().BoolVar(&authorizeCriteria, "authorize-criteria-changes", false, "Explicitly authorize criteria changes in this exact proposal")
	root.AddCommand(proposalApply)
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
			verifier := supervisor.CheckpointVerifierPair{
				Verify: func(ctx context.Context, checkpointID string) error {
					_, verifyErr := checkpointManager.VerifySet(ctx, checkpointID)
					return verifyErr
				},
				VerifyCurrent: checkpointManager.VerifyCheckpointCurrent,
			}
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
		return withRecoveryProject(cmd, stateDir, args[0], args[1], clearCommand, func(manager *checkpoint.Manager) error {
			receipt, err := manager.Clear(cmd.Context(), checkpoint.ClearRequest{CommandID: clearCommand, ExpectedRevision: clearRevision, CheckpointID: args[1], BaselineCheckpointID: args[2]})
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
		return withRecoveryProject(cmd, stateDir, args[0], args[1], restoreCommand, func(manager *checkpoint.Manager) error {
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
	var qualityCheckCommand string
	var qualityPlanWide, qualityFixture bool
	qualityCheck := &cobra.Command{Use: "quality-check PROJECT_ID TARGET_ID CHECK_ID", Short: "Run one approved check in an isolated disposable-fixture copy", Args: cobra.ExactArgs(3), RunE: func(cmd *cobra.Command, args []string) error {
		if !qualityFixture || qualityCheckCommand == "" {
			return errors.New("--synthetic-fixture and --command-id required; production dispatch remains disabled")
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
		target, err := resolveQualityTarget(cmd.Context(), e, args[1], qualityPlanWide)
		if err != nil {
			return err
		}
		owner, err := m.Coordinator.Register(cmd.Context())
		if err != nil {
			return err
		}
		defer owner.Close()
		result, err := (&checks.Runner{Engine: e, Owner: owner}).Run(cmd.Context(), checks.Request{CommandID: qualityCheckCommand, Target: target, CheckID: args[2], Actor: "fixture"})
		if err != nil {
			return err
		}
		return printJSON(cmd, result)
	}}
	qualityCheck.Flags().StringVar(&qualityCheckCommand, "command-id", "", "Unique replay-safe check command")
	qualityCheck.Flags().BoolVar(&qualityPlanWide, "plan-wide", false, "TARGET_ID is a plan rather than a task")
	qualityCheck.Flags().BoolVar(&qualityFixture, "synthetic-fixture", false, "Require marked disposable repositories and the fixture boundary")
	root.AddCommand(qualityCheck)

	var reviewCommand, reviewFile, reviewSession, reviewNative string
	var reviewPlanWide, reviewFixture bool
	qualityReview := &cobra.Command{Use: "quality-review PROJECT_ID TARGET_ID", Short: "Record a fresh closed-schema fixture review through the read-only quality path", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if !reviewFixture || reviewCommand == "" || reviewFile == "" || reviewSession == "" || reviewNative == "" {
			return errors.New("--synthetic-fixture, --command-id, --result, --session-id and --native-identity required")
		}
		raw, err := os.ReadFile(reviewFile)
		if err != nil {
			return err
		}
		if len(raw) > store.MaxDocument {
			return errors.New("review result exceeds 64 KiB")
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
		target, err := resolveQualityTarget(cmd.Context(), e, args[1], reviewPlanWide)
		if err != nil {
			return err
		}
		owner, err := m.Coordinator.Register(cmd.Context())
		if err != nil {
			return err
		}
		defer owner.Close()
		result, err := (&review.Runner{Engine: e, Owner: owner, Reviewer: fixtureReviewer{raw: raw}}).Run(cmd.Context(), review.Request{CommandID: reviewCommand, Target: target, Actor: "fixture", SessionID: reviewSession, NativeIdentity: reviewNative})
		if err != nil {
			return err
		}
		return printJSON(cmd, result)
	}}
	qualityReview.Flags().StringVar(&reviewCommand, "command-id", "", "Unique replay-safe review command")
	qualityReview.Flags().StringVar(&reviewFile, "result", "", "Closed JSON review result file")
	qualityReview.Flags().StringVar(&reviewSession, "session-id", "", "Fresh fixture reviewer session identity")
	qualityReview.Flags().StringVar(&reviewNative, "native-identity", "", "Fresh distinct native reviewer identity")
	qualityReview.Flags().BoolVar(&reviewPlanWide, "plan-wide", false, "TARGET_ID is a plan rather than a task")
	qualityReview.Flags().BoolVar(&reviewFixture, "synthetic-fixture", false, "Use only the labeled fixture reviewer")
	root.AddCommand(qualityReview)

	var manualCommand, manualState, manualEvaluator, manualNotes string
	var manualPlanWide, manualFixture bool
	qualityManual := &cobra.Command{Use: "quality-manual PROJECT_ID TARGET_ID CRITERION_ID", Short: "Record a fingerprint-bound manual outcome", Args: cobra.ExactArgs(3), RunE: func(cmd *cobra.Command, args []string) error {
		if !manualFixture || manualCommand == "" {
			return errors.New("--synthetic-fixture and --command-id required in this offline build")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			target, err := resolveQualityTarget(cmd.Context(), e, args[1], manualPlanWide)
			if err != nil {
				return err
			}
			id, err := quality.RecordManual(cmd.Context(), e, quality.ManualRequest{CommandID: manualCommand, Target: target, CriterionID: args[2], State: manualState, Evaluator: manualEvaluator, Notes: manualNotes, Actor: "fixture_human"})
			if err != nil {
				return err
			}
			return printJSON(cmd, map[string]string{"manual_result_id": id})
		})
	}}
	qualityManual.Flags().StringVar(&manualCommand, "command-id", "", "Unique replay-safe manual command")
	qualityManual.Flags().StringVar(&manualState, "outcome", "pending", "pending, pass, fail or cannot_verify")
	qualityManual.Flags().StringVar(&manualEvaluator, "evaluator", "", "Fixture evaluator identity")
	qualityManual.Flags().StringVar(&manualNotes, "notes", "", "Manual evidence notes")
	qualityManual.Flags().BoolVar(&manualPlanWide, "plan-wide", false, "TARGET_ID is a plan rather than a task")
	qualityManual.Flags().BoolVar(&manualFixture, "synthetic-fixture", false, "Label the action as fixture-human evidence")
	root.AddCommand(qualityManual)

	var decisionCommand, decisionAction, decisionRationale string
	var decisionPlanWide, decisionFixture bool
	qualityDecision := &cobra.Command{Use: "quality-decision PROJECT_ID TARGET_ID", Short: "Record a typed fingerprint-bound human decision", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if !decisionFixture || decisionCommand == "" {
			return errors.New("--synthetic-fixture and --command-id required in this offline build")
		}
		return withProject(cmd, stateDir, args[0], func(e *core.Engine) error {
			target, err := resolveQualityTarget(cmd.Context(), e, args[1], decisionPlanWide)
			if err != nil {
				return err
			}
			id, err := quality.RecordHumanDecision(cmd.Context(), e, quality.HumanDecisionRequest{CommandID: decisionCommand, Target: target, Action: decisionAction, Rationale: decisionRationale, Actor: "fixture_human"})
			if err != nil {
				return err
			}
			return printJSON(cmd, map[string]string{"decision_id": id})
		})
	}}
	qualityDecision.Flags().StringVar(&decisionCommand, "command-id", "", "Unique replay-safe decision command")
	qualityDecision.Flags().StringVar(&decisionAction, "action", "", "accept, request_changes, clarify or stop")
	qualityDecision.Flags().StringVar(&decisionRationale, "rationale", "", "Explicit decision rationale")
	qualityDecision.Flags().BoolVar(&decisionPlanWide, "plan-wide", false, "TARGET_ID is a plan rather than a task")
	qualityDecision.Flags().BoolVar(&decisionFixture, "synthetic-fixture", false, "Label the action as fixture-human evidence")
	root.AddCommand(qualityDecision)

	var acceptCommand string
	var acceptPlanWide, acceptFixture bool
	qualityAccept := &cobra.Command{Use: "quality-accept PROJECT_ID TARGET_ID", Short: "Atomically recheck current quality gates and accept without delivery authority", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if !acceptFixture || acceptCommand == "" {
			return errors.New("--synthetic-fixture and --command-id required in this offline build")
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
		target, err := resolveQualityTarget(cmd.Context(), e, args[1], acceptPlanWide)
		if err != nil {
			return err
		}
		owner, err := m.Coordinator.Register(cmd.Context())
		if err != nil {
			return err
		}
		defer owner.Close()
		result, err := (&quality.Acceptor{Engine: e, Owner: owner}).Accept(cmd.Context(), quality.AcceptanceRequest{CommandID: acceptCommand, Target: target, Actor: "fixture_core"})
		if err != nil {
			return err
		}
		return printJSON(cmd, result)
	}}
	qualityAccept.Flags().StringVar(&acceptCommand, "command-id", "", "Unique replay-safe acceptance command")
	qualityAccept.Flags().BoolVar(&acceptPlanWide, "plan-wide", false, "TARGET_ID is a plan rather than a task")
	qualityAccept.Flags().BoolVar(&acceptFixture, "synthetic-fixture", false, "Accept only labeled disposable fixture evidence")
	root.AddCommand(qualityAccept)

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
