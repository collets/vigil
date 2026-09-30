package scenario

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Stable identities for the scenario. They are deterministic so a rerun produces
// comparable evidence, and they are visibly fixture-labelled so nothing they
// create can be mistaken for a real user or real acceptance.
const (
	ProjectName     = "stage-5.7-scenario"
	ProfileID       = "scenario-local"
	EndpointID      = "scenario-endpoint"
	PlanID          = "scenario-plan"
	TaskID          = "scenario-task"
	RepositoryID    = "primary"
	SpecificationID = "scenario-spec"
	CheckID         = "scenario-check"
	ProposalID      = "scenario-proposal"
	PlanBranch      = "vigil/scenario-plan"
	// ManualCriterionID is the explicitly manual acceptance item. Recording it
	// is a fixture actor, which is Stage 5.7's own rehearsal, never a real
	// user's functional Pass.
	ManualCriterionID = "manual-functional-review"
)

// ProjectConfig is the scenario's explicit project configuration. Every value an
// operator would normally choose is stated here, so the manifest records the
// exact policy, budgets and check definition the rehearsal ran under.
type ProjectConfig struct {
	ModelPolicy             string        `json:"model_policy"`
	RequiredChecks          []string      `json:"required_checks"`
	CheckDefinitions        []interface{} `json:"check_definitions"`
	TaskLimitMS             int64         `json:"task_limit_ms"`
	AttemptLimitMS          int64         `json:"attempt_limit_ms"`
	RepairLimit             int           `json:"repair_limit"`
	SupervisorProfile       string        `json:"supervisor_profile"`
	ApprovalMode            string        `json:"approval_mode"`
	HumanAcceptanceRequired bool          `json:"human_acceptance_required"`
	ReviewBlockingSeverity  string        `json:"review_blocking_severity"`
}

// Profile is the scenario's declared harness profile. It records a credential
// *reference*, never a credential value, and its declaration establishes
// intent only: production eligibility remains false without exact Stage 5.1
// trusted evidence.
type Profile struct {
	ID                 string   `json:"id"`
	Harness            string   `json:"harness"`
	Version            string   `json:"version"`
	Model              string   `json:"model"`
	Provider           string   `json:"provider"`
	CredentialRef      string   `json:"credential_ref"`
	Roles              []string `json:"roles"`
	EndpointID         string   `json:"endpoint_id"`
	LocalInference     bool     `json:"local_inference"`
	AuxiliaryLocal     bool     `json:"auxiliary_local"`
	DelegationDisabled bool     `json:"delegation_disabled"`
}

// Criterion is one acceptance criterion in the scenario plan.
type Criterion struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Manual bool   `json:"manual"`
}

// Task is the single scenario task.
type Task struct {
	ID             string      `json:"id"`
	Objective      string      `json:"objective"`
	Criteria       []Criterion `json:"criteria"`
	Dependencies   []string    `json:"dependencies"`
	Context        []string    `json:"context"`
	Scope          []string    `json:"scope"`
	Checks         []string    `json:"checks"`
	Questions      []string    `json:"questions"`
	Implementation string      `json:"implementation_profile"`
	Reviewer       string      `json:"reviewer_profile"`
	Difficulty     string      `json:"difficulty"`
	Rationale      string      `json:"rationale"`
	ActiveLimitMS  int64       `json:"active_limit_ms"`
	RepairLimit    int         `json:"repair_limit"`
}

// Plan is the single scenario plan.
type Plan struct {
	ID                      string      `json:"id"`
	Title                   string      `json:"title"`
	Specification           string      `json:"specification"`
	Approved                bool        `json:"approved"`
	QualityCriteria         []Criterion `json:"quality_criteria,omitempty"`
	QualityChecks           []string    `json:"quality_checks,omitempty"`
	ReviewerProfile         string      `json:"reviewer_profile"`
	HumanAcceptanceRequired bool        `json:"human_acceptance_required"`
	Tasks                   []Task      `json:"tasks"`
}

// ScenarioConfig builds the explicit project configuration for the rehearsal.
//
// The check definition runs the fixture repository's own committed check script
// from inside the isolated copy the runner creates, using a repository-relative
// path and the project-relative cwd the check contract requires. The objective
// criterion is therefore verified by real content, not by a hard-coded pass.
func ScenarioConfig() ProjectConfig {
	return ProjectConfig{
		ModelPolicy:    "local_only",
		RequiredChecks: []string{CheckID},
		CheckDefinitions: []interface{}{map[string]any{
			"id":               CheckID,
			"argv":             []string{"/bin/sh", CheckScript},
			"cwd":              ".",
			"environment":      []any{},
			"required_outputs": []string{"build/result.txt"},
			"timeout_ms":       30000,
			"max_output_bytes": 65536,
		}},
		TaskLimitMS:             300000,
		AttemptLimitMS:          60000,
		RepairLimit:             2,
		SupervisorProfile:       ProfileID,
		ApprovalMode:            "supervised",
		HumanAcceptanceRequired: true,
		ReviewBlockingSeverity:  "high",
	}
}

// ScenarioProfile declares the local harness profile used for the rehearsal.
func ScenarioProfile(version, model string) Profile {
	return Profile{
		ID:                 ProfileID,
		Harness:            "hermes",
		Version:            version,
		Model:              model,
		Provider:           "custom",
		CredentialRef:      "env:OPENAI_API_KEY",
		Roles:              []string{"implementation", "review", "supervisor", "planning", "finalization"},
		EndpointID:         EndpointID,
		LocalInference:     true,
		AuxiliaryLocal:     true,
		DelegationDisabled: true,
	}
}

// ScenarioPlan builds the single-task plan, including the explicitly manual
// criterion that no automated action may satisfy.
func ScenarioPlan(specification string) Plan {
	return Plan{
		ID:            PlanID,
		Title:         "Stage 5.7 autonomous end-to-end scenario",
		Approved:      true,
		Specification: specification,
		QualityCriteria: []Criterion{
			{ID: ManualCriterionID, Text: "A human functionally verified the resulting artifact in a real repository", Manual: true},
		},
		QualityChecks:           []string{CheckID},
		ReviewerProfile:         ProfileID,
		HumanAcceptanceRequired: true,
		Tasks: []Task{{
			ID:        TaskID,
			Objective: "Make src/greeting.txt contain exactly the greeting the specification requires",
			Criteria: []Criterion{
				{ID: "greeting-matches-spec", Text: "src/greeting.txt contains exactly the specified greeting"},
				{ID: "deterministic-check", Text: "The scenario check exits zero and writes build/result.txt"},
				{ID: ManualCriterionID, Text: "A human functionally verified the artifact in a real repository", Manual: true},
			},
			Dependencies:   []string{},
			Context:        []string{SpecPath},
			Scope:          []string{"src/**"},
			Checks:         []string{CheckID},
			Questions:      []string{},
			Implementation: ProfileID,
			Reviewer:       ProfileID,
			Difficulty:     "small",
			Rationale:      "One independently verifiable file change with an objective check",
			ActiveLimitMS:  60000,
			RepairLimit:    2,
		}},
	}
}

// RepositoryEnrollment is the explicit disposable repository enrollment for a
// repository with no remote — the shape every probe project uses.
func RepositoryEnrollment(root string) map[string]any {
	return repositoryEnrollment(root, "")
}

// RepositoryEnrollmentWithRemote enrolls a repository that has a named remote
// already registered on it.
//
// The remote must be named at enrollment, because enrollment records the remote's
// identity as part of the accepted baseline. Enrolling without one and adding it
// later would leave the enrolled identity describing a repository with no remote,
// and the push path would then correctly refuse a destination it never verified.
func RepositoryEnrollmentWithRemote(root, remote string) map[string]any {
	return repositoryEnrollment(root, remote)
}

func repositoryEnrollment(root, remote string) map[string]any {
	return map[string]any{
		"id":                RepositoryID,
		"plan_id":           PlanID,
		"root":              root,
		"base_ref":          "refs/heads/main",
		"plan_branch":       PlanBranch,
		"remote":            remote,
		"dirty_choice":      "clean",
		"nested_boundaries": []any{},
	}
}

// ReviewDocument is a closed-schema fresh review result.
type ReviewDocument struct {
	SchemaVersion int             `json:"schema_version"`
	Decision      string          `json:"decision"`
	Summary       string          `json:"summary"`
	Findings      []ReviewFinding `json:"findings"`
}

// ReviewFinding is one review finding. The blocking finding this scenario injects
// names the real, committed defect: the greeting does not match the
// specification. It is never fabricated to a preferred result.
type ReviewFinding struct {
	ID             string `json:"id"`
	Severity       string `json:"severity"`
	RepositoryID   string `json:"repository_id,omitempty"`
	Path           string `json:"path,omitempty"`
	Line           int    `json:"line,omitempty"`
	Evidence       string `json:"evidence"`
	Recommendation string `json:"recommendation"`
}

// writeFile writes a bounded private document.
func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

// writeJSON marshals value to a bounded private document.
func writeJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if len(raw) > 1<<20 {
		return fmt.Errorf("document %s exceeds the one mebibyte scenario bound", filepath.Base(path))
	}
	return writeFile(path, string(raw))
}

// mustReadJSON reads a bounded JSON document the production binary wrote.
func mustReadJSON(ctx context.Context, path string, target any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}
