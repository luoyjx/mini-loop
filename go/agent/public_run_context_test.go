package agent_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type publicRunContextOptions struct {
	ActorID              *agent.ActorID        `json:"actor_id"`
	Channel              *string               `json:"channel"`
	StampedBy            *string               `json:"stamped_by"`
	DelegatedBy          string                `json:"delegated_by"`
	ParentMessageID      *agent.MessageID      `json:"parent_message_id"`
	ApprovedCapabilities []agent.RunCapability `json:"approved_capabilities"`
}

type publicRunContextAllows struct {
	Empty          bool `json:""`
	A              bool `json:"a"`
	WorkflowLaunch bool `json:"workflow.launch"`
	WorkflowManage bool `json:"workflow.manage"`
}

// An explicit decoder is needed because encoding/json treats an empty JSON tag
// as the Go field name, whereas the Source membership probe uses the key "".
func (allows *publicRunContextAllows) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return errors.New("capability membership must be an object")
	}
	var decoded publicRunContextAllows
	count := 0
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok {
			return errors.New("capability membership key must be text")
		}
		var value bool
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		switch key {
		case "":
			decoded.Empty = value
		case "a":
			decoded.A = value
		case "workflow.launch":
			decoded.WorkflowLaunch = value
		case "workflow.manage":
			decoded.WorkflowManage = value
		default:
			return fmt.Errorf("unexpected capability membership key %q", key)
		}
		count++
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	if count != 4 {
		return errors.New("Source capability membership must contain four keys")
	}
	*allows = decoded
	return nil
}

type publicRunContextEvent struct {
	Type            agent.SessionEventKind `json:"type"`
	MessageID       agent.MessageID        `json:"message_id"`
	ParentPresent   bool                   `json:"parent_present"`
	ParentMessageID *agent.MessageID       `json:"parent_message_id"`
}

type publicRunContextCase struct {
	Name         string                     `json:"name"`
	Kind         string                     `json:"kind"`
	Mode         string                     `json:"mode"`
	Options      publicRunContextOptions    `json:"options"`
	Snapshot     agent.RunContextSnapshot   `json:"snapshot"`
	ToolContexts []agent.RunContextSnapshot `json:"tool_contexts"`
	Events       []publicRunContextEvent    `json:"events"`
	Allows       publicRunContextAllows     `json:"allows"`
	FreshMessage bool                       `json:"fresh_message"`
	Output       string                     `json:"output"`
	Result       string                     `json:"result"`
	Requests     int                        `json:"requests"`
	Messages     int                        `json:"messages"`
}

type publicRunContextFixture struct {
	Cases []publicRunContextCase `json:"cases"`
}

type publicRunContextIdentity struct {
	base agent.MessageID
	self agent.MessageID
}

func (identity publicRunContextIdentity) normalize(value agent.MessageID) agent.MessageID {
	if value == identity.self {
		return "self"
	}
	if value == identity.base {
		return "base"
	}
	return value
}

func (identity publicRunContextIdentity) snapshot(value agent.RunContextSnapshot) agent.RunContextSnapshot {
	value = value.Clone()
	value.MessageID = identity.normalize(value.MessageID)
	if value.ParentMessageID != nil {
		parent := identity.normalize(*value.ParentMessageID)
		value.ParentMessageID = &parent
	}
	return value
}

func publicRunContextRecipe(t *testing.T, row publicRunContextCase) (agent.RunContext, publicRunContextIdentity) {
	t.Helper()
	var base agent.RunContext
	var err error
	switch row.Kind {
	case "default":
		base, err = agent.DefaultRunContext()
	case "human":
		base, err = agent.ExplicitHumanRunContext(agent.HumanRunConfig{
			ActorID: row.Options.ActorID, Channel: row.Options.Channel, StampedBy: row.Options.StampedBy,
			ApprovedCapabilities: row.Options.ApprovedCapabilities,
		})
	case "peer":
		base, err = agent.PeerAgentRunContext(row.Options.DelegatedBy, agent.PeerRunConfig{
			ActorID: row.Options.ActorID, StampedBy: row.Options.StampedBy, ParentMessageID: row.Options.ParentMessageID,
		})
	default:
		t.Fatalf("unsupported Source constructor %q", row.Kind)
	}
	if err != nil {
		t.Fatal(err)
	}
	run := base
	switch row.Mode {
	case "identity":
	case "new-clear":
		run, err = base.WithNewMessage(nil)
	case "new-grants":
		run, err = base.WithNewMessage([]agent.RunCapability{agent.CapabilityWorkflowLaunch, "", "a", "a"})
	case "derive-peer":
		run, err = base.DerivePeerAgent("controller")
	case "derive-empty-actor":
		run, err = base.DeriveNamedPeerAgent("", "")
	default:
		t.Fatalf("unsupported Source derivation %q", row.Mode)
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []agent.RunContext{base, run} {
		if err := value.Validate(); err != nil {
			t.Fatal("public constructor produced an unusable context", err)
		}
		message := string(value.MessageID())
		if !strings.HasPrefix(message, "msg_") {
			t.Fatal("message ID prefix differs", message)
		}
		uuid, err := hex.DecodeString(strings.TrimPrefix(message, "msg_"))
		if err != nil || len(uuid) != 16 || uuid[6]&0xf0 != 0x40 || uuid[8]&0xc0 != 0x80 {
			t.Fatal("message ID must contain a version 4 UUID", message, err)
		}
	}
	identity := publicRunContextIdentity{base: base.MessageID(), self: run.MessageID()}
	if (identity.base != identity.self) != row.FreshMessage {
		t.Fatal("new-message identity differs", identity, row.FreshMessage)
	}
	return run, identity
}

type publicRunProbeInput struct{}

func (publicRunProbeInput) Validate() error { return nil }

type publicRunProbeCodec struct{}

func (publicRunProbeCodec) DecodeExtensionInput(_ context.Context, input protocol.ToolInput) (publicRunProbeInput, error) {
	if input.Name() != "context_probe" {
		return publicRunProbeInput{}, errors.New("unexpected public context probe")
	}
	data, err := input.MarshalJSON()
	if err != nil {
		return publicRunProbeInput{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var decoded publicRunProbeInput
	return decoded, decoder.Decode(&decoded)
}

type publicRunProbeHandler struct {
	contexts []agent.RunContextSnapshot
}

func (handler *publicRunProbeHandler) ExecuteExtensionTool(_ context.Context, authority agent.ToolAuthority, _ publicRunProbeInput) (string, error) {
	handler.contexts = append(handler.contexts, authority.RunContext.Snapshot())
	return string(authority.RunContext.Authority()), nil
}

type publicRunContextProvider struct {
	requests []protocol.ModelRequest
}

func (provider *publicRunContextProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	provider.requests = append(provider.requests, request.Clone())
	reply := protocol.ModelReply{ID: "context-reply", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: "context-model", StopReason: protocol.StopEndTurn, Content: []protocol.Block{protocol.NewTextBlock("done")}}
	if len(provider.requests) == 1 {
		input, err := protocol.DecodeModelToolInput("context_probe", []byte(`{}`))
		if err != nil {
			return protocol.ModelReply{}, err
		}
		reply.Content = []protocol.Block{protocol.NewToolUse("probe", input)}
		reply.StopReason = protocol.StopToolUse
	}
	return reply, nil
}

func publicRunContextCatalog(t *testing.T, handler *publicRunProbeHandler) *agent.ToolCatalog {
	t.Helper()
	definition, err := agent.NewTypedExtensionTool(protocol.ToolSchema{Name: "context_probe", Description: "Read caller context", InputSchema: protocol.InputSchema{Type: protocol.SchemaObject}}, agent.ToolTraits{Risk: agent.RiskRead, Readonly: true}, publicRunProbeCodec{}, handler)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := agent.NewToolCatalog(definition)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func publicRunContextSession(t *testing.T) (*agent.Session, *publicRunContextProvider, *publicRunProbeHandler) {
	t.Helper()
	handler, provider := &publicRunProbeHandler{}, &publicRunContextProvider{}
	gate, err := agent.NewToolGate(publicRunContextCatalog(t, handler), agent.DefaultPermissionPolicy(nil), agent.GateHooks{})
	if err != nil {
		t.Fatal(err)
	}
	session, err := agent.NewSessionWithGate("public-context", "owner", provider, gate, agent.ModeAuto, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	return session, provider, handler
}

// This boundary distinguishes omitted, null and explicitly empty parent IDs.
type publicRunObservedParent struct {
	present bool
	value   *agent.MessageID
}

func (parent *publicRunObservedParent) UnmarshalJSON(data []byte) error {
	parent.present = true
	return json.Unmarshal(data, &parent.value)
}

type publicRunWireEvent struct {
	Type            agent.SessionEventKind  `json:"type"`
	MessageID       agent.MessageID         `json:"message_id"`
	ParentMessageID publicRunObservedParent `json:"parent_message_id"`
}

func publicRunEventProjection(data []byte, identity publicRunContextIdentity) (publicRunContextEvent, error) {
	var event publicRunWireEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return publicRunContextEvent{}, err
	}
	projection := publicRunContextEvent{Type: event.Type, MessageID: identity.normalize(event.MessageID), ParentPresent: event.ParentMessageID.present}
	if event.ParentMessageID.value != nil {
		parent := identity.normalize(*event.ParentMessageID.value)
		projection.ParentMessageID = &parent
	}
	return projection, nil
}

func readPublicRunContextFixture(t *testing.T) publicRunContextFixture {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-public-run-context.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture publicRunContextFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 70 {
		t.Fatal("Source public context profile count changed", len(fixture.Cases))
	}
	return fixture
}

func TestPublicRunContextMatchesActualSource(t *testing.T) {
	for _, row := range readPublicRunContextFixture(t).Cases {
		t.Run(row.Name, func(t *testing.T) {
			run, identity := publicRunContextRecipe(t, row)
			if got := identity.snapshot(run.Snapshot()); !reflect.DeepEqual(got, row.Snapshot) {
				t.Fatalf("public snapshot differs\ngot:  %+v\nwant: %+v", got, row.Snapshot)
			}
			allows := publicRunContextAllows{run.Allows(""), run.Allows("a"), run.Allows(agent.CapabilityWorkflowLaunch), run.Allows(agent.CapabilityWorkflowManage)}
			if allows != row.Allows {
				t.Fatal("capability membership differs", allows, row.Allows)
			}
			session, provider, handler := publicRunContextSession(t)
			output, err := session.RunWithContext(context.Background(), "context", run)
			if err != nil || output != row.Output || len(provider.requests) != row.Requests || len(session.Messages()) != row.Messages {
				t.Fatal("actual session differs", output, err, len(provider.requests), len(session.Messages()), row.Output, row.Requests, row.Messages)
			}
			contexts := make([]agent.RunContextSnapshot, len(handler.contexts))
			for i, value := range handler.contexts {
				contexts[i] = identity.snapshot(value)
			}
			if !reflect.DeepEqual(contexts, row.ToolContexts) {
				t.Fatal("actual tool authority context differs", contexts, row.ToolContexts)
			}
			results := []string{}
			for _, message := range session.Messages() {
				blocks, _ := message.Content.Blocks()
				for _, block := range blocks {
					if result, ok := block.ToolResult(); ok {
						if result.ToolUseID != "probe" || result.IsError {
							t.Fatal("context result pairing differs", result)
						}
						results = append(results, result.Content)
					}
				}
			}
			if !reflect.DeepEqual(results, []string{row.Result}) {
				t.Fatal("tool result differs", results, row.Result)
			}
			events := []publicRunContextEvent{}
			for _, record := range session.Events() {
				if record.Event.Kind() != agent.EventToolUse && record.Event.Kind() != agent.EventToolResult {
					continue
				}
				if got := identity.snapshot(record.Scope.RunContext.Snapshot()); !reflect.DeepEqual(got, row.Snapshot) {
					t.Fatal("live event provenance differs", got, row.Snapshot)
				}
				data, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				projection, err := publicRunEventProjection(data, identity)
				if err != nil {
					t.Fatal(err)
				}
				events = append(events, projection)
			}
			if !reflect.DeepEqual(events, row.Events) {
				t.Fatal("tool event correlation differs", events, row.Events)
			}
		})
	}
}

func TestPublicRunContextDetachedInputsAndSnapshots(t *testing.T) {
	actor, channel, stamp := agent.ActorID("human"), "", ""
	approved := []agent.RunCapability{"a", agent.CapabilityWorkflowLaunch}
	human, err := agent.ExplicitHumanRunContext(agent.HumanRunConfig{ActorID: &actor, Channel: &channel, StampedBy: &stamp, ApprovedCapabilities: approved})
	if err != nil {
		t.Fatal(err)
	}
	humanBefore := human.Snapshot()
	actor, channel, stamp, approved[0] = "changed", "changed", "changed", "changed"
	snapshot := human.Snapshot()
	*snapshot.ActorID = "snapshot-change"
	snapshot.ApprovedCapabilities[0] = "snapshot-change"
	snapshot.Authority, snapshot.Channel, snapshot.StampedBy = agent.AuthorityPeerAgent, "snapshot-change", "snapshot-change"
	if !reflect.DeepEqual(human.Snapshot(), humanBefore) {
		t.Fatal("caller input or snapshot changed live human context")
	}
	peerActor, parent, peerStamp := agent.ActorID(""), agent.MessageID(""), ""
	peer, err := agent.PeerAgentRunContext("", agent.PeerRunConfig{ActorID: &peerActor, ParentMessageID: &parent, StampedBy: &peerStamp})
	if err != nil {
		t.Fatal(err)
	}
	peerBefore := peer.Snapshot()
	peerActor, parent, peerStamp = "changed", "changed", "changed"
	snapshot = peer.Snapshot()
	*snapshot.ActorID, *snapshot.ParentMessageID, *snapshot.DelegatedBy = "snapshot-change", "snapshot-change", "snapshot-change"
	if !reflect.DeepEqual(peer.Snapshot(), peerBefore) {
		t.Fatal("caller input or snapshot changed live peer context")
	}
	grants := []agent.RunCapability{agent.CapabilityWorkflowLaunch}
	continued, err := peer.WithNewMessage(grants)
	if err != nil {
		t.Fatal(err)
	}
	grants[0] = agent.CapabilityWorkflowManage
	if !continued.Allows(agent.CapabilityWorkflowLaunch) || continued.Allows(agent.CapabilityWorkflowManage) || continued.Authority() != agent.AuthorityPeerAgent {
		t.Fatal("membership promoted peer authority or retained caller slice", continued.Snapshot())
	}
	derived, err := continued.DeriveNamedPeerAgent("child", "child")
	if err != nil || derived.Authority() != agent.AuthorityPeerAgent || derived.Allows(agent.CapabilityWorkflowLaunch) || derived.Snapshot().StampedBy != "" {
		t.Fatal("peer derivation retained approvals or replaced explicit empty stamp", derived.Snapshot(), err)
	}
}

func TestPublicZeroRunContextCannotExecute(t *testing.T) {
	var zero agent.RunContext
	if zero.Validate() == nil || zero.Authority() != agent.AuthorityUntrusted || zero.Allows(agent.CapabilityWorkflowLaunch) {
		t.Fatal("zero context acquired usable authority")
	}
	if _, err := zero.WithNewMessage([]agent.RunCapability{agent.CapabilityWorkflowLaunch}); err == nil {
		t.Fatal("new-message constructor admitted zero context")
	}
	if _, err := zero.DerivePeerAgent("lead"); err == nil {
		t.Fatal("peer constructor admitted zero parent")
	}
	if _, err := zero.DeriveNamedPeerAgent("lead", "actor"); err == nil {
		t.Fatal("named peer constructor admitted zero parent")
	}
	session, provider, handler := publicRunContextSession(t)
	if _, err := session.RunWithContext(context.Background(), "context", zero); err == nil || len(provider.requests) != 0 || len(handler.contexts) != 0 || len(session.Messages()) != 0 {
		t.Fatal("invalid context reached a runtime effect", err, len(provider.requests), len(handler.contexts), len(session.Messages()))
	}
}

type publicRunUnusedBash struct{}

func (publicRunUnusedBash) ExecuteBash(context.Context, protocol.BashInput) (string, error) {
	return "", errors.New("context probe must not invoke Bash")
}

type publicRunTrajectoryWriter struct {
	identity publicRunContextIdentity
	events   []publicRunContextEvent
	restored []agent.RunContextSnapshot
	starts   int
	finished agent.TrajectoryStatus
}

func (writer *publicRunTrajectoryWriter) Start(agent.TrajectoryStart) (agent.TrajectoryID, error) {
	writer.starts++
	return "public-run-context", nil
}

func (writer *publicRunTrajectoryWriter) Append(_ agent.TrajectoryID, record agent.TrajectoryRecord) error {
	if record.Record.Event.Kind() != agent.EventToolUse && record.Record.Event.Kind() != agent.EventToolResult {
		return nil
	}
	data, err := record.MarshalArchiveJSON()
	if err != nil {
		return err
	}
	projection, err := publicRunEventProjection(data, writer.identity)
	if err != nil {
		return err
	}
	archived, err := agent.DecodeStoredEvent(data)
	if err != nil {
		return err
	}
	writer.events = append(writer.events, projection)
	writer.restored = append(writer.restored, archived.Scope.RunContext.Snapshot())
	return nil
}

func (writer *publicRunTrajectoryWriter) Finish(_ agent.TrajectoryID, finish agent.TrajectoryFinish) error {
	writer.finished = finish.Status
	return nil
}

func (*publicRunTrajectoryWriter) Count(agent.SessionID) (int, error) { return 0, nil }

// Source compares the direct Agent above. These two cases separately verify the
// native managed recording boundary and its deliberately inert archive decoder.
func TestPublicRunContextManagedArchiveDoesNotRestoreAuthority(t *testing.T) {
	profiles := 0
	for _, row := range readPublicRunContextFixture(t).Cases {
		if row.Name != "human-empty-provenance/new-grants" && row.Name != "peer-empty-parent/identity" {
			continue
		}
		profiles++
		t.Run(row.Name, func(t *testing.T) {
			run, identity := publicRunContextRecipe(t, row)
			writer := &publicRunTrajectoryWriter{identity: identity}
			handler, provider := &publicRunProbeHandler{}, &publicRunContextProvider{}
			session, err := agent.NewManagedSession(agent.RuntimeConfig{
				ID: "public-context", Owner: "owner", Workspace: t.TempDir(), Provider: provider,
				Bash: publicRunUnusedBash{}, Mode: agent.ModeAuto, MaxRounds: 2,
				Tools: publicRunContextCatalog(t, handler), CachePolicy: agent.NullCachePolicy{}, Trajectories: writer,
			})
			if err != nil {
				t.Fatal(err)
			}
			output, err := session.RunWithContext(context.Background(), "context", run)
			if err != nil || output != "done" || writer.starts != 1 || writer.finished != agent.TrajectoryCompleted || session.Info().TrajectoryRecordingError != nil {
				t.Fatal("managed recording failed", output, err, writer.starts, writer.finished, session.Info().TrajectoryRecordingError)
			}
			if !reflect.DeepEqual(writer.events, row.Events) {
				t.Fatal("archival correlation changed absent or empty parent", writer.events, row.Events)
			}
			if len(writer.restored) != 2 {
				t.Fatal("tool events were not decoded from actual archival records", len(writer.restored))
			}
			for _, restored := range writer.restored {
				if restored.Authority != agent.AuthorityUntrusted || restored.ActorID != nil || len(restored.ApprovedCapabilities) != 0 {
					t.Fatal("archival projection reconstructed live authority", restored)
				}
			}
			if len(handler.contexts) != 1 || !reflect.DeepEqual(identity.snapshot(handler.contexts[0]), row.Snapshot) {
				t.Fatal("managed writer altered live tool authority", handler.contexts)
			}
		})
	}
	if profiles != 2 {
		t.Fatal("representative native archive profiles disappeared", profiles)
	}
}

type publicRunGoalProvider struct{ requests int }

func (provider *publicRunGoalProvider) Complete(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
	provider.requests++
	reply := protocol.ModelReply{ID: "goal-context", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: "context-model", StopReason: protocol.StopEndTurn, Content: []protocol.Block{protocol.NewTextBlock("done")}}
	if provider.requests == 1 {
		reply.StopReason = protocol.StopToolUse
		reply.Content = []protocol.Block{
			protocol.NewToolUse("create", protocol.CreateGoalToolInput(protocol.CreateGoalInput{Objective: "must not arm"})),
			protocol.NewToolUse("resume", protocol.ResumeGoalToolInput(protocol.GoalReferenceInput{Revision: 1})),
		}
	}
	return reply, nil
}

func TestPublicPeerCapabilityMembershipCannotArmGoals(t *testing.T) {
	peer, err := agent.PeerAgentRunContext("lead", agent.PeerRunConfig{})
	if err != nil {
		t.Fatal(err)
	}
	peer, err = peer.WithNewMessage([]agent.RunCapability{agent.CapabilityWorkflowLaunch})
	if err != nil || !peer.Allows(agent.CapabilityWorkflowLaunch) {
		t.Fatal("peer capability membership prerequisite failed", peer.Snapshot(), err)
	}
	provider := &publicRunGoalProvider{}
	session, err := agent.NewRuntimeSession(agent.RuntimeConfig{
		ID: "peer-goal", Owner: "owner", Workspace: t.TempDir(), Provider: provider,
		Bash: publicRunUnusedBash{}, Mode: agent.ModeAuto, MaxRounds: 2,
		GoalTools: true, CachePolicy: agent.NullCachePolicy{},
	})
	if err != nil {
		t.Fatal(err)
	}
	output, err := session.RunWithContext(context.Background(), "try to arm", peer)
	if err != nil || output != "done" || provider.requests != 2 {
		t.Fatal("peer goal attempt did not complete its ordinary tool round", output, err, provider.requests)
	}
	goal := session.GoalSnapshot()
	if goal.Goal != nil || goal.Armed {
		t.Fatal("capability membership armed a peer goal", goal)
	}
	refused := []string{}
	for _, message := range session.Messages() {
		blocks, _ := message.Content.Blocks()
		for _, block := range blocks {
			if result, ok := block.ToolResult(); ok {
				if result.Content != "Error: goal_create and goal_resume require explicit human authority; this run is not carrying it" {
					t.Fatal("privileged tool did not refuse peer authority", result)
				}
				refused = append(refused, result.ToolUseID)
			}
		}
	}
	if !reflect.DeepEqual(refused, []string{"create", "resume"}) {
		t.Fatal("both privileged tool attempts must be observed", refused)
	}
	for _, record := range session.Events() {
		if record.Event.Kind() == agent.EventGoalChange {
			t.Fatal("peer refusal still mutated goal state")
		}
	}
}
