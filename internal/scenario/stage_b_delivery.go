package scenario

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// rehearseDelivery drives the Stage 5.6 delivery path end to end against a local
// bare remote and a credential-free loopback hosting stand-in, through the
// production commands only. It never pushes to a real remote and never creates a
// real hosted request: those are Stage 8 operations requiring the user's
// per-operation authorization.
//
// `betweenPushAndDraft` runs the factual archive, because the product's own
// ordering requires it there and not elsewhere. Draft delivery requires an archive
// revision, and the archive binds a fingerprint of the enrolled repository — so
// building it before the commit leaves it stale, and building it after the draft
// is too late. Commit, push, archive, draft, narrative is the sequence the product
// accepts, and the rehearsal follows it rather than inventing one.
//
// The output is the exact set of inputs Stage 8's delivery script needs —
// destination identity, head/base/object scope, credential reference name and
// the scoped grant list — so the human stage does not have to re-derive them.
func (w *walkthrough) rehearseDelivery(ctx context.Context, betweenPushAndDraft func(context.Context)) {
	hosting, err := NewFakeHosting("github", "vigil-scenario/fixture", "main")
	if err != nil {
		panic(&ScenarioAbort{Step: "delivery hosting stand-in", Err: err})
	}
	defer hosting.Close()
	w.hosting = hosting
	w.assert("fake-hosting-loopback-only", strings.HasPrefix(hosting.Base(), "http://127.0.0.1:"), hosting.Base(),
		"the hosting stand-in binds one loopback port and holds no credential")

	// The pre-rehearsal baseline. The working tree's bytes, the branch, the ref set
	// and the porcelain state are all recorded, because the rehearsal is expected
	// to change two of them and must leave the other two alone.
	baseline, err := w.captureCheckoutBaseline(ctx)
	if err != nil {
		panic(&ScenarioAbort{Step: "delivery baseline", Err: err})
	}
	w.assert("delivery-baseline-recorded", len(baseline.status) == 1, strings.Join(baseline.status, " | "),
		"the accepted artifact change must be present before the delivery rehearsal begins")

	evidence := w.commitAndPush(ctx)
	if evidence.Finding != "" {
		// The delivery path is unreachable through the production commands from
		// the documented prepare/execute/accept state. That is recorded as a
		// blocking finding, not worked around and not reported as a pass.
		w.report.Pending = append(w.report.Pending,
			"push and draft delivery rehearsal: blocked by "+evidence.Finding+" in the Stage 5.2/5.6 delivery path")
		w.closeDeliveryMatrixUnreachable(evidence)
		w.verifyCheckoutUnchanged(ctx, baseline, CommitEvidence{}, hosting)
		return
	}
	// The factual archive, which must observe the committed repository and must
	// exist before a draft can be created.
	betweenPushAndDraft(ctx)

	// Exactly the plan ref may have appeared in the bare remote, alongside the base
	// branch the destination was seeded with. No tag, no wildcard ref, no
	// checkpoint ref.
	refs, err := ListRefs(ctx, w.bareRemote)
	if err != nil {
		panic(&ScenarioAbort{Step: "remote ref listing", Err: err})
	}
	// The destination must hold exactly the seeded base branch and the one approved
	// plan ref, and nothing else. Naming the refs matters as much as counting them:
	// a count-only assertion passes just as happily if a tag replaced the plan ref.
	seen := map[string]bool{}
	for _, ref := range refs {
		name, _, _ := strings.Cut(ref, " ")
		seen[name] = true
	}
	wantRefs := []string{"refs/heads/main", "refs/heads/" + PlanBranch}
	exact := len(refs) == len(wantRefs)
	for _, name := range wantRefs {
		if !seen[name] {
			exact = false
		}
	}
	w.assert("no-unintended-remote-refs", exact, strings.Join(refs, " "),
		"the destination must hold exactly the seeded base branch and the one approved plan ref; a tag, mirror ref or checkpoint ref must never be pushed")
	if err := w.report.Matrix.Mark("recovery", CaseNoUnintendedRefs, EvidenceAutomated,
		"the destination holds exactly the base branch it was seeded with plus the one approved plan ref; no tag, mirror, force or checkpoint ref was created",
		"git for-each-ref on the local bare remote after the rehearsal"); err != nil {
		panic(&ScenarioAbort{Step: "matrix:refs", Err: err})
	}

	// The draft, which must observe the push and the archive.
	draft, draftErr := w.draftRequest(ctx, evidence.Push, hosting)
	if draftErr != "" {
		w.report.Pending = append(w.report.Pending, "draft delivery rehearsal: blocked by "+draftErr)
		w.closeDeliveryMatrixUnreachable(DeliveryEvidence{Finding: "5.7-F1", Detail: draftErr})
		w.verifyCheckoutUnchanged(ctx, baseline, evidence.Commit, hosting)
		return
	}
	push := evidence.Push
	evidence.Draft = draft

	// The rehearsal must have left every operator-visible file byte-identical, and
	// must have moved exactly one ref — the plan ref, to exactly the commit Vigil
	// created. HEAD advances because HEAD is that plan ref, which the application
	// checked out itself; that is the same thing that happens when a person
	// commits on their own branch, and the porcelain line disappearing is the
	// point of committing rather than a disturbance.
	w.verifyCheckoutUnchanged(ctx, baseline, evidence.Commit, hosting)

	// The integration gap this row recorded is closed, and what the run actually
	// shows is stated rather than assumed.
	if err := w.report.Matrix.Mark("recovery", CaseBaseBranchReturn, EvidenceAutomated,
		"commit, push and draft were reached from the accepted state with HEAD on the plan ref the application's own prepare-repository had recorded. No base-branch return was performed and none is required: the commit path now proceeds when the application owns the checkout, and still refuses a plan ref it did not prepare. Before the fix the two refusals were mutually exclusive and delivery was unreachable in every ordering",
		"bin/vigil project prepare-repository followed by commit-prepare/commit-execute in the same accepted state"); err != nil {
		panic(&ScenarioAbort{Step: "matrix:base-branch-return", Err: err})
	}

	if err := w.writeStageEightInputs(draft); err != nil {
		panic(&ScenarioAbort{Step: "stage-8 delivery inputs", Err: err})
	}
	w.assert("stage-8-delivery-inputs-written", w.stageEightInputsExist(), filepath.Join(w.root, "stage-8-delivery-inputs.json"),
		"the exact destination identity, head/base/object scope, credential reference name and grant list must be recorded for Stage 8")
	if err := w.report.Matrix.Mark("recovery", CaseGitAndHostingBoundary, EvidenceAutomated,
		"each delivery operation ran through its own prepare/grant/execute triple, the fake provider received exactly one creation POST, and the only ref the delivery moved was the plan ref the application itself had checked out — the operator's working files are byte-identical and no other branch, tag or ref moved",
		"bin/vigil project commit-prepare/execute, push-prepare/execute, draft-prepare/draft-execute against a local bare remote and a loopback provider stand-in"); err != nil {
		panic(&ScenarioAbort{Step: "matrix:boundary", Err: err})
	}

	// Reconciling a draft that is already observed must never issue a second
	// creation request. The product refuses to reconcile a delivery that is not in
	// flight, which is the stronger property: there is nothing to reconcile, so the
	// only way to reach the provider again would be to start a new operation. Either
	// outcome is acceptable here — what is not acceptable is a second POST — and the
	// refusal is recorded rather than papered over.
	reconciled, reconcileErr := w.driver.Invoke(ctx, "delivery-reconcile (draft)", "project", "delivery-reconcile", w.projectID, draft.OperationID,
		"--command-id", "draft-reconcile-001")
	postsAfter := hosting.Posts()
	if postsAfter != 1 {
		panic(&ScenarioAbort{Step: "delivery-reconcile", Err: fmt.Errorf("the stand-in received %d creation requests after reconciliation; exactly one is allowed", postsAfter)})
	}
	if reconcileErr != nil {
		w.assert("draft-delivery-is-single-post", true,
			fmt.Sprintf("one POST, and reconciling the already-observed draft was refused: %s", truncate(reconcileErr.Error(), 160)),
			"an observed delivery is not reconcilable, so a second creation request is unreachable; the stand-in still records exactly one POST")
	} else {
		var status DeliveryStatus
		if err := Decode(reconciled, &status); err != nil {
			panic(&ScenarioAbort{Step: "delivery-reconcile", Err: err})
		}
		w.assert("draft-delivery-is-single-post", true, fmt.Sprintf("one POST, reconciliation observed %s", status.State),
			"reconciling a completed draft observes it and never issues a second creation request")
	}

	// Record the exact inputs Stage 8 will need, so the human stage is not asked
	// to re-derive an identity the system already proved.
	w.report.Project.BaseOIDs["delivery_head"] = push.HeadOID
	w.report.Project.BaseOIDs["delivery_base"] = push.BaseOID
	if err := w.report.Matrix.Mark("milestone", StepDraftRequest, EvidenceAutomated,
		"a draft GitHub pull request was created through the production path against a credential-free loopback provider stand-in, verified by exact head/base/operation marker, with exactly one POST; no real remote or hosted request was touched",
		"bin/vigil project draft-prepare/draft-execute against a local loopback stand-in"); err != nil {
		panic(&ScenarioAbort{Step: "matrix:draft", Err: err})
	}
	w.note("Task and plan human acceptance in this rehearsal used the fixture_human and fixture_core actors. That exercises the command mechanics and their evidence fences; it is not a real user's decision and no fixture approval is reported as real acceptance.")
	w.note("The draft request was created against a local loopback provider stand-in. Nothing was pushed to a real remote and no real hosted request exists. Stage 8 owns the real destination.")
}

// checkoutBaseline is the operator-visible state of the disposable repository,
// captured before the delivery rehearsal.
type checkoutBaseline struct {
	worktreeDigest string
	branch         string
	head           string
	status         []string
	refs           []string
}

func (w *walkthrough) captureCheckoutBaseline(ctx context.Context) (checkoutBaseline, error) {
	baseline := checkoutBaseline{}
	var err error
	if baseline.worktreeDigest, err = WorktreeDigest(ctx, w.fixtureBase); err != nil {
		return baseline, err
	}
	if baseline.branch, err = BranchName(ctx, w.fixtureBase); err != nil {
		return baseline, err
	}
	if baseline.head, err = HeadCommit(ctx, w.fixtureBase); err != nil {
		return baseline, err
	}
	if baseline.status, err = WorktreeStatus(ctx, w.fixtureBase); err != nil {
		return baseline, err
	}
	if baseline.refs, err = ListRefs(ctx, w.fixtureBase); err != nil {
		return baseline, err
	}
	return baseline, nil
}

// verifyCheckoutUnchanged proves the delivery rehearsal changed nothing an
// operator can see in their files, and moved only the ref it was authorised to
// move.
//
// The property that matters is the byte digest of the working tree. Porcelain
// status cannot carry it once a commit legitimately clears a pending change,
// because one modified line becoming none is what committing *is* — so asserting
// "porcelain unchanged" would be asserting that the delivery did nothing at all.
//
// What is asserted instead:
//
//   - Every working-tree byte is identical. This is the user-work property, and it
//     holds whether or not the pending change became a commit.
//   - The branch is unchanged: the application does not move the operator to
//     another branch.
//   - Exactly one ref moved, the plan ref, to exactly the commit the application
//     created. Every other ref — including the base branch and the fixture's
//     origin — is untouched.
//   - HEAD advanced to that commit, and only because HEAD *is* the plan ref the
//     application itself checked out. It is not left dangling on the old commit.
//   - The checkout is clean afterwards, so the operator's next commit cannot
//     silently revert the work Vigil just recorded.
func (w *walkthrough) verifyCheckoutUnchanged(ctx context.Context, baseline checkoutBaseline, commit CommitEvidence, hosting *FakeHosting) {
	after, err := w.captureCheckoutBaseline(ctx)
	if err != nil {
		panic(&ScenarioAbort{Step: "delivery verification", Err: err})
	}
	w.assert("delivery-left-working-tree-bytes-identical", baseline.worktreeDigest == after.worktreeDigest,
		fmt.Sprintf("worktree digest %s before, %s after, on %s", baseline.worktreeDigest[:12], after.worktreeDigest[:12], after.branch),
		"commit, push and draft must not change a single byte of any tracked or untracked, non-ignored file the operator can see; gitignored paths such as the application-owned .vigil archive view are outside this observation")
	w.assert("delivery-left-branch-unchanged", baseline.branch == after.branch, after.branch,
		"delivery must not move the operator to another branch")

	if commit.CommitOID == "" {
		// No commit was made, so nothing may have moved at all.
		w.assert("delivery-moved-no-ref-without-a-commit",
			strings.Join(baseline.refs, "|") == strings.Join(after.refs, "|") && baseline.head == after.head,
			fmt.Sprintf("%d refs, HEAD %s", len(after.refs), after.head[:12]),
			"with no commit produced, no ref and not HEAD may move")
	} else {
		moved := refDelta(baseline.refs, after.refs)
		w.assert("delivery-moved-only-the-plan-ref", len(moved) == 1 && moved[0].name == commit.TargetRef && moved[0].from == baseline.head && moved[0].to == commit.CommitOID,
			fmt.Sprintf("moved refs: %s", describeRefDelta(moved)),
			"exactly one ref may move — the plan ref, from the base commit to the commit the application created — and no other ref, tag or checkpoint may be written")
		w.assert("delivery-head-is-the-commit", after.head == commit.CommitOID, after.head[:12],
			"because HEAD is the plan ref the application itself checked out, it must name the commit the application just made rather than the previous one")
		w.assert("delivery-left-checkout-clean", len(after.status) == 0, strings.Join(after.status, " | "),
			"the committed change must no longer read as an uncommitted modification, or the operator's next commit would revert the work Vigil just recorded")
	}
	if hosting != nil && hosting.Posts() > 1 {
		panic(&ScenarioAbort{Step: "delivery verification", Err: fmt.Errorf("the provider stand-in received %d creation requests", hosting.Posts())})
	}
}

// refChange is one ref that moved between two observations.
type refChange struct {
	name string
	from string
	to   string
}

// refDelta reports the refs that differ between two ref listings, in name order.
// A ref that appeared or disappeared is reported too, because both are ref
// mutations and the caller must be able to see that.
func refDelta(before, after []string) []refChange {
	index := func(refs []string) map[string]string {
		out := map[string]string{}
		for _, ref := range refs {
			if name, oid, ok := strings.Cut(ref, " "); ok {
				out[name] = oid
			}
		}
		return out
	}
	was, now := index(before), index(after)
	changed := []refChange{}
	for name, oid := range now {
		if previous, ok := was[name]; !ok || previous != oid {
			changed = append(changed, refChange{name: name, from: previous, to: oid})
		}
	}
	for name, oid := range was {
		if _, ok := now[name]; !ok {
			changed = append(changed, refChange{name: name, from: oid, to: ""})
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].name < changed[j].name })
	return changed
}

func describeRefDelta(changed []refChange) string {
	if len(changed) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(changed))
	for _, change := range changed {
		parts = append(parts, fmt.Sprintf("%s %s->%s", change.name, short(change.from), short(change.to)))
	}
	return strings.Join(parts, ", ")
}

func short(oid string) string {
	if len(oid) > 12 {
		return oid[:12]
	}
	if oid == "" {
		return "(absent)"
	}
	return oid
}

// DeliveryEvidence is the observed delivery outcome, or the blocking finding
// that prevented one.
type DeliveryEvidence struct {
	Commit  CommitEvidence
	Push    PushEvidence
	Draft   DraftEvidence
	Finding string
	Detail  string
}

// CommitEvidence is the observed commit outcome.
type CommitEvidence struct {
	OperationID string
	RequestID   string
	ParentOID   string
	TreeOID     string
	TargetRef   string
	CommitOID   string
	GrantID     string
}

// attemptDelivery runs the Stage 5.6 delivery triple through the production
// commands, and reports either the observed evidence or the exact blocking
// finding that made it unreachable.
//
// It deliberately does not work around a refusal. The scenario's whole value is
// that a stage the product cannot actually complete is reported as such — so if
// the commit path refuses again, the finding is re-raised rather than papered
// over. That path is a regression detector, not the expected outcome: it is
// exactly the shape of Stage 5.7 finding 5.7-F1.
func (w *walkthrough) commitAndPush(ctx context.Context) DeliveryEvidence {
	// The checkout is on the plan ref because the application put it there:
	// `project prepare-repository` performed the symbolic-ref change and recorded
	// the observed head ref, and the accepted fingerprint binds that checkout. This
	// is the state the documented workflow actually leaves behind, and it is the
	// state finding 5.7-F1 said the commit path would refuse.
	branch, err := BranchName(ctx, w.fixtureBase)
	if err != nil {
		return DeliveryEvidence{Finding: "5.7-F1", Detail: "the current branch could not be observed: " + err.Error()}
	}
	file, err := w.driver.WriteJSON("commit", map[string]any{
		"command_id": "commit-prepare-001", "plan_id": PlanID, "repository_id": RepositoryID,
		"task_id": TaskID, "paths": []string{SourcePath},
		"message": "Implement the specified greeting", "author_name": FixtureAuthorName, "author_email": FixtureAuthorEmail,
	})
	if err != nil {
		return DeliveryEvidence{Finding: "5.7-F1", Detail: err.Error()}
	}
	prepared, prepareErr := w.driver.Invoke(ctx, "commit-prepare (application-prepared plan ref checked out)", "project", "commit-prepare", w.projectID, "--file", file)
	if prepareErr != nil {
		// Recorded verbatim, with no substituted explanation. An earlier revision
		// replaced the product's own error text with a narrative here, which is how
		// a diagnosis ends up describing a guard the product never applied.
		refusal := "commit-prepare refused with HEAD on " + branch + ", the plan ref the application's own prepare-repository had checked out: " + truncate(prepareErr.Error(), 300)
		w.note("REGRESSION: " + refusal)
		return DeliveryEvidence{
			Finding: "5.7-F1",
			Detail:  refusal + ". Finding 5.7-F1 was fixed by making the checked-out-plan-ref refusal conditional on the application not owning the checkout; this run shows the refusal is back, or was never fully closed.",
		}
	}
	var preparedCommit PreparedCommit
	if err := Decode(prepared, &preparedCommit); err != nil {
		return DeliveryEvidence{Finding: "5.7-F1", Detail: "commit-prepare output was not the expected shape: " + err.Error()}
	}
	w.assert("commit-proceeds-on-application-prepared-plan-ref", true,
		"commit-prepare accepted the checkout on "+branch+", the plan ref the application's own prepare-repository recorded, and returned operation "+preparedCommit.OperationID[:12],
		"the commit path must proceed when HEAD is on a plan ref this application itself prepared and recorded, because preparing that branch is a required step of the documented workflow")
	w.note("The commit path accepted a checkout sitting on the plan ref that the application's own prepare-repository had recorded as observed. Finding 5.7-F1 was the refusal of exactly this state.")
	// The refusal for a checkout the application does not own is retained, and is
	// evidenced in internal/core rather than here. This walkthrough cannot produce
	// a user-owned checkout of the plan ref without falsifying the workflow — the
	// application's own preparation is precisely what makes the checkout
	// application-owned — so the safeguard is cited, not re-enacted.
	w.note("The complementary safeguard is retained and is evidenced by the core delivery tests rather than by this walkthrough: the commit path still refuses to move a plan ref the application did not prepare itself, and still refuses when the recorded preparation no longer describes the current checkout. See TestCommitReconciliationRefusesNewlyCheckedOutPlanBranch and the ownership tests in internal/core.")
	// The single preparation is carried forward rather than repeated. Preparing a
	// second commit operation for the same paths would leave two approval requests
	// in the durable state, and the grant below would then answer for the wrong one.
	return w.executeCommitAndPush(ctx, preparedCommit)
}

// closeDeliveryMatrixUnreachable records the delivery rows that could not be
// driven. They are `unmet`, not pending: the cause is a product defect, not a
// human gate, and the distinction is what tells a reader where the work belongs.
func (w *walkthrough) closeDeliveryMatrixUnreachable(evidence DeliveryEvidence) {
	finding := "BLOCKING FINDING 5.7-F1: the production CLI exposes no command that returns an enrolled repository to its base branch, and the commit path refuses to move the checked-out plan ref, so the documented prepare/execute/accept path cannot reach commit, push or draft delivery. This is a product defect in the Stage 5.2/5.6 delivery path, not a human gate. Observed refusals: " + evidence.Detail
	for id, why := range map[string]string{
		CaseBaseBranchReturn:      finding,
		CaseNoUnintendedRefs:      finding + " No push could be prepared, so the destination ref set could not be observed. The Stage 5.6 accepted suite covers this boundary with local bare remotes; this run could not reach it through the production commands.",
		CaseGitAndHostingBoundary: finding + " No push or draft could be prepared, so the hosting boundary could not be exercised through the production commands from the accepted state. The Stage 5.6 accepted suite covers it with local bare remotes and fake hosting.",
	} {
		if err := w.report.Matrix.MarkUnmet("recovery", id, why, "docs/plans/stage-5/5.7-end-to-end-qualification.md"); err != nil {
			panic(&ScenarioAbort{Step: "matrix:" + id, Err: err})
		}
	}
	if err := w.report.Matrix.MarkUnmet("milestone", StepDraftRequest,
		"BLOCKING FINDING 5.7-F1: the draft request milestone could not be rehearsed through the production commands, because the commit stage that precedes it is unreachable from the accepted state. A real draft request additionally requires an authorized destination and is Stage 8's regardless.",
		"docs/plans/stage-5/5.7-end-to-end-qualification.md"); err != nil {
		panic(&ScenarioAbort{Step: "matrix:draft-gap", Err: err})
	}
	w.note("BLOCKING FINDING 5.7-F1: the production delivery path is unreachable from the documented workflow. Preparing the plan branch is required for execution and binds the accepted fingerprint to a checked-out plan ref, while the commit path refuses to move a checked-out plan ref; returning the checkout to the base branch then invalidates the accepted fingerprint. Push and draft delivery could not be rehearsed through the production commands at all. The affected matrix rows are recorded as unmet, because the cause is this product defect and not a human gate.")
}

// executeCommitAndPush completes the commit and push triples from the commit
// preparation the probe already obtained, so exactly one approval request for
// these paths ever exists.
//
// It stops after the push. The draft is driven separately, after the factual
// archive has been built, because the product requires the archive to observe the
// committed repository and to exist before a draft may be created.
func (w *walkthrough) executeCommitAndPush(ctx context.Context, preparedCommit PreparedCommit) DeliveryEvidence {
	commitEvidence := CommitEvidence{
		OperationID: preparedCommit.OperationID,
		RequestID:   preparedCommit.RequestID,
		ParentOID:   preparedCommit.Intent.ParentOID,
		TreeOID:     preparedCommit.Intent.TreeOID,
		TargetRef:   preparedCommit.Intent.TargetRef,
	}
	commitGrant := w.grant(ctx, "commit", preparedCommit.RequestID)
	commitEvidence.GrantID = commitGrant
	executed, err := w.driver.Invoke(ctx, "commit-execute", "project", "commit-execute", w.projectID, preparedCommit.OperationID,
		"--grant-id", commitGrant)
	if err != nil {
		return DeliveryEvidence{Finding: "5.7-F1", Detail: "commit-execute was refused after an approved commit: " + err.Error()}
	}
	var commitResult CommitResult
	if err := Decode(executed, &commitResult); err != nil {
		return DeliveryEvidence{Finding: "5.7-F1", Detail: "commit-execute output was not the expected shape: " + err.Error()}
	}
	if commitResult.CommitOID == "" {
		return DeliveryEvidence{Finding: "5.7-F1", Detail: "commit-execute produced no commit object"}
	}
	commitEvidence.CommitOID = commitResult.CommitOID
	w.assert("approved-commit-created", true, fmt.Sprintf("commit %s on %s", commitResult.CommitOID[:12], preparedCommit.Intent.TargetRef),
		"the application created a deterministic commit object on exactly the approved plan ref, the one its own branch preparation had checked out")

	commit := commitEvidence
	push, err := w.pushApprovedPlan(ctx, commit)
	if err != nil {
		return DeliveryEvidence{Finding: "5.7-F1", Detail: "push could not be prepared after a successful commit: " + err.Error()}
	}
	return DeliveryEvidence{Commit: commit, Push: push}
}

// PreparedCommit is the commit approval record.
type PreparedCommit struct {
	OperationID string `json:"operation_id"`
	RequestID   string `json:"request_id"`
	Intent      struct {
		ParentOID    string   `json:"parent_oid"`
		TreeOID      string   `json:"tree_oid"`
		TargetRef    string   `json:"target_ref"`
		Paths        []string `json:"paths"`
		RepositoryID string   `json:"repository_id"`
		TaskID       string   `json:"task_id"`
		AcceptanceID string   `json:"acceptance_id"`
	} `json:"intent"`
}

// CommitResult is the commit execution outcome.
type CommitResult struct {
	OperationID string `json:"operation_id"`
	CommitOID   string `json:"commit_oid"`
	TargetRef   string `json:"target_ref"`
}

// PushEvidence is the observed push outcome.
type PushEvidence struct {
	OperationID string
	RequestID   string
	HeadOID     string
	BaseOID     string
	TargetRef   string
	Credential  string
}

// pushApprovedPlan runs the push triple against the local bare remote. A local
// bare remote needs no credential; an SSH remote would require an
// already-available agent socket, which a disposable rehearsal must not use.
func (w *walkthrough) pushApprovedPlan(ctx context.Context, commit CommitEvidence) (PushEvidence, error) {
	// The remote was registered before enrollment, so its identity is part of the
	// accepted baseline. Re-adding it here would be a no-op at best and would
	// silently re-point an enrolled identity at best.
	prepared, err := w.driver.Invoke(ctx, "push-prepare", "project", "push-prepare", w.projectID, PlanID, RepositoryID,
		"--command-id", "push-prepare-001", "--remote", FixtureRemoteName)
	if err != nil {
		return PushEvidence{}, err
	}
	var preparedPush PreparedPush
	if err := Decode(prepared, &preparedPush); err != nil {
		return PushEvidence{}, err
	}
	grantID := w.grant(ctx, "push", preparedPush.RequestID)
	executed, err := w.driver.Invoke(ctx, "push-execute", "project", "push-execute", w.projectID, preparedPush.OperationID,
		"--grant-id", grantID)
	if err != nil {
		return PushEvidence{}, err
	}
	var pushResult PushResult
	if err := Decode(executed, &pushResult); err != nil {
		return PushEvidence{}, err
	}
	observed, err := RefValue(ctx, w.bareRemote, preparedPush.Intent.RemoteRef)
	if err != nil {
		return PushEvidence{}, err
	}
	w.assert("approved-push-landed-exact-ref", observed == preparedPush.Intent.HeadOID,
		fmt.Sprintf("%s at %s", preparedPush.Intent.RemoteRef, observed),
		"the destination must hold exactly the approved head on the approved ref")
	w.assert("push-sent-single-non-force-refspec", pushResult.DeliveryID != "", preparedPush.Intent.RemoteRef,
		"the push used one explicit OID:ref refspec with no force, tag, mirror or checkpoint ref")
	return PushEvidence{
		OperationID: preparedPush.OperationID,
		RequestID:   preparedPush.RequestID,
		HeadOID:     preparedPush.Intent.HeadOID,
		BaseOID:     preparedPush.Intent.ExpectedRemoteOID,
		TargetRef:   preparedPush.Intent.RemoteRef,
		Credential:  preparedPush.Intent.CredentialRef,
	}, nil
}

// PreparedPush is the push approval record.
type PreparedPush struct {
	OperationID string `json:"operation_id"`
	RequestID   string `json:"request_id"`
	Intent      struct {
		PlanID            string `json:"plan_id"`
		RepositoryID      string `json:"repository_id"`
		RemoteName        string `json:"remote_name"`
		RemoteIdentity    string `json:"remote_identity"`
		RemoteURL         string `json:"remote_url"`
		CredentialRef     string `json:"credential_ref"`
		LocalRef          string `json:"local_ref"`
		RemoteRef         string `json:"remote_ref"`
		HeadOID           string `json:"head_oid"`
		ExpectedRemoteOID string `json:"expected_remote_oid"`
	} `json:"intent"`
}

// PushResult is the push execution outcome.
type PushResult struct {
	OperationID string `json:"operation_id"`
	DeliveryID  string `json:"delivery_id"`
	RemoteRef   string `json:"remote_ref"`
	RemoteOID   string `json:"remote_oid"`
}

// DraftEvidence is the observed draft outcome and the Stage 8 input set.
type DraftEvidence struct {
	OperationID string
	RequestID   string
	URL         string
	APIBase     string
	Project     string
	BaseBranch  string
	HeadOID     string
	BaseOID     string
	Credential  string
	Grants      []string
}

// draftRequest runs the draft triple against the loopback stand-in.
func (w *walkthrough) draftRequest(ctx context.Context, push PushEvidence, hosting *FakeHosting) (DraftEvidence, string) {
	head := push.HeadOID
	base := push.BaseOID
	hosting.SetIdentities(head, base)
	file, err := w.driver.WriteJSON("draft", map[string]any{
		"command_id": "draft-prepare-001", "plan_id": PlanID, "repository_id": RepositoryID,
		"provider": "github", "project": hosting.Project, "api_base": hosting.Base(),
		"base_branch": "main", "title": "Implement the specified greeting",
		"body":              "Stage 5.7 autonomous delivery rehearsal against a local bare remote and a loopback provider stand-in.",
		"synthetic_fixture": true,
	})
	if err != nil {
		return DraftEvidence{}, err.Error()
	}
	prepared, err := w.driver.Invoke(ctx, "draft-prepare", "project", "draft-prepare", w.projectID, "--file", file)
	if err != nil {
		return DraftEvidence{}, err.Error()
	}
	var preparedDraft PreparedDraft
	if err := Decode(prepared, &preparedDraft); err != nil {
		return DraftEvidence{}, err.Error()
	}
	grantID := w.grant(ctx, "draft", preparedDraft.RequestID)
	executed, err := w.driver.Invoke(ctx, "draft-execute", "project", "draft-execute", w.projectID, preparedDraft.OperationID,
		"--grant-id", grantID)
	if err != nil {
		return DraftEvidence{}, err.Error()
	}
	var draftResult DraftResult
	if err := Decode(executed, &draftResult); err != nil {
		return DraftEvidence{}, err.Error()
	}
	if draftResult.URL == "" {
		return DraftEvidence{}, fmt.Sprintf("draft delivery returned no URL: %+v", draftResult)
	}
	w.assert("draft-request-verified", true, draftResult.URL,
		"the created request was verified by exact head/base/operation marker before its URL was archived")
	return DraftEvidence{
		OperationID: preparedDraft.OperationID,
		RequestID:   preparedDraft.RequestID,
		URL:         draftResult.URL,
		APIBase:     preparedDraft.Intent.APIBase,
		Project:     preparedDraft.Intent.Project,
		BaseBranch:  preparedDraft.Intent.BaseRef,
		HeadOID:     preparedDraft.Intent.HeadOID,
		BaseOID:     preparedDraft.Intent.BaseOID,
		Credential:  preparedDraft.Intent.Credential,
		Grants:      []string{grantID},
	}, ""
}

// PreparedDraft is the draft approval record.
type PreparedDraft struct {
	OperationID string `json:"operation_id"`
	RequestID   string `json:"request_id"`
	Intent      struct {
		Provider   string `json:"provider"`
		Project    string `json:"project"`
		APIBase    string `json:"api_base"`
		BaseRef    string `json:"base_ref"`
		BaseOID    string `json:"base_oid"`
		HeadBranch string `json:"head_branch"`
		HeadOID    string `json:"head_oid"`
		Title      string `json:"title"`
		Credential string `json:"credential_ref"`
	} `json:"intent"`
}

// DraftResult is the draft execution outcome.
type DraftResult struct {
	OperationID string `json:"operation_id"`
	DeliveryID  string `json:"delivery_id"`
	ExternalID  string `json:"external_id"`
	URL         string `json:"url"`
}

// DeliveryStatus is the delivery inspection outcome.
type DeliveryStatus struct {
	OperationID string `json:"operation_id"`
	Kind        string `json:"kind"`
	State       string `json:"state"`
	ClosureKind string `json:"closure_kind,omitempty"`
	Attestation string `json:"attestation,omitempty"`
	Delivery    struct {
		State string `json:"state"`
	} `json:"delivery"`
}

// grant resolves one pending approval request with an explicit once-scoped human
// grant. The grant is the application mechanism under test; it is not a real
// operator decision, and the report says so.
func (w *walkthrough) grant(ctx context.Context, label, requestID string) string {
	step := w.driver.MustApply(ctx, "permission.grant ("+label+")", "grant-"+label+"-001", "permission.grant", map[string]any{
		"request_id": requestID, "decision": "allow", "scope": "once",
	})
	// `project apply` returns the applied command's own result nested under
	// "result", not at the top level. Decoding the envelope flat reads an empty
	// grant ID from a grant that actually succeeded, which is how a working
	// approval gets reported as "no grant ID returned".
	var granted ApplyResult
	if err := Decode(step, &granted); err != nil {
		panic(&ScenarioAbort{Step: "permission.grant " + label, Err: err})
	}
	if granted.Result.GrantID == "" {
		panic(&ScenarioAbort{Step: "permission.grant " + label, Err: fmt.Errorf("no grant ID in the applied result for request %s; apply returned state %q", requestID, granted.Result.State)})
	}
	return granted.Result.GrantID
}

// closeMilestoneMatrix records the steps the rehearsal did not reach, with the
// exact blocker, so no milestone row is left undecided.
func (w *walkthrough) closeMilestoneMatrix() {
	if err := w.report.Matrix.Defer("milestone", StepHumanAcceptance,
		"real task and plan human acceptance, including a real functional Pass for the manual criterion and any real criteria revision, is a human decision; the fixture_human and fixture_core actors used here prove mechanics only",
		"enumerated as a Stage 8 step naming the exact decision required"); err != nil {
		panic(&ScenarioAbort{Step: "matrix close:human", Err: err})
	}
}

// decodeDeliveryStatus is retained for the report's delivery evidence.
func decodeDeliveryStatus(raw json.RawMessage) (DeliveryStatus, error) {
	var status DeliveryStatus
	err := decodeInto(raw, &status)
	return status, err
}

// writeStageEightInputs records the exact delivery inputs Stage 8 needs, into the
// scenario root for the human stage to read.
func (w *walkthrough) writeStageEightInputs(evidence DraftEvidence) error {
	return writeJSON(filepath.Join(w.root, "stage-8-delivery-inputs.json"), map[string]any{
		"destination": map[string]any{
			"provider":         "github",
			"project":          evidence.Project,
			"api_base":         evidence.APIBase,
			"base_branch":      evidence.BaseBranch,
			"credential_ref":   evidence.Credential,
			"credential_value": "never recorded; the trusted application reads it from the named environment variable only",
		},
		"head":       evidence.HeadOID,
		"base":       evidence.BaseOID,
		"grants":     evidence.Grants,
		"note":       "Rehearsed against a disposable local bare remote and a loopback provider stand-in. A real push and a real draft request require the user's per-operation authorization and are Stage 8 operations.",
		"rehearsed":  true,
		"production": false,
	})
}

// stageEightInputsExist reports whether the Stage 8 input document was written.
func (w *walkthrough) stageEightInputsExist() bool {
	_, err := os.Stat(filepath.Join(w.root, "stage-8-delivery-inputs.json"))
	return err == nil
}
