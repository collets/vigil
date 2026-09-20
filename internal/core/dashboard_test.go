package core

import (
	"context"
	"testing"

	"vigil/internal/store"
)

func TestDashboardShowsReopenedTruthAndLatestHistory(t *testing.T) {
	m, e, p := setup(t)
	apply(t, e, "project.configure", config())
	apply(t, e, "profile.put", profile())
	apply(t, e, "plan.put", plan())
	ctx := context.Background()
	if err := e.DB.Write(ctx, func(tx *store.Tx) error {
		for n := 0; n < 120; n++ {
			if _, err := tx.ExecContext(ctx, "INSERT INTO events(schema_version,occurred_at,kind,payload_json) VALUES(1,?,'fixture','{}')", store.Now()); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reopened, err := m.Open(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.DB.Close()
	snapshot, err := reopened.Dashboard(ctx)
	if err != nil || snapshot.Readiness.Project.Revision != 4 || len(snapshot.Readiness.Tasks) != 2 || len(snapshot.Events) != 100 || snapshot.Events[0].Sequence <= 1 {
		t.Fatal(snapshot, err)
	}
	for n := 1; n < len(snapshot.Events); n++ {
		if snapshot.Events[n].Sequence <= snapshot.Events[n-1].Sequence {
			t.Fatal("history not ordered")
		}
	}
	if snapshot.Readiness.ExecutionEligible {
		t.Fatal("dashboard manufactured execution eligibility")
	}
	var before, after int
	e.DB.SQL.QueryRow("SELECT count(*) FROM events").Scan(&before)
	if _, err := reopened.Dashboard(ctx); err != nil {
		t.Fatal(err)
	}
	e.DB.SQL.QueryRow("SELECT count(*) FROM events").Scan(&after)
	if before != after {
		t.Fatal("dashboard mutated project")
	}
}
