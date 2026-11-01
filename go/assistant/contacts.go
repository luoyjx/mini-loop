package assistant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Contact is someone the user deals with. Admission grading reads it: mail
// from a known contact addressed to the user is an explicit request. Seeded
// contacts are the demo's colleagues; a recipient the outbox sends to becomes
// known.
type Contact struct {
	Address      string     `json:"address"`
	Name         string     `json:"name"`
	Relation     string     `json:"relation"`
	Source       string     `json:"source"`
	Interactions int        `json:"interactions"`
	LastAt       *time.Time `json:"last_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

const contactsSeedKey = "contacts_seeded"

// seedContacts are the demo's known people. They are seeded under their own
// meta key, so a data directory from an earlier demo gains them too.
var seedContacts = []Contact{
	{Address: ManagerAddress, Name: "Alice", Relation: "经理"},
	{Address: ColleagueAddress, Name: "Dave", Relation: "同事"},
	{Address: "bob@friends.example", Name: "Bob", Relation: "朋友"},
	{Address: VendorAddress, Name: "云主机账单", Relation: "供应商"},
}

func normalAddress(address string) string { return strings.ToLower(strings.TrimSpace(address)) }

// addressed reports whether a comma-separated address list names address.
func addressed(list, address string) bool {
	want := normalAddress(address)
	for _, part := range strings.Split(list, ",") {
		if normalAddress(part) == want {
			return true
		}
	}
	return false
}

func (s *Store) seedContacts(ctx context.Context, now time.Time) error {
	if _, done, err := s.metaValue(ctx, contactsSeedKey); err != nil || done {
		return err
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		for _, contact := range seedContacts {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO contacts(address,name,relation,source,created_at) VALUES(?,?,?,?,?)`, normalAddress(contact.Address), contact.Name, contact.Relation, "seed", millis(now)); err != nil {
				return err
			}
		}
		return s.setMeta(ctx, tx, contactsSeedKey, now.Format(time.RFC3339))
	})
}

// noteContact records one interaction with address: a new address becomes a
// contact from source, a known one counts another interaction.
func noteContact(ctx context.Context, q queryer, address, name, source string, now time.Time) error {
	address = normalAddress(address)
	if !strings.Contains(address, "@") {
		return nil
	}
	_, err := q.ExecContext(ctx, `INSERT INTO contacts(address,name,relation,source,interactions,last_at,created_at) VALUES(?,?,'',?,1,?,?)
		ON CONFLICT(address) DO UPDATE SET interactions=interactions+1, last_at=excluded.last_at, name=CASE WHEN contacts.name='' THEN excluded.name ELSE contacts.name END`,
		address, name, source, millis(now), millis(now))
	return err
}

// KnownContact reports whether address is a contact.
func (s *Store) KnownContact(ctx context.Context, address string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM contacts WHERE address=?`, normalAddress(address)).Scan(&count)
	return count > 0, err
}

// Contacts lists contacts, most interactions first.
func (s *Store) Contacts(ctx context.Context, limit int) ([]Contact, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT address,name,relation,source,interactions,last_at,created_at FROM contacts ORDER BY interactions DESC, relation<>'' DESC, address LIMIT ?`, limit)
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
		c.CreatedAt = fromMillis(created)
		if last > 0 {
			at := fromMillis(last)
			c.LastAt = &at
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// CandidateMode is the persisted mode for inferred candidates; observe until
// the user switches it.
func (s *Store) CandidateMode(ctx context.Context) (CandidateMode, error) {
	value, ok, err := s.metaValue(ctx, candidateModeKey)
	if err != nil || !ok {
		return CandidateObserve, err
	}
	return CandidateMode(value), nil
}

var ErrBadMode = errors.New("mode must be observe or auto")

// SetCandidateMode switches the candidate mode with a ledger row and a channel
// line in one transaction.
func (s *Store) SetCandidateMode(ctx context.Context, mode CandidateMode, actor string, now time.Time) error {
	if mode != CandidateObserve && mode != CandidateAuto {
		return ErrBadMode
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if err := s.setMeta(ctx, tx, candidateModeKey, string(mode)); err != nil {
			return err
		}
		text := "推断候选改为观察模式：分诊建议处理的邮件只记录，在「候选」里由你决定。"
		if mode == CandidateAuto {
			text = "推断候选改为自动模式：分诊建议处理的邮件直接交给 worker。"
		}
		if err := appendLedger(ctx, tx, LedgerEntry{At: now, Kind: LedgerControl, Actor: actor, Detail: fmt.Sprintf("推断候选模式：%s", mode)}); err != nil {
			return err
		}
		_, err := postChannel(ctx, tx, ChannelMessage{Role: RoleSystem, Text: text}, now)
		return err
	})
}
