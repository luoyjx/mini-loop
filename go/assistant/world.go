package assistant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// World is the simulated external mail and calendar service behind the mock
// MCP server. It lives in its own process with its own world.db: the daemon
// never opens that file and reaches the world only through MCP tool calls.
type World struct {
	db  *sql.DB
	now clock
}

const worldSchema = `
CREATE TABLE IF NOT EXISTS meta(key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS mail(
	seq INTEGER PRIMARY KEY AUTOINCREMENT, id TEXT NOT NULL UNIQUE, thread TEXT NOT NULL,
	folder TEXT NOT NULL, sender TEXT NOT NULL, sender_name TEXT NOT NULL, recipient TEXT NOT NULL,
	subject TEXT NOT NULL, body TEXT NOT NULL, category TEXT NOT NULL,
	event_id TEXT NOT NULL DEFAULT '', received_at INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS mail_folder ON mail(folder, seq);
CREATE TABLE IF NOT EXISTS calendar(
	id TEXT PRIMARY KEY, title TEXT NOT NULL, start_at INTEGER NOT NULL, end_at INTEGER NOT NULL,
	status TEXT NOT NULL, organizer TEXT NOT NULL, thread TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS effects(
	key TEXT PRIMARY KEY, kind TEXT NOT NULL, receipt TEXT NOT NULL, detail TEXT NOT NULL,
	created_at INTEGER NOT NULL);
`

// OwnerAddress is the simulated mailbox the assistant works for.
const OwnerAddress = "me@acme.example"

const maxPollPage = 100

// OpenWorld opens (and seeds once) the simulated mailbox and calendar.
func OpenWorld(ctx context.Context, path string, now clock) (*World, error) {
	if now == nil {
		now = time.Now
	}
	db, err := openSQLite(ctx, path, worldSchema)
	if err != nil {
		return nil, err
	}
	world := &World{db: db, now: now}
	if err := world.seed(ctx); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return world, nil
}

func (w *World) Close() error { return w.db.Close() }

func (w *World) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

func (w *World) seed(ctx context.Context) error {
	var done int
	if err := w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM meta WHERE key=?`, seedKey).Scan(&done); err != nil || done > 0 {
		return err
	}
	now := w.now()
	return w.inTx(ctx, func(tx *sql.Tx) error {
		mails := []Email{
			{ID: "m-seed-1", Thread: "t-q4-plan", From: ManagerAddress, FromName: "Alice", Subject: "Q4 规划材料周五前给我", Body: "周五例会前把 Q4 规划初稿发我，重点写人力和预算。", Category: CategoryPerson, ReceivedAt: now.Add(-3 * time.Hour)},
			{ID: "m-seed-2", Thread: "t-weekly-ai", From: "newsletter@ai-weekly.example", FromName: "AI 周报", Subject: "本周 AI 周报 #128", Body: "本周要闻：……", Category: CategoryNewsletter, ReceivedAt: now.Add(-5 * time.Hour)},
			{ID: "m-seed-3", Thread: "t-hiking", From: "bob@friends.example", FromName: "Bob", Subject: "周末爬山？", Body: "周六早上去爬山吗？", Category: CategoryPerson, ReceivedAt: now.Add(-20 * time.Hour)},
		}
		for _, mail := range mails {
			mail.Folder, mail.To = FolderInbox, OwnerAddress
			if err := insertMail(ctx, tx, mail); err != nil {
				return err
			}
		}
		events := []CalendarEvent{
			{ID: "e-seed-1", Title: "1:1 with Alice", Start: dayAt(now, 1, 10, 0), End: dayAt(now, 1, 11, 0), Status: EventConfirmed, Organizer: ManagerAddress},
			{ID: "e-seed-2", Title: "设计评审", Start: dayAt(now, 0, 15, 0), End: dayAt(now, 0, 16, 0), Status: EventConfirmed, Organizer: OwnerAddress},
		}
		for _, event := range events {
			if err := insertEvent(ctx, tx, event); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES(?,?)`, seedKey, now.Format(time.RFC3339))
		return err
	})
}

// ---- mail ----

func insertMail(ctx context.Context, q queryer, mail Email) error {
	_, err := q.ExecContext(ctx, `INSERT INTO mail(id,thread,folder,sender,sender_name,recipient,subject,body,category,event_id,received_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		mail.ID, string(mail.Thread), string(mail.Folder), mail.From, mail.FromName, mail.To, mail.Subject, mail.Body, string(mail.Category), mail.EventID, millis(mail.ReceivedAt))
	return err
}

const mailColumns = `id,thread,folder,sender,sender_name,recipient,subject,body,category,event_id,received_at`

func scanMail(row interface{ Scan(...any) error }, extra ...any) (Email, error) {
	var mail Email
	var thread, folder, category string
	var received int64
	targets := append([]any{&mail.ID, &thread, &folder, &mail.From, &mail.FromName, &mail.To, &mail.Subject, &mail.Body, &category, &mail.EventID, &received}, extra...)
	if err := row.Scan(targets...); err != nil {
		return Email{}, err
	}
	mail.Thread, mail.Folder, mail.Category, mail.ReceivedAt = ThreadID(thread), MailFolder(folder), MailCategory(category), fromMillis(received)
	return mail, nil
}

func (w *World) Mail(ctx context.Context, id string) (Email, bool, error) {
	mail, err := scanMail(w.db.QueryRowContext(ctx, `SELECT `+mailColumns+` FROM mail WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Email{}, false, nil
	}
	return mail, err == nil, err
}

func (w *World) ListMail(ctx context.Context, folder MailFolder, limit int) ([]Email, error) {
	rows, err := w.db.QueryContext(ctx, `SELECT `+mailColumns+` FROM mail WHERE folder=? ORDER BY received_at DESC, seq DESC LIMIT ?`, string(folder), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Email{}
	for rows.Next() {
		mail, err := scanMail(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, mail)
	}
	return result, rows.Err()
}

// MailBatch is one mail_poll page: inbox mail with seq > cursor, oldest first.
// Head is the newest inbox seq, so a first-time client can start from now.
type MailBatch struct {
	Mail   []InboundMail `json:"mail"`
	Cursor int64         `json:"cursor"`
	Head   int64         `json:"head"`
}

type InboundMail struct {
	Seq  int64 `json:"seq"`
	Mail Email `json:"mail"`
}

// Poll returns up to limit inbox messages after cursor. limit 0 only reports
// the head, which is how a new daemon skips mail that predates it.
func (w *World) Poll(ctx context.Context, cursor int64, limit int) (MailBatch, error) {
	if cursor < 0 || limit < 0 || limit > maxPollPage {
		return MailBatch{}, fmt.Errorf("cursor must be >= 0 and limit between 0 and %d", maxPollPage)
	}
	batch := MailBatch{Mail: []InboundMail{}, Cursor: cursor}
	if err := w.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM mail WHERE folder=?`, string(FolderInbox)).Scan(&batch.Head); err != nil {
		return MailBatch{}, err
	}
	if limit == 0 {
		return batch, nil
	}
	rows, err := w.db.QueryContext(ctx, `SELECT `+mailColumns+`,seq FROM mail WHERE folder=? AND seq>? ORDER BY seq LIMIT ?`, string(FolderInbox), cursor, limit)
	if err != nil {
		return MailBatch{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var seq int64
		mail, err := scanMail(rows, &seq)
		if err != nil {
			return MailBatch{}, err
		}
		batch.Mail = append(batch.Mail, InboundMail{Seq: seq, Mail: mail})
		batch.Cursor = seq
	}
	return batch, rows.Err()
}

// ---- calendar ----

func insertEvent(ctx context.Context, q queryer, event CalendarEvent) error {
	_, err := q.ExecContext(ctx, `INSERT INTO calendar(id,title,start_at,end_at,status,organizer,thread) VALUES(?,?,?,?,?,?,?)`,
		event.ID, event.Title, millis(event.Start), millis(event.End), string(event.Status), event.Organizer, string(event.Thread))
	return err
}

const eventColumns = `id,title,start_at,end_at,status,organizer,thread`

func scanEvent(row interface{ Scan(...any) error }) (CalendarEvent, error) {
	var event CalendarEvent
	var start, end int64
	var status, thread string
	if err := row.Scan(&event.ID, &event.Title, &start, &end, &status, &event.Organizer, &thread); err != nil {
		return CalendarEvent{}, err
	}
	event.Start, event.End, event.Status, event.Thread = fromMillis(start), fromMillis(end), EventStatus(status), ThreadID(thread)
	return event, nil
}

func (w *World) Event(ctx context.Context, id string) (CalendarEvent, bool, error) {
	event, err := scanEvent(w.db.QueryRowContext(ctx, `SELECT `+eventColumns+` FROM calendar WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return CalendarEvent{}, false, nil
	}
	return event, err == nil, err
}

// Events lists events overlapping [from, to), declined ones included so the
// page can show the outcome of an approved decline.
func (w *World) Events(ctx context.Context, from, to time.Time) ([]CalendarEvent, error) {
	rows, err := w.db.QueryContext(ctx, `SELECT `+eventColumns+` FROM calendar WHERE end_at>? AND start_at<? ORDER BY start_at`, millis(from), millis(to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []CalendarEvent{}
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

// ---- effects (idempotent by caller key) ----

// OutgoingMail asks the service to send one message. Key makes the send
// idempotent: a retry with the same key returns the first receipt.
type OutgoingMail struct {
	Key     string   `json:"idempotency_key"`
	Thread  ThreadID `json:"thread"`
	To      string   `json:"to"`
	Subject string   `json:"subject"`
	Body    string   `json:"body"`
}

// InviteReply answers an invite and notifies the organizer, idempotent by Key.
type InviteReply struct {
	Key      string         `json:"idempotency_key"`
	EventID  string         `json:"event_id"`
	Response InviteResponse `json:"response"`
	Subject  string         `json:"subject"`
	Note     string         `json:"note"`
}

// EffectReceipt identifies the external effect. Duplicate reports a replayed
// key whose effect had already been applied.
type EffectReceipt struct {
	Receipt   string `json:"receipt"`
	Detail    string `json:"detail"`
	Duplicate bool   `json:"duplicate"`
}

const (
	effectSend    = "mail_send"
	effectRespond = "calendar_respond"
)

// replay returns the stored receipt for key, or found=false. A key reused for
// a different kind of effect is refused.
func replay(ctx context.Context, tx *sql.Tx, key, kind string) (EffectReceipt, bool, error) {
	var storedKind string
	var receipt EffectReceipt
	err := tx.QueryRowContext(ctx, `SELECT kind,receipt,detail FROM effects WHERE key=?`, key).Scan(&storedKind, &receipt.Receipt, &receipt.Detail)
	if errors.Is(err, sql.ErrNoRows) {
		return EffectReceipt{}, false, nil
	}
	if err != nil {
		return EffectReceipt{}, false, err
	}
	if storedKind != kind {
		return EffectReceipt{}, false, fmt.Errorf("idempotency key %q was already used for %s", key, storedKind)
	}
	receipt.Duplicate = true
	return receipt, true, nil
}

func (w *World) sent(ctx context.Context, tx *sql.Tx, kind, key string, mail Email, detail string) (EffectReceipt, error) {
	var next int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0)+1 FROM mail`).Scan(&next); err != nil {
		return EffectReceipt{}, err
	}
	mail.ID, mail.Folder, mail.From, mail.FromName, mail.Category, mail.ReceivedAt = fmt.Sprintf("sent-%d", next), FolderSent, OwnerAddress, "我", CategoryPerson, w.now()
	if err := insertMail(ctx, tx, mail); err != nil {
		return EffectReceipt{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO effects(key,kind,receipt,detail,created_at) VALUES(?,?,?,?,?)`, key, kind, mail.ID, detail, millis(w.now())); err != nil {
		return EffectReceipt{}, err
	}
	return EffectReceipt{Receipt: mail.ID, Detail: detail}, nil
}

// Send delivers an outgoing message at most once per key.
func (w *World) Send(ctx context.Context, out OutgoingMail) (EffectReceipt, error) {
	if strings.TrimSpace(out.Key) == "" || !strings.Contains(out.To, "@") || strings.TrimSpace(out.Subject) == "" {
		return EffectReceipt{}, errors.New("idempotency_key, to (an address) and subject are required")
	}
	var receipt EffectReceipt
	err := w.inTx(ctx, func(tx *sql.Tx) error {
		previous, found, err := replay(ctx, tx, out.Key, effectSend)
		if err != nil || found {
			receipt = previous
			return err
		}
		receipt, err = w.sent(ctx, tx, effectSend, out.Key, Email{Thread: out.Thread, To: out.To, Subject: truncateRunes(out.Subject, 200), Body: truncateRunes(out.Body, maxDraftRunes)}, fmt.Sprintf("已发送邮件给 %s：%s", out.To, out.Subject))
		return err
	})
	return receipt, err
}

// RespondInvite updates the event and mails the organizer, at most once per key.
func (w *World) RespondInvite(ctx context.Context, reply InviteReply) (EffectReceipt, error) {
	if strings.TrimSpace(reply.Key) == "" || strings.TrimSpace(reply.EventID) == "" || (reply.Response != InviteAccept && reply.Response != InviteDecline) {
		return EffectReceipt{}, errors.New("idempotency_key, event_id and response (accept or decline) are required")
	}
	var receipt EffectReceipt
	err := w.inTx(ctx, func(tx *sql.Tx) error {
		previous, found, err := replay(ctx, tx, reply.Key, effectRespond)
		if err != nil || found {
			receipt = previous
			return err
		}
		event, err := scanEvent(tx.QueryRowContext(ctx, `SELECT `+eventColumns+` FROM calendar WHERE id=?`, reply.EventID))
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("event %q not found", reply.EventID)
		}
		if err != nil {
			return err
		}
		status := EventConfirmed
		if reply.Response == InviteDecline {
			status = EventDeclined
		}
		if _, err := tx.ExecContext(ctx, `UPDATE calendar SET status=? WHERE id=?`, string(status), event.ID); err != nil {
			return err
		}
		subject := strings.TrimSpace(reply.Subject)
		if subject == "" {
			subject = string(reply.Response) + "：" + event.Title
		}
		receipt, err = w.sent(ctx, tx, effectRespond, reply.Key, Email{Thread: event.Thread, To: event.Organizer, Subject: truncateRunes(subject, 200), Body: truncateRunes(reply.Note, maxDraftRunes)}, fmt.Sprintf("已回复邀约 %s：%s，并通知 %s", event.ID, reply.Response, event.Organizer))
		return err
	})
	return receipt, err
}

// ---- the simulated outside world ----

// DeliveredMail names a message the simulator just put in the inbox.
type DeliveredMail struct {
	ID     string   `json:"id"`
	Thread ThreadID `json:"thread"`
}

// Deliver puts a preset message (and its invite) into the inbox. The daemon
// learns about it only on its next mail_poll.
func (w *World) Deliver(ctx context.Context, preset Preset) (DeliveredMail, error) {
	var delivered DeliveredMail
	err := w.inTx(ctx, func(tx *sql.Tx) error {
		var next int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0)+1 FROM mail`).Scan(&next); err != nil {
			return err
		}
		now := w.now()
		mail, invite, err := presetMail(preset, next, now)
		if err != nil {
			return err
		}
		if invite != nil {
			if err := insertEvent(ctx, tx, *invite); err != nil {
				return err
			}
		}
		delivered = DeliveredMail{ID: mail.ID, Thread: mail.Thread}
		return insertMail(ctx, tx, mail)
	})
	return delivered, err
}

func (w *World) DeliverCustom(ctx context.Context, custom CustomMail) (DeliveredMail, error) {
	from := strings.TrimSpace(custom.From)
	if !strings.Contains(from, "@") || strings.TrimSpace(custom.Subject) == "" {
		return DeliveredMail{}, fmt.Errorf("from (an address) and subject are required")
	}
	name := strings.TrimSpace(custom.FromName)
	if name == "" {
		name = strings.SplitN(from, "@", 2)[0]
	}
	category := CategoryPerson
	if custom.Bulk {
		category = CategoryNewsletter
	}
	var delivered DeliveredMail
	err := w.inTx(ctx, func(tx *sql.Tx) error {
		var next int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0)+1 FROM mail`).Scan(&next); err != nil {
			return err
		}
		mail := Email{ID: fmt.Sprintf("m-%d", next), Thread: ThreadID(fmt.Sprintf("t-%d", next)), Folder: FolderInbox, From: from, FromName: truncateRunes(name, 40), To: OwnerAddress, Subject: truncateRunes(custom.Subject, 120), Body: truncateRunes(custom.Body, maxDraftRunes), Category: category, ReceivedAt: w.now()}
		delivered = DeliveredMail{ID: mail.ID, Thread: mail.Thread}
		return insertMail(ctx, tx, mail)
	})
	return delivered, err
}

// SimEvent is a confirmed meeting the simulator puts on the calendar, as if a
// colleague had booked it. Zero fields take the demo defaults.
type SimEvent struct {
	Title     string `json:"title"`
	InMinutes int    `json:"in_minutes"`
	Minutes   int    `json:"minutes"`
}

// AddEvent books a confirmed event that starts InMinutes from now. The daemon
// learns about it only through calendar_list.
func (w *World) AddEvent(ctx context.Context, sim SimEvent) (CalendarEvent, error) {
	title := strings.TrimSpace(sim.Title)
	if title == "" {
		title = "客户方案电话会"
	}
	if sim.InMinutes == 0 {
		sim.InMinutes = 12
	}
	if sim.Minutes == 0 {
		sim.Minutes = 30
	}
	if sim.InMinutes < 1 || sim.InMinutes > 24*60 || sim.Minutes < 5 || sim.Minutes > 8*60 {
		return CalendarEvent{}, errors.New("in_minutes must be 1-1440 and minutes 5-480")
	}
	var event CalendarEvent
	err := w.inTx(ctx, func(tx *sql.Tx) error {
		var next int64
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*)+1 FROM calendar`).Scan(&next); err != nil {
			return err
		}
		start := w.now().Truncate(time.Minute).Add(time.Duration(sim.InMinutes) * time.Minute)
		event = CalendarEvent{ID: fmt.Sprintf("e-cal-%d", next), Title: truncateRunes(title, 60), Start: start, End: start.Add(time.Duration(sim.Minutes) * time.Minute), Status: EventConfirmed, Organizer: ManagerAddress}
		return insertEvent(ctx, tx, event)
	})
	return event, err
}
