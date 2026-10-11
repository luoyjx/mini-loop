package httpapi

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/sqlitestate"
)

type failingPostureEvents struct{ *sqlitestate.Store }

func (s failingPostureEvents) AppendEvent(context.Context, agent.SessionID, agent.SessionEventRecord) (agent.EventOrdinal, error) {
	return 0, errors.New("fixture event failure fixture-private-value")
}

type failingPostureRegistry struct{ *secrets.Registry }

func (r failingPostureRegistry) Names() []secrets.Name { panic("fixture-private-value") }

func TestPublicHealthPostureReportsSQLiteFaultWithMaskedDetail(t *testing.T) {
	store, err := sqlitestate.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("FIXTURE_KEY", "fixture-private-value")
	manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: t.TempDir(), Services: agent.ManagerServices{Provider: doneProvider{}, StateStore: failingPostureEvents{store}, Secrets: registry}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Stop(context.Background()) })
	auth, err := NewTokenAuth([]TokenBinding{{Token: "fixture-token", Principal: "owner"}})
	if err != nil {
		t.Fatal(err)
	}
	server := testServer(t, Config{Manager: manager, Auth: auth})
	response := request(server, "GET", "/healthz", "", "")
	health := decode[HealthResponse](t, response.Body.Bytes())
	if response.Code != 200 || !health.Posture.Authenticated || health.Posture.StateStore != "failingPostureEvents" || health.Posture.ActionJournal != "StoredActionJournal" || health.Posture.StateStoreError != nil {
		t.Fatal(response.Code, health)
	}
	created := request(server, "POST", "/sessions", "{}", "fixture-token")
	if created.Code != 200 {
		t.Fatal(created.Code, created.Body.String())
	}
	id := decode[SessionInfo](t, created.Body.Bytes()).ID
	turn := request(server, "POST", "/sessions/"+string(id)+"/messages", `{"message":"hello"}`, "fixture-token")
	if turn.Code != 200 {
		t.Fatal(turn.Code, turn.Body.String())
	}
	response = request(server, "GET", "/healthz", "", "")
	health = decode[HealthResponse](t, response.Body.Bytes())
	if response.Code != 200 || health.Posture.StateStoreError == nil || !strings.Contains(*health.Posture.StateStoreError, "fixture event failure <secret-hidden>") || strings.Contains(response.Body.String(), "fixture-private-value") || strings.Contains(response.Body.String(), "FIXTURE_KEY") {
		t.Fatal(response.Code, response.Body.String())
	}
}

func TestPublicHealthPostureFailureReturnsPrivateError(t *testing.T) {
	manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: t.TempDir(), Services: agent.ManagerServices{Provider: doneProvider{}, Secrets: failingPostureRegistry{secrets.New(secrets.Config{})}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Stop(context.Background()) })
	server := testServer(t, Config{Manager: manager})
	response := request(server, "GET", "/healthz", "", "")
	if response.Code != 500 || response.Body.String() != `{"detail":"runtime posture observation failed"}` {
		t.Fatal(response.Code, response.Body.String())
	}
}

// Preserve every Source field, including future unknown fields. Only these two
// known null-wrapper labels differ; NullStateStore remains exact for remote audit.
func sourceNullPostureLabels(t *testing.T, data []byte) []byte {
	t.Helper()
	root, err := jsonvalue.Decode(string(data))
	if err != nil || root.Kind() != jsonvalue.Object {
		t.Fatal("invalid Source health", err)
	}
	var fields []jsonvalue.Field
	for _, key := range root.Keys() {
		value, _ := root.Lookup(key)
		if key == "posture" {
			if value.Kind() != jsonvalue.Object {
				t.Fatal("invalid Source posture")
			}
			var nested []jsonvalue.Field
			for _, name := range value.Keys() {
				entry, _ := value.Lookup(name)
				text, _ := entry.Text()
				if (name == "secrets" && text == "NullSecretRegistry") || (name == "sandbox" && text == "NullSandbox") {
					entry = jsonvalue.TextValue("None")
				}
				nested = append(nested, jsonvalue.Field{Name: name, Value: entry})
			}
			value = jsonvalue.ObjectValue(nested)
		}
		fields = append(fields, jsonvalue.Field{Name: key, Value: value})
	}
	encoded, err := jsonvalue.ObjectValue(fields).MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestSourcePostureNullLabelsKeepUnknownFieldsAndNonNullComponents(t *testing.T) {
	source := `{"future":[1,"x"],"posture":{"state_store":"NullStateStore","secrets":"NullSecretRegistry","sandbox":"NullSandbox","future":true}}`
	want := `{"future":[1,"x"],"posture":{"state_store":"NullStateStore","secrets":"None","sandbox":"None","future":true}}`
	if got := string(sourceNullPostureLabels(t, []byte(source))); got != want {
		t.Fatal(got)
	}
	source = `{"posture":{"state_store":"SQLiteStateStore","secrets":"SecretRegistry","sandbox":"SeatbeltSandbox","future":null}}`
	if got := string(sourceNullPostureLabels(t, []byte(source))); got != source {
		t.Fatal(got)
	}
}
