package core

import (
	"context"
	"strings"
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

func TestDashboardBaselineHealthIsTaskScoped(t *testing.T) {
	_, e, _ := setup(t)
	apply(t, e, "project.configure", config())
	apply(t, e, "profile.put", profile())
	apply(t, e, "plan.put", plan())
	ctx := context.Background()
	digestA, digestB := strings.Repeat("a", 64), strings.Repeat("b", 64)
	constant := strings.Repeat("c", 64)
	if err := e.DB.Write(ctx, func(tx *store.Tx) error {
		insert := `INSERT INTO quality_scopes_v2(id,target_kind,plan_id,plan_revision,task_id,task_revision,repository_set_digest,repository_manifest_json,criteria_digest,definition_digest,config_digest,check_set_digest,reviewer_profile_id,reviewer_profile_revision,reviewer_profile_digest,instruction_digest,created_at) VALUES(?,'task','plan',1,?,1,?,'{}',?,?,?,?,? ,1,?,?,?)`
		if _, err := tx.ExecContext(ctx, insert, strings.Repeat("1", 64), "first", digestA, constant, constant, constant, constant, "local", constant, constant, store.Now()); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, insert, strings.Repeat("2", 64), "second", digestB, constant, constant, constant, constant, "local", constant, constant, store.Now())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	_, err := e.DB.Command(ctx, store.Command{ID: "baseline-task-scope", Actor: "fixture_human", Kind: "quality.baseline.approve", Args: []byte(`{"fixture":true}`)}, func(tx *store.Tx) (any, error) {
		_, err := tx.ExecContext(ctx, `INSERT INTO baseline_exceptions_v2(id,check_id,check_definition_digest,base_repository_set_digest,failure_identities_json,paths_json,rationale,actor,command_id,approved_at) VALUES('baseline-first','test',?,?,'[]','[]','fixture','fixture_human','baseline-task-scope',?)`, constant, digestA, store.Now())
		return map[string]bool{"created": true}, err
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := e.Dashboard(ctx)
	if err != nil {
		t.Fatal(err)
	}
	health := map[string]bool{}
	for _, task := range snapshot.Tasks {
		health[task.ID] = task.BaselineUnhealthy
	}
	if !health["first"] || health["second"] {
		t.Fatal("baseline health leaked across task scopes", health)
	}
}
