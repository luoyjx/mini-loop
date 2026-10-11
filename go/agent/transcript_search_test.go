package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/sessionquery"
)

func searchCall(query string) protocol.Block {
	return protocol.NewToolUse("search", protocol.TranscriptSearchToolInput(protocol.TranscriptSearchInput{Query: query}))
}
func searchResult(t *testing.T, session *Session) protocol.ToolResultBlock {
	t.Helper()
	for _, message := range session.Messages() {
		blocks, _ := message.Content.Blocks()
		for _, block := range blocks {
			if result, ok := block.ToolResult(); ok && result.ToolUseID == "search" {
				return result
			}
		}
	}
	t.Fatal("missing search result")
	return protocol.ToolResultBlock{}
}

func TestTranscriptSearchBareAndManagedNullMatchSource(t *testing.T) {
	var source struct {
		Readonly     bool
		ParallelSafe bool `json:"parallel_safe"`
		Risk         ToolRisk
		Surfaces     []struct{ Name, Output string }
	}
	data, err := os.ReadFile("../testdata/python-transcript-search.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	cfg := runtimeConfig(t.TempDir(), resourceProvider{[]protocol.Block{searchCall("needle")}})
	cfg.SessionQueryTools, cfg.Mode = true, ModeReadonly
	session, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	definition, ok := session.gate.catalog.Lookup(protocol.ToolTranscriptSearch)
	if !ok || definition.readonly != source.Readonly || definition.parallelSafe != source.ParallelSafe || definition.risk != source.Risk {
		t.Fatal("Source traits differ")
	}
	if _, err := session.Run(context.Background(), "search"); err != nil {
		t.Fatal(err)
	}
	if result := searchResult(t, session); result.IsError || result.Content != source.Surfaces[0].Output {
		t.Fatal(result)
	}
	cfg.SessionQueryTools = false
	plain, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plain.gate.catalog.Lookup(protocol.ToolTranscriptSearch); ok {
		t.Fatal("default search installation")
	}
	cfg.SessionQueryTools = true
	cfg.ToolSelection = SelectTools(protocol.ToolReadFile)
	selected, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := selected.gate.catalog.Lookup(protocol.ToolTranscriptSearch); ok {
		t.Fatal("selection widened")
	}
	mcfg := managerTestConfig(t.TempDir(), resourceProvider{[]protocol.Block{searchCall("needle")}})
	mcfg.Services.SessionQueryTools = true
	manager := makeManager(t, mcfg)
	managed := createManaged(t, manager, CreateSessionRequest{Owner: "owner", PermissionMode: ModeReadonly})
	if _, err := managed.Run(context.Background(), "search"); err != nil {
		t.Fatal(err)
	}
	if result := searchResult(t, managed.core); result.IsError || result.Content != source.Surfaces[3].Output {
		t.Fatal(result)
	}
}

func TestManagedTranscriptSearchReadsOwnSupersededHistory(t *testing.T) {
	store := newRuntimeStateStore()
	cfg := managerTestConfig(t.TempDir(), stateProviderFunc(func(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
		for _, message := range request.Messages {
			blocks, _ := message.Content.Blocks()
			for _, block := range blocks {
				if result, ok := block.ToolResult(); ok && result.ToolUseID == "search" {
					return stateFinal(), nil
				}
			}
		}
		return fakeReply([]protocol.Block{searchCall("needle")}, protocol.StopToolUse), nil
	}))
	cfg.Services.StateStore, cfg.Services.SessionQueryTools = store, true
	manager := makeManager(t, cfg)
	alice := createManaged(t, manager, CreateSessionRequest{Owner: "alice", PermissionMode: ModeReadonly})
	bob := createManaged(t, manager, CreateSessionRequest{Owner: "bob"})
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, seed := range []struct {
		id    SessionID
		epoch TranscriptEpoch
		text  string
	}{
		{alice.ID(), 1, "alice old needle"}, {alice.ID(), 3, "new noise"}, {bob.ID(), 1, "bob private needle"},
	} {
		if _, err := store.AppendMessages(context.Background(), seed.id, []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent(seed.text)}}, seed.epoch); err != nil {
			t.Fatal(err)
		}
	}
	restoredManager := makeManager(t, cfg)
	restored, err := restoredManager.RestoreSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, session := range restored {
		if session.ID() == alice.ID() {
			alice = session
			found = true
		}
	}
	if !found {
		t.Fatal("own stored session was not restored")
	}
	if _, err := alice.Run(context.Background(), "search history"); err != nil {
		t.Fatal(err)
	}
	result := searchResult(t, alice.core)
	if result.IsError || !strings.Contains(result.Content, "epoch 1 #0 (user): ...alice old needle...") || strings.Contains(result.Content, "bob private") {
		t.Fatal(result)
	}
}

func TestTranscriptSearchGuardAndSelectedChildCannotReadParentStore(t *testing.T) {
	reads := 0
	store := &transcriptReadStore{runtimeStateStore: newRuntimeStateStore(), epochRead: func(context.Context, SessionID) (TranscriptEpoch, error) { reads++; return 0, nil }}
	cfg := runtimeConfig(t.TempDir(), resourceProvider{[]protocol.Block{searchCall("needle")}})
	cfg.SessionQueryTools = true
	cfg.transcriptSearch = transcriptSearchReader{store: store}
	cfg.Hooks.Guards = []GuardHook{guardHookFunc(func(context.Context, ToolAuthority, ToolCall) (string, bool, error) { return "blocked", true, nil })}
	session, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(context.Background(), "search"); err != nil {
		t.Fatal(err)
	}
	if reads != 0 || searchResult(t, session).Content != "blocked" {
		t.Fatal("guard bypass")
	}
	cfg.Hooks = GateHooks{}
	cfg.RoleToolPolicy = allRoleTools{}
	seen := ""
	cfg.Provider = stateProviderFunc(func(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
		for _, message := range request.Messages {
			blocks, _ := message.Content.Blocks()
			for _, block := range blocks {
				if result, ok := block.ToolResult(); ok {
					seen = result.Content
					return stateFinal(), nil
				}
			}
		}
		return fakeReply([]protocol.Block{searchCall("needle")}, protocol.StopToolUse), nil
	})
	session, err = NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Delegate(context.Background(), "search", RoleExplore); err != nil {
		t.Fatal(err)
	}
	if reads != 0 || seen != "Error: no durable transcript on this surface" {
		t.Fatal("child inherited parent state", seen, reads)
	}
	child, err := DefaultRoleToolPolicy().Select(RoleExplore, session.gate.catalog)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := child.Lookup(protocol.ToolTranscriptSearch); ok {
		t.Fatal("default role widened")
	}
	readFailure := errors.New("read failure")
	store.epochRead = func(context.Context, SessionID) (TranscriptEpoch, error) { return 0, readFailure }
	output, err := sessionquery.ToolSearch(context.Background(), transcriptSearchReader{store: store}, sessionquery.SessionID(cfg.ID), "needle")
	if output != "" || !errors.Is(err, readFailure) {
		t.Fatal(output, err)
	}
}
