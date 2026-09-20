package policy

import "testing"

func TestRestrictionsAccumulate(t *testing.T) {
	r, err := Resolve([]Layer{{Name: "project", Deny: []string{"push"}, Checks: []string{"build"}, LimitMS: 100}, {Name: "task", Checks: []string{"test", "build"}, LimitMS: 200}, {Name: "role", Deny: []string{"commit"}, LimitMS: 50}})
	if err != nil || r.LimitMS != 50 || len(r.Deny) != 2 || len(r.Checks) != 2 || len(r.Origins["check:build"]) != 2 {
		t.Fatal(r, err)
	}
}

func TestRestrictionIntersectionCannotWiden(t *testing.T) {
	r, err := Resolve([]Layer{{Name: "project", Restrictions: Restrictions{"profile_ids": {"local", "review"}}}, {Name: "plan", Restrictions: Restrictions{"profile_ids": {"local", "cloud"}}}, {Name: "task", Restrictions: Restrictions{"profile_ids": {"cloud"}, "operation_categories": {}}}})
	if err != nil || r.Restrictions.Allows("profile_ids", "cloud") || r.Restrictions.Allows("profile_ids", "local") || r.Restrictions.Allows("operation_categories", "commit") || len(r.Origins["restriction:profile_ids"]) != 3 {
		t.Fatal(r, err)
	}
	if !(Restrictions{}).Allows("profile_ids", "local") {
		t.Fatal("omitted dimension should not impose a restriction")
	}
	for _, bad := range []Restrictions{{"unknown": {}}, {"profile_ids": nil}, {"profile_ids": {"*"}}, {"profile_ids": {"local", "local"}}, {"operation_categories": {"merge"}}} {
		if _, err := Resolve([]Layer{{Restrictions: bad}}); err == nil {
			t.Fatal("invalid restriction accepted", bad)
		}
	}
}
