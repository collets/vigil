package scenario

import (
	"fmt"
	"sort"
	"strings"
)

// Evidence classes distinguish what a walkthrough actually demonstrated from
// what it merely reuses or defers. The distinction is the point of Stage 5.7:
// a green unit suite, a contained fixture turn or a fake hosting response can
// never be reported as the milestone being demonstrated.
const (
	// EvidenceAutomated is produced by this scenario run itself, through the
	// production application path.
	EvidenceAutomated = "automated"
	// EvidenceReused is a current result document from an earlier accepted
	// slice, carried forward with an explicit source reference.
	EvidenceReused = "reused"
	// EvidencePendingStage8 requires a human, a live credentialed route or an
	// authorized real destination. It is never reported as a pass.
	EvidencePendingStage8 = "pending_stage_8"
	// EvidenceUnmet means the autonomous slice could not produce this evidence
	// and it is not a human gate either. It is a defect in the product, and it is
	// reported as such so the work is not filed as somebody else's to-do.
	EvidenceUnmet = "unmet"
)

// Milestone steps are the accepted demonstration steps of the first usable
// milestone. Stage 5.7 rehearses the autonomous ones and enumerates the rest.
const (
	StepConfigureProfiles   = "1-configure-profiles"
	StepLoadSpecification   = "2-load-specification"
	StepProducePlan         = "3-produce-and-approve-plan"
	StepExecuteSequentially = "4-execute-tasks-sequentially"
	StepReviewAndRepair     = "5-review-finding-and-repair"
	StepChecksAndEvidence   = "6-checks-findings-summaries"
	StepHumanAcceptance     = "7-task-and-plan-human-review"
	StepDraftRequest        = "8-draft-pull-or-merge-request"
)

// milestoneSteps is the ordered milestone table the report renders.
var milestoneSteps = []string{
	StepConfigureProfiles,
	StepLoadSpecification,
	StepProducePlan,
	StepExecuteSequentially,
	StepReviewAndRepair,
	StepChecksAndEvidence,
	StepHumanAcceptance,
	StepDraftRequest,
}

// Recovery cases are the integrated recovery and boundary cases from
// checkpoint C. Each is either demonstrated by this run or explicitly pending.
const (
	CasePauseBlocksDispatch      = "pause-blocks-dispatch"
	CaseStopPreservesWork        = "stop-interrupts-and-preserves"
	CaseRestartOffersResume      = "restart-offers-resume-or-fresh"
	CaseControllerKillUnknown    = "controller-kill-leaves-unknown-outcome"
	CaseOverlappingProjects      = "overlapping-projects-conflict"
	CaseNonoverlappingProjects   = "nonoverlapping-projects-progress"
	CaseEndpointAliasQueue       = "endpoint-alias-queues-behind-one-capacity"
	CaseCrossHostRejected        = "cross-host-capacity-not-authorized"
	CaseStaleOwnerFenced         = "stale-owner-fenced"
	CaseRepairExhaustion         = "repair-exhaustion-blocks-without-accepting"
	CaseWaitNotCharged           = "resource-wait-not-charged-to-execution"
	CaseRejectedStalePermission  = "rejected-or-stale-permission-blocks-effect"
	CaseMissingQualityEvidence   = "missing-quality-evidence-blocks-acceptance"
	CaseMixedWorkPreserved       = "mixed-user-and-agent-work-preserved"
	CasePartialMultiRepoRestore  = "partial-multi-repository-restore-is-visible"
	CaseNoUnintendedRefs         = "no-unintended-refs-created"
	CaseSummaryOnlyRetry         = "finalization-retry-does-not-rerun-development"
	CaseTranscriptExpiryPreserve = "transcript-expiry-preserves-durable-evidence"
	CaseGitAndHostingBoundary    = "git-and-hosting-boundaries-verified"
	// CaseBaseBranchReturn records the integration gap the rehearsal actually
	// found: the production CLI cannot return an enrolled repository to its base
	// branch, so the commit path is unreachable after the documented
	// prepare/execute/accept path.
	CaseBaseBranchReturn = "base-branch-return-for-commits"
)

// recoveryCases is the ordered recovery/boundary matrix.
var recoveryCases = []string{
	CasePauseBlocksDispatch,
	CaseStopPreservesWork,
	CaseRestartOffersResume,
	CaseControllerKillUnknown,
	CaseOverlappingProjects,
	CaseNonoverlappingProjects,
	CaseEndpointAliasQueue,
	CaseCrossHostRejected,
	CaseStaleOwnerFenced,
	CaseRepairExhaustion,
	CaseWaitNotCharged,
	CaseRejectedStalePermission,
	CaseMissingQualityEvidence,
	CaseMixedWorkPreserved,
	CasePartialMultiRepoRestore,
	CaseNoUnintendedRefs,
	CaseSummaryOnlyRetry,
	CaseTranscriptExpiryPreserve,
	CaseGitAndHostingBoundary,
	CaseBaseBranchReturn,
}

// MatrixEntry is one row of the evidence matrix: what was claimed, what class of
// evidence backs it, and — when it is a gate — the exact missing step.
type MatrixEntry struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Evidence string `json:"evidence"`
	Detail   string `json:"detail,omitempty"`
	Source   string `json:"source,omitempty"`
	Blocker  string `json:"blocker,omitempty"`
}

// Matrix is the full audit: the milestone table, the recovery matrix, and the
// requirement coverage map.
type Matrix struct {
	Milestone    []MatrixEntry `json:"milestone"`
	Recovery     []MatrixEntry `json:"recovery"`
	Requirements []MatrixEntry `json:"requirements"`
	// RequirementGapList is the recorded set of requirement rows that are not
	// fully demonstrated. It is a field rather than a derived value so the report
	// carries it: a gap that exists only in a function nobody called is not a gap
	// the report can be said to have reported.
	RequirementGapList []string `json:"requirement_gaps"`
}

// NewMatrix returns the matrix with every declared step present and unresolved.
// The walkthrough fills entries in; anything it does not reach keeps its initial
// state, which is a gate, never a silent omission.
func NewMatrix() *Matrix {
	matrix := &Matrix{}
	// Rows start undecided, with no evidence class at all. A blank class makes
	// RequireComplete fail until the walkthrough decides every row, so a case it
	// never reached can never be mistaken for a case that passed.
	for _, step := range milestoneSteps {
		matrix.Milestone = append(matrix.Milestone, MatrixEntry{ID: step, Title: step, Detail: "not yet decided by this run"})
	}
	for _, testCase := range recoveryCases {
		matrix.Recovery = append(matrix.Recovery, MatrixEntry{ID: testCase, Title: testCase, Detail: "not yet decided by this run"})
	}
	return matrix
}

// MarkUnmet records a row that this run could not demonstrate because of a
// defect in the product, rather than because a human gate owns it.
//
// This class is deliberately distinct from a pending gate. A pending row says
// "a person must do this"; an unmet row says "the product cannot do this yet",
// and conflating them would let a product defect be filed as somebody else's
// to-do.
func (m *Matrix) MarkUnmet(section, id, detail, source string) error {
	return m.mark(section, id, EvidenceUnmet, detail, source, false)
}

// Mark records the outcome for one matrix row. A row that is claimed as
// automated or reused evidence requires a detail, so no row can be marked
// demonstrated with no explanation.
func (m *Matrix) Mark(section, id, evidence, detail, source string) error {
	return m.mark(section, id, evidence, detail, source, true)
}

// Partial is a row this run only partially observed: the narrower observation
// did happen through the production path, but the full property named by the row
// did not.
//
// It is deliberately a distinct class rather than a flavour of `automated`. A
// report that cannot distinguish a fully observed case from a partially observed
// one cannot be trusted to have earned its pass, and the gap list is built from
// this class.
const Partial = "partial"

// MarkPartial records a partially observed row. It is a separate class, not an
// alias, so the report and the gap list can tell it apart from a full
// observation.
func (m *Matrix) MarkPartial(section, id, detail, source string) error {
	return m.mark(section, id, Partial, detail, source, true)
}

func (m *Matrix) mark(section, id, evidence, detail, source string, pending bool) error {
	rows := m.section(section)
	if rows == nil {
		return fmt.Errorf("unknown matrix section %q", section)
	}
	if evidence != EvidencePendingStage8 && evidence != EvidenceUnmet && strings.TrimSpace(detail) == "" {
		return fmt.Errorf("matrix row %q cannot be marked %q without a detail", id, evidence)
	}
	for index := range rows {
		if rows[index].ID != id {
			continue
		}
		rows[index].Evidence = evidence
		rows[index].Detail = detail
		rows[index].Source = source
		if pending {
			rows[index].Blocker = ""
		} else {
			// An unmet row names the defect, which is the finding itself.
			rows[index].Blocker = detail
		}
		return nil
	}
	return fmt.Errorf("matrix row %q is not declared in section %q", id, section)
}

// Defer records a row as an owned gate with its exact blocker.
//
// An empty blocker is refused. A row that is pending without naming the step that
// would resolve it is exactly the failure mode this stage exists to prevent, so
// the matrix refuses to hold one.
func (m *Matrix) Defer(section, id, blocker, detail string) error {
	rows := m.section(section)
	if rows == nil {
		return fmt.Errorf("unknown matrix section %q", section)
	}
	if strings.TrimSpace(blocker) == "" {
		return fmt.Errorf("matrix row %q cannot be deferred without naming its blocker", id)
	}
	for index := range rows {
		if rows[index].ID != id {
			continue
		}
		rows[index].Evidence = EvidencePendingStage8
		rows[index].Detail = detail
		rows[index].Blocker = blocker
		return nil
	}
	return fmt.Errorf("matrix row %q is not declared in section %q", id, section)
}

func (m *Matrix) section(name string) []MatrixEntry {
	switch name {
	case "milestone":
		return m.Milestone
	case "recovery":
		return m.Recovery
	case "requirements":
		return m.Requirements
	}
	return nil
}

// RequireComplete verifies every declared milestone and recovery row carries a
// decided evidence class. A walkthrough that stops early cannot be reported as a
// completed audit.
func (m *Matrix) RequireComplete() error {
	for _, group := range []struct {
		name  string
		rows  []MatrixEntry
		count int
	}{
		{"milestone", m.Milestone, len(milestoneSteps)},
		{"recovery", m.Recovery, len(recoveryCases)},
		{"requirements", m.Requirements, len(requirementIDs())},
	} {
		if len(group.rows) != group.count {
			return fmt.Errorf("matrix section %q has %d rows, want %d", group.name, len(group.rows), group.count)
		}
		for _, row := range group.rows {
			switch row.Evidence {
			case EvidenceAutomated, EvidenceReused, Partial, EvidencePendingStage8, EvidenceUnmet:
			default:
				return fmt.Errorf("matrix row %q has an undecided evidence class %q", row.ID, row.Evidence)
			}
			if (row.Evidence == EvidencePendingStage8 || row.Evidence == EvidenceUnmet) && strings.TrimSpace(row.Blocker) == "" {
				return fmt.Errorf("matrix row %q is %q but records no exact blocker", row.ID, row.Evidence)
			}
			// A partial row must say what was not observed. A partial claim with
			// no scope statement is indistinguishable from a full one, which is
			// exactly the confusion this class exists to prevent.
			if row.Evidence == Partial && !strings.Contains(strings.ToUpper(row.Detail), "NOT OBSERVED") {
				return fmt.Errorf("matrix row %q is marked partial without stating what was not observed", row.ID)
			}
			if (row.Evidence == EvidenceAutomated || row.Evidence == EvidenceReused || row.Evidence == Partial) && strings.TrimSpace(row.Detail) == "" {
				return fmt.Errorf("matrix row %q claims %q evidence with no detail", row.ID, row.Evidence)
			}
		}
	}
	return nil
}

// RequirementGaps reports the requirement rows this run did not fully
// demonstrate.
//
// Requirements are listed separately from `Gap` because they are a different
// kind of claim from a milestone step or a recovery case, and folding them into
// the same list would blur two things a reader needs told apart. A requirement
// carries no case name, so there is nothing for a partial requirement to be
// partial *about* beyond the requirement text itself — which is why a partial
// requirement must still appear here rather than reading as a pass.
func (m *Matrix) RequirementGaps() []string {
	gaps := []string{}
	for _, row := range m.Requirements {
		switch row.Evidence {
		case EvidenceAutomated, EvidenceReused:
		case "":
			gaps = append(gaps, row.ID+": undecided")
		default:
			gaps = append(gaps, row.ID+": "+row.Evidence)
		}
	}
	sort.Strings(gaps)
	return gaps
}

// Counts summarizes one section by evidence class. The sections are counted
// separately because a milestone step, a recovery case and a requirement are
// different claims, and a single combined total would let one section's passes
// disguise another's gaps.
func (m *Matrix) Counts(section string) map[string]int {
	counts := map[string]int{}
	for _, row := range m.section(section) {
		counts[row.Evidence]++
	}
	return counts
}

// Gap reports every row that is not fully demonstrated, so the report can never
// quietly omit one. A partial row is a gap: the full property was not shown. An
// undecided row is labelled as such rather than as a pass.
func (m *Matrix) Gap() []string {
	gaps := []string{}
	for _, row := range append(append([]MatrixEntry{}, m.Milestone...), m.Recovery...) {
		switch row.Evidence {
		case EvidenceAutomated, EvidenceReused:
		case "":
			gaps = append(gaps, row.ID+": undecided")
		case Partial:
			gaps = append(gaps, row.ID+": partial")
		default:
			gaps = append(gaps, row.ID+": "+row.Evidence)
		}
	}
	sort.Strings(gaps)
	return gaps
}
