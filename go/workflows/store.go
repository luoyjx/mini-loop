package workflows

import (
	"cmp"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/runmeta"
)

type StoreErrorKind string

const (
	StoreFailure             StoreErrorKind = "WorkflowStoreError"
	StoreNotFound            StoreErrorKind = "NotFoundError"
	StoreVersionConflict     StoreErrorKind = "VersionConflict"
	StoreIdempotencyConflict StoreErrorKind = "IdempotencyConflict"
	StoreInvalidTransition   StoreErrorKind = "InvalidTransition"
	StoreValueFailure        StoreErrorKind = "ValueError"
	StoreVersionOverflow     StoreErrorKind = "NativeRecordOverflow"
)

type StoreError struct {
	Kind   StoreErrorKind
	Detail string
}

func (e *StoreError) Error() string                       { return e.Detail }
func storeError(kind StoreErrorKind, detail string) error { return &StoreError{kind, detail} }

type nodeKey struct {
	run  RunID
	node NodeID
}
type launchKey struct {
	session SessionID
	key     IdempotencyKey
}
type launchEntry struct {
	hash Digest
	run  RunID
}
type storedDefinition struct {
	definition Definition
	order      []NodeID
	maxAgents  int
}

// InMemoryStore owns process-local state only. It neither authorizes launch nor
// executes workers. Callers receive detached records; definition values are immutable.
type InMemoryStore struct {
	mu               sync.Mutex
	definitions      map[Revision]storedDefinition
	definitionHashes map[Digest]Revision
	runs             map[RunID]WorkflowRun
	nodes            map[nodeKey]NodeState
	attempts         map[AttemptID]NodeAttempt
	launches         map[launchKey]launchEntry
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		definitions: map[Revision]storedDefinition{}, definitionHashes: map[Digest]Revision{},
		runs: map[RunID]WorkflowRun{}, nodes: map[nodeKey]NodeState{},
		attempts: map[AttemptID]NodeAttempt{}, launches: map[launchKey]launchEntry{},
	}
}
func (s *InMemoryStore) RegisterDefinition(d Definition) (Definition, error) {
	if err := ValidateDefinition(d); err != nil {
		return Definition{}, err
	}
	data, err := d.MarshalJSON()
	if err != nil {
		return Definition{}, err
	}
	var wire definitionWire
	if err := decodeStrict(data, &wire); err != nil {
		return Definition{}, err
	}
	order := make([]NodeID, len(wire.Nodes))
	for i, node := range wire.Nodes {
		order[i] = node.ID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if prior, ok := s.definitions[d.Revision()]; ok {
		if prior.definition.Hash() != d.Hash() {
			return Definition{}, storeError(StoreIdempotencyConflict, fmt.Sprintf("definition revision %s has different content", d.Revision()))
		}
		return prior.definition, nil
	}
	if revision, ok := s.definitionHashes[d.Hash()]; ok {
		return s.definitions[revision].definition, nil
	}
	s.definitions[d.Revision()] = storedDefinition{d, order, wire.Budget.MaxAgents}
	s.definitionHashes[d.Hash()] = d.Revision()
	return d, nil
}
func (s *InMemoryStore) GetDefinition(revision Revision) (Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.definitions[revision]; ok {
		return d.definition, nil
	}
	return Definition{}, storeError(StoreNotFound, fmt.Sprintf("definition %s not found", revision))
}

type CreateRunInput struct {
	DefinitionRevision Revision
	SessionID          SessionID
	RunContext         runmeta.Snapshot
	IdempotencyKey     IdempotencyKey
	Args               Value
	ParentRunID        *RunID
	LaunchActionID     *LaunchActionID
	PolicySnapshotHash Digest
}

func optionalRecordText[T ~string](p *T) Value {
	if p == nil {
		return jsonvalue.NullValue()
	}
	return jsonvalue.TextValue(string(*p))
}
func provenanceValue(s runmeta.Snapshot) Value {
	caps := make([]Value, len(s.ApprovedCapabilities))
	for i, capability := range s.ApprovedCapabilities {
		caps[i] = jsonvalue.TextValue(string(capability))
	}
	return jsonvalue.ObjectValue([]jsonvalue.Field{
		{Name: "message_id", Value: jsonvalue.TextValue(string(s.MessageID))},
		{Name: "origin", Value: jsonvalue.TextValue(s.Origin)},
		{Name: "actor_id", Value: optionalRecordText(s.ActorID)},
		{Name: "channel", Value: jsonvalue.TextValue(s.Channel)},
		{Name: "authority", Value: jsonvalue.TextValue(string(s.Authority))},
		{Name: "stamped_by", Value: jsonvalue.TextValue(s.StampedBy)},
		{Name: "delegated_by", Value: optionalRecordText(s.DelegatedBy)},
		{Name: "parent_message_id", Value: optionalRecordText(s.ParentMessageID)},
		{Name: "approved_capabilities", Value: jsonvalue.ArrayValue(caps)},
	})
}
func launchHash(input CreateRunInput) (Digest, error) {
	return ContentHash(jsonvalue.ObjectValue([]jsonvalue.Field{
		{Name: "definition_revision", Value: jsonvalue.TextValue(string(input.DefinitionRevision))},
		{Name: "args", Value: input.Args},
		{Name: "run_context", Value: provenanceValue(input.RunContext)},
		{Name: "parent_run_id", Value: optionalRecordText(input.ParentRunID)},
		{Name: "launch_action_id", Value: optionalRecordText(input.LaunchActionID)},
		{Name: "policy_snapshot_hash", Value: jsonvalue.TextValue(string(input.PolicySnapshotHash))},
	}), "launch")
}
func workflowID20(prefix string) (string, error) {
	var id [10]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	id[6] = id[6]&0x0f | 0x40
	id[8] = id[8]&0x3f | 0x80
	return prefix + hex.EncodeToString(id[:]), nil
}
func wallTime() float64 { return float64(time.Now().UnixNano()) / 1e9 }

func (s *InMemoryStore) CreateRun(input CreateRunInput) (WorkflowRun, error) {
	if input.SessionID == "" || input.IdempotencyKey == "" {
		return WorkflowRun{}, storeError(StoreValueFailure, "session_id and idempotency_key are required")
	}
	if _, err := s.GetDefinition(input.DefinitionRevision); err != nil {
		return WorkflowRun{}, err
	}
	if input.Args.Kind() != jsonvalue.Object {
		return WorkflowRun{}, ErrRecord
	}
	input.RunContext = input.RunContext.Clone()
	// A newly supplied source RunContext always materializes an empty tuple of
	// capabilities. Preserve archival nulls in Snapshot.Clone, but new launches
	// project the omitted capability slice as the source empty list.
	if input.RunContext.ApprovedCapabilities == nil {
		input.RunContext.ApprovedCapabilities = []runmeta.Capability{}
	}
	input.ParentRunID = recordPointer(input.ParentRunID)
	input.LaunchActionID = recordPointer(input.LaunchActionID)
	hash, err := launchHash(input)
	if err != nil {
		return WorkflowRun{}, err
	}
	key := launchKey{input.SessionID, input.IdempotencyKey}
	s.mu.Lock()
	defer s.mu.Unlock()
	if prior, ok := s.launches[key]; ok {
		if prior.hash != hash {
			return WorkflowRun{}, storeError(StoreIdempotencyConflict, "idempotency key was already used with a different payload")
		}
		return s.runs[prior.run].Clone(), nil
	}
	id, err := workflowID20("wfrun_")
	if err != nil {
		return WorkflowRun{}, err
	}
	r := WorkflowRun{RunID: RunID(id), DefinitionRevision: input.DefinitionRevision, SessionID: input.SessionID, RunContext: input.RunContext, IdempotencyKey: input.IdempotencyKey, Args: input.Args, Status: RunQueued, CreatedAt: wallTime(), ActiveNodeIDs: []NodeID{}, ParentRunID: input.ParentRunID, LaunchActionID: input.LaunchActionID, PolicySnapshotHash: input.PolicySnapshotHash}
	s.runs[r.RunID] = r
	for _, node := range s.definitions[input.DefinitionRevision].order {
		s.nodes[nodeKey{r.RunID, node}] = NodeState{RunID: r.RunID, NodeID: node, Status: NodePending, AttemptIDs: []AttemptID{}, ResultArtifactIDs: []ArtifactID{}}
	}
	s.launches[key] = launchEntry{hash, r.RunID}
	return r.Clone(), nil
}
func (s *InMemoryStore) runLocked(id RunID) (WorkflowRun, error) {
	if r, ok := s.runs[id]; ok {
		return r, nil
	}
	return WorkflowRun{}, storeError(StoreNotFound, fmt.Sprintf("run %s not found", id))
}
func (s *InMemoryStore) GetRun(id RunID) (WorkflowRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.runLocked(id)
	return r.Clone(), err
}
func (s *InMemoryStore) ListRuns(session *SessionID) []WorkflowRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []WorkflowRun{}
	for _, r := range s.runs {
		if session == nil || r.SessionID == *session {
			out = append(out, r.Clone())
		}
	}
	slices.SortFunc(out, func(a, b WorkflowRun) int {
		if order := cmp.Compare(a.CreatedAt, b.CreatedAt); order != 0 {
			return order
		}
		return cmp.Compare(a.RunID, b.RunID)
	})
	return out
}
func (s *InMemoryStore) nodeLocked(run RunID, node NodeID) (NodeState, error) {
	if n, ok := s.nodes[nodeKey{run, node}]; ok {
		return n, nil
	}
	return NodeState{}, storeError(StoreNotFound, fmt.Sprintf("node %s/%s not found", run, node))
}
func (s *InMemoryStore) GetNode(run RunID, node NodeID) (NodeState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.nodeLocked(run, node)
	return n.Clone(), err
}
func (s *InMemoryStore) ListNodes(run RunID) ([]NodeState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.runLocked(run)
	if err != nil {
		return nil, err
	}
	out := []NodeState{}
	for _, node := range s.definitions[r.DefinitionRevision].order {
		n, err := s.nodeLocked(run, node)
		if err != nil {
			return nil, err
		}
		out = append(out, n.Clone())
	}
	return out, nil
}
func transitionAllowed(from, to RunStatus) bool {
	switch from {
	case RunCreated:
		return to == RunPlanning
	case RunPlanning:
		return to == RunAwaitingApproval || to == RunFailed
	case RunAwaitingApproval:
		return to == RunQueued || to == RunRejected || to == RunCancelled
	case RunQueued:
		return to == RunRunning || to == RunCancelled
	case RunRunning:
		return to == RunPausing || to == RunWaitingApproval || to == RunCancelling || to == RunCompleted || to == RunFailed
	case RunPausing:
		return to == RunPaused || to == RunCancelling
	case RunPaused:
		return to == RunQueued || to == RunCancelled
	case RunWaitingApproval:
		return to == RunRunning || to == RunRejected || to == RunCancelled
	case RunCancelling:
		return to == RunCancelled
	}
	return false
}
func checkVersion(id RunID, actual, expected RecordVersion) error {
	if actual != expected {
		return storeError(StoreVersionConflict, fmt.Sprintf("run %s expected version %d, found %d", id, expected, actual))
	}
	return nil
}
func nextVersion(version RecordVersion) (RecordVersion, error) {
	if version == math.MaxInt64 {
		return 0, storeError(StoreVersionOverflow, "workflow record version exhausted")
	}
	return version + 1, nil
}
func (s *InMemoryStore) TransitionRun(id RunID, expected RecordVersion, to RunStatus, detail *string) (WorkflowRun, error) {
	if !to.Valid() {
		quoted := reprNames([]string{string(to)})
		return WorkflowRun{}, storeError(StoreValueFailure, quoted[1:len(quoted)-1]+" is not a valid RunStatus")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.runLocked(id)
	if err != nil {
		return WorkflowRun{}, err
	}
	if err := checkVersion(id, r.Version, expected); err != nil {
		return WorkflowRun{}, err
	}
	if r.Status == to {
		return r.Clone(), nil
	}
	if !transitionAllowed(r.Status, to) {
		return WorkflowRun{}, storeError(StoreInvalidTransition, fmt.Sprintf("%s -> %s", r.Status, to))
	}
	version, err := nextVersion(r.Version)
	if err != nil {
		return WorkflowRun{}, err
	}
	r.Status = to
	r.Version = version
	r.Error = recordPointer(detail)
	if to == RunRunning && r.StartedAt == nil {
		now := wallTime()
		r.StartedAt = &now
	}
	if to.Terminal() {
		now := wallTime()
		r.EndedAt = &now
	}
	s.runs[id] = r
	return r.Clone(), nil
}
func (s *InMemoryStore) ClaimNodes(id RunID, claims []AttemptClaim, expected RecordVersion) ([]NodeAttempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.runLocked(id)
	if err != nil {
		return nil, err
	}
	if err := checkVersion(id, r.Version, expected); err != nil {
		return nil, err
	}
	if r.Status != RunRunning {
		return nil, storeError(StoreInvalidTransition, fmt.Sprintf("cannot claim nodes while run is %s", r.Status))
	}
	d := s.definitions[r.DefinitionRevision]
	if len(claims) > d.maxAgents-int(r.AttemptsUsed) {
		return nil, storeError(StoreFailure, "workflow attempt budget exhausted")
	}
	seen := map[NodeID]bool{}
	for _, claim := range claims {
		if seen[claim.NodeID] {
			return nil, storeError(StoreFailure, "duplicate node claim")
		}
		seen[claim.NodeID] = true
	}
	for _, claim := range claims {
		n, err := s.nodeLocked(id, claim.NodeID)
		if err != nil {
			return nil, err
		}
		if n.Status != NodePending {
			return nil, storeError(StoreInvalidTransition, fmt.Sprintf("node %s is %s, not pending", claim.NodeID, n.Status))
		}
	}
	version, err := nextVersion(r.Version)
	if err != nil {
		return nil, err
	}
	// Stage allocations/version checks before any shared mutation.
	out := make([]NodeAttempt, len(claims))
	versions := make([]RecordVersion, len(claims))
	for i, claim := range claims {
		n := s.nodes[nodeKey{id, claim.NodeID}]
		versions[i], err = nextVersion(n.Version)
		if err != nil {
			return nil, err
		}
		raw, err := workflowID20("wfatt_")
		if err != nil {
			return nil, err
		}
		out[i] = NodeAttempt{AttemptID: AttemptID(raw), RunID: id, NodeID: claim.NodeID, Attempt: AttemptNumber(len(n.AttemptIDs) + 1), AgentID: claim.AgentID, SpawnIndex: claim.SpawnIndex, Status: AttemptClaimed, ParentAgentID: recordPointer(claim.ParentAgentID), VerificationStatus: NotApplicable}
	}
	for i, attempt := range out {
		key := nodeKey{id, attempt.NodeID}
		n := s.nodes[key]
		n.Status = NodeRunning
		n.Version = versions[i]
		n.AttemptIDs = append(slices.Clone(n.AttemptIDs), attempt.AttemptID)
		s.nodes[key] = n
		s.attempts[attempt.AttemptID] = attempt
		out[i] = attempt.Clone()
	}
	r.AttemptsUsed += AttemptCount(len(out))
	r.Version = version
	r.ActiveNodeIDs = []NodeID{}
	for _, node := range d.order {
		if s.nodes[nodeKey{id, node}].Status == NodeRunning {
			r.ActiveNodeIDs = append(r.ActiveNodeIDs, node)
		}
	}
	s.runs[id] = r
	return out, nil
}
func (s *InMemoryStore) GetAttempt(id AttemptID) (NodeAttempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.attempts[id]; ok {
		return a.Clone(), nil
	}
	return NodeAttempt{}, storeError(StoreNotFound, fmt.Sprintf("attempt %s not found", id))
}
func (s *InMemoryStore) ListAttempts(run RunID) []NodeAttempt {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []NodeAttempt{}
	for _, a := range s.attempts {
		if a.RunID == run {
			out = append(out, a.Clone())
		}
	}
	slices.SortFunc(out, func(a, b NodeAttempt) int {
		if order := cmp.Compare(a.SpawnIndex, b.SpawnIndex); order != 0 {
			return order
		}
		return cmp.Compare(a.AttemptID, b.AttemptID)
	})
	return out
}
