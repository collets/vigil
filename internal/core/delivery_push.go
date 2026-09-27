package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"vigil/internal/store"
	"vigil/internal/workspace"
)

type PushRequest struct {
	CommandID     string `json:"command_id"`
	PlanID        string `json:"plan_id"`
	RepositoryID  string `json:"repository_id"`
	RemoteName    string `json:"remote_name"`
	CredentialRef string `json:"credential_ref,omitempty"`
}

type PushIntent struct {
	PlanID             string `json:"plan_id"`
	RepositoryID       string `json:"repository_id"`
	RepositoryRevision int    `json:"repository_revision"`
	RemoteName         string `json:"remote_name"`
	RemoteIdentity     string `json:"remote_identity"`
	RemoteURL          string `json:"remote_url"`
	CredentialRef      string `json:"credential_ref,omitempty"`
	LocalRef           string `json:"local_ref"`
	RemoteRef          string `json:"remote_ref"`
	HeadOID            string `json:"head_oid"`
	ExpectedRemoteOID  string `json:"expected_remote_oid,omitempty"`
}

type PreparedPush struct {
	OperationID string     `json:"operation_id"`
	RequestID   string     `json:"request_id"`
	Intent      PushIntent `json:"intent"`
}

type PushResult struct {
	OperationID string `json:"operation_id"`
	DeliveryID  string `json:"delivery_id"`
	HeadOID     string `json:"head_oid"`
	RemoteRef   string `json:"remote_ref"`
	State       string `json:"state"`
}

func pushDeliveryID(id string) string { return store.Digest([]byte("delivery.push\x00" + id)) }

func remoteGitEnvironment(credentialRef, rawURL string) ([]string, error) {
	if rawURL == "" || len(rawURL) > 2048 || strings.ContainsAny(rawURL, "\x00\r\n") || strings.HasPrefix(rawURL, "-") {
		return nil, errors.New("invalid remote URL")
	}
	env := []string{"GIT_ALLOW_PROTOCOL=ssh:file"}
	if filepath.IsAbs(rawURL) || strings.HasPrefix(rawURL, "file://") {
		if credentialRef != "" {
			return nil, errors.New("local remote cannot request a credential")
		}
		if strings.HasPrefix(rawURL, "file://") {
			u, err := url.Parse(rawURL)
			if err != nil || u.Host != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !filepath.IsAbs(u.Path) {
				return nil, errors.New("file remote must be an absolute local path")
			}
		}
		return env, nil
	}
	if !(strings.HasPrefix(rawURL, "ssh://") || strings.HasPrefix(rawURL, "git@") && strings.Contains(rawURL, ":")) {
		return nil, errors.New("only local or SSH remotes are supported for controlled push")
	}
	if credentialRef != "env:SSH_AUTH_SOCK" {
		return nil, errors.New("SSH push requires named SSH agent socket credential")
	}
	socket := os.Getenv("SSH_AUTH_SOCK")
	if socket == "" || !filepath.IsAbs(socket) {
		return nil, errors.New("named SSH agent socket is unavailable")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return append(env, "SSH_AUTH_SOCK="+socket, "HOME="+home,
		"GIT_SSH_COMMAND=ssh -oBatchMode=yes -oStrictHostKeyChecking=yes"), nil
}

func observeRemoteConfig(ctx context.Context, record RepositoryRecord, name, credentialRef string) (string, string, []string, error) {
	if name == "" || name != record.RemoteName || record.RemoteIdentity == "" {
		return "", "", nil, errors.New("remote differs from enrolled identity")
	}
	identity, err := workspace.RemoteIdentity(ctx, record.Root, name)
	if err != nil || identity != record.RemoteIdentity {
		return "", "", nil, errors.New("remote identity changed since enrollment")
	}
	fetchURL, err := deliveryGit(ctx, record.Root, nil, nil, "remote", "get-url", "--all", name)
	if err != nil || fetchURL == "" || strings.Contains(fetchURL, "\n") {
		return "", "", nil, errors.New("remote has an ambiguous fetch URL")
	}
	pushURL, err := deliveryGit(ctx, record.Root, nil, nil, "remote", "get-url", "--push", "--all", name)
	if err != nil || pushURL != fetchURL {
		return "", "", nil, errors.New("remote push URL differs from the enrolled fetch URL")
	}
	env, err := remoteGitEnvironment(credentialRef, pushURL)
	return identity, pushURL, env, err
}

func observedRemoteRef(ctx context.Context, record RepositoryRecord, remoteName, remoteRef string, env []string) (string, error) {
	output, err := deliveryGit(ctx, record.Root, env, nil, "ls-remote", "--refs", remoteName, remoteRef)
	if err != nil {
		return "", errors.New("exact remote ref observation unavailable")
	}
	if output == "" {
		return "", nil
	}
	parts := strings.Split(output, "\t")
	if len(parts) != 2 || parts[1] != remoteRef || !gitOID(parts[0]) {
		return "", errors.New("ambiguous remote ref observation")
	}
	return parts[0], nil
}

func (e *Engine) pushContext(ctx context.Context, intent PushIntent, needLocalHead bool) (RepositoryRecord, []string, error) {
	record, err := e.Repository(ctx, intent.RepositoryID)
	if err != nil || record.PlanID != intent.PlanID || record.Revision != intent.RepositoryRevision {
		return RepositoryRecord{}, nil, errors.New("enrolled push repository changed")
	}
	if err := record.Identity.Validate(); err != nil {
		return RepositoryRecord{}, nil, err
	}
	if intent.LocalRef != "refs/heads/"+record.PlanBranch || intent.RemoteRef != intent.LocalRef || !gitOID(intent.HeadOID) {
		return RepositoryRecord{}, nil, errors.New("push targets only the exact enrolled plan ref")
	}
	identity, rawURL, env, err := observeRemoteConfig(ctx, record, intent.RemoteName, intent.CredentialRef)
	if err != nil || identity != intent.RemoteIdentity || rawURL != intent.RemoteURL {
		return RepositoryRecord{}, nil, errors.New("approved remote configuration changed")
	}
	var commitOID string
	if err := e.DB.SQL.QueryRowContext(ctx, `SELECT head_oid FROM deliveries WHERE plan_id=? AND repository_id=? AND kind='commit' AND state='succeeded' ORDER BY rowid DESC LIMIT 1`, intent.PlanID, intent.RepositoryID).Scan(&commitOID); err != nil || commitOID != intent.HeadOID {
		return RepositoryRecord{}, nil, errors.New("push head is not the latest app-owned plan commit")
	}
	if needLocalHead {
		local, err := deliveryGit(ctx, record.Root, nil, nil, "rev-parse", "--verify", intent.LocalRef+"^{commit}")
		if err != nil || local != intent.HeadOID {
			return RepositoryRecord{}, nil, errors.New("local plan ref changed before push")
		}
	}
	return record, env, nil
}

func (e *Engine) PreparePush(ctx context.Context, request PushRequest) (PreparedPush, error) {
	var prepared PreparedPush
	if !store.SafeID(request.CommandID) || !store.SafeID(request.PlanID) || !store.SafeID(request.RepositoryID) {
		return prepared, errors.New("exact push command, plan and repository required")
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
		intent, _, err := e.loadPushIntent(ctx, prior.Result.OperationID)
		if err != nil || intent.PlanID != request.PlanID || intent.RepositoryID != request.RepositoryID || intent.RemoteName != request.RemoteName || intent.CredentialRef != request.CredentialRef {
			return prepared, errors.New("push prepare command ID reused with different arguments")
		}
		return PreparedPush{OperationID: prior.Result.OperationID, RequestID: prior.Result.RequestID, Intent: intent}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return prepared, err
	}
	record, err := e.Repository(ctx, request.RepositoryID)
	if err != nil || record.PlanID != request.PlanID {
		return prepared, errors.New("enrolled plan repository required")
	}
	identity, rawURL, env, err := observeRemoteConfig(ctx, record, request.RemoteName, request.CredentialRef)
	if err != nil {
		return prepared, err
	}
	ref := "refs/heads/" + record.PlanBranch
	var head string
	if err := e.DB.SQL.QueryRowContext(ctx, `SELECT head_oid FROM deliveries WHERE plan_id=? AND repository_id=? AND kind='commit' AND state='succeeded' ORDER BY rowid DESC LIMIT 1`, request.PlanID, request.RepositoryID).Scan(&head); err != nil {
		return prepared, errors.New("a succeeded app-owned commit is required before push")
	}
	local, err := deliveryGit(ctx, record.Root, nil, nil, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil || local != head {
		return prepared, errors.New("local plan ref is not the exact app-owned commit")
	}
	remote, err := observedRemoteRef(ctx, record, request.RemoteName, ref, env)
	if err != nil {
		return prepared, err
	}
	if remote == head {
		return prepared, errors.New("approved plan ref is already present at the remote")
	}
	if remote != "" {
		// The approved commit must fast-forward the observed remote ref.
		if _, err := deliveryGit(ctx, record.Root, nil, nil, "merge-base", "--is-ancestor", remote, head); err != nil {
			return prepared, errors.New("remote ref is not an ancestor of the approved head")
		}
	}
	intent := PushIntent{PlanID: request.PlanID, RepositoryID: request.RepositoryID, RepositoryRevision: record.Revision,
		RemoteName: request.RemoteName, RemoteIdentity: identity, RemoteURL: rawURL, CredentialRef: request.CredentialRef,
		LocalRef: ref, RemoteRef: ref, HeadOID: head, ExpectedRemoteOID: remote}
	canonical, _ := json.Marshal(intent)
	resource, _ := json.Marshal(map[string]any{"repository_id": record.ID, "revision": record.Revision, "remote_identity": identity, "remote_ref": ref})
	op := OperationRequest{Category: "push", ResourceDigest: store.Digest(resource), ArgumentsDigest: store.Digest(canonical), PlanID: request.PlanID, IntentJSON: string(canonical)}
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
	return PreparedPush{OperationID: parsed.Result.OperationID, RequestID: parsed.Result.RequestID, Intent: intent}, nil
}

func (e *Engine) loadPushIntent(ctx context.Context, operationID string) (PushIntent, string, error) {
	var raw, state, kind string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT evidence_json,state,kind FROM operations WHERE id=?", operationID).Scan(&raw, &state, &kind); err != nil {
		return PushIntent{}, "", err
	}
	if kind != "push" {
		return PushIntent{}, "", errors.New("operation is not a push intent")
	}
	var op OperationRequest
	if err := json.Unmarshal([]byte(raw), &op); err != nil || op.IntentJSON == "" || store.Digest([]byte(op.IntentJSON)) != op.ArgumentsDigest {
		return PushIntent{}, "", errors.New("push intent is absent or corrupt")
	}
	var intent PushIntent
	if err := json.Unmarshal([]byte(op.IntentJSON), &intent); err != nil || intent.PlanID != op.PlanID {
		return PushIntent{}, "", errors.New("push intent context is invalid")
	}
	return intent, state, nil
}

func (e *Engine) markPushUncertain(ctx context.Context, operationID string) error {
	args, _ := json.Marshal(map[string]string{"operation_id": operationID})
	_, err := e.DB.Command(ctx, store.Command{ID: store.Digest([]byte("delivery.push.uncertain\x00" + operationID)), Actor: "core", Kind: "delivery.push.uncertain", Args: args}, func(tx *store.Tx) (any, error) {
		if _, err := tx.ExecContext(ctx, "UPDATE operations SET state='uncertain' WHERE id=? AND state='executing'", operationID); err != nil {
			return nil, err
		}
		_, err := tx.ExecContext(ctx, "UPDATE deliveries SET state='uncertain' WHERE operation_id=? AND state='pending'", operationID)
		return map[string]string{"state": "uncertain"}, err
	})
	return err
}

func (e *Engine) ExecutePush(ctx context.Context, operationID, grantID string) (PushResult, error) {
	result := PushResult{OperationID: operationID, DeliveryID: pushDeliveryID(operationID)}
	if !store.SafeID(operationID) {
		return result, errors.New("operation ID required")
	}
	intent, state, err := e.loadPushIntent(ctx, operationID)
	if err != nil {
		return result, err
	}
	result.HeadOID, result.RemoteRef = intent.HeadOID, intent.RemoteRef
	if state == "observed" || state == "reconciled" {
		if err := e.DB.SQL.QueryRowContext(ctx, "SELECT state FROM deliveries WHERE id=? AND head_oid=?", result.DeliveryID, intent.HeadOID).Scan(&result.State); err != nil || result.State != "succeeded" {
			return PushResult{}, errors.New("observed push lacks succeeded delivery")
		}
		return result, nil
	}
	if state != "prepared" && state != "executing" && state != "uncertain" {
		return result, errors.New("push operation is not executable")
	}
	record, env, err := e.pushContext(ctx, intent, state == "prepared")
	if err != nil {
		return result, err
	}
	remote, err := observedRemoteRef(ctx, record, intent.RemoteName, intent.RemoteRef, env)
	if err != nil {
		return result, err
	}
	if state == "prepared" {
		if remote != intent.ExpectedRemoteOID || !store.SafeID(grantID) {
			return result, errors.New("approved remote ref changed or exact grant missing")
		}
		envelope, err := e.deliveryEnvelope(ctx, store.Digest([]byte("delivery.push.start\x00"+operationID)), "operation.start", map[string]string{"operation_id": operationID, "grant_id": grantID})
		if err != nil {
			return result, err
		}
		if _, err := e.Apply(ctx, Core, envelope); err != nil {
			return result, err
		}
	}
	args, _ := json.Marshal(map[string]string{"operation_id": operationID, "head_oid": intent.HeadOID})
	if _, err := e.DB.Command(ctx, store.Command{ID: store.Digest([]byte("delivery.push.pending\x00" + operationID)), Actor: "core", Kind: "delivery.push.pending", Args: args}, func(tx *store.Tx) (any, error) {
		_, err := tx.ExecContext(ctx, `INSERT INTO deliveries(id,plan_id,repository_id,operation_id,kind,state,remote_identity,head_oid,base_ref)
			VALUES(?,?,?,?,'push','pending',?,?,?)`, result.DeliveryID, intent.PlanID, intent.RepositoryID, operationID, intent.RemoteIdentity, intent.HeadOID, intent.RemoteRef)
		return map[string]string{"delivery_id": result.DeliveryID}, err
	}); err != nil {
		return result, err
	}
	if remote != intent.HeadOID {
		if state == "uncertain" || remote != intent.ExpectedRemoteOID {
			_ = e.markPushUncertain(ctx, operationID)
			return result, errors.New("remote ref does not prove the approved push succeeded; operation remains uncertain")
		}
		// Explicit OID source and destination prevent configured default/mirror
		// refspecs, tags, hooks and unrelated checkpoint refs from being sent.
		_, pushErr := deliveryGit(ctx, record.Root, env, nil, "-c", "push.default=nothing", "push", "--porcelain", "--no-verify", "--no-follow-tags", "--recurse-submodules=no", intent.RemoteName, intent.HeadOID+":"+intent.RemoteRef)
		remote, err = observedRemoteRef(ctx, record, intent.RemoteName, intent.RemoteRef, env)
		if err != nil || remote != intent.HeadOID {
			_ = e.markPushUncertain(ctx, operationID)
			if pushErr != nil {
				return result, errors.New("push outcome uncertain; remote observation did not prove the exact head")
			}
			return result, errors.New("push response did not match exact remote ref; operation remains uncertain")
		}
	}
	_, err = e.DB.Command(ctx, store.Command{ID: store.Digest([]byte("delivery.push.observed\x00" + operationID)), Actor: "core", Kind: "delivery.push.observed", Args: args}, func(tx *store.Tx) (any, error) {
		updated, err := tx.ExecContext(ctx, "UPDATE deliveries SET state='succeeded' WHERE id=? AND head_oid=? AND state IN ('pending','uncertain')", result.DeliveryID, intent.HeadOID)
		if err != nil {
			return nil, err
		}
		if n, err := updated.RowsAffected(); err != nil || n != 1 {
			return nil, errors.New("push delivery changed before observation")
		}
		updated, err = tx.ExecContext(ctx, "UPDATE operations SET state='observed' WHERE id=? AND state IN ('executing','uncertain')", operationID)
		if err != nil {
			return nil, err
		}
		if n, err := updated.RowsAffected(); err != nil || n != 1 {
			return nil, errors.New("push operation changed before observation")
		}
		return result, nil
	})
	if err != nil {
		return result, err
	}
	result.State = "succeeded"
	return result, nil
}
