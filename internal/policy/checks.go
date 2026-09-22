package policy

import (
	"errors"
	"strings"
)

// CheckDefinition is approved data, not an instruction to run a subprocess.
// The future check runner must use the qualified boundary and recorded argv.
type CheckDefinition struct {
	ID              string                `json:"id"`
	Argv            []string              `json:"argv"`
	Cwd             string                `json:"cwd"`
	Environment     []EnvironmentVariable `json:"environment,omitempty"`
	RequiredOutputs []string              `json:"required_outputs,omitempty"`
	TimeoutMS       int64                 `json:"timeout_ms"`
	MaxOutputBytes  int64                 `json:"max_output_bytes,omitempty"`
}

type EnvironmentVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ManualPrerequisite records human setup evidence. It is separate from manual
// functional acceptance, which must later bind to the evaluated code fingerprint.
type ManualPrerequisite struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Satisfied   bool   `json:"satisfied"`
	Evidence    string `json:"evidence,omitempty"`
}

func definitionID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return value != "." && value != ".."
}

func (c CheckDefinition) Validate(ceiling int64) error {
	if !definitionID(c.ID) || len(c.Argv) == 0 || len(c.Argv) > 64 || c.TimeoutMS <= 0 || c.TimeoutMS > ceiling {
		return errors.New("check requires an ID, bounded argv and timeout within the attempt ceiling")
	}
	for _, arg := range c.Argv {
		if len(arg) > 4096 || strings.ContainsRune(arg, 0) {
			return errors.New("invalid check argument")
		}
	}
	if c.Argv[0] == "" {
		return errors.New("check executable required")
	}
	if err := checkRelativePath(c.Cwd, true); err != nil {
		return errors.New("check cwd must be an explicit project-relative directory")
	}
	if c.MaxOutputBytes < 0 || c.MaxOutputBytes > 16<<20 {
		return errors.New("check output ceiling must be at most 16 MiB")
	}
	if len(c.Environment) > 64 || len(c.RequiredOutputs) > 64 {
		return errors.New("too many check environment variables or required outputs")
	}
	last := ""
	for _, variable := range c.Environment {
		if !environmentName(variable.Name) || variable.Name <= last || len(variable.Value) > 4096 || strings.ContainsRune(variable.Value, 0) {
			return errors.New("check environment must be unique, sorted and bounded")
		}
		last = variable.Name
	}
	last = ""
	for _, output := range c.RequiredOutputs {
		if err := checkRelativePath(output, false); err != nil || output <= last {
			return errors.New("required outputs must be unique, sorted project-relative paths")
		}
		last = output
	}
	return nil
}

func (c CheckDefinition) OutputLimit() int64 {
	if c.MaxOutputBytes == 0 {
		return 1 << 20
	}
	return c.MaxOutputBytes
}

func environmentName(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for i, r := range value {
		if !(r == '_' || r >= 'A' && r <= 'Z' || i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func checkRelativePath(value string, allowDot bool) error {
	if value == "" || len(value) > 4096 || strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\\\x00*?[]") || (!allowDot && value == ".") {
		return errors.New("invalid relative path")
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == ".." || part == "." && value != "." {
			return errors.New("relative path escapes or is not canonical")
		}
	}
	return nil
}

func validateCheckReferences(ids []string) error {
	if len(ids) > 100 {
		return errors.New("too many check references")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !definitionID(id) || seen[id] {
			return errors.New("invalid or duplicate check reference")
		}
		seen[id] = true
	}
	return nil
}

func validatePrerequisites(prerequisites []ManualPrerequisite) error {
	if len(prerequisites) > 50 {
		return errors.New("too many manual prerequisites")
	}
	seen := map[string]bool{}
	for _, p := range prerequisites {
		if !definitionID(p.ID) || seen[p.ID] || strings.TrimSpace(p.Description) == "" || len(p.Description) > 4096 || len(p.Evidence) > 4096 {
			return errors.New("invalid or duplicate manual prerequisite")
		}
		if p.Satisfied && strings.TrimSpace(p.Evidence) == "" {
			return errors.New("satisfied manual prerequisite requires human evidence")
		}
		seen[p.ID] = true
	}
	return nil
}
