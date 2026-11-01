// Package assistant is a Go-only, opt-in demo of a proactive personal agent.
//
// It layers the six personal-agent parts on top of the existing Go session
// manager: a long-running local daemon with SQLite state (own computer), mail
// and calendar reached only through a simulated MCP server in a child process
// (integrations), owner-scoped memory, durable routines and polled event
// triggers, a proposal outbox that only a human can release (approval gates),
// and an append-only ledger (audit log). Durable tasks let a worker follow one
// piece of work across many events: it schedules its own checks and the
// scheduler wakes it until the task is done, cancelled or out of budget. The
// user stays in charge with little attention: one list shows what waits for
// them, one switch pauses all processing, and their own messages run first on
// a worker slot kept for them. The channel is a simulated local web page.
// Nothing here changes the defaults of cmd/miniloop.
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
	// TriggerTaskWakeup is a task's scheduled check; Ref is "task-<id>".
	TriggerTaskWakeup TriggerKind = "task.wakeup"
	// TriggerUpcoming fires once per confirmed event shortly before it starts;
	// Ref is the event ID.
	TriggerUpcoming TriggerKind = "calendar.upcoming"
	// TriggerAnswer carries the user's answer to a worker's question back to
	// the question's thread; Ref is "question-<id>".
	TriggerAnswer TriggerKind = "user.answer"
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
// it came from; the worker reads that object through connector tools. Pending
// triggers of one thread that queue up while its worker is busy are claimed
// together and handled in one turn; the oldest leads the batch.
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

// A proposal moves pending → approved (human decision, local) → sent or
// failed (the outbox executed it through the connector), or pending → denied.
const (
	ProposalPending  ProposalStatus = "pending"
	ProposalApproved ProposalStatus = "approved"
	ProposalSent     ProposalStatus = "sent"
	ProposalDenied   ProposalStatus = "denied"
	ProposalFailed   ProposalStatus = "failed"
)

type InviteResponse string

const (
	InviteAccept  InviteResponse = "accept"
	InviteDecline InviteResponse = "decline"
)

// Proposal is a drafted external action. Creating one is a local write; only
// a human decision on the channel releases it to the outbox, which executes
// it through the connector under the idempotency key "proposal-<id>".
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
	Attempts  int            `json:"attempts,omitempty"`
	Receipt   string         `json:"receipt,omitempty"`
	LastError string         `json:"last_error,omitempty"`
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
	LedgerEffectErr LedgerKind = "effect_error"
	LedgerChannel   LedgerKind = "channel"
	LedgerRoutine   LedgerKind = "routine"
	LedgerRecovery  LedgerKind = "recovery"
	LedgerWorkerErr LedgerKind = "worker_error"
	LedgerTask      LedgerKind = "task"
	// LedgerControl records the user's pause switch and attention actions.
	LedgerControl LedgerKind = "control"
	// LedgerReport records a worker's structured report.
	LedgerReport LedgerKind = "report"
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

// Routine fires once per local day at Hour:Minute. A slot missed earlier the
// same day (daemon down or the Mac asleep) is caught up once.
type Routine struct {
	ID       RoutineID `json:"id"`
	Name     string    `json:"name"`
	Hour     int       `json:"hour"`
	Minute   int       `json:"minute"`
	Prompt   string    `json:"prompt"`
	Enabled  bool      `json:"enabled"`
	LastSlot string    `json:"last_slot"`
}

type TaskStatus string

// A task moves active → done (a worker reports completion), cancelled (the
// user) or stalled (its check budget ran out and the user takes over).
const (
	TaskActive    TaskStatus = "active"
	TaskDone      TaskStatus = "done"
	TaskCancelled TaskStatus = "cancelled"
	TaskStalled   TaskStatus = "stalled"
)

// Task is follow-up work that outlives one trigger. A worker creates it on
// its thread with a first check time; at that time the scheduler enqueues a
// task.wakeup trigger on the same thread, so the subscribed worker resumes
// with its context, and the worker either completes the task or schedules the
// next check. Claiming a check also sets a fallback check, so an active task
// always has a next check even when a turn crashes or forgets to schedule one,
// and MaxChecks bounds how often it can wake before the user takes over.
type Task struct {
	ID          int64      `json:"id"`
	Title       string     `json:"title"`
	Thread      ThreadID   `json:"thread"`
	SessionID   string     `json:"session_id"`
	TriggerID   int64      `json:"trigger_id"`
	Status      TaskStatus `json:"status"`
	Check       string     `json:"check"`
	Note        string     `json:"note"`
	Result      string     `json:"result,omitempty"`
	WakeAt      *time.Time `json:"wake_at,omitempty"`
	WakeReason  string     `json:"wake_reason"`
	Checks      int        `json:"checks"`
	MaxChecks   int        `json:"max_checks"`
	LastTrigger int64      `json:"last_trigger,omitempty"`
	ClosedBy    string     `json:"closed_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// ReportOutcome is what a worker's turn means for the user. The worker says it
// with the report tool instead of a marker in free text.
type ReportOutcome string

const (
	// ReportNotify posts the message to the channel.
	ReportNotify ReportOutcome = "notify"
	// ReportAsk posts one concrete question only the user can answer; it waits
	// on the attention list and the answer goes back to the same thread.
	ReportAsk ReportOutcome = "ask"
	// ReportQuiet keeps the user undisturbed; the reason goes to the ledger.
	ReportQuiet ReportOutcome = "quiet"
)

// Report is one worker turn's structured result. The router, the only writer
// to the channel, acts on it after the turn.
type Report struct {
	Outcome  ReportOutcome `json:"outcome"`
	Message  string        `json:"message,omitempty"`
	Question string        `json:"question,omitempty"`
	Reason   string        `json:"reason,omitempty"`
}

type QuestionStatus string

// A question moves open → answered (the user replied) and stays answered.
const (
	QuestionOpen     QuestionStatus = "open"
	QuestionAnswered QuestionStatus = "answered"
)

// Question is a worker's question that waits for the user. The answer becomes
// a user.answer trigger on the question's thread, so the worker that asked
// continues with its context.
type Question struct {
	ID         int64          `json:"id"`
	TriggerID  int64          `json:"trigger_id"`
	Thread     ThreadID       `json:"thread"`
	SessionID  string         `json:"session_id"`
	Text       string         `json:"text"`
	Status     QuestionStatus `json:"status"`
	Answer     string         `json:"answer,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	AnsweredAt *time.Time     `json:"answered_at,omitempty"`
}
