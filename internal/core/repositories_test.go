package core

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initRepository(t *testing.T, root string) {
	t.Helper()
	cmds := [][]string{{"init", "-q", "-b", "main", root}, {"-C", root, "add", "file.txt"}, {"-C", root, "-c", "user.name=Fixture", "-c", "user.email=fixture@invalid", "commit", "-qm", "fixture"}}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range cmds {
		if b, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatal(err, string(b))
		}
	}
}

func TestRepositoryEnrollmentBranchCrashReconciliation(t *testing.T) {
	_, e, p := setup(t)
	initRepository(t, p.Root)
	apply(t, e, "project.configure", config())
	apply(t, e, "plan.put", plan())
	apply(t, e, "repository.enroll", RepositoryEnrollment{ID: "primary", PlanID: "plan", Root: p.Root, BaseRef: "refs/heads/main", PlanBranch: "vigil/fixture", DirtyChoice: "clean"})
	record, err := e.Repository(context.Background(), "primary")
	if err != nil || record.Revision != 1 || record.Baseline.Dirty {
		t.Fatal(record, err)
	}
	var revision int
	if err := e.DB.SQL.QueryRow("SELECT revision FROM project").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("crash after branch effect")
	operation, err := e.prepareRepository(context.Background(), "prepare", "primary", revision, func(point string) error {
		if point == "after_effect" {
			return injected
		}
		return nil
	})
	if !errors.Is(err, injected) || operation.State != "prepared" {
		t.Fatal(operation, err)
	}
	operation, err = e.PrepareRepository(context.Background(), "prepare", "primary", revision)
	if err != nil || operation.State != "observed" || operation.ObservedHeadRef != "refs/heads/vigil/fixture" {
		t.Fatal(operation, err)
	}
	if err := os.WriteFile(filepath.Join(p.Root, "after-prepare.txt"), []byte("preserve replay work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	operation, err = e.PrepareRepository(context.Background(), "prepare", "primary", revision)
	if err != nil || operation.State != "observed" {
		t.Fatal(operation, err)
	}
	b, err := os.ReadFile(filepath.Join(p.Root, "after-prepare.txt"))
	if err != nil || string(b) != "preserve replay work\n" {
		t.Fatal("idempotent replay changed later checkout work", err)
	}
	var count int
	if err := e.DB.SQL.QueryRow("SELECT count(*) FROM repository_branch_operations").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestRepositoryEnrollmentPreservesDirtyWorkAndNestedBoundary(t *testing.T) {
	_, e, p := setup(t)
	initRepository(t, p.Root)
	nested := filepath.Join(p.Root, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	initRepository(t, nested)
	if err := os.WriteFile(filepath.Join(p.Root, "user.txt"), []byte("preserve me\n"), 0600); err != nil {
		t.Fatal(err)
	}
	apply(t, e, "project.configure", config())
	apply(t, e, "plan.put", plan())
	if _, err := e.Apply(context.Background(), Human, envelope(t, e, "repository.enroll", RepositoryEnrollment{ID: "bad", PlanID: "plan", Root: p.Root, BaseRef: "main", PlanBranch: "vigil/bad", DirtyChoice: "clean", NestedBoundaries: []string{"nested"}})); err == nil {
		t.Fatal("dirty checkout accepted as clean")
	}
	apply(t, e, "repository.enroll", RepositoryEnrollment{ID: "primary", PlanID: "plan", Root: p.Root, BaseRef: "main", PlanBranch: "vigil/fixture", DirtyChoice: "include", IncludedPaths: []string{"user.txt"}, NestedBoundaries: []string{"nested"}})
	record, err := e.Repository(context.Background(), "primary")
	if err != nil || !record.Baseline.Dirty || len(record.NestedBoundaries) != 1 {
		t.Fatal(record, err)
	}
	b, _ := os.ReadFile(filepath.Join(p.Root, "user.txt"))
	if string(b) != "preserve me\n" {
		t.Fatal("existing work changed")
	}
	if _, err := e.Apply(context.Background(), Human, envelope(t, e, "repository.enroll", RepositoryEnrollment{ID: "save", PlanID: "plan", Root: p.Root, BaseRef: "main", PlanBranch: "vigil/save", DirtyChoice: "save"})); err == nil {
		t.Fatal("unavailable save accepted")
	}
}
