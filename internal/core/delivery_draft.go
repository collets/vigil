package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"vigil/internal/store"
)

type DraftRequest struct {
	CommandID     string `json:"command_id"`
	PlanID        string `json:"plan_id"`
	RepositoryID  string `json:"repository_id"`
	Provider      string `json:"provider"`
	Project       string `json:"project"`
	APIBase       string `json:"api_base"`
	BaseBranch    string `json:"base_branch"`
	Title         string `json:"title"`
	Body          string `json:"body,omitempty"`
	CredentialRef string `json:"credential_ref,omitempty"`
	Fixture       bool   `json:"synthetic_fixture,omitempty"`
}

type DraftIntent struct {
	PlanID             string `json:"plan_id"`
	RepositoryID       string `json:"repository_id"`
	RepositoryRevision int    `json:"repository_revision"`
	Provider           string `json:"provider"`
	Project            string `json:"project"`
	APIBase            string `json:"api_base"`
	BaseBranch         string `json:"base_branch"`
	BaseOID            string `json:"base_oid"`
	HeadBranch         string `json:"head_branch"`
	HeadOID            string `json:"head_oid"`
	RemoteName         string `json:"remote_name"`
	RemoteIdentity     string `json:"remote_identity"`
	PlanAcceptanceID   string `json:"plan_acceptance_id"`
	ArchiveRevision    int    `json:"archive_revision"`
	Title              string `json:"title"`
	Body               string `json:"body,omitempty"`
	CredentialRef      string `json:"credential_ref,omitempty"`
	Fixture            bool   `json:"synthetic_fixture"`
	Draft              bool   `json:"draft"`
}

type PreparedDraft struct {
	OperationID string      `json:"operation_id"`
	RequestID   string      `json:"request_id"`
	Intent      DraftIntent `json:"intent"`
}

type DraftResult struct {
	OperationID string `json:"operation_id"`
	DeliveryID  string `json:"delivery_id"`
	ExternalID  string `json:"external_id"`
	URL         string `json:"url"`
	HeadOID     string `json:"head_oid"`
	State       string `json:"state"`
}

func draftDeliveryID(id string) string { return store.Digest([]byte("delivery.draft\x00" + id)) }

func realHostedProject(provider, rawRemote string) (string, error) {
	prefix := "git@github.com:"
	if provider == "gitlab" {
		prefix = "git@gitlab.com:"
	}
	if !strings.HasPrefix(rawRemote, prefix) {
		return "", errors.New("hosting provider does not match enrolled SSH remote")
	}
	project := strings.TrimSuffix(strings.TrimPrefix(rawRemote, prefix), ".git")
	if project == "" || strings.ContainsAny(project, "?%#\\:@") || strings.HasPrefix(project, "/") || strings.HasSuffix(project, "/") {
		return "", errors.New("invalid hosted project path")
	}
	parts := strings.Split(project, "/")
	if len(parts) < 2 || provider == "github" && len(parts) != 2 {
		return "", errors.New("invalid hosted project identity")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", errors.New("invalid hosted project path segment")
		}
	}
	return project, nil
}

func (e *Engine) draftContext(ctx context.Context, intent DraftIntent) (RepositoryRecord, hostingAdapter, error) {
	record, err := e.Repository(ctx, intent.RepositoryID)
	if err != nil || record.PlanID != intent.PlanID || record.Revision != intent.RepositoryRevision || record.RemoteName != intent.RemoteName || record.RemoteIdentity != intent.RemoteIdentity {
		return RepositoryRecord{}, nil, errors.New("enrolled draft repository or remote identity changed")
	}
	if err := record.Identity.Validate(); err != nil {
		return RepositoryRecord{}, nil, err
	}
	identity, rawRemote, env, err := observeRemoteConfig(ctx, record, intent.RemoteName, "")
	if err != nil || identity != intent.RemoteIdentity {
		// SSH Git auth is separate from hosting API auth. Its named agent is
		// required when observing the remote below.
		identity, rawRemote, env, err = observeRemoteConfig(ctx, record, intent.RemoteName, "env:SSH_AUTH_SOCK")
		if err != nil || identity != intent.RemoteIdentity {
			return RepositoryRecord{}, nil, errors.New("draft remote configuration changed")
		}
	}
	if !intent.Fixture {
		project, err := realHostedProject(intent.Provider, rawRemote)
		if err != nil || project != intent.Project {
			return RepositoryRecord{}, nil, errors.New("hosted project does not match enrolled remote")
		}
	} else if err := e.requireFixtureArchive(ctx, intent.PlanID); err != nil {
		return RepositoryRecord{}, nil, err
	}
	if intent.HeadBranch != record.PlanBranch || intent.BaseBranch == intent.HeadBranch || intent.BaseBranch == "" ||
		!intent.Draft || !gitOID(intent.HeadOID) || !gitOID(intent.BaseOID) {
		return RepositoryRecord{}, nil, errors.New("invalid exact draft branch/base/head")
	}
	var planState, acceptanceID, actor string
	if err := e.DB.SQL.QueryRowContext(ctx, `SELECT p.state,a.id,a.actor FROM plans p JOIN quality_acceptances_v2 a ON a.plan_id=p.id AND a.target_kind='plan' AND a.invalidated_at IS NULL WHERE p.id=?`, intent.PlanID).Scan(&planState, &acceptanceID, &actor); err != nil || planState != "finalization_pending" || acceptanceID != intent.PlanAcceptanceID {
		return RepositoryRecord{}, nil, errors.New("draft requires a current accepted plan in finalization pending")
	}
	if intent.Fixture != (actor == "fixture_core") {
		return RepositoryRecord{}, nil, errors.New("fixture provenance differs from plan acceptance")
	}
	var archiveRevision int
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT coalesce(max(revision),0) FROM archives WHERE plan_id=?", intent.PlanID).Scan(&archiveRevision); err != nil || archiveRevision != intent.ArchiveRevision {
		return RepositoryRecord{}, nil, errors.New("factual archive revision changed before draft")
	}
	var pushOID string
	if err := e.DB.SQL.QueryRowContext(ctx, `SELECT head_oid FROM deliveries WHERE plan_id=? AND repository_id=? AND kind='push' AND state='succeeded' AND remote_identity=? ORDER BY rowid DESC LIMIT 1`, intent.PlanID, intent.RepositoryID, intent.RemoteIdentity).Scan(&pushOID); err != nil || pushOID != intent.HeadOID {
		return RepositoryRecord{}, nil, errors.New("draft head is not the latest observed exact push")
	}
	remoteHead, err := observedRemoteRef(ctx, record, intent.RemoteName, "refs/heads/"+intent.HeadBranch, env)
	if err != nil || remoteHead != intent.HeadOID {
		return RepositoryRecord{}, nil, errors.New("draft remote head changed")
	}
	remoteBase, err := observedRemoteRef(ctx, record, intent.RemoteName, "refs/heads/"+intent.BaseBranch, env)
	if err != nil || remoteBase != intent.BaseOID {
		return RepositoryRecord{}, nil, errors.New("draft destination base changed")
	}
	adapter, err := newHostingAdapter(hostingSpec{Provider: intent.Provider, APIBase: intent.APIBase, Project: intent.Project,
		HeadRef: intent.HeadBranch, HeadOID: intent.HeadOID, BaseRef: intent.BaseBranch,
		Title: intent.Title, Body: intent.Body, OperationID: "placeholder", CredentialRef: intent.CredentialRef, Fixture: intent.Fixture})
	return record, adapter, err
}

func (e *Engine) PrepareDraft(ctx context.Context, request DraftRequest) (PreparedDraft, error) {
	var prepared PreparedDraft
	if !store.SafeID(request.CommandID) || !store.SafeID(request.PlanID) || !store.SafeID(request.RepositoryID) ||
		(request.Provider != "github" && request.Provider != "gitlab") || request.BaseBranch == "" ||
		strings.ContainsAny(request.BaseBranch, "\x00\r\n") || !validCommitIdentity("Draft", "draft@invalid", request.Title) || len(request.Title) > 256 || len(request.Body) > 8192 {
		return prepared, errors.New("bounded exact draft request required")
	}
	var priorRaw string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT result_json FROM command_receipts WHERE id=? AND actor='human'", request.CommandID).Scan(&priorRaw); err == nil {
		var prior struct {
			Result struct {
				OperationID string `json:"operation_id"`
				RequestID   string `json:"request_id"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(priorRaw), &prior); err != nil || prior.Result.OperationID == "" {
			return prepared, errors.New("command ID already belongs to another action")
		}
		intent, _, err := e.loadDraftIntent(ctx, prior.Result.OperationID)
		if err != nil || intent.PlanID != request.PlanID || intent.RepositoryID != request.RepositoryID || intent.Provider != request.Provider ||
			intent.Project != request.Project || intent.APIBase != request.APIBase || intent.BaseBranch != request.BaseBranch ||
			intent.Title != request.Title || intent.Body != request.Body || intent.CredentialRef != request.CredentialRef || intent.Fixture != request.Fixture {
			return prepared, errors.New("draft prepare command ID reused with different arguments")
		}
		return PreparedDraft{OperationID: prior.Result.OperationID, RequestID: prior.Result.RequestID, Intent: intent}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return prepared, err
	}
	record, err := e.Repository(ctx, request.RepositoryID)
	if err != nil || record.PlanID != request.PlanID {
		return prepared, errors.New("enrolled draft repository required")
	}
	credential := ""
	if !request.Fixture {
		credential = "env:SSH_AUTH_SOCK"
	}
	identity, remoteURL, env, err := observeRemoteConfig(ctx, record, record.RemoteName, credential)
	if err != nil {
		return prepared, err
	}
	if !request.Fixture {
		project, err := realHostedProject(request.Provider, remoteURL)
		if err != nil || project != request.Project {
			return prepared, errors.New("hosted project does not match the enrolled remote")
		}
	} else if err := e.requireFixtureArchive(ctx, request.PlanID); err != nil {
		return prepared, err
	}
	if _, err := newHostingAdapter(hostingSpec{Provider: request.Provider, APIBase: request.APIBase, Project: request.Project,
		HeadRef: record.PlanBranch, HeadOID: record.BaseOID, BaseRef: request.BaseBranch,
		Title: request.Title, Body: request.Body, OperationID: "preview", CredentialRef: request.CredentialRef, Fixture: request.Fixture}); err != nil {
		return prepared, err
	}
	var planState, acceptanceID, actor string
	if err := e.DB.SQL.QueryRowContext(ctx, `SELECT p.state,a.id,a.actor FROM plans p JOIN quality_acceptances_v2 a ON a.plan_id=p.id AND a.target_kind='plan' AND a.invalidated_at IS NULL WHERE p.id=?`, request.PlanID).Scan(&planState, &acceptanceID, &actor); err != nil || planState != "finalization_pending" || request.Fixture != (actor == "fixture_core") {
		return prepared, errors.New("current accepted plan must await finalization with matching provenance")
	}
	var archiveRevision int
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT coalesce(max(revision),0) FROM archives WHERE plan_id=?", request.PlanID).Scan(&archiveRevision); err != nil || archiveRevision < 1 {
		return prepared, errors.New("durable factual archive required before draft")
	}
	var head string
	if err := e.DB.SQL.QueryRowContext(ctx, `SELECT head_oid FROM deliveries WHERE plan_id=? AND repository_id=? AND kind='push' AND state='succeeded' AND remote_identity=? ORDER BY rowid DESC LIMIT 1`, request.PlanID, request.RepositoryID, identity).Scan(&head); err != nil || !gitOID(head) {
		return prepared, errors.New("observed exact push required before draft")
	}
	remoteHead, err := observedRemoteRef(ctx, record, record.RemoteName, "refs/heads/"+record.PlanBranch, env)
	if err != nil || remoteHead != head {
		return prepared, errors.New("remote draft head differs from observed push")
	}
	base, err := observedRemoteRef(ctx, record, record.RemoteName, "refs/heads/"+request.BaseBranch, env)
	if err != nil || !gitOID(base) || request.BaseBranch == record.PlanBranch {
		return prepared, errors.New("exact destination base branch unavailable")
	}
	intent := DraftIntent{PlanID: request.PlanID, RepositoryID: request.RepositoryID, RepositoryRevision: record.Revision,
		Provider: request.Provider, Project: request.Project, APIBase: request.APIBase, BaseBranch: request.BaseBranch, BaseOID: base,
		HeadBranch: record.PlanBranch, HeadOID: head, RemoteName: record.RemoteName, RemoteIdentity: identity,
		PlanAcceptanceID: acceptanceID, ArchiveRevision: archiveRevision, Title: request.Title, Body: request.Body,
		CredentialRef: request.CredentialRef, Fixture: request.Fixture, Draft: true}
	canonical, _ := json.Marshal(intent)
	resource, _ := json.Marshal(map[string]any{"repository_id": record.ID, "remote_identity": identity, "project": request.Project,
		"head": head, "base": base, "draft": true})
	op := OperationRequest{Category: "draft_request", ResourceDigest: store.Digest(resource), ArgumentsDigest: store.Digest(canonical), PlanID: request.PlanID, IntentJSON: string(canonical)}
	envelope, err := e.deliveryEnvelope(ctx, request.CommandID, "operation.request", op)
	if err != nil {
		return prepared, err
	}
	result, err := e.Apply(ctx, Human, envelope)
	if err != nil {
		return prepared, err
	}
	var parsed struct {
		Result struct {
			OperationID string `json:"operation_id"`
			RequestID   string `json:"request_id"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		return prepared, err
	}
	return PreparedDraft{OperationID: parsed.Result.OperationID, RequestID: parsed.Result.RequestID, Intent: intent}, nil
}

func (e *Engine) loadDraftIntent(ctx context.Context, operationID string) (DraftIntent, string, error) {
	var raw, state, kind string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT evidence_json,state,kind FROM operations WHERE id=?", operationID).Scan(&raw, &state, &kind); err != nil {
		return DraftIntent{}, "", err
	}
	if kind != "draft_request" {
		return DraftIntent{}, "", errors.New("operation is not a draft intent")
	}
	var op OperationRequest
	if err := json.Unmarshal([]byte(raw), &op); err != nil || op.IntentJSON == "" || store.Digest([]byte(op.IntentJSON)) != op.ArgumentsDigest {
		return DraftIntent{}, "", errors.New("draft intent is absent or corrupt")
	}
	var intent DraftIntent
	if err := json.Unmarshal([]byte(op.IntentJSON), &intent); err != nil || intent.PlanID != op.PlanID || !intent.Draft {
		return DraftIntent{}, "", errors.New("draft intent context is invalid")
	}
	return intent, state, nil
}

func (e *Engine) markDraftUncertain(ctx context.Context, operationID string) error {
	args, _ := json.Marshal(map[string]string{"operation_id": operationID})
	_, err := e.DB.Command(ctx, store.Command{ID: store.Digest([]byte("delivery.draft.uncertain\x00" + operationID)), Actor: "core", Kind: "delivery.draft.uncertain", Args: args}, func(tx *store.Tx) (any, error) {
		if _, err := tx.ExecContext(ctx, "UPDATE operations SET state='uncertain' WHERE id=? AND state='executing'", operationID); err != nil {
			return nil, err
		}
		_, err := tx.ExecContext(ctx, "UPDATE deliveries SET state='uncertain' WHERE operation_id=? AND state='pending'", operationID)
		return map[string]string{"state": "uncertain"}, err
	})
	return err
}

func (e *Engine) recordDraftSuccess(ctx context.Context, intent DraftIntent, operationID string, hosted hostedDraft) (DraftResult, error) {
	result := DraftResult{OperationID: operationID, DeliveryID: draftDeliveryID(operationID), ExternalID: hosted.ExternalID,
		URL: hosted.URL, HeadOID: intent.HeadOID, State: "succeeded"}
	args, _ := json.Marshal(result)
	if _, err := e.DB.Command(ctx, store.Command{ID: store.Digest([]byte("delivery.draft.observed\x00" + operationID)), Actor: "core", Kind: "delivery.draft.observed", Args: args}, func(tx *store.Tx) (any, error) {
		updated, err := tx.ExecContext(ctx, "UPDATE deliveries SET state='succeeded',external_id=?,url=? WHERE id=? AND head_oid=? AND state IN ('pending','uncertain')", hosted.ExternalID, hosted.URL, result.DeliveryID, intent.HeadOID)
		if err != nil {
			return nil, err
		}
		if n, err := updated.RowsAffected(); err != nil || n != 1 {
			return nil, errors.New("draft delivery changed before observation")
		}
		updated, err = tx.ExecContext(ctx, "UPDATE operations SET state='observed' WHERE id=? AND state IN ('executing','uncertain')", operationID)
		if err != nil {
			return nil, err
		}
		if n, err := updated.RowsAffected(); err != nil || n != 1 {
			return nil, errors.New("draft operation changed before observation")
		}
		return result, nil
	}); err != nil {
		return result, err
	}
	if _, err := e.BuildFactualArchive(ctx, store.Digest([]byte("archive.after.draft\x00"+operationID)), intent.PlanID); err != nil {
		return result, fmt.Errorf("draft observed but archive revision is pending: %w", err)
	}
	return result, nil
}

// ExecuteDraft never retries a possibly delivered POST. An executing/uncertain
// operation is observation-only, even if a crash happened just before POST.
func (e *Engine) ExecuteDraft(ctx context.Context, operationID, grantID string) (DraftResult, error) {
	result := DraftResult{OperationID: operationID, DeliveryID: draftDeliveryID(operationID)}
	if !store.SafeID(operationID) {
		return result, errors.New("operation ID required")
	}
	intent, state, err := e.loadDraftIntent(ctx, operationID)
	if err != nil {
		return result, err
	}
	result.HeadOID = intent.HeadOID
	if state == "observed" || state == "reconciled" {
		if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state,external_id,url FROM deliveries WHERE id=? AND head_oid=?", result.DeliveryID, intent.HeadOID).Scan(&result.State, &result.ExternalID, &result.URL); err != nil || result.State != "succeeded" {
			return DraftResult{}, errors.New("observed draft lacks succeeded delivery")
		}
		if _, err := e.BuildFactualArchive(ctx, store.Digest([]byte("archive.after.draft\x00"+operationID)), intent.PlanID); err != nil {
			return result, fmt.Errorf("draft observed but archive revision is pending: %w", err)
		}
		return result, nil
	}
	if state != "prepared" && state != "executing" && state != "uncertain" {
		return result, errors.New("draft operation is not executable")
	}
	_, _, err = e.draftContext(ctx, intent)
	if err != nil {
		return result, err
	}
	adapter, err := newHostingAdapter(hostingSpec{Provider: intent.Provider, APIBase: intent.APIBase, Project: intent.Project,
		HeadRef: intent.HeadBranch, HeadOID: intent.HeadOID, BaseRef: intent.BaseBranch,
		Title: intent.Title, Body: intent.Body, OperationID: operationID, CredentialRef: intent.CredentialRef, Fixture: intent.Fixture})
	if err != nil {
		return result, err
	}
	if state == "prepared" {
		if !store.SafeID(grantID) {
			return result, errors.New("exact draft grant required")
		}
		if _, found, err := adapter.List(ctx); err != nil || found {
			return result, errors.New("pre-existing, ambiguous or unavailable draft listing blocks creation")
		}
		envelope, err := e.deliveryEnvelope(ctx, store.Digest([]byte("delivery.draft.start\x00"+operationID)), "operation.start", map[string]string{"operation_id": operationID, "grant_id": grantID})
		if err != nil {
			return result, err
		}
		if _, err := e.Apply(ctx, Core, envelope); err != nil {
			return result, err
		}
	}
	args, _ := json.Marshal(map[string]string{"operation_id": operationID, "head_oid": intent.HeadOID})
	if _, err := e.DB.Command(ctx, store.Command{ID: store.Digest([]byte("delivery.draft.pending\x00" + operationID)), Actor: "core", Kind: "delivery.draft.pending", Args: args}, func(tx *store.Tx) (any, error) {
		_, err := tx.ExecContext(ctx, `INSERT INTO deliveries(id,plan_id,repository_id,operation_id,kind,state,remote_identity,head_oid,base_ref)
			VALUES(?,?,?,?,'draft_request','pending',?,?,?)`, result.DeliveryID, intent.PlanID, intent.RepositoryID, operationID, intent.RemoteIdentity, intent.HeadOID, intent.BaseBranch)
		return map[string]string{"delivery_id": result.DeliveryID}, err
	}); err != nil {
		return result, err
	}
	hosted, found, err := adapter.List(ctx)
	if err != nil {
		_ = e.markDraftUncertain(ctx, operationID)
		return result, errors.New("draft listing unavailable after effect start; operation uncertain")
	}
	if found {
		return e.recordDraftSuccess(ctx, intent, operationID, hosted)
	}
	if state != "prepared" {
		_ = e.markDraftUncertain(ctx, operationID)
		return result, errors.New("prior draft POST may have occurred; absence in listing is not proof of non-delivery")
	}
	hosted, err = adapter.Create(ctx)
	if err != nil {
		// The response may have been lost after remote success. Search once,
		// but do not interpret an empty result as permission to POST again.
		observed, found, observationErr := adapter.List(ctx)
		if observationErr != nil || !found {
			_ = e.markDraftUncertain(ctx, operationID)
			return result, errors.New("draft creation outcome uncertain; no automatic retry")
		}
		hosted = observed
	}
	return e.recordDraftSuccess(ctx, intent, operationID, hosted)
}
