package workflows

import (
	"errors"
	"slices"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/runmeta"
)

type SessionID string
type AgentID string
type IdempotencyKey string
type LaunchActionID string
type RecordVersion int64
type EventCursor int64
type AttemptCount int
type AttemptNumber int
type SpawnIndex int
type OutboxID string
type OutboxKind string
type ClaimToken string

var ErrRecord = errors.New("invalid workflow record")

// WorkflowRun is a detached state record, not a live execution authority.
// Store implementations must clone records at both write and read boundaries.
type WorkflowRun struct {
	RunID              RunID            `json:"run_id"`
	DefinitionRevision Revision         `json:"definition_revision"`
	SessionID          SessionID        `json:"session_id"`
	RunContext         runmeta.Snapshot `json:"run_context"`
	IdempotencyKey     IdempotencyKey   `json:"idempotency_key"`
	Args               Value            `json:"args"`
	Status             RunStatus        `json:"status"`
	Version            RecordVersion    `json:"version"`
	ParentRunID        *RunID           `json:"parent_run_id"`
	LaunchActionID     *LaunchActionID  `json:"launch_action_id"`
	CreatedAt          float64          `json:"created_at"`
	StartedAt          *float64         `json:"started_at"`
	EndedAt            *float64         `json:"ended_at"`
	ActiveNodeIDs      []NodeID         `json:"active_node_ids"`
	EventCursor        EventCursor      `json:"event_cursor"`
	AttemptsUsed       AttemptCount     `json:"attempts_used"`
	PolicySnapshotHash Digest           `json:"policy_snapshot_hash"`
	WorkspaceBaseline  *string          `json:"workspace_baseline"`
	FinalArtifactID    *ArtifactID      `json:"final_artifact_id"`
	Error              *string          `json:"error"`
	CancelReason       *string          `json:"cancel_reason"`
}

type NodeState struct {
	RunID             RunID         `json:"run_id"`
	NodeID            NodeID        `json:"node_id"`
	Status            NodeStatus    `json:"status"`
	Version           RecordVersion `json:"version"`
	AttemptIDs        []AttemptID   `json:"attempt_ids"`
	ResultArtifactIDs []ArtifactID  `json:"result_artifact_ids"`
	Error             *string       `json:"error"`
}

type AttemptClaim struct {
	NodeID        NodeID     `json:"node_id"`
	AgentID       AgentID    `json:"agent_id"`
	SpawnIndex    SpawnIndex `json:"spawn_index"`
	ParentAgentID *AgentID   `json:"parent_agent_id"`
}

type NodeAttempt struct {
	AttemptID          AttemptID          `json:"attempt_id"`
	RunID              RunID              `json:"run_id"`
	NodeID             NodeID             `json:"node_id"`
	Attempt            AttemptNumber      `json:"attempt"`
	AgentID            AgentID            `json:"agent_id"`
	SpawnIndex         SpawnIndex         `json:"spawn_index"`
	Status             AttemptStatus      `json:"status"`
	Version            RecordVersion      `json:"version"`
	ParentAgentID      *AgentID           `json:"parent_agent_id"`
	StartedAt          *float64           `json:"started_at"`
	HeartbeatAt        *float64           `json:"heartbeat_at"`
	EndedAt            *float64           `json:"ended_at"`
	ResultArtifactID   *ArtifactID        `json:"result_artifact_id"`
	VerificationStatus VerificationStatus `json:"verification_status"`
	Error              *string            `json:"error"`
}

// OutboxSnapshot describes delivery state. It does not prove external delivery.
// Kind is source-extensible text; payload is a closed immutable JSON object.
type OutboxSnapshot struct {
	MessageID   OutboxID    `json:"message_id"`
	RunID       RunID       `json:"run_id"`
	SessionID   SessionID   `json:"session_id"`
	Kind        OutboxKind  `json:"kind"`
	Payload     Value       `json:"payload"`
	CreatedAt   float64     `json:"created_at"`
	ClaimToken  *ClaimToken `json:"claim_token"`
	ClaimedAt   *float64    `json:"claimed_at"`
	DeliveredAt *float64    `json:"delivered_at"`
}

// These detached mutable projections support the source store's state folds.
// Value owns private immutable containers; only pointer/slice fields need copies.
func (r WorkflowRun) Clone() WorkflowRun {
	r.RunContext = r.RunContext.Clone()
	r.ParentRunID = recordPointer(r.ParentRunID)
	r.LaunchActionID = recordPointer(r.LaunchActionID)
	r.StartedAt = recordPointer(r.StartedAt)
	r.EndedAt = recordPointer(r.EndedAt)
	r.ActiveNodeIDs = slices.Clone(r.ActiveNodeIDs)
	r.WorkspaceBaseline = recordPointer(r.WorkspaceBaseline)
	r.FinalArtifactID = recordPointer(r.FinalArtifactID)
	r.Error = recordPointer(r.Error)
	r.CancelReason = recordPointer(r.CancelReason)
	return r
}
func (r WorkflowRun) Terminal() bool { return r.Status.Terminal() }
func (r NodeState) Clone() NodeState {
	r.AttemptIDs = slices.Clone(r.AttemptIDs)
	r.ResultArtifactIDs = slices.Clone(r.ResultArtifactIDs)
	r.Error = recordPointer(r.Error)
	return r
}
func (r AttemptClaim) Clone() AttemptClaim {
	r.ParentAgentID = recordPointer(r.ParentAgentID)
	return r
}
func (r NodeAttempt) Clone() NodeAttempt {
	r.ParentAgentID = recordPointer(r.ParentAgentID)
	r.StartedAt = recordPointer(r.StartedAt)
	r.HeartbeatAt = recordPointer(r.HeartbeatAt)
	r.EndedAt = recordPointer(r.EndedAt)
	r.ResultArtifactID = recordPointer(r.ResultArtifactID)
	r.Error = recordPointer(r.Error)
	return r
}
func (r OutboxSnapshot) Clone() OutboxSnapshot {
	r.ClaimToken = recordPointer(r.ClaimToken)
	r.ClaimedAt = recordPointer(r.ClaimedAt)
	r.DeliveredAt = recordPointer(r.DeliveredAt)
	return r
}

func recordPointer[T ~string | ~float64](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// decodeRecord lowers only the five concrete record variants. Python permits
// broader live objects; native boundaries require advertised JSON field types.
func decodeRecord[T WorkflowRun | NodeState | AttemptClaim | NodeAttempt | OutboxSnapshot](data []byte, out *T, texts, integers, objects, arrays []string) error {
	input, err := jsonvalue.Decode(string(data))
	if err != nil {
		return err
	}
	if input.Kind() != jsonvalue.Object {
		return ErrRecord
	}
	// encoding/json silently ignores null for scalar fields. Refuse it before
	// lowering so an explicit null cannot acquire an advertised default status.
	for _, key := range input.Keys() {
		v, _ := input.Lookup(key)
		if v.Kind() == jsonvalue.Null {
			switch key {
			case "parent_run_id", "launch_action_id", "started_at", "ended_at", "workspace_baseline", "final_artifact_id", "error", "cancel_reason", "parent_agent_id", "heartbeat_at", "result_artifact_id", "claim_token", "claimed_at", "delivered_at":
			default:
				return ErrRecord
			}
		}
	}
	for kind, keys := range map[jsonvalue.Kind][]string{
		jsonvalue.Text: texts, jsonvalue.Integer: integers, jsonvalue.Object: objects,
	} {
		for _, key := range keys {
			v, exists := input.Lookup(key)
			if !exists || v.Kind() != kind {
				return ErrRecord
			}
		}
	}
	for _, key := range arrays {
		if v, exists := input.Lookup(key); exists {
			items, ok := v.Array()
			if !ok {
				return ErrRecord
			}
			for _, item := range items {
				if item.Kind() != jsonvalue.Text {
					return ErrRecord
				}
			}
		}
	}
	return decodeStrict(data, out)
}

func DecodeWorkflowRun(data []byte) (WorkflowRun, error) {
	r := WorkflowRun{Status: RunQueued, CreatedAt: float64(time.Now().UnixNano()) / 1e9, ActiveNodeIDs: []NodeID{}}
	err := decodeRecord(data, &r, []string{"run_id", "definition_revision", "session_id", "idempotency_key"}, nil, []string{"run_context", "args"}, []string{"active_node_ids"})
	if err != nil {
		return WorkflowRun{}, err
	}
	if !r.Status.Valid() {
		return WorkflowRun{}, ErrRecord
	}
	return r.Clone(), nil
}
func DecodeNodeState(data []byte) (NodeState, error) {
	r := NodeState{Status: NodePending, AttemptIDs: []AttemptID{}, ResultArtifactIDs: []ArtifactID{}}
	err := decodeRecord(data, &r, []string{"run_id", "node_id"}, nil, nil, []string{"attempt_ids", "result_artifact_ids"})
	if err != nil {
		return NodeState{}, err
	}
	if !r.Status.Valid() {
		return NodeState{}, ErrRecord
	}
	return r.Clone(), nil
}
func DecodeAttemptClaim(data []byte) (AttemptClaim, error) {
	var r AttemptClaim
	if err := decodeRecord(data, &r, []string{"node_id", "agent_id"}, []string{"spawn_index"}, nil, nil); err != nil {
		return AttemptClaim{}, err
	}
	return r.Clone(), nil
}
func DecodeNodeAttempt(data []byte) (NodeAttempt, error) {
	r := NodeAttempt{Status: AttemptClaimed, VerificationStatus: NotApplicable}
	err := decodeRecord(data, &r, []string{"attempt_id", "run_id", "node_id", "agent_id"}, []string{"attempt", "spawn_index"}, nil, nil)
	if err != nil {
		return NodeAttempt{}, err
	}
	if !r.Status.Valid() || !r.VerificationStatus.Valid() {
		return NodeAttempt{}, ErrRecord
	}
	return r.Clone(), nil
}
func DecodeOutboxSnapshot(data []byte) (OutboxSnapshot, error) {
	r := OutboxSnapshot{CreatedAt: float64(time.Now().UnixNano()) / 1e9}
	if err := decodeRecord(data, &r, []string{"message_id", "run_id", "session_id", "kind"}, nil, []string{"payload"}, nil); err != nil {
		return OutboxSnapshot{}, err
	}
	return r.Clone(), nil
}
