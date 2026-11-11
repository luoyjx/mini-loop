package assistant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Remote channel names; each is also the source tag on the user's lines.
const (
	RemoteTelegram = "telegram"
	RemoteDiscord  = "discord"
	RemoteEmail    = "email"
)

const (
	// remoteDeliverTick is how often a bridge looks for new assistant lines
	// besides its wake-ups.
	remoteDeliverTick = 2 * time.Second
	// remoteRetry is the pause after a failed receive.
	remoteRetry = 5 * time.Second
	// remoteDeliverPage bounds one delivery round.
	remoteDeliverPage = 20
	// remoteCardHint and remoteQuestionHint tell the remote reader what
	// they can and cannot do from there.
	remoteCardHint     = "（到本机助理网页批准或拒绝；这里不能批准）"
	remoteQuestionHint = "（直接回复这条消息即可回答）"
)

// RemoteChannel is a real messaging service the owner talks to the assistant
// through, besides the local page: a private chat with a Telegram bot, a DM
// with a Discord bot, or a mailbox the assistant reads and writes. Each one
// talks to exactly one person, the owner, whose account it checks on every
// message. Lines arriving there join the page's chat thread; the assistant's
// lines are copied there.
type RemoteChannel interface {
	// Name is the stable key ("telegram", "discord", "email").
	Name() string
	// Label is what the page and the ledger call it.
	Label() string
	// Account names the owner's account there, for the page.
	Account() string
	// Interval is the pause between receives; 0 for a long poll that waits
	// on the service itself.
	Interval() time.Duration
	// Receive returns what arrived after cursor and the cursor to store with
	// it. An empty cursor asks for the service's head: a first start skips
	// history instead of replaying it.
	Receive(ctx context.Context, cursor string) (RemoteBatch, error)
	// Send delivers one assistant line to the owner and returns the service's
	// IDs for what it posted (a long line may take several messages).
	Send(ctx context.Context, message RemoteOutbound) ([]string, error)
}

// RemoteBatch is one receive: the messages in order and the new cursor.
type RemoteBatch struct {
	Messages []RemoteInbound
	Cursor   string
}

// RemoteInbound is one message from the service. Refused is set when the
// channel would not accept it (another sender, not text, a mail that failed
// sender authentication); the bridge records the refusal and drops the text.
type RemoteInbound struct {
	ID      string
	Text    string
	ReplyTo string
	Sender  string
	Refused string
}

// RemoteOutbound is one assistant line on its way to a remote channel.
type RemoteOutbound struct {
	MessageID int64
	Text      string
	// Subject is a short title, used by email.
	Subject string
}

// RemoteRejectedError is a send the service refused for this message's
// content. Retrying cannot help, so the bridge records it and moves on
// instead of blocking every later line.
type RemoteRejectedError struct{ Err error }

func (e *RemoteRejectedError) Error() string { return e.Err.Error() }
func (e *RemoteRejectedError) Unwrap() error { return e.Err }

// RemoteStatus is what the page shows for one remote channel. Counts and
// errors are since this process started.
type RemoteStatus struct {
	Name         string     `json:"name"`
	Label        string     `json:"label"`
	Account      string     `json:"account"`
	Received     int        `json:"received"`
	Refused      int        `json:"refused"`
	Sent         int        `json:"sent"`
	LastReceived *time.Time `json:"last_received,omitempty"`
	LastSent     *time.Time `json:"last_sent,omitempty"`
	ReceiveError string     `json:"receive_error,omitempty"`
	SendError    string     `json:"send_error,omitempty"`
}

// Bridge connects one remote channel to the store. Its receive loop records
// the owner's messages as chat on the page's chat thread, or as the answer
// to a question when the owner replied to that question's message. Its
// delivery loop copies the assistant's lines (not the user's) to the service
// in channel order. Both loops keep durable cursors: received messages,
// their dedupe links and the receive cursor commit together, and a line is
// marked delivered right after the service accepted it, so a crash in
// between sends that line again once. While the user has paused processing,
// messages are still recorded and delivery waits.
type Bridge struct {
	channel RemoteChannel
	store   *Store
	wake    func()
	health  *Health
	now     clock
	log     io.Writer
	deliver chan struct{}

	mu     sync.Mutex
	status RemoteStatus
}

func NewBridge(channel RemoteChannel, store *Store, wake func(), health *Health, now clock, log io.Writer) *Bridge {
	if now == nil {
		now = time.Now
	}
	if log == nil {
		log = io.Discard
	}
	if wake == nil {
		wake = func() {}
	}
	return &Bridge{channel: channel, store: store, wake: wake, health: health, now: now, log: log, deliver: make(chan struct{}, 1),
		status: RemoteStatus{Name: channel.Name(), Label: channel.Label(), Account: channel.Account()}}
}

// Status is the page's view of this bridge.
func (b *Bridge) Status() RemoteStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.status
}

// Wake asks for a delivery round now, e.g. after the user resumed.
func (b *Bridge) Wake() {
	select {
	case b.deliver <- struct{}{}:
	default:
	}
}

// ReceiveLoop receives until ctx ends.
func (b *Bridge) ReceiveLoop(ctx context.Context) {
	for ctx.Err() == nil {
		_, err := b.ReceiveOnce(ctx)
		wait := b.channel.Interval()
		if err != nil && wait < remoteRetry {
			wait = remoteRetry
		}
		if wait > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(wait):
			}
		}
	}
}

// ReceiveOnce runs one receive and records what it accepted.
func (b *Bridge) ReceiveOnce(ctx context.Context) (int, error) {
	name := b.channel.Name()
	cursor, _, err := b.store.metaValue(ctx, remoteCursorKey(name))
	var batch RemoteBatch
	if err == nil {
		batch, err = b.channel.Receive(ctx, cursor)
	}
	accepted, refused := 0, 0
	if err == nil {
		accepted, refused, err = b.store.ReceiveRemote(ctx, name, b.channel.Label(), batch, b.now())
	}
	if ctx.Err() != nil {
		return accepted, err
	}
	b.health.Record(remoteHealth(name, false), err, b.now())
	b.mu.Lock()
	changed := errorText(err) != b.status.ReceiveError
	b.status.ReceiveError = errorText(err)
	b.status.Received += accepted
	b.status.Refused += refused
	if accepted > 0 {
		at := b.now()
		b.status.LastReceived = &at
	}
	b.mu.Unlock()
	b.logChange(changed, "receive", err)
	if accepted > 0 {
		b.wake()
	}
	return accepted, err
}

// DeliverLoop copies new assistant lines until ctx ends.
func (b *Bridge) DeliverLoop(ctx context.Context) {
	ticker := time.NewTicker(remoteDeliverTick)
	defer ticker.Stop()
	for {
		_, _ = b.DeliverOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-b.deliver:
		case <-ticker.C:
		}
	}
}

// DeliverOnce sends the lines posted since the delivery cursor, in order,
// and stops at the first failure so order holds.
func (b *Bridge) DeliverOnce(ctx context.Context) (int, error) {
	name := b.channel.Name()
	// Listing first pins a new channel's head even while paused, so lines
	// written during the pause still go out on resume.
	pending, err := b.store.RemotePending(ctx, name, remoteDeliverPage)
	if _, paused := b.store.Paused(); paused && err == nil {
		return 0, nil
	}
	sent := 0
	for _, line := range pending {
		ids, sendErr := b.channel.Send(ctx, outbound(line))
		var rejected *RemoteRejectedError
		switch {
		case errors.As(sendErr, &rejected):
			err = b.store.SkipRemote(ctx, name, b.channel.Label(), line.ID, rejected.Error(), b.now())
		case sendErr != nil:
			err = sendErr
		default:
			if err = b.store.SentRemote(ctx, name, line.ID, ids, b.now()); err == nil {
				sent++
			}
		}
		if err != nil {
			break
		}
	}
	if ctx.Err() != nil {
		return sent, err
	}
	b.health.Record(remoteHealth(name, true), err, b.now())
	b.mu.Lock()
	changed := errorText(err) != b.status.SendError
	b.status.SendError = errorText(err)
	b.status.Sent += sent
	if sent > 0 {
		at := b.now()
		b.status.LastSent = &at
	}
	b.mu.Unlock()
	b.logChange(changed, "send", err)
	return sent, err
}

// logChange logs only transitions, so a service outage does not flood the
// launchd log.
func (b *Bridge) logChange(changed bool, what string, err error) {
	if !changed {
		return
	}
	if err != nil {
		fmt.Fprintf(b.log, "assistant: %s %s failing: %v\n", b.channel.Name(), what, err)
	} else {
		fmt.Fprintf(b.log, "assistant: %s %s recovered\n", b.channel.Name(), what)
	}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return truncateRunes(err.Error(), 200)
}

// pendingLine is one assistant line waiting for a remote channel; Question
// is set when it carries a question that is still open.
type pendingLine struct {
	ChannelMessage
	Subject  string
	Question bool
}

// outbound adapts a channel line for a remote reader: an approval card says
// it can only be decided on the page, and an open question says a reply
// answers it.
func outbound(line pendingLine) RemoteOutbound {
	text := line.Text
	switch {
	case line.ProposalID != nil:
		text += "\n" + remoteCardHint
	case line.Question:
		text += "\n" + remoteQuestionHint
	}
	subject := strings.TrimSpace(strings.SplitN(line.Text, "\n", 2)[0])
	subject = strings.TrimSpace(strings.TrimPrefix(subject, "❓"))
	return RemoteOutbound{MessageID: line.ID, Text: text, Subject: "mini-loop 助理：" + truncateRunes(subject, 30)}
}

func remoteCursorKey(name string) string   { return "remote_" + name + "_cursor" }
func remoteDeliveryKey(name string) string { return "remote_" + name + "_delivered" }

// ReceiveRemote records one receive in one transaction: each new message
// becomes the user's chat line (tagged with the channel) and a chat trigger,
// or answers the open question whose message it replies to; refusals are
// counted in the ledger; the receive cursor advances. A message seen before
// is skipped by its external ID, so a receive repeated after a crash adds
// nothing.
func (s *Store) ReceiveRemote(ctx context.Context, source, label string, batch RemoteBatch, now time.Time) (accepted, refused int, err error) {
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		accepted, refused = 0, 0
		refusals := []string{}
		for _, message := range batch.Messages {
			if message.Refused != "" {
				refused++
				refusals = append(refusals, truncateRunes(firstNonEmpty(message.Sender, "未知发送者"), 60)+"："+message.Refused)
				continue
			}
			text := truncateRunes(message.Text, MaxChatRunes)
			if text == "" || message.ID == "" {
				continue
			}
			inserted, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO remote_links(source,external_id,message_id,direction,created_at) VALUES(?,?,0,'in',?)`, source, message.ID, millis(now))
			if err != nil {
				return err
			}
			fresh, err := inserted.RowsAffected()
			if err != nil {
				return err
			}
			if fresh == 0 {
				continue
			}
			line, trigger, detail, err := receiveLine(ctx, tx, source, label, message, text, now)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE remote_links SET message_id=? WHERE source=? AND external_id=?`, line, source, message.ID); err != nil {
				return err
			}
			if err := appendLedger(ctx, tx, LedgerEntry{At: now, Kind: LedgerRemote, Actor: "me（" + label + "）", TriggerID: trigger, Thread: ChatThread, Detail: detail}); err != nil {
				return err
			}
			accepted++
		}
		if refused > 0 {
			detail := fmt.Sprintf("%s：拒收 %d 条消息（%s）", label, refused, strings.Join(refusals, "；"))
			if err := appendLedger(ctx, tx, LedgerEntry{At: now, Kind: LedgerRemote, Actor: "router", Detail: truncateRunes(detail, 400)}); err != nil {
				return err
			}
		}
		return s.setMeta(ctx, tx, remoteCursorKey(source), batch.Cursor)
	})
	return accepted, refused, err
}

// receiveLine records one accepted message: the answer to an open question
// when it replies to that question's line, else a chat line.
func receiveLine(ctx context.Context, tx *sql.Tx, source, label string, message RemoteInbound, text string, now time.Time) (int64, int64, string, error) {
	if message.ReplyTo != "" {
		question, err := openQuestionFor(ctx, tx, source, message.ReplyTo)
		if err != nil {
			return 0, 0, "", err
		}
		if question != 0 {
			_, trigger, line, err := answerQuestion(ctx, tx, question, text, "me（"+label+"）", source, now)
			return line, trigger, fmt.Sprintf("%s：你回复了提问 #%d，作为回答交回原 thread", label, question), err
		}
	}
	line, trigger, err := chatFromUser(ctx, tx, text, source, now)
	return line, trigger, fmt.Sprintf("%s：收到你的消息，进入信道的 chat thread", label), err
}

// openQuestionFor finds the open question whose channel line was delivered
// to this remote channel as external message replyTo; 0 when there is none.
func openQuestionFor(ctx context.Context, tx *sql.Tx, source, replyTo string) (int64, error) {
	var question int64
	err := tx.QueryRowContext(ctx, `SELECT q.id FROM remote_links l JOIN channel c ON c.id=l.message_id
		JOIN questions q ON q.trigger_id=c.trigger_id AND c.trigger_id<>0
		WHERE l.source=? AND l.external_id=? AND l.direction='out' AND q.status=?`, source, replyTo, string(QuestionOpen)).Scan(&question)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return question, err
}

// RemotePending lists the assistant and system lines a remote channel has
// not received yet, oldest first. A channel's first call starts it at the
// channel's head, so enabling it does not replay history.
func (s *Store) RemotePending(ctx context.Context, source string, limit int) ([]pendingLine, error) {
	value, ok, err := s.metaValue(ctx, remoteDeliveryKey(source))
	if err != nil {
		return nil, err
	}
	if !ok {
		var head int64
		if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM channel`).Scan(&head); err != nil {
			return nil, err
		}
		return nil, s.setMeta(ctx, s.db, remoteDeliveryKey(source), fmt.Sprint(head))
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.role,c.text,c.proposal_id,c.trigger_id,c.created_at,
		EXISTS(SELECT 1 FROM questions q WHERE q.trigger_id=c.trigger_id AND c.trigger_id<>0 AND q.status=?)
		FROM channel c WHERE c.id>? AND c.role<>? ORDER BY c.id LIMIT ?`, string(QuestionOpen), value, string(RoleUser), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lines := []pendingLine{}
	for rows.Next() {
		var line pendingLine
		var role string
		var proposal sql.NullInt64
		var created int64
		if err := rows.Scan(&line.ID, &role, &line.Text, &proposal, &line.TriggerID, &created, &line.Question); err != nil {
			return nil, err
		}
		line.Role, line.CreatedAt = ChannelRole(role), fromMillis(created)
		if proposal.Valid {
			id := proposal.Int64
			line.ProposalID = &id
		}
		// Only the question line itself invites a reply, not the cards
		// posted with it.
		line.Question = line.Question && line.ProposalID == nil && strings.Contains(line.Text, "❓")
		lines = append(lines, line)
	}
	return lines, rows.Err()
}

// SentRemote links the service's IDs to the line, so a reply to one of them
// can answer its question, and moves the delivery cursor past it.
func (s *Store) SentRemote(ctx context.Context, source string, line int64, ids []string, now time.Time) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO remote_links(source,external_id,message_id,direction,created_at) VALUES(?,?,?,'out',?)`, source, id, line, millis(now)); err != nil {
				return err
			}
		}
		return s.setMeta(ctx, tx, remoteDeliveryKey(source), fmt.Sprint(line))
	})
}

// SkipRemote records a line the service refused and moves past it.
func (s *Store) SkipRemote(ctx context.Context, source, label string, line int64, reason string, now time.Time) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if err := appendLedger(ctx, tx, LedgerEntry{At: now, Kind: LedgerRemote, Actor: "router", Detail: truncateRunes(fmt.Sprintf("%s：服务拒收信道消息 #%d，跳过：%s", label, line, reason), 300)}); err != nil {
			return err
		}
		return s.setMeta(ctx, tx, remoteDeliveryKey(source), fmt.Sprint(line))
	})
}

// splitRunes cuts text into parts of at most limit runes, preferring line
// breaks, for services that cap a message's length.
func splitRunes(text string, limit int) []string {
	runes := []rune(text)
	parts := []string{}
	for len(runes) > limit {
		cut := limit
		for i := limit; i > limit/2; i-- {
			if runes[i-1] == '\n' {
				cut = i
				break
			}
		}
		parts = append(parts, strings.TrimRight(string(runes[:cut]), "\n"))
		runes = runes[cut:]
	}
	return append(parts, string(runes))
}

// remoteBase checks a service base URL: HTTPS, or plain HTTP to a loopback
// host (a local Bot API server, a test). The token travels with every
// request, so it must not cross the network in the clear.
func remoteBase(base, fallback string) (string, error) {
	if base == "" {
		base = fallback
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("bad API base URL %q", base)
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopbackHost(parsed.Hostname())) {
		return "", fmt.Errorf("API base URL %q must use https (plain http only to a loopback host)", base)
	}
	return strings.TrimRight(base, "/"), nil
}

// loopbackHost reports whether host names this machine.
func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
