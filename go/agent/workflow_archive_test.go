package agent

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	secretpkg "github.com/luoyjx/mini-loop/go/secrets"
)

func TestWorkflowObservationsMatchActualSourceArchive(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-workflow-archive.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Kinds []WorkflowEventKind
		Rows  []struct {
			Name          string
			Raw, Recorded jsonvalue.Value
		}
		Refusals []struct {
			Name, Error, Detail string
			Input               jsonvalue.Value
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Kinds) != 18 || len(fixture.Rows) != 22 || len(fixture.Refusals) != 10 {
		t.Fatal("incomplete corpus")
	}
	for _, kind := range fixture.Kinds {
		if !workflowKind(SessionEventKind(kind)) {
			t.Fatal("missing kind", kind)
		}
	}
	store := newRuntimeStateStore()
	sink := &workflowCaptureSink{}
	registry := secretpkg.New(secretpkg.Config{})
	registry.RegisterValue("WORKFLOW_TOKEN", "workflow-archive-secret")
	parent, err := NewManagedSession(RuntimeConfig{ID: "s", Owner: "owner", Workspace: t.TempDir(), Provider: &FakeProvider{}, Bash: echoExecutor{}, Mode: ModeAuto, MaxRounds: 2, StateStore: store, StateLeaseOwner: "native", Secrets: registry, EventSink: sink})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { parent.core.persistence.release() })
	sub := parent.Subscribe(false)
	defer sub.Close()
	equal := func(label string, got []byte, want jsonvalue.Value) {
		t.Helper()
		value, err := jsonvalue.Decode(string(got))
		if err != nil {
			t.Fatal(label, err)
		}
		if !reflect.DeepEqual(value.Sorted(), want.Sorted()) {
			expected, _ := want.MarshalJSON()
			t.Fatalf("%s: %s; expected %s", label, got, expected)
		}
	}
	for _, row := range fixture.Rows {
		raw, _ := row.Raw.MarshalJSON()
		event, err := DecodeWorkflowObservation(raw)
		if err != nil {
			t.Fatal(row.Name, err)
		}
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(row.Name, err)
		}
		equal(row.Name+" raw", encoded, row.Raw)
		original, ok := event.ObservationPayload()
		if !ok || original.Kind() != jsonvalue.Object {
			t.Fatal(row.Name)
		}
		if _, typed := event.Progress(); typed {
			t.Fatal("archive pretended to have a live progress payload")
		}
		// Replacing a returned Value must not alter the private retained observation.
		replacement := original
		if err := json.Unmarshal([]byte(`{}`), &replacement); err != nil {
			t.Fatal(err)
		}
		unchanged, _ := event.ObservationPayload()
		if !reflect.DeepEqual(original, unchanged) {
			t.Fatal("payload alias")
		}
		if err := (managedWorkflowEvents{parent}).EmitWorkflowEvent(context.Background(), event); err != nil {
			t.Fatal(row.Name, err)
		}
		var captured SessionEventRecord
		select {
		case captured = <-sub.Events():
		case <-time.After(time.Second):
			t.Fatal("missing capture")
		}
		captured.Timestamp = 0
		encoded, err = json.Marshal(captured)
		if err != nil {
			t.Fatal(err)
		}
		equal(row.Name+" recorded", encoded, row.Recorded)
		if strings.Contains(string(encoded), "workflow-archive-secret") {
			t.Fatal("secret retained")
		}
		restored, err := DecodeStoredEvent(encoded)
		if err != nil {
			t.Fatal(row.Name, err)
		}
		if restored.Scope.RunContext.Authority() != AuthorityUntrusted || restored.Scope.RunContext.Allows(CapabilityWorkflowLaunch) {
			t.Fatal("observation gained authority")
		}
		roundtrip, err := json.Marshal(restored)
		if err != nil {
			t.Fatal(err)
		}
		equal(row.Name+" restored", roundtrip, row.Recorded)
	}
	saved, err := store.LoadEvents(context.Background(), parent.ID(), 0, nil)
	if err != nil || len(saved) != len(fixture.Rows) || len(parent.Events()) != len(saved) || len(sink.rows) != len(saved) {
		t.Fatal(len(saved), err)
	}
	for i, row := range saved {
		row.Timestamp = 0
		raw, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		equal("stored", raw, fixture.Rows[i].Recorded)
	}
	for _, row := range fixture.Refusals {
		raw, _ := row.Input.MarshalJSON()
		_, err := DecodeWorkflowObservation(raw)
		if row.Error != "ValueError" || err == nil || err.Error() != row.Detail {
			t.Fatal(row.Name, err, row.Detail)
		}
	}
}
