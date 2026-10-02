package tui

import (
	"context"
	"fmt"
	"io"

	tea "charm.land/bubbletea/v2"
	"vigil/internal/coordinator"
	"vigil/internal/core"
	"vigil/internal/supervisor"
)

func RunProject(ctx context.Context, engine *core.Engine, coordination *coordinator.Coordinator, input io.Reader, output io.Writer) error {
	return RunProjectWithOwner(ctx, engine, coordination, nil, input, output)
}

func RunProjectWithOwner(ctx context.Context, engine *core.Engine, coordination *coordinator.Coordinator, owner *supervisor.InteractiveOwner, input io.Reader, output io.Writer) error {
	projects := []ProjectRef{{ID: engine.ProjectID}}
	if coordination != nil {
		if listed, err := listRegistered(ctx, coordination); err == nil {
			projects = listed
		}
	}
	return runWith(ctx, engine, coordination, owner, projects, func(id string) (source, mutator, error) {
		return nil, nil, fmt.Errorf("project switching needs the state directory opener")
	}, input, output)
}

// RunProjectWithProjects opens the shell with the full registered project
// list and a switcher. The PROJECT_ID argument still selects the opening
// project; P offers the rest.
func RunProjectWithProjects(ctx context.Context, engine *core.Engine, coordination *coordinator.Coordinator, owner *supervisor.InteractiveOwner, projects []ProjectRef, open func(id string) (*core.Engine, *coordinator.Coordinator, *supervisor.InteractiveOwner, error), input io.Reader, output io.Writer) error {
	return runWith(ctx, engine, coordination, owner, projects, func(id string) (source, mutator, error) {
		next, nextCoordination, nextOwner, err := open(id)
		if err != nil {
			return nil, nil, err
		}
		_ = nextCoordination
		return next.Dashboard, projectMutator(next, coordination, nextOwner), nil
	}, input, output)
}

func runWith(ctx context.Context, engine *core.Engine, coordination *coordinator.Coordinator, owner *supervisor.InteractiveOwner, projects []ProjectRef, onSwitch func(id string) (source, mutator, error), input io.Reader, output io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	initial := model{ctx: ctx, load: engine.Dashboard, mutate: projectMutator(engine, coordination, owner), loading: true, width: 80, height: 24, projects: projects, onSwitch: onSwitch}
	final, err := tea.NewProgram(initial, tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(output)).Run()
	if err != nil {
		return fmt.Errorf("run dashboard: %w", err)
	}
	if m, ok := final.(model); ok && m.snapshot == nil {
		return m.err
	}
	return nil
}

func listRegistered(ctx context.Context, coordination *coordinator.Coordinator) ([]ProjectRef, error) {
	rows, err := coordination.DB.SQL.QueryContext(ctx, "SELECT id,root FROM project_registry ORDER BY created_at,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var refs []ProjectRef
	for rows.Next() {
		var ref ProjectRef
		if err := rows.Scan(&ref.ID, &ref.Root); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}
