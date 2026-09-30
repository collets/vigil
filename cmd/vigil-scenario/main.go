// Command vigil-scenario runs the Stage 5.7 autonomous end-to-end qualification
// walkthrough against a real production binary.
//
// It is a development qualification tool, not a product path. It creates no
// delivery authority, never enables production dispatch, never contacts a real
// remote or a metered provider, and it runs entirely inside an agent-owned
// disposable root. Every human-gated step is recorded as a Stage 8 gate with its
// exact blocker rather than simulated.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"vigil/internal/scenario"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "vigil-scenario: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		binary  = flag.String("binary", "bin/vigil", "production binary under qualification")
		root    = flag.String("root", "", "agent-owned disposable scenario root (required)")
		commit  = flag.String("source-commit", "", "candidate commit the binary is expected to come from")
		stage   = flag.String("stage", "", "limit the run to one checkpoint: A, B, C or D")
		out     = flag.String("out", "", "write the JSON report to this path (default: ROOT/report.json)")
		keep    = flag.Bool("keep", true, "retain the disposable root for inspection")
		docker  = flag.Bool("allow-docker", false, "opt in to container-boundary probes")
		live    = flag.Bool("allow-local-inference", false, "opt in to bounded live local llama turns")
		codex   = flag.Bool("allow-codex-live", false, "request the contained Codex route (refused in this build)")
		remote  = flag.Bool("allow-remote-delivery", false, "request real delivery (refused in this build)")
		version = flag.String("harness-version", "0.21.3", "declared harness version for the profile")
		model   = flag.String("model", "qwen3.8-27b-local", "declared model for the profile")
		verbose = flag.Bool("verbose", false, "print each production command as it runs")
	)
	flag.Parse()

	if strings.TrimSpace(*root) == "" {
		return errors.New("--root is required; the scenario must run inside an agent-owned disposable directory")
	}
	absoluteRoot, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	absoluteRoot = filepath.Clean(absoluteRoot)
	if err := os.MkdirAll(absoluteRoot, 0o700); err != nil {
		return err
	}
	// Refuse to run inside a checkout: the scenario creates and removes its own
	// root, and it must never do that to a repository the operator owns.
	if inside := insideCheckout(absoluteRoot); inside != "" {
		return fmt.Errorf("refusing to use %s: it is inside the checkout at %s", absoluteRoot, inside)
	}

	config := scenario.Config{
		Binary:         *binary,
		Root:           absoluteRoot,
		SourceCommit:   *commit,
		OptIns:         scenario.OptIns{Docker: *docker, LocalInference: *live, CodexLive: *codex, RemoteDelivery: *remote},
		Stage:          scenario.Stage(strings.ToUpper(strings.TrimSpace(*stage))),
		HarnessVersion: *version,
		Model:          *model,
	}
	switch config.Stage {
	case "", "A", "B", "C", "D":
	default:
		return fmt.Errorf("--stage must be A, B, C or D, not %q", *stage)
	}

	// A bounded context: the run itself enforces per-step and total bounds, and
	// an operator interrupt is an honest recorded failure rather than a partial
	// report claimed as a pass.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Minute)
	defer cancel()

	started := time.Now()
	report, failure := scenario.Run(ctx, config)
	if report == nil {
		return failure
	}
	if *verbose {
		for _, step := range report.Steps {
			fmt.Fprintf(os.Stderr, "  %3d %-46s exit=%d %dms %s\n", step.Index, step.Name, step.ExitCode, step.DurationMS, step.Error)
		}
	}

	destination := *out
	if destination == "" {
		destination = filepath.Join(absoluteRoot, "report.json")
	}
	if err := writeReport(destination, report); err != nil {
		return err
	}
	printSummary(report, destination, started)

	if !*keep {
		// Only the agent-owned root is removed, and only after the report has
		// been written.
		if err := os.RemoveAll(absoluteRoot); err != nil {
			return fmt.Errorf("remove disposable root: %w", err)
		}
	}
	// A completed walkthrough that recorded honest gaps exits zero: the gaps are
	// the deliverable, not a failure of the tool. A nonzero exit means a stage
	// aborted, which is a different and louder outcome. The gap list is printed and
	// carried in the report so neither can be missed.
	if failure != nil {
		return failure
	}
	return nil
}

func writeReport(path string, report *scenario.Report) error {
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func printSummary(report *scenario.Report, destination string, started time.Time) {
	fmt.Print(summaryLines(report, destination, time.Since(started).Round(time.Millisecond)))
}

// summaryLines renders the human-facing summary.
//
// It is a separate function returning a string rather than a sequence of
// `fmt.Printf` calls because this is the one place the report's central property —
// that a gap can never be quietly omitted — is rendered for a human reader, and
// the previous revision silently dropped a whole evidence class from the counts
// line while the suite stayed green. A renderer with no test is how that happens
// again; `TestSummaryLinesReconcileEveryClass` pins it.
func summaryLines(report *scenario.Report, destination string, elapsed time.Duration) string {
	lines := []string{
		"Stage 5.7 autonomous qualification",
		fmt.Sprintf("  scenario     %s (stage %s)", report.ScenarioID, report.Stage),
		fmt.Sprintf("  binary       %s sha256=%s", report.Binary.Path, report.Binary.SHA256[:16]),
		fmt.Sprintf("  platform     %s/%s", report.Platform.OS, report.Platform.Arch),
		fmt.Sprintf("  steps        %d recorded, %d live model turns", len(report.Steps), report.LiveTurn.Attempted),
		fmt.Sprintf("  assertions   %d", countAssertions(report)),
	}
	// Each section is counted separately: a milestone step, a recovery case and a
	// requirement are different claims, and a combined total would let one
	// section's passes disguise another's gaps.
	//
	// Every class the report can hold is printed, including `partial`, and the
	// line is built so the printed numbers must add up to the printed total. A
	// total that silently does not reconcile is worse than a longer line, because
	// the reader cannot tell which reading is correct.
	for _, section := range []string{"milestone", "recovery", "requirements"} {
		lines = append(lines, countLine(section, report.Matrix.Counts(section)))
	}
	lines = append(lines, fmt.Sprintf("  report       %s", destination))
	// An aborted run leaves rows undecided. Saying so up front stops an empty
	// evidence column from being read as a set of passes.
	if report.Aborted {
		lines = append(lines, "  state        ABORTED: the rows below were never decided, and none of them is a result")
	}
	for _, gap := range report.Gaps {
		lines = append(lines, "  gap          "+gap)
	}
	// Requirement gaps are printed under their own label rather than folded into
	// the list above, so a partial requirement is not mistaken for a recovery case
	// that failed.
	for _, gap := range report.Matrix.RequirementGapList {
		lines = append(lines, "  req gap      "+gap)
	}
	for _, pending := range report.Pending {
		lines = append(lines, "  pending      "+pending)
	}
	for _, limitation := range report.Limits {
		lines = append(lines, "  limitation   "+limitation)
	}
	lines = append(lines, fmt.Sprintf("  elapsed      %s", elapsed))
	return strings.Join(lines, "\n") + "\n"
}

// evidenceClasses lists every class a matrix row can hold, in print order.
//
// It is a declared list rather than an inline set of arguments at the call site
// because a new class added to the matrix but not here would be silently omitted
// from the rendered summary — the exact defect this refactor exists to prevent.
var evidenceClasses = []struct{ label, class string }{
	{"automated", scenario.EvidenceAutomated},
	{"partial", scenario.Partial},
	{"reused", scenario.EvidenceReused},
	{"pending", scenario.EvidencePendingStage8},
	{"unmet", scenario.EvidenceUnmet},
}

// countLine renders one section's counts. The total is the sum of the rows, and
// the printed per-class numbers are the only way to reconstruct it, so a class
// that is counted but not printed would make the line unreconcilable.
func countLine(section string, counts map[string]int) string {
	printed := 0
	fields := make([]string, 0, len(evidenceClasses))
	for _, entry := range evidenceClasses {
		count := counts[entry.class]
		fields = append(fields, fmt.Sprintf("%s=%d", entry.label, count))
		printed += count
	}
	total := 0
	for _, count := range counts {
		total += count
	}
	if printed != total {
		// A class the report holds that the renderer does not print. Rendering
		// an explicit contradiction beats rendering a summary that quietly does
		// not add up.
		fields = append(fields, fmt.Sprintf("UNPRINTED=%d", total-printed))
	}
	return fmt.Sprintf("  %-12s total=%d %s", section, total, strings.Join(fields, " "))
}

func countAssertions(report *scenario.Report) int {
	passed := 0
	for _, check := range report.Checks {
		if check.Pass {
			passed++
		}
	}
	return passed
}

// insideCheckout reports the working-tree root containing path, if any.
//
// It asks Git itself rather than looking for a `.git` entry, because an empty
// stray `.git` directory (for example under a shared temporary directory) is not
// a repository and must not make every path beneath it unusable.
func insideCheckout(path string) string {
	command := exec.Command("git", "rev-parse", "--show-toplevel")
	command.Dir = path
	// Ambient configuration is irrelevant here, but a prompt or pager must never
	// be possible.
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1"}
	out, err := command.Output()
	if err != nil {
		// Not a working tree; that is the safe answer.
		return ""
	}
	top := strings.TrimSpace(string(out))
	if top == "" || top == path {
		// The root itself is a working tree, which the scenario also refuses.
		return top
	}
	return top
}
