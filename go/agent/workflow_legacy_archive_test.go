package agent

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	secretpkg "github.com/luoyjx/mini-loop/go/secrets"
)

func TestWorkflowLegacyObservationsMatchActualSourceCapture(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-workflow-legacy-archive.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			Name          string  `json:"name"`
			Raw           string  `json:"raw_wire"`
			Recorded      string  `json:"recorded_wire"`
			Frame         string  `json:"frame_wire"`
			Stored        *string `json:"stored_wire"`
			PersistError  *string `json:"persist_error"`
			UTF8Error     *string `json:"utf8_error"`
			ForeignStatus int     `json:"foreign_status"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Rows) != 5 {
		t.Fatal(err)
	}
	canonical := func(data []byte) string {
		t.Helper()
		value, err := jsonvalue.Decode(string(data))
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := jsonvalue.AppendLegacyDefault(value.Sorted())
		if err != nil {
			t.Fatal(err)
		}
		return string(encoded)
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			registry := secretpkg.New(secretpkg.Config{})
			registry.RegisterValue("WORKFLOW_TOKEN", "legacy-workflow-secret")
			store := newRuntimeStateStore()
			parent, err := NewManagedSession(RuntimeConfig{ID: "s", Owner: "alice", Workspace: t.TempDir(), Provider: &FakeProvider{}, Bash: echoExecutor{}, Mode: ModeAuto, MaxRounds: 2, Secrets: registry, StateStore: store, StateLeaseOwner: "native"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { parent.core.persistence.release() })
			parent.emitFor(RunContext{}, SessionEvent{kind: EventTodo})
			subscription := parent.Subscribe(false)
			defer subscription.Close()
			event, err := DecodeWorkflowObservation([]byte(row.Raw))
			if err != nil {
				t.Fatal(err)
			}
			if err := (managedWorkflowEvents{parent}).EmitWorkflowEvent(context.Background(), event); err != nil {
				t.Fatal(err)
			}
			captured := <-subscription.Events()
			captured.Timestamp = 0
			archive, err := captured.MarshalWorkflowArchiveJSON(nil)
			if err != nil {
				t.Fatal(err)
			}
			if canonical(archive) != canonical([]byte(row.Recorded)) || canonical(archive) != canonical([]byte(row.Frame)) {
				t.Fatalf("capture differs: %s", archive)
			}
			if strings.Contains(string(archive), "legacy-workflow-secret") {
				t.Fatal("unmasked scalar capture")
			}
			restored, err := DecodeStoredEvent([]byte(row.Recorded))
			if err != nil {
				t.Fatal(err)
			}
			if restored.Scope.RunContext.Authority() != AuthorityUntrusted || restored.Scope.RunContext.Allows(CapabilityWorkflowLaunch) {
				t.Fatal("archive gained authority")
			}
			encoded, err := restored.MarshalWorkflowArchiveJSON(nil)
			if err != nil || canonical(encoded) != canonical(archive) {
				t.Fatal(err, string(encoded))
			}
			if row.Stored != nil {
				if canonical([]byte(*row.Stored)) != canonical(archive) || row.PersistError != nil {
					t.Fatal("Source storage projection")
				}
			} else if row.PersistError == nil || *row.PersistError != "UnicodeEncodeError" || row.UTF8Error == nil {
				t.Fatal("missing Source scalar boundary evidence")
			}
			if row.ForeignStatus != 404 {
				t.Fatal("Source owner boundary")
			}
			if row.Name != "masked-key-collision" {
				if _, err := json.Marshal(captured); err == nil {
					t.Fatal("legacy scalars entered standard JSON")
				}
			}
		})
	}
}

func TestWorkflowLegacyBoundaryRefusesMetadataAndUnknownVariants(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-workflow-legacy-archive.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			Recorded string `json:"recorded_wire"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	raw := fixture.Rows[0].Recorded
	for _, mutation := range []string{
		strings.Replace(raw, `"occurred_at": 1.5`, `"occurred_at": NaN`, 1),
		strings.Replace(raw, `"transcript_epoch": 1`, `"transcript_epoch": NaN`, 1),
		strings.Replace(raw, `"type": "workflow_paused"`, `"type": "status"`, 1),
	} {
		if mutation == raw {
			t.Fatal("vacuous mutation")
		}
		if _, err := DecodeStoredEvent([]byte(mutation)); err == nil {
			t.Fatal("accepted non-observational scalar", mutation)
		}
	}
}
