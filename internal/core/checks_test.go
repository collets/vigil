package core

import (
	"context"
	"strings"
	"testing"

	"vigil/internal/policy"
)

func TestReadinessRequiresCheckDefinitionsAndManualSetupEvidence(t *testing.T) {
	_, e, _ := setup(t)
	ctx := context.Background()
	c := config()
	c.CheckDefinitions = nil
	apply(t, e, "project.configure", c)
	apply(t, e, "profile.put", profile())
	p := plan()
	p.Tasks[0].ManualPrerequisites = []policy.ManualPrerequisite{{ID: "device", Description: "Fixture device connected"}}
	apply(t, e, "plan.put", p)
	r, err := e.Readiness(ctx)
	if err != nil || !strings.Contains(strings.Join(r.DefinitionIssues, " "), "required check definition missing") || !strings.Contains(strings.Join(r.Tasks[0].Issues, " "), "manual prerequisite unsatisfied") {
		t.Fatal(r, err)
	}
	apply(t, e, "project.configure", config())
	p.Tasks[0].ManualPrerequisites[0].Satisfied = true
	if _, err := e.Apply(ctx, Human, envelope(t, e, "plan.put", p)); err == nil {
		t.Fatal("prerequisite completion without evidence accepted")
	}
	p.Tasks[0].ManualPrerequisites[0].Evidence = "Human checked disposable fixture connection"
	apply(t, e, "plan.put", p)
	r, err = e.Readiness(ctx)
	if err != nil || len(r.DefinitionIssues) != 0 || len(r.Tasks[0].Issues) != 0 || r.ExecutionEligible {
		t.Fatal(r, err)
	}
	for _, table := range []string{"manual_checks", "acceptances", "runs"} {
		var count int
		if err := e.DB.SQL.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatal("setup evidence manufactured quality/execution evidence", table, count, err)
		}
	}
	// Required project checks remain in the union when a task supplies none.
	p.Tasks[0].Checks = nil
	apply(t, e, "plan.put", p)
	r, err = e.Readiness(ctx)
	if err != nil || !policy.Contains(r.Tasks[0].RequiredChecks, "project-check") {
		t.Fatal("task removed required project check", r, err)
	}
}
