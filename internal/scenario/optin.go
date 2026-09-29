// Package scenario implements Stage 5.7's autonomous end-to-end qualification
// walkthrough: a disposable repository fixture, a scenario manifest that bounds
// the run before it starts, a driver that exercises the *production* application
// path as real `vigil` subprocesses, an evidence matrix that maps milestone
// steps and requirements to observed or pending evidence, and an honest report.
//
// Nothing in this package is a product path. It creates no delivery authority,
// never enables production dispatch, and never contacts a real remote or a
// metered provider. Every human-gated step is recorded as a Stage 8 pending item
// with its exact blocker rather than simulated.
package scenario

import (
	"errors"
	"os"
	"sort"
	"strings"
)

// Capability names one of the four independent opt-ins Stage 5.7 checkpoint A
// requires. They are deliberately separate: enabling one never implies another,
// and the default is that all four are off.
type Capability string

const (
	// CapabilityDocker permits container-boundary probes against a local Docker
	// or OrbStack engine. The default suite must not require it.
	CapabilityDocker Capability = "docker"
	// CapabilityLocalInference permits bounded live turns through the existing
	// loopback llama route and a freshly prepared native Hermes home.
	CapabilityLocalInference Capability = "local_inference"
	// CapabilityCodexLive would permit contained live Codex subscription use.
	// It is refused: the contained relay supports only scoped OpenAI-compatible
	// API credentials, so no supported ChatGPT-auth route exists. See
	// docs/process/pending-decisions.md.
	CapabilityCodexLive Capability = "codex_live"
	// CapabilityRemoteDelivery would permit a real push and a real hosted draft
	// request. It is refused: no destination is authorized. It is Stage 8's.
	CapabilityRemoteDelivery Capability = "remote_delivery"
)

// Capabilities lists every capability in a stable order.
var Capabilities = []Capability{
	CapabilityDocker,
	CapabilityLocalInference,
	CapabilityCodexLive,
	CapabilityRemoteDelivery,
}

// Recorded blockers for the two capabilities that no authorized configuration
// can currently satisfy. They are recorded here so the refusal is a durable,
// reviewable fact rather than a run-time accident.
const (
	CodexLiveBlocker = "no supported contained ChatGPT-auth route: the boundary relay accepts only " +
		"scoped OpenAI-compatible API credentials, and using the Codex account home or extracting " +
		"login credentials is not authorized; see docs/process/pending-decisions.md"

	RemoteDeliveryBlocker = "no authorized real delivery destination: a real push, a real hosted draft " +
		"request and the delivery-cancel/delivery-reconcile/retention-expire human commands are Stage 8 " +
		"inputs that require per-operation user authorization; see docs/process/pending-decisions.md"
)

// CapabilityRecord is the manifest entry for one capability: what was asked
// for, what was granted, and — when refused — the exact recorded blocker.
type CapabilityRecord struct {
	Name       Capability `json:"name"`
	Requested  bool       `json:"requested"`
	Enabled    bool       `json:"enabled"`
	Refusal    string     `json:"refusal,omitempty"`
	Evidence   string     `json:"evidence,omitempty"`
	OwnerStage string     `json:"owner_stage,omitempty"`
}

// OptIns is the operator's request, before environment validation.
type OptIns struct {
	Docker         bool
	LocalInference bool
	CodexLive      bool
	RemoteDelivery bool
}

// Refusable reports the capabilities that no operator request can enable in this
// build, and the recorded reason for each.
func Refusable() map[Capability]string {
	return map[Capability]string{
		CapabilityCodexLive:      CodexLiveBlocker,
		CapabilityRemoteDelivery: RemoteDeliveryBlocker,
	}
}

// LocalRoute describes the loopback inference route without ever carrying a
// credential value: only presence and route identity are ever recorded.
type LocalRoute struct {
	BaseURL    string
	APIKeySet  bool
	Model      string
	Probed     bool
	ProbeError string
}

// ProbeLocalRoute checks the presence and identity of the inherited loopback
// route without reading, printing or persisting the key value. It reports the
// advertised model so the manifest can record the route it actually used.
func ProbeLocalRoute() LocalRoute {
	return probeLocalRoute(loopbackCredential, os.Getenv("OPENAI_BASE_URL"))
}

func probeLocalRoute(credential func() string, baseURL string) LocalRoute {
	route := LocalRoute{BaseURL: strings.TrimRight(baseURL, "/"), APIKeySet: credential() != ""}
	if !route.APIKeySet || !isLoopbackBaseURL(route.BaseURL) {
		return route
	}
	model, err := probeModels(route.BaseURL)
	if err != nil {
		route.ProbeError = err.Error()
		return route
	}
	route.Probed = true
	route.Model = model
	return route
}

func isLoopbackBaseURL(base string) bool {
	if base == "" {
		return false
	}
	rest := base
	switch {
	case strings.HasPrefix(rest, "http://"):
		rest = strings.TrimPrefix(rest, "http://")
	case strings.HasPrefix(rest, "https://"):
		rest = strings.TrimPrefix(rest, "https://")
	default:
		return false
	}
	host := rest
	if index := strings.IndexAny(rest, "/?#"); index >= 0 {
		host = rest[:index]
	}
	if index := strings.LastIndex(host, ":"); index >= 0 && !strings.Contains(host[index:], "]") {
		host = host[:index]
	}
	host = strings.Trim(host, "[]")
	switch host {
	case "127.0.0.1", "::1", "localhost":
		return true
	}
	return strings.HasPrefix(host, "127.")
}

// Resolve turns an operator request into one record per capability. A refused
// capability is never an error: it is a recorded, owned Stage 8 gate. A
// requested-but-unavailable capability that the scenario actually needs is an
// error, so a missing local service is never silently reported as a pass.
func Resolve(request OptIns, route LocalRoute) []CapabilityRecord {
	refusable := Refusable()
	records := make([]CapabilityRecord, 0, len(Capabilities))
	for _, capability := range Capabilities {
		record := CapabilityRecord{Name: capability}
		switch capability {
		case CapabilityDocker:
			record.Requested = request.Docker
			record.Enabled = request.Docker
		case CapabilityLocalInference:
			record.Requested = request.LocalInference
			record.Enabled = request.LocalInference && route.Probed
			switch {
			case !request.LocalInference:
			case !route.APIKeySet:
				record.Refusal = "no loopback inference credential is present in the environment"
			case route.BaseURL == "":
				record.Refusal = "OPENAI_BASE_URL does not name the prepared loopback endpoint"
			case !route.Probed:
				record.Refusal = "the loopback route did not answer a metadata probe: " + route.ProbeError
			default:
				record.Evidence = "loopback route " + route.BaseURL + " advertises model " + route.Model
			}
		case CapabilityCodexLive:
			record.Requested = request.CodexLive
			record.Refusal = CodexLiveBlocker
			record.OwnerStage = "stage-8"
		case CapabilityRemoteDelivery:
			record.Requested = request.RemoteDelivery
			record.Refusal = RemoteDeliveryBlocker
			record.OwnerStage = "stage-8"
		}
		if _, refused := refusable[capability]; refused {
			record.Enabled = false
			if record.OwnerStage == "" {
				record.OwnerStage = "stage-8"
			}
		}
		records = append(records, record)
	}
	return records
}

// Enabled reports whether a capability was granted.
func Enabled(records []CapabilityRecord, capability Capability) bool {
	for _, record := range records {
		if record.Name == capability {
			return record.Enabled
		}
	}
	return false
}

// Refusal returns the recorded refusal for a capability.
func Refusal(records []CapabilityRecord, capability Capability) (string, bool) {
	for _, record := range records {
		if record.Name == capability {
			return record.Refusal, record.Refusal != ""
		}
	}
	return "", false
}

// Pending returns the capabilities that were requested but refused, which the
// report must surface rather than bury.
func Pending(records []CapabilityRecord) []string {
	pending := []string{}
	for _, record := range records {
		if record.Requested && !record.Enabled {
			pending = append(pending, string(record.Name)+": "+record.Refusal)
		}
	}
	sort.Strings(pending)
	return pending
}

// RequireLocalInference fails when a phase that genuinely needs a live local
// turn was requested but the route is unavailable, so a missing service is
// reported as a failure instead of a silent skip.
func RequireLocalInference(records []CapabilityRecord) error {
	if Enabled(records, CapabilityLocalInference) {
		return nil
	}
	refusal, ok := Refusal(records, CapabilityLocalInference)
	if !ok {
		refusal = "the local inference opt-in was not requested"
	}
	if refusal == "" {
		refusal = "the local inference opt-in is unavailable"
	}
	return errors.New("live local inference was requested but is unavailable: " + refusal)
}

// probeModels performs a bounded metadata-only read against the loopback route
// and returns the single advertised model identifier. It never logs the
// credential, sends no prompt, and starts no inference.
func probeModels(base string) (string, error) {
	body, err := loopbackGet(base + "/models")
	if err != nil {
		return "", err
	}
	return parseModelList(body)
}
