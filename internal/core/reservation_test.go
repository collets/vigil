package core

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"vigil/internal/coordinator"
	"vigil/internal/workspace"
)

func TestResourceJournalRecoversEveryReservationGap(t *testing.T) {
	for _, phase := range []string{"after_intent", "after_claim", "after_enqueue", "after_slot", "after_observation"} {
		t.Run(phase, func(t *testing.T) {
			m, e, p := setup(t)
			ctx := context.Background()
			apply(t, e, "project.configure", config())
			if err := m.Coordinator.Endpoint(ctx, "local", []string{"http://127.0.0.1:1/v1"}, 1, coordinator.Host()); err != nil {
				t.Fatal(err)
			}
			owner, err := m.Coordinator.Register(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Close()
			injected := errors.New("injected persistence gap")
			_, err = e.reserveResources(ctx, owner, "reservation", "run", "local", func(at string) error {
				if at == phase {
					return injected
				}
				return nil
			})
			if !errors.Is(err, injected) {
				t.Fatal("missing fault", err)
			}
			reopened, err := m.Open(ctx, p.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.DB.Close()
			r, err := reopened.ReserveResources(ctx, owner, "reservation", "run", "local")
			if err != nil || r.Phase != "reserved" || len(r.Claims) != 1 || r.Ticket.Generation == 0 {
				t.Fatal(r, err)
			}
			again, err := reopened.ReserveResources(ctx, owner, "reservation", "run", "local")
			if err != nil || again.Ticket != r.Ticket || again.Claims[0].Generation != r.Claims[0].Generation {
				t.Fatal("reservation replay duplicated resource", again, err)
			}
			for _, table := range []string{"workspace_claims", "queue_tickets", "endpoint_slots"} {
				var count int
				if err := m.Coordinator.DB.SQL.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 1 {
					t.Fatal(table, count, err)
				}
			}
			if _, err := reopened.ReserveResources(ctx, owner, "reservation", "different-run", "local"); err == nil {
				t.Fatal("changed intent reused identity")
			}
		})
	}
}

func TestResourceJournalOwnerLossRetainsQuarantine(t *testing.T) {
	m, e, p := setup(t)
	ctx := context.Background()
	apply(t, e, "project.configure", config())
	if err := m.Coordinator.Endpoint(ctx, "local", []string{"http://127.0.0.1:1/v1"}, 1, coordinator.Host()); err != nil {
		t.Fatal(err)
	}
	owner, err := m.Coordinator.Register(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.ReserveResources(ctx, owner, "reservation", "run", "local")
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := e.RetireReservation(ctx, r.OperationID, owner.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.RetireReservation(ctx, r.OperationID, owner.ID); err != nil {
		t.Fatal("retirement not idempotent", err)
	}
	next, err := m.Coordinator.Register(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if _, err = e.ReserveResources(ctx, next, "reservation", "run", "local"); err == nil {
		t.Fatal("new owner resumed old intent")
	}
	if _, err = e.ReserveResources(ctx, next, "new-reservation", "new-run", "local"); !errors.Is(err, coordinator.ErrBusy) {
		t.Fatal("new intent bypassed quarantine", err)
	}
	var state string
	if err = e.DB.SQL.QueryRow("SELECT state FROM operations WHERE id=?", r.OperationID).Scan(&state); err != nil || state != "uncertain" {
		t.Fatal(state, err)
	}
	root, err := workspace.Inspect(ctx, p.Root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = next.Claim(ctx, p.ID, "another", []workspace.Identity{root}); !errors.Is(err, coordinator.ErrBusy) {
		t.Fatal("retirement released resource", err)
	}
}

func TestResourceJournalRejectsPolicyAndRootReplacement(t *testing.T) {
	m, e, p := setup(t)
	ctx := context.Background()
	apply(t, e, "project.configure", config())
	if err := m.Coordinator.Endpoint(ctx, "local", []string{"http://127.0.0.1:1/v1"}, 1, coordinator.Host()); err != nil {
		t.Fatal(err)
	}
	owner, err := m.Coordinator.Register(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	_, err = e.reserveResources(ctx, owner, "reservation", "run", "local", func(phase string) error {
		if phase == "after_intent" {
			return errors.New("pause")
		}
		return nil
	})
	if err == nil {
		t.Fatal("missing pause")
	}
	apply(t, e, "project.configure", config())
	if _, err := e.ReserveResources(ctx, owner, "reservation", "run", "local"); err == nil {
		t.Fatal("stale policy admitted")
	}
	var claims int
	if err := m.Coordinator.DB.SQL.QueryRow("SELECT count(*) FROM workspace_claims").Scan(&claims); err != nil || claims != 0 {
		t.Fatal("stale intent acquired workspace", claims, err)
	}
	if b, err := exec.Command("git", "init", "-q", p.Root).CombinedOutput(); err != nil {
		t.Fatal(err, string(b))
	}
	if _, err := e.ReserveResources(ctx, owner, "changed-git", "run", "local"); err == nil {
		t.Fatal("changed registered Git identity admitted")
	}
	old := p.Root + "-old"
	if err := os.Rename(p.Root, old); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(old) })
	if err := os.Mkdir(p.Root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ReserveResources(ctx, owner, "fresh", "run", "local"); err == nil {
		t.Fatal("replaced registered root admitted")
	}
}

func TestResourceJournalWaitsWithoutDuplicatingCapacity(t *testing.T) {
	m, e, p := setup(t)
	ctx := context.Background()
	apply(t, e, "project.configure", config())
	if err := m.Coordinator.Endpoint(ctx, "local", []string{"http://127.0.0.1:1/v1"}, 1, coordinator.Host()); err != nil {
		t.Fatal(err)
	}
	ownerA, err := m.Coordinator.Register(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer ownerA.Close()
	first, err := e.ReserveResources(ctx, ownerA, "same-operation-id", "run", "local")
	if err != nil {
		t.Fatal(err)
	}
	rootB := filepath.Join(filepath.Dir(p.Root), "other-work")
	if err := os.Mkdir(rootB, 0700); err != nil {
		t.Fatal(err)
	}
	projectB, err := m.Init(ctx, rootB)
	if err != nil {
		t.Fatal(err)
	}
	eB, err := m.Open(ctx, projectB.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer eB.DB.Close()
	apply(t, eB, "project.configure", config())
	ownerB, err := m.Coordinator.Register(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer ownerB.Close()
	second, err := eB.ReserveResources(ctx, ownerB, "same-operation-id", "run", "local")
	if !errors.Is(err, coordinator.ErrWaiting) || second.Phase != "capacity_wait" || second.Ticket.Generation != 0 || len(second.Claims) != 1 {
		t.Fatal(second, err)
	}
	if err := ownerA.FinishTicket(ctx, first.Ticket, "never_started"); err != nil {
		t.Fatal(err)
	}
	if err := ownerA.Release(ctx, first.Claims[0], "never_started"); err != nil {
		t.Fatal(err)
	}
	reserved, err := eB.ReserveResources(ctx, ownerB, "same-operation-id", "run", "local")
	if err != nil || reserved.Ticket.Sequence != second.Ticket.Sequence || reserved.Ticket.Generation == 0 {
		t.Fatal(reserved, err)
	}
	var slots int
	if err := m.Coordinator.DB.SQL.QueryRow("SELECT count(*) FROM endpoint_slots").Scan(&slots); err != nil || slots != 1 {
		t.Fatal(slots, err)
	}
}

func TestReservationClaimsEveryEnrolledRepositoryAtomically(t *testing.T) {
	m, e, p := setup(t)
	ctx := context.Background()
	initRepository(t, p.Root)
	nested := filepath.Join(p.Root, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	initRepository(t, nested)
	apply(t, e, "project.configure", config())
	apply(t, e, "plan.put", plan())
	apply(t, e, "repository.enroll", RepositoryEnrollment{ID: "root", PlanID: "plan", Root: p.Root, BaseRef: "main", PlanBranch: "vigil/root", DirtyChoice: "clean", NestedBoundaries: []string{"nested"}})
	apply(t, e, "repository.enroll", RepositoryEnrollment{ID: "nested", PlanID: "plan", Root: nested, BaseRef: "main", PlanBranch: "vigil/nested", DirtyChoice: "clean"})
	if err := m.Coordinator.Endpoint(ctx, "local", []string{"http://127.0.0.1:1/v1"}, 1, coordinator.Host()); err != nil {
		t.Fatal(err)
	}
	owner, err := m.Coordinator.Register(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	reservation, err := e.ReserveResources(ctx, owner, "all-roots", "run", "local")
	if err != nil || len(reservation.Roots) != 2 || len(reservation.Claims) != 2 {
		t.Fatal(reservation, err)
	}
	var count int
	if err := m.Coordinator.DB.SQL.QueryRow("SELECT count(*) FROM workspace_claims WHERE instance_id=?", owner.ID).Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
}
