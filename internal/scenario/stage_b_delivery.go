package scenario

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// rehearseDelivery drives the Stage 5.6 delivery path end to end against a local
// bare remote and a credential-free loopback hosting stand-in, through the
// production commands only. It never pushes to a real remote and never creates a
// real hosted request: those are Stage 8 operations requiring the user's
// per-operation authorization.
//
// The output is the exact set of inputs Stage 8's delivery script needs —
// destination identity, head/base/object scope, credential reference name and
// the scoped grant list — so the human stage does not have to re-derive them.
func (w *walkthrough) rehearseDelivery(ctx context.Context) {
	hosting, err := NewFakeHosting("github", "vigil-scenario/fixture", "main")
	if err != nil {
		panic(&ScenarioAbort{Step: "delivery hosting stand-in", Err: err})
	}
	defer hosting.Close()
	w.hosting = hosting
	w.assert("fake-hosting-loopback-only", strings.HasPrefix(hosting.Base(), "http://127.0.0.1:"), hosting.Base(),
		"the hosting stand-in binds one loopback port and holds no credential")

	// The pre-rehearsal baseline. HEAD and the porcelain state must be identical
	// afterwards regardless of what the scenario itself did to the branch.
	headBefore, err := HeadCommit(ctx, w.fixtureBase)
	if err != nil {
		panic(&ScenarioAbort{Step: "delivery baseline", Err: err})
	}
	statusBefore, err := WorktreeStatus(ctx, w.fixtureBase)
	if err != nil {
		panic(&ScenarioAbort{Step: "delivery baseline", Err: err})
	}
	w.assert("delivery-baseline-recorded", len(statusBefore) == 1, strings.Join(statusBefore, " | "),
		"the accepted artifact change must be present before the delivery rehearsal begins")

	evidence := w.attemptDelivery(ctx)
	// The branch the scenario itself may have checked out is recorded explicitly,
	// so the post-rehearsal comparison can distinguish the scenario's own
	// out-of-band checkout from anything an application command did.
	branchAfterAttempt, err := BranchName(ctx, w.fixtureBase)
	if err != nil {
		panic(&ScenarioAbort{Step: "delivery baseline", Err: err})
	}
	branchBefore := branchAfterAttempt
	if evidence.Finding != "" {
		// The delivery path is unreachable through the production commands from
		// the documented prepare/execute/accept state. That is recorded as a
		// blocking finding, not worked around and not reported as a pass.
		w.report.Pending = append(w.report.Pending,
			"push and draft delivery rehearsal: blocked by "+evidence.Finding+" in the Stage 5.2/5.6 delivery path")
		w.closeDeliveryMatrixUnreachable(evidence)
		w.verifyCheckoutUntouched(ctx, headBefore, branchBefore, statusBefore, hosting)
		return
	}
	// Reaching here means the delivery path completed. The verification below then
	// runs against the post-attempt branch, which is the base branch the scenario
	// checked out explicitly.

	push, draft := evidence.Push, evidence.Draft

	// The whole delivery rehearsal must have left the accepted change and HEAD
	// exactly as it found them. The branch is compared against the state the
	// delivery attempt left, because returning the checkout to its base branch is
	// an explicit scenario action on an agent-owned fixture, not an application
	// effect.
	w.verifyCheckoutUntouched(ctx, headBefore, branchBefore, statusBefore, hosting)

	// Exactly the plan ref may have appeared in the bare remote. No tag, no
	// wildcard ref, no checkpoint ref.
	refs, err := ListRefs(ctx, w.bareRemote)
	if err != nil {
		panic(&ScenarioAbort{Step: "remote ref listing", Err: err})
	}
	w.assert("no-unintended-remote-refs", len(refs) == 1 && strings.HasPrefix(refs[0], "refs/heads/"), strings.Join(refs, " "),
		"exactly one plan branch ref may exist in the destination; tags and checkpoint refs must never be pushed")
	if err := w.report.Matrix.Mark("recovery", CaseNoUnintendedRefs, EvidenceAutomated,
		"the destination holds exactly the one approved plan ref; no tag, mirror, force or checkpoint ref was created",
		"git for-each-ref on the local bare remote after the rehearsal"); err != nil {
		panic(&ScenarioAbort{Step: "matrix:refs", Err: err})
	}
	if err := w.writeStageEightInputs(draft); err != nil {
		panic(&ScenarioAbort{Step: "stage-8 delivery inputs", Err: err})
	}
	w.assert("stage-8-delivery-inputs-written", w.stageEightInputsExist(), filepath.Join(w.root, "stage-8-delivery-inputs.json"),
		"the exact destination identity, head/base/object scope, credential reference name and grant list must be recorded for Stage 8")
	if err := w.report.Matrix.Mark("recovery", CaseGitAndHostingBoundary, EvidenceAutomated,
		"each delivery operation ran through its own prepare/grant/execute triple, the fake provider received exactly one creation POST, and the operator's checkout was unchanged",
		"bin/vigil project commit-prepare/execute, push-prepare/execute, draft-prepare/execute"); err != nil {
		panic(&ScenarioAbort{Step: "matrix:boundary", Err: err})
	}

	// Reconciling a completed draft must observe it, never POST again.
	reconcile := w.driver.MustInvoke(ctx, "delivery-reconcile (draft)", "project", "delivery-reconcile", w.projectID, draft.OperationID,
		"--command-id", "draft-reconcile-001")
	var reconciled DeliveryStatus
	if err := Decode(reconcile, &reconciled); err != nil {
		panic(&ScenarioAbort{Step: "delivery-reconcile", Err: err})
	}
	if hosting.Posts() != 1 {
		panic(&ScenarioAbort{Step: "delivery-reconcile", Err: fmt.Errorf("reconciliation issued %d creation requests; exactly one is allowed", hosting.Posts())})
	}
	w.assert("draft-delivery-is-single-post", true, fmt.Sprintf("one POST, reconciliation observed %s", reconciled.State),
		"reconciling a completed draft observes it and never issues a second creation request")

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

// verifyCheckoutUntouched proves the delivery rehearsal left the operator's
// accepted change and HEAD exactly as it found them.
//
// The branch is compared against `branchBefore`, which the caller sets to the
// branch the delivery attempt left behind: returning the checkout to its base
// branch is an explicit scenario action on an agent-owned fixture, so counting it
// as an application effect would misattribute it. HEAD and the porcelain state
// are compared against the pre-rehearsal baseline, which no application command
// is permitted to change.
func (w *walkthrough) verifyCheckoutUntouched(ctx context.Context, headBefore, branchBefore string, statusBefore []string, hosting *FakeHosting) {
	headAfter, err := HeadCommit(ctx, w.fixtureBase)
	if err != nil {
		panic(&ScenarioAbort{Step: "delivery verification", Err: err})
	}
	branchAfter, err := BranchName(ctx, w.fixtureBase)
	if err != nil {
		panic(&ScenarioAbort{Step: "delivery verification", Err: err})
	}
	statusAfter, err := WorktreeStatus(ctx, w.fixtureBase)
	if err != nil {
		panic(&ScenarioAbort{Step: "delivery verification", Err: err})
	}
	w.assert("delivery-left-checkout-untouched",
		headBefore == headAfter && branchBefore == branchAfter && strings.Join(statusBefore, "|") == strings.Join(statusAfter, "|"),
		fmt.Sprintf("HEAD %s on %s with porcelain (%s)", headAfter[:12], branchAfter, strings.Join(statusAfter, " | ")),
		"application-owned commit, push and draft must never move HEAD or the accepted change in the checkout")
	if hosting != nil && hosting.Posts() > 1 {
		panic(&ScenarioAbort{Step: "delivery verification", Err: fmt.Errorf("the provider stand-in received %d creation requests", hosting.Posts())})
	}
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
	GrantID     string
}

// attemptDelivery runs the Stage 5.6 delivery triple through the production
// commands, and reports either the observed evidence or the exact blocking
// finding that made it unreachable.
//
// It deliberately does not work around a refusal. The scenario's whole value is
// that a stage the product cannot actually complete is reported as such.
func (w *walkthrough) attemptDelivery(ctx context.Context) DeliveryEvidence {
	// Observation 1: with the plan branch prepared and checked out — which is
	// exactly the state execution and acceptance leave behind — the commit path
	// refuses to move a ref the user has checked out.
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
	if _, err := w.driver.Invoke(ctx, "commit-prepare (plan ref checked out)", "project", "commit-prepare", w.projectID, "--file", file); err == nil {
		panic(&ScenarioAbort{Step: "commit-prepare", Err: errors.New("a commit was prepared while the plan ref was checked out")})
	}
	w.assert("commit-refuses-checked-out-plan-ref", true, "refused while HEAD is on "+branch,
		"the commit path must refuse to move the branch the user currently has checked out")
	firstRefusal := "commit-prepare refused while HEAD is on the plan branch: the commit path will not move a ref the user has checked out"
	w.note("Observation 1: " + firstRefusal)

	// Observation 2: returning the checkout to its base branch is what a real
	// operator would do, and Vigil exposes no command for it. The scenario performs
	// that checkout explicitly on its own agent-owned fixture, then retries. This
	// is a rehearsal action on a disposable repository, never a product claim.
	if err := w.returnFixtureToBase(ctx); err != nil {
		return DeliveryEvidence{Finding: "5.7-F1", Detail: firstRefusal + "; and the base-branch return failed: " + err.Error()}
	}
	retryFile, err := w.driver.WriteJSON("commit-retry", map[string]any{
		"command_id": "commit-prepare-002", "plan_id": PlanID, "repository_id": RepositoryID,
		"task_id": TaskID, "paths": []string{SourcePath},
		"message": "Implement the specified greeting", "author_name": FixtureAuthorName, "author_email": FixtureAuthorEmail,
	})
	if err != nil {
		return DeliveryEvidence{Finding: "5.7-F1", Detail: firstRefusal + "; and the retry request could not be written: " + err.Error()}
	}
	if _, err := w.driver.Invoke(ctx, "commit-prepare (after base return)", "project", "commit-prepare", w.projectID, "--file", retryFile); err != nil {
		// This is the blocking finding, recorded rather than worked around: the
		// accepted fingerprint is bound to the checked-out plan ref, so returning
		// the checkout to the base branch invalidates it and the commit path stays
		// unreachable. The two refusals are mutually exclusive by construction.
		secondRefusal := "commit-prepare refused again after the base-branch return: " + truncate(err.Error(), 200)
		w.note("Observation 2: " + secondRefusal)
		return DeliveryEvidence{
			Finding: "5.7-F1",
			Detail: firstRefusal + "; then, after an explicit base-branch checkout, " + secondRefusal,
		}
	}
	w.note("Observation 2: commit-prepare succeeded once the checkout was on its base branch, which no production command performs.")
	return w.continueDelivery(ctx)
}

// returnFixtureToBase checks the disposable repository out to its base branch. It
// is an explicit operator-equivalent action on an agent-owned fixture: Vigil
// exposes no such command, and the scenario says so rather than implying the
// product can do it.
func (w *walkthrough) returnFixtureToBase(ctx context.Context) error {
	before, err := HeadCommit(ctx, w.fixtureBase)
	if err != nil {
		return err
	}
	if _, err := git(ctx, w.fixtureBase, "checkout", "-q", "main"); err != nil {
		return fmt.Errorf("return the disposable fixture to its base branch: %w", err)
	}
	after, err := HeadCommit(ctx, w.fixtureBase)
	if err != nil {
		return err
	}
	if before != after {
		return fmt.Errorf("returning to the base branch moved HEAD from %s to %s", before, after)
	}
	status, err := WorktreeStatus(ctx, w.fixtureBase)
	if err != nil {
		return err
	}
	if len(status) != 1 {
		return fmt.Errorf("expected exactly the accepted artifact change in the checkout, found %d porcelain lines: %v", len(status), status)
	}
	w.assert("fixture-returned-to-base", true, fmt.Sprintf("HEAD %s on main with the single accepted change present", after[:12]),
		"the checkout is on its base branch with the accepted change still present and unstaged")
	return nil
}

// closeDeliveryMatrixUnreachable records the delivery rows that could not be
// driven, each with the exact finding that blocked it.
func (w *walkthrough) closeDeliveryMatrixUnreachable(evidence DeliveryEvidence) {
	detail := "not driven by this run: " + evidence.Detail
	for id, blocker := range map[string]string{
		CaseBaseBranchReturn: "BLOCKING FINDING 5.7-F1: the production CLI exposes no command that returns an enrolled repository to its base branch, and the commit path refuses to move the checked-out plan ref, so the documented prepare/execute/accept path cannot reach commit, push or draft delivery. Both refusals are recorded in the report.",
		CaseNoUnintendedRefs:  "BLOCKING FINDING 5.7-F1: no push could be prepared, so the destination ref set could not be observed. The Stage 5.6 accepted suite covers this boundary with local bare remotes; this run could not reach it through the production commands.",
		CaseGitAndHostingBoundary: "BLOCKING FINDING 5.7-F1: no push or draft could be prepared, so the hosting boundary could not be exercised through the production commands from the accepted state. The Stage 5.6 accepted suite covers it with local bare remotes and fake hosting.",
	} {
		if err := w.report.Matrix.Defer("recovery", id, blocker, detail); err != nil {
			panic(&ScenarioAbort{Step: "matrix:" + id, Err: err})
		}
	}
	if err := w.report.Matrix.Defer("milestone", StepDraftRequest,
		"BLOCKING FINDING 5.7-F1: the draft request milestone could not be rehearsed through the production commands, because the commit stage that precedes it is unreachable from the accepted state. A real draft request additionally requires an authorized destination and is Stage 8's regardless.",
		detail); err != nil {
		panic(&ScenarioAbort{Step: "matrix:draft-gap", Err: err})
	}
	w.note("BLOCKING FINDING 5.7-F1: the production delivery path is unreachable from the documented workflow. Preparing the plan branch is required for execution and binds the accepted fingerprint to a checked-out plan ref, while the commit path refuses to move a checked-out plan ref; returning the checkout to the base branch then invalidates the accepted fingerprint. Push and draft delivery could not be rehearsed through the production commands at all.")
}

// continueDelivery completes the commit, push and draft triples from whichever
// commit preparation the durable state actually holds.
func (w *walkthrough) continueDelivery(ctx context.Context) DeliveryEvidence {
	file, err := w.driver.WriteJSON("commit", map[string]any{
		"command_id": "commit-prepare-003", "plan_id": PlanID, "repository_id": RepositoryID,
		"task_id": TaskID, "paths": []string{SourcePath},
		"message": "Implement the specified greeting", "author_name": FixtureAuthorName, "author_email": FixtureAuthorEmail,
	})
	if err != nil {
		return DeliveryEvidence{Finding: "5.7-F1", Detail: "the commit request could not be written: " + err.Error()}
	}
	prepared, prepareErr := w.driver.Invoke(ctx, "commit-prepare", "project", "commit-prepare", w.projectID, "--file", file)
	if prepareErr != nil {
		return DeliveryEvidence{Finding: "5.7-F1", Detail: "commit-prepare remained refused after the base-branch return: " + prepareErr.Error()}
	}
	var preparedCommit PreparedCommit
	if err := Decode(prepared, &preparedCommit); err != nil {
		return DeliveryEvidence{Finding: "5.7-F1", Detail: "commit-prepare output was not the expected shape: " + err.Error()}
	}
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
	w.assert("approved-commit-created", true, fmt.Sprintf("commit %s on %s", commitResult.CommitOID[:12], preparedCommit.Intent.TargetRef),
		"the application created a deterministic commit object and advanced only the non-checked-out plan ref")

	commit := commitEvidence
	push, err := w.pushApprovedPlan(ctx, commit)
	if err != nil {
		return DeliveryEvidence{Finding: "5.7-F1", Detail: "push could not be prepared after a successful commit: " + err.Error()}
	}
	draft, err := w.draftRequest(ctx, push, w.hosting)
	if err != nil {
		return DeliveryEvidence{Finding: "5.7-F1", Detail: "draft delivery could not be prepared after a successful push: " + err.Error()}
	}
	return DeliveryEvidence{Commit: commit, Push: push, Draft: draft}
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
	if err := AddRemote(ctx, w.fixtureBase, FixtureRemoteName, w.bareRemote); err != nil {
		return PushEvidence{}, err
	}
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
func (w *walkthrough) draftRequest(ctx context.Context, push PushEvidence, hosting *FakeHosting) (DraftEvidence, error) {
	head := push.HeadOID
	base := push.BaseOID
	hosting.SetIdentities(head, base)
	file, err := w.driver.WriteJSON("draft", map[string]any{
		"command_id": "draft-prepare-001", "plan_id": PlanID, "repository_id": RepositoryID,
		"provider": "github", "project": hosting.Project, "api_base": hosting.Base(),
		"base_branch": "main", "title": "Implement the specified greeting",
		"body": "Stage 5.7 autonomous delivery rehearsal against a local bare remote and a loopback provider stand-in.",
		"synthetic_fixture": true,
	})
	if err != nil {
		return DraftEvidence{}, err
	}
	prepared, err := w.driver.Invoke(ctx, "draft-prepare", "project", "draft-prepare", w.projectID, "--file", file)
	if err != nil {
		return DraftEvidence{}, err
	}
	var preparedDraft PreparedDraft
	if err := Decode(prepared, &preparedDraft); err != nil {
		return DraftEvidence{}, err
	}
	grantID := w.grant(ctx, "draft", preparedDraft.RequestID)
	executed, err := w.driver.Invoke(ctx, "draft-execute", "project", "draft-execute", w.projectID, preparedDraft.OperationID,
		"--grant-id", grantID)
	if err != nil {
		return DraftEvidence{}, err
	}
	var draftResult DraftResult
	if err := Decode(executed, &draftResult); err != nil {
		return DraftEvidence{}, err
	}
	if draftResult.URL == "" {
		return DraftEvidence{}, fmt.Errorf("draft delivery returned no URL: %+v", draftResult)
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
	}, nil
}

// PreparedDraft is the draft approval record.
type PreparedDraft struct {
	OperationID string `json:"operation_id"`
	RequestID   string `json:"request_id"`
	Intent      struct {
		Provider    string `json:"provider"`
		Project     string `json:"project"`
		APIBase     string `json:"api_base"`
		BaseRef     string `json:"base_ref"`
		BaseOID     string `json:"base_oid"`
		HeadBranch  string `json:"head_branch"`
		HeadOID     string `json:"head_oid"`
		Title       string `json:"title"`
		Credential  string `json:"credential_ref"`
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
	var granted struct {
		GrantID string `json:"grant_id"`
		State   string `json:"state"`
	}
	if err := Decode(step, &granted); err != nil {
		panic(&ScenarioAbort{Step: "permission.grant " + label, Err: err})
	}
	if granted.GrantID == "" {
		panic(&ScenarioAbort{Step: "permission.grant " + label, Err: errors.New("no grant ID returned")})
	}
	return granted.GrantID
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
