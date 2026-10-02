package core

import (
	"context"
	"database/sql"
	"strings"
	"unicode"
)

// ProgressionPlan is the active plan as seen in one consistent snapshot.
type ProgressionPlan struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
	State    string `json:"state"`
	Rank     int    `json:"rank"`
}

// ProgressionTask is the selected current task with its exact blockers.
type ProgressionTask struct {
	ID       string   `json:"id"`
	Revision int      `json:"revision"`
	State    string   `json:"state"`
	Blockers []string `json:"blockers"`
}

// ActivityItem is one bounded, sanitized activity entry derived from
// persisted event and session state. It carries the event kind label only,
// never raw harness output and never a reconstructed provider handle.
type ActivityItem struct {
	Sequence int64  `json:"sequence"`
	At       int64  `json:"occurred_at"`
	Label    string `json:"label"`
}

// ActiveRunDetail is the read model for the live run checkpoint C requires:
// identity, budgets and the allowed next commands, plus a bounded activity
// summary. A nil *ActiveRunDetail means no run is active.
type ActiveRunDetail struct {
	RunID            string `json:"run_id"`
	State            string `json:"state"`
	RuntimeKind      string `json:"runtime_kind"`
	TaskID           string `json:"task_id"`
	PlanID           string `json:"plan_id"`
	GenerationID     string `json:"generation_id"`
	GenerationState  string `json:"generation_state"`
	SessionID        string `json:"session_id,omitempty"`
	NativeRequestKey string `json:"native_request_key,omitempty"`
	WallLimitMS      int64  `json:"wall_limit_ms"`
	ActiveLimitMS    int64  `json:"active_limit_ms"`
	// BudgetObserved reports whether the application has recorded any
	// active_segment for this run. It is false when nothing has been
	// recorded, and a view must then render "unavailable" rather than a
	// zero, which is the honesty rule P16 exists to enforce. The v1 table
	// `time_segments` is deliberately NOT read here: it has no production
	// writer, so reading it yielded a structural zero that read as a
	// measurement. `active_segments` is the table the supervisor writes.
	BudgetObserved bool `json:"budget_observed"`
	// ActiveChargedMS is this RUN's charged time summed from
	// active_segments, and is therefore attempt-scoped: it is comparable
	// with ActiveLimitMS, which comes from the same attempt.
	ActiveChargedMS int64 `json:"active_charged_ms"`
	// WallConsumedMS is this run's recorded elapsed wall time, summed as
	// (wall_checkpoint_at - wall_started_at) over its active segments.
	WallConsumedMS int64 `json:"wall_consumed_ms"`
	// TaskChargedMS, TaskLimitMS and UnknownMS are the TASK-cumulative
	// accounting from budget_ledgers (scope='task'), which spans every
	// attempt of the task. They stay separate from the attempt-scoped
	// pair above on purpose: pairing an attempt limit with a task
	// cumulative charge misreports the remaining allowance after any
	// retry, which is what 6.3's review round 9 found.
	TaskChargedMS   int64          `json:"task_charged_ms"`
	TaskLimitMS     int64          `json:"task_limit_ms"`
	UnknownMS       int64          `json:"unknown_ms"`
	AllowedNext     []string       `json:"allowed_next_commands"`
	Activity        []ActivityItem `json:"recent_activity"`
	ActivitySummary string         `json:"activity_summary"`
}

// UsageSummary reports cost/usage honesty for the main screen. Observed is
// false when the application recorded nothing, and the view must render an
// explicit unavailable marker in that case, never a zero.
type UsageSummary struct {
	Observed       bool   `json:"observed"`
	Provenance     string `json:"provenance,omitempty"`
	InputTokens    int64  `json:"input_tokens,omitempty"`
	OutputTokens   int64  `json:"output_tokens,omitempty"`
	HasCost        bool   `json:"has_cost,omitempty"`
	CostMicrounits int64  `json:"cost_microunits,omitempty"`
	Currency       string `json:"currency,omitempty"`
}

// ProgressionDetail exposes the R45/P15 main-screen state from one
// consistent snapshot: the active plan, the selected task, the full ranked
// queue with the active plan's position, and whether execution is eligible
// with the exact blocking issues.
type ProgressionDetail struct {
	ProjectID      string           `json:"project_id"`
	ProjectRev     int              `json:"project_revision"`
	ProjectState   string           `json:"project_state"`
	ActivePlan     *ProgressionPlan `json:"active_plan,omitempty"`
	SelectedTask   *ProgressionTask `json:"selected_task,omitempty"`
	Queue          []PlanQueueEntry `json:"queue"`
	QueuePosition  int              `json:"queue_position"`
	ActiveRun      string           `json:"active_run,omitempty"`
	Run            *ActiveRunDetail `json:"run,omitempty"`
	Usage          UsageSummary     `json:"usage"`
	Eligible       bool             `json:"eligible"`
	BlockingIssues []string         `json:"blocking_issues"`
}

// sanitizeLabel keeps activity labels bounded printable text.
func sanitizeLabel(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, value)
	runes := []rune(value)
	if len(runes) > 80 {
		return string(runes[:80]) + "…"
	}
	return value
}

func readProgressionSnapshot(ctx context.Context, tx *sql.Tx) (ProgressionDetail, Readiness, []TaskDetail, []PlanDetail, []InboxEntry, []Event, error) {
	// All helpers below take this same *sql.Tx: one read-only snapshot,
	// never mixed revisions. The underlying reads are bounded (queue,
	// tasks, plans, inbox and events each cap at 100 rows), so past 100
	// rows the selected-task search and queue position operate on a
	// truncated window; that bound matches the existing dashboard and
	// CLI windows rather than introducing a new one.
	var detail ProgressionDetail
	var readiness Readiness
	var tasks []TaskDetail
	var plans []PlanDetail
	var inbox []InboxEntry
	var events []Event
	var err error
	if readiness, err = readReadinessTx(ctx, tx); err != nil {
		return detail, readiness, nil, nil, nil, nil, err
	}
	var queue []PlanQueueEntry
	if queue, err = readPlanQueue(ctx, tx); err != nil {
		return detail, readiness, nil, nil, nil, nil, err
	}
	if tasks, plans, err = readDashboardDetails(ctx, tx); err != nil {
		return detail, readiness, nil, nil, nil, nil, err
	}
	if inbox, err = readInbox(ctx, tx); err != nil {
		return detail, readiness, nil, nil, nil, nil, err
	}
	if events, err = readEvents(ctx, tx, 0, true); err != nil {
		return detail, readiness, nil, nil, nil, nil, err
	}
	var activeRun string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM runs WHERE state IN('prepared','starting','active','stopping','unknown') ORDER BY created_at DESC,id DESC LIMIT 1`).Scan(&activeRun); err != nil && err != sql.ErrNoRows {
		return detail, readiness, nil, nil, nil, nil, err
	}
	detail = buildProgression(readiness, queue, tasks, activeRun)
	if activeRun != "" {
		// A run selected in this snapshot cannot vanish before the
		// detail read in the same transaction; propagate the error
		// rather than presenting "run X" with no detail.
		run, rerr := readActiveRunDetail(ctx, tx, activeRun, events)
		if rerr != nil {
			return detail, readiness, nil, nil, nil, nil, rerr
		}
		detail.Run = run
		detail.Usage = readUsage(ctx, tx, activeRun)
	}
	return detail, readiness, tasks, plans, inbox, events, nil
}

func readReadinessTx(ctx context.Context, tx *sql.Tx) (Readiness, error) {
	// readiness() takes a readinessReader; *sql.Tx satisfies it.
	e := &Engine{}
	return e.readiness(ctx, tx)
}

func buildProgression(readiness Readiness, queue []PlanQueueEntry, tasks []TaskDetail, activeRun string) ProgressionDetail {
	detail := ProgressionDetail{
		ProjectID:      readiness.Project.ID,
		ProjectRev:     readiness.Project.Revision,
		ProjectState:   readiness.Project.State,
		Queue:          queue,
		QueuePosition:  -1,
		ActiveRun:      activeRun,
		BlockingIssues: []string{},
	}
	if len(queue) > 0 {
		// Active plan is the head of the ranked queue. readPlanQueue
		// orders active/blocked/paused before queued before the rest,
		// so the head is the plan the scheduler would consider first;
		// with several live plans this is a display choice, not a
		// scheduling claim, and QueuePosition is its index (0 here by
		// construction, non-zero only if the ordering ever changes).
		first := queue[0]
		detail.ActivePlan = &ProgressionPlan{ID: first.ID, Revision: first.Revision, State: first.State, Rank: first.Rank}
		detail.QueuePosition = 0
	}
	// Selected task: lowest-ranked non-terminal task of the active plan.
	// Terminal states are accepted and stopped; everything else
	// (draft/ready/running/checking/reviewing/awaiting_human/
	// needs_repair/blocked) is still the operator's current work.
	if detail.ActivePlan != nil {
		for _, task := range tasks {
			if task.PlanID != detail.ActivePlan.ID {
				continue
			}
			if task.State == "accepted" || task.State == "stopped" {
				continue
			}
			blockers := []string{}
			if task.BlockReason != "" {
				blockers = append(blockers, task.BlockReason)
			}
			// Join readiness issues for the same task id.
			for _, ready := range readiness.Tasks {
				if ready.ID == task.ID {
					blockers = append(blockers, ready.Issues...)
				}
			}
			detail.SelectedTask = &ProgressionTask{ID: task.ID, Revision: task.Revision, State: task.State, Blockers: blockers}
			break
		}
	}
	detail.BlockingIssues = append(detail.BlockingIssues, readiness.DefinitionIssues...)
	detail.BlockingIssues = append(detail.BlockingIssues, readiness.RuntimeIssues...)
	if detail.SelectedTask != nil {
		detail.BlockingIssues = append(detail.BlockingIssues, detail.SelectedTask.Blockers...)
	}
	// Eligible is fail-closed against the canonical readiness gate:
	// ExecutionEligible is authoritative (currently always false while
	// production qualification is pending) and the blocking issues
	// explain why. The two cannot contradict because Eligible requires
	// both.
	detail.Eligible = readiness.ExecutionEligible && len(detail.BlockingIssues) == 0
	return detail
}

func readActiveRunDetail(ctx context.Context, tx *sql.Tx, runID string, events []Event) (*ActiveRunDetail, error) {
	detail := &ActiveRunDetail{RunID: runID, AllowedNext: []string{}}
	var writerState, submissionState string
	// NativeRequestKey carries the generation's native turn identity when
	// present. That is a display handle in the run's domain, distinct
	// from requests.native_request_key in the inbox domain; it is never
	// used to reconstruct a provider handle.
	err := tx.QueryRowContext(ctx, `SELECT r.state,r.task_id,r.plan_id,r.active_limit_ms,r.wall_limit_ms,r.writer_state,g.id,g.state,g.submission_state,coalesce(g.runtime_kind,''),coalesce(g.native_session_id,''),coalesce(g.native_turn_id,'') FROM runs r JOIN run_generations g ON g.run_id=r.id WHERE r.id=? ORDER BY g.ordinal DESC LIMIT 1`, runID).Scan(&detail.State, &detail.TaskID, &detail.PlanID, &detail.ActiveLimitMS, &detail.WallLimitMS, &writerState, &detail.GenerationID, &detail.GenerationState, &submissionState, &detail.RuntimeKind, &detail.SessionID, &detail.NativeRequestKey)
	if err != nil {
		// Fall back to the runs row alone when no generation exists yet.
		if ferr := tx.QueryRowContext(ctx, `SELECT state,task_id,plan_id,active_limit_ms,wall_limit_ms FROM runs WHERE id=?`, runID).Scan(&detail.State, &detail.TaskID, &detail.PlanID, &detail.ActiveLimitMS, &detail.WallLimitMS); ferr != nil {
			return nil, ferr
		}
		submissionState = ""
	}
	// Attempt-scoped accounting from active_segments, the table the
	// supervisor actually writes: this run's charged time, its unknown
	// time, and its recorded elapsed wall time. When no segment exists
	// the run's budget is unobserved, not zero.
	var segments int
	var charged, unknown, wall int64
	if err := tx.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(charged_ms),0),coalesce(sum(unknown_ms),0),coalesce(sum(max(wall_checkpoint_at-wall_started_at,0)),0) FROM active_segments WHERE run_id=?`, runID).Scan(&segments, &charged, &unknown, &wall); err == nil {
		detail.BudgetObserved = segments > 0
		detail.ActiveChargedMS = charged
		detail.UnknownMS = unknown
		detail.WallConsumedMS = wall
	}
	// Task-cumulative accounting from the task ledger, kept separate from
	// the attempt-scoped fields above. UnknownMS stays attempt-scoped.
	_ = tx.QueryRowContext(ctx, `SELECT coalesce(charged_ms,0),active_limit_ms FROM budget_ledgers WHERE scope='task' AND task_id=?`, detail.TaskID).Scan(&detail.TaskChargedMS, &detail.TaskLimitMS)
	// Session identity: the durable sessions row when one exists,
	// otherwise the generation's native session handle. The durable id
	// and the provider handle live in different domains; the durable
	// one wins because it is the application-owned identity.
	var durable string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM sessions WHERE run_id=? ORDER BY rowid DESC LIMIT 1`, runID).Scan(&durable); err == nil && durable != "" {
		detail.SessionID = durable
	}
	// Allowed-next mirrors supervisor.RunView (reconcile.go): submission
	// uncertainty gates reconcile/stop, terminal runs inspect only,
	// live states offer the full set. It is duplicated here because
	// core cannot import supervisor (import cycle); keep the two in
	// step. writer_state is selected for display completeness; like
	// RunView this mapping does not branch on it.
	switch {
	case submissionState == "uncertain":
		detail.AllowedNext = []string{"inspect", "reconcile", "stop"}
	// The completed branch is unreachable through the live-run selector
	// above (it selects only non-terminal states) and is kept so a
	// widened selector cannot offer controls on a terminal run.
	case detail.State == "completed":
		detail.AllowedNext = []string{"inspect"}
	case detail.State == "prepared" || detail.State == "starting" || detail.State == "active":
		detail.AllowedNext = []string{"inspect", "start", "reconcile", "stop"}
	default:
		detail.AllowedNext = []string{"inspect", "reconcile"}
	}
	// Bounded sanitized activity: the most recent event kinds only.
	const maxActivity = 10
	start := len(events) - maxActivity
	if start < 0 {
		start = 0
	}
	for _, event := range events[start:] {
		detail.Activity = append(detail.Activity, ActivityItem{Sequence: event.Sequence, At: event.At, Label: sanitizeLabel(event.Kind)})
	}
	if len(detail.Activity) == 0 {
		detail.ActivitySummary = "no recorded activity"
	} else {
		kinds := make([]string, 0, len(detail.Activity))
		for _, item := range detail.Activity {
			kinds = append(kinds, item.Label)
		}
		// Keep the one-line summary bounded.
		summary := strings.Join(kinds, ", ")
		if len([]rune(summary)) > 160 {
			summary = string([]rune(summary)[:160]) + "…"
		}
		detail.ActivitySummary = summary
	}
	return detail, nil
}

func readUsage(ctx context.Context, tx *sql.Tx, runID string) UsageSummary {
	var summary UsageSummary
	// Aggregate across provenances: totals sum every row, and the
	// provenance names the single source or "mixed". Currency is
	// reported only when every cost-bearing row agrees; otherwise it is
	// left empty rather than attaching one row's currency to another
	// row's cost. A read error maps to unobserved (Observed=false),
	// deliberately: this is a display hint, and a failed usage read
	// must never fail the whole progression snapshot.
	rows, err := tx.QueryContext(ctx, `SELECT provenance,coalesce(sum(input_tokens),0),coalesce(sum(output_tokens),0),sum(cost_microunits),max(currency) FROM usage_observations WHERE run_id=? GROUP BY provenance`, runID)
	if err != nil {
		return summary
	}
	defer rows.Close()
	provenances := []string{}
	currencies := map[string]bool{}
	var totalIn, totalOut, totalCost int64
	hasCost := false
	for rows.Next() {
		var provenance string
		var inTokens, outTokens int64
		var cost sql.NullInt64
		var currency sql.NullString
		if err := rows.Scan(&provenance, &inTokens, &outTokens, &cost, &currency); err != nil {
			return UsageSummary{}
		}
		provenances = append(provenances, provenance)
		totalIn += inTokens
		totalOut += outTokens
		if cost.Valid {
			hasCost = true
			totalCost += cost.Int64
		}
		if currency.Valid && currency.String != "" {
			currencies[currency.String] = true
		}
	}
	if err := rows.Err(); err != nil || len(provenances) == 0 {
		return UsageSummary{}
	}
	summary.Observed = true
	summary.InputTokens = totalIn
	summary.OutputTokens = totalOut
	if len(provenances) == 1 {
		summary.Provenance = provenances[0]
	} else {
		summary.Provenance = "mixed"
	}
	if hasCost {
		summary.HasCost = true
		summary.CostMicrounits = totalCost
		if len(currencies) == 1 {
			for currency := range currencies {
				summary.Currency = currency
			}
		}
	}
	return summary
}

// Progression returns the P15 read model from one consistent snapshot.
// It currently pays the full dashboard read and discards the non-P15
// fields; fine at this scale, revisit if the snapshot grows.
func (e *Engine) Progression(ctx context.Context) (ProgressionDetail, error) {
	tx, err := e.DB.SQL.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ProgressionDetail{}, err
	}
	defer tx.Rollback()
	detail, _, _, _, _, _, err := readProgressionSnapshot(ctx, tx)
	return detail, err
}
