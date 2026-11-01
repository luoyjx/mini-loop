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

	"github.com/luoyjx/mini-loop/go/agent"
	_ "github.com/mattn/go-sqlite3"
)

// Store is the daemon's durable state: the trigger queue with its mail-poll
// cursor, thread subscriptions, the proposal outbox, the channel log and the
// ledger. Mail and calendar are not here: they belong to the external service
// behind the MCP connector. It is separate from the agent session store so the
// demo never edits that schema. One pinned connection serializes writers.
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS meta(key TEXT PRIMARY KEY, value TEXT NOT NULL);
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
	dedupe TEXT NOT NULL UNIQUE, created_at INTEGER NOT NULL, decided_at INTEGER,
	attempts INTEGER NOT NULL DEFAULT 0, not_before INTEGER NOT NULL DEFAULT 0,
	receipt TEXT NOT NULL DEFAULT '', last_error TEXT NOT NULL DEFAULT '');
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
CREATE TABLE IF NOT EXISTS ingested(
	mail_id TEXT PRIMARY KEY, seq INTEGER NOT NULL, trigger_id INTEGER NOT NULL,
	created_at INTEGER NOT NULL);
`

// addedColumns upgrades a data directory written by the first demo, whose
// proposals table predates the outbox. Its old mail and calendar tables are
// left in place and unused.
var addedColumns = []struct{ table, column, definition string }{
	{"proposals", "attempts", "INTEGER NOT NULL DEFAULT 0"},
	{"proposals", "not_before", "INTEGER NOT NULL DEFAULT 0"},
	{"proposals", "receipt", "TEXT NOT NULL DEFAULT ''"},
	{"proposals", "last_error", "TEXT NOT NULL DEFAULT ''"},
}

func OpenStore(ctx context.Context, path string) (*Store, error) {
	db, err := openSQLite(ctx, path, schema)
	if err != nil {
		return nil, err
	}
	for _, added := range addedColumns {
		var present int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`, added.table, added.column).Scan(&present); err != nil {
			return nil, errors.Join(err, db.Close())
		}
		if present == 0 {
			if _, err := db.ExecContext(ctx, `ALTER TABLE `+added.table+` ADD COLUMN `+added.column+` `+added.definition); err != nil {
				return nil, errors.Join(err, db.Close())
			}
		}
	}
	return &Store{db: db}, nil
}

// openSQLite opens one database file on a single pinned connection in WAL mode
// and applies its schema. The daemon store and the mock World both use it.
func openSQLite(ctx context.Context, path, ddl string) (*sql.DB, error) {
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
	for _, statement := range []string{"PRAGMA journal_mode=WAL", "PRAGMA synchronous=NORMAL", "PRAGMA busy_timeout=5000", ddl} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return nil, errors.Join(err, db.Close())
		}
	}
	return db, nil
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
	err = appendLedger(ctx, q, LedgerEntry{At: now, Kind: LedgerTrigger, Actor: triggerSource(trigger.Kind), TriggerID: id, Thread: trigger.Thread, Detail: string(trigger.Kind) + "：" + truncateRunes(trigger.Summary, 80)})
	return id, err
}

func triggerSource(kind TriggerKind) string {
	switch kind {
	case TriggerEmail, TriggerInvite:
		return "poller（MCP mail_poll）"
	case TriggerChat:
		return "me（信道）"
	case TriggerRoutine:
		return "scheduler"
	default:
		return "environment"
	}
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

const proposalColumns = `id,kind,trigger_id,thread,session_id,action_id,recipient,subject,body,event_id,response,reason,status,decided_by,created_at,decided_at,attempts,receipt,last_error`

func scanProposal(row interface{ Scan(...any) error }) (Proposal, error) {
	var p Proposal
	var kind, thread, response, status string
	var created int64
	var decided sql.NullInt64
	if err := row.Scan(&p.ID, &kind, &p.TriggerID, &thread, &p.SessionID, &p.ActionID, &p.To, &p.Subject, &p.Body, &p.EventID, &response, &p.Reason, &status, &p.DecidedBy, &created, &decided, &p.Attempts, &p.Receipt, &p.LastError); err != nil {
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

// DecideProposal is the human side of the approval gate and the only way out
// of pending. Approval only moves the proposal to approved; the outbox then
// executes it through the connector. A compare-and-set on pending means
// concurrent clicks release a proposal at most once.
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
		status, verb := ProposalDenied, "拒绝"
		if approve {
			status, verb = ProposalApproved, "批准"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE proposals SET status=?, decided_by=?, decided_at=? WHERE id=? AND status=?`, string(status), actor, millis(now), id, string(ProposalPending)); err != nil {
			return err
		}
		if err := appendLedger(ctx, tx, LedgerEntry{At: now, Kind: LedgerDecision, Actor: actor, TriggerID: p.TriggerID, Thread: p.Thread, SessionID: p.SessionID, ActionID: p.ActionID, Detail: fmt.Sprintf("%s提案 #%d", verb, p.ID)}); err != nil {
			return err
		}
		p.Status, p.DecidedBy, p.DecidedAt = status, actor, &now
		decided = p
		return nil
	})
	return decided, err
}

// IdempotencyKey is the connector key for one proposal's effect. Every retry,
// including one after a crash, reuses it.
func IdempotencyKey(id int64) string { return fmt.Sprintf("proposal-%d", id) }

// NextApproved returns the oldest approved proposal that is due. Only the
// single outbox loop calls it, so it needs no lease.
func (s *Store) NextApproved(ctx context.Context, now time.Time) (Proposal, bool, error) {
	p, err := scanProposal(s.db.QueryRowContext(ctx, `SELECT `+proposalColumns+` FROM proposals WHERE status=? AND not_before<=? ORDER BY id LIMIT 1`, string(ProposalApproved), millis(now)))
	if errors.Is(err, sql.ErrNoRows) {
		return Proposal{}, false, nil
	}
	return p, err == nil, err
}

// CompleteProposal records a confirmed external effect: approved → sent, the
// effect ledger row with the connector receipt, and one channel line.
func (s *Store) CompleteProposal(ctx context.Context, p Proposal, receipt EffectReceipt, actor string, now time.Time) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE proposals SET status=?, attempts=attempts+1, receipt=?, last_error='' WHERE id=? AND status=?`, string(ProposalSent), receipt.Receipt, p.ID, string(ProposalApproved))
		if err != nil {
			return err
		}
		if affected, err := result.RowsAffected(); err != nil || affected == 0 {
			return errors.Join(err, ErrProposalDecided)
		}
		detail := fmt.Sprintf("%s（回执 %s，幂等键 %s", receipt.Detail, receipt.Receipt, IdempotencyKey(p.ID))
		if receipt.Duplicate {
			detail += "，重放命中已执行的效果"
		}
		if err := appendLedger(ctx, tx, LedgerEntry{At: now, Kind: LedgerEffect, Actor: actor, TriggerID: p.TriggerID, Thread: p.Thread, SessionID: p.SessionID, ActionID: p.ActionID, Tool: effectTool(p.Kind), Risk: string(agent.RiskExternal), Detail: detail + "）"}); err != nil {
			return err
		}
		_, err = postChannel(ctx, tx, ChannelMessage{Role: RoleSystem, Text: fmt.Sprintf("已经 MCP 发出提案 #%d：%s → %s", p.ID, p.Subject, p.To), TriggerID: p.TriggerID}, now)
		return err
	})
}

// FailProposalAttempt records one failed execution. A retryable failure keeps
// the proposal approved until next; a final one marks it failed and tells the
// user. Nothing was sent, or the next retry's idempotency key absorbs it.
func (s *Store) FailProposalAttempt(ctx context.Context, p Proposal, cause error, final bool, next time.Time, actor string, now time.Time) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		status := ProposalApproved
		if final {
			status = ProposalFailed
		}
		message := truncateRunes(cause.Error(), 300)
		if _, err := tx.ExecContext(ctx, `UPDATE proposals SET status=?, attempts=attempts+1, not_before=?, last_error=? WHERE id=? AND status=?`, string(status), millis(next), message, p.ID, string(ProposalApproved)); err != nil {
			return err
		}
		detail := fmt.Sprintf("提案 #%d 第 %d 次执行失败：%s", p.ID, p.Attempts+1, message)
		if !final {
			detail += fmt.Sprintf("；%s 重试", next.Format("15:04:05"))
		}
		if err := appendLedger(ctx, tx, LedgerEntry{At: now, Kind: LedgerEffectErr, Actor: actor, TriggerID: p.TriggerID, Thread: p.Thread, SessionID: p.SessionID, ActionID: p.ActionID, Tool: effectTool(p.Kind), Risk: string(agent.RiskExternal), Detail: detail}); err != nil {
			return err
		}
		if !final {
			return nil
		}
		_, err := postChannel(ctx, tx, ChannelMessage{Role: RoleSystem, Text: fmt.Sprintf("提案 #%d 没能发出：%s", p.ID, truncateRunes(message, 120)), TriggerID: p.TriggerID}, now)
		return err
	})
}

func effectTool(kind ProposalKind) string {
	if kind == ProposalRespondInvite {
		return MCPCalendarRespond
	}
	return MCPMailSend
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

// ---- mail ingest ----

const mailCursorKey = "mail_cursor"

// MailCursor is the last mail_poll seq this daemon ingested; ok=false means
// the daemon has never polled and should start from the service's head.
func (s *Store) MailCursor(ctx context.Context) (int64, bool, error) {
	value, ok, err := s.metaValue(ctx, mailCursorKey)
	if err != nil || !ok {
		return 0, false, err
	}
	var cursor int64
	_, err = fmt.Sscan(value, &cursor)
	return cursor, err == nil, err
}

// StartMailCursor records where a first-time daemon starts watching.
func (s *Store) StartMailCursor(ctx context.Context, head int64, now time.Time) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if err := s.setMeta(ctx, tx, mailCursorKey, fmt.Sprint(head)); err != nil {
			return err
		}
		return appendLedger(ctx, tx, LedgerEntry{At: now, Kind: LedgerTrigger, Actor: "poller（MCP mail_poll）", Detail: fmt.Sprintf("首次连接：从收件箱 seq=%d 之后开始监听，之前的邮件不触发", head)})
	})
}

// IngestMail turns one polled page into triggers and advances the cursor in
// the same transaction. A re-polled message is skipped by its mail ID, so a
// crash between the poll and this commit cannot enqueue it twice.
func (s *Store) IngestMail(ctx context.Context, batch []InboundMail, cursor int64, now time.Time) (int, error) {
	ingested := 0
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		for _, inbound := range batch {
			mail := inbound.Mail
			result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO ingested(mail_id,seq,trigger_id,created_at) VALUES(?,?,0,?)`, mail.ID, inbound.Seq, millis(now))
			if err != nil {
				return err
			}
			if affected, err := result.RowsAffected(); err != nil || affected == 0 {
				if err != nil {
					return err
				}
				continue
			}
			kind := TriggerEmail
			if mail.Category == CategoryInvite {
				kind = TriggerInvite
			}
			id, err := enqueueTrigger(ctx, tx, Trigger{Kind: kind, Thread: mail.Thread, Ref: mail.ID, Summary: fmt.Sprintf("%s <%s>：%s", mail.FromName, mail.From, mail.Subject)}, now)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE ingested SET trigger_id=? WHERE mail_id=?`, id, mail.ID); err != nil {
				return err
			}
			ingested++
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value WHERE CAST(meta.value AS INTEGER) < CAST(excluded.value AS INTEGER)`, mailCursorKey, fmt.Sprint(cursor))
		return err
	})
	return ingested, err
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
