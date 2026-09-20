package policy

import "testing"

func TestRestrictionsAccumulate(t *testing.T) {
	r, err := Resolve([]Layer{{Name: "project", Deny: []string{"push"}, Checks: []string{"build"}, LimitMS: 100}, {Name: "task", Checks: []string{"test", "build"}, LimitMS: 200}, {Name: "role", Deny: []string{"commit"}, LimitMS: 50}})
	if err != nil || r.LimitMS != 50 || len(r.Deny) != 2 || len(r.Checks) != 2 || len(r.Origins["check:build"]) != 2 {
		t.Fatal(r, err)
	}
}
