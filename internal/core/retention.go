package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"vigil/internal/artifacts"
	"vigil/internal/policy"
	"vigil/internal/store"
)

func (e *Engine) transcriptRetention(ctx context.Context) (*artifacts.Repository, int, error) {
	var raw string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT s.resolved_json FROM project_configurations c JOIN config_snapshots s ON s.id=c.config_id ORDER BY c.revision DESC LIMIT 1").Scan(&raw); err != nil {
		return nil, 0, err
	}
	var config policy.Config
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return nil, 0, err
	}
	if err := config.Validate(); err != nil {
		return nil, 0, err
	}
	repository, err := artifacts.New(e.DB)
	return repository, config.EffectiveTranscriptRetentionDays(), err
}

// InspectTranscriptExpiry is the mandatory dry run before any transcript
// expiry: it persists the exact observed candidate set as a durable human
// receipt that ExpireTranscripts must consume unchanged.
func (e *Engine) InspectTranscriptExpiry(ctx context.Context, commandID string, now int64) ([]artifacts.ExpiryCandidate, error) {
	if !store.SafeID(commandID) {
		return nil, errors.New("exact retention inspection command required")
	}
	repository, days, err := e.transcriptRetention(ctx)
	if err != nil {
		return nil, err
	}
	args, _ := json.Marshal(map[string]int64{"now": now})
	command := store.Command{ID: commandID, Actor: "human", Kind: "retention.inspect", Args: args}
	if receipt, found, err := e.DB.Receipt(ctx, command); err != nil || found {
		var candidates []artifacts.ExpiryCandidate
		if err == nil {
			err = json.Unmarshal(receipt, &candidates)
		}
		return candidates, err
	}
	candidates, err := repository.TranscriptExpiryCandidates(ctx, repository.DB.SQL, now, days, true)
	if err != nil {
		return nil, err
	}
	receipt, err := e.DB.Command(ctx, command, func(tx *store.Tx) (any, error) {
		fresh, err := repository.TranscriptExpiryCandidates(ctx, tx, now, days, false)
		if err != nil {
			return nil, err
		}
		if len(fresh) != len(candidates) {
			return nil, errors.New("retention candidates changed before inspection")
		}
		for i := range fresh {
			if fresh[i] != candidates[i] {
				return nil, errors.New("retention candidates changed before inspection")
			}
		}
		return candidates, nil
	})
	if err != nil {
		return nil, err
	}
	var result []artifacts.ExpiryCandidate
	if err := json.Unmarshal(receipt, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// ExpireTranscripts deletes eligible raw transcript bytes only after an
// explicitly referenced dry-run inspection receipt still matches a fresh
// recomputation, through a human envelope with an expected-revision check.
// The irreversible blob deletion runs only after that transaction commits.
func (e *Engine) ExpireTranscripts(ctx context.Context, commandID, inspectCommandID string, now int64) (artifacts.ExpiryResult, error) {
	result := artifacts.ExpiryResult{Expired: []artifacts.ExpiryCandidate{}, Deleted: []string{}}
	if !store.SafeID(commandID) || !store.SafeID(inspectCommandID) {
		return result, errors.New("exact expiry command and inspected dry-run receipt required")
	}
	repository, days, err := e.transcriptRetention(ctx)
	if err != nil {
		return result, err
	}
	expired, err := e.applyTranscriptExpiry(ctx, commandID, inspectCommandID, now, days)
	if err != nil {
		return result, err
	}
	result.Expired = expired
	// A replay after a crash between marking and deletion cleans the
	// remaining blobs; the receipt replay above is idempotent.
	deleted, err := repository.CleanupExpiredTranscriptBlobs(ctx)
	if err != nil {
		return result, err
	}
	result.Deleted = deleted
	return result, nil
}

// applyTranscriptExpiry runs the human expiry envelope once. The envelope's
// expected project revision changes when it commits, so an exact replay is
// recognized by its recorded receipt instead of a repeated envelope.
func (e *Engine) applyTranscriptExpiry(ctx context.Context, commandID, inspectCommandID string, now int64, days int) ([]artifacts.ExpiryCandidate, error) {
	var priorRaw string
	err := e.DB.SQL.QueryRowContext(ctx, "SELECT result_json FROM command_receipts WHERE id=? AND actor='human'", commandID).Scan(&priorRaw)
	if err == nil {
		var prior struct {
			Result transcriptExpiryRecord `json:"result"`
		}
		if json.Unmarshal([]byte(priorRaw), &prior) != nil || prior.Result.InspectCommandID != inspectCommandID {
			return nil, errors.New("command ID already belongs to another action")
		}
		return prior.Result.Expired, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	envelope, err := e.deliveryEnvelope(ctx, commandID, "retention.expire", map[string]any{"inspect_command_id": inspectCommandID, "now": now, "days": days})
	if err != nil {
		return nil, err
	}
	raw, err := e.Apply(ctx, Human, envelope)
	if err != nil {
		return nil, err
	}
	var outcome struct {
		Result transcriptExpiryRecord `json:"result"`
	}
	if err := json.Unmarshal(raw, &outcome); err != nil {
		return nil, err
	}
	return outcome.Result.Expired, nil
}

type transcriptExpiryRecord struct {
	InspectCommandID string                     `json:"inspect_command_id"`
	Expired          []artifacts.ExpiryCandidate `json:"expired"`
}

// expireTranscriptsCommand runs inside the Apply transaction: it consumes the
// referenced dry-run receipt — the exact recorded candidate set must still
// match a fresh recomputation — before any artifact is marked expired.
func (e *Engine) expireTranscriptsCommand(ctx context.Context, tx *store.Tx, cmd Envelope) (any, error) {
	var request struct {
		InspectCommandID string `json:"inspect_command_id"`
		Now              int64  `json:"now"`
		Days             int    `json:"days"`
	}
	if err := store.Decode(cmd.Payload, &request); err != nil {
		return nil, err
	}
	if !store.SafeID(request.InspectCommandID) {
		return nil, errors.New("exact inspected dry-run receipt required")
	}
	var actor, recorded string
	if err := tx.QueryRowContext(ctx, "SELECT actor,result_json FROM command_receipts WHERE id=?", request.InspectCommandID).Scan(&actor, &recorded); err != nil || actor != "human" {
		return nil, errors.New("human dry-run inspection receipt required")
	}
	var candidates []artifacts.ExpiryCandidate
	if err := json.Unmarshal([]byte(recorded), &candidates); err != nil {
		return nil, errors.New("referenced receipt is not a transcript dry-run inspection")
	}
	repository, err := artifacts.New(e.DB)
	if err != nil {
		return nil, err
	}
	fresh, err := repository.TranscriptExpiryCandidates(ctx, tx, request.Now, request.Days, false)
	if err != nil {
		return nil, err
	}
	if len(fresh) != len(candidates) {
		return nil, errors.New("retention candidates changed since the dry-run inspection; re-inspect first")
	}
	for i := range fresh {
		if fresh[i] != candidates[i] {
			return nil, errors.New("retention candidates changed since the dry-run inspection; re-inspect first")
		}
	}
	if err := repository.ExpireCandidates(ctx, tx, request.Now, fresh); err != nil {
		return nil, err
	}
	return transcriptExpiryRecord{InspectCommandID: request.InspectCommandID, Expired: fresh}, nil
}
