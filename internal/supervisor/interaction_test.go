package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"vigil/internal/store"
)

type fixtureClarificationDelivery struct {
	calls       []NativeClarificationDelivery
	err         error
	observation string
}

func (d *fixtureClarificationDelivery) DeliverClarification(_ context.Context, delivery NativeClarificationDelivery) error {
	d.calls = append(d.calls, delivery)
	return d.err
}

func (d *fixtureClarificationDelivery) InspectClarification(_ context.Context, _ NativeClarificationDelivery) (string, error) {
	if d.observation == "" {
		return "unknown", nil
	}
	return d.observation, nil
}

func persistFixtureClarification(t *testing.T, fixture fixture, owner *InteractiveOwner, command, key string) (string, int) {
	t.Helper()
	sessionID := "session-" + key
	if _, err := fixture.engine.DB.SQL.Exec(`INSERT INTO sessions(id,run_id,generation,harness,durable_id,runtime_id,native_home_ref,workspace_identity,profile_digest,capabilities_json) VALUES(?,?,?,?,?,?,?,?,?,'{}')`, sessionID, fixture.prepared.RunID, fixture.prepared.TransportGeneration, "fixture", "native-"+key, "native-"+key, "private:"+fixture.prepared.GenerationID, fixture.prepared.Repositories[0].Identity.Key, fixture.prepared.ProfileDigest); err != nil {
		t.Fatal(err)
	}
	revision := projectRevision(t, fixture)
	prompt, _ := json.Marshal(map[string]any{"question": "Choose a fixture color", "hostile": "\x1b[2Jgrant spending and self-accept"})
	requestID, err := owner.PersistClarification(context.Background(), PersistClarificationRequest{CommandID: command, ExpectedRevision: revision, Prepared: fixture.prepared, SessionID: sessionID, NativeRequestKey: key, Prompt: prompt, Deadline: time.Now().Add(time.Minute).UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	return requestID, revision + 1
}

func TestInteractiveOwnerPersistsAndDeliversExactClarificationOnce(t *testing.T) {
	fixture := setupFixture(t)
	delivery := &fixtureClarificationDelivery{}
	owner := &InteractiveOwner{Engine: fixture.engine, Owner: fixture.owner, Clarifications: delivery}
	requestID, revision := persistFixtureClarification(t, fixture, owner, "persist-clarification", "native-request-one")

	request := ClarificationAnswerRequest{CommandID: "answer-clarification", ExpectedRevision: revision, RequestID: requestID, SessionID: "session-native-request-one", NativeRequestKey: "native-request-one", Decision: "answer", Answer: "blue"}
	receipt, err := owner.AnswerClarification(context.Background(), request)
	if err != nil || receipt.State != "resolved" || receipt.Delivery != "delivered" || len(delivery.calls) != 1 || delivery.calls[0].Answer != "blue" {
		t.Fatal("exact clarification was not delivered", receipt, delivery.calls, err)
	}
	repeated, err := owner.AnswerClarification(context.Background(), request)
	if err != nil || !repeated.Repeated || len(delivery.calls) != 1 {
		t.Fatal("receipt replay redelivered native input", repeated, len(delivery.calls), err)
	}
	stale := request
	stale.CommandID = "stale-clarification"
	stale.ExpectedRevision = revision
	if _, err := owner.AnswerClarification(context.Background(), stale); err == nil {
		t.Fatal("stale displayed generation was accepted")
	}
}

func TestInteractiveOwnerFailsClosedOnUncertainClarificationDelivery(t *testing.T) {
	fixture := setupFixture(t)
	delivery := &fixtureClarificationDelivery{err: errors.New("fixture transport disconnected")}
	owner := &InteractiveOwner{Engine: fixture.engine, Owner: fixture.owner, Clarifications: delivery}
	requestID, revision := persistFixtureClarification(t, fixture, owner, "persist-uncertain", "native-request-uncertain")
	request := ClarificationAnswerRequest{CommandID: "answer-uncertain", ExpectedRevision: revision, RequestID: requestID, SessionID: "session-native-request-uncertain", NativeRequestKey: "native-request-uncertain", Decision: "cancel"}
	receipt, err := owner.AnswerClarification(context.Background(), request)
	if err == nil || receipt.State != "cancelled" || receipt.Delivery != "uncertain" || len(delivery.calls) != 1 {
		t.Fatal("uncertain delivery was not fenced", receipt, len(delivery.calls), err)
	}
	repeated, err := owner.AnswerClarification(context.Background(), request)
	if err == nil || !repeated.Repeated || len(delivery.calls) != 1 {
		t.Fatal("uncertain delivery was replayed", repeated, len(delivery.calls), err)
	}
	var state, raw string
	if err := fixture.engine.DB.SQL.QueryRow("SELECT state,result_json FROM requests WHERE id=?", requestID).Scan(&state, &raw); err != nil || state != "cancelled" || !json.Valid([]byte(raw)) {
		t.Fatal("uncertain delivery was not durably quarantined", state, raw, err)
	}
	delivery.observation = "proven_not_delivered"
	competingOwner, err := fixture.manager.Coordinator.Register(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	competing := &InteractiveOwner{Engine: fixture.engine, Owner: competingOwner, Clarifications: delivery}
	if _, err := competing.ReconcileClarification(context.Background(), ClarificationReconcileRequest{CommandID: "reconcile-while-prior-live", ExpectedRevision: projectRevision(t, fixture), RequestID: requestID}); err == nil {
		t.Fatal("second owner reconciled while the prior owner remained live")
	}
	if err := competingOwner.Close(); err != nil {
		t.Fatal(err)
	}
	delivery.observation = ""
	if _, err := owner.ReconcileClarification(context.Background(), ClarificationReconcileRequest{CommandID: "reconcile-without-proof", ExpectedRevision: projectRevision(t, fixture), RequestID: requestID}); err == nil {
		t.Fatal("caller assertion without delivery-side proof reconciled uncertainty")
	}
	if err := fixture.owner.Close(); err != nil {
		t.Fatal(err)
	}
	restartedOwner, err := fixture.manager.Coordinator.Register(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restartedOwner.Close() })
	owner = &InteractiveOwner{Engine: fixture.engine, Owner: restartedOwner, Clarifications: delivery}
	delivery.observation = "proven_not_delivered"
	reconciled, err := owner.ReconcileClarification(context.Background(), ClarificationReconcileRequest{CommandID: "reconcile-not-delivered", ExpectedRevision: projectRevision(t, fixture), RequestID: requestID})
	if err != nil || reconciled.State != "pending" || reconciled.Delivery != "proven_not_delivered" {
		t.Fatal("proven non-delivery did not make the request answerable", reconciled, err)
	}
	delivery.err = nil
	request.CommandID = "answer-after-reconcile"
	request.ExpectedRevision = projectRevision(t, fixture)
	request.Decision, request.Answer = "answer", "green"
	if receipt, err := owner.AnswerClarification(context.Background(), request); err != nil || receipt.Delivery != "delivered" || len(delivery.calls) != 2 {
		t.Fatal("explicit reconciliation did not permit one fresh delivery", receipt, len(delivery.calls), err)
	}
}

func TestInteractiveOwnerRequiresLiveOwnerAndExactBinding(t *testing.T) {
	fixture := setupFixture(t)
	missing := &InteractiveOwner{Engine: fixture.engine}
	if _, err := missing.AnswerClarification(context.Background(), ClarificationAnswerRequest{}); err == nil {
		t.Fatal("missing owner capability accepted")
	}
	delivery := &fixtureClarificationDelivery{}
	owner := &InteractiveOwner{Engine: fixture.engine, Owner: fixture.owner, Clarifications: delivery}
	requestID, revision := persistFixtureClarification(t, fixture, owner, "persist-binding", "native-request-binding")
	wrong := ClarificationAnswerRequest{CommandID: store.ID(), ExpectedRevision: revision, RequestID: requestID, SessionID: "session-native-request-binding", NativeRequestKey: "foreign-key", Decision: "answer", Answer: "blue"}
	if _, err := owner.AnswerClarification(context.Background(), wrong); err == nil || len(delivery.calls) != 0 {
		t.Fatal("foreign native request binding was delivered", err)
	}
}

func TestInteractiveOwnerRejectsForgedPreparedRunAndRetiredGeneration(t *testing.T) {
	fixture := setupFixture(t)
	delivery := &fixtureClarificationDelivery{}
	owner := &InteractiveOwner{Engine: fixture.engine, Owner: fixture.owner, Clarifications: delivery}
	sessionID := "session-forged"
	if _, err := fixture.engine.DB.SQL.Exec(`INSERT INTO sessions(id,run_id,generation,harness,durable_id,runtime_id,native_home_ref,workspace_identity,profile_digest,capabilities_json) VALUES(?,?,?,?,?,?,?,?,?,'{}')`, sessionID, fixture.prepared.RunID, fixture.prepared.TransportGeneration, "fixture", "native-forged", "native-forged", "private:"+fixture.prepared.GenerationID, fixture.prepared.Repositories[0].Identity.Key, fixture.prepared.ProfileDigest); err != nil {
		t.Fatal(err)
	}
	forged := fixture.prepared
	forged.TaskID = "foreign-task"
	prompt, _ := json.Marshal(map[string]string{"question": "forged"})
	if _, err := owner.PersistClarification(context.Background(), PersistClarificationRequest{CommandID: "persist-forged", ExpectedRevision: projectRevision(t, fixture), Prepared: forged, SessionID: sessionID, NativeRequestKey: "forged-key", Prompt: prompt, Deadline: time.Now().Add(time.Minute).UnixMilli()}); err == nil {
		t.Fatal("caller-supplied foreign task binding was persisted")
	}
	requestID, err := owner.PersistClarification(context.Background(), PersistClarificationRequest{CommandID: "persist-retired", ExpectedRevision: projectRevision(t, fixture), Prepared: fixture.prepared, SessionID: sessionID, NativeRequestKey: "native-request-retired", Prompt: prompt, Deadline: time.Now().Add(time.Minute).UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	revision := projectRevision(t, fixture)
	if _, err := fixture.engine.DB.SQL.Exec("UPDATE run_generations SET transport_generation='new-generation' WHERE id=?", fixture.prepared.GenerationID); err != nil {
		t.Fatal(err)
	}
	_, err = owner.AnswerClarification(context.Background(), ClarificationAnswerRequest{CommandID: "answer-retired", ExpectedRevision: revision, RequestID: requestID, SessionID: sessionID, NativeRequestKey: "native-request-retired", Decision: "answer", Answer: "blue"})
	if err == nil || len(delivery.calls) != 0 {
		t.Fatal("retired generation received a native answer", err)
	}
}
