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

	"github.com/luoyjx/mini-loop/go/protocol"
)

type subagentContract struct {
	Definitions []struct {
		Schema       protocol.ToolSchema
		Capabilities []Capability
	}
	Roles []struct {
		Role   AgentRole
		Names  []protocol.ToolName
		Failed bool
	}
	Contexts []struct {
		Name     string
		Snapshot RunContextSnapshot
		AllowsA  bool `json:"allows_a"`
	}
	Children []struct {
		Role                           AgentRole
		Exhausted                      bool
		Output, Summary, System, Model string
		Names                          []protocol.ToolName
		Messages                       []protocol.Message
		MaxTokens                      int `json:"max_tokens"`
		Context                        RunContextSnapshot
		Lineage                        SubagentLineage
		Made                           *string
	}
	DefaultMaxDepth  int `json:"default_max_depth"`
	DefaultMaxRounds int `json:"default_max_rounds"`
	Refusal          string
}

func readSubagentContract(t *testing.T) subagentContract {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-subagents.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture subagentContract
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}
func TestRoleCapabilitySelectionMatchesPython(t *testing.T) {
	fixture := readSubagentContract(t)
	var definitions []ToolDefinition
	for _, input := range fixture.Definitions {
		definition, err := NewToolDefinitionWithSchema(input.Schema, ToolTraits{Risk: RiskRead, Readonly: true, Capabilities: input.Capabilities}, &gateHandler{})
		if err != nil {
			t.Fatal(err)
		}
		definitions = append(definitions, definition)
	}
	parent, err := NewToolCatalog(definitions...)
	if err != nil {
		t.Fatal(err)
	}
	policy := DefaultRoleToolPolicy()
	for _, expected := range fixture.Roles {
		selected, err := policy.Select(expected.Role, parent)
		if (err != nil) != expected.Failed {
			t.Fatalf("role %s failure drift: %v", expected.Role, err)
		}
		if err == nil && !reflect.DeepEqual(selected.Names(), expected.Names) {
			t.Fatalf("role %s selection drift: %v != %v", expected.Role, selected.Names(), expected.Names)
		}
	}
	profiles := []RoleCapabilityProfile{{RoleExplore, []Capability{CapabilityRepoRead}}}
	policy, err = NewCapabilityRoleToolPolicy(profiles)
	if err != nil {
		t.Fatal(err)
	}
	profiles[0].Capabilities[0] = CapabilityWorkspaceWrite
	selected, err := policy.Select(RoleExplore, parent)
	if err != nil || !reflect.DeepEqual(selected.Names(), []protocol.ToolName{protocol.ToolReadFile}) {
		t.Fatal("role profile was mutable")
	}
}

func TestRunContextDerivationMatchesPython(t *testing.T) {
	actor := ActorID("human-1")
	human, err := ExplicitHumanRunContext(HumanRunConfig{ActorID: &actor, ApprovedCapabilities: []RunCapability{CapabilityWorkflowLaunch, "a", CapabilityWorkflowLaunch}})
	if err != nil {
		t.Fatal(err)
	}
	base, err := DefaultRunContext()
	if err != nil {
		t.Fatal(err)
	}
	peer, err := human.DerivePeerAgent("main")
	if err != nil {
		t.Fatal(err)
	}
	nextHuman, err := human.WithNewMessage(nil)
	if err != nil {
		t.Fatal(err)
	}
	nextPeer, err := peer.WithNewMessage([]RunCapability{"a"})
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]RunContext{"default": base, "human": human, "peer": peer, "new_human": nextHuman, "new_peer": nextPeer}
	identities := make(map[MessageID]MessageID)
	for name, value := range values {
		identities[value.MessageID()] = MessageID(name)
	}
	for _, expected := range readSubagentContract(t).Contexts {
		value := values[expected.Name]
		snapshot := value.Snapshot()
		snapshot.MessageID = identities[snapshot.MessageID]
		if snapshot.ParentMessageID != nil {
			mapped := identities[*snapshot.ParentMessageID]
			snapshot.ParentMessageID = &mapped
		}
		if !reflect.DeepEqual(snapshot, expected.Snapshot) || value.Allows("a") != expected.AllowsA {
			t.Fatalf("context %s drift: %+v != %+v", expected.Name, snapshot, expected.Snapshot)
		}
	}
	actor = "mutated"
	snapshot := human.Snapshot()
	snapshot.ApprovedCapabilities[0] = "mutated"
	if *human.Snapshot().ActorID != "human-1" || !human.Allows("a") {
		t.Fatal("run context leaked caller storage")
	}
	if (RunContext{}).Authority() != AuthorityUntrusted || (RunContext{}).Allows(CapabilityWorkflowLaunch) {
		t.Fatal("zero context grants authority")
	}
}

type childContractProvider struct {
	role     AgentRole
	requests []protocol.ModelRequest
}

func (provider *childContractProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	if err := request.Validate(); err != nil {
		return protocol.ModelReply{}, err
	}
	provider.requests = append(provider.requests, request.Clone())
	child := request.System != nil && strings.Contains(*request.System, " subagent in ")
	last := request.Messages[len(request.Messages)-1]
	if _, plain := last.Content.Plain(); plain {
		if child {
			input := protocol.ReadFileToolInput(protocol.ReadFileInput{Path: "proof"})
			if provider.role != RoleExplore {
				input = protocol.WriteFileToolInput(protocol.WriteFileInput{Path: "made.txt", Content: "worker file"})
			}
			return fakeReply([]protocol.Block{protocol.NewTextBlock("child progress"), protocol.NewToolUse("child", input)}, protocol.StopToolUse), nil
		}
		role := protocol.AgentType(provider.role)
		return fakeReply([]protocol.Block{protocol.NewToolUse("parent", protocol.TaskToolInput(protocol.TaskInput{Prompt: "delegated prompt", AgentType: &role}))}, protocol.StopToolUse), nil
	}
	text := "parent done"
	if child {
		text = "child done"
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock(text)}, protocol.StopEndTurn), nil
}

type authorityCapture struct{ authorities []ToolAuthority }

func (hook *authorityCapture) BeforeTool(_ context.Context, authority ToolAuthority, _ ToolCall) (BeforeDecision, error) {
	hook.authorities = append(hook.authorities, authority)
	return KeepToolCall(), nil
}

func TestRealTaskChildrenMatchPython(t *testing.T) {
	fixture := readSubagentContract(t)
	for _, expected := range fixture.Children {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "proof"), []byte("proof content"), 0600); err != nil {
			t.Fatal(err)
		}
		provider := &childContractProvider{role: expected.Role}
		hook := &authorityCapture{}
		subagents := &InProcessSubagents{}
		config := runtimeConfig(root, provider)
		config.Label = "main"
		config.Subagents = subagents
		config.Model = expected.Model
		config.MaxTokens = expected.MaxTokens
		config.TokenThreshold = 10_000_000
		config.Hooks.Before = []BeforeHook{hook}
		config.SubagentMaxRounds = 2
		if expected.Exhausted {
			config.SubagentMaxRounds = 1
		}
		session, err := NewRuntimeSession(config)
		if err != nil {
			t.Fatal(err)
		}
		root = session.workspace
		session.messages = append(session.messages, protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent("private parent history")})
		actor := ActorID("human-1")
		human, err := ExplicitHumanRunContext(HumanRunConfig{ActorID: &actor, ApprovedCapabilities: []RunCapability{CapabilityWorkflowLaunch}})
		if err != nil {
			t.Fatal(err)
		}
		output, err := session.RunWithContext(context.Background(), "delegate", human)
		if err != nil || output != expected.Output {
			t.Fatalf("run %s: %q %v", expected.Role, output, err)
		}
		var childRequest protocol.ModelRequest
		for _, request := range provider.requests {
			if request.System != nil && strings.Contains(*request.System, " subagent in ") {
				childRequest = request
				break
			}
		}
		names := make([]protocol.ToolName, len(childRequest.Tools))
		for i, tool := range childRequest.Tools {
			names[i] = tool.Name
		}
		if childRequest.System == nil || strings.ReplaceAll(*childRequest.System, root, "<workspace>") != expected.System || !reflect.DeepEqual(names, expected.Names) || !reflect.DeepEqual(childRequest.Messages, expected.Messages) || childRequest.MaxTokens != expected.MaxTokens || childRequest.Model != expected.Model {
			t.Fatalf("child %s request drift: system=%v tools=%v messages=%v model=%s max=%d", expected.Role, childRequest.System, names, childRequest.Messages, childRequest.Model, childRequest.MaxTokens)
		}
		var childAuthority ToolAuthority
		for _, authority := range hook.authorities {
			if authority.RunContext.Authority() == AuthorityPeerAgent {
				childAuthority = authority
				break
			}
		}
		snapshot := childAuthority.RunContext.Snapshot()
		snapshot.MessageID = "child"
		parentID := MessageID("human")
		snapshot.ParentMessageID = &parentID
		if !reflect.DeepEqual(snapshot, expected.Context) || childAuthority.OwnerID != config.Owner || childAuthority.Workspace != root || childAuthority.SessionID == config.ID {
			t.Fatalf("child context drift: %+v", snapshot)
		}
		if expected.Role == RoleExplore && childAuthority.Mode != ModeReadonly {
			t.Fatal("explore was not readonly")
		}
		lineage, ok := subagents.LastLineage()
		if !ok || lineage != expected.Lineage {
			t.Fatal("lineage drift")
		}
		blocks, _ := session.Messages()[3].Content.Blocks()
		result, _ := blocks[0].ToolResult()
		if result.Content != expected.Summary || result.IsError {
			t.Fatalf("task summary drift: %+v", result)
		}
		data, err := os.ReadFile(filepath.Join(root, "made.txt"))
		if expected.Made == nil {
			if !os.IsNotExist(err) {
				t.Fatal("explore wrote a file")
			}
		} else if err != nil || string(data) != *expected.Made {
			t.Fatal("worker file effect drift")
		}
		if expected.Exhausted {
			found := false
			for _, record := range session.Events() {
				if event, ok := record.Event.RunError(); ok {
					found = true
					run := record.Scope.RunContext.Snapshot()
					if event.Kind() != ErrorRoundExhaustion || record.Scope.Depth != 1 || record.Scope.Label != "main>general-purpose" || run.Authority != AuthorityPeerAgent || run.ParentMessageID == nil || *run.ParentMessageID != human.MessageID() {
						t.Fatal("forwarded child event lost lineage")
					}
				}
			}
			if !found {
				t.Fatal("exhausted child emitted no error")
			}
		}
	}
}

type cannedSubagents struct {
	calls   int
	summary string
	err     error
	request SubagentRequest
}

func (provider *cannedSubagents) RunSubagent(_ context.Context, request SubagentRequest) (string, error) {
	provider.calls++
	provider.request = request
	return provider.summary, provider.err
}
func TestEverySubagentProviderObeysDepthQuotaAndTelemetry(t *testing.T) {
	fixture := readSubagentContract(t)
	if DefaultSubagentMaxDepth != fixture.DefaultMaxDepth || DefaultSubagentMaxRounds != fixture.DefaultMaxRounds {
		t.Fatal("default quotas drift")
	}
	for _, depth := range []int{1, 2} {
		provider := &cannedSubagents{summary: strings.Repeat("界", 2500)}
		config := runtimeConfig(t.TempDir(), &FakeProvider{})
		config.Subagents = provider
		config.Depth = depth
		session, err := NewRuntimeSession(config)
		if err != nil {
			t.Fatal(err)
		}
		output, err := session.Delegate(context.Background(), strings.Repeat("🌱", 2500), RoleWorker)
		if err != nil {
			t.Fatal(err)
		}
		events := session.Events()
		if depth == 2 {
			if output != fixture.Refusal || provider.calls != 0 || len(events) != 1 || events[0].Event.Kind() != EventSubagentRefused {
				t.Fatal("quota ran provider or emitted start")
			}
			refused, ok := events[0].Event.Subagent()
			childDepth, limit, isRefusal := refused.Refusal()
			if !ok || !isRefusal || childDepth != 3 || limit != 2 {
				t.Fatal("refusal metadata drift")
			}
		} else {
			if output != provider.summary || provider.calls != 1 || len(events) != 2 {
				t.Fatal("provider/telemetry missing")
			}
			start, _ := events[0].Event.Subagent()
			prompt, ok := start.Prompt()
			end, _ := events[1].Event.Subagent()
			summary, endOK := end.Summary()
			if !ok || !endOK || len([]rune(prompt)) != 2000 || len([]rune(summary)) != 2000 || len([]rune(provider.request.Prompt)) != 2500 {
				t.Fatal("display cap changed execution input")
			}
		}
	}
}
func TestSubagentErrorsAndEmptySummary(t *testing.T) {
	for _, failure := range []error{nil, errors.New("remote worker failed"), context.Canceled} {
		provider := &cannedSubagents{err: failure}
		config := runtimeConfig(t.TempDir(), &FakeProvider{})
		config.Subagents = provider
		session, err := NewRuntimeSession(config)
		if err != nil {
			t.Fatal(err)
		}
		output, err := session.Delegate(context.Background(), "work", RoleExplore)
		if failure != nil {
			if !errors.Is(err, failure) || len(session.Events()) != 1 {
				t.Fatal("provider failure fabricated completion")
			}
		} else if err != nil || output != "(subagent produced no summary)" || len(session.Events()) != 2 {
			t.Fatal("empty summary fallback drift")
		}
	}
}
