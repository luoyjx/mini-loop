package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workflows"
)

func workflowKind(kind SessionEventKind) bool {
	switch WorkflowEventKind(kind) {
	case WorkflowPlanned, WorkflowDecisionRecorded, WorkflowStarted, WorkflowNodeClaimed,
		WorkflowAgentStarted, WorkflowAgentProgress, WorkflowAgentCompleted, WorkflowVerdictRecorded,
		WorkflowCompleted, WorkflowFailed, WorkflowCancelled, WorkflowResultEnqueued,
		WorkflowApprovalRequired, WorkflowRejected, WorkflowPhaseStarted, WorkflowCheckpointed, WorkflowPaused, WorkflowResumed:
		return true
	}
	return false
}

func (event SessionEvent) Workflow() (WorkflowEvent, bool) {
	return event.workflow.Clone(), workflowKind(event.kind)
}

type workflowSessionJSON struct {
	Event    WorkflowEvent
	Sequence EventSequence
}

func (row workflowSessionJSON) MarshalJSON() ([]byte, error) {
	body, err := row.Event.encode(false)
	if err != nil {
		return nil, err
	}
	sequence, err := json.Marshal(struct {
		Sequence EventSequence `json:"sequence"`
	}{row.Sequence})
	if err != nil {
		return nil, err
	}
	body[len(body)-1] = ','
	return append(body, sequence[1:]...), nil
}

type workflowEventResolver func(SessionID) WorkflowEventSink

type managedWorkflowEvents struct{ session *ManagedSession }

func (sink managedWorkflowEvents) EmitWorkflowEvent(_ context.Context, event WorkflowEvent) error {
	if event.SessionID != sink.session.ID() {
		return fmt.Errorf("workflow event belongs to a different session")
	}
	if _, err := event.MarshalJSON(); err != nil {
		return err
	}
	sink.session.emitFor(RunContext{}, SessionEvent{kind: SessionEventKind(event.Kind), workflow: event.Clone()})
	if p := sink.session.core.events.persistence; p != nil {
		p.mu.Lock()
		lost := p.lost || p.pendingRestore
		p.mu.Unlock()
		if lost {
			return ErrSessionLeaseLost
		}
	}
	return nil
}

// Archival data becomes only a known observational variant. It cannot reconstruct
// the private trusted context needed to launch or manage a workflow.
func decodeWorkflowEvent(data []byte) (WorkflowEvent, error) {
	var row struct {
		Type          WorkflowEventKind    `json:"type"`
		Kind          WorkflowEventKind    `json:"kind"`
		EventID       string               `json:"event_id"`
		OccurredAt    float64              `json:"occurred_at"`
		SessionID     SessionID            `json:"session_id"`
		RunID         workflows.RunID      `json:"run_id"`
		WorkflowRunID workflows.RunID      `json:"workflow_run_id"`
		Name          string               `json:"workflow_name"`
		Revision      workflows.Revision   `json:"definition_revision"`
		Version       int                  `json:"payload_version"`
		NodeID        *workflows.NodeID    `json:"node_id"`
		AttemptID     *workflows.AttemptID `json:"attempt_id"`
		AgentID       *workflows.AgentID   `json:"agent_id"`
		ParentAgentID *workflows.AgentID   `json:"parent_agent_id"`
		PhaseID       *workflows.PhaseID   `json:"phase_id"`
		Payload       workflows.Value      `json:"payload"`
	}
	if err := json.Unmarshal(data, &row); err != nil {
		return WorkflowEvent{}, err
	}
	if row.Version != 1 {
		return WorkflowEvent{}, fmt.Errorf("unsupported workflow event payload_version")
	}
	if row.Type != row.Kind || row.RunID != row.WorkflowRunID {
		return WorkflowEvent{}, fmt.Errorf("workflow event aliases or version do not match")
	}
	if !workflowKind(SessionEventKind(row.Kind)) {
		return WorkflowEvent{}, fmt.Errorf("unsupported workflow event kind: %s", row.Kind)
	}
	if row.Payload.Kind() != jsonvalue.Object {
		return WorkflowEvent{}, fmt.Errorf("workflow observation requires known kind and object payload")
	}
	event := WorkflowEvent{Kind: row.Kind, EventID: row.EventID, OccurredAt: row.OccurredAt,
		SessionID: row.SessionID, RunID: row.RunID, Name: row.Name, Revision: row.Revision,
		PhaseID: row.PhaseID, NodeID: row.NodeID, AttemptID: row.AttemptID, AgentID: row.AgentID, ParentAgentID: row.ParentAgentID,
		observation: &row.Payload}
	_, err := event.MarshalJSON()
	return event, err
}

func maskWorkflowEvent(event WorkflowEvent, mask func(string) string) WorkflowEvent {
	if event.Kind == "" {
		return event
	}
	event = event.Clone()
	event.EventID = mask(event.EventID)
	event.SessionID = SessionID(mask(string(event.SessionID)))
	event.RunID = workflows.RunID(mask(string(event.RunID)))
	event.Name = mask(event.Name)
	event.Revision = workflows.Revision(mask(string(event.Revision)))
	if event.NodeID != nil {
		*event.NodeID = workflows.NodeID(mask(string(*event.NodeID)))
	}
	if event.AttemptID != nil {
		*event.AttemptID = workflows.AttemptID(mask(string(*event.AttemptID)))
	}
	if event.AgentID != nil {
		*event.AgentID = workflows.AgentID(mask(string(*event.AgentID)))
	}
	if event.ParentAgentID != nil {
		*event.ParentAgentID = workflows.AgentID(mask(string(*event.ParentAgentID)))
	}
	if event.PhaseID != nil {
		*event.PhaseID = workflows.PhaseID(mask(string(*event.PhaseID)))
	}
	if event.observation != nil {
		value := event.observation.MapStrings(mask)
		event.observation = &value
		return event
	}
	p := &event.payload
	p.definitionHash = workflows.Digest(mask(string(p.definitionHash)))
	p.hash = workflows.Digest(mask(string(p.hash)))
	p.size = mask(p.size)
	p.reason = mask(p.reason)
	p.nodeKind = workflows.NodeKind(mask(string(p.nodeKind)))
	p.authority = RunAuthority(mask(string(p.authority)))
	p.verification = workflows.VerificationStatus(mask(string(p.verification)))
	p.status = workflows.RunStatus(mask(string(p.status)))
	p.message = workflows.OutboxID(mask(string(p.message)))
	if p.actor != nil {
		*p.actor = ActorID(mask(string(*p.actor)))
	}
	if p.artifact != nil {
		*p.artifact = workflows.ArtifactID(mask(string(*p.artifact)))
	}
	maskStringPointer(mask, &p.error)
	if p.progress.Name != nil {
		*p.progress.Name = protocol.ToolName(mask(string(*p.progress.Name)))
	}
	p.progress.Type = SessionEventKind(mask(string(p.progress.Type)))
	maskStringPointer(mask, &p.progress.ID)
	maskStringPointer(mask, &p.progress.Error)
	return event
}

// DecodeWorkflowObservation retains a Source event's inert payload and metadata.
// It neither reconstructs a trusted context nor activates a workflow controller.
func DecodeWorkflowObservation(data []byte) (WorkflowEvent, error) {
	return decodeWorkflowEvent(data)
}
