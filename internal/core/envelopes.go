// Package core owns application commands, revisions and readiness. No model is
// launched from these planning handlers; native observations are not authority.
package core

// EnvelopeKinds is the authoritative list of apply envelope kinds accepted by
// Engine.Apply. It exists so the Stage 6 parity check can walk the real kind
// list instead of a hardcoded copy in a test: the parity test iterates this
// slice, and Apply rejects anything outside it before reaching the switch, so
// the list cannot drift from the implementation in either direction without a
// test failing. A ValidateEnvelopeKind predicate alone would not suffice,
// because the test has to walk a list; a predicate would leave the names
// hardcoded in the test, which is the drift being eliminated.
//
// repository.enroll is dispatched through an early branch before the switch
// while every other kind is a case label inside it; both paths consult this
// list first, so the early-if shape does not create a second source of truth.
var EnvelopeKinds = []string{
	"repository.enroll",
	"project.configure",
	"profile.put",
	"plan.put",
	"plan.reorder",
	"task.criteria.revise",
	"planning.proposal.apply",
	"planning.proposal.decide",
	"input.resolve",
	"operation.request",
	"permission.grant",
	"permission.revoke",
	"operation.start",
	"retention.expire",
}

var envelopeKindSet = func() map[string]bool {
	set := make(map[string]bool, len(EnvelopeKinds))
	for _, kind := range EnvelopeKinds {
		set[kind] = true
	}
	return set
}()

// ValidEnvelopeKind reports whether kind is an accepted apply envelope kind.
// It is a convenience over EnvelopeKinds; callers that need the full list
// must walk EnvelopeKinds rather than probing names one at a time.
func ValidEnvelopeKind(kind string) bool {
	return envelopeKindSet[kind]
}
