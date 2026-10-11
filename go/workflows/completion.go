package workflows

import (
	"fmt"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

const WorkflowCompleted OutboxKind = "workflow_completed"

// CancelClaimedAttempts settles tasks that never started, in claim insertion order.
// Nil detail selects the source default; an explicit empty string is retained.
func (s *InMemoryStore) CancelClaimedAttempts(id RunID, detail *string) ([]NodeAttempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.runLocked(id)
	if err != nil {
		return nil, err
	}
	message := "cancelled before start"
	if detail != nil {
		message = *detail
	}
	out := []NodeAttempt{}
	// Native bounded-counter admission precedes publication. A later source
	// node-status refusal still retains earlier cancellations, as Python does.
	for _, aid := range s.attemptOrder {
		a := s.attempts[aid]
		if a.RunID != id || a.Status != AttemptClaimed {
			continue
		}
		if _, err := nextVersion(a.Version); err != nil {
			return nil, err
		}
		if _, err := nextVersion(s.nodes[nodeKey{id, a.NodeID}].Version); err != nil {
			return nil, err
		}
		if _, err := nextVersion(r.Version); err != nil {
			return nil, err
		}
	}
	now := wallTime()
	for _, aid := range s.attemptOrder {
		a := s.attempts[aid]
		if a.RunID != id || a.Status != AttemptClaimed {
			continue
		}
		key := nodeKey{id, a.NodeID}
		n := s.nodes[key]
		if n.Status != NodeRunning {
			return nil, storeError(StoreInvalidTransition, fmt.Sprintf("claimed attempt %s has node status %s", aid, n.Status))
		}
		a.Status = AttemptCancelled
		a.Version++
		a.EndedAt, a.HeartbeatAt = &now, &now
		a.Error = recordPointer(&message)
		n.Status = NodeCancelled
		n.Version++
		n.Error = recordPointer(&message)
		s.attempts[aid], s.nodes[key] = a, n
		out = append(out, a.Clone())
	}
	if len(out) != 0 {
		r.ActiveNodeIDs = s.activeNodesLocked(r)
		r.Version++
		s.runs[id] = r
	}
	return out, nil
}

func checkCompletionVersion(id RunID, actual, expected RecordVersion) error {
	if actual != expected {
		return storeError(StoreVersionConflict, fmt.Sprintf("run %s version conflict", id))
	}
	return nil
}

// RequestCancel retains the first nonempty reason, including source refusal effects.
func (s *InMemoryStore) RequestCancel(id RunID, expected RecordVersion, reason *string) (WorkflowRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.runLocked(id)
	if err != nil {
		return WorkflowRun{}, err
	}
	if err := checkCompletionVersion(id, r.Version, expected); err != nil {
		return WorkflowRun{}, err
	}
	if r.Terminal() {
		return r.Clone(), nil
	}
	if r.CancelReason == nil || *r.CancelReason == "" {
		message := "requested"
		if reason != nil {
			message = *reason
		}
		r.CancelReason = &message
	}
	active := len(s.activeNodesLocked(r)) != 0
	if r.Status == RunCancelling {
		if active {
			s.runs[id] = r
			return r.Clone(), nil
		}
		version, err := nextVersion(r.Version)
		if err != nil {
			return WorkflowRun{}, err
		}
		now := wallTime()
		r.Status, r.Version, r.EndedAt, r.ActiveNodeIDs = RunCancelled, version, &now, []NodeID{}
		s.runs[id] = r
		return r.Clone(), nil
	}
	target := RunCancelled
	if active || r.Status == RunRunning {
		target = RunCancelling
	}
	if !transitionAllowed(r.Status, target) {
		// Python stores the reason before it checks the transition.
		s.runs[id] = r
		return WorkflowRun{}, storeError(StoreInvalidTransition, fmt.Sprintf("%s -> %s", r.Status, target))
	}
	version, err := nextVersion(r.Version)
	if err != nil {
		return WorkflowRun{}, err
	}
	if target == RunCancelling && !active {
		version, err = nextVersion(version)
		if err != nil {
			return WorkflowRun{}, err
		}
	}
	order := s.definitions[r.DefinitionRevision].order
	for _, nid := range order {
		n := s.nodes[nodeKey{id, nid}]
		if n.Status == NodePending {
			if _, err := nextVersion(n.Version); err != nil {
				return WorkflowRun{}, err
			}
		}
	}
	r.Status, r.Version = target, version
	for _, nid := range order {
		key := nodeKey{id, nid}
		n := s.nodes[key]
		if n.Status == NodePending {
			n.Status = NodeCancelled
			n.Version++
			s.nodes[key] = n
		}
	}
	if target == RunCancelled || !active {
		now := wallTime()
		r.Status, r.EndedAt = RunCancelled, &now
		if target == RunCancelling {
			r.ActiveNodeIDs = []NodeID{}
		}
	}
	s.runs[id] = r
	return r.Clone(), nil
}

func (s *InMemoryStore) FinishCancellation(id RunID) (WorkflowRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.runLocked(id)
	if err != nil {
		return WorkflowRun{}, err
	}
	if r.Status == RunCancelled {
		return r.Clone(), nil
	}
	if r.Status != RunCancelling {
		return WorkflowRun{}, storeError(StoreInvalidTransition, fmt.Sprintf("run %s is %s", id, r.Status))
	}
	if len(s.activeNodesLocked(r)) != 0 {
		return WorkflowRun{}, storeError(StoreInvalidTransition, "cannot finish cancellation with active nodes")
	}
	version, err := nextVersion(r.Version)
	if err != nil {
		return WorkflowRun{}, err
	}
	now := wallTime()
	r.Status, r.Version, r.EndedAt, r.ActiveNodeIDs = RunCancelled, version, &now, []NodeID{}
	s.runs[id] = r
	return r.Clone(), nil
}

// FailRun keeps the source read-then-CAS boundary; a concurrent writer can conflict.
func (s *InMemoryStore) FailRun(id RunID, detail string) (WorkflowRun, error) {
	r, err := s.GetRun(id)
	if err != nil {
		return WorkflowRun{}, err
	}
	if r.Status == RunFailed {
		return r, nil
	}
	return s.TransitionRun(id, r.Version, RunFailed, &detail)
}

// FinalizeRun requires an existing artifact and successful nodes. The source store
// does not require that the selected artifact belongs to this run or its return node.
func (s *InMemoryStore) FinalizeRun(id RunID, expected RecordVersion, artifact ArtifactID) (WorkflowRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.runLocked(id)
	if err != nil {
		return WorkflowRun{}, err
	}
	if err := checkCompletionVersion(id, r.Version, expected); err != nil {
		return WorkflowRun{}, err
	}
	if r.Status != RunRunning {
		return WorkflowRun{}, storeError(StoreInvalidTransition, fmt.Sprintf("run %s is %s", id, r.Status))
	}
	if _, ok := s.artifacts[artifact]; !ok {
		return WorkflowRun{}, storeError(StoreNotFound, fmt.Sprintf("artifact %s not found", artifact))
	}
	for _, nid := range s.definitions[r.DefinitionRevision].order {
		if !s.nodes[nodeKey{id, nid}].Status.SatisfiesDependency() {
			return WorkflowRun{}, storeError(StoreInvalidTransition, "not all workflow nodes completed successfully")
		}
	}
	version, err := nextVersion(r.Version)
	if err != nil {
		return WorkflowRun{}, err
	}
	raw, err := workflowID20("wfout_")
	if err != nil {
		return WorkflowRun{}, err
	}
	now := wallTime()
	message := OutboxSnapshot{MessageID: OutboxID(raw), RunID: id, SessionID: r.SessionID, Kind: WorkflowCompleted, CreatedAt: now,
		Payload: jsonvalue.ObjectValue([]jsonvalue.Field{
			{Name: "run_id", Value: jsonvalue.TextValue(string(id))},
			{Name: "definition_revision", Value: jsonvalue.TextValue(string(r.DefinitionRevision))},
			{Name: "artifact_id", Value: jsonvalue.TextValue(string(artifact))},
		})}
	r.Status, r.Version, r.EndedAt, r.FinalArtifactID, r.ActiveNodeIDs = RunCompleted, version, &now, &artifact, []NodeID{}
	s.runs[id] = r
	s.outbox[message.MessageID] = message
	s.outboxOrder = append(s.outboxOrder, message.MessageID)
	s.outboxKeys[outboxKey{id, message.Kind}] = message.MessageID
	return r.Clone(), nil
}

type OutboxFilter struct {
	RunID           *RunID
	SessionID       *SessionID
	UndeliveredOnly bool
}

func (s *InMemoryStore) ListOutbox(filter OutboxFilter) []OutboxSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []OutboxSnapshot{}
	for _, m := range s.outbox {
		if filter.RunID != nil && m.RunID != *filter.RunID {
			continue
		}
		if filter.SessionID != nil && m.SessionID != *filter.SessionID {
			continue
		}
		if filter.UndeliveredOnly && m.DeliveredAt != nil {
			continue
		}
		out = append(out, m.Clone())
	}
	sortOutbox(out)
	return out
}
