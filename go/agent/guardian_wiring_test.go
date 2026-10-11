package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type guardianWiringCase struct {
	Name, Answer, Missing                                                  string
	Enabled, Human, Fault, Cancel, Hostile, Wide, Repeat, Remember, Masked bool
	ConstructorFault                                                       bool `json:"constructor_fault"`
	Outcomes                                                               []jsonvalue.Value
	Parked                                                                 []bool
	Writes                                                                 []ApprovalRecord
	Events                                                                 []approvalProjection
	Problems, Prompts, Inspections                                         []string
	Catalogues                                                             [][]protocol.ToolName
}

type guardianWiringProvider struct {
	row         guardianWiringCase
	requests    []protocol.ModelRequest
	inspections []string
}

func (p *guardianWiringProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	p.requests = append(p.requests, request)
	if p.row.Cancel {
		return protocol.ModelReply{}, context.Canceled
	}
	if p.row.Fault {
		return protocol.ModelReply{}, errors.New(p.row.Answer)
	}
	var results []string
	for _, message := range request.Messages {
		blocks, _ := message.Content.Blocks()
		for _, block := range blocks {
			if result, ok := block.ToolResult(); ok {
				results = append(results, result.Content)
			}
		}
	}
	if (p.row.Hostile || p.row.Repeat) && len(results) == 0 {
		name, payload := protocol.ToolReadFile, `{"path":"marker.txt"}`
		if p.row.Hostile {
			name, payload = protocol.ToolWriteFile, `{"path":"planted.txt","content":"x"}`
		}
		input, err := protocol.DecodeToolInput(name, []byte(payload))
		if err != nil {
			return protocol.ModelReply{}, err
		}
		return fakeReply([]protocol.Block{protocol.NewToolUse("inspect", input)}, protocol.StopToolUse), nil
	}
	p.inspections = append(p.inspections, results...)
	return fakeReply([]protocol.Block{protocol.NewTextBlock(p.row.Answer)}, protocol.StopEndTurn), nil
}

type guardianFaultPolicy struct{}

func (guardianFaultPolicy) Select(AgentRole, *ToolCatalog) (*ToolCatalog, error) {
	return nil, errors.New("fixture constructor fault")
}

func TestManagedGuardianApprovalMatchesSourceWiring(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-guardian-wiring.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Cases []guardianWiringCase }
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 17 {
		t.Fatal("Source wiring inventory changed")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			provider := &guardianWiringProvider{row: row}
			config := managerTestConfig(t.TempDir(), provider)
			config.Services.GuardianEnabled = row.Enabled
			config.Defaults.PermissionMode = ModeInteractive
			config.Defaults.SubagentMaxRounds = 3
			config.Services.BashFactory = bashFactoryFunc(func(context.Context, SessionBinding) (BashExecutor, error) { return echoExecutor{}, nil })
			if row.Wide {
				config.Services.RoleToolPolicy = guardianWidePolicy{}
			}
			if row.ConstructorFault {
				config.Services.RoleToolPolicy = guardianFaultPolicy{}
			}
			if row.Masked {
				config.Services.Secrets = literalApprovalRedactor{"fixture-credential"}
			}
			manager := makeManager(t, config)
			parent := createManaged(t, manager, CreateSessionRequest{Owner: "owner"}).core
			broker := manager.Approvals()
			store := &approvalStoreSpy{}
			broker.store = store
			// Match the Source probe's explicit rule while retaining the real gate,
			// broker, manager-built runtime, fresh auditor and model transport.
			parent.gate.policy.rules = []PermissionRule{{name: "fixture-rule", message: "approval required", action: RuleAsk,
				matches: func(_ ToolAuthority, call ToolCall, _ ToolRisk, _ bool) bool { return call.Name() == protocol.ToolBash }}}
			parent.messages = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("private parent history")}}
			run, err := DefaultRunContext()
			if err != nil {
				t.Fatal(err)
			}
			command := "git status --short"
			if row.Masked {
				command = "git status fixture-credential"
			}
			input := protocol.BashToolInput(protocol.BashInput{Command: command})
			var outcomes []jsonvalue.Value
			var parked []bool
			for index := range row.Outcomes {
				if err := os.WriteFile(filepath.Join(parent.workspace, "marker.txt"), []byte("current-"+string(rune('0'+index))+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if row.Missing != "" {
					// A missing/foreign parent cannot obtain a matching broker surface.
					surface := parent.gate.policy.approver.(*ApprovalSurface)
					allowed, err := surface.Approve(context.Background(), ApprovalRequest{Authority: ToolAuthority{}, Call: ToolCall{ID: "action", Input: input}})
					if err != nil || allowed {
						t.Fatal("unbound caller approved", err)
					}
					outcomes = append(outcomes, jsonvalue.BoolValue(false))
					parked = append(parked, false)
					continue
				}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				done := make(chan toolAttempt, 1)
				go func() {
					parent.mu.Lock()
					defer parent.mu.Unlock()
					outcome, err := parent.dispatchTool(ctx, run, protocol.ToolUseBlock{ID: "action", Name: protocol.ToolBash, Input: input})
					done <- toolAttempt{outcome: outcome, err: err}
				}()
				var result toolAttempt
				wasParked := false
				waiting := true
				for waiting {
					select {
					case result = <-done:
						waiting = false
					case <-ctx.Done():
						cancel()
						t.Fatal("Guardian dispatch did not finish", ctx.Err())
					case <-time.After(time.Millisecond):
						pending := broker.List(parent.id)
						if len(pending) > 0 {
							wasParked = true
							if !broker.Resolve(pending[0].ApprovalID, ApprovalResolution{SessionID: parent.id, Allowed: row.Human, Remember: row.Remember}) {
								t.Fatal("human resolution failed")
							}
						}
					}
				}
				cancel()
				parked = append(parked, wasParked)
				if row.Cancel {
					if !errors.Is(result.err, context.Canceled) {
						t.Fatal(result.err)
					}
					outcomes = append(outcomes, jsonvalue.TextValue("cancelled"))
				} else {
					if result.err != nil {
						t.Fatal(result.err)
					}
					outcomes = append(outcomes, jsonvalue.BoolValue(!result.outcome.Denied))
				}
			}
			if !reflect.DeepEqual(outcomes, row.Outcomes) || !slices.Equal(parked, row.Parked) {
				t.Fatal("approval outcome differs", outcomes, parked)
			}
			var prompts []string
			var catalogues [][]protocol.ToolName
			for _, request := range provider.requests {
				prompt, _ := request.Messages[0].Content.Plain()
				prompts = append(prompts, prompt)
				var names []protocol.ToolName
				for _, tool := range request.Tools {
					names = append(names, tool.Name)
				}
				catalogues = append(catalogues, names)
			}
			if !slices.Equal(prompts, row.Prompts) || (len(catalogues) > 0 && !reflect.DeepEqual(catalogues, row.Catalogues)) || !slices.Equal(provider.inspections, row.Inspections) {
				t.Fatal("review prompt/catalogue/inspection differs", prompts, catalogues, provider.inspections)
			}
			var events []approvalProjection
			for _, record := range parent.events.snapshot() {
				if event, ok := record.Event.Approval(); ok {
					projection := projectApproval(event)
					if projection.SessionID != "" {
						projection.SessionID = "parent"
					}
					events = append(events, projection)
				}
			}
			writes := store.snapshot()
			for index := range writes {
				writes[index].SessionID = "parent"
			}
			normalizeApprovalEvidence(writes, events)
			if !reflect.DeepEqual(writes, row.Writes) || len(events) != len(row.Events) || (len(events) > 0 && !reflect.DeepEqual(events, row.Events)) {
				t.Fatalf("receipt differs\nwrites: %#v\nwant: %#v\nevents: %#v\nwant: %#v", writes, row.Writes, events, row.Events)
			}
			if len(broker.Problems()) != len(row.Problems) || len(broker.List(parent.id)) != 0 {
				t.Fatal("broker fault/pending state differs", broker.Problems())
			}
			if len(parent.messages) != 1 {
				t.Fatal("review changed parent history")
			}
			if _, err := os.Stat(filepath.Join(parent.workspace, "planted.txt")); !os.IsNotExist(err) {
				t.Fatal("review wrote to workspace", err)
			}
		})
	}
}

// Stateless shared provider: each auditor must read its own parent's workspace.
type guardianOwnedLoopProvider struct{}

func (guardianOwnedLoopProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	var result *protocol.ToolResultBlock
	for _, message := range request.Messages {
		blocks, _ := message.Content.Blocks()
		for _, block := range blocks {
			if value, ok := block.ToolResult(); ok {
				result = &value
			}
		}
	}
	reviewing := request.System != nil && *request.System == guardianSystem
	if result != nil {
		return fakeReply([]protocol.Block{protocol.NewTextBlock(result.Content)}, protocol.StopEndTurn), nil
	}
	input := protocol.BashToolInput(protocol.BashInput{Command: "git status --short"})
	if reviewing {
		input = protocol.ReadFileToolInput(protocol.ReadFileInput{Path: "marker.txt"})
	}
	return fakeReply([]protocol.Block{protocol.NewToolUse("work", input)}, protocol.StopToolUse), nil
}

func TestGuardianRunsInsideConcurrentOwnedTurns(t *testing.T) {
	config := managerTestConfig(t.TempDir(), guardianOwnedLoopProvider{})
	config.Services.GuardianEnabled = true
	config.Services.BashFactory = bashFactoryFunc(func(context.Context, SessionBinding) (BashExecutor, error) { return echoExecutor{}, nil })
	config.Defaults.PermissionMode = ModeInteractive
	config.Defaults.MaxRounds, config.Defaults.SubagentMaxRounds = 3, 3
	manager := makeManager(t, config)
	parents := []*ManagedSession{
		createManaged(t, manager, CreateSessionRequest{Owner: "allow-owner"}),
		createManaged(t, manager, CreateSessionRequest{Owner: "deny-owner"}),
	}
	for i, parent := range parents {
		parent.core.gate.policy.rules = []PermissionRule{{name: "fixture-rule", message: "approval required", action: RuleAsk,
			matches: func(_ ToolAuthority, call ToolCall, _ ToolRisk, _ bool) bool { return call.Name() == protocol.ToolBash }}}
		marker := "ALLOW current owner"
		if i == 1 {
			marker = "DENY current owner"
		}
		if err := os.WriteFile(filepath.Join(parent.core.workspace, "marker.txt"), []byte(marker), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	type result struct {
		index  int
		output string
		err    error
	}
	done := make(chan result, 2)
	for i, parent := range parents {
		go func() { output, err := parent.Run(ctx, "inspect current owner"); done <- result{i, output, err} }()
	}
	for range parents {
		value := <-done
		if value.err != nil {
			t.Fatal(value.err)
		}
		denied := strings.Contains(value.output, "Permission denied")
		if denied != (value.index == 1) {
			t.Fatal("review borrowed another parent's workspace", value)
		}
	}
	for i, parent := range parents {
		var verdicts []ReviewVerdict
		for _, record := range parent.core.events.snapshot() {
			if event, ok := record.Event.Approval(); ok {
				if _, _, verdict, ok := event.AutoReviewed(); ok {
					verdicts = append(verdicts, verdict)
				}
			}
		}
		expected := ReviewAllow
		if i == 1 {
			expected = ReviewDeny
		}
		if !slices.Equal(verdicts, []ReviewVerdict{expected}) {
			t.Fatal("missing owned automatic approval", verdicts)
		}
		for _, message := range parent.core.messages {
			if plain, ok := message.Content.Plain(); ok && strings.Contains(plain, "Review this action") {
				t.Fatal("Guardian prompt leaked into parent transcript")
			}
		}
	}
}

func TestAutomaticGuardianCannotBypassPermissionDenials(t *testing.T) {
	for _, row := range []struct {
		name    string
		mode    PermissionMode
		command string
	}{
		{"immutable-deny", ModeInteractive, "rm -rf /"},
		{"readonly-deny", ModeReadonly, "rm fixture"},
	} {
		t.Run(row.name, func(t *testing.T) {
			provider := &guardianSourceProvider{answer: "ALLOW anything"}
			config := runtimeConfig(t.TempDir(), provider)
			config.GuardianEnabled, config.Mode = true, row.mode
			if _, err := NewRuntimeSession(config); err == nil {
				t.Fatal("unbound automatic review was accepted")
			}
			config.Approvals, _ = NewApprovalBroker(ApprovalBrokerConfig{})
			parent, err := NewRuntimeSession(config)
			if err != nil {
				t.Fatal(err)
			}
			run, err := DefaultRunContext()
			if err != nil {
				t.Fatal(err)
			}
			parent.mu.Lock()
			outcome, err := parent.dispatchTool(context.Background(), run, protocol.ToolUseBlock{ID: "blocked", Name: protocol.ToolBash, Input: protocol.BashToolInput(protocol.BashInput{Command: row.command})})
			parent.mu.Unlock()
			if err != nil || !outcome.Denied || len(provider.requests) != 0 || len(config.Approvals.List(parent.id)) != 0 {
				t.Fatal("Guardian bypassed permission denial", outcome, err)
			}
		})
	}
}
