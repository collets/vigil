package boundary

import (
	"errors"
	"fmt"
	"sort"

	"vigil/internal/store"
)

// LifecycleEvidence keeps observations independent. In particular, worker or
// relay termination never derives a provider-idle observation.
type InferenceRouteBoundary struct {
	RouteID        string `json:"route_id"`
	StartupState   string `json:"startup_state"` // not_reached, reached, unknown
	EvidenceDigest string `json:"evidence_digest,omitempty"`
}

type LifecycleEvidence struct {
	RunID                  string                   `json:"run_id"`
	Generation             string                   `json:"generation"`
	RuntimeResourceID      string                   `json:"runtime_resource_id"`
	WorkerState            string                   `json:"worker_state"`          // running, stopped, unknown
	BoundaryState          string                   `json:"boundary_state"`        // nonempty, empty, unknown
	RelayTransportState    string                   `json:"relay_transport_state"` // open, closed, unknown
	SubmissionState        string                   `json:"submission_state"`      // proven_not_delivered, delivered, uncertain
	ProviderState          string                   `json:"provider_state"`        // not_started, active, idle, unknown
	InferenceRoutes        []InferenceRouteBoundary `json:"inference_routes"`
	RuntimeEvidenceDigest  string                   `json:"runtime_evidence_digest"`
	ProviderEvidenceDigest string                   `json:"provider_evidence_digest,omitempty"`
	ObservedAt             int64                    `json:"observed_at"`
}

type ReleaseDecision struct {
	Release bool     `json:"release"`
	Reasons []string `json:"reasons"`
}

func (e LifecycleEvidence) Validate(expectedRoutes []string) error {
	for name, value := range map[string]string{"run_id": e.RunID, "generation": e.Generation, "runtime_resource_id": e.RuntimeResourceID} {
		if err := bounded(value, name); err != nil {
			return err
		}
	}
	if !contains([]string{"running", "stopped", "unknown"}, e.WorkerState) ||
		!contains([]string{"nonempty", "empty", "unknown"}, e.BoundaryState) ||
		!contains([]string{"open", "closed", "unknown"}, e.RelayTransportState) ||
		!contains([]string{"proven_not_delivered", "delivered", "uncertain"}, e.SubmissionState) ||
		!contains([]string{"not_started", "active", "idle", "unknown"}, e.ProviderState) {
		return errors.New("invalid lifecycle observation state")
	}
	if !validDigest(e.RuntimeEvidenceDigest) || e.ObservedAt <= 0 {
		return errors.New("runtime lifecycle evidence is incomplete")
	}
	if e.ProviderState == "idle" || e.ProviderState == "active" {
		if !validDigest(e.ProviderEvidenceDigest) {
			return errors.New("provider state requires independent provider evidence")
		}
	}
	if len(expectedRoutes) == 0 || len(expectedRoutes) > 32 || len(e.InferenceRoutes) != len(expectedRoutes) {
		return errors.New("inference route observations do not match the expected route set")
	}
	expected := map[string]bool{}
	for _, route := range expectedRoutes {
		if err := bounded(route, "expected inference route"); err != nil || expected[route] {
			return errors.New("invalid or duplicate expected inference route")
		}
		expected[route] = true
	}
	observed := map[string]bool{}
	for _, route := range e.InferenceRoutes {
		if err := bounded(route.RouteID, "inference route"); err != nil || !expected[route.RouteID] || observed[route.RouteID] {
			return errors.New("unexpected or duplicate inference route observation")
		}
		if !contains([]string{"not_reached", "reached", "unknown"}, route.StartupState) {
			return errors.New("invalid inference route startup state")
		}
		if route.StartupState == "unknown" {
			if route.EvidenceDigest != "" {
				return errors.New("unknown inference route state cannot claim evidence")
			}
		} else if !validDigest(route.EvidenceDigest) {
			return errors.New("known inference route state requires independent evidence")
		}
		observed[route.RouteID] = true
	}
	if e.ProviderState == "not_started" {
		if e.SubmissionState != "proven_not_delivered" {
			return errors.New("provider not_started requires proof that the inference-capable startup boundary was never reached")
		}
		for _, route := range e.InferenceRoutes {
			if route.StartupState != "not_reached" {
				return errors.New("provider not_started requires every expected inference route to be proven not reached")
			}
		}
	}
	return nil
}

// EndpointRelease evaluates only evidence for a single run generation. Unknown
// or uncertain inference always retains capacity quarantine.
func EndpointRelease(evidence LifecycleEvidence, expectedRoutes []string) (ReleaseDecision, error) {
	decision := ReleaseDecision{Reasons: []string{}}
	if err := evidence.Validate(expectedRoutes); err != nil {
		return decision, err
	}
	if evidence.WorkerState != "stopped" {
		decision.Reasons = append(decision.Reasons, "worker stop has not been observed")
	}
	if evidence.BoundaryState != "empty" {
		decision.Reasons = append(decision.Reasons, "execution boundary is not proven empty")
	}
	if evidence.SubmissionState == "uncertain" {
		decision.Reasons = append(decision.Reasons, "native submission delivery is uncertain")
	}
	if evidence.ProviderState != "idle" && evidence.ProviderState != "not_started" {
		decision.Reasons = append(decision.Reasons, "provider idle has not been independently observed")
	}
	decision.Release = len(decision.Reasons) == 0
	sort.Strings(decision.Reasons)
	return decision, nil
}

type EndpointRoute struct {
	EndpointAuthority      string
	CapacityAuthority      string
	CapacityAuthorityScope string
	HostIdentity           string
}

// ValidateEndpointAuthorities rejects WSL and Mac acting as independent
// schedulers for the same physical llama endpoint. A shared_cross_host scope is
// admissible only when every route names the same capacity authority.
func ValidateEndpointAuthorities(routes []EndpointRoute) error {
	byEndpoint := map[string]EndpointRoute{}
	for _, route := range routes {
		for name, value := range map[string]string{"endpoint authority": route.EndpointAuthority, "capacity authority": route.CapacityAuthority, "capacity authority scope": route.CapacityAuthorityScope, "host identity": route.HostIdentity} {
			if err := bounded(value, name); err != nil {
				return err
			}
		}
		if route.CapacityAuthorityScope != "single_host" && route.CapacityAuthorityScope != "shared_cross_host" {
			return errors.New("route lacks a supported capacity authority scope")
		}
		prior, exists := byEndpoint[route.EndpointAuthority]
		if !exists {
			byEndpoint[route.EndpointAuthority] = route
			continue
		}
		if prior.HostIdentity == route.HostIdentity {
			if prior.CapacityAuthority != route.CapacityAuthority || prior.CapacityAuthorityScope != route.CapacityAuthorityScope {
				return fmt.Errorf("endpoint %s has competing capacity authorities on one host", route.EndpointAuthority)
			}
			continue
		}
		if prior.CapacityAuthorityScope != "shared_cross_host" || route.CapacityAuthorityScope != "shared_cross_host" || prior.CapacityAuthority != route.CapacityAuthority {
			return fmt.Errorf("endpoint %s has uncoordinated cross-host capacity authorities", route.EndpointAuthority)
		}
	}
	return nil
}

// LifecycleDigest produces the evidence binding Stage 5.2 stores alongside a
// run generation. It does not make the observation trustworthy by itself.
func LifecycleDigest(evidence LifecycleEvidence, expectedRoutes []string) (string, error) {
	if err := evidence.Validate(expectedRoutes); err != nil {
		return "", err
	}
	raw, err := canonical(evidence)
	if err != nil {
		return "", err
	}
	return store.Digest(raw), nil
}
