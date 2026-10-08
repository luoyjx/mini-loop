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

func storeDefinition(t *testing.T) Definition {
	t.Helper()
	d, err := DecodeDefinition([]byte(`{"name":"wf","revision":"base","definition_id":"stable","return_from":"a","input_schema":{"type":"object","required":["must"]},"nodes":[{"id":"b","kind":"agent","needs":["a"]},{"id":"a","kind":"agent"}],"budget":{"max_agents":3,"max_concurrent_agents":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func storeContext() runmeta.Snapshot {
	actor := runmeta.ActorID("owner")
	return runmeta.Snapshot{MessageID: "msg_fixed", Origin: "api", ActorID: &actor, Channel: "internal", Authority: runmeta.AuthorityUntrusted, StampedBy: "mini_loop", ApprovedCapabilities: []runmeta.Capability{}}
}
func initializedStore(t *testing.T) (*InMemoryStore, WorkflowRun) {
	t.Helper()
	s := NewInMemoryStore()
	d := storeDefinition(t)
	if _, err := s.RegisterDefinition(d); err != nil {
		t.Fatal(err)
	}
	r, err := s.CreateRun(CreateRunInput{DefinitionRevision: d.Revision(), SessionID: "session", IdempotencyKey: "key", RunContext: storeContext(), Args: objectSchema()})
	if err != nil {
		t.Fatal(err)
	}
	return s, r
}

type storeCommand struct {
	Op, Run, Save, Attempt string
	Definition             Value
	Revision               Revision
	Session                *SessionID
	Key                    *IdempotencyKey
	ArgsJSON               string `json:"args_json"`
	Context                *runmeta.Snapshot
	Parent                 *RunID
	Action                 *LaunchActionID
	Policy                 Digest
	Expected               RecordVersion
	Status                 RunStatus
	Detail                 *string
	Node                   NodeID
	Claims                 []AttemptClaim
}

func runProjection(r WorkflowRun) WorkflowRun {
	r.CreatedAt = 0
	if r.StartedAt != nil {
		v := 0.0
		r.StartedAt = &v
	}
	if r.EndedAt != nil {
		v := 0.0
		r.EndedAt = &v
	}
	return r
}
func storeProjection[T Definition | WorkflowRun | NodeState | NodeAttempt | []WorkflowRun | []NodeState | []NodeAttempt](r T) (Value, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return Value{}, err
	}
	return jsonvalue.Decode(string(data))
}
func normalizeStoreIDs(v Value, labels map[string]string) Value {
	if text, ok := v.Text(); ok {
		if label, exists := labels[text]; exists {
			return jsonvalue.TextValue(label)
		}
		return v
	}
	if items, ok := v.Array(); ok {
		for i := range items {
			items[i] = normalizeStoreIDs(items[i], labels)
		}
		return jsonvalue.ArrayValue(items)
	}
	if v.Kind() == jsonvalue.Object {
		fields := []jsonvalue.Field{}
		for _, key := range v.Keys() {
			child, _ := v.Lookup(key)
			fields = append(fields, jsonvalue.Field{Name: key, Value: normalizeStoreIDs(child, labels)})
		}
		return jsonvalue.ObjectValue(fields)
	}
	return v
}
func TestStoreCoreMatchesActualPython(t *testing.T) {
	var fixture struct {
		Steps []struct {
			CommandJSON   string `json:"command_json"`
			OutputJSON    string `json:"output_json"`
			Error, Detail string
			LaunchHash    Digest `json:"launch_hash"`
		}
		Matrix []struct {
			Before, Target, Status RunStatus
			Error, Detail          string
			Version                RecordVersion
			Started, Ended         bool
			StoredError            string `json:"stored_error"`
		}
		CanonicalErrors []struct {
			ArgsJSON  string `json:"args_json"`
			ActorJSON string `json:"actor_json"`
			Error     string
		} `json:"canonical_errors"`
	}
	data, err := os.ReadFile("../testdata/python-workflow-store-core.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	s := NewInMemoryStore()
	refs := map[string]RunID{}
	labels := map[string]string{}
	attemptRefs := map[string]AttemptID{}
	runs := 0
	attempts := 0
	ref := func(label string) RunID {
		if id, ok := refs[label]; ok {
			return id
		}
		return RunID(label)
	}
	for i, row := range fixture.Steps {
		t.Run(fmt.Sprintf("step-%02d", i), func(t *testing.T) {
			var c storeCommand
			if err := json.Unmarshal([]byte(row.CommandJSON), &c); err != nil {
				t.Fatal(err)
			}
			var out Value
			var effectErr error
			switch c.Op {
			case "register":
				raw, _ := c.Definition.MarshalJSON()
				d, e := DecodeDefinition(raw)
				if e == nil {
					d, e = s.RegisterDefinition(d)
				}
				effectErr = e
				if e == nil {
					out = d.Data()
				}
			case "definition":
				d, e := s.GetDefinition(c.Revision)
				effectErr = e
				if e == nil {
					out = d.Data()
				}
			case "create":
				input := CreateRunInput{DefinitionRevision: "base", SessionID: "session", IdempotencyKey: "key", RunContext: storeContext(), ParentRunID: c.Parent, LaunchActionID: c.Action, PolicySnapshotHash: c.Policy}
				if c.Revision != "" {
					input.DefinitionRevision = c.Revision
				}
				if c.Session != nil {
					input.SessionID = *c.Session
				}
				if c.Key != nil {
					input.IdempotencyKey = *c.Key
				}
				if c.Context != nil {
					input.RunContext = *c.Context
				}
				args := c.ArgsJSON
				if args == "" {
					args = `{"choice":[true,1.0],"中文":"值"}`
				}
				input.Args, effectErr = jsonvalue.Decode(args)
				if effectErr != nil {
					t.Fatal(effectErr)
				}
				r, e := s.CreateRun(input)
				effectErr = e
				if e == nil {
					if _, ok := labels[string(r.RunID)]; !ok {
						runs++
						labels[string(r.RunID)] = fmt.Sprintf("<run-%d>", runs)
					}
					if c.Save != "" {
						refs[c.Save] = r.RunID
					}
					if got := s.launches[launchKey{input.SessionID, input.IdempotencyKey}].hash; got != row.LaunchHash {
						t.Fatalf("launch hash %s want %s", got, row.LaunchHash)
					}
					out, effectErr = storeProjection(runProjection(r))
				}
			case "run":
				r, e := s.GetRun(ref(c.Run))
				effectErr = e
				if e == nil {
					out, effectErr = storeProjection(runProjection(r))
				}
			case "runs":
				r := s.ListRuns(c.Session)
				for i := range r {
					r[i] = runProjection(r[i])
				}
				out, effectErr = storeProjection(r)
			case "nodes":
				r, e := s.ListNodes(ref(c.Run))
				effectErr = e
				if e == nil {
					out, effectErr = storeProjection(r)
				}
			case "node":
				r, e := s.GetNode(ref(c.Run), c.Node)
				effectErr = e
				if e == nil {
					out, effectErr = storeProjection(r)
				}
			case "transition":
				r, e := s.TransitionRun(ref(c.Run), c.Expected, c.Status, c.Detail)
				effectErr = e
				if e == nil {
					out, effectErr = storeProjection(runProjection(r))
				}
			case "claim":
				r, e := s.ClaimNodes(ref(c.Run), c.Claims, c.Expected)
				effectErr = e
				if e == nil {
					for _, a := range r {
						attempts++
						label := fmt.Sprintf("<attempt-%d>", attempts)
						labels[string(a.AttemptID)] = label
						attemptRefs[label] = a.AttemptID
					}
					out, effectErr = storeProjection(r)
				}
			case "attempts":
				out, effectErr = storeProjection(s.ListAttempts(ref(c.Run)))
			case "attempt":
				id, ok := attemptRefs[c.Attempt]
				if !ok {
					id = AttemptID(c.Attempt)
				}
				r, e := s.GetAttempt(id)
				effectErr = e
				if e == nil {
					out, effectErr = storeProjection(r)
				}
			default:
				t.Fatalf("unsupported fixture op %s", c.Op)
			}
			kind, detail := "", ""
			if effectErr != nil {
				var storeErr *StoreError
				var validationErr *ValidationError
				switch {
				case errors.As(effectErr, &storeErr):
					kind = string(storeErr.Kind)
				case errors.As(effectErr, &validationErr):
					kind = string(validationErr.Kind)
				default:
					t.Fatal(effectErr)
				}
				detail = effectErr.Error()
				for raw, label := range labels {
					detail = strings.ReplaceAll(detail, raw, label)
				}
			}
			if kind != row.Error || detail != row.Detail {
				t.Fatalf("%s: %s %s want %s %s", c.Op, kind, detail, row.Error, row.Detail)
			}
			want, err := jsonvalue.Decode(row.OutputJSON)
			if err != nil {
				t.Fatal(err)
			}
			actual, _ := CanonicalJSON(normalizeStoreIDs(out, labels))
			expected, _ := CanonicalJSON(want)
			if string(actual) != string(expected) {
				t.Fatalf("%s: %s want %s", c.Op, actual, expected)
			}
		})
	}
	for _, row := range fixture.Matrix {
		t.Run(string(row.Before)+"/"+string(row.Target), func(t *testing.T) {
			probe, r := initializedStore(t)
			seeded := probe.runs[r.RunID]
			seeded.Status = row.Before
			seeded.Version = 7
			old := "old"
			seeded.Error = &old
			probe.runs[r.RunID] = seeded
			newDetail := "new"
			_, err := probe.TransitionRun(r.RunID, 7, row.Target, &newDetail)
			if row.Error == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				var se *StoreError
				if !errors.As(err, &se) || string(se.Kind) != row.Error || se.Detail != row.Detail {
					t.Fatalf("%v want %s %s", err, row.Error, row.Detail)
				}
			}
			got, e := probe.GetRun(r.RunID)
			if e != nil {
				t.Fatal(e)
			}
			if got.Status != row.Status || got.Version != row.Version || (got.StartedAt != nil) != row.Started || (got.EndedAt != nil) != row.Ended || got.Error == nil || *got.Error != row.StoredError {
				t.Fatalf("matrix fold changed: %+v", got)
			}
		})
	}
	for _, row := range fixture.CanonicalErrors {
		args, e := jsonvalue.Decode(row.ArgsJSON)
		if e != nil {
			t.Fatal(e)
		}
		actor, e := jsonvalue.Decode(row.ActorJSON)
		if e != nil {
			t.Fatal(e)
		}
		text, _ := actor.Text()
		ctx := storeContext()
		ctx.MessageID = "msg"
		a := runmeta.ActorID(text)
		ctx.ActorID = &a
		_, e = launchHash(CreateRunInput{DefinitionRevision: "base", Args: args, RunContext: ctx})
		if row.Error == "ValueError" && !errors.Is(e, jsonvalue.ErrNonfinite) || row.Error == "UnicodeEncodeError" && !errors.Is(e, jsonvalue.ErrSurrogate) {
			t.Fatalf("canonical error %v want %s", e, row.Error)
		}
	}
}

func TestStoreConcurrentAdmissionAndCAS(t *testing.T) {
	s, r := initializedStore(t)
	var wg sync.WaitGroup
	ids := make(chan RunID, 24)
	errs := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, e := s.CreateRun(CreateRunInput{DefinitionRevision: "base", SessionID: "session", IdempotencyKey: "key", RunContext: storeContext(), Args: objectSchema()})
			ids <- got.RunID
			errs <- e
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	for id := range ids {
		if id != r.RunID {
			t.Fatal("duplicate launch admitted")
		}
	}
	if len(s.ListRuns(nil)) != 1 {
		t.Fatal("duplicate runs")
	}
	r, e := s.TransitionRun(r.RunID, 0, RunRunning, nil)
	if e != nil {
		t.Fatal(e)
	}
	results := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.ClaimNodes(r.RunID, []AttemptClaim{{NodeID: "a", AgentID: "worker"}}, 1)
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	winners := 0
	for e := range results {
		if e == nil {
			winners++
			continue
		}
		var se *StoreError
		if !errors.As(e, &se) || se.Kind != StoreVersionConflict {
			t.Fatal(e)
		}
	}
	if winners != 1 || len(s.ListAttempts(r.RunID)) != 1 {
		t.Fatal("claim did not have exactly one winner")
	}
}

func TestStoreDetachedStateAndOverflowAtomicity(t *testing.T) {
	s, r := initializedStore(t)
	*r.RunContext.ActorID = "other"
	r.Status = RunFailed
	stored, e := s.GetRun(r.RunID)
	if e != nil || stored.Status != RunQueued || *stored.RunContext.ActorID != "owner" {
		t.Fatal("returned launch reached stored state")
	}
	r, e = s.TransitionRun(r.RunID, 0, RunRunning, nil)
	if e != nil {
		t.Fatal(e)
	}
	n := s.nodes[nodeKey{r.RunID, "b"}]
	n.Version = math.MaxInt64
	s.nodes[nodeKey{r.RunID, "b"}] = n
	_, e = s.ClaimNodes(r.RunID, []AttemptClaim{{NodeID: "a", AgentID: "worker"}, {NodeID: "b", AgentID: "worker"}}, 1)
	var se *StoreError
	if !errors.As(e, &se) || se.Kind != StoreVersionOverflow {
		t.Fatal(e)
	}
	a, _ := s.GetNode(r.RunID, "a")
	stored, _ = s.GetRun(r.RunID)
	if a.Status != NodePending || a.Version != 0 || len(s.ListAttempts(r.RunID)) != 0 || stored.Version != 1 || stored.AttemptsUsed != 0 {
		t.Fatal("overflow partially committed claim")
	}
	seeded := s.runs[r.RunID]
	seeded.Version = math.MaxInt64
	s.runs[r.RunID] = seeded
	if _, e = s.ClaimNodes(r.RunID, nil, math.MaxInt64); !errors.As(e, &se) || se.Kind != StoreVersionOverflow {
		t.Fatal(e)
	}
	if _, e = s.TransitionRun(r.RunID, math.MaxInt64, RunCompleted, nil); !errors.As(e, &se) || se.Kind != StoreVersionOverflow {
		t.Fatal(e)
	}
	if _, e = s.TransitionRun(r.RunID, math.MaxInt64, RunRunning, nil); e != nil {
		t.Fatal("same-state no-op rejected exhausted version")
	}
}

func TestStoreInputIsolationResumeAndGeneratedIdentities(t *testing.T) {
	s, r := initializedStore(t)
	validID := func(raw, prefix string) bool {
		return strings.HasPrefix(raw, prefix) && len(raw) == len(prefix)+20 && raw[len(prefix)+12] == '4' && strings.ContainsRune("89ab", rune(raw[len(prefix)+16]))
	}
	if !validID(string(r.RunID), "wfrun_") {
		t.Fatal("run UUIDv4 projection changed")
	}
	r, err := s.TransitionRun(r.RunID, 0, RunRunning, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := *r.StartedAt
	for _, to := range []RunStatus{RunPausing, RunPaused, RunQueued, RunRunning} {
		r, err = s.TransitionRun(r.RunID, r.Version, to, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	if *r.StartedAt != first {
		t.Fatal("resume rewrote first start time")
	}
	parent := AgentID("parent")
	claims := []AttemptClaim{{NodeID: "a", AgentID: "worker", ParentAgentID: &parent}}
	got, err := s.ClaimNodes(r.RunID, claims, r.Version)
	if err != nil {
		t.Fatal(err)
	}
	if !validID(string(got[0].AttemptID), "wfatt_") {
		t.Fatal("attempt UUIDv4 projection changed")
	}
	parent = "other"
	*got[0].ParentAgentID = "returned"
	got[0].Status = AttemptFailed
	stored, err := s.GetAttempt(got[0].AttemptID)
	if err != nil || *stored.ParentAgentID != "parent" || stored.Status != AttemptClaimed {
		t.Fatal("claim pointers reached stored attempt")
	}
	nodes, err := s.ListNodes(r.RunID)
	if err != nil {
		t.Fatal(err)
	}
	nodes[1].AttemptIDs[0] = "other"
	node, err := s.GetNode(r.RunID, "a")
	if err != nil || node.AttemptIDs[0] != stored.AttemptID {
		t.Fatal("node read list reached stored state")
	}
	listed := s.ListAttempts(r.RunID)
	*listed[0].ParentAgentID = "other"
	again, err := s.GetAttempt(stored.AttemptID)
	if err != nil || *again.ParentAgentID != "parent" {
		t.Fatal("attempt list reached stored state")
	}
	ctx := storeContext()
	ctx.ApprovedCapabilities = []runmeta.Capability{"workflow.launch"}
	p := RunID("parent")
	input := CreateRunInput{DefinitionRevision: "base", SessionID: "s2", IdempotencyKey: "key", RunContext: ctx, Args: objectSchema(), ParentRunID: &p}
	created, err := s.CreateRun(input)
	if err != nil {
		t.Fatal(err)
	}
	*ctx.ActorID = "other"
	ctx.ApprovedCapabilities[0] = "other"
	p = "other"
	read, err := s.GetRun(created.RunID)
	if err != nil || *read.RunContext.ActorID != "owner" || read.RunContext.ApprovedCapabilities[0] != "workflow.launch" || *read.ParentRunID != "parent" {
		t.Fatal("launch input reached stored state")
	}
	all := s.ListRuns(nil)
	for i := range all {
		if all[i].RunID == created.RunID {
			*all[i].RunContext.ActorID = "other"
		}
	}
	read, err = s.GetRun(created.RunID)
	if err != nil || *read.RunContext.ActorID != "owner" {
		t.Fatal("run list reached stored state")
	}
}

func TestStoreListTieOrdering(t *testing.T) {
	s := NewInMemoryStore()
	s.runs["z"] = WorkflowRun{RunID: "z", CreatedAt: 1.25}
	s.runs["a"] = WorkflowRun{RunID: "a", CreatedAt: 1.25}
	if got := s.ListRuns(nil); len(got) != 2 || got[0].RunID != "a" || got[1].RunID != "z" {
		t.Fatal("run creation tie was not ordered by ID")
	}
	s.attempts["z"] = NodeAttempt{AttemptID: "z", RunID: "run", SpawnIndex: 1}
	s.attempts["a"] = NodeAttempt{AttemptID: "a", RunID: "run", SpawnIndex: 1}
	if got := s.ListAttempts("run"); len(got) != 2 || got[0].AttemptID != "a" || got[1].AttemptID != "z" {
		t.Fatal("attempt spawn tie was not ordered by ID")
	}
}
