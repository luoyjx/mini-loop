// Package workflows provides native workflow contracts. It does not install or
// dispatch a workflow service; activation and trusted execution remain adapter-owned.
package workflows

type PhaseID string
type NodeID string
type RunID string
type AttemptID string
type ArtifactID string
type Revision string
type DefinitionID string
type Digest string
type NodeKind string
type RunStatus string
type NodeStatus string
type AttemptStatus string
type VerificationStatus string
type DefinitionSource string

const SchemaVersion = 1
const HardMaxConcurrentAgents = 4
const HardMaxAgentsPerRun = 32

const (
	Agent       NodeKind = "agent"
	Map         NodeKind = "map"
	Reduce      NodeKind = "reduce"
	Verify      NodeKind = "verify"
	Sequence    NodeKind = "sequence"
	Branch      NodeKind = "branch"
	RepeatUntil NodeKind = "repeat_until"
	Barrier     NodeKind = "barrier"
	Return      NodeKind = "return"
)

func (s NodeKind) Valid() bool {
	switch s {
	case Agent, Map, Reduce, Verify, Sequence, Branch, RepeatUntil, Barrier, Return:
		return true
	}
	return false
}

const (
	RunCreated          RunStatus = "CREATED"
	RunPlanning         RunStatus = "PLANNING"
	RunAwaitingApproval RunStatus = "AWAITING_APPROVAL"
	RunQueued           RunStatus = "QUEUED"
	RunRunning          RunStatus = "RUNNING"
	RunPausing          RunStatus = "PAUSING"
	RunPaused           RunStatus = "PAUSED"
	RunWaitingApproval  RunStatus = "WAITING_APPROVAL"
	RunCancelling       RunStatus = "CANCELLING"
	RunCancelled        RunStatus = "CANCELLED"
	RunRejected         RunStatus = "REJECTED"
	RunCompleted        RunStatus = "COMPLETED"
	RunFailed           RunStatus = "FAILED"
)

func (s RunStatus) Valid() bool {
	switch s {
	case RunCreated, RunPlanning, RunAwaitingApproval, RunQueued, RunRunning, RunPausing, RunPaused, RunWaitingApproval, RunCancelling, RunCancelled, RunRejected, RunCompleted, RunFailed:
		return true
	}
	return false
}
func (s RunStatus) Terminal() bool {
	return s == RunCancelled || s == RunRejected || s == RunCompleted || s == RunFailed
}

const (
	NodePending    NodeStatus = "PENDING"
	NodeRunning    NodeStatus = "RUNNING"
	NodeSucceeded  NodeStatus = "SUCCEEDED"
	NodeUnverified NodeStatus = "UNVERIFIED"
	NodeFailed     NodeStatus = "FAILED"
	NodeCancelled  NodeStatus = "CANCELLED"
)

func (s NodeStatus) Valid() bool {
	switch s {
	case NodePending, NodeRunning, NodeSucceeded, NodeUnverified, NodeFailed, NodeCancelled:
		return true
	}
	return false
}
func (s NodeStatus) Terminal() bool {
	return s == NodeSucceeded || s == NodeUnverified || s == NodeFailed || s == NodeCancelled
}
func (s NodeStatus) SatisfiesDependency() bool { return s == NodeSucceeded || s == NodeUnverified }

const (
	AttemptPending   AttemptStatus = "PENDING"
	AttemptClaimed   AttemptStatus = "CLAIMED"
	AttemptRunning   AttemptStatus = "RUNNING"
	AttemptSucceeded AttemptStatus = "SUCCEEDED"
	AttemptFailed    AttemptStatus = "FAILED"
	AttemptUnknown   AttemptStatus = "UNKNOWN"
	AttemptCancelled AttemptStatus = "CANCELLED"
)

func (s AttemptStatus) Valid() bool {
	switch s {
	case AttemptPending, AttemptClaimed, AttemptRunning, AttemptSucceeded, AttemptFailed, AttemptUnknown, AttemptCancelled:
		return true
	}
	return false
}
func (s AttemptStatus) Terminal() bool {
	return s == AttemptSucceeded || s == AttemptFailed || s == AttemptUnknown || s == AttemptCancelled
}

const (
	NotApplicable VerificationStatus = "not_applicable"
	Verified      VerificationStatus = "verified"
	Refuted       VerificationStatus = "refuted"
	Unverified    VerificationStatus = "unverified"
	Dynamic       DefinitionSource   = "dynamic"
	Project       DefinitionSource   = "project"
	Personal      DefinitionSource   = "personal"
	Plugin        DefinitionSource   = "plugin"
)

func (s VerificationStatus) Valid() bool {
	return s == NotApplicable || s == Verified || s == Refuted || s == Unverified
}
func (s DefinitionSource) Valid() bool {
	return s == Dynamic || s == Project || s == Personal || s == Plugin
}
