package agent

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type callerIdentityFixture struct {
	Cases []callerIdentityCase `json:"cases"`
}

type callerIdentityCase struct {
	Mode, Name string
	Expected   jsonvalue.Value `json:"expected_content"`
}

type callerIdentityProvider struct {
	blocks   []protocol.Block
	requests []protocol.ModelRequest
}

func (provider *callerIdentityProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	provider.requests = append(provider.requests, request.Clone())
	if len(provider.requests) == 1 {
		return fakeReply(provider.blocks, protocol.StopToolUse), nil
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
}

type callerIdentityHandler struct {
	authorities []ToolAuthority
}

func (handler *callerIdentityHandler) ExecuteTool(_ context.Context, authority ToolAuthority, _ protocol.ToolInput) (string, error) {
	handler.authorities = append(handler.authorities, authority)
	return "safe handler output", nil
}

func TestCallerIDObservationsNeverReplaceExecutionIdentity(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-caller.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture callerIdentityFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	seenKinds := make(map[jsonvalue.Kind]bool)
	profiles := 0
	for _, row := range fixture.Cases {
		if row.Mode != "direct" || !strings.HasPrefix(row.Name, "tool_use/code-2025-") {
			continue
		}
		profiles++
		encoded, err := json.Marshal(row.Expected)
		if err != nil {
			t.Fatal(err)
		}
		var blocks []protocol.Block
		if err := json.Unmarshal(encoded, &blocks); err != nil {
			t.Fatal(row.Name, err)
		}
		if len(blocks) == 0 {
			t.Fatal(row.Name, "fixture has no blocks")
		}
		use, ok := blocks[0].ToolUse()
		if !ok || use.Caller == nil {
			t.Fatal(row.Name, "fixture has no code caller")
		}
		seenKinds[use.Caller.ToolIDMetadata().Observation().Kind()] = true
		modes := []PermissionMode{ModeAuto}
		if row.Name == "tool_use/code-2025-tool-id-object" {
			modes = append(modes, ModeReadonly)
		}
		for _, mode := range modes {
			t.Run(row.Name+"/"+string(mode), func(t *testing.T) {
				provider := &callerIdentityProvider{blocks: blocks}
				handler := &callerIdentityHandler{}
				definition, err := NewToolDefinition(protocol.ToolBash, ToolTraits{Risk: RiskExec, Capabilities: []Capability{CapabilityProcessExec}}, handler)
				if err != nil {
					t.Fatal(err)
				}
				catalog, err := NewToolCatalog(definition)
				if err != nil {
					t.Fatal(err)
				}
				var calls []ToolCall
				var bindings []ToolAuthority
				gate, err := NewToolGate(catalog, DefaultPermissionPolicy(nil), GateHooks{
					Before: []BeforeHook{beforeHookFunc(func(_ context.Context, authority ToolAuthority, call ToolCall) (BeforeDecision, error) {
						calls = append(calls, call)
						bindings = append(bindings, authority)
						return KeepToolCall(), nil
					})},
				})
				if err != nil {
					t.Fatal(err)
				}
				session, err := NewSessionWithGate("trusted-session", "trusted-owner", provider, gate, mode, "", 2)
				if err != nil {
					t.Fatal(err)
				}
				actor := ActorID("trusted-actor")
				run, err := PeerAgentRunContext("trusted-delegator", PeerRunConfig{ActorID: &actor})
				if err != nil {
					t.Fatal(err)
				}
				wantAction, err := ToolActionID(session.ID(), run, ToolCall{ID: use.ID, Input: use.Input})
				if err != nil {
					t.Fatal(err)
				}
				answer, err := session.RunWithContext(context.Background(), "execute the safe test handler", run)
				if err != nil || answer != "done" {
					t.Fatal(answer, err)
				}
				if len(calls) != 1 || calls[0].ID != use.ID || calls[0].Name() != use.Name {
					t.Fatal("caller metadata changed the dispatched tool identity", calls)
				}
				denied := mode == ModeReadonly
				wantHandlerCalls := 1
				if denied {
					wantHandlerCalls = 0
				}
				if len(handler.authorities) != wantHandlerCalls {
					t.Fatal("caller observation changed permission outcome", len(handler.authorities), wantHandlerCalls)
				}
				for _, authority := range append(bindings, handler.authorities...) {
					if authority.SessionID != session.ID() || authority.OwnerID != "trusted-owner" || authority.Workspace != "" || authority.Mode != mode || authority.ToolUseID != use.ID || authority.ActionID != wantAction {
						t.Fatal("caller metadata changed trusted execution binding", authority)
					}
					if !reflect.DeepEqual(authority.RunContext.Snapshot(), run.Snapshot()) || authority.RunContext.Authority() != AuthorityPeerAgent || authority.RunContext.Allows(CapabilityWorkflowLaunch) {
						t.Fatal("caller metadata changed live run authority")
					}
				}
				uses := eventRecordsOfKind(session.Events(), EventToolUse)
				results := eventRecordsOfKind(session.Events(), EventToolResult)
				if len(uses) != 1 || len(results) != 1 {
					t.Fatal("tool lifecycle count differs", len(uses), len(results))
				}
				useEvent, _ := uses[0].Event.ToolUse()
				resultEvent, _ := results[0].Event.ToolResult()
				if useEvent.ID != use.ID || resultEvent.ID != use.ID || useEvent.ActionID != wantAction || resultEvent.ActionID != wantAction || useEvent.SpanID != resultEvent.SpanID || resultEvent.Denied != denied || resultEvent.Failed {
					t.Fatal("caller metadata changed event identity or outcome", useEvent, resultEvent)
				}
				for _, event := range append(uses, results...) {
					if event.SessionID != session.ID() || !reflect.DeepEqual(event.Scope.RunContext.Snapshot(), run.Snapshot()) {
						t.Fatal("caller metadata changed event authority scope")
					}
				}
				if len(provider.requests) != 2 {
					t.Fatal("unexpected model request count", len(provider.requests))
				}
				for _, history := range [][]protocol.Message{session.Messages(), provider.requests[1].Messages} {
					if err := protocol.ValidateTranscript(history); err != nil {
						t.Fatal("tool results no longer pair with actual use IDs", err)
					}
					assertCallerIdentityHistory(t, history, use, resultEvent.Output)
				}
			})
		}
	}
	if profiles != 8 {
		t.Fatal("Source execution-identity profile inventory changed", profiles)
	}
	for _, kind := range []jsonvalue.Kind{jsonvalue.Null, jsonvalue.Text, jsonvalue.Integer, jsonvalue.Boolean, jsonvalue.Array, jsonvalue.Object} {
		if !seenKinds[kind] {
			t.Fatal("Source fixture lacks required caller ID observation kind", kind)
		}
	}
}

func assertCallerIdentityHistory(t *testing.T, messages []protocol.Message, use protocol.ToolUseBlock, output string) {
	t.Helper()
	uses, results := 0, 0
	for _, message := range messages {
		blocks, _ := message.Content.Blocks()
		for _, block := range blocks {
			if stored, ok := block.ToolUse(); ok {
				uses++
				if stored.ID != use.ID || stored.Caller == nil || stored.Caller.Kind() != use.Caller.Kind() {
					t.Fatal("caller observation changed stored execution ID")
				}
				gotID, wantID := stored.Caller.ToolIDMetadata().Observation(), use.Caller.ToolIDMetadata().Observation()
				if gotID.Kind() != wantID.Kind() || !gotID.PythonEqual(wantID) {
					t.Fatal("execution changed the inert caller observation")
				}
			}
			if result, ok := block.ToolResult(); ok {
				results++
				if result.ToolUseID != use.ID || result.Content != output {
					t.Fatal("tool result uses caller metadata instead of execution identity", result)
				}
			}
		}
	}
	if uses != 1 || results != 1 {
		t.Fatal("unexpected stored tool pairing", uses, results)
	}
}
