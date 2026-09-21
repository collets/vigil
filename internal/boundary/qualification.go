package boundary

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"vigil/internal/artifacts"
	"vigil/internal/store"
)

// Qualification claims are deliberately separate. Stage 5.1 can establish a
// contained boundary without manufacturing Stage 5.2's persisted launch and
// uncertain-submission guarantees.
const (
	ClaimBoundaryExecution = "boundary_execution"
	ClaimProviderIdle      = "provider_idle"
	ClaimProductionLaunch  = "production_launch_recovery"
)

var validClaims = map[string]bool{
	ClaimBoundaryExecution: true,
	ClaimProviderIdle:      true,
	ClaimProductionLaunch:  true,
}

const QualificationEvidenceArtifactKind = "qualification-evidence"

// QualificationInputs identifies every security-relevant component of an
// execution combination. Any field change creates a different input digest and
// therefore invalidates earlier evidence.
type QualificationInputs struct {
	Harness                string `json:"harness"`
	HarnessVersion         string `json:"harness_version"`
	HarnessSourceDigest    string `json:"harness_source_digest"`
	ExecutableDigest       string `json:"executable_digest"`
	ImageDigest            string `json:"image_digest"`
	GuardianDigest         string `json:"guardian_digest"`
	WorkerDigest           string `json:"worker_digest"`
	RelayDigest            string `json:"relay_digest"`
	ProfileID              string `json:"profile_id"`
	ProfileRevision        int    `json:"profile_revision"`
	ProfileDigest          string `json:"profile_digest"`
	ProfileConfigDigest    string `json:"profile_config_digest"`
	ToolConfigDigest       string `json:"tool_config_digest"`
	InstructionDigest      string `json:"instruction_digest"`
	MountPlanDigest        string `json:"mount_plan_digest"`
	RuntimeDigest          string `json:"runtime_digest"`
	HostOS                 string `json:"host_os"`
	HostArch               string `json:"host_arch"`
	WorkerOS               string `json:"worker_os"`
	WorkerArch             string `json:"worker_arch"`
	RuntimeName            string `json:"runtime_name"`
	RuntimeVersion         string `json:"runtime_version"`
	Provider               string `json:"provider"`
	Model                  string `json:"model"`
	ReasoningEffort        string `json:"reasoning_effort"`
	CredentialMode         string `json:"credential_mode"`
	RelayTopology          string `json:"relay_topology"`
	EndpointAuthority      string `json:"endpoint_authority"`
	CapacityAuthority      string `json:"capacity_authority"`
	CapacityAuthorityScope string `json:"capacity_authority_scope"`
}

type QualificationEvidence struct {
	Class      string `json:"class"` // metadata, synthetic, or live
	Name       string `json:"name"`
	ArtifactID string `json:"artifact_id"`
	Digest     string `json:"digest"`
	ObservedAt int64  `json:"observed_at"`
	Result     string `json:"result"` // pass or fail
}

// QualificationRecord is immutable once recorded. Reasons are mandatory for
// negative or incomplete records and are safe, credential-free diagnostics.
type QualificationRecord struct {
	SchemaVersion   int                     `json:"schema_version"`
	Status          string                  `json:"status"`
	Inputs          QualificationInputs     `json:"inputs"`
	Claims          []string                `json:"claims"`
	Roles           []string                `json:"roles"`
	Layouts         []string                `json:"layouts"`
	RecoveryClasses []string                `json:"recovery_classes"`
	Evidence        []QualificationEvidence `json:"evidence"`
	Reasons         []string                `json:"reasons"`
	ObservedAt      int64                   `json:"observed_at"`
}

type EligibilityRequest struct {
	Inputs                  QualificationInputs `json:"inputs"`
	Role                    string              `json:"role"`
	Layout                  string              `json:"layout"`
	RequiredClaims          []string            `json:"required_claims"`
	RequiredRecoveryClasses []string            `json:"required_recovery_classes"`
}

type Eligibility struct {
	Status      string   `json:"status"`
	Reasons     []string `json:"reasons"`
	EvidenceID  string   `json:"evidence_id,omitempty"`
	InputDigest string   `json:"input_digest"`
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func bounded(value, name string) error {
	if value == "" || len(value) > 256 || strings.ContainsRune(value, 0) {
		return fmt.Errorf("%s is required and must be bounded", name)
	}
	return nil
}

func (in QualificationInputs) Validate() error {
	values := map[string]string{
		"harness": in.Harness, "harness_version": in.HarnessVersion,
		"host_os": in.HostOS, "host_arch": in.HostArch, "worker_os": in.WorkerOS,
		"worker_arch": in.WorkerArch, "runtime_name": in.RuntimeName,
		"runtime_version": in.RuntimeVersion, "provider": in.Provider, "model": in.Model,
		"reasoning_effort": in.ReasoningEffort, "credential_mode": in.CredentialMode,
		"relay_topology": in.RelayTopology, "endpoint_authority": in.EndpointAuthority,
		"capacity_authority":       in.CapacityAuthority,
		"capacity_authority_scope": in.CapacityAuthorityScope, "profile_id": in.ProfileID,
	}
	for name, value := range values {
		if err := bounded(value, name); err != nil {
			return err
		}
	}
	if in.Harness != "codex" && in.Harness != "hermes" {
		return errors.New("unsupported harness")
	}
	if in.ProfileRevision < 1 {
		return errors.New("profile_revision must be positive")
	}
	for name, value := range map[string]string{
		"harness_source_digest": in.HarnessSourceDigest, "executable_digest": in.ExecutableDigest,
		"image_digest": in.ImageDigest, "guardian_digest": in.GuardianDigest,
		"worker_digest": in.WorkerDigest, "relay_digest": in.RelayDigest,
		"profile_digest": in.ProfileDigest, "profile_config_digest": in.ProfileConfigDigest,
		"tool_config_digest": in.ToolConfigDigest, "instruction_digest": in.InstructionDigest,
		"mount_plan_digest": in.MountPlanDigest, "runtime_digest": in.RuntimeDigest,
	} {
		if !validDigest(value) {
			return fmt.Errorf("%s must be a sha256 digest", name)
		}
	}
	if in.CapacityAuthorityScope != "single_host" && in.CapacityAuthorityScope != "shared_cross_host" && in.CapacityAuthorityScope != "uncoordinated_cross_host" {
		return errors.New("unknown capacity authority scope")
	}
	return nil
}

func canonical(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return store.Canonical(raw)
}

func inputDigest(in QualificationInputs) (string, error) {
	if err := in.Validate(); err != nil {
		return "", err
	}
	raw, err := canonical(in)
	if err != nil {
		return "", err
	}
	return store.Digest(raw), nil
}

func validateSet(name string, values []string, allowed map[string]bool, required bool) error {
	if (required && len(values) == 0) || len(values) > 32 {
		return fmt.Errorf("%s must contain 1-32 values", name)
	}
	seen := map[string]bool{}
	for _, value := range values {
		if err := bounded(value, name); err != nil {
			return err
		}
		if seen[value] || (allowed != nil && !allowed[value]) {
			return fmt.Errorf("invalid or duplicate %s value %q", name, value)
		}
		seen[value] = true
	}
	return nil
}

func validateRecord(record QualificationRecord, now int64) error {
	if record.SchemaVersion != 1 {
		return errors.New("unsupported qualification record schema")
	}
	if record.Status != "supported" && record.Status != "unsupported" && record.Status != "unverified" {
		return errors.New("invalid qualification status")
	}
	if err := record.Inputs.Validate(); err != nil {
		return err
	}
	if record.ObservedAt <= 0 || record.ObservedAt > now+int64((5*time.Minute)/time.Millisecond) {
		return errors.New("invalid qualification observation time")
	}
	if err := validateSet("claim", record.Claims, validClaims, true); err != nil {
		return err
	}
	if err := validateSet("role", record.Roles, nil, true); err != nil {
		return err
	}
	if err := validateSet("layout", record.Layouts, nil, true); err != nil {
		return err
	}
	if err := validateSet("recovery class", record.RecoveryClasses, nil, false); err != nil {
		return err
	}
	if len(record.Evidence) == 0 || len(record.Evidence) > 128 {
		return errors.New("qualification evidence must contain 1-128 observations")
	}
	type evidenceKey struct {
		class string
		name  string
	}
	classes := map[string]bool{}
	// Keep exact plain names used by class-agnostic recovery evidence separate
	// from class-specific requirements such as live provider-idle proof.
	evidenceNames := map[string]bool{}
	evidenceByClassAndName := map[evidenceKey]bool{}
	for _, evidence := range record.Evidence {
		if evidence.Class != "metadata" && evidence.Class != "synthetic" && evidence.Class != "live" {
			return errors.New("invalid qualification evidence class")
		}
		if err := bounded(evidence.Name, "evidence name"); err != nil || !store.SafeID(evidence.ArtifactID) || len(evidence.ArtifactID) > 128 || !validDigest(evidence.Digest) || evidence.ObservedAt <= 0 || evidence.ObservedAt > now+int64((5*time.Minute)/time.Millisecond) || (evidence.Result != "pass" && evidence.Result != "fail") {
			return errors.New("invalid qualification evidence observation")
		}
		if _, exists := evidenceNames[evidence.Name]; exists {
			return errors.New("duplicate qualification evidence name")
		}
		passed := evidence.Result == "pass"
		evidenceNames[evidence.Name] = passed
		evidenceByClassAndName[evidenceKey{class: evidence.Class, name: evidence.Name}] = passed
		classes[evidence.Class] = true
		if record.Status == "supported" && evidence.Result != "pass" {
			return errors.New("supported qualification contains failed evidence")
		}
	}
	if record.Status == "supported" {
		if len(record.Reasons) != 0 || !classes["metadata"] || !classes["synthetic"] || !classes["live"] {
			return errors.New("supported qualification requires passing metadata, synthetic and live evidence")
		}
		if record.Inputs.CapacityAuthorityScope == "uncoordinated_cross_host" {
			return errors.New("uncoordinated cross-host capacity cannot be qualified")
		}
		for _, claim := range record.Claims {
			if claim == ClaimProviderIdle && !evidenceByClassAndName[evidenceKey{class: "live", name: "provider_idle"}] {
				return errors.New("provider-idle claim requires passing live provider_idle evidence")
			}
			if claim == ClaimProductionLaunch {
				if len(record.RecoveryClasses) == 0 {
					return errors.New("production launch claim requires recovery classes")
				}
				for _, recovery := range record.RecoveryClasses {
					if !evidenceNames["recovery:"+recovery] {
						return fmt.Errorf("production launch claim lacks recovery evidence for %s", recovery)
					}
				}
			}
		}
	} else if len(record.Reasons) == 0 || len(record.Reasons) > 32 {
		return errors.New("unsupported or unverified qualification requires reasons")
	}
	for _, reason := range record.Reasons {
		if err := bounded(reason, "reason"); err != nil {
			return err
		}
	}
	return nil
}

func verifyQualificationEvidence(ctx context.Context, db *store.DB, record QualificationRecord) error {
	repository, err := artifacts.New(db)
	if err != nil {
		return err
	}
	for _, evidence := range record.Evidence {
		if err := repository.Verify(ctx, evidence.ArtifactID, evidence.Digest, QualificationEvidenceArtifactKind); err != nil {
			return fmt.Errorf("qualification evidence %q is unavailable or corrupt: %w", evidence.Name, err)
		}
	}
	return nil
}

// RecordTrustedQualification is intentionally absent from Engine.Apply. Only a
// verifier in the trusted core may call it after checking the referenced
// manifests and observations; user-authored profile capabilities are not input.
func RecordTrustedQualification(ctx context.Context, db *store.DB, record QualificationRecord) (string, error) {
	if db == nil || db.Kind != "project" {
		return "", errors.New("qualification requires a project database")
	}
	now := store.Now()
	if err := validateRecord(record, now); err != nil {
		return "", err
	}
	if err := verifyQualificationEvidence(ctx, db, record); err != nil {
		return "", err
	}
	raw, err := canonical(record)
	if err != nil {
		return "", err
	}
	if len(raw) > store.MaxDocument {
		return "", errors.New("qualification record exceeds document limit")
	}
	id := store.Digest(raw)
	input, err := inputDigest(record.Inputs)
	if err != nil {
		return "", err
	}
	err = db.Write(ctx, func(tx *store.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO execution_qualifications(id,input_digest,record_digest,status,observed_at,recorded_at,record_json) VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING", id, input, id, record.Status, record.ObservedAt, now, string(raw))
		return err
	})
	return id, err
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func validateRequest(request EligibilityRequest) error {
	if err := request.Inputs.Validate(); err != nil {
		return err
	}
	if err := bounded(request.Role, "role"); err != nil {
		return err
	}
	if err := bounded(request.Layout, "layout"); err != nil {
		return err
	}
	if err := validateSet("required claim", request.RequiredClaims, validClaims, true); err != nil {
		return err
	}
	return validateSet("required recovery class", request.RequiredRecoveryClasses, nil, false)
}

// QueryEligibility uses the latest inserted immutable record for the exact input
// digest. It never combines observations from different profiles, images,
// runtimes or platforms.
func QueryEligibility(ctx context.Context, db *store.DB, request EligibilityRequest) (Eligibility, error) {
	result := Eligibility{Status: "unverified", Reasons: []string{}}
	if db == nil || db.Kind != "project" {
		return result, errors.New("eligibility requires a project database")
	}
	if err := validateRequest(request); err != nil {
		return result, err
	}
	digest, err := inputDigest(request.Inputs)
	if err != nil {
		return result, err
	}
	result.InputDigest = digest
	var id, recordDigest, status, raw string
	err = db.SQL.QueryRowContext(ctx, "SELECT id,record_digest,status,record_json FROM execution_qualifications WHERE input_digest=? ORDER BY sequence DESC LIMIT 1", digest).Scan(&id, &recordDigest, &status, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		result.Reasons = []string{"no trusted qualification record for the exact execution inputs"}
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.EvidenceID = id
	canonicalRaw, canonicalErr := store.Canonical([]byte(raw))
	if canonicalErr != nil || id != recordDigest || id != store.Digest(canonicalRaw) {
		result.Status = "unsupported"
		result.Reasons = []string{"trusted qualification record is corrupt"}
		return result, nil
	}
	var record QualificationRecord
	if err := store.Decode(canonicalRaw, &record); err != nil || validateRecord(record, store.Now()) != nil {
		result.Status = "unsupported"
		result.Reasons = []string{"trusted qualification record is invalid"}
		return result, nil
	}
	actualDigest, err := inputDigest(record.Inputs)
	if err != nil || actualDigest != digest || status != record.Status {
		result.Status = "unsupported"
		result.Reasons = []string{"trusted qualification identity does not match its index"}
		return result, nil
	}
	if err := verifyQualificationEvidence(ctx, db, record); err != nil {
		result.Status = "unsupported"
		result.Reasons = []string{"referenced qualification evidence is unavailable or corrupt"}
		return result, nil
	}
	if record.Status != "supported" {
		result.Status = record.Status
		result.Reasons = append(result.Reasons, record.Reasons...)
		sort.Strings(result.Reasons)
		return result, nil
	}
	for _, claim := range request.RequiredClaims {
		if !contains(record.Claims, claim) {
			result.Reasons = append(result.Reasons, "qualification does not cover claim: "+claim)
		}
	}
	if !contains(record.Roles, request.Role) {
		result.Reasons = append(result.Reasons, "qualification does not cover role: "+request.Role)
	}
	if !contains(record.Layouts, request.Layout) {
		result.Reasons = append(result.Reasons, "qualification does not cover layout: "+request.Layout)
	}
	for _, recovery := range request.RequiredRecoveryClasses {
		if !contains(record.RecoveryClasses, recovery) {
			result.Reasons = append(result.Reasons, "qualification does not cover recovery class: "+recovery)
		}
	}
	if len(result.Reasons) == 0 {
		result.Status = "supported"
	} else {
		result.Status = "unverified"
		sort.Strings(result.Reasons)
	}
	return result, nil
}
