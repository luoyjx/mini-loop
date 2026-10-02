package agent

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"slices"
)

type MessageID string
type ActorID string
type RunAuthority string
type RunCapability string

const (
	AuthorityUntrusted       RunAuthority  = "untrusted"
	AuthorityExplicitHuman   RunAuthority  = "explicit_human"
	AuthorityPeerAgent       RunAuthority  = "peer_agent"
	CapabilityWorkflowLaunch RunCapability = "workflow.launch"
	CapabilityWorkflowManage RunCapability = "workflow.manage"
)

// RunContext is stamped by a trusted caller, never decoded from model text.
// A zero value carries no approval. Accessors detach optional/slice data.
type RunContext struct {
	messageID       MessageID
	origin          string
	actorID         *ActorID
	channel         string
	authority       RunAuthority
	stampedBy       string
	delegatedBy     *string
	parentMessageID *MessageID
	approved        []RunCapability
}
type RunContextSnapshot struct {
	MessageID            MessageID       `json:"message_id"`
	Origin               string          `json:"origin"`
	ActorID              *ActorID        `json:"actor_id"`
	Channel              string          `json:"channel"`
	Authority            RunAuthority    `json:"authority"`
	StampedBy            string          `json:"stamped_by"`
	DelegatedBy          *string         `json:"delegated_by"`
	ParentMessageID      *MessageID      `json:"parent_message_id"`
	ApprovedCapabilities []RunCapability `json:"approved_capabilities"`
}

func newMessageID() (MessageID, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return MessageID("msg_" + hex.EncodeToString(bytes[:])), nil
}
func DefaultRunContext() (RunContext, error) {
	id, err := newMessageID()
	return RunContext{messageID: id, origin: "api", channel: "internal", authority: AuthorityUntrusted, stampedBy: "mini_loop"}, err
}

type HumanRunConfig struct {
	ActorID              *ActorID
	Channel              string
	StampedBy            string
	ApprovedCapabilities []RunCapability
}

func ExplicitHumanRunContext(config HumanRunConfig) (RunContext, error) {
	value, err := DefaultRunContext()
	if err != nil {
		return RunContext{}, err
	}
	value.origin, value.authority = "explicit_human", AuthorityExplicitHuman
	value.channel, value.stampedBy = config.Channel, config.StampedBy
	if value.channel == "" {
		value.channel = "local"
	}
	if value.stampedBy == "" {
		value.stampedBy = "trusted_local"
	}
	value.actorID = config.ActorID
	value.approved = normalizeRunCapabilities(config.ApprovedCapabilities)
	return value.clone(), nil
}
func normalizeRunCapabilities(values []RunCapability) []RunCapability {
	result := append([]RunCapability{}, values...)
	slices.Sort(result)
	return slices.Compact(result)
}
func (value RunContext) Authority() RunAuthority {
	if value.authority == "" {
		return AuthorityUntrusted
	}
	return value.authority
}
func (value RunContext) MessageID() MessageID { return value.messageID }
func (value RunContext) Allows(capability RunCapability) bool {
	return slices.Contains(value.approved, capability)
}
func (value RunContext) Validate() error {
	if value.messageID == "" || value.channel == "" || value.stampedBy == "" {
		return errors.New("run context requires message identity, channel and stamp")
	}
	if value.authority != AuthorityUntrusted && value.authority != AuthorityExplicitHuman && value.authority != AuthorityPeerAgent {
		return errors.New("invalid run authority")
	}
	return nil
}
func (value RunContext) clone() RunContext {
	if value.actorID != nil {
		copy := *value.actorID
		value.actorID = &copy
	}
	if value.delegatedBy != nil {
		copy := *value.delegatedBy
		value.delegatedBy = &copy
	}
	if value.parentMessageID != nil {
		copy := *value.parentMessageID
		value.parentMessageID = &copy
	}
	value.approved = append([]RunCapability{}, value.approved...)
	return value
}
func (value RunContext) Snapshot() RunContextSnapshot {
	value = value.clone()
	return RunContextSnapshot{value.messageID, value.origin, value.actorID, value.channel, value.Authority(), value.stampedBy, value.delegatedBy, value.parentMessageID, value.approved}
}
func (value RunContext) DerivePeerAgent(delegatedBy string) (RunContext, error) {
	if err := value.Validate(); err != nil {
		return RunContext{}, err
	}
	id, err := newMessageID()
	if err != nil {
		return RunContext{}, err
	}
	parent := value.messageID
	return RunContext{messageID: id, origin: "peer_agent", channel: "agent", authority: AuthorityPeerAgent, stampedBy: value.stampedBy, delegatedBy: &delegatedBy, parentMessageID: &parent}, nil
}
func (value RunContext) WithNewMessage(approved []RunCapability) (RunContext, error) {
	if err := value.Validate(); err != nil {
		return RunContext{}, err
	}
	id, err := newMessageID()
	if err != nil {
		return RunContext{}, err
	}
	parent := value.messageID
	value = value.clone()
	value.messageID = id
	value.parentMessageID = &parent
	value.approved = normalizeRunCapabilities(approved)
	if err := value.Validate(); err != nil {
		return RunContext{}, err
	}
	return value, nil
}
