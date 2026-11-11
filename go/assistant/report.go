package assistant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
)

// Report limits: a notify message fits one channel message; a question is one
// concrete question; a quiet reason is one ledger line.
const (
	maxReportMessage  = maxChannelRunes
	maxReportQuestion = 200
	maxReportReason   = 200
)

type reportInput struct {
	Outcome  ReportOutcome `json:"outcome"`
	Message  string        `json:"message"`
	Question string        `json:"question"`
	Reason   string        `json:"reason"`
}

func (input reportInput) Validate() error {
	var required error
	switch input.Outcome {
	case ReportNotify:
		required = validText("message", input.Message, maxReportMessage, true)
	case ReportAsk:
		required = errors.Join(validText("question", input.Question, maxReportQuestion, true), validText("message", input.Message, maxReportMessage, false))
	case ReportQuiet:
		required = validText("reason", input.Reason, maxReportReason, true)
	default:
		return errors.New("outcome must be notify, ask or quiet")
	}
	return errors.Join(required, validText("reason", input.Reason, maxReportReason, false))
}

// reportSink keeps the report a session made during its current turn.
type reportSink interface {
	bindingSource
	report(agent.SessionID, Report) bool
}

// reportTool records the turn's structured result. It writes nothing outside
// the daemon: the router acts on the report after the turn, so a worker still
// never writes to the channel itself.
type reportTool struct{ sink reportSink }

func (tool reportTool) ExecuteExtensionTool(_ context.Context, authority agent.ToolAuthority, input reportInput) (string, error) {
	if _, ok := tool.sink.binding(authority.SessionID); !ok {
		return "", errors.New("no active trigger for this session")
	}
	report := Report{Outcome: input.Outcome, Message: strings.TrimSpace(input.Message), Question: strings.TrimSpace(input.Question), Reason: strings.TrimSpace(input.Reason)}
	replaced := tool.sink.report(authority.SessionID, report)
	text := map[ReportOutcome]string{ReportNotify: "回合结束后由 router 发到信道", ReportAsk: "回合结束后由 router 把问题交给用户，用户的回答会回到这个 thread", ReportQuiet: "不打扰用户，原因只写进账本"}[report.Outcome]
	if replaced {
		return "已更新本回合的汇报（" + string(report.Outcome) + "）：" + text + "。", nil
	}
	return "已记录本回合的汇报（" + string(report.Outcome) + "）：" + text + "。", nil
}

// QuestionRef names a question on the attention list and its answer trigger.
func QuestionRef(id int64) string { return fmt.Sprintf("question-%d", id) }

const questionColumns = `id,trigger_id,thread,session_id,text,status,answer,created_at,answered_at`

func scanQuestion(row interface{ Scan(...any) error }) (Question, error) {
	var q Question
	var thread, status string
	var created, answered int64
	if err := row.Scan(&q.ID, &q.TriggerID, &thread, &q.SessionID, &q.Text, &status, &q.Answer, &created, &answered); err != nil {
		return Question{}, err
	}
	q.Thread, q.Status, q.CreatedAt = ThreadID(thread), QuestionStatus(status), fromMillis(created)
	if answered > 0 {
		at := fromMillis(answered)
		q.AnsweredAt = &at
	}
	return q, nil
}

func (s *Store) Question(ctx context.Context, id int64) (Question, bool, error) {
	q, err := scanQuestion(s.db.QueryRowContext(ctx, `SELECT `+questionColumns+` FROM questions WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Question{}, false, nil
	}
	return q, err == nil, err
}

// AskQuestion records a worker's question with the turn's channel messages in
// one transaction. It is idempotent per lead trigger: a re-run after a crash
// returns the question it already asked and posts nothing new.
func (s *Store) AskQuestion(ctx context.Context, q Question, messages []ChannelMessage, now time.Time) (Question, bool, error) {
	var asked Question
	created := false
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		existing, err := scanQuestion(tx.QueryRowContext(ctx, `SELECT `+questionColumns+` FROM questions WHERE trigger_id=?`, q.TriggerID))
		if err == nil {
			asked = existing
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		inserted, err := tx.ExecContext(ctx, `INSERT INTO questions(trigger_id,thread,session_id,text,status,created_at) VALUES(?,?,?,?,?,?)`, q.TriggerID, string(q.Thread), q.SessionID, q.Text, string(QuestionOpen), millis(now))
		if err != nil {
			return err
		}
		if q.ID, err = inserted.LastInsertId(); err != nil {
			return err
		}
		for _, message := range messages {
			if _, err := postChannel(ctx, tx, message, now); err != nil {
				return err
			}
		}
		q.Status, q.CreatedAt, asked, created = QuestionOpen, now, q, true
		return appendLedger(ctx, tx, LedgerEntry{At: now, Kind: LedgerReport, Actor: "router", TriggerID: q.TriggerID, Thread: q.Thread, SessionID: q.SessionID, Detail: fmt.Sprintf("worker 提问 #%d：%s", q.ID, truncateRunes(q.Text, 120))})
	})
	return asked, created, err
}

// AnswerQuestion records the user's answer: open → answered, the answer as
// the user's channel line, and a user.answer trigger on the question's
// thread so the worker that asked continues there. The answer turn runs like
// any other event on that thread, untrusted, because the thread's transcript
// carries external content.
func (s *Store) AnswerQuestion(ctx context.Context, id int64, answer, actor string, now time.Time) (Question, int64, error) {
	var answered Question
	var trigger int64
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		answered, trigger, _, err = answerQuestion(ctx, tx, id, answer, actor, "", now)
		return err
	})
	return answered, trigger, err
}

// answerQuestion is AnswerQuestion inside the caller's transaction; source
// tags the answer's channel line with the remote channel it came from. It
// also returns that channel line.
func answerQuestion(ctx context.Context, tx *sql.Tx, id int64, answer, actor, source string, now time.Time) (Question, int64, int64, error) {
	q, err := scanQuestion(tx.QueryRowContext(ctx, `SELECT `+questionColumns+` FROM questions WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Question{}, 0, 0, ErrUnknownRef
	}
	if err != nil {
		return Question{}, 0, 0, err
	}
	if q.Status != QuestionOpen {
		return Question{}, 0, 0, fmt.Errorf("question #%d is %s: %w", id, q.Status, ErrNotActionable)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE questions SET status=?, answer=?, answered_at=? WHERE id=? AND status=?`, string(QuestionAnswered), answer, millis(now), id, string(QuestionOpen)); err != nil {
		return Question{}, 0, 0, err
	}
	if err := undismiss(ctx, tx, QuestionRef(id)); err != nil {
		return Question{}, 0, 0, err
	}
	message, err := postChannel(ctx, tx, ChannelMessage{Role: RoleUser, Text: fmt.Sprintf("回答提问 #%d：%s", id, answer), TriggerID: q.TriggerID, Source: source}, now)
	if err != nil {
		return Question{}, 0, 0, err
	}
	trigger, err := enqueueTrigger(ctx, tx, Trigger{Kind: TriggerAnswer, Thread: q.Thread, Ref: QuestionRef(id), Summary: truncateRunes(answer, MaxChatRunes)}, now)
	if err != nil {
		return Question{}, 0, 0, err
	}
	q.Status, q.Answer, q.AnsweredAt = QuestionAnswered, answer, &now
	err = appendLedger(ctx, tx, LedgerEntry{At: now, Kind: LedgerControl, Actor: actor, TriggerID: trigger, Thread: q.Thread, SessionID: q.SessionID, Detail: fmt.Sprintf("回答提问 #%d，交回 thread %s 的 worker", id, q.Thread)})
	return q, trigger, message, err
}

// questionID parses a QuestionRef.
func questionID(ref string) (int64, bool) {
	kind, id, ok := splitRef(ref)
	return id, ok && kind == "question"
}
