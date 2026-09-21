package boundary

import (
	"strings"
	"testing"

	"vigil/internal/store"
)

func lifecycleEvidence() LifecycleEvidence {
	return LifecycleEvidence{
		RunID: "run", Generation: "generation", RuntimeResourceID: "container",
		WorkerState: "stopped", BoundaryState: "empty", RelayTransportState: "closed",
		SubmissionState: "delivered", ProviderState: "idle",
		InferenceRoutes: []InferenceRouteBoundary{
			{RouteID: "primary", StartupState: "reached", EvidenceDigest: store.Digest([]byte("primary native create crossed inference boundary"))},
			{RouteID: "auxiliary", StartupState: "not_reached", EvidenceDigest: store.Digest([]byte("auxiliary route remained disabled"))},
		},
		RuntimeEvidenceDigest:  store.Digest([]byte("runtime inspection")),
		ProviderEvidenceDigest: store.Digest([]byte("provider observation")), ObservedAt: store.Now(),
	}
}

func lifecycleRoutes() []string { return []string{"primary", "auxiliary"} }

func TestEndpointReleaseRequiresIndependentBoundaryAndProviderEvidence(t *testing.T) {
	evidence := lifecycleEvidence()
	routes := lifecycleRoutes()
	decision, err := EndpointRelease(evidence, routes)
	if err != nil || !decision.Release {
		t.Fatal("complete stop evidence did not release", decision, err)
	}

	for name, mutate := range map[string]func(*LifecycleEvidence){
		"worker only stopped": func(e *LifecycleEvidence) { e.BoundaryState = "unknown" },
		"relay closed":        func(e *LifecycleEvidence) { e.ProviderState = "unknown"; e.ProviderEvidenceDigest = "" },
		"provider active":     func(e *LifecycleEvidence) { e.ProviderState = "active" },
		"uncertain submit": func(e *LifecycleEvidence) {
			e.SubmissionState = "uncertain"
			e.ProviderState = "unknown"
			e.ProviderEvidenceDigest = ""
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := evidence
			mutate(&changed)
			decision, err := EndpointRelease(changed, routes)
			if err != nil || decision.Release || len(decision.Reasons) == 0 {
				t.Fatal("incomplete evidence released endpoint", decision, err)
			}
		})
	}

	beforeSubmit := evidence
	beforeSubmit.InferenceRoutes = append([]InferenceRouteBoundary(nil), evidence.InferenceRoutes...)
	beforeSubmit.SubmissionState = "proven_not_delivered"
	beforeSubmit.ProviderState = "not_started"
	for n := range beforeSubmit.InferenceRoutes {
		beforeSubmit.InferenceRoutes[n].StartupState = "not_reached"
		beforeSubmit.InferenceRoutes[n].EvidenceDigest = store.Digest([]byte("durable pre-start journal proof for " + beforeSubmit.InferenceRoutes[n].RouteID))
	}
	beforeSubmit.ProviderEvidenceDigest = ""
	decision, err = EndpointRelease(beforeSubmit, routes)
	if err != nil || !decision.Release {
		t.Fatal("proven pre-submit stop remained quarantined", decision, err)
	}

	// Native create/resume can cause auxiliary inference before the task prompt.
	// Prompt non-delivery therefore cannot establish provider not_started once
	// the inference-capable startup boundary has been crossed.
	auxiliary := evidence
	auxiliary.SubmissionState = "proven_not_delivered"
	auxiliary.ProviderState = "not_started"
	auxiliary.ProviderEvidenceDigest = ""
	if decision, err = EndpointRelease(auxiliary, routes); err == nil || decision.Release {
		t.Fatal("auxiliary inference path released from prompt non-delivery", decision, err)
	}
	auxiliary.ProviderState = "unknown"
	if decision, err = EndpointRelease(auxiliary, routes); err != nil || decision.Release {
		t.Fatal("unknown auxiliary inference did not retain quarantine", decision, err)
	}
	auxiliary.ProviderState = "idle"
	auxiliary.ProviderEvidenceDigest = store.Digest([]byte("all inference routes independently idle"))
	if decision, err = EndpointRelease(auxiliary, routes); err != nil || !decision.Release {
		t.Fatal("independently idle auxiliary inference did not release", decision, err)
	}
	missingRoute := evidence
	missingRoute.InferenceRoutes = missingRoute.InferenceRoutes[:1]
	if decision, err = EndpointRelease(missingRoute, routes); err == nil || decision.Release {
		t.Fatal("incomplete inference route set was accepted", decision, err)
	}
}

func TestEndpointAuthoritiesRejectCrossHostBypass(t *testing.T) {
	local := EndpointRoute{EndpointAuthority: "windows-llama", CapacityAuthority: "wsl-db", CapacityAuthorityScope: "single_host", HostIdentity: "wsl"}
	mac := EndpointRoute{EndpointAuthority: "windows-llama", CapacityAuthority: "mac-db", CapacityAuthorityScope: "single_host", HostIdentity: "mac"}
	if err := ValidateEndpointAuthorities([]EndpointRoute{local, mac}); err == nil || !strings.Contains(err.Error(), "cross-host") {
		t.Fatal("competing WSL/Mac authorities accepted", err)
	}
	inconsistent := local
	inconsistent.CapacityAuthorityScope = "shared_cross_host"
	if err := ValidateEndpointAuthorities([]EndpointRoute{local, inconsistent}); err == nil {
		t.Fatal("inconsistent scope for one host authority accepted")
	}
	local.CapacityAuthority = "shared-db"
	local.CapacityAuthorityScope = "shared_cross_host"
	mac.CapacityAuthority = "shared-db"
	mac.CapacityAuthorityScope = "shared_cross_host"
	if err := ValidateEndpointAuthorities([]EndpointRoute{local, mac}); err != nil {
		t.Fatal("shared cross-host authority rejected", err)
	}
}
