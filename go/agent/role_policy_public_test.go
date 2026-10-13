package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type publicRoleTool struct {
	Name         protocol.ToolName    `json:"name"`
	Description  string               `json:"description"`
	Capabilities []agent.Capability   `json:"capabilities"`
	Risk         agent.ToolRisk       `json:"risk"`
	Readonly     bool                 `json:"readonly"`
	ParallelSafe bool                 `json:"parallel_safe"`
	Schema       protocol.InputSchema `json:"schema"`
}

type publicRoleProfile struct {
	Role         agent.AgentRole    `json:"role"`
	Capabilities []agent.Capability `json:"capabilities"`
}

type publicRoleCase struct {
	Name     string              `json:"name"`
	Default  bool                `json:"default"`
	Profiles []publicRoleProfile `json:"profiles"`
	Role     agent.AgentRole     `json:"role"`
	Names    []protocol.ToolName `json:"names"`
	Selected []publicRoleTool    `json:"selected"`
	Failed   bool                `json:"failed"`
	Error    string              `json:"error"`
}

type publicRoleAttempt struct {
	Name protocol.ToolName `json:"name"`
	ID   string            `json:"id"`
}

type publicRoleOutput struct {
	ID     string            `json:"id"`
	Name   protocol.ToolName `json:"name"`
	Output string            `json:"output"`
	Denied bool              `json:"denied"`
	Failed bool              `json:"failed"`
}

// Result observers and model tool-result blocks have separate error contracts.
// Retain field presence from the actual Source model request, including an
// omitted is_error versus an explicitly false value.
type publicRoleModelResult struct {
	Type      protocol.BlockKind `json:"type"`
	ToolUseID string             `json:"tool_use_id"`
	Content   string             `json:"content"`
	IsError   *bool              `json:"is_error,omitempty"`
}

type publicRoleObservation struct {
	Label               string                   `json:"label"`
	Depth               int                      `json:"depth"`
	Mode                agent.PermissionMode     `json:"mode"`
	SameWorkspace       bool                     `json:"same_workspace"`
	ParentMarkerPresent bool                     `json:"parent_marker_present"`
	Context             agent.RunContextSnapshot `json:"context"`
}

type publicRoleChild struct {
	Name                   string                   `json:"name"`
	Default                bool                     `json:"default"`
	Profiles               []publicRoleProfile      `json:"profiles"`
	Role                   agent.AgentRole          `json:"role"`
	Attempts               []publicRoleAttempt      `json:"attempts"`
	ChildTools             []protocol.ToolSchema    `json:"child_tools"`
	FirstMessages          []protocol.Message       `json:"first_messages"`
	RequestCount           int                      `json:"request_count"`
	Summary                string                   `json:"summary"`
	Outputs                []publicRoleOutput       `json:"outputs"`
	ToolResults            []publicRoleModelResult  `json:"tool_results"`
	Observations           []publicRoleObservation  `json:"observations"`
	Received               []publicRoleObservation  `json:"received"`
	Effects                []protocol.ToolName      `json:"effects"`
	ParentContext          agent.RunContextSnapshot `json:"parent_context"`
	ParentMode             agent.PermissionMode     `json:"parent_mode"`
	PeerContext            agent.RunContextSnapshot `json:"peer_context"`
	ChildLabel             string                   `json:"child_label"`
	Lineage                agent.SubagentLineage    `json:"lineage"`
	ParentHistoryUnchanged bool                     `json:"parent_history_unchanged"`
	ParentMarkerUnchanged  bool                     `json:"parent_marker_unchanged"`
}

type publicRoleInvalidTool struct {
	Name         protocol.ToolName  `json:"name"`
	Capabilities []agent.Capability `json:"capabilities"`
	Error        string             `json:"error"`
}

type publicRoleFixture struct {
	Tools        []publicRoleTool        `json:"tools"`
	Cases        []publicRoleCase        `json:"cases"`
	Children     []publicRoleChild       `json:"children"`
	InvalidTools []publicRoleInvalidTool `json:"invalid_tools"`
	Limits       []string                `json:"limits"`
}

func readPublicRoleFixture(t *testing.T) publicRoleFixture {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-role-policy.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture publicRoleFixture
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Tools) != 12 || len(fixture.Cases) != 34 || len(fixture.Children) != 7 || len(fixture.InvalidTools) != 6 {
		t.Fatalf("incomplete Source role corpus: tools=%d cases=%d children=%d invalid=%d", len(fixture.Tools), len(fixture.Cases), len(fixture.Children), len(fixture.InvalidTools))
	}
	return fixture
}

func publicRolePolicy(t *testing.T, useDefault bool, rows []publicRoleProfile) *agent.CapabilityRoleToolPolicy {
	t.Helper()
	if useDefault {
		return agent.DefaultRoleToolPolicy()
	}
	profiles := make([]agent.RoleCapabilityProfile, len(rows))
	for i, row := range rows {
		profiles[i] = agent.RoleCapabilityProfile{Role: row.Role, Capabilities: row.Capabilities}
	}
	policy, err := agent.NewCapabilityRoleToolPolicy(profiles)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

type publicRoleInput struct{}

func (publicRoleInput) Validate() error { return nil }

type publicRoleCodec struct{}

func (publicRoleCodec) DecodeExtensionInput(_ context.Context, input protocol.ToolInput) (publicRoleInput, error) {
	data, err := input.MarshalJSON()
	if err != nil {
		return publicRoleInput{}, err
	}
	var decoded publicRoleInput
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoded, decoder.Decode(&decoded)
}

type publicRoleTrace struct {
	before   []agent.ToolAuthority
	received []agent.ToolAuthority
	effects  []protocol.ToolName
	outputs  []publicRoleOutput
}

func (trace *publicRoleTrace) BeforeTool(_ context.Context, authority agent.ToolAuthority, _ agent.ToolCall) (agent.BeforeDecision, error) {
	trace.before = append(trace.before, authority)
	return agent.KeepToolCall(), nil
}

func (trace *publicRoleTrace) OnResult(_ context.Context, _ agent.ToolAuthority, call agent.ToolCall, outcome agent.ToolOutcome) error {
	trace.outputs = append(trace.outputs, publicRoleOutput{ID: call.ID, Name: call.Name(), Output: outcome.Output, Denied: outcome.Denied, Failed: outcome.Failed})
	return nil
}

type publicRoleHandler struct {
	name  protocol.ToolName
	trace *publicRoleTrace
}

func (handler publicRoleHandler) ExecuteExtensionTool(_ context.Context, authority agent.ToolAuthority, _ publicRoleInput) (string, error) {
	if handler.trace == nil {
		return "", fmt.Errorf("selection unexpectedly executed %s", handler.name)
	}
	handler.trace.received = append(handler.trace.received, authority)
	handler.trace.effects = append(handler.trace.effects, handler.name)
	return "effect:" + string(handler.name), nil
}

func publicRoleDefinition(row publicRoleTool, trace *publicRoleTrace) (agent.ToolDefinition, error) {
	data, err := json.Marshal(row.Schema)
	if err != nil {
		return agent.ToolDefinition{}, err
	}
	schema, err := protocol.ExtensionToolSchema(row.Name, row.Description, data)
	if err != nil {
		return agent.ToolDefinition{}, err
	}
	return agent.NewTypedExtensionTool(schema, agent.ToolTraits{Risk: row.Risk, Readonly: row.Readonly, ParallelSafe: row.ParallelSafe, Capabilities: row.Capabilities}, publicRoleCodec{}, publicRoleHandler{row.Name, trace})
}

func publicRoleCatalog(t *testing.T, rows []publicRoleTool, trace *publicRoleTrace) *agent.ToolCatalog {
	t.Helper()
	definitions := make([]agent.ToolDefinition, len(rows))
	for i, row := range rows {
		definition, err := publicRoleDefinition(row, trace)
		if err != nil {
			t.Fatalf("construct %s: %v", row.Name, err)
		}
		definitions[i] = definition
	}
	catalog, err := agent.NewToolCatalog(definitions...)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func publicRoleMetadata(t *testing.T, catalog *agent.ToolCatalog) []publicRoleTool {
	t.Helper()
	snapshot, err := catalog.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	result := make([]publicRoleTool, 0, len(catalog.Names()))
	for _, schema := range snapshot.Schemas() {
		definition, ok := catalog.Lookup(schema.Name)
		if !ok {
			t.Fatalf("snapshot lost definition %s", schema.Name)
		}
		capabilities := append([]agent.Capability{}, definition.Capabilities()...)
		sort.Slice(capabilities, func(i, j int) bool { return capabilities[i] < capabilities[j] })
		result = append(result, publicRoleTool{Name: definition.Name(), Description: schema.Description, Capabilities: capabilities, Risk: definition.Risk(), Readonly: definition.Readonly(), ParallelSafe: definition.ParallelSafe(), Schema: schema.InputSchema})
	}
	return result
}

func TestPublicRolePolicyMatchesActualSourceSelection(t *testing.T) {
	fixture := readPublicRoleFixture(t)
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			catalog := publicRoleCatalog(t, fixture.Tools, nil)
			policy := publicRolePolicy(t, row.Default, row.Profiles)
			selected, err := policy.Select(row.Role, catalog)
			if row.Failed {
				if err == nil || "ValueError: "+err.Error() != row.Error || selected != nil {
					t.Fatalf("Source failure differs: catalogue=%v error=%v want=%q", selected, err, row.Error)
				}
				return
			}
			if err != nil || selected == nil {
				t.Fatalf("select %q: %v", row.Role, err)
			}
			if names := append([]protocol.ToolName{}, selected.Names()...); !reflect.DeepEqual(names, row.Names) {
				t.Fatalf("selected names=%v want=%v", names, row.Names)
			}
			actual, err := json.Marshal(publicRoleMetadata(t, selected))
			if err != nil {
				t.Fatal(err)
			}
			want, err := json.Marshal(row.Selected)
			if err != nil || !bytes.Equal(actual, want) {
				t.Fatalf("selected schema/traits/capability set differs:\ngot %s\nwant %s\nerror %v", actual, want, err)
			}
		})
	}
}

func TestPublicRoleToolAdmissionMatchesActualSource(t *testing.T) {
	for _, row := range readPublicRoleFixture(t).InvalidTools {
		t.Run(string(row.Name), func(t *testing.T) {
			_, err := publicRoleDefinition(publicRoleTool{Name: row.Name, Description: "invalid capability", Capabilities: row.Capabilities, Risk: agent.RiskRead, Readonly: true, Schema: protocol.InputSchema{Type: protocol.SchemaObject}}, nil)
			if err == nil || "ValueError: "+err.Error() != row.Error {
				t.Fatalf("registered capability admission=%v want=%q", err, row.Error)
			}
		})
	}
}

type publicRoleProvider struct {
	attempts []publicRoleAttempt
	requests []protocol.ModelRequest
}

func (provider *publicRoleProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	provider.requests = append(provider.requests, request.Clone())
	reply := protocol.ModelReply{ID: "role-reply", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: "role-model", Content: []protocol.Block{protocol.NewTextBlock("child done")}, StopReason: protocol.StopEndTurn}
	if len(provider.requests) == 1 {
		reply.Content = make([]protocol.Block, 0, len(provider.attempts))
		for _, attempt := range provider.attempts {
			input, err := protocol.DecodeModelToolInput(attempt.Name, []byte(`{}`))
			if err != nil {
				return protocol.ModelReply{}, err
			}
			reply.Content = append(reply.Content, protocol.NewToolUse(attempt.ID, input))
		}
		reply.StopReason = protocol.StopToolUse
	}
	return reply, nil
}

type publicRoleUnusedBash struct{}

func (publicRoleUnusedBash) ExecuteBash(context.Context, protocol.BashInput) (string, error) {
	return "", fmt.Errorf("role fixture must not execute a shell")
}

func publicRoleSnapshot(value agent.RunContextSnapshot, parent, child agent.MessageID) agent.RunContextSnapshot {
	if value.MessageID == parent {
		value.MessageID = "parent"
	} else if value.MessageID == child {
		value.MessageID = "child"
	}
	if value.ParentMessageID != nil && *value.ParentMessageID == parent {
		id := agent.MessageID("parent")
		value.ParentMessageID = &id
	}
	return value
}

func publicRoleObservations(t *testing.T, authorities []agent.ToolAuthority, owner agent.OwnerID, parentID agent.SessionID, workspace string, parent, child agent.MessageID) []publicRoleObservation {
	t.Helper()
	result := make([]publicRoleObservation, 0, len(authorities))
	for _, authority := range authorities {
		snapshot := authority.RunContext.Snapshot()
		if authority.OwnerID != owner || authority.SessionID == parentID || snapshot.MessageID != child || snapshot.ParentMessageID == nil || *snapshot.ParentMessageID != parent || snapshot.Authority != agent.AuthorityPeerAgent || snapshot.ActorID != nil || len(snapshot.ApprovedCapabilities) != 0 {
			t.Fatalf("child inherited identity or human authority: %+v snapshot=%+v", authority, snapshot)
		}
		result = append(result, publicRoleObservation{Label: authority.Scope.Label, Depth: authority.Scope.Depth, Mode: authority.Mode, SameWorkspace: authority.Workspace == workspace, Context: publicRoleSnapshot(snapshot, parent, child)})
	}
	return result
}

func TestPublicRolePolicyFreshChildrenMatchActualSource(t *testing.T) {
	fixture := readPublicRoleFixture(t)
	for _, row := range fixture.Children {
		t.Run(row.Name, func(t *testing.T) {
			workspace, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			trace := &publicRoleTrace{effects: []protocol.ToolName{}, outputs: []publicRoleOutput{}}
			provider := &publicRoleProvider{attempts: row.Attempts}
			subagents := &agent.InProcessSubagents{}
			owner := agent.OwnerID("role-owner")
			if row.ParentMode != agent.ModeAuto {
				t.Fatalf("Source parent mode=%q want auto", row.ParentMode)
			}
			parent, err := agent.NewRuntimeSession(agent.RuntimeConfig{
				ID: "role-parent", Owner: owner, Provider: provider, Bash: publicRoleUnusedBash{},
				Workspace: workspace, Mode: agent.ModeAuto, MaxRounds: 2, Label: "main",
				Tools: publicRoleCatalog(t, fixture.Tools, trace), RoleToolPolicy: publicRolePolicy(t, row.Default, row.Profiles),
				Subagents: subagents, SubagentMaxRounds: 2,
				Hooks: agent.GateHooks{Before: []agent.BeforeHook{trace}, Observers: []agent.ResultObserver{trace}},
			})
			if err != nil {
				t.Fatal(err)
			}
			actor := agent.ActorID("human")
			human, err := agent.ExplicitHumanRunContext(agent.HumanRunConfig{ActorID: &actor, ApprovedCapabilities: []agent.RunCapability{agent.CapabilityWorkflowLaunch, agent.CapabilityWorkflowManage}})
			if err != nil {
				t.Fatal(err)
			}
			original := parent.Messages()
			summary, err := parent.DelegateWithContext(context.Background(), "child", row.Role, human)
			if err != nil || summary != row.Summary || len(provider.requests) != row.RequestCount || len(provider.requests) < 2 {
				t.Fatalf("child summary=%q requests=%d error=%v want=%q/%d", summary, len(provider.requests), err, row.Summary, row.RequestCount)
			}
			actualSchemas, err := protocol.PythonJSON(provider.requests[0].Tools, false, true)
			if err != nil {
				t.Fatal(err)
			}
			wantSchemas, err := protocol.PythonJSON(row.ChildTools, false, true)
			if err != nil || actualSchemas != wantSchemas {
				t.Fatalf("child model catalogue differs:\ngot %s\nwant %s\nerror %v", actualSchemas, wantSchemas, err)
			}
			if !reflect.DeepEqual(provider.requests[0].Messages, row.FirstMessages) || !reflect.DeepEqual(parent.Messages(), original) || !row.ParentHistoryUnchanged {
				t.Fatal("child request borrowed or changed parent history", provider.requests[0].Messages, row.FirstMessages)
			}
			if !reflect.DeepEqual(trace.outputs, row.Outputs) || !reflect.DeepEqual(trace.effects, row.Effects) {
				t.Fatalf("child permission/effects differ: outputs=%+v want=%+v effects=%v want=%v", trace.outputs, row.Outputs, trace.effects, row.Effects)
			}
			followup := provider.requests[1].Messages
			if err := protocol.ValidateTranscript(followup); err != nil {
				t.Fatal("child sent an unpaired tool transcript", err)
			}
			attempts := []publicRoleAttempt{}
			results := []publicRoleModelResult{}
			for _, message := range followup {
				blocks, _ := message.Content.Blocks()
				for _, block := range blocks {
					if use, ok := block.ToolUse(); ok {
						attempts = append(attempts, publicRoleAttempt{Name: use.Name, ID: use.ID})
					}
					if block.Kind() == protocol.BlockToolResult {
						data, err := json.Marshal(block)
						if err != nil {
							t.Fatal(err)
						}
						var result publicRoleModelResult
						decoder := json.NewDecoder(bytes.NewReader(data))
						decoder.DisallowUnknownFields()
						if err := decoder.Decode(&result); err != nil {
							t.Fatal(err)
						}
						results = append(results, result)
					}
				}
			}
			if !reflect.DeepEqual(attempts, row.Attempts) || !reflect.DeepEqual(results, row.ToolResults) {
				t.Fatalf("child model followup differs: attempts=%+v results=%+v want=%+v", attempts, results, row.ToolResults)
			}
			if len(trace.before) == 0 {
				t.Fatal("child never reached the execution boundary")
			}
			childID := trace.before[0].RunContext.MessageID()
			if childID == "" || childID == human.MessageID() {
				t.Fatal("child did not receive a fresh message identity")
			}
			observations := publicRoleObservations(t, trace.before, owner, parent.ID(), workspace, human.MessageID(), childID)
			received := publicRoleObservations(t, trace.received, owner, parent.ID(), workspace, human.MessageID(), childID)
			if !reflect.DeepEqual(observations, row.Observations) || !reflect.DeepEqual(received, row.Received) || observations[0].Label != row.ChildLabel || !reflect.DeepEqual(observations[0].Context, row.PeerContext) {
				t.Fatalf("child scope/provenance differs: observations=%+v want=%+v received=%+v want=%+v", observations, row.Observations, received, row.Received)
			}
			if actual := publicRoleSnapshot(human.Snapshot(), human.MessageID(), childID); !reflect.DeepEqual(actual, row.ParentContext) || !human.Allows(agent.CapabilityWorkflowLaunch) || !human.Allows(agent.CapabilityWorkflowManage) {
				t.Fatalf("delegation modified parent approval: %+v want=%+v", actual, row.ParentContext)
			}
			if lineage, ok := subagents.LastLineage(); !ok || !reflect.DeepEqual(lineage, row.Lineage) {
				t.Fatalf("lineage=%+v present=%v want=%+v", lineage, ok, row.Lineage)
			}
			// Go exposes no arbitrary mutable Agent.state. Source also proves that
			// its private state marker is absent from the fresh child.
			if !row.ParentMarkerUnchanged {
				t.Fatal("Source parent marker changed")
			}
		})
	}
}

func TestPublicRolePolicyDetachesProfilesAndSelectedMetadata(t *testing.T) {
	capabilities := []agent.Capability{agent.CapabilityRepoRead, agent.CapabilityRepoRead, agent.CapabilityWorkspaceWrite}
	properties := protocol.SchemaProperties{"label": {Type: protocol.SchemaString}}
	schema := protocol.ToolSchema{Name: "detached_probe", Description: "original", InputSchema: protocol.InputSchema{Type: protocol.SchemaObject, Properties: &properties}}
	definition, err := agent.NewTypedExtensionTool(schema, agent.ToolTraits{Risk: agent.RiskRead, Readonly: true, Capabilities: capabilities}, publicRoleCodec{}, publicRoleHandler{"detached_probe", nil})
	if err != nil {
		t.Fatal(err)
	}
	if got := definition.Capabilities(); !reflect.DeepEqual(got, []agent.Capability{agent.CapabilityRepoRead, agent.CapabilityWorkspaceWrite}) {
		t.Fatalf("capability set must retain first occurrence order: %v", got)
	}
	definitions := []agent.ToolDefinition{definition}
	catalog, err := agent.NewToolCatalog(definitions...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.NewToolCatalog(definition, definition); err == nil {
		t.Fatal("capability set normalization widened duplicate tool-name admission")
	}
	if _, err := agent.NewTypedExtensionTool(schema, agent.ToolTraits{Risk: agent.RiskExec, Readonly: true}, publicRoleCodec{}, publicRoleHandler{"detached_probe", nil}); err == nil {
		t.Fatal("capability set normalization widened the readonly/risk contract")
	}
	profileCaps := []agent.Capability{agent.CapabilityRepoRead, agent.CapabilityWorkspaceWrite}
	profiles := []agent.RoleCapabilityProfile{{Role: "custom", Capabilities: profileCaps}}
	policy, err := agent.NewCapabilityRoleToolPolicy(profiles)
	if err != nil {
		t.Fatal(err)
	}
	capabilities[0], profileCaps[0] = "mutated", "mutated"
	profiles[0].Role = "mutated"
	definitions[0] = agent.ToolDefinition{}
	schema.Description = "mutated"
	properties["label"] = protocol.InputSchema{Type: protocol.SchemaBoolean}
	selected, err := policy.Select("custom", catalog)
	if err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(publicRoleMetadata(t, selected))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(selected.Names(), []protocol.ToolName{"detached_probe"}) {
		t.Fatal("caller profile or catalogue mutation changed selection", selected.Names())
	}
	snapshot, err := selected.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	returned := snapshot.Schemas()
	if len(returned) != 1 || returned[0].Description != "original" || (*returned[0].InputSchema.Properties)["label"].Type != protocol.SchemaString {
		t.Fatal("caller schema mutation entered catalogue", returned)
	}
	returned[0].Description = "returned mutation"
	(*returned[0].InputSchema.Properties)["label"] = protocol.InputSchema{Type: protocol.SchemaBoolean}
	names := selected.Names()
	names[0] = "returned mutation"
	selectedDefinition, ok := selected.Lookup("detached_probe")
	if !ok {
		t.Fatal("selected definition missing")
	}
	selectedCaps := selectedDefinition.Capabilities()
	selectedCaps[0] = "returned mutation"
	again, err := policy.Select("custom", catalog)
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(publicRoleMetadata(t, again))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("selected metadata modified live policy/catalogue: before=%s after=%s error=%v", before, after, err)
	}
}

func TestPublicRolePolicyDoesNotWidenModelTaskRoles(t *testing.T) {
	for _, role := range []protocol.AgentType{"custom", "İ", "", " Explore "} {
		input := protocol.TaskToolInput(protocol.TaskInput{Prompt: "child", AgentType: &role})
		if err := input.Validate(); err == nil {
			t.Fatalf("model task accepted public-only role %q", role)
		}
	}
	for _, role := range []protocol.AgentType{protocol.AgentExplore, protocol.AgentWorker, protocol.AgentGeneralPurpose} {
		if err := protocol.TaskToolInput(protocol.TaskInput{Prompt: "child", AgentType: &role}).Validate(); err != nil {
			t.Fatalf("model task lost built-in role %q: %v", role, err)
		}
	}
}
