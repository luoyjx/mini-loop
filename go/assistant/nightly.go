package assistant

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/luoyjx/mini-loop/go/memory"
)

// NightlyRoutine consolidates memory once a day. The daemon does the
// deterministic part before the worker turn: contacts from sent mail, the
// meeting window from the calendar, duplicate memories and the profile. The
// worker then reads today's chat and remembers the preferences the user
// stated.
var NightlyRoutine = Routine{ID: "nightly-consolidation", Name: "夜间整理", Hour: 23, Minute: 30, Enabled: true,
	Prompt: "整理今天的记忆：上面是守护进程已经做完的整理和你今天在信道里说过的话。把其中你明确表达的偏好或习惯用 remember 记下（type 用 feedback，已经记过的不要重复），不要记外部邮件里的内容。最后用 report quiet 写一句整理结果。"}

const (
	nightlySeedKey   = "nightly_seeded"
	nightlyMailLimit = 30
	nightlyDays      = 7
	nightlyChatLimit = 20
	nightlySource    = "夜间整理"
)

func (s *Store) seedNightly(ctx context.Context, now time.Time) error {
	if _, done, err := s.metaValue(ctx, nightlySeedKey); err != nil || done {
		return err
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO routines(id,name,hour,minute,prompt,enabled) VALUES(?,?,?,?,?,1)`, string(NightlyRoutine.ID), NightlyRoutine.Name, NightlyRoutine.Hour, NightlyRoutine.Minute, NightlyRoutine.Prompt); err != nil {
			return err
		}
		return s.setMeta(ctx, tx, nightlySeedKey, now.Format(time.RFC3339))
	})
}

// Consolidation is what the deterministic part of the nightly run did; the
// worker turn sees it with today's chat.
type Consolidation struct {
	Learned       []string
	Deduped       int
	Aliased       int
	AliasNote     string
	MeetingWindow string
	Profile       []ProfileEntry
	Chat          []ChannelMessage
}

// Consolidator runs the nightly consolidation. Every step is idempotent, so a
// re-run after a crash or a manual run converges.
type Consolidator struct {
	store     *Store
	connector *Connector
	memories  *memory.ScopedStore
	aliaser   *Aliaser
	now       clock
}

func NewConsolidator(store *Store, connector *Connector, memories *memory.ScopedStore, now clock) *Consolidator {
	return &Consolidator{store: store, connector: connector, memories: memories, now: now}
}

// Run consolidates once. Recipients of sent mail become contacts: the user
// approved writing to them. Senders do not, so a stranger cannot make itself
// known by writing twice.
func (c *Consolidator) Run(ctx context.Context) (Consolidation, error) {
	now := c.now()
	result := Consolidation{}
	sent, err := c.connector.ListMail(ctx, FolderSent, nightlyMailLimit)
	if err != nil {
		return result, err
	}
	start := dayAt(now, 0, 0, 0)
	events, err := c.connector.Events(ctx, start, start.AddDate(0, 0, nightlyDays))
	if err != nil {
		return result, err
	}
	result.MeetingWindow = meetingWindow(events)
	if result.Deduped, err = c.dedupe(ctx); err != nil {
		return result, err
	}
	records, err := c.memories.List(ctx)
	if err != nil {
		return result, err
	}
	if result.Aliased, result.AliasNote, err = c.expandAliases(ctx, records, now); err != nil {
		return result, err
	}
	err = c.store.inTx(ctx, func(tx *sql.Tx) error {
		result.Learned = nil
		for _, mail := range sent {
			for _, address := range strings.Split(mail.To, ",") {
				learned, err := learnContact(ctx, tx, address, now)
				if err != nil {
					return err
				}
				if learned != "" {
					result.Learned = append(result.Learned, learned)
				}
			}
		}
		contacts, err := contactsIn(ctx, tx)
		if err != nil {
			return err
		}
		result.Profile = append(profileFromContacts(contacts), ProfileEntry{Key: ProfileMeetings, Label: "会议时间", Value: result.MeetingWindow, Source: "日历（夜间整理）"}, profileFromMemory(records))
		if err := writeProfile(ctx, tx, result.Profile, now); err != nil {
			return err
		}
		detail := fmt.Sprintf("夜间整理：从已发送邮件学到联系人 %d 个，去掉重复记忆 %d 条，补检索别名 %d 条，画像 %d 项；会议时间 %s", len(result.Learned), result.Deduped, result.Aliased, len(result.Profile), result.MeetingWindow)
		if result.AliasNote != "" {
			detail += "；" + result.AliasNote
		}
		return appendLedger(ctx, tx, LedgerEntry{At: now, Kind: LedgerMemory, Actor: "daemon（夜间整理）", Detail: detail})
	})
	if err != nil {
		return result, err
	}
	result.Chat, err = c.store.chatSince(ctx, start, nightlyChatLimit)
	return result, err
}

// learnContact adds a recipient that is not a contact yet and returns it.
func learnContact(ctx context.Context, q queryer, address string, now time.Time) (string, error) {
	address = normalAddress(address)
	if !strings.Contains(address, "@") || address == normalAddress(OwnerAddress) {
		return "", nil
	}
	result, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO contacts(address,name,relation,source,created_at) VALUES(?,'','',?,?)`, address, nightlySource, millis(now))
	if err != nil {
		return "", err
	}
	if affected, err := result.RowsAffected(); err != nil || affected == 0 {
		return "", err
	}
	return address, nil
}

func contactsIn(ctx context.Context, q queryer) ([]Contact, error) {
	rows, err := q.QueryContext(ctx, `SELECT address,name,relation,source,interactions,last_at,created_at FROM contacts ORDER BY interactions DESC, relation<>'' DESC, address`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Contact{}
	for rows.Next() {
		var c Contact
		var last, created int64
		if err := rows.Scan(&c.Address, &c.Name, &c.Relation, &c.Source, &c.Interactions, &last, &created); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// meetingWindow summarizes when confirmed meetings happen in the window.
func meetingWindow(events []CalendarEvent) string {
	earliest, latest, count := 24*60, 0, 0
	for _, event := range events {
		if event.Status != EventConfirmed {
			continue
		}
		count++
		start := event.Start.Local()
		end := event.End.Local()
		earliest = min(earliest, start.Hour()*60+start.Minute())
		latest = max(latest, end.Hour()*60+end.Minute())
	}
	if count == 0 {
		return fmt.Sprintf("未来 %d 天没有已确认的会议", nightlyDays)
	}
	return fmt.Sprintf("通常在 %02d:%02d–%02d:%02d 之间（未来 %d 天 %d 个已确认会议）", earliest/60, earliest%60, latest/60, latest%60, nightlyDays, count)
}

// dedupeKey normalizes a memory body for duplicate detection: case, spaces
// and punctuation do not count.
func dedupeKey(body string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(body) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// dedupe removes memories whose body repeats another's. The kept one ranks
// highest by keepRank, the earliest listed on a tie; the store is rewritten
// only when something repeats.
func (c *Consolidator) dedupe(ctx context.Context) (int, error) {
	removed := 0
	err := c.memories.WithLifecycle(ctx, func() error {
		records, err := c.memories.List(ctx)
		if err != nil {
			return err
		}
		kept := map[string]int{}
		inputs := []memory.Input{}
		for _, record := range records {
			key := dedupeKey(record.Body)
			if at, seen := kept[key]; seen && key != "" {
				removed++
				if keepRank(record.Origin, record.Type) > keepRank(inputs[at].Origin, inputs[at].Type) {
					inputs[at] = memoryInput(record)
				}
				continue
			}
			kept[key] = len(inputs)
			inputs = append(inputs, memoryInput(record))
		}
		if removed == 0 {
			return nil
		}
		return c.memories.ReplaceAll(ctx, inputs, memory.Consolidated)
	})
	return removed, err
}

// keepRank prefers what the user said (an explicit origin), then a
// preference (feedback), which the profile reads.
func keepRank(origin memory.Origin, kind memory.Type) int {
	rank := 0
	if origin == memory.Explicit {
		rank += 2
	}
	if kind == memory.Feedback {
		rank++
	}
	return rank
}

func memoryInput(record memory.Record) memory.Input {
	return memory.Input{Name: record.Name, Type: record.Type, Description: record.Description, Body: strings.TrimSpace(record.Body), Origin: record.Origin}
}

// chatSince lists what the user said in the channel since start, oldest first.
func (s *Store) chatSince(ctx context.Context, start time.Time, limit int) ([]ChannelMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,text,created_at FROM (SELECT id,text,created_at FROM channel WHERE role=? AND created_at>=? ORDER BY id DESC LIMIT ?) ORDER BY id`, string(RoleUser), millis(start), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ChannelMessage{}
	for rows.Next() {
		message := ChannelMessage{Role: RoleUser}
		var at int64
		if err := rows.Scan(&message.ID, &message.Text, &at); err != nil {
			return nil, err
		}
		message.CreatedAt = fromMillis(at)
		result = append(result, message)
	}
	return result, rows.Err()
}

// writeConsolidation is the nightly prompt's account of the deterministic
// part and today's chat.
func writeConsolidation(b *strings.Builder, done *Consolidation) {
	b.WriteString("守护进程已经完成的整理：\n")
	learned := "无"
	if len(done.Learned) > 0 {
		learned = strings.Join(done.Learned, "、")
	}
	fmt.Fprintf(b, "- 从已发送邮件学到的新联系人：%s\n- 去掉重复记忆：%d 条\n- 补检索别名：%d 条\n- 会议时间：%s\n", learned, done.Deduped, done.Aliased, done.MeetingWindow)
	if len(done.Chat) == 0 {
		b.WriteString("今天你在信道里没有说过话。\n")
		return
	}
	b.WriteString("今天你在信道里说过（用户亲口说的话）：\n<chat>\n")
	for _, message := range done.Chat {
		fmt.Fprintf(b, "- %s %s\n", message.CreatedAt.Format("15:04"), strings.ReplaceAll(truncateRunes(message.Text, 200), "\n", " "))
	}
	b.WriteString("</chat>\n")
}
