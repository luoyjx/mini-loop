package assistant

import (
	"context"
	"fmt"
	"io"
	"time"
)

const (
	outboxAttempts = 5
	outboxPoll     = 2 * time.Second
	outboxActor    = "outbox（经人工批准，MCP）"
)

// Outbox executes approved proposals through the connector. It is the only
// caller of mail_send and calendar_respond. Each proposal uses one
// idempotency key for every attempt, so a retry after a timeout or a crash
// between the remote effect and the local "sent" commit converges on exactly
// one external effect. Server refusals fail at once; transport failures back
// off and retry, and approved proposals resume after a restart.
type Outbox struct {
	store     *Store
	connector *Connector
	now       clock
	log       io.Writer
	wake      chan struct{}
	backoff   func(attempt int) time.Duration
	// settled runs after an outcome is recorded: a sent or failed proposal
	// may have brought a task's check forward.
	settled func()
}

func NewOutbox(store *Store, connector *Connector, now clock, log io.Writer) *Outbox {
	if now == nil {
		now = time.Now
	}
	if log == nil {
		log = io.Discard
	}
	return &Outbox{store: store, connector: connector, now: now, log: log, wake: make(chan struct{}, 1),
		backoff: func(attempt int) time.Duration { return time.Duration(attempt*attempt) * 2 * time.Second }}
}

// Wake asks the outbox to look for approved work now.
func (o *Outbox) Wake() {
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

func (o *Outbox) Run(ctx context.Context) {
	ticker := time.NewTicker(outboxPoll)
	defer ticker.Stop()
	for {
		for o.Step(ctx) {
		}
		select {
		case <-ctx.Done():
			return
		case <-o.wake:
		case <-ticker.C:
		}
	}
}

// Step executes at most one due approved proposal and reports whether it did.
func (o *Outbox) Step(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	p, ok, err := o.store.NextApproved(ctx, o.now())
	if err != nil {
		if ctx.Err() == nil {
			fmt.Fprintf(o.log, "assistant: outbox: %v\n", err)
		}
		return false
	}
	if !ok {
		return false
	}
	receipt, err := o.execute(ctx, p)
	if ctx.Err() != nil {
		// Shutdown mid-call: the proposal stays approved and the next process
		// retries it under the same key.
		return false
	}
	now := o.now()
	if err == nil {
		err = o.store.CompleteProposal(ctx, p, receipt, outboxActor, now)
	} else {
		final := !retryable(err) || p.Attempts+1 >= outboxAttempts
		err = o.store.FailProposalAttempt(ctx, p, err, final, now.Add(o.backoff(p.Attempts+1)), outboxActor, now)
	}
	if err != nil {
		fmt.Fprintf(o.log, "assistant: outbox record #%d: %v\n", p.ID, err)
		return false
	}
	if o.settled != nil {
		o.settled()
	}
	return true
}

func (o *Outbox) execute(ctx context.Context, p Proposal) (EffectReceipt, error) {
	key := IdempotencyKey(p.ID)
	switch p.Kind {
	case ProposalSendEmail:
		return o.connector.SendMail(ctx, OutgoingMail{Key: key, Thread: p.Thread, To: p.To, Subject: p.Subject, Body: p.Body})
	case ProposalRespondInvite:
		return o.connector.RespondInvite(ctx, InviteReply{Key: key, EventID: p.EventID, Response: p.Response, Subject: p.Subject, Note: p.Body})
	default:
		return EffectReceipt{}, &ToolError{Tool: "outbox", Message: fmt.Sprintf("unknown proposal kind %q", p.Kind)}
	}
}
