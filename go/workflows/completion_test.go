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
	"github.com/luoyjx/mini-loop/go/runmeta"
)

type completionRecipe struct {
	Op                                      string
	Status                                  RunStatus
	Active, Stale, Foreign, Missing, Claims bool
	NoArtifact                              bool          `json:"no_artifact"`
	NodeStatus                              NodeStatus    `json:"node_status"`
	AttemptStatus                           AttemptStatus `json:"attempt_status"`
	BadNode                                 NodeID        `json:"bad_node"`
	Reason                                  *string
	PriorReason                             *string `json:"prior_reason"`
}

type completionOutcome struct {
	Error, Detail string
	Cancelled     []AttemptID
}

func completionStore(t *testing.T, recipe completionRecipe) (*InMemoryStore, RunID, ArtifactID, map[string]string) {
	t.Helper()
	s := NewInMemoryStore()
	d, err := DecodeDefinition([]byte(`{"name":"wf","revision":"base","return_from":"a","nodes":[{"id":"a","kind":"agent"},{"id":"b","kind":"agent"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RegisterDefinition(d); err != nil {
		t.Fatal(err)
	}
	r, err := s.CreateRun(CreateRunInput{DefinitionRevision: "base", SessionID: "s", IdempotencyKey: "k", Args: jsonvalue.ObjectValue(nil), RunContext: runmeta.Snapshot{MessageID: "msg", Origin: "api", Channel: "internal", Authority: runmeta.AuthorityUntrusted, StampedBy: "mini_loop", ApprovedCapabilities: []runmeta.Capability{}}})
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{string(r.RunID): "<run>"}
	if recipe.Claims {
		r.Status = RunRunning
		s.runs[r.RunID] = r
		attempts, err := s.ClaimNodes(r.RunID, []AttemptClaim{{NodeID: "b", AgentID: "worker-b", SpawnIndex: 9}, {NodeID: "a", AgentID: "worker-a", SpawnIndex: 1}}, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range attempts {
			labels[string(a.AttemptID)] = "<attempt-" + string(a.NodeID) + ">"
		}
		a := attempts[1]
		a.Status = recipe.AttemptStatus
		if a.Status == "" {
			a.Status = AttemptClaimed
		}
		s.attempts[a.AttemptID] = a
		if recipe.BadNode != "" {
			key := nodeKey{r.RunID, recipe.BadNode}
			n := s.nodes[key]
			n.Status = NodeFailed
			s.nodes[key] = n
		}
	}
	r = s.runs[r.RunID]
	r.Status = recipe.Status
	r.CancelReason = recordPointer(recipe.PriorReason)
	if recipe.Active {
		key := nodeKey{r.RunID, "a"}
		n := s.nodes[key]
		n.Status = NodeRunning
		s.nodes[key] = n
		r.ActiveNodeIDs = []NodeID{"a"}
	}
	s.runs[r.RunID] = r
	var artifactID ArtifactID
	if recipe.Op == "finalize" {
		status := recipe.NodeStatus
		if status == "" {
			status = NodeSucceeded
		}
		for key, n := range s.nodes {
			n.Status = status
			s.nodes[key] = n
		}
		owner := r.RunID
		if recipe.Foreign {
			owner = "foreign"
		}
		valid := false
		a, err := NewArtifact(ArtifactInput{Run: owner, Node: "b", Attempt: "seed-attempt", Value: jsonvalue.ObjectValue([]jsonvalue.Field{{Name: "ok", Value: jsonvalue.BoolValue(true)}}), Schema: jsonvalue.ObjectValue(nil), SchemaValid: &valid, Verification: Refuted})
		if err != nil {
			t.Fatal(err)
		}
		artifactID = a.Snapshot().ArtifactID
		labels[string(artifactID)] = "<artifact>"
		if !recipe.NoArtifact {
			s.artifacts[artifactID] = a
		}
	}
	return s, r.RunID, artifactID, labels
}

func completionProjection(t *testing.T, s *InMemoryStore, id RunID, labels map[string]string) Value {
	t.Helper()
	r, err := s.GetRun(id)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := s.ListNodes(id)
	if err != nil {
		t.Fatal(err)
	}
	attempts := s.ListAttempts(id)
	for i := range attempts {
		for _, stamp := range []*float64{attempts[i].StartedAt, attempts[i].HeartbeatAt, attempts[i].EndedAt} {
			if stamp != nil {
				*stamp = 0
			}
		}
	}
	messages := s.ListOutbox(OutboxFilter{RunID: &id})
	for i := range messages {
		labels[string(messages[i].MessageID)] = "<outbox>"
		messages[i].CreatedAt = 0
	}
	data, err := json.Marshal(struct {
		Run      WorkflowRun      `json:"run"`
		Nodes    []NodeState      `json:"nodes"`
		Attempts []NodeAttempt    `json:"attempts"`
		Outbox   []OutboxSnapshot `json:"outbox"`
	}{runProjection(r), nodes, attempts, messages})
	if err != nil {
		t.Fatal(err)
	}
	v, err := jsonvalue.Decode(string(data))
	if err != nil {
		t.Fatal(err)
	}
	return normalizeStoreIDs(v, labels)
}

func invokeCompletion(t *testing.T, s *InMemoryStore, id RunID, artifact ArtifactID, recipe completionRecipe, repeated bool, labels map[string]string) completionOutcome {
	t.Helper()
	r, err := s.GetRun(id)
	if err != nil {
		t.Fatal(err)
	}
	version := r.Version
	if recipe.Stale && !repeated {
		version++
	}
	target := id
	if recipe.Missing {
		target = "missing"
	}
	reason := recipe.Reason
	if repeated {
		text := "second"
		reason = &text
	}
	var cancelled []NodeAttempt
	switch recipe.Op {
	case "request":
		_, err = s.RequestCancel(target, version, reason)
	case "finish":
		_, err = s.FinishCancellation(target)
	case "fail":
		_, err = s.FailRun(target, "failure")
	case "finalize":
		_, err = s.FinalizeRun(target, version, artifact)
	case "cancel_claimed":
		cancelled, err = s.CancelClaimedAttempts(target, reason)
	default:
		t.Fatalf("unknown completion operation %q", recipe.Op)
	}
	out := completionOutcome{Cancelled: []AttemptID{}}
	if err != nil {
		var storeErr *StoreError
		if !errors.As(err, &storeErr) {
			t.Fatal(err)
		}
		out.Error, out.Detail = string(storeErr.Kind), storeErr.Detail
		for raw, label := range labels {
			out.Detail = strings.ReplaceAll(out.Detail, raw, label)
		}
	} else {
		for _, a := range cancelled {
			out.Cancelled = append(out.Cancelled, AttemptID(labels[string(a.AttemptID)]))
		}
	}
	return out
}

func TestWorkflowCompletionMatchesActualPython(t *testing.T) {
	var fixture struct {
		Rows []struct {
			Recipe completionRecipe
			completionOutcome
			OutputJSON string `json:"output_json"`
			Repeat     completionOutcome
			RepeatJSON string `json:"repeat_json"`
		}
	}
	data, err := os.ReadFile("../testdata/python-workflow-completion.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Rows) != 131 {
		t.Fatalf("expected full completion corpus, got %d", len(fixture.Rows))
	}
	for i, row := range fixture.Rows {
		t.Run(fmt.Sprintf("%03d/%s/%s", i, row.Recipe.Op, row.Recipe.Status), func(t *testing.T) {
			s, id, artifact, labels := completionStore(t, row.Recipe)
			out := invokeCompletion(t, s, id, artifact, row.Recipe, false, labels)
			if out.Error != row.Error || out.Detail != row.Detail || !equalAttemptIDs(out.Cancelled, row.Cancelled) {
				t.Fatalf("first outcome: %+v want %+v", out, row.completionOutcome)
			}
			compareAttemptProjection(t, completionProjection(t, s, id, labels), row.OutputJSON)
			out = invokeCompletion(t, s, id, artifact, row.Recipe, true, labels)
			if out.Error != row.Repeat.Error || out.Detail != row.Repeat.Detail || !equalAttemptIDs(out.Cancelled, row.Repeat.Cancelled) {
				t.Fatalf("repeat outcome: %+v want %+v", out, row.Repeat)
			}
			compareAttemptProjection(t, completionProjection(t, s, id, labels), row.RepeatJSON)
		})
	}
}

func equalAttemptIDs(a, b []AttemptID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestWorkflowCompletionConcurrentFinalize(t *testing.T) {
	s, id, artifact, _ := completionStore(t, completionRecipe{Op: "finalize", Status: RunRunning, NodeStatus: NodeSucceeded})
	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.FinalizeRun(id, 0, artifact)
			if err == nil {
				mu.Lock()
				winners++
				mu.Unlock()
				return
			}
			var se *StoreError
			if !errors.As(err, &se) || se.Kind != StoreVersionConflict {
				t.Errorf("losing finalization: %v", err)
			}
		}()
	}
	wg.Wait()
	if winners != 1 || len(s.outbox) != 1 || len(s.outboxKeys) != 1 {
		t.Fatalf("winners=%d outbox=%d keys=%d", winners, len(s.outbox), len(s.outboxKeys))
	}
	messages := s.ListOutbox(OutboxFilter{UndeliveredOnly: true})
	if len(messages) != 1 || !strings.HasPrefix(string(messages[0].MessageID), "wfout_") || len(messages[0].MessageID) != 26 {
		t.Fatalf("completion outbox: %+v", messages)
	}
	foreign := SessionID("foreign")
	if len(s.ListOutbox(OutboxFilter{SessionID: &foreign})) != 0 {
		t.Fatal("foreign session filter leaked message")
	}
	foreignRun := RunID("foreign")
	if len(s.ListOutbox(OutboxFilter{RunID: &foreignRun})) != 0 {
		t.Fatal("foreign run filter leaked message")
	}
	stamp := 1.0
	messages[0].DeliveredAt = &stamp
	if s.ListOutbox(OutboxFilter{})[0].DeliveredAt != nil {
		t.Fatal("read mutated stored delivery state")
	}
}

func TestWorkflowCompletionOverflowAdmission(t *testing.T) {
	for _, op := range []string{"request", "finish", "finalize", "cancel_claimed"} {
		t.Run(op, func(t *testing.T) {
			recipe := completionRecipe{Op: op, Status: RunRunning, Claims: op == "cancel_claimed", NodeStatus: NodeSucceeded}
			if op == "finish" {
				recipe.Status = RunCancelling
			}
			s, id, artifact, labels := completionStore(t, recipe)
			r := s.runs[id]
			r.Version = RecordVersion(math.MaxInt64)
			s.runs[id] = r
			before := completionProjection(t, s, id, labels)
			out := invokeCompletion(t, s, id, artifact, recipe, false, labels)
			if out.Error != string(StoreVersionOverflow) {
				t.Fatalf("overflow outcome: %+v", out)
			}
			beforeJSON, err := CanonicalJSON(before)
			if err != nil {
				t.Fatal(err)
			}
			compareAttemptProjection(t, completionProjection(t, s, id, labels), string(beforeJSON))
		})
	}
}

func TestWorkflowCancellationStartedAndUnstartedTasks(t *testing.T) {
	s, id, _, _ := completionStore(t, completionRecipe{Op: "request", Status: RunRunning, Claims: true})
	var started NodeAttempt
	for _, a := range s.ListAttempts(id) {
		if a.NodeID == "a" {
			var err error
			started, err = s.StartAttempt(a.AttemptID, 0)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.RequestCancel(id, 1, nil)
			if err == nil {
				if r.Status != RunCancelling {
					t.Errorf("active cancellation: %s", r.Status)
				}
				mu.Lock()
				winners++
				mu.Unlock()
				return
			}
			var se *StoreError
			if !errors.As(err, &se) || se.Kind != StoreVersionConflict {
				t.Errorf("cancel CAS: %v", err)
			}
		}()
	}
	wg.Wait()
	if winners != 1 {
		t.Fatalf("cancel CAS winners=%d", winners)
	}
	cancelled, err := s.CancelClaimedAttempts(id, nil)
	if err != nil || len(cancelled) != 1 || cancelled[0].NodeID != "b" {
		t.Fatalf("unstarted settlement: %+v %v", cancelled, err)
	}
	if _, err = s.FinishCancellation(id); err == nil {
		t.Fatal("finished while a task was running")
	}
	if _, err = s.CommitAttempt(CommitAttemptInput{AttemptID: started.AttemptID, ExpectedVersion: 1, AttemptStatus: AttemptCancelled, NodeStatus: NodeCancelled}); err != nil {
		t.Fatal(err)
	}
	r, err := s.FinishCancellation(id)
	if err != nil || r.Status != RunCancelled || r.Version != 5 || len(r.ActiveNodeIDs) != 0 {
		t.Fatalf("finished cancellation: %+v %v", r, err)
	}
	if len(s.ListOutbox(OutboxFilter{})) != 0 {
		t.Fatal("cancellation created a completion notification")
	}
	if again, err := s.CancelClaimedAttempts(id, nil); err != nil || len(again) != 0 {
		t.Fatalf("repeated unstarted settlement: %+v %v", again, err)
	}
}

func TestWorkflowCancellationCounterPreflight(t *testing.T) {
	for _, target := range []string{"second-run-increment", "pending-node", "claimed-node", "claimed-attempt"} {
		t.Run(target, func(t *testing.T) {
			recipe := completionRecipe{Op: "request", Status: RunRunning}
			if strings.HasPrefix(target, "claimed") {
				recipe.Op = "cancel_claimed"
				recipe.Claims = true
			}
			s, id, artifact, labels := completionStore(t, recipe)
			switch target {
			case "second-run-increment":
				r := s.runs[id]
				r.Version = RecordVersion(math.MaxInt64 - 1)
				s.runs[id] = r
			case "pending-node", "claimed-node":
				key := nodeKey{id, "a"}
				n := s.nodes[key]
				n.Version = RecordVersion(math.MaxInt64)
				s.nodes[key] = n
			case "claimed-attempt":
				aid := s.attemptOrder[1]
				a := s.attempts[aid]
				a.Version = RecordVersion(math.MaxInt64)
				s.attempts[aid] = a
			}
			before, err := CanonicalJSON(completionProjection(t, s, id, labels))
			if err != nil {
				t.Fatal(err)
			}
			out := invokeCompletion(t, s, id, artifact, recipe, false, labels)
			if out.Error != string(StoreVersionOverflow) {
				t.Fatalf("counter admission: %+v", out)
			}
			compareAttemptProjection(t, completionProjection(t, s, id, labels), string(before))
		})
	}
}

func TestWorkflowOutboxReadOrderAndDeliveryFilter(t *testing.T) {
	s := NewInMemoryStore()
	stamp := 3.0
	for _, m := range []OutboxSnapshot{
		{MessageID: "z", RunID: "r", SessionID: "s", CreatedAt: 1, Payload: jsonvalue.ObjectValue(nil)},
		{MessageID: "a", RunID: "r", SessionID: "s", CreatedAt: 1, DeliveredAt: &stamp, Payload: jsonvalue.ObjectValue(nil)},
		{MessageID: "b", RunID: "foreign", SessionID: "foreign", CreatedAt: 0, Payload: jsonvalue.ObjectValue(nil)},
	} {
		s.outbox[m.MessageID] = m
	}
	id, session := RunID("r"), SessionID("s")
	messages := s.ListOutbox(OutboxFilter{RunID: &id, SessionID: &session})
	if len(messages) != 2 || messages[0].MessageID != "a" || messages[1].MessageID != "z" {
		t.Fatalf("tie order/filter: %+v", messages)
	}
	*messages[0].DeliveredAt = 99
	if *s.ListOutbox(OutboxFilter{RunID: &id})[0].DeliveredAt != 3 {
		t.Fatal("delivery timestamp alias")
	}
	messages = s.ListOutbox(OutboxFilter{RunID: &id, SessionID: &session, UndeliveredOnly: true})
	if len(messages) != 1 || messages[0].MessageID != "z" {
		t.Fatalf("undelivered filter: %+v", messages)
	}
}
