package policy

import (
	"errors"
	"strings"
)

// Omitted dimensions are unrestricted; an explicit empty list permits nothing.
// Lists are exact values, never glob patterns. Intersect at every enclosing scope.
type Restrictions map[string][]string

var OperationCategories = []string{"plan_accept", "profile_assignment", "paid_spending", "task_change", "commit", "push", "draft_request", "checkpoint_restore"}

func (r Restrictions) Validate() error {
	for dimension, values := range r {
		if dimension != "profile_ids" && dimension != "operation_categories" {
			return errors.New("unknown restriction dimension")
		}
		if values == nil {
			return errors.New("restriction requires a list; use [] to permit nothing")
		}
		if len(values) > 100 {
			return errors.New("too many restriction values")
		}
		seen := map[string]bool{}
		for _, value := range values {
			if value == "" || len(value) > 128 || strings.ContainsAny(value, "*\r\n\t\x00") || strings.TrimSpace(value) != value || seen[value] {
				return errors.New("restriction values must be unique exact identifiers")
			}
			if dimension == "operation_categories" && !Contains(OperationCategories, value) {
				return errors.New("unknown restricted operation category")
			}
			seen[value] = true
		}
	}
	return nil
}

func (r Restrictions) Allows(dimension, value string) bool {
	values, present := r[dimension]
	return !present || Contains(values, value)
}
