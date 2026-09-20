package policy

import "testing"

func TestCheckDefinitionsAreBoundedAndRelative(t *testing.T) {
	valid := CheckDefinition{ID: "unit", Argv: []string{"go", "test", "./..."}, Cwd: ".", TimeoutMS: 1000}
	if err := valid.Validate(2000); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*CheckDefinition){func(c *CheckDefinition) { c.Argv = nil }, func(c *CheckDefinition) { c.Argv = []string{""} }, func(c *CheckDefinition) { c.Argv = []string{"go\x00"} }, func(c *CheckDefinition) { c.Cwd = "../outside" }, func(c *CheckDefinition) { c.Cwd = "/host" }, func(c *CheckDefinition) { c.Cwd = "src/**" }, func(c *CheckDefinition) { c.TimeoutMS = 3000 }, func(c *CheckDefinition) { c.ID = "bad/id" }} {
		c := valid
		change(&c)
		if c.Validate(2000) == nil {
			t.Fatal("unsafe check definition accepted", c)
		}
	}
}

func TestManualPrerequisitesNeedExplicitEvidence(t *testing.T) {
	p := ManualPrerequisite{ID: "device", Description: "Test device connected"}
	if err := validatePrerequisites([]ManualPrerequisite{p}); err != nil {
		t.Fatal(err)
	}
	p.Satisfied = true
	if validatePrerequisites([]ManualPrerequisite{p}) == nil {
		t.Fatal("completion without evidence accepted")
	}
	p.Evidence = "Human verified fixture device connection"
	if err := validatePrerequisites([]ManualPrerequisite{p}); err != nil {
		t.Fatal(err)
	}
	if validatePrerequisites([]ManualPrerequisite{p, p}) == nil {
		t.Fatal("duplicate prerequisite accepted")
	}
}
