package assistant

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

const (
	defaultPollInterval = 2 * time.Second
	pollPage            = 50
)

// Poller discovers inbound mail through the connector's mail_poll cursor and
// turns each new message into one trigger. Ingest and cursor advance commit
// together, and a re-polled message is skipped by mail ID, so a crash at any
// point neither loses nor duplicates a trigger. A first-time daemon starts at
// the service's head instead of replaying old mail.
type Poller struct {
	store     *Store
	connector *Connector
	router    *Router
	now       clock
	interval  time.Duration
	log       io.Writer
	wake      chan struct{}

	mu      sync.Mutex // serializes PollOnce
	health  sync.Mutex
	lastErr string
}

func NewPoller(store *Store, connector *Connector, router *Router, now clock, interval time.Duration, log io.Writer) *Poller {
	if now == nil {
		now = time.Now
	}
	if interval <= 0 {
		interval = defaultPollInterval
	}
	if log == nil {
		log = io.Discard
	}
	return &Poller{store: store, connector: connector, router: router, now: now, interval: interval, log: log, wake: make(chan struct{}, 1)}
}

// Wake asks for an immediate poll, e.g. right after the simulator delivered.
func (p *Poller) Wake() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *Poller) Run(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		_, _ = p.PollOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-p.wake:
		case <-ticker.C:
		}
	}
}

// PollOnce drains every available page and returns the triggers it created.
func (p *Poller) PollOnce(ctx context.Context) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	total, err := p.drain(ctx)
	if ctx.Err() == nil {
		p.record(err)
	}
	if total > 0 {
		p.router.Wake()
	}
	return total, err
}

func (p *Poller) drain(ctx context.Context) (int, error) {
	cursor, ok, err := p.store.MailCursor(ctx)
	if err != nil {
		return 0, err
	}
	if !ok {
		head, err := p.connector.Poll(ctx, 0, 0)
		if err != nil {
			return 0, err
		}
		return 0, p.store.StartMailCursor(ctx, head.Head, p.now())
	}
	total := 0
	for {
		batch, err := p.connector.Poll(ctx, cursor, pollPage)
		if err != nil {
			return total, err
		}
		if len(batch.Mail) == 0 {
			return total, nil
		}
		created, err := p.store.IngestMail(ctx, batch.Mail, batch.Cursor, p.now())
		if err != nil {
			return total, err
		}
		total += created
		cursor = batch.Cursor
		if len(batch.Mail) < pollPage {
			return total, nil
		}
	}
}

// record keeps the last poll error for the page and logs only changes, so a
// stopped connector does not flood the launchd log every two seconds.
func (p *Poller) record(err error) {
	message := ""
	if err != nil {
		message = err.Error()
	}
	p.health.Lock()
	changed := message != p.lastErr
	p.lastErr = message
	p.health.Unlock()
	if !changed {
		return
	}
	if err != nil {
		fmt.Fprintf(p.log, "assistant: mail poll failing: %v\n", err)
	} else {
		fmt.Fprintln(p.log, "assistant: mail poll recovered")
	}
}

// LastError is the most recent poll failure, or "" when polling is healthy.
func (p *Poller) LastError() string {
	p.health.Lock()
	defer p.health.Unlock()
	return p.lastErr
}
