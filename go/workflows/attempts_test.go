package workflows

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/runmeta"
)

func attemptStore(t *testing.T) (*InMemoryStore, WorkflowRun, NodeAttempt) {
	t.Helper()
	s := NewInMemoryStore()
	d, e := DecodeDefinition([]byte(`{"name":"wf","revision":"base","return_from":"a","nodes":[{"id":"a","kind":"agent"}]}`))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.RegisterDefinition(d); e != nil {
		t.Fatal(e)
	}
	r, e := s.CreateRun(CreateRunInput{DefinitionRevision: "base", SessionID: "s", IdempotencyKey: "k", Args: jsonvalue.ObjectValue(nil), RunContext: runmeta.Snapshot{MessageID: "msg", Origin: "api", Channel: "internal", Authority: runmeta.AuthorityUntrusted, StampedBy: "mini_loop", ApprovedCapabilities: []runmeta.Capability{}}})
	if e != nil {
		t.Fatal(e)
	}
	r, e = s.TransitionRun(r.RunID, 0, RunRunning, nil)
	if e != nil {
		t.Fatal(e)
	}
	as, e := s.ClaimNodes(r.RunID, []AttemptClaim{{NodeID: "a", AgentID: "worker", SpawnIndex: 0}}, 1)
	if e != nil {
		t.Fatal(e)
	}
	return s, r, as[0]
}
func attemptArtifact(t *testing.T, a NodeAttempt, foreign string, valid bool) Artifact {
	t.Helper()
	run, node, attempt := a.RunID, a.NodeID, a.AttemptID
	switch foreign {
	case "run":
		run = "foreign"
	case "node":
		node = "foreign"
	case "attempt":
		attempt = "foreign"
	}
	value, _ := jsonvalue.Decode(`{"ok":true}`)
	schema, _ := jsonvalue.Decode(`{"type":"object"}`)
	artifact, e := NewArtifact(ArtifactInput{Run: run, Node: node, Attempt: attempt, Value: value, Schema: schema, Verification: Unverified, SchemaValid: &valid})
	if e != nil {
		t.Fatal(e)
	}
	return artifact
}
func attemptProjection(t *testing.T, s *InMemoryStore, r WorkflowRun, a NodeAttempt, artifact *Artifact) Value {
	t.Helper()
	r, e := s.GetRun(r.RunID)
	if e != nil {
		t.Fatal(e)
	}
	n, e := s.GetNode(r.RunID, "a")
	if e != nil {
		t.Fatal(e)
	}
	a, e = s.GetAttempt(a.AttemptID)
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range []*float64{a.StartedAt, a.HeartbeatAt, a.EndedAt} {
		if p != nil {
			*p = 0
		}
	}
	artifacts, e := s.ArtifactsForNode(r.RunID, "a")
	if e != nil {
		t.Fatal(e)
	}
	views := []ArtifactSnapshot{}
	for _, item := range artifacts {
		view := item.Snapshot()
		view.CreatedAt = 0
		views = append(views, view)
	}
	data, e := json.Marshal(struct {
		Run       WorkflowRun        `json:"run"`
		Node      NodeState          `json:"node"`
		Attempt   NodeAttempt        `json:"attempt"`
		Artifacts []ArtifactSnapshot `json:"artifacts"`
	}{runProjection(r), n, a, views})
	if e != nil {
		t.Fatal(e)
	}
	out, e := jsonvalue.Decode(string(data))
	if e != nil {
		t.Fatal(e)
	}
	labels := map[string]string{string(r.RunID): "<run>", string(a.AttemptID): "<attempt>"}
	if artifact != nil {
		labels[string(artifact.Snapshot().ArtifactID)] = "<artifact>"
	}
	return normalizeStoreIDs(out, labels)
}
func checkAttemptError(t *testing.T, err error, kind, detail string, a NodeAttempt) {
	t.Helper()
	if kind == "" {
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	var se *StoreError
	if !errors.As(err, &se) {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(strings.ReplaceAll(se.Detail, string(a.AttemptID), "<attempt>"), string(a.RunID), "<run>")
	if string(se.Kind) != kind || text != detail {
		t.Fatalf("%s %s want %s %s", se.Kind, text, kind, detail)
	}
}
func compareAttemptProjection(t *testing.T, actual Value, expected string) {
	t.Helper()
	want, e := jsonvalue.Decode(expected)
	if e != nil {
		t.Fatal(e)
	}
	got, _ := CanonicalJSON(actual)
	golden, _ := CanonicalJSON(want)
	if string(got) != string(golden) {
		t.Fatalf("%s want %s", got, golden)
	}
}
func TestAttemptsMatchActualPython(t *testing.T) {
	var fixture struct {
		Starts []struct {
			Status        AttemptStatus
			Expected      RecordVersion
			OutputJSON    string `json:"output_json"`
			Error, Detail string
		}
		Commits []struct {
			Recipe struct {
				Name          string
				Artifact      bool
				Foreign       string
				SchemaValid   *bool `json:"schema_valid"`
				Verification  *VerificationStatus
				Expected      *RecordVersion
				Before        AttemptStatus
				NodeBefore    NodeStatus    `json:"node_before"`
				RunBefore     RunStatus     `json:"run_before"`
				AttemptStatus AttemptStatus `json:"attempt_status"`
				NodeStatus    NodeStatus    `json:"node_status"`
			}
			OutputJSON    string `json:"output_json"`
			Error, Detail string
			Repeat        struct{ Error, Detail string }
		}
	}
	data, e := os.ReadFile("../testdata/python-workflow-attempts.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(data, &fixture); e != nil {
		t.Fatal(e)
	}
	for _, row := range fixture.Starts {
		t.Run("start/"+string(row.Status)+"/"+string(rune('0'+row.Expected)), func(t *testing.T) {
			s, r, a := attemptStore(t)
			seed := s.attempts[a.AttemptID]
			seed.Status = row.Status
			s.attempts[a.AttemptID] = seed
			_, e := s.StartAttempt(a.AttemptID, row.Expected)
			checkAttemptError(t, e, row.Error, row.Detail, a)
			compareAttemptProjection(t, attemptProjection(t, s, r, a, nil), row.OutputJSON)
		})
	}
	for _, row := range fixture.Commits {
		t.Run("commit/"+row.Recipe.Name, func(t *testing.T) {
			s, r, a := attemptStore(t)
			recipe := row.Recipe
			if recipe.Before != AttemptClaimed {
				var e error
				a, e = s.StartAttempt(a.AttemptID, 0)
				if e != nil {
					t.Fatal(e)
				}
			}
			if recipe.NodeBefore != "" {
				n := s.nodes[nodeKey{r.RunID, "a"}]
				n.Status = recipe.NodeBefore
				s.nodes[nodeKey{r.RunID, "a"}] = n
			}
			if recipe.RunBefore != "" {
				stored := s.runs[r.RunID]
				stored.Status = recipe.RunBefore
				s.runs[r.RunID] = stored
			}
			input := CommitAttemptInput{AttemptID: a.AttemptID, ExpectedVersion: 1, AttemptStatus: AttemptSucceeded, NodeStatus: NodeSucceeded, Verification: recipe.Verification}
			detail := "detail"
			input.Error = &detail
			if recipe.Expected != nil {
				input.ExpectedVersion = *recipe.Expected
			}
			if recipe.AttemptStatus != "" {
				input.AttemptStatus = recipe.AttemptStatus
			}
			if recipe.NodeStatus != "" {
				input.NodeStatus = recipe.NodeStatus
			}
			if recipe.Artifact {
				valid := true
				if recipe.SchemaValid != nil {
					valid = *recipe.SchemaValid
				}
				artifact := attemptArtifact(t, a, recipe.Foreign, valid)
				input.Artifact = &artifact
			}
			_, e := s.CommitAttempt(input)
			checkAttemptError(t, e, row.Error, row.Detail, a)
			compareAttemptProjection(t, attemptProjection(t, s, r, a, input.Artifact), row.OutputJSON)
			_, e = s.CommitAttempt(input)
			checkAttemptError(t, e, row.Repeat.Error, row.Repeat.Detail, a)
		})
	}
}
func TestAttemptConcurrentCASAndOverflow(t *testing.T) {
	s, r, a := attemptStore(t)
	var wg sync.WaitGroup
	contend := func(call func() error) {
		t.Helper()
		results := make(chan error, 16)
		for i := 0; i < 16; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); results <- call() }()
		}
		wg.Wait()
		close(results)
		wins := 0
		for e := range results {
			if e == nil {
				wins++
				continue
			}
			var se *StoreError
			if !errors.As(e, &se) || se.Kind != StoreVersionConflict {
				t.Fatal(e)
			}
		}
		if wins != 1 {
			t.Fatal("CAS did not elect one winner")
		}
	}
	contend(func() error { _, e := s.StartAttempt(a.AttemptID, 0); return e })
	artifact := attemptArtifact(t, a, "", true)
	input := CommitAttemptInput{AttemptID: a.AttemptID, ExpectedVersion: 1, AttemptStatus: AttemptSucceeded, NodeStatus: NodeSucceeded, Artifact: &artifact}
	contend(func() error { _, e := s.CommitAttempt(input); return e })
	if got, e := s.ArtifactsForNode(r.RunID, "a"); e != nil || len(got) != 1 {
		t.Fatal("artifact publication duplicated")
	}
	if _, e := s.GetArtifact("missing"); e == nil {
		t.Fatal("missing artifact admitted")
	}
	probe, pr, pa := attemptStore(t)
	if _, e := probe.StartAttempt("missing", 0); e == nil {
		t.Fatal("missing attempt admitted")
	}
	if _, e := probe.StartAttempt(pa.AttemptID, 0); e != nil {
		t.Fatal(e)
	}
	n := probe.nodes[nodeKey{pr.RunID, "a"}]
	n.Version = math.MaxInt64
	probe.nodes[nodeKey{pr.RunID, "a"}] = n
	paArtifact := attemptArtifact(t, pa, "", true)
	_, e := probe.CommitAttempt(CommitAttemptInput{AttemptID: pa.AttemptID, ExpectedVersion: 1, AttemptStatus: AttemptSucceeded, NodeStatus: NodeSucceeded, Artifact: &paArtifact})
	var se *StoreError
	if !errors.As(e, &se) || se.Kind != StoreVersionOverflow {
		t.Fatal(e)
	}
	current, _ := probe.GetAttempt(pa.AttemptID)
	if current.Status != AttemptRunning || current.Version != 1 || len(probe.artifacts) != 0 {
		t.Fatal("overflow partially settled attempt")
	}
}
