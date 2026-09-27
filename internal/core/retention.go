package core

import (
	"context"
	"encoding/json"

	"vigil/internal/artifacts"
	"vigil/internal/policy"
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

func (e *Engine) InspectTranscriptExpiry(ctx context.Context, now int64) ([]artifacts.ExpiryCandidate, error) {
	repository, days, err := e.transcriptRetention(ctx)
	if err != nil {
		return nil, err
	}
	return repository.InspectTranscriptExpiry(ctx, now, days)
}

func (e *Engine) ExpireTranscripts(ctx context.Context, commandID string, now int64) (artifacts.ExpiryResult, error) {
	repository, days, err := e.transcriptRetention(ctx)
	if err != nil {
		return artifacts.ExpiryResult{}, err
	}
	return repository.ExpireTranscripts(ctx, commandID, now, days)
}
