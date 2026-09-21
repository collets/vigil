package boundary

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"vigil/internal/artifacts"
	"vigil/internal/store"
)

func qualificationDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "private", "project.sqlite"), "project")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func testQualificationInputs() QualificationInputs {
	digest := func(value string) string { return store.Digest([]byte(value)) }
	return QualificationInputs{
		Harness: "hermes", HarnessVersion: "0.21.3", HarnessSourceDigest: digest("source"),
		ExecutableDigest: digest("executable"), ImageDigest: digest("image"),
		GuardianDigest: digest("guardian"), WorkerDigest: digest("worker"), RelayDigest: digest("relay"),
		ProfileID: "local", ProfileRevision: 1, ProfileDigest: digest("profile"),
		ProfileConfigDigest: digest("profile-config"), ToolConfigDigest: digest("tools"),
		InstructionDigest: digest("instructions"), MountPlanDigest: digest("mounts"),
		RuntimeDigest: digest("runtime"), HostOS: "linux", HostArch: "amd64",
		WorkerOS: "linux", WorkerArch: "amd64", RuntimeName: "docker",
		RuntimeVersion: "29.8.0", Provider: "custom", Model: "fixture-local",
		ReasoningEffort: "not_applicable", CredentialMode: "scoped_api_relay",
		RelayTopology: "host_socket_relay", EndpointAuthority: "windows-llama",
		CapacityAuthority: "wsl-coordinator", CapacityAuthorityScope: "single_host",
	}
}

func testQualificationRecord() QualificationRecord {
	now := store.Now()
	evidence := func(class, name string) QualificationEvidence {
		return QualificationEvidence{Class: class, Name: name, ArtifactID: "missing-" + strings.NewReplacer(":", "-").Replace(name), Digest: store.Digest([]byte(class + name)), ObservedAt: now, Result: "pass"}
	}
	return QualificationRecord{
		SchemaVersion: 1, Status: "supported", Inputs: testQualificationInputs(),
		Claims: []string{ClaimBoundaryExecution, ClaimProviderIdle, ClaimProductionLaunch},
		Roles:  []string{"implementation", "review"}, Layouts: []string{"ordinary_single_repository"},
		RecoveryClasses: []string{"docker_create", "native_submission", "result_persistence"},
		Evidence: []QualificationEvidence{
			evidence("metadata", "identity"), evidence("synthetic", "containment"),
			evidence("live", "provider_idle"), evidence("live", "bounded_task"), evidence("synthetic", "recovery:docker_create"),
			evidence("synthetic", "recovery:native_submission"), evidence("synthetic", "recovery:result_persistence"),
		},
		ObservedAt: now,
	}
}

func retainQualificationEvidence(t *testing.T, db *store.DB, record *QualificationRecord) *artifacts.Repository {
	t.Helper()
	repository, err := artifacts.New(db)
	if err != nil {
		t.Fatal(err)
	}
	for n := range record.Evidence {
		evidence := &record.Evidence[n]
		content := fmt.Sprintf("qualification evidence %d: %s/%s/%s", n, evidence.Class, evidence.Name, evidence.Result)
		artifact, err := repository.Put(context.Background(), "qualification-"+store.ID(), QualificationEvidenceArtifactKind, "durable", strings.NewReader(content))
		if err != nil {
			t.Fatal(err)
		}
		evidence.ArtifactID = artifact.ID
		evidence.Digest = artifact.Digest
	}
	return repository
}

func productionRequest(inputs QualificationInputs) EligibilityRequest {
	return EligibilityRequest{
		Inputs: inputs, Role: "implementation", Layout: "ordinary_single_repository",
		RequiredClaims:          []string{ClaimBoundaryExecution, ClaimProviderIdle, ClaimProductionLaunch},
		RequiredRecoveryClasses: []string{"docker_create", "native_submission", "result_persistence"},
	}
}

func TestQualificationExactInputsAndImmutability(t *testing.T) {
	ctx := context.Background()
	db := qualificationDB(t)
	record := testQualificationRecord()
	retainQualificationEvidence(t, db, &record)
	id, err := RecordTrustedQualification(ctx, db, record)
	if err != nil {
		t.Fatal(err)
	}
	got, err := QueryEligibility(ctx, db, productionRequest(record.Inputs))
	if err != nil || got.Status != "supported" || got.EvidenceID != id {
		t.Fatal("exact qualification not supported", got, err)
	}

	// Every input is part of the identity. Reflection keeps this list aligned
	// when a new field is added to QualificationInputs.
	base := reflect.ValueOf(record.Inputs)
	typeOfInputs := base.Type()
	for n := 0; n < base.NumField(); n++ {
		name := typeOfInputs.Field(n).Name
		t.Run("invalidates_"+name, func(t *testing.T) {
			changed := reflect.New(typeOfInputs).Elem()
			changed.Set(base)
			field := changed.Field(n)
			switch field.Kind() {
			case reflect.Int:
				field.SetInt(field.Int() + 1)
			case reflect.String:
				current := field.String()
				switch name {
				case "Harness":
					field.SetString("codex")
				case "CapacityAuthorityScope":
					field.SetString("shared_cross_host")
				default:
					if strings.HasSuffix(name, "Digest") {
						field.SetString(store.Digest([]byte("changed-" + name)))
					} else {
						field.SetString(current + "-changed")
					}
				}
			}
			request := productionRequest(changed.Interface().(QualificationInputs))
			result, err := QueryEligibility(ctx, db, request)
			if err != nil || result.Status != "unverified" || len(result.Reasons) == 0 {
				t.Fatal("changed input reused qualification", result, err)
			}
		})
	}

	if _, err = db.SQL.Exec("UPDATE execution_qualifications SET status='unsupported' WHERE id=?", id); err == nil {
		t.Fatal("qualification update accepted")
	}
	if _, err = db.SQL.Exec("DELETE FROM execution_qualifications WHERE id=?", id); err == nil {
		t.Fatal("qualification delete accepted")
	}
}

func TestQualificationMissingCorruptAndUnsupportedEvidence(t *testing.T) {
	ctx := context.Background()
	db := qualificationDB(t)
	request := productionRequest(testQualificationInputs())
	result, err := QueryEligibility(ctx, db, request)
	if err != nil || result.Status != "unverified" {
		t.Fatal("missing evidence did not remain unverified", result, err)
	}

	record := testQualificationRecord()
	record.Status = "unsupported"
	record.Reasons = []string{"provider lifecycle cannot be observed"}
	record.Evidence[2].Result = "fail"
	retainQualificationEvidence(t, db, &record)
	id, err := RecordTrustedQualification(ctx, db, record)
	if err != nil {
		t.Fatal(err)
	}
	result, err = QueryEligibility(ctx, db, request)
	if err != nil || result.Status != "unsupported" || len(result.Reasons) != 1 {
		t.Fatal("explicit negative evidence was not enforced", result, err)
	}

	// Corruption is tested only in this disposable database. Dropping the
	// immutability trigger simulates bytes changed outside the trusted API.
	if _, err = db.SQL.Exec("DROP TRIGGER execution_qualification_no_update"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SQL.Exec("UPDATE execution_qualifications SET record_json='{}' WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	result, err = QueryEligibility(ctx, db, request)
	if err != nil || result.Status != "unsupported" || !strings.Contains(strings.Join(result.Reasons, " "), "corrupt") {
		t.Fatal("corrupt evidence was not denied", result, err)
	}
}

func TestQualificationUsesLatestInsertionWhenTimestampsMatch(t *testing.T) {
	ctx := context.Background()
	db := qualificationDB(t)
	passing := testQualificationRecord()
	retainQualificationEvidence(t, db, &passing)
	if _, err := RecordTrustedQualification(ctx, db, passing); err != nil {
		t.Fatal(err)
	}
	denial := passing
	denial.Status = "unverified"
	denial.Reasons = []string{"new recovery evidence is still pending"}
	if _, err := RecordTrustedQualification(ctx, db, denial); err != nil {
		t.Fatal(err)
	}
	result, err := QueryEligibility(ctx, db, productionRequest(passing.Inputs))
	if err != nil || result.Status != "unverified" || !strings.Contains(strings.Join(result.Reasons, " "), "pending") {
		t.Fatal("same-timestamp records were not ordered by insertion", result, err)
	}
}

func TestQualificationEvidenceArtifactsAreRequiredAndReverified(t *testing.T) {
	ctx := context.Background()
	t.Run("absent at admission", func(t *testing.T) {
		db := qualificationDB(t)
		if _, err := RecordTrustedQualification(ctx, db, testQualificationRecord()); err == nil || !strings.Contains(err.Error(), "unavailable") {
			t.Fatal("qualification accepted absent evidence artifacts", err)
		}
	})

	for _, mode := range []string{"missing after admission", "corrupt after admission"} {
		t.Run(mode, func(t *testing.T) {
			db := qualificationDB(t)
			record := testQualificationRecord()
			repository := retainQualificationEvidence(t, db, &record)
			if _, err := RecordTrustedQualification(ctx, db, record); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(repository.Dir, "blobs", record.Evidence[0].Digest)
			if mode == "missing after admission" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
				t.Fatal(err)
			}
			result, err := QueryEligibility(ctx, db, productionRequest(record.Inputs))
			if err != nil || result.Status != "unsupported" || !strings.Contains(strings.Join(result.Reasons, " "), "evidence") {
				t.Fatal("missing or corrupt referenced evidence remained eligible", result, err)
			}
		})
	}
}

func TestQualificationEvidenceRemainsValidAfterReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "private", "project.sqlite")
	db, err := store.Open(ctx, path, "project")
	if err != nil {
		t.Fatal(err)
	}
	record := testQualificationRecord()
	retainQualificationEvidence(t, db, &record)
	if _, err := RecordTrustedQualification(ctx, db, record); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(ctx, path, "project")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	result, err := QueryEligibility(ctx, db, productionRequest(record.Inputs))
	if err != nil || result.Status != "supported" {
		t.Fatal("retained evidence failed after reopen", result, err)
	}
}

func TestSupportedQualificationRequiresCompleteEvidenceAndAuthority(t *testing.T) {
	db := qualificationDB(t)
	record := testQualificationRecord()
	record.Evidence = record.Evidence[:2]
	if _, err := RecordTrustedQualification(context.Background(), db, record); err == nil {
		t.Fatal("supported record without live evidence accepted")
	}
	record = testQualificationRecord()
	record.Inputs.CapacityAuthorityScope = "uncoordinated_cross_host"
	if _, err := RecordTrustedQualification(context.Background(), db, record); err == nil {
		t.Fatal("uncoordinated cross-host authority accepted")
	}
	record = testQualificationRecord()
	record.RecoveryClasses = nil
	if _, err := RecordTrustedQualification(context.Background(), db, record); err == nil {
		t.Fatal("production launch claim without recovery classes accepted")
	}
	record = testQualificationRecord()
	record.Evidence = append(record.Evidence[:2], record.Evidence[3:]...)
	retainQualificationEvidence(t, db, &record)
	if _, err := RecordTrustedQualification(context.Background(), db, record); err == nil || !strings.Contains(err.Error(), "provider-idle") {
		t.Fatal("provider-idle claim without claim-specific evidence accepted", err)
	}
}

func TestQualificationEvidenceClassCannotBeSpoofedByName(t *testing.T) {
	db := qualificationDB(t)
	record := testQualificationRecord()
	for n := range record.Evidence {
		if record.Evidence[n].Class == "live" && record.Evidence[n].Name == "provider_idle" {
			record.Evidence[n].Class = "synthetic"
			record.Evidence[n].Name = "live:provider_idle"
		}
	}
	retainQualificationEvidence(t, db, &record)
	if _, err := RecordTrustedQualification(context.Background(), db, record); err == nil || !strings.Contains(err.Error(), "passing live provider_idle") {
		t.Fatal("synthetic evidence name satisfied the live provider-idle claim", err)
	}
}
