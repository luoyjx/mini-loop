// Package runmeta carries inert provenance records shared by runtime consumers.
// A Snapshot is data, not execution authority; decoding one cannot mint a trusted
// agent.RunContext or approve a capability.
package runmeta

type MessageID string
type ActorID string
type Authority string
type Capability string

const (
	AuthorityUntrusted                   Authority  = "untrusted"
	AuthorityExplicitHuman               Authority  = "explicit_human"
	AuthorityPeerAgent                   Authority  = "peer_agent"
	CapabilityWorkflowLaunch             Capability = "workflow.launch"
	CapabilityWorkflowManage             Capability = "workflow.manage"
	CapabilityPersonalSkillCaptureSource Capability = "personal_skill.capture_source"
)

// Snapshot preserves the existing provenance wire shape. Fields describe the
// recorded caller; only a trusted live context may authorize runtime effects.
type Snapshot struct {
	MessageID            MessageID    `json:"message_id"`
	Origin               string       `json:"origin"`
	ActorID              *ActorID     `json:"actor_id"`
	Channel              string       `json:"channel"`
	Authority            Authority    `json:"authority"`
	StampedBy            string       `json:"stamped_by"`
	DelegatedBy          *string      `json:"delegated_by"`
	ParentMessageID      *MessageID   `json:"parent_message_id"`
	ApprovedCapabilities []Capability `json:"approved_capabilities"`
}

// Clone detaches optional fields and capability storage. Nil capabilities remain
// nil so cloning a decoded archival record does not change its JSON projection.
func (s Snapshot) Clone() Snapshot {
	if s.ActorID != nil {
		v := *s.ActorID
		s.ActorID = &v
	}
	if s.DelegatedBy != nil {
		v := *s.DelegatedBy
		s.DelegatedBy = &v
	}
	if s.ParentMessageID != nil {
		v := *s.ParentMessageID
		s.ParentMessageID = &v
	}
	if s.ApprovedCapabilities != nil {
		s.ApprovedCapabilities = append([]Capability{}, s.ApprovedCapabilities...)
	}
	return s
}
