package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workflows"
)

type WorkflowEventKind string

const (
	WorkflowPlanned          WorkflowEventKind = "workflow_planned"
	WorkflowDecisionRecorded WorkflowEventKind = "workflow_decision_recorded"
	WorkflowStarted          WorkflowEventKind = "workflow_started"
	WorkflowNodeClaimed      WorkflowEventKind = "workflow_node_claimed"
	WorkflowAgentStarted     WorkflowEventKind = "workflow_agent_started"
	WorkflowAgentProgress    WorkflowEventKind = "workflow_agent_progress"
	WorkflowAgentCompleted   WorkflowEventKind = "workflow_agent_completed"
	WorkflowVerdictRecorded  WorkflowEventKind = "workflow_verdict_recorded"
	WorkflowCompleted        WorkflowEventKind = "workflow_completed"
	WorkflowFailed           WorkflowEventKind = "workflow_failed"
	WorkflowCancelled        WorkflowEventKind = "workflow_cancelled"
	WorkflowResultEnqueued   WorkflowEventKind = "workflow_result_enqueued"
)

type WorkflowProgress struct {
	Type       SessionEventKind   `json:"type"`
	Name       *protocol.ToolName `json:"name,omitempty"`
	ID         *string            `json:"id,omitempty"`
	Error      *string            `json:"error,omitempty"`
	DurationMS *float64           `json:"duration_ms,omitempty"`
}

// WorkflowEvent is a closed service-event union. Its private payload is selected
// by Kind; it cannot carry arbitrary tool output, arguments, transcript or maps.
type WorkflowEvent struct {
	Kind          WorkflowEventKind
	EventID       string
	OccurredAt    float64
	SessionID     SessionID
	RunID         workflows.RunID
	Name          string
	Revision      workflows.Revision
	NodeID        *workflows.NodeID
	AttemptID     *workflows.AttemptID
	AgentID       *workflows.AgentID
	ParentAgentID *workflows.AgentID
	payload       workflowEventPayload
}
type workflowEventPayload struct {
	definitionHash workflows.Digest
	nodeCount      int
	size           string
	actor          *ActorID
	authority      RunAuthority
	startedAt      *float64
	nodeKind       workflows.NodeKind
	spawn          workflows.SpawnIndex
	progress       WorkflowProgress
	success        bool
	error          *string
	verification   workflows.VerificationStatus
	artifact       *workflows.ArtifactID
	hash           workflows.Digest
	reason         string
	message        workflows.OutboxID
	status         workflows.RunStatus
}

func (e WorkflowEvent) Clone() WorkflowEvent {
	e.NodeID = clonePointer(e.NodeID)
	e.AttemptID = clonePointer(e.AttemptID)
	e.AgentID = clonePointer(e.AgentID)
	e.ParentAgentID = clonePointer(e.ParentAgentID)
	p := &e.payload
	p.actor = clonePointer(p.actor)
	p.startedAt = clonePointer(p.startedAt)
	p.error = clonePointer(p.error)
	p.artifact = clonePointer(p.artifact)
	p.progress.Name = clonePointer(p.progress.Name)
	p.progress.ID = clonePointer(p.progress.ID)
	p.progress.Error = clonePointer(p.progress.Error)
	p.progress.DurationMS = clonePointer(p.progress.DurationMS)
	return e
}
func (e WorkflowEvent) Progress() (WorkflowProgress, bool) {
	return e.Clone().payload.progress, e.Kind == WorkflowAgentProgress
}
func (e WorkflowEvent) MarshalJSON() ([]byte, error) {
	if e.SessionID == "" || e.RunID == "" || e.Name == "" || e.Revision == "" || e.EventID == "" {
		return nil, fmt.Errorf("workflow event requires session, run, definition and event identity")
	}
	switch e.Kind {
	case WorkflowNodeClaimed, WorkflowAgentStarted, WorkflowAgentProgress, WorkflowAgentCompleted, WorkflowVerdictRecorded:
		if e.NodeID == nil || *e.NodeID == "" || e.AttemptID == nil || *e.AttemptID == "" {
			return nil, fmt.Errorf("workflow attempt event requires node and attempt identity")
		}
	}
	switch e.Kind {
	case WorkflowAgentStarted, WorkflowAgentProgress, WorkflowAgentCompleted:
		if e.AgentID == nil || *e.AgentID == "" {
			return nil, fmt.Errorf("workflow agent event requires agent identity")
		}
	}
	payload, err := e.payloadJSON()
	if err != nil {
		return nil, err
	}
	value, err := jsonvalue.Decode(string(payload))
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
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
		Payload       workflows.Value      `json:"payload"`
		NodeID        *workflows.NodeID    `json:"node_id,omitempty"`
		AttemptID     *workflows.AttemptID `json:"attempt_id,omitempty"`
		AgentID       *workflows.AgentID   `json:"agent_id,omitempty"`
		ParentAgentID *workflows.AgentID   `json:"parent_agent_id,omitempty"`
	}{e.Kind, e.Kind, e.EventID, e.OccurredAt, e.SessionID, e.RunID, e.RunID, e.Name, e.Revision, 1, value, e.NodeID, e.AttemptID, e.AgentID, e.ParentAgentID})
}
func (e WorkflowEvent) payloadJSON() ([]byte, error) {
	p := e.payload
	switch e.Kind {
	case WorkflowPlanned:
		return json.Marshal(struct {
			Hash  workflows.Digest `json:"definition_hash"`
			Count int              `json:"node_count"`
			Size  string           `json:"size_guideline"`
		}{p.definitionHash, p.nodeCount, p.size})
	case WorkflowDecisionRecorded:
		return json.Marshal(struct {
			Decision  string       `json:"decision"`
			Actor     *ActorID     `json:"actor_id"`
			Authority RunAuthority `json:"authority"`
			Mode      string       `json:"mode"`
		}{"approved", p.actor, p.authority, "trusted_local_preapproval"})
	case WorkflowStarted:
		return json.Marshal(struct {
			Count   int      `json:"node_count"`
			Started *float64 `json:"started_at"`
		}{p.nodeCount, p.startedAt})
	case WorkflowNodeClaimed:
		return json.Marshal(struct {
			Kind  workflows.NodeKind   `json:"kind"`
			Spawn workflows.SpawnIndex `json:"spawn_index"`
		}{p.nodeKind, p.spawn})
	case WorkflowAgentStarted:
		return json.Marshal(struct {
			Kind workflows.NodeKind `json:"kind"`
		}{p.nodeKind})
	case WorkflowAgentProgress:
		return json.Marshal(p.progress)
	case WorkflowAgentCompleted:
		return json.Marshal(struct {
			Success bool    `json:"success"`
			Error   *string `json:"error,omitempty"`
		}{p.success, p.error})
	case WorkflowVerdictRecorded:
		return json.Marshal(struct {
			Status   workflows.VerificationStatus `json:"status"`
			Artifact *workflows.ArtifactID        `json:"artifact_id"`
			Error    *string                      `json:"error"`
		}{p.verification, p.artifact, p.error})
	case WorkflowCompleted:
		return json.Marshal(struct {
			Artifact *workflows.ArtifactID `json:"artifact_id"`
			Hash     workflows.Digest      `json:"content_hash"`
		}{p.artifact, p.hash})
	case WorkflowFailed:
		return json.Marshal(struct {
			Error *string `json:"error"`
		}{p.error})
	case WorkflowCancelled:
		return json.Marshal(struct {
			Reason string `json:"reason"`
		}{p.reason})
	case WorkflowResultEnqueued:
		return json.Marshal(struct {
			Message  workflows.OutboxID    `json:"message_id"`
			Status   workflows.RunStatus   `json:"status"`
			Artifact *workflows.ArtifactID `json:"artifact_id"`
		}{p.message, p.status, p.artifact})
	default:
		return nil, fmt.Errorf("unsupported workflow service event %q", e.Kind)
	}
}
func workflowEventIdentity() (string, float64, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", 0, err
	}
	return "wfevt_" + hex.EncodeToString(value[:]), float64(time.Now().UnixNano()) / 1e9, nil
}

type WorkflowEventSink interface {
	EmitWorkflowEvent(context.Context, WorkflowEvent) error
}
type WorkflowEventSinkFunc func(context.Context, WorkflowEvent) error

func (f WorkflowEventSinkFunc) EmitWorkflowEvent(ctx context.Context, event WorkflowEvent) error {
	return f(ctx, event)
}
