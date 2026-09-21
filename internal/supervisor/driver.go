package supervisor

import (
	"context"
	"encoding/json"

	"vigil/internal/boundary"
)

type DriverEvent struct {
	Sequence int64           `json:"sequence"`
	Kind     string          `json:"kind"`
	At       int64           `json:"occurred_at"`
	Payload  json.RawMessage `json:"payload"`
}

type Usage struct {
	InputTokens  *int64 `json:"input_tokens"`
	OutputTokens *int64 `json:"output_tokens"`
	CachedTokens *int64 `json:"cached_tokens"`
	CostMicros   *int64 `json:"cost_microunits"`
	Currency     string `json:"currency,omitempty"`
	Provenance   string `json:"provenance,omitempty"`
}

type Observation struct {
	Exists          bool                     `json:"exists"`
	Started         bool                     `json:"started"`
	Attached        bool                     `json:"attached"`
	NativeSessionID string                   `json:"native_session_id,omitempty"`
	NativeTurnID    string                   `json:"native_turn_id,omitempty"`
	SubmissionState string                   `json:"submission_state"`
	Terminal        bool                     `json:"terminal"`
	Outcome         string                   `json:"outcome,omitempty"`
	Result          json.RawMessage          `json:"result,omitempty"`
	WriterState     string                   `json:"writer_state"`
	Mounts          []boundary.ObservedMount `json:"mounts,omitempty"`
	Events          []DriverEvent            `json:"events,omitempty"`
	Usage           Usage                    `json:"usage"`
}

type Driver interface {
	Inspect(context.Context, PreparedRun) (Observation, error)
	Create(context.Context, PreparedRun) error
	Start(context.Context, PreparedRun) error
	Attach(context.Context, PreparedRun) error
	CreateNative(context.Context, PreparedRun) (string, error)
	Submit(context.Context, PreparedRun, string) (submissionState string, nativeTurnID string, err error)
	Await(context.Context, PreparedRun) (Observation, error)
	RenewLease(context.Context, PreparedRun) error
	Stop(context.Context, PreparedRun) (Observation, error)
}

// ProductionBindingProvider is required for every non-synthetic driver. It
// reports the security-relevant inputs and inference routes the driver will
// actually use, rather than accepting the caller's qualification request as a
// description of its own configuration.
type ProductionBindingProvider interface {
	ProductionBinding(context.Context, PreparedRun) (boundary.QualificationInputs, []string, error)
}

type Result struct {
	SchemaVersion int      `json:"schema_version"`
	Status        string   `json:"status"`
	Summary       string   `json:"summary"`
	ChangedPaths  []string `json:"changed_paths"`
}
