package workflows

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type outboxRecipe struct {
	Op, State         string
	Kind              OutboxKind
	Terminal, Missing bool
	Session           *SessionID
	RunIDs            *[]string `json:"run_ids"`
	LeaseSeconds      *float64  `json:"lease_seconds"`
	LeaseKind         string    `json:"lease_kind"`
	Limit             *int
	Token             ClaimToken
	Messages          []string
	MismatchB         bool `json:"mismatch_b"`
}

type outboxOutcome struct {
	Error, Detail string
	Token         *ClaimToken
	ResultJSON    string `json:"result_json"`
}

func outboxStore(t *testing.T, recipe outboxRecipe) (*InMemoryStore, map[string]RunID, map[string]OutboxID, map[string]string) {
	t.Helper()
	s := NewInMemoryStore()
	d, err := DecodeDefinition([]byte(`{"name":"wf","revision":"base","return_from":"a","nodes":[{"id":"a","kind":"agent"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RegisterDefinition(d); err != nil {
		t.Fatal(err)
	}
	runs, messages, labels := map[string]RunID{}, map[string]OutboxID{}, map[string]string{}
	for _, item := range []struct {
		key     string
		session SessionID
		created float64
	}{{"a", "s", 30}, {"b", "s", 10}, {"foreign", "foreign", 20}} {
		r, err := s.CreateRun(CreateRunInput{DefinitionRevision: "base", SessionID: item.session, IdempotencyKey: IdempotencyKey(item.key), Args: jsonvalue.ObjectValue(nil)})
		if err != nil {
			t.Fatal(err)
		}
		runs[item.key] = r.RunID
		labels[string(r.RunID)] = "<run-" + item.key + ">"
		m, err := s.EnqueueOutbox(EnqueueOutboxInput{RunID: r.RunID, Kind: OutboxKind(item.key), Payload: jsonvalue.ObjectValue([]jsonvalue.Field{{Name: "value", Value: jsonvalue.TextValue(item.key)}})})
		if err != nil {
			t.Fatal(err)
		}
		messages[item.key] = m.MessageID
		labels[string(m.MessageID)] = "<message-" + item.key + ">"
		m.CreatedAt = item.created
		s.outbox[m.MessageID] = m
	}
	if recipe.Terminal {
		r := s.runs[runs["a"]]
		r.Status = RunCompleted
		s.runs[r.RunID] = r
	}
	now := wallTime()
	for _, key := range []string{"a", "b"} {
		m := s.outbox[messages[key]]
		switch recipe.State {
		case "active", "expired", "no-time", "future", "empty-token":
			token := ClaimToken("old")
			if recipe.State == "empty-token" {
				token = ""
			}
			m.ClaimToken = &token
			if recipe.State != "no-time" {
				stamp := now - 5
				if recipe.State == "expired" {
					stamp = now - 60
				}
				if recipe.State == "future" {
					stamp = now + 60
				}
				m.ClaimedAt = &stamp
			}
		case "no-token":
			stamp := now - 5
			m.ClaimedAt = &stamp
		case "delivered":
			stamp := 1.0
			m.DeliveredAt = &stamp
		}
		if key == "b" && recipe.MismatchB {
			token := ClaimToken("other")
			m.ClaimToken = &token
		}
		s.outbox[m.MessageID] = m
	}
	return s, runs, messages, labels
}

func outboxValue(t *testing.T, messages []OutboxSnapshot, labels map[string]string) Value {
	t.Helper()
	views := make([]OutboxSnapshot, len(messages))
	for i, m := range messages {
		views[i] = m.Clone()
		views[i].CreatedAt = 0
		if views[i].ClaimedAt != nil {
			*views[i].ClaimedAt = 0
		}
		if views[i].DeliveredAt != nil {
			*views[i].DeliveredAt = 0
		}
	}
	data, err := json.Marshal(views)
	if err != nil {
		t.Fatal(err)
	}
	v, err := jsonvalue.Decode(string(data))
	if err != nil {
		t.Fatal(err)
	}
	return normalizeStoreIDs(v, labels)
}

func invokeOutbox(t *testing.T, s *InMemoryStore, runs map[string]RunID, messages map[string]OutboxID, labels map[string]string, recipe outboxRecipe, repeated bool) (outboxOutcome, Value) {
	t.Helper()
	var err error
	out := outboxOutcome{}
	result := []OutboxSnapshot{}
	session := SessionID("s")
	if recipe.Session != nil {
		session = *recipe.Session
	}
	switch recipe.Op {
	case "enqueue":
		id := runs["a"]
		if recipe.Missing {
			id = "missing"
		}
		var m OutboxSnapshot
		m, err = s.EnqueueOutbox(EnqueueOutboxInput{RunID: id, Kind: recipe.Kind, Payload: jsonvalue.ObjectValue([]jsonvalue.Field{{Name: "changed", Value: jsonvalue.BoolValue(true)}})})
		if err == nil {
			if _, ok := labels[string(m.MessageID)]; !ok {
				labels[string(m.MessageID)] = "<message-new>"
			}
			result = append(result, m)
		}
	case "claim":
		input := ClaimOutboxInput{SessionID: session, LeaseSeconds: recipe.LeaseSeconds, Limit: recipe.Limit}
		if recipe.RunIDs != nil {
			input.RunIDs = []RunID{}
			for _, key := range *recipe.RunIDs {
				input.RunIDs = append(input.RunIDs, runs[key])
			}
		}
		if recipe.LeaseKind != "" {
			x := math.NaN()
			if recipe.LeaseKind == "inf" {
				x = math.Inf(1)
			}
			input.LeaseSeconds = &x
		}
		var lease OutboxLease
		lease, err = s.ClaimOutbox(input)
		if err == nil {
			if !strings.HasPrefix(string(lease.Token), "wfclaim_") || len(lease.Token) != 28 {
				t.Fatalf("claim token: %q", lease.Token)
			}
			label := "<claim>"
			if repeated {
				label = "<claim-repeat>"
			}
			labels[string(lease.Token)] = label
			token := ClaimToken(label)
			out.Token = &token
			result = lease.Messages
		}
	case "ack", "release":
		input := SettleOutboxInput{SessionID: session, Token: recipe.Token}
		for _, key := range recipe.Messages {
			id, ok := messages[key]
			if !ok {
				id = "missing"
			}
			input.MessageIDs = append(input.MessageIDs, id)
		}
		if recipe.Op == "ack" {
			result, err = s.AcknowledgeOutbox(input)
		} else {
			err = s.ReleaseOutbox(input)
		}
	default:
		t.Fatalf("unknown outbox operation %q", recipe.Op)
	}
	if err != nil {
		var se *StoreError
		if !errors.As(err, &se) {
			t.Fatal(err)
		}
		out.Error, out.Detail = string(se.Kind), se.Detail
		for raw, label := range labels {
			out.Detail = strings.ReplaceAll(out.Detail, raw, label)
		}
		result = []OutboxSnapshot{}
	}
	return out, outboxValue(t, result, labels)
}

func checkOutboxOutcome(t *testing.T, actual, expected outboxOutcome, result Value) {
	t.Helper()
	if actual.Error != expected.Error || actual.Detail != expected.Detail {
		t.Fatalf("outcome %+v want %+v", actual, expected)
	}
	if (actual.Token == nil) != (expected.Token == nil) || (actual.Token != nil && *actual.Token != *expected.Token) {
		t.Fatalf("token %+v want %+v", actual.Token, expected.Token)
	}
	compareAttemptProjection(t, result, expected.ResultJSON)
}

func TestOutboxMatchesActualPython(t *testing.T) {
	var fixture struct {
		Rows []struct {
			Recipe outboxRecipe
			outboxOutcome
			OutputJSON string `json:"output_json"`
			Repeat     outboxOutcome
			RepeatJSON string `json:"repeat_json"`
		}
	}
	data, err := os.ReadFile("../testdata/python-workflow-outbox.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Rows) != 106 {
		t.Fatalf("outbox corpus has %d rows", len(fixture.Rows))
	}
	for i, row := range fixture.Rows {
		t.Run(fmt.Sprintf("%03d/%s/%s", i, row.Recipe.Op, row.Recipe.State), func(t *testing.T) {
			s, runs, messages, labels := outboxStore(t, row.Recipe)
			out, result := invokeOutbox(t, s, runs, messages, labels, row.Recipe, false)
			checkOutboxOutcome(t, out, row.outboxOutcome, result)
			compareAttemptProjection(t, outboxValue(t, s.ListOutbox(OutboxFilter{}), labels), row.OutputJSON)
			out, result = invokeOutbox(t, s, runs, messages, labels, row.Recipe, true)
			checkOutboxOutcome(t, out, row.Repeat, result)
			compareAttemptProjection(t, outboxValue(t, s.ListOutbox(OutboxFilter{}), labels), row.RepeatJSON)
		})
	}
}

func TestOutboxConcurrentClaimAndLeaseFencing(t *testing.T) {
	s, _, _, _ := outboxStore(t, outboxRecipe{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	claims := map[OutboxID]ClaimToken{}
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			limit := 1
			lease, err := s.ClaimOutbox(ClaimOutboxInput{SessionID: "s", Limit: &limit})
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, m := range lease.Messages {
				if _, ok := claims[m.MessageID]; ok {
					t.Errorf("duplicate lease %s", m.MessageID)
				}
				claims[m.MessageID] = lease.Token
			}
		}()
	}
	wg.Wait()
	if len(claims) != 2 {
		t.Fatalf("claimed %d messages", len(claims))
	}
	var id OutboxID
	var old ClaimToken
	for message, token := range claims {
		id, old = message, token
		break
	}
	m := s.outbox[id]
	stamp := wallTime() - 60
	m.ClaimedAt = &stamp
	s.outbox[id] = m
	lease, err := s.ClaimOutbox(ClaimOutboxInput{SessionID: "s"})
	if err != nil || len(lease.Messages) != 1 || lease.Messages[0].MessageID != id || lease.Token == old {
		t.Fatalf("expired re-lease: %+v %v", lease, err)
	}
	for _, ack := range []bool{false, true} {
		input := SettleOutboxInput{SessionID: "s", MessageIDs: []OutboxID{id}, Token: old}
		var err error
		if ack {
			_, err = s.AcknowledgeOutbox(input)
		} else {
			err = s.ReleaseOutbox(input)
		}
		var se *StoreError
		if !errors.As(err, &se) || se.Kind != StoreVersionConflict {
			t.Fatalf("stale token settled message: %v", err)
		}
	}
	if s.outbox[id].DeliveredAt != nil || *s.outbox[id].ClaimToken != lease.Token {
		t.Fatal("stale token changed current lease")
	}
	*lease.Messages[0].ClaimedAt = 0
	*lease.Messages[0].ClaimToken = "changed"
	if *s.outbox[id].ClaimedAt == 0 || *s.outbox[id].ClaimToken != lease.Token {
		t.Fatal("lease projection alias")
	}
	ack, err := s.AcknowledgeOutbox(SettleOutboxInput{SessionID: "s", MessageIDs: []OutboxID{id}, Token: lease.Token})
	if err != nil || len(ack) != 1 || ack[0].DeliveredAt == nil {
		t.Fatalf("current lease acknowledgment: %+v %v", ack, err)
	}
	*ack[0].DeliveredAt = 0
	if *s.outbox[id].DeliveredAt == 0 {
		t.Fatal("acknowledgment timestamp alias")
	}
}

func TestOutboxEnqueueBoundaryAndFinalizeIndex(t *testing.T) {
	s, id, artifact, _ := completionStore(t, completionRecipe{Op: "finalize", Status: RunRunning, NodeStatus: NodeSucceeded})
	if _, err := s.FinalizeRun(id, 0, artifact); err != nil {
		t.Fatal(err)
	}
	// A replay keeps the finalization payload and ignores the new invalid payload.
	m, err := s.EnqueueOutbox(EnqueueOutboxInput{RunID: id, Kind: WorkflowCompleted, Payload: jsonvalue.NullValue()})
	if err != nil || len(s.outboxOrder) != 1 {
		t.Fatalf("finalization key/index replay: %+v %v", m, err)
	}
	if _, ok := m.Payload.Lookup("artifact_id"); !ok {
		t.Fatal("replacement payload overwrote completion")
	}
	if _, err = s.EnqueueOutbox(EnqueueOutboxInput{RunID: id, Kind: "new", Payload: jsonvalue.NullValue()}); err == nil {
		t.Fatal("new non-object payload admitted")
	}
	if len(s.outboxOrder) != 1 {
		t.Fatal("payload refusal published index")
	}
	lease, err := s.ClaimOutbox(ClaimOutboxInput{SessionID: "s"})
	if err != nil || len(lease.Messages) != 1 || lease.Messages[0].MessageID != m.MessageID {
		t.Fatalf("completion claim index: %+v %v", lease, err)
	}
}

func TestOutboxConcurrentEnqueueAndReleaseRetry(t *testing.T) {
	s, runs, _, _ := outboxStore(t, outboxRecipe{})
	var wg sync.WaitGroup
	ids := make(chan OutboxID, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, err := s.EnqueueOutbox(EnqueueOutboxInput{RunID: runs["a"], Kind: "notification", Payload: jsonvalue.ObjectValue(nil)})
			if err != nil {
				t.Error(err)
				return
			}
			ids <- m.MessageID
		}()
	}
	wg.Wait()
	close(ids)
	var single OutboxID
	for id := range ids {
		if single == "" {
			single = id
		}
		if single != id {
			t.Fatal("concurrent enqueue created duplicate run/kind messages")
		}
	}
	if single == "" || len(s.outboxOrder) != 4 || len(s.outboxKeys) != 4 {
		t.Fatal("enqueue indexes disagree")
	}
	lease, err := s.ClaimOutbox(ClaimOutboxInput{SessionID: "s", RunIDs: []RunID{runs["a"]}})
	if err != nil || len(lease.Messages) != 2 {
		t.Fatalf("filtered lease: %+v %v", lease, err)
	}
	selected := []OutboxID{}
	for _, m := range lease.Messages {
		selected = append(selected, m.MessageID)
	}
	input := SettleOutboxInput{SessionID: "s", MessageIDs: selected, Token: lease.Token}
	if err := s.ReleaseOutbox(input); err != nil {
		t.Fatal(err)
	}
	for _, id := range selected {
		if m := s.outbox[id]; m.ClaimToken != nil || m.ClaimedAt != nil || m.DeliveredAt != nil {
			t.Fatalf("release marked delivery: %+v", m)
		}
	}
	retry, err := s.ClaimOutbox(ClaimOutboxInput{SessionID: "s", RunIDs: []RunID{runs["a"]}})
	if err != nil || len(retry.Messages) != 2 || retry.Token == lease.Token {
		t.Fatalf("retry lease: %+v %v", retry, err)
	}
	input.Token = retry.Token
	if _, err := s.AcknowledgeOutbox(input); err != nil {
		t.Fatal(err)
	}
	again, err := s.ClaimOutbox(ClaimOutboxInput{SessionID: "s", RunIDs: []RunID{runs["a"]}})
	if err != nil || len(again.Messages) != 0 {
		t.Fatalf("delivered messages reclaimed: %+v %v", again, err)
	}
}
