// Package policy evaluates definitions and permission restrictions without effects.
package policy

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type Capability struct {
	Name           string `json:"name"`
	Support        string `json:"support"`
	Guarantee      string `json:"guarantee"`
	Platform       string `json:"platform"`
	HarnessVersion string `json:"harness_version"`
	EvidenceID     string `json:"evidence_id"`
}
type Profile struct {
	ID                 string       `json:"id"`
	Harness            string       `json:"harness"`
	Version            string       `json:"version"`
	Model              string       `json:"model"`
	Provider           string       `json:"provider"`
	CredentialRef      string       `json:"credential_ref"`
	Roles              []string     `json:"roles"`
	EndpointID         string       `json:"endpoint_id,omitempty"`
	LocalInference     bool         `json:"local_inference"`
	AuxiliaryLocal     bool         `json:"auxiliary_local"`
	DelegationDisabled bool         `json:"delegation_disabled"`
	Capabilities       []Capability `json:"capabilities"`
}
type Config struct {
	Restrictions      Restrictions `json:"restrictions,omitempty"`
	ModelPolicy       string       `json:"model_policy"`
	Deny              []string     `json:"deny"`
	RequiredChecks    []string     `json:"required_checks"`
	TaskLimitMS       int64        `json:"task_limit_ms"`
	AttemptLimitMS    int64        `json:"attempt_limit_ms"`
	RepairLimit       int          `json:"repair_limit"`
	SupervisorProfile string       `json:"supervisor_profile"`
	ApprovalMode      string       `json:"approval_mode"`
}
type Layer struct {
	Restrictions Restrictions
	Name         string
	Deny         []string
	Checks       []string
	LimitMS      int64
}
type Resolved struct {
	Restrictions Restrictions        `json:"restrictions"`
	Deny         []string            `json:"deny"`
	Checks       []string            `json:"checks"`
	LimitMS      int64               `json:"limit_ms"`
	Origins      map[string][]string `json:"origins"`
}

func Contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func Resolve(layers []Layer) (Resolved, error) {
	r := Resolved{Origins: map[string][]string{}, Restrictions: Restrictions{}}
	denies, checks := map[string]bool{}, map[string]bool{}
	for _, layer := range layers {
		if err := layer.Restrictions.Validate(); err != nil {
			return r, err
		}
		for dimension, values := range layer.Restrictions {
			prior, present := r.Restrictions[dimension]
			intersection := []string{}
			for _, value := range values {
				if !present || Contains(prior, value) {
					intersection = append(intersection, value)
				}
			}
			sort.Strings(intersection)
			r.Restrictions[dimension] = intersection
			r.Origins["restriction:"+dimension] = append(r.Origins["restriction:"+dimension], layer.Name)
		}
		if layer.LimitMS < 0 {
			return r, errors.New("negative execution limit")
		}
		if layer.LimitMS > 0 && (r.LimitMS == 0 || layer.LimitMS < r.LimitMS) {
			r.LimitMS = layer.LimitMS
		}
		for _, d := range layer.Deny {
			denies[d] = true
			r.Origins["deny:"+d] = append(r.Origins["deny:"+d], layer.Name)
		}
		for _, c := range layer.Checks {
			checks[c] = true
			r.Origins["check:"+c] = append(r.Origins["check:"+c], layer.Name)
		}
	}
	for d := range denies {
		r.Deny = append(r.Deny, d)
	}
	for c := range checks {
		r.Checks = append(r.Checks, c)
	}
	sort.Strings(r.Deny)
	sort.Strings(r.Checks)
	return r, nil
}
func (c Config) Validate() error {
	if err := c.Restrictions.Validate(); err != nil {
		return err
	}
	if c.ModelPolicy != "local_only" && c.ModelPolicy != "hybrid" && c.ModelPolicy != "cloud_allowed" {
		return errors.New("choose explicit model_policy: local_only, hybrid or cloud_allowed")
	}
	if c.ApprovalMode != "supervised" && c.ApprovalMode != "autonomous" {
		return errors.New("explicit approval_mode required")
	}
	if c.AttemptLimitMS <= 0 || c.TaskLimitMS < c.AttemptLimitMS || c.RepairLimit < 0 || c.RepairLimit > 20 || c.SupervisorProfile == "" {
		return errors.New("explicit bounded limits and supervisor profile required")
	}
	return nil
}
func (p Profile) Validate() error {
	if p.ID == "" || p.Version == "" || p.Model == "" || p.Provider == "" || (p.Harness != "codex" && p.Harness != "hermes") || len(p.Roles) == 0 {
		return errors.New("profile requires harness/version/model/provider/roles")
	}
	if !strings.HasPrefix(p.CredentialRef, "env:") && !strings.HasPrefix(p.CredentialRef, "file:/") {
		return errors.New("credential_ref must name env:VARIABLE or file:/absolute/path, never contain a key")
	}
	if strings.HasPrefix(p.CredentialRef, "env:") {
		name := strings.TrimPrefix(p.CredentialRef, "env:")
		if name == "" {
			return errors.New("empty credential variable")
		}
		for _, r := range name {
			if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
				return errors.New("invalid credential variable")
			}
		}
	}
	for _, role := range p.Roles {
		if !Contains([]string{"implementation", "review", "supervisor", "finalization"}, role) {
			return fmt.Errorf("unsupported profile role %s", role)
		}
	}
	if p.LocalInference && p.EndpointID == "" {
		return errors.New("local inference requires physical endpoint identity")
	}
	for _, c := range p.Capabilities {
		if c.Name == "" || !Contains([]string{"available", "unsupported", "unverified"}, c.Support) || !Contains([]string{"application_enforced", "native_enforced", "advisory"}, c.Guarantee) || c.Platform == "" || c.HarnessVersion != p.Version || c.EvidenceID == "" {
			return errors.New("incomplete or stale capability evidence")
		}
	}
	return nil
}
func Eligibility(c Config, p Profile, role string) []string {
	var reasons []string
	if !c.Restrictions.Allows("profile_ids", p.ID) {
		reasons = append(reasons, "profile denied by enclosing restriction: "+p.ID)
	}
	if !Contains(p.Roles, role) {
		reasons = append(reasons, "profile not eligible for "+role)
	}
	if c.ModelPolicy == "local_only" && (!p.LocalInference || !p.AuxiliaryLocal || !p.DelegationDisabled) {
		reasons = append(reasons, "local-only requires every inference route local and delegation disabled")
	}
	return reasons
}

type Criterion struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Manual bool   `json:"manual"`
}
type Task struct {
	Restrictions   Restrictions `json:"restrictions,omitempty"`
	ID             string       `json:"id"`
	Objective      string       `json:"objective"`
	Criteria       []Criterion  `json:"criteria"`
	Dependencies   []string     `json:"dependencies"`
	Context        []string     `json:"context"`
	Scope          []string     `json:"scope"`
	Checks         []string     `json:"checks"`
	Questions      []string     `json:"questions"`
	Implementation string       `json:"implementation_profile"`
	Reviewer       string       `json:"reviewer_profile"`
	Difficulty     string       `json:"difficulty"`
	Rationale      string       `json:"rationale"`
	ActiveLimitMS  int64        `json:"active_limit_ms"`
	RepairLimit    int          `json:"repair_limit"`
}

func ValidateTasks(tasks []Task) error {
	if len(tasks) == 0 || len(tasks) > 100 {
		return errors.New("plan requires 1–100 tasks")
	}
	byID := map[string]Task{}
	for _, t := range tasks {
		if err := t.Restrictions.Validate(); err != nil {
			return err
		}
		if t.ID == "" || byID[t.ID].ID != "" || t.Objective == "" || len(t.Criteria) == 0 || len(t.Scope) == 0 || t.Implementation == "" || t.Reviewer == "" || t.Difficulty == "" || t.Rationale == "" || t.ActiveLimitMS <= 0 || t.RepairLimit < 0 {
			return errors.New("duplicate task ID or incomplete task definition")
		}
		criteria := map[string]bool{}
		for _, c := range t.Criteria {
			if c.ID == "" || c.Text == "" || criteria[c.ID] {
				return errors.New("duplicate or empty criterion")
			}
			criteria[c.ID] = true
		}
		for _, path := range t.Scope {
			if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "\\") || strings.ContainsRune(path, 0) {
				return errors.New("scope must contain repository-relative paths")
			}
			for _, component := range strings.Split(path, "/") {
				if component == ".." {
					return errors.New("scope escapes repository")
				}
			}
		}
		byID[t.ID] = t
	}
	seen, active := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if active[id] {
			return errors.New("task dependency cycle")
		}
		if seen[id] {
			return nil
		}
		t, ok := byID[id]
		if !ok {
			return fmt.Errorf("unknown or foreign dependency %s", id)
		}
		active[id] = true
		unique := map[string]bool{}
		for _, dep := range t.Dependencies {
			if unique[dep] {
				return errors.New("duplicate dependency")
			}
			unique[dep] = true
			if err := visit(dep); err != nil {
				return err
			}
		}
		active[id] = false
		seen[id] = true
		return nil
	}
	for id := range byID {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}
