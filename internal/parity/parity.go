// Package parity holds the machine-readable Stage 6 parity register and the
// mechanical check that stops the terminal interface and the product surface
// from diverging again.
//
// The register classifies every reachable product capability as E
// (expressible), C (expressible with a documented compromise) or X
// (excluded, with reason), exactly as docs/research/stage-6/results.md §4
// does. The Go table is the form the check runs against; the Markdown
// register is the form humans read. Their reason texts are pinned against
// each other by test, so neither copy can drift silently.
//
// The guarantee is procedural and review-enforced, not mechanical magic: the
// test catches drift between the register and the closed exclusion list in
// docs/core/requirements.md (an X nobody sanctioned, or a sanctioned entry
// nobody classified). It does not prevent an agent from adding an entry to
// requirements.md and classifying the matching row X in the same commit —
// that satisfies both clauses and the test passes. Unlike the installed
// migrations, requirements.md is a live document, so no gate can make it
// immutable without changing the repository's own rules, which is outside
// Stage 6's authority. Adding an exclusion entry is a user scope decision,
// the two copies are pinned against each other, and every change to either
// copy is visible in an independent review's diff. Do not read a green test
// as more than it is.
package parity

// Entry is one register row: a command path or envelope-kind path, its
// classification, the sub-stage that owns it, and — for C and X — the reason
// text, which must read the same as the Markdown register's.
type Entry struct {
	Path   string
	Class  string
	Owner  string
	Reason string
	Status string
}

const (
	ClassExpressible  = "E"
	ClassCompromise   = "C"
	ClassExcluded     = "X"
	StatusDone        = "done"
	StatusPlanned     = "planned"
	OwnerDecided      = "—"
)

// LandedThrough names the latest sub-stage whose owned rows must read done.
// Each later sub-stage bumps this constant and marks its own rows done; the
// test fails when an owned row of a landed sub-stage is still planned. The
// table, not the prose, is the source of truth.
const LandedThrough = "6.3"

// Entries is the full 85-row register: 62 project commands, 3 resources, 6
// root commands and 14 apply envelope kinds.
var Entries = []Entry{
	// 4.1 Root commands.
	{Path: "dashboard", Class: "E", Owner: "6.2", Reason: "—", Status: "done"},
	{Path: "doctor", Class: "E", Owner: "6.8", Reason: "—", Status: "planned"},
	{Path: "hello", Class: "X", Owner: "—", Reason: "Scope. A SQLite connectivity smoke test with no project state; it is not a product capability and has nothing to drive.", Status: "done"},
	{Path: "spike", Class: "X", Owner: "—", Reason: "Scope. A development-only Stage 1–3 experiment runner outside the persisted core. Stage 6's scope forbids adding or extending product capability.", Status: "done"},
	{Path: "completion", Class: "X", Owner: "—", Reason: "Mechanism. Cobra shell-completion script generation; shell scaffolding with no Vigil state, and the interface has its own help surface.", Status: "done"},
	{Path: "help", Class: "X", Owner: "—", Reason: "Mechanism. Documents the CLI surface, which the interface is explicitly replacing as primary.", Status: "done"},
	// 4.2 Resources.
	{Path: "resources endpoint", Class: "E", Owner: "6.8", Reason: "—", Status: "planned"},
	{Path: "resources status", Class: "E", Owner: "6.8", Reason: "—", Status: "planned"},
	{Path: "resources reconcile", Class: "E", Owner: "6.8", Reason: "—", Status: "planned"},
	// 4.3 Project commands, part 1 — definitions and lifecycle.
	{Path: "project init", Class: "C", Owner: "6.8", Reason: "Situation (a): the root path is typed and validated, not browsed", Status: "planned"},
	{Path: "project list", Class: "E", Owner: "6.8", Reason: "—", Status: "planned"},
	{Path: "project status", Class: "E", Owner: "6.3", Reason: "—", Status: "done"},
	{Path: "project inbox", Class: "E", Owner: "6.4", Reason: "—", Status: "planned"},
	{Path: "project events", Class: "E", Owner: "6.3", Reason: "—", Status: "done"},
	{Path: "project apply", Class: "C", Owner: "6.8", Reason: "Situation (a): the real command is `apply PROJECT_ID --file COMMAND.json`, so the operator supplies a filesystem path. In exchange the interface builds one typed form per envelope kind and never accepts a pasted envelope, so an unknown field or duplicate key cannot be submitted", Status: "planned"},
	{Path: "project discover", Class: "E", Owner: "6.8", Reason: "—", Status: "planned"},
	{Path: "project repository", Class: "E", Owner: "6.8", Reason: "—", Status: "planned"},
	{Path: "project prepare-repository", Class: "E", Owner: "6.8", Reason: "—", Status: "planned"},
	{Path: "project spec-import", Class: "C", Owner: "6.8", Reason: "Situation (a): the owned `.md` path is typed inside the project root, not browsed", Status: "planned"},
	{Path: "project spec-show", Class: "E", Owner: "6.8", Reason: "—", Status: "planned"},
	{Path: "project proposal-create", Class: "C", Owner: "6.8", Reason: "Situation (b): the nested plan/task/criteria/dependency definition is composed through a guided editor and the exact envelope is displayed before creation", Status: "planned"},
	{Path: "project proposal-show", Class: "E", Owner: "6.8", Reason: "—", Status: "planned"},
	{Path: "project proposal-apply", Class: "E", Owner: "6.4", Reason: "—", Status: "planned"},
	{Path: "project proposal-decide", Class: "E", Owner: "6.4", Reason: "—", Status: "planned"},
	{Path: "project input-resolve", Class: "E", Owner: "—", Reason: "—", Status: "done"},
	{Path: "project queue", Class: "E", Owner: "6.3", Reason: "—", Status: "done"},
	{Path: "project queue-list", Class: "E", Owner: "6.3", Reason: "—", Status: "done"},
	{Path: "project advance", Class: "E", Owner: "—", Reason: "—", Status: "done"},
	{Path: "project pause", Class: "E", Owner: "—", Reason: "—", Status: "done"},
	{Path: "project continue", Class: "E", Owner: "—", Reason: "—", Status: "done"},
	// 4.4 Project commands, part 2 — execution, recovery, checkpoints.
	{Path: "project execution-prepare", Class: "E", Owner: "6.6", Reason: "—", Status: "planned"},
	{Path: "project execution-start", Class: "E", Owner: "6.6", Reason: "—", Status: "planned"},
	{Path: "project execution-inspect", Class: "E", Owner: "6.6", Reason: "—", Status: "planned"},
	{Path: "project execution-reconcile", Class: "E", Owner: "6.6", Reason: "—", Status: "planned"},
	{Path: "project execution-stop", Class: "E", Owner: "6.6", Reason: "—", Status: "planned"},
	{Path: "project execution-recovery-choose", Class: "E", Owner: "6.4", Reason: "—", Status: "planned"},
	{Path: "project execution-resume-prepare", Class: "E", Owner: "6.6", Reason: "—", Status: "planned"},
	{Path: "project execution-followup-prepare", Class: "E", Owner: "6.6", Reason: "—", Status: "planned"},
	{Path: "project checkpoint-save", Class: "E", Owner: "6.6", Reason: "—", Status: "planned"},
	{Path: "project checkpoint-clear", Class: "E", Owner: "6.6", Reason: "—", Status: "planned"},
	{Path: "project checkpoint-restore", Class: "E", Owner: "6.6", Reason: "—", Status: "planned"},
	{Path: "project reservation", Class: "E", Owner: "6.5", Reason: "—", Status: "planned"},
	{Path: "project artifact", Class: "C", Owner: "6.5", Reason: "Situation (a): the file path is typed, not browsed", Status: "planned"},
	{Path: "project artifacts", Class: "E", Owner: "6.5", Reason: "—", Status: "planned"},
	// 4.5 Project commands, part 3 — quality and acceptance.
	{Path: "project quality-check", Class: "E", Owner: "6.5", Reason: "—", Status: "planned"},
	{Path: "project quality-review", Class: "E", Owner: "6.5", Reason: "—", Status: "planned"},
	{Path: "project quality-manual", Class: "E", Owner: "6.5", Reason: "—", Status: "planned"},
	{Path: "project quality-decision", Class: "E", Owner: "6.5", Reason: "—", Status: "planned"},
	{Path: "project quality-accept", Class: "E", Owner: "6.5", Reason: "—", Status: "planned"},
	// 4.6 Project commands, part 4 — delivery, finalization, retention.
	{Path: "project commit-prepare", Class: "E", Owner: "6.7", Reason: "—", Status: "planned"},
	{Path: "project commit-execute", Class: "E", Owner: "6.7", Reason: "—", Status: "planned"},
	{Path: "project push-prepare", Class: "E", Owner: "6.7", Reason: "—", Status: "planned"},
	{Path: "project push-execute", Class: "E", Owner: "6.7", Reason: "—", Status: "planned"},
	{Path: "project draft-prepare", Class: "E", Owner: "6.7", Reason: "—", Status: "planned"},
	{Path: "project draft-execute", Class: "E", Owner: "6.7", Reason: "—", Status: "planned"},
	{Path: "project delivery-status", Class: "E", Owner: "6.7", Reason: "—", Status: "planned"},
	{Path: "project delivery-cancel", Class: "E", Owner: "6.7", Reason: "—", Status: "planned"},
	{Path: "project delivery-reconcile", Class: "E", Owner: "6.7", Reason: "—", Status: "planned"},
	{Path: "project delivery-close-unobserved", Class: "E", Owner: "6.7", Reason: "—", Status: "planned"},
	{Path: "project archive-build", Class: "E", Owner: "6.7", Reason: "—", Status: "planned"},
	{Path: "project archive-show", Class: "E", Owner: "6.7", Reason: "—", Status: "planned"},
	{Path: "project archive-export", Class: "C", Owner: "6.7", Reason: "Situation (a): the destination directory is typed and its non-symbolic-link parent is checked and stated in the confirmation; there is no directory browser", Status: "planned"},
	{Path: "project archive-narrative", Class: "E", Owner: "6.7", Reason: "—", Status: "planned"},
	{Path: "project finalization-run", Class: "C", Owner: "6.7", Reason: "Situations (a) and (c): the prepared manifest path and the optional private key path are typed; the interface cannot browse the operator's filesystem or verify a prepared credential-free home", Status: "planned"},
	{Path: "project finalization-quarantine", Class: "E", Owner: "6.7", Reason: "—", Status: "planned"},
	{Path: "project retention-inspect", Class: "E", Owner: "6.7", Reason: "—", Status: "planned"},
	{Path: "project retention-expire", Class: "E", Owner: "6.7", Reason: "—", Status: "planned"},
	// 4.7 Project commands, part 5 — live routes.
	{Path: "project planning-run", Class: "C", Owner: "6.7", Reason: "Situations (a) and (c), for the same reason as `finalization-run`", Status: "planned"},
	{Path: "project planning-reconcile", Class: "E", Owner: "6.7", Reason: "—", Status: "planned"},
	{Path: "project tool-qualify", Class: "C", Owner: "6.7", Reason: "Situations (a) and (c), as `finalization-run`; this is a qualification action, not a day-to-day capability", Status: "planned"},
	{Path: "project tool-server", Class: "X", Owner: "—", Reason: "Mechanism. A newline-delimited JSON-RPC stdio MCP server for a native harness subprocess. It is spawned by the application with a pre-opened session, is not an operator action and grants no operator authority. No human surface can express it, and giving one would widen authority.", Status: "done"},
	// 4.8 apply envelope kinds.
	{Path: "kind repository.enroll", Class: "C", Owner: "6.8", Reason: "Situation (b): the `nested_boundaries` list and the `dirty_choice` included-path set are composed through a guided editor from the `discover` output", Status: "planned"},
	{Path: "kind project.configure", Class: "C", Owner: "6.8", Reason: "Situation (b): the policy document is nested, carrying the `check_definitions` argv arrays and their environment and output lists; it is composed through a guided editor and the effective policy is displayed before it is committed", Status: "planned"},
	{Path: "kind profile.put", Class: "E", Owner: "6.8", Reason: "—", Status: "planned"},
	{Path: "kind plan.put", Class: "C", Owner: "6.8", Reason: "Situation (b): the plan/task/criteria/dependency graph is recursive; it is composed through a guided editor, dependency validation runs live, and the exact envelope is displayed before commit", Status: "planned"},
	{Path: "kind plan.reorder", Class: "E", Owner: "6.8", Reason: "—", Status: "planned"},
	{Path: "kind task.criteria.revise", Class: "E", Owner: "6.8", Reason: "—", Status: "planned"},
	{Path: "kind planning.proposal.apply", Class: "E", Owner: "6.4", Reason: "—", Status: "planned"},
	{Path: "kind planning.proposal.decide", Class: "E", Owner: "6.4", Reason: "—", Status: "planned"},
	{Path: "kind input.resolve", Class: "E", Owner: "—", Reason: "—", Status: "done"},
	{Path: "kind operation.request", Class: "E", Owner: "6.4", Reason: "—", Status: "planned"},
	{Path: "kind permission.grant", Class: "E", Owner: "6.4", Reason: "—", Status: "planned"},
	{Path: "kind permission.revoke", Class: "E", Owner: "6.4", Reason: "—", Status: "planned"},
	{Path: "kind operation.start", Class: "X", Owner: "—", Reason: "Mechanism. `Apply` accepts it only for the `Core` authority; it is issued by the trusted coordinator at effect start and is not a human decision. It is not reachable from the CLI either.", Status: "done"},
	{Path: "kind retention.expire", Class: "E", Owner: "6.7", Reason: "—", Status: "planned"},
}
