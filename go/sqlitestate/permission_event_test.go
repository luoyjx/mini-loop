package sqlitestate

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/trajectory"
)

type permissionSQLBash struct{ calls atomic.Int32 }

func (bash *permissionSQLBash) ExecuteBash(context.Context, protocol.BashInput) (string, error) {
	bash.calls.Add(1)
	return "unexpected safe handler invocation", nil
}

func (bash *permissionSQLBash) BashFor(context.Context, agent.SessionBinding) (agent.BashExecutor, error) {
	return bash, nil
}

func permissionSQLRecord(t *testing.T, records []agent.SessionEventRecord) agent.SessionEventRecord {
	t.Helper()
	var found agent.SessionEventRecord
	var use, result agent.EventSequence
	count := 0
	for _, record := range records {
		switch record.Event.Kind() {
		case agent.EventToolUse:
			use = record.Sequence
		case agent.EventPermission:
			found, count = record, count+1
		case agent.EventToolResult:
			result = record.Sequence
		}
	}
	if count != 1 || use == 0 || !(use < found.Sequence && found.Sequence < result) {
		t.Fatal("runtime permission publication missing, duplicated, or reordered", count, use, found.Sequence, result)
	}
	return found
}

func TestRuntimePermissionSurvivesSQLiteReopenAndMaskedTrajectory(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "state.db")
	recordingRoot := filepath.Join(root, "recordings")
	store := openStore(t, path)
	recordings, err := trajectory.New(trajectory.Config{Root: recordingRoot, CaptureContent: true})
	if err != nil {
		t.Fatal(err)
	}
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("PERMISSION_RULE", "readonly-mode")
	registry.RegisterValue("PERMISSION_REASON", "risk=exec")
	bash := &permissionSQLBash{}
	manager, err := agent.NewSessionManager(agent.ManagerConfig{
		WorkspaceRoot: filepath.Join(root, "workspaces"),
		Defaults:      agent.SessionDefaults{Model: "permission-model", MaxTokens: 64, MaxRounds: 2, PermissionMode: agent.ModeReadonly},
		Services: agent.ManagerServices{Provider: &agent.FakeProvider{}, BashFactory: bash,
			StateStore: store, Trajectories: recordings, Secrets: registry},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopRestartManager(t, manager) })
	session, err := manager.Create(ctx, agent.CreateSessionRequest{Owner: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := agent.ExplicitHumanRunContext(agent.HumanRunConfig{ApprovedCapabilities: []agent.RunCapability{agent.CapabilityWorkflowLaunch}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.RunWithContext(ctx, "safe readonly probe", run); err != nil {
		t.Fatal(err)
	}
	if bash.calls.Load() != 0 {
		t.Fatal("readonly permission allowed the safe test handler")
	}
	live := permissionSQLRecord(t, session.Events())
	want := agent.PermissionEvent{Decision: agent.PermissionDeny, Rule: secrets.Mask, Tool: protocol.ToolBash, Reason: secrets.Mask}
	if permission, ok := live.Event.Permission(); !ok || permission != want {
		t.Fatal("live permission was not masked", permission, ok)
	}
	if live.Scope.RunContext.MessageID() != run.MessageID() || live.Scope.RunContext.Authority() != agent.AuthorityExplicitHuman || !live.Scope.RunContext.Allows(agent.CapabilityWorkflowLaunch) {
		t.Fatal("live run did not carry the explicit test authority", live.Scope.RunContext.Snapshot())
	}
	if state := session.PersistenceStatus(); state.Error != nil || !state.LeaseConfirmed {
		t.Fatal("permission event degraded persistence", state)
	}
	if info := session.Info(); info.TrajectoryCount != 1 || info.TrajectoryRecordingError != nil {
		t.Fatal("permission recording did not finish", info)
	}
	id := session.ID()
	stopRestartManager(t, manager)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, path)
	records, err := store.LoadEvents(ctx, id, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	restored := permissionSQLRecord(t, records)
	if permission, ok := restored.Event.Permission(); !ok || permission != want || restored.Sequence != live.Sequence {
		t.Fatal("permission payload changed across real database reopen", permission, ok, restored.Sequence, live.Sequence)
	}
	if restored.SessionID != id || restored.Scope.RunContext.MessageID() != run.MessageID() || restored.Scope.RunContext.Authority() != agent.AuthorityUntrusted || restored.Scope.RunContext.Allows(agent.CapabilityWorkflowLaunch) || len(restored.Scope.RunContext.Snapshot().ApprovedCapabilities) != 0 {
		t.Fatal("historical permission changed identity or restored authority", restored)
	}
	rows, err := store.conn.QueryContext(ctx, "SELECT payload FROM events WHERE session_id=? ORDER BY ordinal", id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(payload, "readonly-mode") || strings.Contains(payload, "risk=exec") {
			t.Fatal("permission secret reached physical SQLite payload", payload)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(recordingRoot, "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatal("expected one actual trajectory file", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("readonly-mode")) || bytes.Contains(data, []byte("risk=exec")) {
		t.Fatal("permission secret reached JSONL")
	}
	permissionLines := 0
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var header struct {
			Type agent.SessionEventKind `json:"type"`
		}
		if err := json.Unmarshal(line, &header); err != nil {
			t.Fatal(err)
		}
		if header.Type != agent.EventPermission {
			continue
		}
		permissionLines++
		record, err := agent.DecodeStoredEvent(line)
		if err != nil {
			t.Fatal(err)
		}
		permission, ok := record.Event.Permission()
		if !ok || permission != want || record.Scope.RunContext.MessageID() != run.MessageID() {
			t.Fatal("JSONL lost the masked permission or run identity", record)
		}
	}
	if permissionLines != 1 {
		t.Fatal("runtime trajectory did not contain exactly one permission", permissionLines)
	}
}
