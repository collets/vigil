package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"vigil/internal/checkpoint"
	"vigil/internal/coordinator"
	"vigil/internal/core"
	"vigil/internal/storage"
	"vigil/internal/store"
	"vigil/internal/supervisor"
	"vigil/internal/tui"
	"vigil/internal/workspace"
)

type dashboardFixtureInspector struct {
	engine     *core.Engine
	state      string
	class      string
	automatic  bool
	checkpoint *checkpoint.Manager
}

func requireSyntheticFixture(ctx context.Context, engine *core.Engine, prepared supervisor.PreparedRun) error {
	if prepared.RuntimeKind != "synthetic" {
		return errors.New("interactive fixture owner cannot control a production run")
	}
	for _, repository := range prepared.Repositories {
		identity, err := workspace.Inspect(ctx, repository.Root)
		if err != nil || identity.Root != repository.Root || identity.Key != repository.Identity.Key || identity.CommonGit != repository.Identity.CommonGit || identity.CommonGitPath != repository.Identity.CommonGitPath {
			return errors.New("interactive fixture repository identity changed")
		}
		info, err := os.Lstat(filepath.Join(identity.Root, ".vigil-disposable-fixture"))
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("interactive fixture owner requires every run repository to carry the disposable marker")
		}
	}
	return nil
}

func (i dashboardFixtureInspector) InspectHistory(ctx context.Context, prepared supervisor.PreparedRun) (supervisor.HistoryObservation, error) {
	if err := requireSyntheticFixture(ctx, i.engine, prepared); err != nil {
		return supervisor.HistoryObservation{}, err
	}
	observation := supervisor.HistoryObservation{State: i.state, RecoveryClass: i.class, AutomaticWork: i.automatic, Qualification: "synthetic"}
	if i.state == "readable" {
		if err := i.engine.DB.SQL.QueryRowContext(ctx, `SELECT native_home_ref,coalesce(durable_id,''),profile_digest,workspace_identity,generation FROM sessions WHERE run_id=? ORDER BY rowid DESC LIMIT 1`, prepared.RunID).Scan(&observation.NativeHomeRef, &observation.NativeSessionID, &observation.ProfileDigest, &observation.WorkspaceIdentity, &observation.TransportGeneration); err != nil {
			return observation, err
		}
	}
	return observation, nil
}

func (i dashboardFixtureInspector) VerifyCheckpoint(ctx context.Context, id string) error {
	_, err := i.checkpoint.VerifySet(ctx, id)
	return err
}

func (i dashboardFixtureInspector) VerifyCheckpointCurrent(ctx context.Context, id string) error {
	return i.checkpoint.VerifyCheckpointCurrent(ctx, id)
}

type dashboardFixtureClarifications struct{ engine *core.Engine }

func (d dashboardFixtureClarifications) DeliverClarification(ctx context.Context, delivery supervisor.NativeClarificationDelivery) error {
	prepared, err := supervisor.LoadPrepared(ctx, d.engine, delivery.RunID)
	if err != nil {
		return err
	}
	if err := requireSyntheticFixture(ctx, d.engine, prepared); err != nil {
		return err
	}
	if prepared.GenerationID != delivery.GenerationID || prepared.TransportGeneration != delivery.TransportGeneration {
		return errors.New("fixture clarification generation changed")
	}
	payload, _ := json.Marshal(map[string]any{"run_id": delivery.RunID, "generation_id": delivery.GenerationID, "session_id": delivery.SessionID, "native_request_key": delivery.NativeRequestKey, "decision": delivery.Decision, "fixture_mechanics_only": true, "answer_digest": store.Digest([]byte(delivery.Answer))})
	return d.engine.DB.Write(ctx, func(tx *store.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO events(schema_version,kind,occurred_at,payload_json) VALUES(1,'fixture_native_clarification_delivered',?,?)", store.Now(), string(payload))
		return err
	})
}

func (d dashboardFixtureClarifications) InspectClarification(ctx context.Context, delivery supervisor.NativeClarificationDelivery) (string, error) {
	prepared, err := supervisor.LoadPrepared(ctx, d.engine, delivery.RunID)
	if err != nil {
		return "unknown", err
	}
	if err := requireSyntheticFixture(ctx, d.engine, prepared); err != nil {
		return "unknown", err
	}
	if prepared.GenerationID != delivery.GenerationID || prepared.TransportGeneration != delivery.TransportGeneration {
		return "unknown", errors.New("fixture clarification generation changed")
	}
	var delivered int
	if err := d.engine.DB.SQL.QueryRowContext(ctx, `SELECT count(*) FROM events WHERE kind='fixture_native_clarification_delivered' AND json_extract(payload_json,'$.run_id')=? AND json_extract(payload_json,'$.generation_id')=? AND json_extract(payload_json,'$.session_id')=? AND json_extract(payload_json,'$.native_request_key')=?`, delivery.RunID, delivery.GenerationID, delivery.SessionID, delivery.NativeRequestKey).Scan(&delivered); err != nil {
		return "unknown", err
	}
	if delivered == 1 {
		return "delivered", nil
	}
	if delivered == 0 {
		return "proven_not_delivered", nil
	}
	return "unknown", errors.New("duplicate fixture clarification delivery evidence")
}

func fixtureInteractive(ctx context.Context, m *core.Manager, e *core.Engine, historyState, historyClass string, historyAutomatic bool) (*supervisor.InteractiveOwner, *coordinator.Owner, error) {
	checkpointManager, err := checkpoint.NewManager(e)
	if err != nil {
		return nil, nil, err
	}
	owner, err := m.Coordinator.Register(ctx)
	if err != nil {
		return nil, nil, err
	}
	interactive := &supervisor.InteractiveOwner{Engine: e, Owner: owner, Recovery: dashboardFixtureInspector{engine: e, state: historyState, class: historyClass, automatic: historyAutomatic, checkpoint: checkpointManager}, Clarifications: dashboardFixtureClarifications{engine: e}}
	return interactive, owner, nil
}

// runDashboardWithSwitcher opens the shell on the requested project and
// offers every other registered project through the P switcher. The
// PROJECT_ID argument still selects the opening project unchanged; switching
// re-opens the target engine with the same fixture mode and closes the
// previous engine, so exactly one project database is open at a time.
func runDashboardWithSwitcher(cmd *cobra.Command, m *core.Manager, e *core.Engine, interactive *supervisor.InteractiveOwner, owner *coordinator.Owner, historyState, historyClass string, historyAutomatic bool) error {
	ctx := cmd.Context()
	listed, err := m.List(ctx)
	if err != nil {
		return err
	}
	projects := make([]tui.ProjectRef, 0, len(listed))
	for _, p := range listed {
		projects = append(projects, tui.ProjectRef{ID: p.ID, Root: p.Root})
	}
	if len(projects) == 0 {
		projects = []tui.ProjectRef{{ID: e.ProjectID}}
	}
	current := e
	currentInteractive := interactive
	currentOwner := owner
	// The shell owns the active owner: the opening owner is closed on exit
	// and every switched-to owner is closed when the next switch replaces
	// it, so P-switching in fixture mode leaks no lock, flock or row.
	defer func() {
		if currentOwner != nil {
			currentOwner.Close()
		}
		current.DB.Close()
	}()
	open := func(id string) (*core.Engine, *coordinator.Coordinator, *supervisor.InteractiveOwner, error) {
		next, err := m.Open(ctx, id)
		if err != nil {
			return nil, nil, nil, err
		}
		var nextInteractive *supervisor.InteractiveOwner
		var nextOwner *coordinator.Owner
		if currentInteractive != nil {
			built, builtOwner, err := fixtureInteractive(ctx, m, next, historyState, historyClass, historyAutomatic)
			if err != nil {
				next.DB.Close()
				return nil, nil, nil, err
			}
			nextInteractive = built
			nextOwner = builtOwner
		}
		current.DB.Close()
		if currentOwner != nil {
			currentOwner.Close()
		}
		current = next
		currentInteractive = nextInteractive
		currentOwner = nextOwner
		return next, m.Coordinator, nextInteractive, nil
	}
	return tui.RunProjectWithProjects(ctx, e, m.Coordinator, interactive, projects, open, cmd.InOrStdin(), cmd.OutOrStdout())
}

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
	var dashboardFixture bool
	var dashboardHistoryState, dashboardHistoryClass string
	var dashboardHistoryAutomatic bool
	var dashboardClarificationRun, dashboardClarificationSession, dashboardClarificationKey, dashboardClarificationPrompt string
	dashboard := &cobra.Command{
		Use:   "dashboard PROJECT_ID",
		Short: "Inspect persisted readiness, tasks, inbox and history",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := manager(cmd, &stateDir)
			if err != nil {
				return err
			}
			defer m.Close()
			e, err := m.Open(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			// Ownership of the open engine (and below, the fixture owner)
			// transfers to runDashboardWithSwitcher on entry; these defers
			// run only when an error returns before that handoff, so no
			// handle is ever closed twice.
			engineTransferred := false
			defer func() {
				if !engineTransferred {
					e.DB.Close()
				}
			}()
			if !dashboardFixture {
				if dashboardClarificationRun != "" || dashboardClarificationSession != "" || dashboardClarificationKey != "" || dashboardClarificationPrompt != "" {
					return errors.New("fixture clarification flags require --synthetic-interactions")
				}
				engineTransferred = true
			return runDashboardWithSwitcher(cmd, m, e, nil, nil, "", "", false)
			}
			if dashboardHistoryState != "readable" && dashboardHistoryState != "missing" && dashboardHistoryState != "corrupt" && dashboardHistoryState != "unsupported" {
				return errors.New("--history-state must be readable, missing, corrupt, or unsupported")
			}
			interactive, owner, err := fixtureInteractive(cmd.Context(), m, e, dashboardHistoryState, dashboardHistoryClass, dashboardHistoryAutomatic)
			if err != nil {
				return err
			}
			ownerTransferred := false
			defer func() {
				if !ownerTransferred {
					owner.Close()
				}
			}()
			if dashboardClarificationRun != "" {
				if dashboardClarificationSession == "" || dashboardClarificationKey == "" || dashboardClarificationPrompt == "" {
					return errors.New("fixture clarification requires --clarification-session, --clarification-key and --clarification-prompt")
				}
				prepared, err := supervisor.LoadPrepared(cmd.Context(), e, dashboardClarificationRun)
				if err != nil {
					return err
				}
				if err := requireSyntheticFixture(cmd.Context(), e, prepared); err != nil {
					return err
				}
				var revision int
				if err := e.DB.SQL.QueryRowContext(cmd.Context(), "SELECT revision FROM project WHERE id=?", e.ProjectID).Scan(&revision); err != nil {
					return err
				}
				prompt, _ := json.Marshal(map[string]any{"question": dashboardClarificationPrompt, "fixture_mechanics_only": true, "untrusted_context": true})
				if _, err := interactive.PersistClarification(cmd.Context(), supervisor.PersistClarificationRequest{CommandID: store.ID(), ExpectedRevision: revision, Prepared: prepared, SessionID: dashboardClarificationSession, NativeRequestKey: dashboardClarificationKey, Prompt: prompt, Deadline: time.Now().Add(30 * time.Minute).UnixMilli()}); err != nil {
					return err
				}
			}
			engineTransferred = true
			ownerTransferred = true
			return runDashboardWithSwitcher(cmd, m, e, interactive, owner, dashboardHistoryState, dashboardHistoryClass, dashboardHistoryAutomatic)
		},
	}
	dashboard.Flags().BoolVar(&dashboardFixture, "synthetic-interactions", false, "Enable owner-routed interactions only for marked disposable synthetic runs")
	dashboard.Flags().StringVar(&dashboardHistoryState, "history-state", "missing", "Observed synthetic history state for recovery choices")
	dashboard.Flags().StringVar(&dashboardHistoryClass, "history-class", "interrupted", "Observed synthetic recovery class")
	dashboard.Flags().BoolVar(&dashboardHistoryAutomatic, "history-automatic-work", false, "Record automatic work in synthetic history")
	dashboard.Flags().StringVar(&dashboardClarificationRun, "clarification-run", "", "Synthetic run receiving one fixture clarification request")
	dashboard.Flags().StringVar(&dashboardClarificationSession, "clarification-session", "", "Exact persisted fixture session record")
	dashboard.Flags().StringVar(&dashboardClarificationKey, "clarification-key", "", "Exact opaque fixture native-request key")
	dashboard.Flags().StringVar(&dashboardClarificationPrompt, "clarification-prompt", "", "Bounded visible fixture question")
	root.AddCommand(dashboard)
	return root
}
