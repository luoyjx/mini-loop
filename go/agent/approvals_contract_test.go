package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type approvalProjection struct {
	Type           ApprovalEventKind `json:"type"`
	ApprovalID     ApprovalID        `json:"approval_id"`
	SessionID      SessionID         `json:"session_id"`
	Tool           protocol.ToolName `json:"tool"`
	ToolUseID      string            `json:"tool_use_id"`
	Rule           string            `json:"rule"`
	Message        string            `json:"message"`
	InputPreview   string            `json:"input_preview"`
	CreatedAt      float64           `json:"created_at"`
	Kind           ApprovalKind      `json:"kind"`
	GrantCandidate []string          `json:"grant_candidate"`
	GrantProposed  bool              `json:"grant_proposed"`
	Grant          []string          `json:"grant"`
	Reason         string            `json:"reason"`
	Decision       ReviewVerdict     `json:"decision"`
	Waited         float64           `json:"waited"`
}
type approvalContracts struct {
	Candidates []struct {
		Block             protocol.Block
		Default, Proposed []string
		Banned            bool
	}
	Cases []struct {
		Name                   string
		Kind                   ApprovalKind
		Block                  protocol.Block
		Question, Action       string
		Remember               bool
		Answer, Reviewer       *string
		Masking                bool
		BrokenStore            bool `json:"broken_store"`
		Repeat                 bool
		Allowed                *bool
		Response               *string
		Second, Foreign, Twice *bool
		Writes                 []ApprovalRecord
		Events                 []approvalProjection
		Problems               int
		ReviewCalls            int64 `json:"review_calls"`
		Remaining              []approvalProjection
	}
	Secret string
}

func readApprovalContracts(t *testing.T) approvalContracts {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-approvals.json")
	if err != nil {
		t.Fatal(err)
	}
	var value approvalContracts
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}
func TestApprovalGrantCandidatesMatchPython(t *testing.T) {
	for _, fixture := range readApprovalContracts(t).Candidates {
		use, ok := fixture.Block.ToolUse()
		if !ok {
			t.Fatal("missing use")
		}
		candidate, proposed := DefaultGrantCandidate(use.Input), ProposedGrantCandidate(use.Input)
		if !reflect.DeepEqual(candidate.Tokens(), fixture.Default) || !reflect.DeepEqual(proposed.Tokens(), fixture.Proposed) || candidate.Banned() != fixture.Banned {
			t.Fatalf("%#v: %v / %v; want %v / %v", use.Input, candidate.Tokens(), proposed.Tokens(), fixture.Default, fixture.Proposed)
		}
		detached := candidate.Tokens()
		if len(detached) > 0 {
			detached[0] = "changed"
			if candidate.Tokens()[0] == "changed" {
				t.Fatal("candidate aliases tokens")
			}
		}
	}
}

type approvalStoreSpy struct {
	mu     sync.Mutex
	writes []ApprovalRecord
	broken bool
}

func (store *approvalStoreSpy) WriteApproval(_ context.Context, record ApprovalRecord) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.broken {
		return errors.New("sensitive detail")
	}
	store.writes = append(store.writes, record.Clone())
	return nil
}
func (store *approvalStoreSpy) snapshot() []ApprovalRecord {
	store.mu.Lock()
	defer store.mu.Unlock()
	values := make([]ApprovalRecord, len(store.writes))
	for i, value := range store.writes {
		values[i] = value.Clone()
	}
	return values
}

type approvalSinkSpy struct {
	mu       sync.Mutex
	events   []approvalProjection
	required chan ApprovalSnapshot
}

func newApprovalSinkSpy() *approvalSinkSpy {
	return &approvalSinkSpy{required: make(chan ApprovalSnapshot, 16)}
}
func projectApproval(event ApprovalEvent) approvalProjection {
	value := approvalProjection{Type: event.Kind()}
	if snapshot, ok := event.Required(); ok {
		value.ApprovalID, value.SessionID, value.Tool, value.ToolUseID, value.Rule, value.Message, value.InputPreview, value.CreatedAt, value.Kind, value.GrantCandidate, value.GrantProposed = snapshot.ApprovalID, snapshot.SessionID, snapshot.Tool, snapshot.ToolUseID, snapshot.Rule, snapshot.Message, snapshot.InputPreview, snapshot.CreatedAt, snapshot.Kind, snapshot.GrantCandidate.Tokens(), snapshot.GrantProposed
	}
	if id, tool, waited, ok := event.Timeout(); ok {
		value.ApprovalID, value.Tool, value.Waited = id, tool, waited
	}
	if tool, rule, grant, reason, ok := event.Grant(); ok {
		value.Tool, value.Rule, value.Grant, value.Reason = tool, rule, grant.Tokens(), reason
	}
	if tool, rule, verdict, ok := event.AutoReviewed(); ok {
		value.Tool, value.Rule, value.Decision = tool, rule, verdict
	}
	return value
}
func (sink *approvalSinkSpy) EmitApproval(_ context.Context, event ApprovalEvent) error {
	sink.mu.Lock()
	sink.events = append(sink.events, projectApproval(event))
	sink.mu.Unlock()
	if value, ok := event.Required(); ok {
		sink.required <- value
	}
	return nil
}
func (sink *approvalSinkSpy) snapshot() []approvalProjection {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return append([]approvalProjection{}, sink.events...)
}

type approvalReviewerFunc func(context.Context, ApprovalRequest) (ReviewVerdict, error)

func (review approvalReviewerFunc) ReviewApproval(ctx context.Context, request ApprovalRequest) (ReviewVerdict, error) {
	return review(ctx, request)
}

type literalApprovalRedactor struct{ secret string }

func (mask literalApprovalRedactor) MaskText(value string) string {
	return strings.ReplaceAll(value, mask.secret, "<secret-hidden>")
}
func (mask literalApprovalRedactor) MaskApprovalInput(input protocol.ToolInput) protocol.ToolInput {
	if value, ok := input.Bash(); ok {
		value.Command = mask.MaskText(value.Command)
		if value.ApprovalPrefix != nil {
			for i, text := range *value.ApprovalPrefix {
				(*value.ApprovalPrefix)[i] = mask.MaskText(text)
			}
		}
		return protocol.BashToolInput(value)
	}
	return input
}
func normalizeApprovalEvidence(writes []ApprovalRecord, events []approvalProjection) {
	identities := map[ApprovalID]ApprovalID{}
	normalized := func(id ApprovalID) ApprovalID {
		if id == "" {
			return id
		}
		if value, exists := identities[id]; exists {
			return value
		}
		value := ApprovalID(fmt.Sprintf("apr_%d", len(identities)))
		identities[id] = value
		return value
	}
	for i := range writes {
		writes[i].ApprovalID = normalized(writes[i].ApprovalID)
		writes[i].CreatedAt = 0
		if writes[i].ResolvedAt != nil {
			zero := float64(0)
			writes[i].ResolvedAt = &zero
		}
	}
	for i := range events {
		events[i].ApprovalID = normalized(events[i].ApprovalID)
		events[i].CreatedAt, events[i].Waited = 0, 0
	}
}

type approvalResult struct {
	allowed *bool
	text    *string
	err     error
}

func TestApprovalBrokerMatchesActualPythonOutcomes(t *testing.T) {
	ctx := context.Background()
	contracts := readApprovalContracts(t)
	for _, fixture := range contracts.Cases {
		t.Run(fixture.Name, func(t *testing.T) {
			store, sink := &approvalStoreSpy{broken: fixture.BrokenStore}, newApprovalSinkSpy()
			timeout := time.Second
			if fixture.Action == "timeout" {
				timeout = 5 * time.Millisecond
			}
			config := ApprovalBrokerConfig{Timeout: timeout, Store: store}
			if fixture.Masking {
				config.Redactor = literalApprovalRedactor{contracts.Secret}
			}
			var reviewCalls atomic.Int64
			if fixture.Reviewer != nil {
				config.Reviewer = approvalReviewerFunc(func(context.Context, ApprovalRequest) (ReviewVerdict, error) {
					reviewCalls.Add(1)
					switch *fixture.Reviewer {
					case "allow":
						return ReviewAllow, nil
					case "deny":
						return ReviewDeny, nil
					case "error":
						return ReviewAbstain, errors.New("sensitive detail")
					}
					return ReviewAbstain, nil
				})
			}
			broker, err := NewApprovalBroker(config)
			if err != nil {
				t.Fatal(err)
			}
			binding := gateTestAuthority(ModeInteractive)
			binding.ToolUseID = "u"
			surface, err := broker.ForSession(binding, sink)
			if err != nil {
				t.Fatal(err)
			}
			use, ok := fixture.Block.ToolUse()
			if !ok {
				t.Fatal("missing use")
			}
			invoke := func() approvalResult {
				if fixture.Kind == ApprovalQuestion {
					answer, err := surface.AskQuestion(ctx, QuestionRequest{binding, fixture.Question})
					text, ok := answer.Text()
					if ok {
						return approvalResult{text: &text, err: err}
					}
					return approvalResult{err: err}
				}
				allowed, err := surface.Approve(ctx, ApprovalRequest{binding, ToolCall{use.ID, use.Input}, "test-rule", "approval needed"})
				return approvalResult{allowed: &allowed, err: err}
			}
			done := make(chan approvalResult, 1)
			go func() { done <- invoke() }()
			var result approvalResult
			var foreign, twice *bool
			select {
			case pending := <-sink.required:
				if fixture.Action != "timeout" {
					value := broker.Resolve(pending.ApprovalID, ApprovalResolution{SessionID: "foreign", Allowed: true})
					foreign = &value
					if fixture.Action == "cancel" {
						broker.CancelSession("s")
					} else {
						broker.Resolve(pending.ApprovalID, ApprovalResolution{SessionID: "s", Allowed: fixture.Action == "allow", Remember: fixture.Remember, Answer: fixture.Answer})
					}
					value = broker.Resolve(pending.ApprovalID, ApprovalResolution{SessionID: "s", Allowed: true})
					twice = &value
				}
				select {
				case result = <-done:
				case <-time.After(2 * time.Second):
					t.Fatal("broker did not settle")
				}
			case result = <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("broker did not publish")
			}
			if result.err != nil || !reflect.DeepEqual(result.allowed, fixture.Allowed) || !reflect.DeepEqual(result.text, fixture.Response) || !reflect.DeepEqual(foreign, fixture.Foreign) || !reflect.DeepEqual(twice, fixture.Twice) {
				t.Fatalf("result %#v / foreign %v twice %v; fixture %#v", result, foreign, twice, fixture)
			}
			if fixture.Repeat {
				second := invoke()
				if second.err != nil || !reflect.DeepEqual(second.allowed, fixture.Second) {
					t.Fatal("remembered grant not used", second)
				}
			}
			actualWrites, actualEvents := store.snapshot(), sink.snapshot()
			expectedEvents := append([]approvalProjection{}, fixture.Events...)
			for i := range expectedEvents {
				expectedEvents[i].Waited = 0
			}
			normalizeApprovalEvidence(actualWrites, actualEvents)
			if !reflect.DeepEqual(actualWrites, fixture.Writes) {
				t.Fatalf("writes\n%#v\nwant\n%#v", actualWrites, fixture.Writes)
			}
			if !reflect.DeepEqual(actualEvents, expectedEvents) {
				t.Fatalf("events\n%#v\nwant\n%#v", actualEvents, expectedEvents)
			}
			if len(broker.Problems()) != fixture.Problems || reviewCalls.Load() != fixture.ReviewCalls || len(broker.List("s")) != len(fixture.Remaining) {
				t.Fatal("diagnostics/reviewer/pending mismatch", broker.Problems(), reviewCalls.Load(), broker.List("s"))
			}
			for _, problem := range broker.Problems() {
				if strings.Contains(problem, "sensitive detail") {
					t.Fatal("plugin detail entered diagnostics")
				}
			}
		})
	}
}
