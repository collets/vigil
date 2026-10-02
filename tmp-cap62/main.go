package main

import (
	"context"
	"fmt"

	"vigil/internal/core"
)

func main() {
	ctx := context.Background()
	stateDir, proj := "/tmp/opencode/vigil-62/state", "ed0018c7b084f111176f8518d57fe8de"
	manager, err := core.OpenManager(ctx, stateDir)
	if err != nil {
		panic(err)
	}
	defer manager.Close()
	engine, err := manager.Open(ctx, proj)
	if err != nil {
		panic(err)
	}
	defer engine.DB.Close()
	snap, err := engine.Dashboard(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Printf("revision=%d state=%s tasks=%d inbox=%d events=%d plans=%d queue=%d activerun=%q\n",
		snap.Readiness.Project.Revision, snap.Readiness.Project.State,
		len(snap.Readiness.Tasks), len(snap.Inbox), len(snap.Events),
		len(snap.Plans), len(snap.Queue), snap.ActiveRun)
	for _, t := range snap.Readiness.Tasks {
		fmt.Printf("task %s rev=%d state=%s issues=%v checks=%v\n", t.ID, t.Revision, t.State, t.Issues, t.RequiredChecks)
	}
}
