package userresources

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

type DraftID string
type DraftSessionID string
type DraftDigest string
type DraftCoverage string

const (
	AuthenticatedTurns     DraftCoverage = "authenticated_turns"
	AuthenticatedTurnsTail DraftCoverage = "authenticated_turns_tail"
	CurrentEpoch           DraftCoverage = "current_epoch"
	CurrentEpochTail       DraftCoverage = "current_epoch_tail"
)

type DraftCode string

const (
	DraftInvalidOwner             DraftCode = "invalid_owner"
	DraftInvalidSession           DraftCode = "invalid_session"
	DraftInvalidPreview           DraftCode = "invalid_preview"
	DraftCapacity                 DraftCode = "draft_capacity"
	DraftNotFound                 DraftCode = "draft_not_found"
	DraftExpired                  DraftCode = "draft_expired"
	DraftDigestMismatch           DraftCode = "draft_digest_mismatch"
	DraftCaptureSourceUnavailable DraftCode = "capture_source_unavailable"
)

// DraftError never includes owner bindings, model output or host faults.
type DraftError struct {
	code   DraftCode
	status int
}

func (e *DraftError) Error() string               { return strings.ReplaceAll(string(e.code), "_", " ") }
func (e *DraftError) Code() DraftCode             { return e.code }
func (e *DraftError) StatusCode() int             { return e.status }
func draftError(code DraftCode, status int) error { return &DraftError{code, status} }

type DraftStoreConfig struct {
	TTL           time.Duration
	MaxItems      int
	MaxPerOwner   *int
	MaxPerSession int
	Clock         func() time.Time
}

func DefaultDraftStoreConfig() DraftStoreConfig {
	return DraftStoreConfig{TTL: 15 * time.Minute, MaxItems: 64, MaxPerSession: 4}
}

// DraftPreview excludes private authority bindings and owns a detached evidence list.
type DraftPreview struct {
	SkillFields
	ID                       DraftID       `json:"draft_id"`
	EvidenceIndexes          []int         `json:"evidence_indexes"`
	Digest                   DraftDigest   `json:"digest"`
	CreatedAt                float64       `json:"created_at"`
	ExpiresAt                float64       `json:"expires_at"`
	Coverage                 DraftCoverage `json:"coverage"`
	Omitted                  int           `json:"omitted"`
	CompactedHistoryExcluded bool          `json:"compacted_history_excluded"`
}

type draftRecord struct {
	preview DraftPreview
	owner   OwnerID
	session DraftSessionID
	expires time.Time
}

// Draft is an immutable in-process identity handle. Only Preview is public data.
// Copying the handle preserves identity for cleanup after durable publication.
type Draft struct{ record *draftRecord }

func (d Draft) Preview() DraftPreview {
	if d.record == nil {
		return DraftPreview{}
	}
	p := d.record.preview
	p.EvidenceIndexes = append([]int{}, p.EvidenceIndexes...)
	return p
}

type DraftInput struct {
	Owner   OwnerID
	Session DraftSessionID
	SkillFields
	EvidenceIndexes          []int
	Coverage                 DraftCoverage
	Omitted                  int
	CompactedHistoryExcluded bool
}

type DraftQuery struct {
	ID      DraftID
	Owner   OwnerID
	Session DraftSessionID
	Digest  *DraftDigest
}

// DraftStore is bounded, process-local and FIFO. Access does not promote drafts.
// It is an operator library; it adds no route, model call or publication authority.
type DraftStore struct {
	mu                             sync.Mutex
	ttl                            time.Duration
	maxItems, maxOwner, maxSession int
	clock                          func() time.Time
	order                          []*draftRecord
	drafts                         map[DraftID]*draftRecord
}

func NewDraftStore(config DraftStoreConfig) (*DraftStore, error) {
	ownerLimit := min(16, config.MaxItems)
	if config.MaxPerOwner != nil {
		ownerLimit = *config.MaxPerOwner
	}
	if config.TTL <= 0 || config.MaxItems <= 0 || ownerLimit <= 0 || ownerLimit > config.MaxItems || config.MaxPerSession <= 0 || config.MaxPerSession > ownerLimit {
		return nil, draftError(DraftInvalidPreview, 422)
	}
	clock := config.Clock
	if clock == nil {
		clock = time.Now
	}
	return &DraftStore{ttl: config.TTL, maxItems: config.MaxItems, maxOwner: ownerLimit, maxSession: config.MaxPerSession, clock: clock, drafts: make(map[DraftID]*draftRecord)}, nil
}

func draftSeconds(t time.Time) float64 { return float64(t.Unix()) + float64(t.Nanosecond())/1e9 }

func newDraftID() (DraftID, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 15) | 64
	raw[8] = (raw[8] & 63) | 128
	return DraftID(hex.EncodeToString(raw[:])), nil
}

func (s *DraftStore) remove(id DraftID) {
	delete(s.drafts, id)
	for i, d := range s.order {
		if d.preview.ID == id {
			copy(s.order[i:], s.order[i+1:])
			s.order[len(s.order)-1] = nil
			s.order = s.order[:len(s.order)-1]
			return
		}
	}
}

func (s *DraftStore) purge(now time.Time) {
	for i := 0; i < len(s.order); {
		d := s.order[i]
		if !d.expires.After(now) {
			s.remove(d.preview.ID)
		} else {
			i++
		}
	}
}

func (s *DraftStore) count(owner OwnerID, session *DraftSessionID) int {
	count := 0
	for _, d := range s.order {
		if d.owner == owner && (session == nil || d.session == *session) {
			count++
		}
	}
	return count
}

func (s *DraftStore) evict(owner OwnerID, session *DraftSessionID) bool {
	for _, d := range s.order {
		if d.owner == owner && (session == nil || d.session == *session) {
			s.remove(d.preview.ID)
			return true
		}
	}
	return false
}

func (s *DraftStore) Add(input DraftInput) (Draft, error) {
	if input.Owner == "" {
		return Draft{}, draftError(DraftInvalidOwner, 500)
	}
	if input.Session == "" {
		return Draft{}, draftError(DraftInvalidSession, 500)
	}
	canonical, err := NewCanonicalSkill(input.SkillFields)
	if err != nil || input.Omitted < 0 {
		return Draft{}, draftError(DraftInvalidPreview, 422)
	}
	switch input.Coverage {
	case AuthenticatedTurns, AuthenticatedTurnsTail, CurrentEpoch, CurrentEpochTail:
	default:
		return Draft{}, draftError(DraftInvalidPreview, 422)
	}
	id, err := newDraftID()
	if err != nil {
		return Draft{}, draftError(DraftInvalidPreview, 500)
	}
	now := s.clock()
	record := &draftRecord{owner: input.Owner, session: input.Session, expires: now.Add(s.ttl), preview: DraftPreview{SkillFields: canonical.Fields(), ID: id, EvidenceIndexes: append([]int{}, input.EvidenceIndexes...), Digest: DraftDigest(canonical.Digest()), CreatedAt: draftSeconds(now), ExpiresAt: draftSeconds(now.Add(s.ttl)), Coverage: input.Coverage, Omitted: input.Omitted, CompactedHistoryExcluded: input.CompactedHistoryExcluded}}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purge(now)
	for s.count(input.Owner, &input.Session) >= s.maxSession {
		s.evict(input.Owner, &input.Session)
	}
	for s.count(input.Owner, nil) >= s.maxOwner {
		s.evict(input.Owner, nil)
	}
	for len(s.drafts) >= s.maxItems {
		if !s.evict(input.Owner, nil) {
			return Draft{}, draftError(DraftCapacity, 429)
		}
	}
	for s.drafts[record.preview.ID] != nil {
		id, err := newDraftID()
		if err != nil {
			return Draft{}, draftError(DraftInvalidPreview, 500)
		}
		record.preview.ID = id
	}
	s.drafts[record.preview.ID] = record
	s.order = append(s.order, record)
	return Draft{record}, nil
}

func (s *DraftStore) find(query DraftQuery, consume bool) (Draft, error) {
	now := s.clock()
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.drafts[query.ID]
	if d == nil {
		s.purge(now)
		return Draft{}, draftError(DraftNotFound, 404)
	}
	if d.owner != query.Owner || d.session != query.Session {
		return Draft{}, draftError(DraftNotFound, 404)
	}
	if !d.expires.After(now) {
		s.remove(query.ID)
		s.purge(now)
		return Draft{}, draftError(DraftExpired, 410)
	}
	if query.Digest != nil && subtle.ConstantTimeCompare([]byte(*query.Digest), []byte(d.preview.Digest)) != 1 {
		return Draft{}, draftError(DraftDigestMismatch, 409)
	}
	s.purge(now)
	if consume {
		s.remove(query.ID)
	}
	return Draft{d}, nil
}

func (s *DraftStore) Get(query DraftQuery) (Draft, error)  { return s.find(query, false) }
func (s *DraftStore) Peek(query DraftQuery) (Draft, error) { return s.Get(query) }
func (s *DraftStore) Consume(query DraftQuery, digest DraftDigest) (Draft, error) {
	query.Digest = &digest
	return s.find(query, true)
}

// DiscardCommitted cleans the exact identity after publication, even after TTL.
func (s *DraftStore) DiscardCommitted(draft Draft) bool {
	if draft.record == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.drafts[draft.record.preview.ID] != draft.record {
		return false
	}
	s.remove(draft.record.preview.ID)
	return true
}
