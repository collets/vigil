package core

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"vigil/internal/artifacts"
	"vigil/internal/boundary"
	"vigil/internal/policy"
	"vigil/internal/store"
)

func coreQualification(t *testing.T, e *Engine, p policy.Profile) (boundary.QualificationInputs, boundary.QualificationRecord, boundary.EligibilityRequest) {
	t.Helper()
	var revision int
	var profileDigest string
	if err := e.DB.SQL.QueryRow(`SELECT p.revision,c.digest FROM profiles p JOIN config_snapshots c ON c.id=p.config_id WHERE p.id=? ORDER BY p.revision DESC LIMIT 1`, p.ID).Scan(&revision, &profileDigest); err != nil {
		t.Fatal(err)
	}
	digest := func(value string) string { return store.Digest([]byte(value)) }
	inputs := boundary.QualificationInputs{
		Harness: p.Harness, HarnessVersion: p.Version, HarnessSourceDigest: digest("source"),
		ExecutableDigest: digest("executable"), ImageDigest: digest("image"), GuardianDigest: digest("guardian"),
		WorkerDigest: digest("worker"), RelayDigest: digest("relay"), ProfileID: p.ID,
		ProfileRevision: revision, ProfileDigest: profileDigest, ProfileConfigDigest: digest("native-config"),
		ToolConfigDigest: digest("tools"), InstructionDigest: digest("instructions"), MountPlanDigest: digest("mounts"),
		RuntimeDigest: digest("runtime"), HostOS: "linux", HostArch: "amd64", WorkerOS: "linux",
		WorkerArch: "amd64", RuntimeName: "docker", RuntimeVersion: "29.8.0", Provider: p.Provider,
		Model: p.Model, ReasoningEffort: "not_applicable", CredentialMode: "scoped_api_relay",
		RelayTopology: "host_socket_relay", EndpointAuthority: p.EndpointID,
		CapacityAuthority: "wsl-coordinator", CapacityAuthorityScope: "single_host",
	}
	now := store.Now()
	evidence := func(class, name string) boundary.QualificationEvidence {
		return boundary.QualificationEvidence{Class: class, Name: name, ArtifactID: "unpublished-" + name, Digest: digest(class + name), ObservedAt: now, Result: "pass"}
	}
	record := boundary.QualificationRecord{
		SchemaVersion: 1, Status: "supported", Inputs: inputs,
		Claims: []string{boundary.ClaimBoundaryExecution}, Roles: []string{"implementation"},
		Layouts: []string{"ordinary_single_repository"}, Evidence: []boundary.QualificationEvidence{
			evidence("metadata", "identity"), evidence("synthetic", "containment"), evidence("live", "bounded_task"),
		}, ObservedAt: now,
	}
	request := boundary.EligibilityRequest{Inputs: inputs, Role: "implementation", Layout: "ordinary_single_repository", RequiredClaims: []string{boundary.ClaimBoundaryExecution}}
	return inputs, record, request
}

func retainCoreQualificationEvidence(t *testing.T, e *Engine, record *boundary.QualificationRecord) {
	t.Helper()
	repository, err := artifacts.New(e.DB)
	if err != nil {
		t.Fatal(err)
	}
	for n := range record.Evidence {
		evidence := &record.Evidence[n]
		artifact, err := repository.Put(context.Background(), "core-qualification-"+store.ID(), boundary.QualificationEvidenceArtifactKind, "durable", strings.NewReader(fmt.Sprintf("%s/%s/%s", evidence.Class, evidence.Name, evidence.Result)))
		if err != nil {
			t.Fatal(err)
		}
		evidence.ArtifactID = artifact.ID
		evidence.Digest = artifact.Digest
	}
}

func TestCoreExecutionEligibilityRequiresTrustedExactCurrentProfile(t *testing.T) {
	_, e, _ := setup(t)
	p := profile()
	apply(t, e, "profile.put", p)
	inputs, record, request := coreQualification(t, e, p)

	result, err := e.ExecutionEligibility(context.Background(), request)
	if err != nil || result.Status != "unverified" {
		t.Fatal("profile declaration manufactured qualification", result, err)
	}
	retainCoreQualificationEvidence(t, e, &record)
	if _, err = boundary.RecordTrustedQualification(context.Background(), e.DB, record); err != nil {
		t.Fatal(err)
	}
	result, err = e.ExecutionEligibility(context.Background(), request)
	if err != nil || result.Status != "supported" {
		t.Fatal("trusted exact qualification not consumed", result, err)
	}

	// A user-authored capability reference and a new profile revision cannot
	// revive evidence bound to the previous exact revision.
	p.Capabilities = []policy.Capability{{Name: "boundary", Support: "available", Guarantee: "native_enforced", Platform: "linux/amd64", HarnessVersion: p.Version, EvidenceID: result.EvidenceID}}
	apply(t, e, "profile.put", p)
	request.Inputs = inputs
	result, err = e.ExecutionEligibility(context.Background(), request)
	if err != nil || result.Status != "unsupported" {
		t.Fatal("stale profile revision retained eligibility", result, err)
	}
	if len(result.Reasons) == 0 {
		t.Fatal("stale denial lacks reason")
	}
}
