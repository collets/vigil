package scenario

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// stageA materializes the disposable repository, the local bare remote and the
// private state directory, and verifies the fixture is exactly what the manifest
// claims before any application command touches it.
func (w *walkthrough) stageA(ctx context.Context) {
	w.fixtureBase = filepath.Join(w.root, "work")
	state := filepath.Join(w.root, "state")
	bare := filepath.Join(w.root, "fixture-origin.git")
	if err := os.MkdirAll(state, 0o700); err != nil {
		panic(&ScenarioAbort{Step: "stage-a-state", Err: err})
	}
	if err := RequireBareRemoteUnderRoot(w.root, bare); err != nil {
		panic(&ScenarioAbort{Step: "stage-a-remote-root", Err: err})
	}
	if err := NewFixture().Materialize(w.fixtureBase); err != nil {
		panic(&ScenarioAbort{Step: "stage-a-fixture", Err: err})
	}
	// Verify the materialized bytes match the manifest digests exactly, so the
	// evidence can never describe content the run did not actually use.
	for _, file := range w.report.Fixture.Files {
		raw, err := os.ReadFile(filepath.Join(w.fixtureBase, filepath.FromSlash(file.Path)))
		if err != nil {
			panic(&ScenarioAbort{Step: "stage-a-fixture-read", Err: err})
		}
		w.assert("fixture-digest:"+file.Path, Digest(raw) == file.Digest, Digest(raw), "materialized fixture content must match the manifest")
	}
	baseCommit, err := InitRepository(ctx, w.fixtureBase)
	if err != nil {
		panic(&ScenarioAbort{Step: "stage-a-git-init", Err: err})
	}
	if err := InitBareRemote(ctx, bare); err != nil {
		panic(&ScenarioAbort{Step: "stage-a-bare-remote", Err: err})
	}
	remoteHead, err := git(ctx, w.fixtureBase, "rev-parse", "HEAD")
	if err != nil {
		panic(&ScenarioAbort{Step: "stage-a-head", Err: err})
	}
	w.assert("fixture-base-commit", baseCommit == remoteHead && len(baseCommit) == 40, baseCommit, "the fixture base commit must be a real, resolvable object")
	// The injected defect must actually be present and must actually fail the
	// committed check. A rehearsal that cannot fail its own check proves
	// nothing about repair.
	w.assert("committed-content-verified", fileContains(w.fixtureBase, SourcePath, CommittedGreeting), CommittedGreeting,
		"the committed fixture must start from the previous-stage content the implementation replaces")
	if err := w.runFixtureCheck(ctx); err != nil {
		panic(&ScenarioAbort{Step: "stage-a-committed-check-fails", Err: err})
	}
	w.bareRemote = bare
	// The remote is registered here, before the repository is enrolled, because
	// enrollment records the remote's identity as part of the accepted baseline.
	// Adding it afterwards would leave the enrolled identity describing a
	// repository with no remote, and the push path would then correctly refuse a
	// destination it had never verified.
	if err := AddRemote(ctx, w.fixtureBase, FixtureRemoteName, w.bareRemote); err != nil {
		panic(&ScenarioAbort{Step: "stage-a-fixture-remote", Err: err})
	}
	// The bare remote is seeded with the base branch, because every real hosting
	// destination already has one and the draft path must resolve the base it
	// proposes to merge into. An empty bare repository would make draft delivery
	// fail for a reason that has nothing to do with the product: a destination
	// with no base branch is not a destination a pull request can target.
	if _, err := git(ctx, w.fixtureBase, "push", "-q", FixtureRemoteName, baseCommit+":refs/heads/main"); err != nil {
		panic(&ScenarioAbort{Step: "stage-a-remote-seed", Err: err})
	}
	w.report.Project = ProjectIdentity{Root: w.fixtureBase, StateDir: state, BaseOIDs: map[string]string{"fixture_base": baseCommit}}
	w.note("The committed fixture fails its own check, and the implementation attempt writes the specified greeting with a trailing space. The automated check judges the specified text and passes; the fresh reviewer reports the formatting defect, and the bounded repair removes it.")
}

// fileContains reports whether a repository-relative file exists with the given
// content. Comparison is exact, including the trailing newline.
func fileContains(root, relative, want string) bool {
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		return false
	}
	return string(raw) == want
}

// runFixtureCheck runs the fixture's own committed check script in a throwaway
// copy of the repository, to establish the pre-implementation baseline without
// involving the application. It must fail: the committed content is wrong, and a
// rehearsal whose check already passed would prove nothing about the work.
//
// It also runs the check against the specified greeting, and requires that to
// pass, which establishes the boundary the review/repair cycle depends on: an
// automated check judges the specified text, and does not judge trailing
// whitespace. The copy is discarded, so the committed state is untouched.
func (w *walkthrough) runFixtureCheck(ctx context.Context) error {
	contents := fixtureFiles()

	run := func(source string) (string, int, error) {
		scratch, err := os.MkdirTemp(w.root, "check-baseline-")
		if err != nil {
			return "", 0, err
		}
		defer os.RemoveAll(scratch)
		for path, content := range contents {
			full := filepath.Join(scratch, filepath.FromSlash(path))
			if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
				return "", 0, err
			}
			if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
				return "", 0, err
			}
		}
		if err := os.WriteFile(filepath.Join(scratch, filepath.FromSlash(SourcePath)), []byte(source), 0o600); err != nil {
			return "", 0, err
		}
		if err := os.Chmod(filepath.Join(scratch, filepath.FromSlash(CheckScript)), 0o700); err != nil {
			return "", 0, err
		}
		command := exec.CommandContext(ctx, "/bin/sh", filepath.Join(scratch, filepath.FromSlash(CheckScript)))
		command.Dir = scratch
		combined, runErr := command.CombinedOutput()
		code := 0
		var exit *exec.ExitError
		switch {
		case runErr == nil:
		case errors.As(runErr, &exit):
			code = exit.ExitCode()
		default:
			return string(combined), 0, runErr
		}
		return string(combined), code, nil
	}

	committedOutput, committedCode, err := run(CommittedGreeting)
	if err != nil {
		return fmt.Errorf("fixture check did not run: %w", err)
	}
	if committedCode != 1 {
		return fmt.Errorf("the committed fixture check exited %d; the committed content must fail its own check", committedCode)
	}
	w.assert("committed-check-fails", true, fmt.Sprintf("exit %d: %s", committedCode, truncate(committedOutput, 160)),
		"the committed check must exit nonzero on the committed wrong content")

	// The check passes on the specified greeting, and it also passes on the
	// injected-defect greeting, because it does not judge trailing whitespace.
	for label, source := range map[string]string{
		"specified":       ExpectedGreeting,
		"injected-defect": InjectedDefectGreeting,
	} {
		output, code, err := run(source)
		if err != nil {
			return fmt.Errorf("fixture check did not run for the %s case: %w", label, err)
		}
		if code != 0 {
			return fmt.Errorf("the fixture check exited %d on the %s greeting: %s", code, label, truncate(output, 160))
		}
		w.assert("check-passes-on-"+label, true, "exit 0", "the automated check must judge the specified text, not its formatting")
	}
	return nil
}
