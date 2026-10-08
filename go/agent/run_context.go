package agent

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"slices"

	"github.com/luoyjx/mini-loop/go/runmeta"
)

type MessageID = runmeta.MessageID
type ActorID = runmeta.ActorID
type RunAuthority = runmeta.Authority
type RunCapability = runmeta.Capability

const (
	AuthorityUntrusted                   = runmeta.AuthorityUntrusted
	AuthorityExplicitHuman               = runmeta.AuthorityExplicitHuman
	AuthorityPeerAgent                   = runmeta.AuthorityPeerAgent
	CapabilityWorkflowLaunch             = runmeta.CapabilityWorkflowLaunch
	CapabilityWorkflowManage             = runmeta.CapabilityWorkflowManage
	CapabilityPersonalSkillCaptureSource = runmeta.CapabilityPersonalSkillCaptureSource
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
type RunContextSnapshot = runmeta.Snapshot

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

// AuthenticatedHTTPRunContext records the admitted principal without granting
// explicit-human tool authority. Null authentication still stamps anonymous.
func AuthenticatedHTTPRunContext(actor ActorID) (RunContext, error) {
	if actor == "" {
		return RunContext{}, errors.New("HTTP actor must be non-empty")
	}
	value, err := DefaultRunContext()
	if err != nil {
		return RunContext{}, err
	}
	value.origin, value.channel, value.stampedBy = "authenticated_http", "http", "mini_loop.server"
	value.actorID = &actor
	value.approved = []RunCapability{CapabilityPersonalSkillCaptureSource}
	return value, nil
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

// WorkflowHTTPRunContext is for an admitted human HTTP action, never model text.
// The HTTP adapter must establish authenticated deployment and session ownership.
// Stable message identity lets the action journal recognize a client retry.
func WorkflowHTTPRunContext(actor ActorID, action ActionID) (RunContext, error) {
	if actor == "" || action == "" {
		return RunContext{}, errors.New("workflow HTTP actor and action must be non-empty")
	}
	value, err := ExplicitHumanRunContext(HumanRunConfig{ActorID: &actor, Channel: "http", StampedBy: "mini_loop.server", ApprovedCapabilities: []RunCapability{CapabilityWorkflowLaunch}})
	if err != nil {
		return RunContext{}, err
	}
	value.messageID = MessageID("msg_" + string(action))
	return value, nil
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
	return RunContextSnapshot{
		MessageID: value.messageID, Origin: value.origin, ActorID: value.actorID,
		Channel: value.channel, Authority: value.Authority(), StampedBy: value.stampedBy,
		DelegatedBy: value.delegatedBy, ParentMessageID: value.parentMessageID,
		ApprovedCapabilities: value.approved,
	}
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

// DeriveNamedPeerAgent retains provenance while dropping human capabilities.
// The actor is a manager-established teammate, never a model-supplied owner.
func (value RunContext) DeriveNamedPeerAgent(delegatedBy string, actor ActorID) (RunContext, error) {
	peer, err := value.DerivePeerAgent(delegatedBy)
	if err != nil {
		return RunContext{}, err
	}
	peer.actorID = &actor
	return peer, nil
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
