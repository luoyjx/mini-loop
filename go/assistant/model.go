// Package assistant is a Go-only, opt-in demo of a proactive personal agent.
//
// It layers the six personal-agent parts on top of the existing Go session
// manager: a long-running daemon with SQLite state (own computer), mock mail
// and calendar connectors (integrations), owner-scoped memory, durable
// routines and event triggers, a proposal outbox that only a human can release
// (approval gates), and an append-only ledger (audit log). The channel is a
// simulated local web page. Nothing here changes the defaults of cmd/miniloop.
package assistant

import (
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
)

// Owner is the single demo principal. Workers run as this owner, but with
// untrusted run authority because their prompts carry external content.
const Owner agent.OwnerID = "me"

type ThreadID string

type TriggerKind string

const (
	TriggerEmail   TriggerKind = "email.received"
	TriggerInvite  TriggerKind = "calendar.invite"
	TriggerChat    TriggerKind = "chat.message"
	TriggerRoutine TriggerKind = "routine.fired"
)

type TriggerStatus string

const (
	TriggerPending TriggerStatus = "pending"
	TriggerRunning TriggerStatus = "running"
	TriggerIgnored TriggerStatus = "ignored"
	TriggerDone    TriggerStatus = "done"
	TriggerQuiet   TriggerStatus = "quiet"
	TriggerFailed  TriggerStatus = "failed"
)

// Trigger is one environment event. Ref names the mail, invite or routine slot
// it came from; the worker reads that object through connector tools.
type Trigger struct {
	ID        int64         `json:"id"`
	Kind      TriggerKind   `json:"kind"`
	Thread    ThreadID      `json:"thread"`
	Ref       string        `json:"ref"`
	Summary   string        `json:"summary"`
	Status    TriggerStatus `json:"status"`
	Decision  string        `json:"decision"`
	SessionID string        `json:"session_id"`
	Attempts  int           `json:"attempts"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

type MailFolder string

const (
	FolderInbox MailFolder = "inbox"
	FolderSent  MailFolder = "sent"
)

type MailCategory string

const (
	CategoryPerson     MailCategory = "person"
	CategoryNewsletter MailCategory = "newsletter"
	CategoryInvite     MailCategory = "invite"
)

type Email struct {
	ID         string       `json:"id"`
	Thread     ThreadID     `json:"thread"`
	Folder     MailFolder   `json:"folder"`
	From       string       `json:"from"`
	FromName   string       `json:"from_name"`
	To         string       `json:"to"`
	Subject    string       `json:"subject"`
	Body       string       `json:"body"`
	Category   MailCategory `json:"category"`
	EventID    string       `json:"event_id,omitempty"`
	ReceivedAt time.Time    `json:"received_at"`
}

type EventStatus string

const (
	EventConfirmed EventStatus = "confirmed"
	EventInvited   EventStatus = "invited"
	EventDeclined  EventStatus = "declined"
)

type CalendarEvent struct {
	ID        string      `json:"id"`
	Title     string      `json:"title"`
	Start     time.Time   `json:"start"`
	End       time.Time   `json:"end"`
	Status    EventStatus `json:"status"`
	Organizer string      `json:"organizer"`
	Thread    ThreadID    `json:"thread"`
}

type ProposalKind string

const (
	ProposalSendEmail     ProposalKind = "send_email"
	ProposalRespondInvite ProposalKind = "respond_invite"
)

type ProposalStatus string

const (
	ProposalPending ProposalStatus = "pending"
	ProposalSent    ProposalStatus = "sent"
	ProposalDenied  ProposalStatus = "denied"
	ProposalFailed  ProposalStatus = "failed"
)

type InviteResponse string

const (
	InviteAccept  InviteResponse = "accept"
	InviteDecline InviteResponse = "decline"
)

// Proposal is a drafted external action. Creating one is a local write; only
// a human decision on the channel releases its effect.
type Proposal struct {
	ID        int64          `json:"id"`
	Kind      ProposalKind   `json:"kind"`
	TriggerID int64          `json:"trigger_id"`
	Thread    ThreadID       `json:"thread"`
	SessionID string         `json:"session_id"`
	ActionID  string         `json:"action_id"`
	To        string         `json:"to"`
	Subject   string         `json:"subject"`
	Body      string         `json:"body"`
	EventID   string         `json:"event_id,omitempty"`
	Response  InviteResponse `json:"response,omitempty"`
	Reason    string         `json:"reason"`
	Status    ProposalStatus `json:"status"`
	DecidedBy string         `json:"decided_by,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	DecidedAt *time.Time     `json:"decided_at,omitempty"`
}

type ChannelRole string

const (
	RoleAssistant ChannelRole = "assistant"
	RoleUser      ChannelRole = "user"
	RoleSystem    ChannelRole = "system"
)

type ChannelMessage struct {
	ID         int64       `json:"id"`
	Role       ChannelRole `json:"role"`
	Text       string      `json:"text"`
	ProposalID *int64      `json:"proposal_id,omitempty"`
	TriggerID  int64       `json:"trigger_id,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
}

type LedgerKind string

const (
	LedgerTrigger   LedgerKind = "trigger"
	LedgerFilter    LedgerKind = "filter"
	LedgerRoute     LedgerKind = "route"
	LedgerTool      LedgerKind = "tool"
	LedgerProposal  LedgerKind = "proposal"
	LedgerDecision  LedgerKind = "decision"
	LedgerEffect    LedgerKind = "effect"
	LedgerChannel   LedgerKind = "channel"
	LedgerRoutine   LedgerKind = "routine"
	LedgerRecovery  LedgerKind = "recovery"
	LedgerWorkerErr LedgerKind = "worker_error"
)

// LedgerEntry ties an effect to the trigger, session, gate action and the
// actor who allowed it. Rows are append-only.
type LedgerEntry struct {
	ID        int64      `json:"id"`
	At        time.Time  `json:"at"`
	Kind      LedgerKind `json:"kind"`
	Actor     string     `json:"actor"`
	TriggerID int64      `json:"trigger_id,omitempty"`
	Thread    ThreadID   `json:"thread,omitempty"`
	SessionID string     `json:"session_id,omitempty"`
	ActionID  string     `json:"action_id,omitempty"`
	Tool      string     `json:"tool,omitempty"`
	Risk      string     `json:"risk,omitempty"`
	Detail    string     `json:"detail"`
}

type RoutineID string

// Routine fires once per local day at Hour:Minute. A missed slot is caught up
// once on start when the daemon was down at that time.
type Routine struct {
	ID       RoutineID `json:"id"`
	Name     string    `json:"name"`
	Hour     int       `json:"hour"`
	Minute   int       `json:"minute"`
	Prompt   string    `json:"prompt"`
	Enabled  bool      `json:"enabled"`
	LastSlot string    `json:"last_slot"`
}
