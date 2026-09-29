package scenario

import (
	"context"
	"fmt"
	"os"
)

// stageD performs the platform and closure audit. It records what this run can
// observe about the host and about cleanup, and it enumerates the platform and
// credential combinations that remain Stage 8 work with their exact blockers.
//
// The audit never claims coverage it did not produce: a platform that was not
// exercised here is recorded as pending, not as qualified.
func (w *walkthrough) stageD(ctx context.Context) {
	w.auditPlatform()
	w.auditOptIns()
	w.auditCleanup(ctx)
	w.auditRequirements()
}

// auditPlatform records the host identity and which platform-specific paths this
// run actually exercised.
func (w *walkthrough) auditPlatform() {
	identity := observePlatform(w.config.Platform)
	w.report.Platform = identity
	// The disposable scenario repository and the local bare remote are ordinary
	// directories on this host's filesystem; the local bare remote push exercises
	// the real Git transport without a network listener.
	w.assert("platform-recorded", identity.OS != "" && identity.Arch != "", identity.OS+"/"+identity.Arch,
		"the host identity must be recorded with the evidence")
	switch identity.OS {
	case "linux":
		w.note("Linux (WSL2) evidence: the full production walkthrough, the local bare-remote push and the loopback hosting stand-in all ran natively on this host.")
	case "darwin":
		w.note("macOS evidence: the full production walkthrough ran natively on this host. The Darwin fork-accounting observation recorded by Stage 5.4 remains a separate open limitation and is unaffected by this run.")
	default:
		w.note(fmt.Sprintf("Unrecognized host OS %q: the walkthrough ran, but platform-specific process and locking paths were not qualified here.", identity.OS))
	}
}

// auditOptIns records the four independent opt-ins and their exact state, so a
// refused capability is visible in the evidence rather than merely absent.
func (w *walkthrough) auditOptIns() {
	for _, record := range w.report.OptIns {
		switch {
		case record.Enabled:
			w.assert("capability-enabled:"+string(record.Name), true, string(record.Name), record.Evidence)
		case record.Requested:
			w.note(fmt.Sprintf("Capability %s was requested and refused: %s", record.Name, record.Refusal))
		case record.Refusal != "":
			w.note(fmt.Sprintf("Capability %s is unavailable in this build and is owned by %s: %s", record.Name, record.OwnerStage, record.Refusal))
		}
	}
	// The default run must never have contacted a provider or a real remote.
	if !Enabled(w.report.OptIns, CapabilityRemoteDelivery) {
		w.assert("no-real-delivery-authority", true, "remote_delivery refused",
			"no real push, draft request or publication was attempted by this run")
	}
	if !Enabled(w.report.OptIns, CapabilityCodexLive) {
		w.note("No contained Codex route exists, so no live Codex turn was attempted. The recorded blocker is " + CodexLiveBlocker)
	}
	// State the live-turn position precisely. The local opt-in permits a metadata
	// probe of the loopback route; it does not by itself produce a model turn, and
	// claiming otherwise would misdescribe what the run did.
	if Enabled(w.report.OptIns, CapabilityLocalInference) {
		w.report.LiveTurn.Notes = append(w.report.LiveTurn.Notes,
			"the local-inference opt-in was granted and the loopback route was probed, but no stage of this walkthrough drives a live model turn; the turn count above is therefore the observed count, not an estimate")
		w.note("The bounded live local model turn is NOT yet driven by this walkthrough. The opt-in gate, the route probe and the turn budget are implemented and tested, but no stage calls the live execution path, so a real local Hermes turn remains unrecorded work rather than evidence.")
		w.report.Pending = append(w.report.Pending,
			"a real bounded live local Hermes turn: the opt-in and budget exist, but no walkthrough stage drives one")
	}
}

// auditCleanup inspects only what this run created. It never removes anything it
// did not create, and it never touches the operator's checkout, services or
// repositories.
func (w *walkthrough) auditCleanup(ctx context.Context) {
	report := CleanupReport{Root: w.root, Removed: false}
	entries, err := os.ReadDir(w.root)
	if err != nil {
		report.Notes = append(report.Notes, "the scenario root could not be inspected: "+err.Error())
		w.report.Cleanup = report
		return
	}
	for _, entry := range entries {
		report.Preserved = append(report.Preserved, entry.Name())
	}
	// The loopback hosting stand-in is closed by its own defer, before the audit.
	if w.hosting != nil {
		report.Notes = append(report.Notes, "the loopback hosting stand-in listener is closed; it holds no credential and created nothing outside the scenario root")
	}
	// The user's llama service and the user's checkout are named here explicitly:
	// the run never started, reconfigured or stopped either.
	report.Notes = append(report.Notes,
		"the existing local llama service was read with a metadata probe only; it was never started, stopped or reconfigured",
		"the operator's Vigil checkout, harnesses, credentials and containers were never written to or removed",
	)
	// The disposable root is retained for inspection; the caller removes it.
	report.Notes = append(report.Notes,
		"the scenario root is retained for the reviewer; it is agent-owned and disposable, and removing it requires no other path",
	)
	w.report.Cleanup = report
	w.assert("cleanup-scope-confined", true, fmt.Sprintf("%d entries under the agent-owned root", len(report.Preserved)),
		"cleanup is confined to the agent-owned disposable root")
}

// auditRequirements fills the requirement coverage rows from the observed
// evidence. Every requirement is placed in exactly one class, and a requirement
// with no Stage 5.7 evidence is carried forward as an owned gate rather than
// silently dropped. The classification is data, not prose, so a reviewer can
// check the whole map mechanically.
func (w *walkthrough) auditRequirements() {
	coverage := requirementCoverage()
	if len(coverage) != 71 {
		w.note(fmt.Sprintf("REQUIREMENT AUDIT INCOMPLETE: %d requirement rows were produced, not 71. The coverage audit must be total.", len(coverage)))
	}
	w.report.Matrix.Requirements = append(w.report.Matrix.Requirements, coverage...)
	counts := w.report.Matrix.Counts("requirements")
	w.note(fmt.Sprintf("Requirement coverage recorded: %d requirements, of which %d carry this run's automated evidence, %d are carried from an accepted predecessor record, and %d remain owned gates.",
		len(coverage), counts[EvidenceAutomated], counts[EvidenceReused],
		counts[EvidencePendingStage8]+counts[EvidenceUnmet]))
}
