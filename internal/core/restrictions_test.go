package core

import (
	"context"
	"strings"
	"testing"

	"vigil/internal/policy"
	"vigil/internal/store"
)

func TestRestrictionsEnforcedByReadinessAndAuthority(t *testing.T) {
	_, e, _ := setup(t)
	c := config()
	c.Restrictions = policy.Restrictions{"profile_ids": {"local"}, "operation_categories": {"commit", "push"}}
	apply(t, e, "project.configure", c)
	apply(t, e, "profile.put", profile())
	p := plan()
	p.Restrictions = policy.Restrictions{"profile_ids": {"cloud"}, "operation_categories": {"commit"}}
	p.Tasks[0].Restrictions = policy.Restrictions{"profile_ids": {"local"}, "operation_categories": {"commit", "push"}}
	apply(t, e, "plan.put", p)
	r, err := e.Readiness(context.Background())
	if err != nil || !strings.Contains(strings.Join(r.Tasks[0].Issues, " "), "profile denied by enclosing restriction") {
		t.Fatal(r, err)
	}
	op := OperationRequest{Category: "push", ResourceDigest: store.Digest([]byte("fixture")), ArgumentsDigest: store.Digest([]byte("args")), PlanID: "plan", TaskID: "first", TaskRevision: 1}
	if _, err := e.Apply(context.Background(), Human, envelope(t, e, "operation.request", op)); err == nil {
		t.Fatal("task widened plan operation restriction")
	}
	op.Category = "commit"
	request := apply(t, e, "operation.request", op)
	grant := apply(t, e, "permission.grant", GrantRequest{RequestID: resultString(t, request, "request_id"), Scope: "once", Decision: "allow"})
	// Changing only plan restrictions must still retire previously granted authority.
	p.Restrictions["operation_categories"] = []string{}
	apply(t, e, "plan.put", p)
	start := envelope(t, e, "operation.start", map[string]string{"operation_id": resultString(t, request, "operation_id"), "grant_id": resultString(t, grant, "grant_id")})
	if _, err := e.Apply(context.Background(), Core, start); err == nil {
		t.Fatal("old grant survived plan restriction revision")
	}
	// Widening the policy later cannot revive the old operation/grant pair.
	p.Restrictions["operation_categories"] = []string{"commit"}
	apply(t, e, "plan.put", p)
	start = envelope(t, e, "operation.start", map[string]string{"operation_id": resultString(t, request, "operation_id"), "grant_id": resultString(t, grant, "grant_id")})
	if _, err := e.Apply(context.Background(), Core, start); err == nil {
		t.Fatal("old grant revived after policy widening")
	}
}
