package teams

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

const MaxProtocols = 200
const Lead MemberName = "lead"

type RequestID string
type ProtocolType string
type ProtocolStatus string

const (
	ProtocolShutdown     ProtocolType   = "shutdown"
	ProtocolPlanApproval ProtocolType   = "plan_approval"
	ProtocolPending      ProtocolStatus = "pending"
	ProtocolApproved     ProtocolStatus = "approved"
	ProtocolRejected     ProtocolStatus = "rejected"
)

type ProtocolState struct {
	RequestID RequestID      `json:"request_id"`
	Type      ProtocolType   `json:"type"`
	Sender    MailboxKey     `json:"sender"`
	Target    MailboxKey     `json:"target"`
	Status    ProtocolStatus `json:"status"`
	Payload   string         `json:"payload"`
	CreatedAt float64        `json:"created_at"`
	Feedback  string         `json:"feedback"`
}

// MemberState distinguishes an absent teammate from a session without an agent.
// Lead is not a teammate lookup. Applications own this trusted directory.
type MemberState uint8

const (
	MemberMissing MemberState = iota
	MemberWithoutAgent
	MemberReady
)

type MemberDirectory interface{ Member(Identity) MemberState }
type CoordinatorConfig struct {
	Bus     *Bus
	Members MemberDirectory
}

// Coordinator owns the process-local bounded handshake table. Source correlation
// checks request ID/type/status; sender authenticity is not an added guarantee.
// It publishes before delivery and is not a transactional or durable outbox.
type Coordinator struct {
	mu      sync.Mutex
	bus     *Bus
	members MemberDirectory
	states  map[RequestID]ProtocolState
	order   []RequestID
	limit   int
	now     func() float64
	newID   func() (RequestID, error)
}

var ErrProtocolMetadata = errors.New("protocol message metadata must be an object")
var ErrProtocolRequestID = errors.New("protocol request id is not hashable")
var ErrMemberState = errors.New("team directory returned an invalid member state")

func NewCoordinator(config CoordinatorConfig) (*Coordinator, error) {
	if config.Bus == nil {
		return nil, errors.New("team coordinator requires a bus")
	}
	return &Coordinator{bus: config.Bus, members: config.Members, states: make(map[RequestID]ProtocolState), limit: MaxProtocols,
		now: func() float64 { return float64(time.Now().UnixNano()) / 1e9 }, newID: func() (RequestID, error) {
			var bytes [5]byte
			if _, err := rand.Read(bytes[:]); err != nil {
				return "", err
			}
			return RequestID("req_" + hex.EncodeToString(bytes[:])), nil
		}}, nil
}
func (c *Coordinator) member(identity Identity) (MemberState, error) {
	if c.members == nil {
		return MemberMissing, nil
	}
	state := c.members.Member(identity)
	if state > MemberReady {
		return MemberMissing, ErrMemberState
	}
	return state, nil
}

// Snapshot is a detached insertion-ordered operator view, including all teams.
func (c *Coordinator) Snapshot() []ProtocolState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshotLocked()
}
func (c *Coordinator) snapshotLocked() []ProtocolState {
	states := make([]ProtocolState, 0, len(c.order))
	for _, id := range c.order {
		states = append(states, c.states[id])
	}
	return states
}
func (c *Coordinator) TeamProtocols(team TeamID) []ProtocolState {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []ProtocolState{}
	for _, id := range c.order {
		state := c.states[id]
		if strings.HasPrefix(string(state.Sender), string(team)+"/") {
			out = append(out, state)
		}
	}
	return out
}
func (c *Coordinator) insert(kind ProtocolType, team TeamID, sender, target MemberName, payload string) (ProtocolState, error) {
	id, err := c.newID()
	if err != nil {
		return ProtocolState{}, err
	}
	state := ProtocolState{id, kind, Key(team, sender), Key(team, target), ProtocolPending, payload, c.now(), ""}
	if _, known := c.states[id]; !known {
		c.order = append(c.order, id)
	}
	c.states[id] = state
	overflow := len(c.order) - c.limit
	if overflow > 0 {
		victims := make(map[RequestID]bool, overflow)
		for _, id := range c.order {
			if overflow > 0 && c.states[id].Status != ProtocolPending {
				victims[id] = true
				overflow--
			}
		}
		for _, id := range c.order {
			if overflow > 0 && !victims[id] {
				victims[id] = true
				overflow--
			}
		}
		retained := make([]RequestID, 0, len(c.order)-len(victims))
		for _, id := range c.order {
			if victims[id] {
				delete(c.states, id)
			} else {
				retained = append(retained, id)
			}
		}
		c.order = retained
	}
	return state, nil
}

// Deliver retains bounded report content and records bus refusals. Instructions
// use Bus.Send directly so an oversized task is never silently changed.
func (c *Coordinator) Deliver(ctx context.Context, request SendRequest) (SendResult, error) {
	length := jsonvalue.RuneCount(request.Content)
	if length > MaxContent {
		request.Content = jsonvalue.TextPrefix(request.Content, MaxContent-200) + fmt.Sprintf("\n\n[truncated: %s characters delivered as 16,000]", decimalGrouped(length))
	}
	result, err := c.bus.Send(ctx, request)
	if err == nil && result.Status == Refused {
		c.bus.appendProblem(fmt.Sprintf("delivery to %s failed: %s", pytext.Repr(string(request.To)), result.Text))
	}
	return result, err
}
func (c *Coordinator) RequestShutdown(ctx context.Context, team TeamID, target MemberName, reason string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	member, err := c.member(Identity{team, target})
	if err != nil {
		return "", err
	}
	if member == MemberMissing {
		return "Error: no teammate " + string(target), nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	state, err := c.insert(ProtocolShutdown, team, Lead, target, reason)
	if err != nil {
		return "", err
	}
	content := reason
	if content == "" {
		content = "Please shut down."
	}
	kind := MessageShutdownRequest
	_, err = c.Deliver(ctx, SendRequest{From: state.Sender, To: state.Target, Content: content, Type: &kind, Metadata: NewMetadata(Field{"request_id", Text(string(state.RequestID))})})
	return string(state.RequestID), err
}
func (c *Coordinator) RequestPlan(ctx context.Context, team TeamID, target MemberName, task string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	member, err := c.member(Identity{team, target})
	if err != nil {
		return "", err
	}
	if member == MemberMissing {
		return "Error: no teammate " + string(target), nil
	}
	kind := MessagePlanRequest
	result, err := c.bus.Send(ctx, SendRequest{From: Key(team, Lead), To: Key(team, target), Content: "Please submit a plan for: " + task, Type: &kind})
	return result.Text, err
}
func (c *Coordinator) SubmitPlan(ctx context.Context, sender Identity, plan string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if sender.Name == Lead {
		return "Error: only teammates submit plans to the lead", nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	state, err := c.insert(ProtocolPlanApproval, sender.Team, sender.Name, Lead, plan)
	if err != nil {
		return "", err
	}
	kind := MessagePlanApprovalRequest
	_, err = c.Deliver(ctx, SendRequest{From: state.Sender, To: state.Target, Content: plan, Type: &kind, Metadata: NewMetadata(Field{"request_id", Text(string(state.RequestID))})})
	return string(state.RequestID), err
}
func (c *Coordinator) ReviewPlan(ctx context.Context, team TeamID, id RequestID, approve bool, feedback string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	state, known := c.states[id]
	if !known || state.Type != ProtocolPlanApproval {
		return "Error: no plan request " + string(id), nil
	}
	if state.Status != ProtocolPending {
		return fmt.Sprintf("Error: request %s is already %s", id, state.Status), nil
	}
	if !strings.HasPrefix(string(state.Sender), string(team)+"/") {
		return "Error: request " + string(id) + " belongs to another team", nil
	}
	state.Status = ProtocolRejected
	if approve {
		state.Status = ProtocolApproved
	}
	state.Feedback = feedback
	c.states[id] = state
	content := feedback
	if content == "" {
		content = string(state.Status)
	}
	kind := MessagePlanResponse
	_, err := c.Deliver(ctx, SendRequest{From: Key(team, Lead), To: state.Sender, Content: content, Type: &kind, Metadata: NewMetadata(Field{"request_id", Text(string(id))}, Field{"approve", Bool(approve)})})
	return fmt.Sprintf("Plan %s %s", id, state.Status), err
}
func messageField(message Message, key string, fallback Data) Data {
	if value, ok := message.data.Lookup(key); ok {
		return value
	}
	return fallback
}
func protocolID(message Message) (Data, error) {
	metadata := messageField(message, "metadata", Object())
	if metadata.Kind() != jsonvalue.Object {
		return Data{}, ErrProtocolMetadata
	}
	id, known := metadata.Lookup("request_id")
	if !known {
		id = Text("")
	}
	if id.Kind() == jsonvalue.Array || id.Kind() == jsonvalue.Object {
		return Data{}, ErrProtocolRequestID
	}
	return id, nil
}
func (c *Coordinator) match(message Message) (Data, error) {
	id, err := protocolID(message)
	if err != nil {
		return Data{}, err
	}
	text, ok := id.Text()
	if !ok {
		return id, nil
	}
	state, known := c.states[RequestID(text)]
	if !known || state.Status != ProtocolPending {
		return id, nil
	}
	kind, _ := messageField(message, "type", Text("")).Text()
	expected := MessagePlanResponse
	if state.Type == ProtocolShutdown {
		expected = MessageShutdownResponse
	}
	if kind != string(expected) {
		return id, nil
	}
	metadata := messageField(message, "metadata", Object())
	approve, _ := metadata.Lookup("approve")
	state.Status = ProtocolRejected
	if approve.Truth() {
		state.Status = ProtocolApproved
	}
	state.Feedback = messageField(message, "content", Text("")).PythonString()
	c.states[state.RequestID] = state
	return id, nil
}

type ConsumedInbox struct {
	Messages          []Message
	ShutdownRequested bool
}

// Consume drains before processing, as the source does. A later malformed row
// can return an error after earlier protocol changes/acks. Callers must retain a
// true ShutdownRequested outcome even on error; no failed batch is put back.
func (c *Coordinator) Consume(ctx context.Context, identity Identity) (ConsumedInbox, error) {
	messages, err := c.bus.Read(ctx, identity.Key())
	result := ConsumedInbox{Messages: messages}
	if err != nil {
		return result, err
	}
	member := MemberMissing
	if identity.Name != Lead {
		member, err = c.member(identity)
		if err != nil {
			return result, err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, message := range messages {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		id, err := c.match(message)
		if err != nil {
			return result, err
		}
		kind, _ := messageField(message, "type", Text("")).Text()
		if kind == string(MessageShutdownRequest) && member == MemberReady {
			response := MessageShutdownResponse
			_, err = c.Deliver(ctx, SendRequest{From: identity.Key(), To: Key(identity.Team, Lead), Content: "Shutdown approved.", Type: &response, Metadata: NewMetadata(Field{"request_id", id}, Field{"approve", Bool(true)})})
			if err != nil {
				return result, err
			}
			result.ShutdownRequested = true
		}
	}
	return result, nil
}
