package assistant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Store is the assistant's durable state: the mock world (mail, calendar),
// the trigger queue, thread subscriptions, the proposal outbox, the channel
// log and the ledger. It is separate from the agent session store so the
// demo never edits that schema. One pinned connection serializes writers.
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS meta(key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS mail(
	id TEXT PRIMARY KEY, thread TEXT NOT NULL, folder TEXT NOT NULL,
	sender TEXT NOT NULL, sender_name TEXT NOT NULL, recipient TEXT NOT NULL,
	subject TEXT NOT NULL, body TEXT NOT NULL, category TEXT NOT NULL,
	event_id TEXT NOT NULL DEFAULT '', received_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS calendar(
	id TEXT PRIMARY KEY, title TEXT NOT NULL, start_at INTEGER NOT NULL,
	end_at INTEGER NOT NULL, status TEXT NOT NULL, organizer TEXT NOT NULL,
	thread TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS triggers(
	id INTEGER PRIMARY KEY AUTOINCREMENT, kind TEXT NOT NULL, thread TEXT NOT NULL,
	ref TEXT NOT NULL, summary TEXT NOT NULL, status TEXT NOT NULL,
	decision TEXT NOT NULL DEFAULT '', session_id TEXT NOT NULL DEFAULT '',
	attempts INTEGER NOT NULL DEFAULT 0, not_before INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS triggers_status ON triggers(status, not_before, id);
CREATE TABLE IF NOT EXISTS subscriptions(
	thread TEXT PRIMARY KEY, session_id TEXT NOT NULL, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS proposals(
	id INTEGER PRIMARY KEY AUTOINCREMENT, kind TEXT NOT NULL, trigger_id INTEGER NOT NULL,
	thread TEXT NOT NULL, session_id TEXT NOT NULL, action_id TEXT NOT NULL,
	recipient TEXT NOT NULL, subject TEXT NOT NULL, body TEXT NOT NULL,
	event_id TEXT NOT NULL DEFAULT '', response TEXT NOT NULL DEFAULT '',
	reason TEXT NOT NULL, status TEXT NOT NULL, decided_by TEXT NOT NULL DEFAULT '',
	dedupe TEXT NOT NULL UNIQUE, created_at INTEGER NOT NULL, decided_at INTEGER);
CREATE TABLE IF NOT EXISTS channel(
	id INTEGER PRIMARY KEY AUTOINCREMENT, role TEXT NOT NULL, text TEXT NOT NULL,
	proposal_id INTEGER, trigger_id INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS ledger(
	id INTEGER PRIMARY KEY AUTOINCREMENT, at INTEGER NOT NULL, kind TEXT NOT NULL,
	actor TEXT NOT NULL, trigger_id INTEGER NOT NULL DEFAULT 0, thread TEXT NOT NULL DEFAULT '',
	session_id TEXT NOT NULL DEFAULT '', action_id TEXT NOT NULL DEFAULT '',
	tool TEXT NOT NULL DEFAULT '', risk TEXT NOT NULL DEFAULT '', detail TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS routines(
	id TEXT PRIMARY KEY, name TEXT NOT NULL, hour INTEGER NOT NULL, minute INTEGER NOT NULL,
	prompt TEXT NOT NULL, enabled INTEGER NOT NULL, last_slot TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS routine_runs(
	routine_id TEXT NOT NULL, slot TEXT NOT NULL, trigger_id INTEGER NOT NULL,
	catchup INTEGER NOT NULL, created_at INTEGER NOT NULL, PRIMARY KEY(routine_id, slot));
`

func OpenStore(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	dsn := path
	if path != ":memory:" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		dsn = (&url.URL{Scheme: "file", Path: absolute}).String()
	}
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, statement := range []string{"PRAGMA journal_mode=WAL", "PRAGMA synchronous=NORMAL", "PRAGMA busy_timeout=5000", schema} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return nil, errors.Join(err, db.Close())
		}
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func millis(t time.Time) int64 { return t.UnixMilli() }
func fromMillis(v int64) time.Time {
	return time.UnixMilli(v)
}

// queryer is satisfied by *sql.DB and *sql.Tx so helpers run inside callers'
// transactions without opening a second statement on the pinned connection.
type queryer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

// ---- meta ----

func (s *Store) metaValue(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

func (s *Store) setMeta(ctx context.Context, q queryer, key, value string) error {
	_, err := q.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// ---- mail ----

func insertMail(ctx context.Context, q queryer, mail Email) error {
	_, err := q.ExecContext(ctx, `INSERT INTO mail(id,thread,folder,sender,sender_name,recipient,subject,body,category,event_id,received_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		mail.ID, string(mail.Thread), string(mail.Folder), mail.From, mail.FromName, mail.To, mail.Subject, mail.Body, string(mail.Category), mail.EventID, millis(mail.ReceivedAt))
	return err
}

const mailColumns = `id,thread,folder,sender,sender_name,recipient,subject,body,category,event_id,received_at`

func scanMail(row interface{ Scan(...any) error }) (Email, error) {
	var mail Email
	var thread, folder, category string
	var received int64
	if err := row.Scan(&mail.ID, &thread, &folder, &mail.From, &mail.FromName, &mail.To, &mail.Subject, &mail.Body, &category, &mail.EventID, &received); err != nil {
		return Email{}, err
	}
	mail.Thread, mail.Folder, mail.Category, mail.ReceivedAt = ThreadID(thread), MailFolder(folder), MailCategory(category), fromMillis(received)
	return mail, nil
}

func (s *Store) Mail(ctx context.Context, id string) (Email, bool, error) {
	mail, err := scanMail(s.db.QueryRowContext(ctx, `SELECT `+mailColumns+` FROM mail WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Email{}, false, nil
	}
	return mail, err == nil, err
}

func (s *Store) ListMail(ctx context.Context, folder MailFolder, limit int) ([]Email, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+mailColumns+` FROM mail WHERE folder=? ORDER BY received_at DESC LIMIT ?`, string(folder), limit)
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

func (s *Store) Event(ctx context.Context, id string) (CalendarEvent, bool, error) {
	event, err := scanEvent(s.db.QueryRowContext(ctx, `SELECT `+eventColumns+` FROM calendar WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return CalendarEvent{}, false, nil
	}
	return event, err == nil, err
}

// Events lists events overlapping [from, to), declined ones included so the
// page can show the outcome of an approved decline.
func (s *Store) Events(ctx context.Context, from, to time.Time) ([]CalendarEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+eventColumns+` FROM calendar WHERE end_at>? AND start_at<? ORDER BY start_at`, millis(from), millis(to))
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

// ---- triggers ----

func enqueueTrigger(ctx context.Context, q queryer, trigger Trigger, now time.Time) (int64, error) {
	result, err := q.ExecContext(ctx, `INSERT INTO triggers(kind,thread,ref,summary,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		string(trigger.Kind), string(trigger.Thread), trigger.Ref, trigger.Summary, string(TriggerPending), millis(now), millis(now))
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	err = appendLedger(ctx, q, LedgerEntry{At: now, Kind: LedgerTrigger, Actor: "environment", TriggerID: id, Thread: trigger.Thread, Detail: string(trigger.Kind) + "：" + truncateRunes(trigger.Summary, 80)})
	return id, err
}

const triggerColumns = `id,kind,thread,ref,summary,status,decision,session_id,attempts,created_at,updated_at`

func scanTrigger(row interface{ Scan(...any) error }) (Trigger, error) {
	var trigger Trigger
	var kind, thread, status string
	var created, updated int64
	if err := row.Scan(&trigger.ID, &kind, &thread, &trigger.Ref, &trigger.Summary, &status, &trigger.Decision, &trigger.SessionID, &trigger.Attempts, &created, &updated); err != nil {
		return Trigger{}, err
	}
	trigger.Kind, trigger.Thread, trigger.Status = TriggerKind(kind), ThreadID(thread), TriggerStatus(status)
	trigger.CreatedAt, trigger.UpdatedAt = fromMillis(created), fromMillis(updated)
	return trigger, nil
}

// ClaimTrigger takes the oldest due pending trigger whose thread has no
// running trigger, so one thread is processed in order while different
// threads run in parallel.
func (s *Store) ClaimTrigger(ctx context.Context, now time.Time) (Trigger, bool, error) {
	var claimed Trigger
	found := false
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		trigger, err := scanTrigger(tx.QueryRowContext(ctx, `SELECT `+triggerColumns+` FROM triggers t WHERE status=? AND not_before<=?
			AND NOT EXISTS (SELECT 1 FROM triggers r WHERE r.thread=t.thread AND r.status=?) ORDER BY id LIMIT 1`,
			string(TriggerPending), millis(now), string(TriggerRunning)))
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE triggers SET status=?, attempts=attempts+1, updated_at=? WHERE id=?`, string(TriggerRunning), millis(now), trigger.ID); err != nil {
			return err
		}
		trigger.Status, trigger.Attempts = TriggerRunning, trigger.Attempts+1
		claimed, found = trigger, true
		return nil
	})
	return claimed, found, err
}

func (s *Store) FinishTrigger(ctx context.Context, id int64, status TriggerStatus, decision, session string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE triggers SET status=?, decision=?, session_id=?, updated_at=? WHERE id=?`, string(status), decision, session, millis(now), id)
	return err
}

func (s *Store) RetryTrigger(ctx context.Context, id int64, notBefore time.Time, decision string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE triggers SET status=?, decision=?, not_before=?, updated_at=? WHERE id=?`, string(TriggerPending), decision, millis(notBefore), millis(now), id)
	return err
}

// RequeueInterrupted returns triggers a crashed process left running to the
// queue. Their workers may have produced proposals already; proposal dedupe
// keys make the re-run converge instead of duplicating drafts.
func (s *Store) RequeueInterrupted(ctx context.Context, now time.Time) (int64, error) {
	var count int64
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE triggers SET status=?, decision='进程重启后重新排队', updated_at=? WHERE status=?`, string(TriggerPending), millis(now), string(TriggerRunning))
		if err != nil {
			return err
		}
		count, err = result.RowsAffected()
		if err != nil || count == 0 {
			return err
		}
		return appendLedger(ctx, tx, LedgerEntry{At: now, Kind: LedgerRecovery, Actor: "daemon", Detail: fmt.Sprintf("重启恢复：%d 个中断的触发器重新排队", count)})
	})
	return count, err
}

func (s *Store) Triggers(ctx context.Context, limit int) ([]Trigger, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+triggerColumns+` FROM triggers ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Trigger{}
	for rows.Next() {
		trigger, err := scanTrigger(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, trigger)
	}
	return result, rows.Err()
}

// ---- subscriptions ----

func (s *Store) Subscription(ctx context.Context, thread ThreadID) (string, bool, error) {
	var session string
	err := s.db.QueryRowContext(ctx, `SELECT session_id FROM subscriptions WHERE thread=?`, string(thread)).Scan(&session)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return session, err == nil, err
}

func (s *Store) Subscribe(ctx context.Context, thread ThreadID, session string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO subscriptions(thread,session_id,created_at) VALUES(?,?,?) ON CONFLICT(thread) DO UPDATE SET session_id=excluded.session_id`, string(thread), session, millis(now))
	return err
}

// ---- proposals ----

const proposalColumns = `id,kind,trigger_id,thread,session_id,action_id,recipient,subject,body,event_id,response,reason,status,decided_by,created_at,decided_at`

func scanProposal(row interface{ Scan(...any) error }) (Proposal, error) {
	var p Proposal
	var kind, thread, response, status string
	var created int64
	var decided sql.NullInt64
	if err := row.Scan(&p.ID, &kind, &p.TriggerID, &thread, &p.SessionID, &p.ActionID, &p.To, &p.Subject, &p.Body, &p.EventID, &response, &p.Reason, &status, &p.DecidedBy, &created, &decided); err != nil {
		return Proposal{}, err
	}
	p.Kind, p.Thread, p.Response, p.Status, p.CreatedAt = ProposalKind(kind), ThreadID(thread), InviteResponse(response), ProposalStatus(status), fromMillis(created)
	if decided.Valid {
		at := fromMillis(decided.Int64)
		p.DecidedAt = &at
	}
	return p, nil
}

// CreateProposal is idempotent per (trigger, kind, target): a re-run after a
// crash returns the existing draft with created=false.
func (s *Store) CreateProposal(ctx context.Context, p Proposal, now time.Time) (Proposal, bool, error) {
	target := p.To
	if p.Kind == ProposalRespondInvite {
		target = p.EventID
	}
	dedupe := fmt.Sprintf("%d|%s|%s", p.TriggerID, p.Kind, strings.ToLower(target))
	var result Proposal
	created := false
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		existing, err := scanProposal(tx.QueryRowContext(ctx, `SELECT `+proposalColumns+` FROM proposals WHERE dedupe=?`, dedupe))
		if err == nil {
			result = existing
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		inserted, err := tx.ExecContext(ctx, `INSERT INTO proposals(kind,trigger_id,thread,session_id,action_id,recipient,subject,body,event_id,response,reason,status,dedupe,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			string(p.Kind), p.TriggerID, string(p.Thread), p.SessionID, p.ActionID, p.To, p.Subject, p.Body, p.EventID, string(p.Response), p.Reason, string(ProposalPending), dedupe, millis(now))
		if err != nil {
			return err
		}
		if p.ID, err = inserted.LastInsertId(); err != nil {
			return err
		}
		p.Status, p.CreatedAt, result, created = ProposalPending, now, p, true
		return appendLedger(ctx, tx, LedgerEntry{At: now, Kind: LedgerProposal, Actor: "worker", TriggerID: p.TriggerID, Thread: p.Thread, SessionID: p.SessionID, ActionID: p.ActionID, Detail: fmt.Sprintf("提案 #%d %s → %s：%s（待批准）", p.ID, p.Kind, p.To, p.Subject)})
	})
	return result, created, err
}

func (s *Store) Proposal(ctx context.Context, id int64) (Proposal, bool, error) {
	p, err := scanProposal(s.db.QueryRowContext(ctx, `SELECT `+proposalColumns+` FROM proposals WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Proposal{}, false, nil
	}
	return p, err == nil, err
}

func (s *Store) Proposals(ctx context.Context, limit int) ([]Proposal, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+proposalColumns+` FROM proposals ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Proposal{}
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (s *Store) ProposalsForTrigger(ctx context.Context, trigger int64) ([]Proposal, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+proposalColumns+` FROM proposals WHERE trigger_id=? ORDER BY id`, trigger)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Proposal{}
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

var ErrProposalDecided = errors.New("proposal is no longer pending")

// DecideProposal is the only path that releases an external effect. The status
// transition and the mock-world effect commit in one transaction, so a
// proposal is sent at most once even under concurrent clicks or a crash.
func (s *Store) DecideProposal(ctx context.Context, id int64, approve bool, actor string, now time.Time) (Proposal, error) {
	var decided Proposal
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		p, err := scanProposal(tx.QueryRowContext(ctx, `SELECT `+proposalColumns+` FROM proposals WHERE id=?`, id))
		if err != nil {
			return err
		}
		if p.Status != ProposalPending {
			return ErrProposalDecided
		}
		status := ProposalDenied
		if approve {
			status = ProposalSent
		}
		if _, err := tx.ExecContext(ctx, `UPDATE proposals SET status=?, decided_by=?, decided_at=? WHERE id=? AND status=?`, string(status), actor, millis(now), id, string(ProposalPending)); err != nil {
			return err
		}
		verb := "拒绝"
		if approve {
			verb = "批准"
		}
		if err := appendLedger(ctx, tx, LedgerEntry{At: now, Kind: LedgerDecision, Actor: actor, TriggerID: p.TriggerID, Thread: p.Thread, SessionID: p.SessionID, ActionID: p.ActionID, Detail: fmt.Sprintf("%s提案 #%d", verb, p.ID)}); err != nil {
			return err
		}
		if approve {
			if err := applyProposal(ctx, tx, p, now); err != nil {
				return err
			}
		}
		p.Status, p.DecidedBy, p.DecidedAt = status, actor, &now
		decided = p
		return nil
	})
	return decided, err
}

func applyProposal(ctx context.Context, tx *sql.Tx, p Proposal, now time.Time) error {
	detail := fmt.Sprintf("已发送邮件给 %s：%s", p.To, p.Subject)
	if p.Kind == ProposalRespondInvite {
		status := EventConfirmed
		if p.Response == InviteDecline {
			status = EventDeclined
		}
		if _, err := tx.ExecContext(ctx, `UPDATE calendar SET status=? WHERE id=?`, string(status), p.EventID); err != nil {
			return err
		}
		detail = fmt.Sprintf("已回复邀约 %s：%s，并通知 %s", p.EventID, p.Response, p.To)
	}
	mail := Email{ID: fmt.Sprintf("sent-%d", p.ID), Thread: p.Thread, Folder: FolderSent, From: "me@acme.example", FromName: "我", To: p.To, Subject: p.Subject, Body: p.Body, Category: CategoryPerson, ReceivedAt: now}
	if err := insertMail(ctx, tx, mail); err != nil {
		return err
	}
	return appendLedger(ctx, tx, LedgerEntry{At: now, Kind: LedgerEffect, Actor: "daemon（经人工批准）", TriggerID: p.TriggerID, Thread: p.Thread, SessionID: p.SessionID, ActionID: p.ActionID, Risk: "external", Detail: detail})
}

// ---- channel ----

func postChannel(ctx context.Context, q queryer, message ChannelMessage, now time.Time) (int64, error) {
	var proposal sql.NullInt64
	if message.ProposalID != nil {
		proposal = sql.NullInt64{Int64: *message.ProposalID, Valid: true}
	}
	result, err := q.ExecContext(ctx, `INSERT INTO channel(role,text,proposal_id,trigger_id,created_at) VALUES(?,?,?,?,?)`, string(message.Role), message.Text, proposal, message.TriggerID, millis(now))
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *Store) Post(ctx context.Context, message ChannelMessage, now time.Time) (int64, error) {
	return postChannel(ctx, s.db, message, now)
}

// PostAll writes one trigger's report and its cards in one transaction so
// concurrent workers never interleave their messages.
func (s *Store) PostAll(ctx context.Context, messages []ChannelMessage, now time.Time) (int, error) {
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		for _, message := range messages {
			if _, err := postChannel(ctx, tx, message, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(messages), nil
}

func (s *Store) Channel(ctx context.Context, limit int) ([]ChannelMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,role,text,proposal_id,trigger_id,created_at FROM (SELECT * FROM channel ORDER BY id DESC LIMIT ?) ORDER BY id`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ChannelMessage{}
	for rows.Next() {
		var m ChannelMessage
		var role string
		var proposal sql.NullInt64
		var created int64
		if err := rows.Scan(&m.ID, &role, &m.Text, &proposal, &m.TriggerID, &created); err != nil {
			return nil, err
		}
		m.Role, m.CreatedAt = ChannelRole(role), fromMillis(created)
		if proposal.Valid {
			id := proposal.Int64
			m.ProposalID = &id
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// CardPosted reports whether a proposal card already reached the channel, so a
// re-run after a crash does not post the same approval twice.
func (s *Store) CardPosted(ctx context.Context, proposal int64) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM channel WHERE proposal_id=?`, proposal).Scan(&count)
	return count > 0, err
}

// ---- ledger ----

func appendLedger(ctx context.Context, q queryer, entry LedgerEntry) error {
	_, err := q.ExecContext(ctx, `INSERT INTO ledger(at,kind,actor,trigger_id,thread,session_id,action_id,tool,risk,detail) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		millis(entry.At), string(entry.Kind), entry.Actor, entry.TriggerID, string(entry.Thread), entry.SessionID, entry.ActionID, entry.Tool, entry.Risk, entry.Detail)
	return err
}

func (s *Store) Append(ctx context.Context, entry LedgerEntry) error {
	return appendLedger(ctx, s.db, entry)
}

func (s *Store) Ledger(ctx context.Context, limit int) ([]LedgerEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,at,kind,actor,trigger_id,thread,session_id,action_id,tool,risk,detail FROM ledger ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []LedgerEntry{}
	for rows.Next() {
		var e LedgerEntry
		var at int64
		var kind, thread string
		if err := rows.Scan(&e.ID, &at, &kind, &e.Actor, &e.TriggerID, &thread, &e.SessionID, &e.ActionID, &e.Tool, &e.Risk, &e.Detail); err != nil {
			return nil, err
		}
		e.At, e.Kind, e.Thread = fromMillis(at), LedgerKind(kind), ThreadID(thread)
		result = append(result, e)
	}
	return result, rows.Err()
}

// ---- routines ----

func (s *Store) Routines(ctx context.Context) ([]Routine, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,hour,minute,prompt,enabled,last_slot FROM routines ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Routine{}
	for rows.Next() {
		var r Routine
		var id string
		var enabled int
		if err := rows.Scan(&id, &r.Name, &r.Hour, &r.Minute, &r.Prompt, &enabled, &r.LastSlot); err != nil {
			return nil, err
		}
		r.ID, r.Enabled = RoutineID(id), enabled != 0
		result = append(result, r)
	}
	return result, rows.Err()
}

func (s *Store) Routine(ctx context.Context, id RoutineID) (Routine, bool, error) {
	routines, err := s.Routines(ctx)
	if err != nil {
		return Routine{}, false, err
	}
	for _, routine := range routines {
		if routine.ID == id {
			return routine, true, nil
		}
	}
	return Routine{}, false, nil
}

// ClaimRoutineSlot records (routine, slot) and enqueues its trigger in one
// transaction. A slot fires at most once across ticks, restarts and processes.
func (s *Store) ClaimRoutineSlot(ctx context.Context, routine Routine, slot string, catchup bool, now time.Time) (int64, bool, error) {
	var triggerID int64
	claimed := false
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO routine_runs(routine_id,slot,trigger_id,catchup,created_at) VALUES(?,?,0,?,?)`, string(routine.ID), slot, boolInt(catchup), millis(now))
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil || affected == 0 {
			return err
		}
		summary := routine.Name + "（" + slot + "）"
		if catchup {
			summary += "，补跑错过的时段"
		}
		triggerID, err = enqueueTrigger(ctx, tx, Trigger{Kind: TriggerRoutine, Thread: ThreadID("routine:" + string(routine.ID) + ":" + slot), Ref: string(routine.ID), Summary: summary}, now)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE routine_runs SET trigger_id=? WHERE routine_id=? AND slot=?`, triggerID, string(routine.ID), slot); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE routines SET last_slot=? WHERE id=?`, slot, string(routine.ID)); err != nil {
			return err
		}
		claimed = true
		return appendLedger(ctx, tx, LedgerEntry{At: now, Kind: LedgerRoutine, Actor: "scheduler", TriggerID: triggerID, Detail: summary})
	})
	return triggerID, claimed, err
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// ---- world mutations used by the simulated channel ----

// DeliverMail stores an inbound message (and its invite) and enqueues the
// trigger in one transaction, as a webhook ingress would.
func (s *Store) DeliverMail(ctx context.Context, mail Email, invite *CalendarEvent, now time.Time) (int64, error) {
	var id int64
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		if invite != nil {
			if err := insertEvent(ctx, tx, *invite); err != nil {
				return err
			}
		}
		if err := insertMail(ctx, tx, mail); err != nil {
			return err
		}
		kind := TriggerEmail
		if mail.Category == CategoryInvite {
			kind = TriggerInvite
		}
		var err error
		id, err = enqueueTrigger(ctx, tx, Trigger{Kind: kind, Thread: mail.Thread, Ref: mail.ID, Summary: fmt.Sprintf("%s <%s>：%s", mail.FromName, mail.From, mail.Subject)}, now)
		return err
	})
	return id, err
}

// ChatFromUser records the user's channel message and enqueues it.
func (s *Store) ChatFromUser(ctx context.Context, text string, now time.Time) (int64, error) {
	var id int64
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		message, err := postChannel(ctx, tx, ChannelMessage{Role: RoleUser, Text: text}, now)
		if err != nil {
			return err
		}
		id, err = enqueueTrigger(ctx, tx, Trigger{Kind: TriggerChat, Thread: ChatThread, Ref: fmt.Sprintf("channel-%d", message), Summary: truncateRunes(text, MaxChatRunes)}, now)
		return err
	})
	return id, err
}

// MaxChatRunes bounds one channel message; the trigger keeps the full text.
const MaxChatRunes = 2000

// ChatThread is the one continuous conversation with the user.
const ChatThread ThreadID = "chat"

func truncateRunes(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}
