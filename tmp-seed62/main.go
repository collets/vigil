package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"vigil/internal/core"
	"vigil/internal/policy"
	"vigil/internal/store"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func apply(ctx context.Context, engine *core.Engine, kind string, payload any) {
	var revision int
	must(engine.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&revision))
	raw, _ := json.Marshal(payload)
	_, err := engine.Apply(ctx, core.Human, core.Envelope{CommandID: store.ID(), ExpectedRevision: revision, Kind: kind, Payload: raw})
	must(err)
}

func main() {
	ctx := context.Background()
	stateDir, root := os.Args[1], os.Args[2]
	manager, err := core.OpenManager(ctx, stateDir)
	must(err)
	defer manager.Close()
	project, err := manager.Init(ctx, root)
	must(err)
	engine, err := manager.Open(ctx, project.ID)
	must(err)
	defer engine.DB.Close()
	config := policy.Config{ModelPolicy: "local_only", RequiredChecks: []string{"test"}, CheckDefinitions: []policy.CheckDefinition{{ID: "test", Argv: []string{"true"}, Cwd: ".", TimeoutMS: 1000}}, TaskLimitMS: 60000, AttemptLimitMS: 30000, RepairLimit: 1, SupervisorProfile: "local", ApprovalMode: "supervised"}
	profile := policy.Profile{ID: "local", Harness: "hermes", Version: "fixture", Model: "fixture", Provider: "custom", CredentialRef: "env:FIXTURE_KEY", Roles: []string{"implementation", "review", "supervisor"}, EndpointID: "fixture-endpoint", LocalInference: true, AuxiliaryLocal: true, DelegationDisabled: true}
	task := policy.Task{ID: "first-task", Objective: "fixture", Criteria: []policy.Criterion{{ID: "device", Text: "fixture"}, {ID: "visual", Text: "fixture", Manual: true}}, Scope: []string{"src/**"}, Implementation: "local", Reviewer: "local", Checks: []string{"test"}, Difficulty: "small", Rationale: "fixture", ActiveLimitMS: 30000, RepairLimit: 1}
	apply(ctx, engine, "project.configure", config)
	apply(ctx, engine, "profile.put", profile)
	apply(ctx, engine, "plan.put", core.Plan{ID: "first-plan", Title: "Fixture", Specification: "fixture", Approved: true, Tasks: []policy.Task{task}})
	var revision int
	must(engine.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&revision))
	_, err = engine.QueuePlan(ctx, "queue-1", revision, "first-plan", 0)
	must(err)
	fmt.Println("PROJECT=" + project.ID)
}
