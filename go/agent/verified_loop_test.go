package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/shell"
	"github.com/luoyjx/mini-loop/go/verifiedloop"
)

func verifiedManaged(t *testing.T, provider Provider, store StateStore) *ManagedSession {
	t.Helper()
	root := t.TempDir()
	executor, err := shell.New(shell.Config{Workspace: root, Timeout: time.Second, Secrets: runtimeSecrets()})
	if err != nil {
		t.Fatal(err)
	}
	config := runtimeConfig(root, provider)
	config.Bash, config.Mode, config.Secrets = executor, ModeAuto, runtimeSecrets()
	config.StateStore = store
	if store != nil {
		config.StateLeaseOwner = "verified-process"
	}
	session, err := NewManagedSession(config)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func TestManagedVerifiedRealWorkerGateCommandAndRecording(t *testing.T) {
	provider := &childContractProvider{role: RoleWorker}
	store := newRuntimeStateStore()
	session := verifiedManaged(t, provider, store)
	hook := &authorityCapture{}
	session.core.gate.before = append(session.core.gate.before, hook)
	run, _ := ExplicitHumanRunContext(HumanRunConfig{ApprovedCapabilities: []RunCapability{CapabilityPersonalSkillCaptureSource}})
	outcome, err := session.RunVerifiedWithContext(context.Background(), "write proof "+runtimeCanary, VerifiedRunOptions{AcceptanceCommand: "test -f made.txt", CheckInstruments: true}, run)
	if err != nil || outcome.Status != verifiedloop.TaskComplete || outcome.Rounds != 1 || outcome.Summary != "child done" {
		t.Fatal(outcome, err)
	}
	if data, err := os.ReadFile(filepath.Join(session.core.workspace, "made.txt")); err != nil || string(data) != "worker file" {
		t.Fatal(string(data), err)
	}
	if len(hook.authorities) != 1 {
		t.Fatal(hook.authorities)
	}
	authority := hook.authorities[0]
	if authority.OwnerID != session.Owner() || authority.Workspace != session.core.workspace || authority.RunContext.Authority() != AuthorityPeerAgent || authority.RunContext.Allows(CapabilityPersonalSkillCaptureSource) {
		t.Fatal(authority)
	}
	if len(provider.requests) != 2 || provider.requests[0].System == nil || !strings.Contains(*provider.requests[0].System, "worker subagent") {
		t.Fatal(provider.requests)
	}
	if len(session.Messages()) != 0 {
		t.Fatal("worker transcript leaked into parent")
	}
	info := session.Info()
	if info.Busy || info.Status != StatusIdle || info.RunCount != 1 {
		t.Fatal(info)
	}
	var order []SessionEventKind
	for _, record := range session.Events() {
		switch record.Event.Kind() {
		case EventVerifiedRound, EventSubagentStart, EventSubagentEnd, EventVerifiedReceipt, EventVerifiedCheckpoint, EventDone:
			order = append(order, record.Event.Kind())
		}
		if round, ok := record.Event.VerifiedRound(); ok {
			if strings.Contains(round.Objective, runtimeCanary) || record.Scope.RunContext.MessageID() != run.MessageID() {
				t.Fatal(record)
			}
		}
		if receipt, ok := record.Event.VerifiedReceipt(); ok {
			*receipt.ExitCode = 42
			again, _ := record.Event.VerifiedReceipt()
			if *again.ExitCode != 0 {
				t.Fatal("mutable receipt event")
			}
		}
	}
	want := []SessionEventKind{EventVerifiedRound, EventSubagentStart, EventSubagentEnd, EventVerifiedReceipt, EventVerifiedCheckpoint, EventDone}
	if !reflect.DeepEqual(order, want) {
		t.Fatal(order)
	}
	stored, err := store.LoadEvents(context.Background(), session.ID(), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	verifiedCount := 0
	for _, record := range stored {
		if record.Event.kind != EventVerifiedRound && record.Event.kind != EventVerifiedReceipt && record.Event.kind != EventVerifiedCheckpoint {
			continue
		}
		verifiedCount++
		data, err := json.Marshal(record)
		if err != nil || strings.Contains(string(data), runtimeCanary) {
			t.Fatal(string(data), err)
		}
		decoded, err := DecodeStoredEvent(data)
		if err != nil || decoded.Event.Kind() != record.Event.Kind() {
			t.Fatal(decoded, err)
		}
	}
	if verifiedCount != 3 {
		t.Fatal(verifiedCount)
	}
}

type verifiedDenyHook struct{}

func (verifiedDenyHook) BeforeTool(context.Context, ToolAuthority, ToolCall) (BeforeDecision, error) {
	return DenyToolCall("operator denial"), nil
}

func TestManagedVerifiedDeniedWorkerCannotVerifyWithProse(t *testing.T) {
	session := verifiedManaged(t, &childContractProvider{role: RoleWorker}, nil)
	session.core.gate.before = append(session.core.gate.before, verifiedDenyHook{})
	run, _ := DefaultRunContext()
	maximum := int64(1)
	outcome, err := session.RunVerifiedWithContext(context.Background(), "write proof", VerifiedRunOptions{AcceptanceCommand: "test -f made.txt", MaxRounds: &maximum}, run)
	if err != nil || outcome.Status != verifiedloop.TaskUnverified || len(outcome.Receipts) != 1 {
		t.Fatal(outcome, err)
	}
	if _, err := os.Stat(filepath.Join(session.core.workspace, "made.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(eventRecordsOfKind(session.Events(), EventToolResult)) != 1 {
		t.Fatal("worker gate skipped")
	}
}

func TestManagedVerifiedCancelAdmissionAndLeaseLoss(t *testing.T) {
	t.Run("cancel and reuse", func(t *testing.T) {
		provider := &parkedProvider{started: make(chan struct{})}
		session := verifiedManaged(t, provider, nil)
		run, _ := DefaultRunContext()
		done := make(chan error, 1)
		go func() {
			outcome, err := session.RunVerifiedWithContext(context.Background(), "wait", VerifiedRunOptions{AcceptanceCommand: "touch forbidden"}, run)
			if outcome.Status != "" {
				err = errors.New("cancel returned an outcome")
			}
			done <- err
		}()
		<-provider.started
		if _, err := session.TryRunWithContext(context.Background(), "competing", run); !errors.Is(err, ErrSessionBusy) {
			t.Fatal(err)
		}
		if ok, err := session.Cancel(context.Background(), "operator"); !ok || err != nil {
			t.Fatal(ok, err)
		}
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(session.core.workspace, "forbidden")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if len(eventRecordsOfKind(session.Events(), EventDone)) != 0 || len(eventRecordsOfKind(session.Events(), EventVerifiedReceipt)) != 0 {
			t.Fatal("cancel manufactured success")
		}
		if output, err := session.Run(context.Background(), "next"); err != nil || output != "next" {
			t.Fatal(output, err)
		}
	})
	for _, boundary := range []SessionEventKind{EventVerifiedRound, EventSubagentEnd, EventVerifiedCheckpoint} {
		t.Run("lease loss after "+string(boundary), func(t *testing.T) {
			store := newRuntimeStateStore()
			provider := &childContractProvider{role: RoleWorker}
			session := verifiedManaged(t, provider, store)
			store.onEvent = func(record SessionEventRecord) {
				if record.Event.Kind() == boundary {
					store.mu.Lock()
					store.holders[session.ID()] = "foreign"
					store.mu.Unlock()
				}
			}
			run, _ := DefaultRunContext()
			outcome, err := session.RunVerifiedWithContext(context.Background(), "write", VerifiedRunOptions{AcceptanceCommand: "touch forbidden"}, run)
			if !errors.Is(err, ErrSessionLeaseLost) || outcome.Status != "" {
				t.Fatal(outcome, err, provider.requests)
			}
			if boundary == EventVerifiedRound && len(provider.requests) != 0 {
				t.Fatal("worker ran after lease loss")
			}
			if len(eventRecordsOfKind(session.Events(), EventDone)) != 0 {
				t.Fatal("published successful terminal after lease loss")
			}
			if _, err := os.Stat(filepath.Join(session.core.workspace, "forbidden")); !errors.Is(err, os.ErrNotExist) && boundary != EventVerifiedCheckpoint {
				t.Fatal(err)
			}
		})
	}
	t.Run("string executor cannot attest exit", func(t *testing.T) {
		session := managedForTest(t, resourceProvider{})
		run, _ := DefaultRunContext()
		outcome, err := session.RunVerifiedWithContext(context.Background(), "write", VerifiedRunOptions{AcceptanceCommand: "true"}, run)
		if err == nil || outcome.Status != "" {
			t.Fatal(outcome, err)
		}
	})
}

func TestVerifiedSessionEventsMatchSourceFixtures(t *testing.T) {
	var fixture struct {
		Cases []struct {
			Name   string            `json:"name"`
			Events []json.RawMessage `json:"events"`
		} `json:"cases"`
	}
	data, err := os.ReadFile("../testdata/python-verified-service.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 22 {
		t.Fatal(len(fixture.Cases))
	}
	for _, recipe := range fixture.Cases {
		t.Run(recipe.Name, func(t *testing.T) {
			for _, raw := range recipe.Events {
				stored := append([]byte(`{"seq":1,"transcript_epoch":1,"session":"session",`), raw[1:]...)
				record, err := DecodeStoredEvent(stored)
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				// Generic objects are transient wire comparisons only, never runtime state.
				var want, got map[string]json.RawMessage
				json.Unmarshal(raw, &want)
				json.Unmarshal(encoded, &got)
				for _, key := range []string{"seq", "ts", "session", "transcript_epoch"} {
					delete(got, key)
				}
				w, _ := json.Marshal(want)
				g, _ := json.Marshal(got)
				if string(w) != string(g) {
					t.Fatalf("source event %s != %s", w, g)
				}
			}
		})
	}
	for _, bad := range []string{`{"type":"verified_round","round":0}`, `{"type":"verified_receipt","round":1,"verdict":"bogus","integrity":"clean"}`, `{"type":"verified_checkpoint","state_revision":-1,"status":"complete"}`} {
		stored := append([]byte(`{"seq":1,"transcript_epoch":1,"session":"session",`), []byte(bad)[1:]...)
		if _, err := DecodeStoredEvent(stored); err == nil {
			t.Fatal(bad)
		}
	}
}
