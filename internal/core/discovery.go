package core

import (
	"context"
	"encoding/json"
	"errors"

	"vigil/internal/workspace"
)

func (e *Engine) DiscoverRepositories(ctx context.Context) (workspace.Discovery, error) {
	var root, raw string
	if err := e.DB.SQL.QueryRowContext(ctx, "SELECT root,identity_json FROM project WHERE id=?", e.ProjectID).Scan(&root, &raw); err != nil {
		return workspace.Discovery{}, err
	}
	var identity workspace.Identity
	if err := json.Unmarshal([]byte(raw), &identity); err != nil {
		return workspace.Discovery{}, err
	}
	if identity.Root != root {
		return workspace.Discovery{}, errors.New("registered project identity mismatch")
	}
	if err := identity.Validate(); err != nil {
		return workspace.Discovery{}, err
	}
	return workspace.Discover(ctx, root)
}
