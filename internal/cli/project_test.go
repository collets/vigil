package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistedCLIWorkflow(t *testing.T) {
	base := t.TempDir()
	state, root := filepath.Join(base, "state"), filepath.Join(base, "work")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, error) {
		cmd := NewCommand()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(append([]string{"--state-dir", state}, args...))
		err := cmd.Execute()
		return out.String(), err
	}
	out, err := run("project", "init", root)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatal(err)
	}
	if out, err := run("project", "discover", p.ID); err != nil || !strings.Contains(out, `"repositories": []`) {
		t.Fatal("read-only discovery", out, err)
	}
	file := filepath.Join(base, "command.json")
	input := `{"command_id":"config-1","expected_revision":1,"kind":"project.configure","payload":{"model_policy":"local_only","deny":["push"],"required_checks":[],"task_limit_ms":600000,"attempt_limit_ms":300000,"repair_limit":1,"supervisor_profile":"local","approval_mode":"supervised"}}`
	if err = os.WriteFile(file, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := run("project", "apply", p.ID, "--file", file)
	if err != nil {
		t.Fatal(err)
	}
	again, err := run("project", "apply", p.ID, "--file", file)
	if err != nil || first != again {
		t.Fatal("reopened receipt replay", err)
	}
	out, err = run("project", "status", p.ID)
	if err != nil || !strings.Contains(out, `"execution_eligible": false`) || !strings.Contains(out, `"revision": 2`) {
		t.Fatal(out, err)
	}
	out, err = run("project", "events", p.ID)
	if err != nil || !strings.Contains(out, "project.initialize") || !strings.Contains(out, "project.configure") {
		t.Fatal(out, err)
	}
	if err = os.WriteFile(file, []byte(strings.Replace(input, `"config-1"`, `"config-2"`, 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = run("project", "apply", p.ID, "--file", file); err == nil {
		t.Fatal("stale revision accepted")
	}
	if _, err = run("resources", "endpoint", "local", "http://localhost:8080/v1"); err == nil {
		t.Fatal("implicit cross-host authority")
	}
	if _, err = run("resources", "endpoint", "local", "http://localhost:8080/v1", "http://127.0.0.1:8080/v1", "--single-host"); err != nil {
		t.Fatal(err)
	}
}
