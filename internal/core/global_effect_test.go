package core

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestGlobalEffectCrossDatabaseUncertaintyNeverReplays(t *testing.T) {
	m, e, _ := setup(t)
	ctx := context.Background()
	apply(t, e, "project.configure", config())
	apply(t, e, "plan.put", plan())
	request := apply(t, e, "operation.request", OperationRequest{Category: "commit", ResourceDigest: strings.Repeat("a", 64), ArgumentsDigest: strings.Repeat("b", 64), PlanID: "plan", TaskID: "first", TaskRevision: 1})
	operationID := resultString(t, request, "operation_id")
	if _, err := m.Coordinator.CreateGlobalGrant(ctx, "global", "commit", strings.Repeat("a", 64), strings.Repeat("b", 64), true); err != nil {
		t.Fatal(err)
	}
	owner, err := m.Coordinator.Register(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	injected := errors.New("crash after coordinator start")
	link, err := e.startGlobalEffect(ctx, owner, operationID, "global", func(point string) error {
		if point == "after_coordinator_start" {
			return injected
		}
		return nil
	})
	if !errors.Is(err, injected) || link.State != "prepared" {
		t.Fatal(link, err)
	}
	shared, err := m.Coordinator.GlobalEffect(ctx, link.CoordinatorOperationID)
	if err != nil || shared.State != "executing" {
		t.Fatal(shared, err)
	}
	link, err = e.ReconcileGlobalEffect(ctx, m.Coordinator, operationID)
	if err != nil || link.State != "uncertain" {
		t.Fatal(link, err)
	}
	if _, err = e.StartGlobalEffect(ctx, owner, operationID, "global"); err == nil {
		t.Fatal("uncertain cross-DB start replayed")
	}
	var state string
	if err := e.DB.SQL.QueryRow("SELECT state FROM operations WHERE id=?", operationID).Scan(&state); err != nil || state != "uncertain" {
		t.Fatal(state, err)
	}
}

func TestGlobalEffectRevocationBeforeStartFailsClosed(t *testing.T) {
	m, e, _ := setup(t)
	ctx := context.Background()
	apply(t, e, "project.configure", config())
	apply(t, e, "plan.put", plan())
	request := apply(t, e, "operation.request", OperationRequest{Category: "commit", ResourceDigest: strings.Repeat("c", 64), ArgumentsDigest: strings.Repeat("d", 64), PlanID: "plan", TaskID: "first", TaskRevision: 1})
	operationID := resultString(t, request, "operation_id")
	if _, err := m.Coordinator.CreateGlobalGrant(ctx, "revoked", "commit", strings.Repeat("c", 64), strings.Repeat("d", 64), true); err != nil {
		t.Fatal(err)
	}
	if err := m.Coordinator.RevokeGlobalGrant(ctx, "revoked"); err != nil {
		t.Fatal(err)
	}
	owner, err := m.Coordinator.Register(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if _, err = e.StartGlobalEffect(ctx, owner, operationID, "revoked"); err == nil {
		t.Fatal("revocation lost effect-start race")
	}
	var state string
	if err := e.DB.SQL.QueryRow("SELECT state FROM operations WHERE id=?", operationID).Scan(&state); err != nil || state != "prepared" {
		t.Fatal(state, err)
	}
}
