package checkpoint

import "context"

// VerifyCheckpointCurrent proves both stored-set integrity and that every
// enrolled destination still exactly matches that set.
func (m *Manager) VerifyCheckpointCurrent(ctx context.Context, id string) error {
	manifest, err := m.VerifySet(ctx, id)
	if err != nil {
		return err
	}
	return m.verifyCurrentDestination(ctx, manifest)
}
