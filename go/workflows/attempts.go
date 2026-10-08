package workflows

import (
	"fmt"
	"slices"
)

func enumStoreError(value, name string) error {
	quoted := reprNames([]string{value})
	return storeError(StoreValueFailure, quoted[1:len(quoted)-1]+" is not a valid "+name)
}
func (s *InMemoryStore) attemptLocked(id AttemptID) (NodeAttempt, error) {
	if a, ok := s.attempts[id]; ok {
		return a, nil
	}
	return NodeAttempt{}, storeError(StoreNotFound, fmt.Sprintf("attempt %s not found", id))
}
func checkAttemptVersion(id AttemptID, actual, expected RecordVersion) error {
	if actual != expected {
		return storeError(StoreVersionConflict, fmt.Sprintf("attempt %s version conflict", id))
	}
	return nil
}
func (s *InMemoryStore) StartAttempt(id AttemptID, expected RecordVersion) (NodeAttempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.attemptLocked(id)
	if err != nil {
		return NodeAttempt{}, err
	}
	if err := checkAttemptVersion(id, a.Version, expected); err != nil {
		return NodeAttempt{}, err
	}
	if a.Status != AttemptClaimed {
		return NodeAttempt{}, storeError(StoreInvalidTransition, fmt.Sprintf("attempt %s is %s", id, a.Status))
	}
	version, err := nextVersion(a.Version)
	if err != nil {
		return NodeAttempt{}, err
	}
	now := wallTime()
	a.Status = AttemptRunning
	a.Version = version
	a.StartedAt = &now
	a.HeartbeatAt = &now
	s.attempts[id] = a
	return a.Clone(), nil
}

type CommitAttemptInput struct {
	AttemptID       AttemptID
	ExpectedVersion RecordVersion
	AttemptStatus   AttemptStatus
	NodeStatus      NodeStatus
	Artifact        *Artifact
	// Nil denotes the source default; a provided value is converted at the
	// source's late verification boundary, including its partial-error effects.
	Verification *VerificationStatus
	Error        *string
}

func (s *InMemoryStore) activeNodesLocked(r WorkflowRun) []NodeID {
	out := []NodeID{}
	for _, id := range s.definitions[r.DefinitionRevision].order {
		if s.nodes[nodeKey{r.RunID, id}].Status == NodeRunning {
			out = append(out, id)
		}
	}
	return out
}
func (s *InMemoryStore) CommitAttempt(input CommitAttemptInput) (NodeAttempt, error) {
	if !input.AttemptStatus.Valid() {
		return NodeAttempt{}, enumStoreError(string(input.AttemptStatus), "AttemptStatus")
	}
	if !input.NodeStatus.Valid() {
		return NodeAttempt{}, enumStoreError(string(input.NodeStatus), "NodeStatus")
	}
	if !input.AttemptStatus.Terminal() || !input.NodeStatus.Terminal() {
		return NodeAttempt{}, storeError(StoreInvalidTransition, "attempt and node commit statuses must be terminal")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.attemptLocked(input.AttemptID)
	if err != nil {
		return NodeAttempt{}, err
	}
	if err := checkAttemptVersion(a.AttemptID, a.Version, input.ExpectedVersion); err != nil {
		return NodeAttempt{}, err
	}
	if a.Status != AttemptRunning {
		return NodeAttempt{}, storeError(StoreInvalidTransition, fmt.Sprintf("attempt %s is %s", a.AttemptID, a.Status))
	}
	n, err := s.nodeLocked(a.RunID, a.NodeID)
	if err != nil {
		return NodeAttempt{}, err
	}
	if n.Status != NodeRunning {
		return NodeAttempt{}, storeError(StoreInvalidTransition, fmt.Sprintf("node %s is %s", n.NodeID, n.Status))
	}
	if input.Artifact != nil {
		artifact := input.Artifact.Snapshot()
		if artifact.RunID != a.RunID || artifact.NodeID != a.NodeID || artifact.AttemptID != a.AttemptID {
			return NodeAttempt{}, storeError(StoreFailure, "artifact provenance does not match attempt")
		}
	}
	r, err := s.runLocked(a.RunID)
	if err != nil {
		return NodeAttempt{}, err
	}
	av, err := nextVersion(a.Version)
	if err != nil {
		return NodeAttempt{}, err
	}
	nv, err := nextVersion(n.Version)
	if err != nil {
		return NodeAttempt{}, err
	}
	rv, err := nextVersion(r.Version)
	if err != nil {
		return NodeAttempt{}, err
	}
	if input.Artifact != nil {
		artifact := *input.Artifact
		snapshot := artifact.Snapshot()
		s.artifacts[snapshot.ArtifactID] = artifact
		n.ResultArtifactIDs = append(slices.Clone(n.ResultArtifactIDs), snapshot.ArtifactID)
		id := snapshot.ArtifactID
		a.ResultArtifactID = &id
	}
	now := wallTime()
	a.Status = input.AttemptStatus
	a.Version = av
	a.EndedAt = &now
	a.HeartbeatAt = &now
	verification := NotApplicable
	if input.Verification != nil {
		verification = *input.Verification
	}
	if !verification.Valid() {
		// Preserve the actual source failure: artifact references and the attempt
		// terminal status are already stored; node/run settlement has not occurred.
		s.attempts[a.AttemptID] = a
		s.nodes[nodeKey{a.RunID, a.NodeID}] = n
		return NodeAttempt{}, enumStoreError(string(verification), "VerificationStatus")
	}
	a.VerificationStatus = verification
	a.Error = recordPointer(input.Error)
	n.Status = input.NodeStatus
	n.Version = nv
	n.Error = recordPointer(input.Error)
	s.attempts[a.AttemptID] = a
	s.nodes[nodeKey{a.RunID, a.NodeID}] = n
	r.ActiveNodeIDs = s.activeNodesLocked(r)
	r.Version = rv
	s.runs[r.RunID] = r
	return a.Clone(), nil
}
func (s *InMemoryStore) GetArtifact(id ArtifactID) (Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.artifacts[id]; ok {
		return a, nil
	}
	return Artifact{}, storeError(StoreNotFound, fmt.Sprintf("artifact %s not found", id))
}
func (s *InMemoryStore) ArtifactsForNode(run RunID, node NodeID) ([]Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.nodeLocked(run, node)
	if err != nil {
		return nil, err
	}
	out := []Artifact{}
	for _, id := range n.ResultArtifactIDs {
		a, ok := s.artifacts[id]
		if !ok {
			return nil, storeError(StoreNotFound, fmt.Sprintf("artifact %s not found", id))
		}
		out = append(out, a)
	}
	return out, nil
}
