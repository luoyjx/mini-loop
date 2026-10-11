package workflows

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/runmeta"
)

type retentionRecipe struct {
	Status     RunStatus
	Notice     string
	Keep       TerminalRunLimit
	NodeStatus NodeStatus `json:"node_status"`
}

type retentionSeed struct {
	Store     *InMemoryStore
	Runs      map[string]RunID
	Attempts  map[string]AttemptID
	Artifacts map[string]ArtifactID
	Inputs    map[string]CreateRunInput
	Labels    map[string]string
}

func retentionStore(t *testing.T, recipe retentionRecipe) retentionSeed {
	t.Helper()
	seed := retentionSeed{Store: NewInMemoryStore(), Runs: map[string]RunID{}, Attempts: map[string]AttemptID{}, Artifacts: map[string]ArtifactID{}, Inputs: map[string]CreateRunInput{}, Labels: map[string]string{}}
	s := seed.Store
	d, err := DecodeDefinition([]byte(`{"name":"wf","revision":"base","return_from":"a","nodes":[{"id":"a","kind":"agent"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RegisterDefinition(d); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		key     string
		session SessionID
		created float64
	}{{"a", "s", 20}, {"b", "s", 30}, {"c", "s", 10}, {"d", "foreign", 40}} {
		input := CreateRunInput{DefinitionRevision: "base", SessionID: item.session, IdempotencyKey: IdempotencyKey(item.key), Args: jsonvalue.ObjectValue(nil), RunContext: runmeta.Snapshot{MessageID: "msg", Origin: "api", Channel: "internal", Authority: runmeta.AuthorityUntrusted, StampedBy: "mini_loop", ApprovedCapabilities: []runmeta.Capability{}}}
		if item.key == "d" {
			parent := seed.Runs["a"]
			input.ParentRunID = &parent
		}
		seed.Inputs[item.key] = input
		r, err := s.CreateRun(input)
		if err != nil {
			t.Fatal(err)
		}
		seed.Runs[item.key] = r.RunID
		seed.Labels[string(r.RunID)] = "<run-" + item.key + ">"
		if _, err = s.TransitionRun(r.RunID, 0, RunRunning, nil); err != nil {
			t.Fatal(err)
		}
		attempts, err := s.ClaimNodes(r.RunID, []AttemptClaim{{NodeID: "a", AgentID: "worker", SpawnIndex: 0}}, 1)
		if err != nil {
			t.Fatal(err)
		}
		a := attempts[0]
		seed.Attempts[item.key] = a.AttemptID
		seed.Labels[string(a.AttemptID)] = "<attempt-" + item.key + ">"
		if _, err = s.StartAttempt(a.AttemptID, 0); err != nil {
			t.Fatal(err)
		}
		artifact, err := NewArtifact(ArtifactInput{Run: r.RunID, Node: "a", Attempt: a.AttemptID, Value: jsonvalue.ObjectValue([]jsonvalue.Field{{Name: "key", Value: jsonvalue.TextValue(item.key)}}), Schema: jsonvalue.ObjectValue(nil)})
		if err != nil {
			t.Fatal(err)
		}
		seed.Artifacts[item.key] = artifact.Snapshot().ArtifactID
		seed.Labels[string(artifact.Snapshot().ArtifactID)] = "<artifact-" + item.key + ">"
		if _, err = s.CommitAttempt(CommitAttemptInput{AttemptID: a.AttemptID, ExpectedVersion: 1, AttemptStatus: AttemptSucceeded, NodeStatus: NodeSucceeded, Artifact: &artifact}); err != nil {
			t.Fatal(err)
		}
		if _, err = s.FinalizeRun(r.RunID, 3, artifact.Snapshot().ArtifactID); err != nil {
			t.Fatal(err)
		}
		message := s.ListOutbox(OutboxFilter{RunID: &r.RunID})[0]
		seed.Labels[string(message.MessageID)] = "<outbox-" + item.key + ">"
		notice := "delivered"
		if item.key == "a" {
			notice = recipe.Notice
		}
		switch notice {
		case "none":
			delete(s.outbox, message.MessageID)
			delete(s.outboxKeys, outboxKey{r.RunID, message.Kind})
			s.outboxOrder = slices.DeleteFunc(s.outboxOrder, func(id OutboxID) bool { return id == message.MessageID })
		case "claimed", "delivered", "expired":
			lease, err := s.ClaimOutbox(ClaimOutboxInput{SessionID: item.session, RunIDs: []RunID{r.RunID}})
			if err != nil {
				t.Fatal(err)
			}
			seed.Labels[string(lease.Token)] = "<claim-" + item.key + ">"
			if notice == "delivered" {
				if _, err = s.AcknowledgeOutbox(SettleOutboxInput{SessionID: item.session, MessageIDs: []OutboxID{message.MessageID}, Token: lease.Token}); err != nil {
					t.Fatal(err)
				}
			} else if notice == "expired" {
				m := s.outbox[message.MessageID]
				stamp := 0.0
				m.ClaimedAt = &stamp
				s.outbox[message.MessageID] = m
			}
		}
		r = s.runs[r.RunID]
		r.CreatedAt = item.created
		r.Status = RunCompleted
		if item.key == "a" {
			r.Status = recipe.Status
		}
		if item.key == "c" {
			r.Status = RunRunning
		}
		s.runs[r.RunID] = r
		if m, ok := s.outbox[message.MessageID]; ok {
			m.CreatedAt = item.created
			s.outbox[m.MessageID] = m
		}
	}
	if recipe.NodeStatus != "" {
		key := nodeKey{seed.Runs["a"], "a"}
		n := s.nodes[key]
		n.Status = recipe.NodeStatus
		s.nodes[key] = n
	}
	return seed
}

func retentionProjection(t *testing.T, seed retentionSeed) Value {
	t.Helper()
	s := seed.Store
	var data struct {
		Runs        []WorkflowRun      `json:"runs"`
		Nodes       []NodeState        `json:"nodes"`
		Attempts    []NodeAttempt      `json:"attempts"`
		Artifacts   []ArtifactSnapshot `json:"artifacts"`
		Outbox      []OutboxSnapshot   `json:"outbox"`
		Launches    [][2]string        `json:"launches"`
		OutboxKeys  [][3]string        `json:"outbox_keys"`
		Definitions []Revision         `json:"definitions"`
		Hashes      []Digest           `json:"hashes"`
	}
	data.Runs = s.ListRuns(nil)
	for i := range data.Runs {
		data.Runs[i] = runProjection(data.Runs[i])
	}
	data.Nodes = []NodeState{}
	data.Attempts = []NodeAttempt{}
	data.Artifacts = []ArtifactSnapshot{}
	data.Launches = [][2]string{}
	data.OutboxKeys = [][3]string{}
	for _, key := range []string{"a", "b", "c", "d"} {
		id := seed.Runs[key]
		if _, ok := s.runs[id]; ok {
			n, err := s.GetNode(id, "a")
			if err != nil {
				t.Fatal(err)
			}
			data.Nodes = append(data.Nodes, n)
		}
		if a, ok := s.attempts[seed.Attempts[key]]; ok {
			a = a.Clone()
			for _, stamp := range []*float64{a.StartedAt, a.HeartbeatAt, a.EndedAt} {
				if stamp != nil {
					*stamp = 0
				}
			}
			data.Attempts = append(data.Attempts, a)
		}
		if a, ok := s.artifacts[seed.Artifacts[key]]; ok {
			v := a.Snapshot()
			v.CreatedAt = 0
			data.Artifacts = append(data.Artifacts, v)
		}
		input := seed.Inputs[key]
		if launch, ok := s.launches[launchKey{input.SessionID, input.IdempotencyKey}]; ok {
			data.Launches = append(data.Launches, [2]string{key, seed.Labels[string(launch.run)]})
		}
	}
	data.Outbox = s.ListOutbox(OutboxFilter{})
	for i := range data.Outbox {
		data.Outbox[i].CreatedAt = 0
		for _, stamp := range []*float64{data.Outbox[i].ClaimedAt, data.Outbox[i].DeliveredAt} {
			if stamp != nil {
				*stamp = 0
			}
		}
	}
	for key, id := range s.outboxKeys {
		data.OutboxKeys = append(data.OutboxKeys, [3]string{seed.Labels[string(key.run)], string(key.kind), seed.Labels[string(id)]})
	}
	slices.SortFunc(data.OutboxKeys, func(a, b [3]string) int { return slices.Compare(a[:], b[:]) })
	for revision := range s.definitions {
		data.Definitions = append(data.Definitions, revision)
	}
	for hash := range s.definitionHashes {
		data.Hashes = append(data.Hashes, hash)
	}
	slices.Sort(data.Definitions)
	slices.Sort(data.Hashes)
	body, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	v, err := jsonvalue.Decode(string(body))
	if err != nil {
		t.Fatal(err)
	}
	return normalizeStoreIDs(v, seed.Labels)
}

func checkRetentionIndexes(t *testing.T, s *InMemoryStore) {
	t.Helper()
	seenAttempts := map[AttemptID]bool{}
	for _, id := range s.attemptOrder {
		if _, ok := s.attempts[id]; !ok || seenAttempts[id] {
			t.Fatalf("dangling/duplicate attempt index %s", id)
		}
		seenAttempts[id] = true
	}
	seenOutbox := map[OutboxID]bool{}
	for _, id := range s.outboxOrder {
		if _, ok := s.outbox[id]; !ok || seenOutbox[id] {
			t.Fatalf("dangling/duplicate outbox index %s", id)
		}
		seenOutbox[id] = true
	}
	if len(seenAttempts) != len(s.attempts) || len(seenOutbox) != len(s.outbox) {
		t.Fatal("missing insertion index")
	}
	for key := range s.nodes {
		if _, ok := s.runs[key.run]; !ok {
			t.Fatal("dangling node")
		}
	}
	for _, a := range s.attempts {
		if _, ok := s.runs[a.RunID]; !ok {
			t.Fatal("dangling attempt")
		}
	}
	for _, a := range s.artifacts {
		if _, ok := s.runs[a.Snapshot().RunID]; !ok {
			t.Fatal("dangling artifact")
		}
	}
	for _, m := range s.outbox {
		if _, ok := s.runs[m.RunID]; !ok {
			t.Fatal("dangling outbox")
		}
	}
	for key, id := range s.outboxKeys {
		m, ok := s.outbox[id]
		if !ok || m.RunID != key.run || m.Kind != key.kind {
			t.Fatal("dangling outbox key")
		}
	}
	for _, launch := range s.launches {
		if _, ok := s.runs[launch.run]; !ok {
			t.Fatal("dangling launch key")
		}
	}
}

func TestRetentionMatchesActualPython(t *testing.T) {
	var fixture struct {
		Rows []struct {
			Recipe         retentionRecipe
			Pruned, Repeat []string
			OutputJSON     string    `json:"output_json"`
			FreshReplay    bool      `json:"fresh_replay"`
			ReplayStatus   RunStatus `json:"replay_status"`
			ChangedError   string    `json:"changed_error"`
		}
		Default struct {
			Limit               TerminalRunLimit
			RemovedFirst        bool `json:"removed_first"`
			Remaining           int
			RepeatCount         int  `json:"repeat_count"`
			LaunchFirstRetained bool `json:"launch_first_retained"`
		}
		Ties []RunID
	}
	body, err := os.ReadFile("../testdata/python-workflow-retention.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(body, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Rows) != 65 {
		t.Fatalf("retention corpus: %d rows", len(fixture.Rows))
	}
	for i, row := range fixture.Rows {
		t.Run(fmt.Sprintf("%02d/%s/%s/%d", i, row.Recipe.Status, row.Recipe.Notice, row.Recipe.Keep), func(t *testing.T) {
			seed := retentionStore(t, row.Recipe)
			s := seed.Store
			pruned := s.PruneTerminalRuns(&row.Recipe.Keep)
			labels := []string{}
			for _, id := range pruned {
				labels = append(labels, seed.Labels[string(id)])
			}
			if !slices.Equal(labels, row.Pruned) {
				t.Fatalf("pruned %v want %v", labels, row.Pruned)
			}
			compareAttemptProjection(t, retentionProjection(t, seed), row.OutputJSON)
			checkRetentionIndexes(t, s)
			repeat := s.PruneTerminalRuns(&row.Recipe.Keep)
			if len(repeat) != len(row.Repeat) {
				t.Fatalf("repeat %v want %v", repeat, row.Repeat)
			}
			for _, id := range pruned {
				if _, err := s.GetRun(id); err == nil {
					t.Fatal("evicted run readable")
				}
				if _, err := s.GetNode(id, "a"); err == nil {
					t.Fatal("evicted node readable")
				}
			}
			replay, err := s.CreateRun(seed.Inputs["a"])
			if err != nil {
				t.Fatal(err)
			}
			if (replay.RunID != seed.Runs["a"]) != row.FreshReplay || replay.Status != row.ReplayStatus {
				t.Fatalf("launch replay %+v fresh=%v", replay, row.FreshReplay)
			}
			input := seed.Inputs["a"]
			input.Args = jsonvalue.ObjectValue([]jsonvalue.Field{{Name: "changed", Value: jsonvalue.BoolValue(true)}})
			_, err = s.CreateRun(input)
			var se *StoreError
			if !errors.As(err, &se) || string(se.Kind) != row.ChangedError {
				t.Fatalf("changed launch %v want %s", err, row.ChangedError)
			}
			checkRetentionIndexes(t, s)
		})
	}
	t.Run("default-500", func(t *testing.T) {
		if fixture.Default.Limit != MaxTerminalRuns || !fixture.Default.RemovedFirst || fixture.Default.Remaining != 500 || fixture.Default.LaunchFirstRetained {
			t.Fatalf("source default: %+v", fixture.Default)
		}
		s := NewInMemoryStore()
		d, err := DecodeDefinition([]byte(`{"name":"wf","revision":"base","return_from":"a","nodes":[{"id":"a","kind":"agent"}]}`))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.RegisterDefinition(d); err != nil {
			t.Fatal(err)
		}
		var first RunID
		for i := 0; i <= int(MaxTerminalRuns); i++ {
			r, err := s.CreateRun(CreateRunInput{DefinitionRevision: "base", SessionID: "s", IdempotencyKey: IdempotencyKey(fmt.Sprint(i)), Args: jsonvalue.ObjectValue(nil)})
			if err != nil {
				t.Fatal(err)
			}
			if i == 0 {
				first = r.RunID
			}
			r.Status = RunFailed
			r.CreatedAt = float64(i)
			s.runs[r.RunID] = r
		}
		pruned := s.PruneTerminalRuns(nil)
		if !slices.Equal(pruned, []RunID{first}) || len(s.runs) != fixture.Default.Remaining || len(s.PruneTerminalRuns(nil)) != fixture.Default.RepeatCount {
			t.Fatalf("default eviction %v runs=%d", pruned, len(s.runs))
		}
		if _, ok := s.launches[launchKey{"s", "0"}]; ok {
			t.Fatal("default retained evicted launch")
		}
		checkRetentionIndexes(t, s)
	})
	t.Run("timestamp-ties-and-minimum-limit", func(t *testing.T) {
		s := NewInMemoryStore()
		for _, id := range []RunID{"z", "a", "b"} {
			s.runs[id] = WorkflowRun{RunID: id, CreatedAt: 1, Status: RunFailed}
		}
		keep := TerminalRunLimit(1)
		if got := s.PruneTerminalRuns(&keep); !slices.Equal(got, fixture.Ties) {
			t.Fatalf("tie ordering %v want %v", got, fixture.Ties)
		}
		keep = TerminalRunLimit(math.MinInt)
		if got := s.PruneTerminalRuns(&keep); !slices.Equal(got, []RunID{"z"}) {
			t.Fatalf("minimum keep %v", got)
		}
	})
}

func TestRetentionConcurrentAckAndPrune(t *testing.T) {
	seed := retentionStore(t, retentionRecipe{Status: RunCompleted, Notice: "claimed"})
	s, id := seed.Store, seed.Runs["a"]
	message := s.ListOutbox(OutboxFilter{RunID: &id})[0]
	keep := TerminalRunLimit(0)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.PruneTerminalRuns(&keep) }()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := s.AcknowledgeOutbox(SettleOutboxInput{SessionID: "s", MessageIDs: []OutboxID{message.MessageID}, Token: *message.ClaimToken}); err != nil {
			t.Error(err)
		}
	}()
	wg.Wait()
	s.PruneTerminalRuns(&keep)
	if _, err := s.GetRun(id); err == nil {
		t.Fatal("delivered terminal run survived final sweep")
	}
	if _, err := s.GetRun(seed.Runs["c"]); err != nil {
		t.Fatal("active run evicted")
	}
	checkRetentionIndexes(t, s)
}

func TestRetentionPinsProtectWholeGraphUntilReleased(t *testing.T) {
	seed := retentionStore(t, retentionRecipe{Status: RunCompleted, Notice: "delivered"})
	id := seed.Runs["a"]
	views, err := NewServiceViews(seed.Store)
	if err != nil {
		t.Fatal(err)
	}
	if err := views.RecordLaunchTurn(id, 2); err != nil {
		t.Fatal(err)
	}
	keep := TerminalRunLimit(0)
	views.PruneTerminalRunsExcept(&keep, []RunID{id, id, "missing"})
	if _, err := views.Status(id, nil); err != nil {
		t.Fatal("pinned graph was removed:", err)
	}
	if _, err := seed.Store.GetArtifact(seed.Artifacts["a"]); err != nil {
		t.Fatal("pinned artifact was removed:", err)
	}
	if _, ok := views.launchTurns[id]; !ok {
		t.Fatal("pinned launch bookkeeping was removed")
	}
	checkRetentionIndexes(t, seed.Store)
	views.PruneTerminalRuns(&keep)
	if _, err := seed.Store.GetRun(id); err == nil {
		t.Fatal("released terminal graph survived pruning")
	}
	if _, ok := views.launchTurns[id]; ok {
		t.Fatal("released launch bookkeeping survived pruning")
	}
	checkRetentionIndexes(t, seed.Store)
}
