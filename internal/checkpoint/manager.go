package checkpoint

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"vigil/internal/artifacts"
	"vigil/internal/core"
	"vigil/internal/store"
)

type Manager struct {
	Engine *core.Engine
	Store  *Store
	Fault  func(point string) error
}

type SaveRequest struct {
	CommandID        string           `json:"command_id"`
	ExpectedRevision int              `json:"expected_revision"`
	RunID            string           `json:"run_id"`
	Repositories     []RepositorySpec `json:"repositories"`
}

type SaveReceipt struct {
	CheckpointID   string `json:"checkpoint_id"`
	OperationID    string `json:"operation_id"`
	State          string `json:"state"`
	ManifestDigest string `json:"manifest_digest,omitempty"`
	Repeated       bool   `json:"repeated,omitempty"`
}

func NewManager(engine *core.Engine) (*Manager, error) {
	if engine == nil || engine.DB == nil {
		return nil, errors.New("project engine required")
	}
	checkpointStore, err := Open(filepath.Join(filepath.Dir(engine.DB.Path), "checkpoints"))
	if err != nil {
		return nil, err
	}
	return &Manager{Engine: engine, Store: checkpointStore}, nil
}

func (m *Manager) checkpoint(point string) error {
	if m.Fault != nil {
		return m.Fault(point)
	}
	return nil
}

func (m *Manager) writerSafe(ctx context.Context, runID string) error {
	var state, writer string
	if err := m.Engine.DB.SQL.QueryRowContext(ctx, "SELECT state,writer_state FROM runs WHERE id=?", runID).Scan(&state, &writer); err != nil {
		return err
	}
	if state == "prepared" {
		var submissions int
		if err := m.Engine.DB.SQL.QueryRowContext(ctx, "SELECT count(*) FROM run_generations WHERE run_id=? AND submission_state='not_attempted'", runID).Scan(&submissions); err != nil {
			return err
		}
		if submissions == 1 {
			return nil
		}
	}
	if writer != "contained_stopped" {
		return errors.New("checkpoint requires independently proven writer containment")
	}
	return nil
}

func (m *Manager) validateSpecs(ctx context.Context, runID string, specs []RepositorySpec) error {
	if len(specs) == 0 || len(specs) > 100 {
		return errors.New("bounded participating repository set required")
	}
	rows, err := m.Engine.DB.SQL.QueryContext(ctx, `SELECT json_extract(value,'$.id'),json_extract(value,'$.root'),json_extract(value,'$.identity.key'),json_extract(value,'$.identity.common_git') FROM run_snapshots,json_each(repository_snapshot_json) WHERE run_id=?`, runID)
	if err != nil {
		return err
	}
	defer rows.Close()
	known := map[string][3]string{}
	for rows.Next() {
		var id, root, key, common string
		if err := rows.Scan(&id, &root, &key, &common); err != nil {
			return err
		}
		known[id] = [3]string{root, key, common}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(known) != len(specs) {
		return errors.New("checkpoint must include every participating repository exactly once")
	}
	seen := map[string]bool{}
	for _, spec := range specs {
		want, ok := known[spec.ID]
		if !ok || seen[spec.ID] || spec.Root != want[0] || spec.Identity.Key != want[1] || spec.Identity.CommonGit != want[2] {
			return errors.New("checkpoint repository differs from immutable run participation")
		}
		seen[spec.ID] = true
	}
	return nil
}

func (m *Manager) Save(ctx context.Context, request SaveRequest) (SaveReceipt, error) {
	var receipt SaveReceipt
	if m == nil || m.Engine == nil || m.Store == nil || !store.SafeID(request.CommandID) || !store.SafeID(request.RunID) || request.ExpectedRevision < 1 {
		return receipt, errors.New("valid save command, run and project revision required")
	}
	if err := m.writerSafe(ctx, request.RunID); err != nil {
		return receipt, err
	}
	if err := m.validateSpecs(ctx, request.RunID, request.Repositories); err != nil {
		return receipt, err
	}
	args, err := json.Marshal(request)
	if err != nil || len(args) > store.MaxDocument {
		return receipt, errors.New("checkpoint save definition exceeds command limit")
	}
	command := store.Command{ID: request.CommandID, Actor: string(core.Human), Kind: "checkpoint.save", Args: args}
	commandReceipt, repeated, err := m.Engine.DB.Receipt(ctx, command)
	if err != nil {
		return receipt, err
	}
	if !repeated {
		commandReceipt, err = m.Engine.DB.Command(ctx, command, func(tx *store.Tx) (any, error) {
			var revision int
			if err := tx.QueryRowContext(ctx, "SELECT revision FROM project WHERE id=?", m.Engine.ProjectID).Scan(&revision); err != nil {
				return nil, err
			}
			if revision != request.ExpectedRevision {
				return nil, fmt.Errorf("stale project revision: expected %d, current %d", request.ExpectedRevision, revision)
			}
			operationID, checkpointID := store.ID(), store.ID()
			scopeDigest := store.Digest(args)
			var planID, taskID string
			if err := tx.QueryRowContext(ctx, "SELECT plan_id,task_id FROM runs WHERE id=?", request.RunID).Scan(&planID, &taskID); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO operations(id,kind,resource_digest,args_digest,policy_epoch,state,plan_id,task_id,run_id,evidence_json,created_at) SELECT ?,'checkpoint_save',?,?,policy_epoch,'prepared',?,?,?, ?,? FROM project WHERE id=?`, operationID, scopeDigest, scopeDigest, planID, taskID, request.RunID, string(args), store.Now(), m.Engine.ProjectID); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO checkpoint_sets(id,run_id,operation_id,state,created_at) VALUES(?,?,?,'capturing',?)`, checkpointID, request.RunID, operationID, store.Now()); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_operations(id,kind,checkpoint_id,authority_digest,state,created_at) VALUES(?,'save',?,?,'prepared',?)`, operationID, checkpointID, scopeDigest, store.Now()); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE project SET state='paused',revision=revision+1 WHERE id=?", m.Engine.ProjectID); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE plans SET state='paused' WHERE id=? AND state IN('active','blocked')", planID); err != nil {
				return nil, err
			}
			return SaveReceipt{CheckpointID: checkpointID, OperationID: operationID, State: "capturing"}, nil
		})
		if err != nil {
			return receipt, err
		}
	}
	if err := json.Unmarshal(commandReceipt, &receipt); err != nil {
		return receipt, err
	}
	receipt.Repeated = repeated
	var state string
	if err := m.Engine.DB.SQL.QueryRowContext(ctx, "SELECT state FROM checkpoint_sets WHERE id=?", receipt.CheckpointID).Scan(&state); err != nil {
		return receipt, err
	}
	if state == "verified" || state == "saved" || state == "restored" {
		manifest, err := m.verifiedSet(ctx, receipt.CheckpointID, state)
		if err != nil {
			return receipt, err
		}
		receipt.State, receipt.ManifestDigest = state, manifest.Digest
		return receipt, nil
	}
	if state != "capturing" && state != "incomplete" {
		return receipt, errors.New("checkpoint save is not safely resumable")
	}
	if state == "incomplete" {
		return receipt, errors.New("checkpoint capture is incomplete; use a new save command after inspection")
	}
	if err := m.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE operations SET state='executing' WHERE id=? AND state='prepared'", receipt.OperationID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE recovery_operations SET state='executing' WHERE id=? AND state='prepared'", receipt.OperationID)
		return err
	}); err != nil {
		return receipt, err
	}
	manifest, captureErr := m.Store.Capture(ctx, receipt.CheckpointID, store.Now(), request.Repositories)
	if captureErr != nil {
		m.markSaveFailure(receipt, captureErr)
		return receipt, captureErr
	}
	if err := m.Store.Verify(ctx, manifest); err != nil {
		m.markSaveFailure(receipt, err)
		return receipt, err
	}
	if err := m.checkpoint("after_set_publish"); err != nil {
		return receipt, err
	}
	manifestBytes, _ := json.Marshal(manifest)
	artifactRepository, err := artifacts.New(m.Engine.DB)
	if err != nil {
		return receipt, err
	}
	artifact, err := artifactRepository.PutCore(ctx, "checkpoint-manifest:"+receipt.CheckpointID, "checkpoint-manifest", "durable", bytes.NewReader(manifestBytes))
	if err != nil {
		m.markSaveFailure(receipt, err)
		return receipt, err
	}
	if err := m.checkpoint("after_manifest_artifact"); err != nil {
		return receipt, err
	}
	err = m.Engine.DB.Write(ctx, func(tx *store.Tx) error {
		for _, repository := range manifest.Repositories {
			if _, err := tx.ExecContext(ctx, `INSERT INTO checkpoint_repositories(checkpoint_id,repository_id,base_oid,before_digest,captured_digest,artifact_id,progress_json) VALUES(?,?,?,?,?,?,'{}')`, receipt.CheckpointID, repository.RepositoryID, repository.HeadOID, repository.Digest, repository.Digest, artifact.ID); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO checkpoint_manifests(checkpoint_id,manifest_digest,set_digest,store_relative_path,repository_count,verified_at) VALUES(?,?,?,?,?,?)`, receipt.CheckpointID, artifact.Digest, manifest.Digest, "sets/"+receipt.CheckpointID+"/manifest.json", len(manifest.Repositories), store.Now()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE checkpoint_sets SET state='verified',manifest_id=? WHERE id=? AND state='capturing'", artifact.ID, receipt.CheckpointID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE recovery_operations SET state='observed',observed_at=? WHERE id=?", store.Now(), receipt.OperationID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE operations SET state='observed' WHERE id=?", receipt.OperationID)
		return err
	})
	if err != nil {
		return receipt, err
	}
	receipt.State, receipt.ManifestDigest = "verified", manifest.Digest
	return receipt, nil
}

func (m *Manager) markSaveFailure(receipt SaveReceipt, cause error) {
	raw, _ := json.Marshal(map[string]string{"error": cause.Error()})
	_ = m.Engine.DB.Write(context.Background(), func(tx *store.Tx) error {
		if _, err := tx.ExecContext(context.Background(), "UPDATE checkpoint_sets SET state='incomplete' WHERE id=? AND state='capturing'", receipt.CheckpointID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(context.Background(), "UPDATE recovery_operations SET state='uncertain',observed_at=? WHERE id=?", store.Now(), receipt.OperationID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(context.Background(), "UPDATE operations SET state='uncertain',evidence_json=? WHERE id=?", string(raw), receipt.OperationID); err != nil {
			return err
		}
		_, err := tx.ExecContext(context.Background(), "UPDATE project SET state='recovering' WHERE id=?", m.Engine.ProjectID)
		return err
	})
}

func (m *Manager) verifiedSet(ctx context.Context, id, expectedState string) (SetManifest, error) {
	var result SetManifest
	if !store.SafeID(id) {
		return result, errors.New("invalid checkpoint ID")
	}
	var state, manifestID, manifestDigest, setDigest, relative string
	var count int
	err := m.Engine.DB.SQL.QueryRowContext(ctx, `SELECT s.state,s.manifest_id,m.manifest_digest,m.set_digest,m.store_relative_path,m.repository_count FROM checkpoint_sets s JOIN checkpoint_manifests m ON m.checkpoint_id=s.id WHERE s.id=?`, id).Scan(&state, &manifestID, &manifestDigest, &setDigest, &relative, &count)
	if err != nil {
		return result, err
	}
	if expectedState != "" && state != expectedState {
		return result, fmt.Errorf("checkpoint state is %s, expected %s", state, expectedState)
	}
	if relative != "sets/"+id+"/manifest.json" {
		return result, errors.New("checkpoint manifest path is invalid")
	}
	artifactRepository, err := artifacts.New(m.Engine.DB)
	if err != nil {
		return result, err
	}
	if err := artifactRepository.Verify(ctx, manifestID, manifestDigest, "checkpoint-manifest"); err != nil {
		return result, err
	}
	result, err = m.Store.Read(ctx, id)
	if err != nil {
		return result, err
	}
	if result.Digest != setDigest || len(result.Repositories) != count {
		return result, errors.New("checkpoint database and private set differ")
	}
	return result, nil
}
