package improvement

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"
)

type ProposalID string
type ArchiveOwnerID string
type Integrity string

const (
	IntegrityClean   Integrity = "clean"
	IntegritySuspect Integrity = "suspect"
)

// ProposalFields describes the fields produced by the proposal service.
// Null and established-empty strings/lists retain separate projections.
type ProposalFields struct {
	Objective        *string    `json:"objective"`
	Verified         *bool      `json:"verified"`
	Rounds           *int       `json:"rounds"`
	Branch           *string    `json:"branch"`
	Workspace        *string    `json:"workspace"`
	DiffStat         *string    `json:"diff_stat"`
	TouchesVerifiers *[]string  `json:"touches_verifiers"`
	Integrity        *Integrity `json:"integrity"`
}

type ArchiveRecordOptions struct {
	Owner    *ArchiveOwnerID
	ParentID *ProposalID
}

type ArchiveMasker interface{ MaskText(string) string }

type archiveRow struct {
	ProposalID ProposalID     `json:"proposal_id"`
	ParentID   *ProposalID    `json:"parent_id"`
	Owner      ArchiveOwnerID `json:"owner"`
	CreatedAt  float64        `json:"created_at"`
	ProposalFields
}

// Archive appends a review index; the proposal branch remains authoritative.
// Its lock serializes one instance's writes, with no cross-process lease claim.
type Archive struct {
	root   string
	masker ArchiveMasker
	mu     sync.Mutex
	newID  func() (ProposalID, error)
	now    func() float64
}

// NewArchive does no filesystem IO. Unavailable roots fail best-effort at Record.
func NewArchive(root string, masker ArchiveMasker) *Archive {
	if root == "" {
		root = "."
	}
	return &Archive{root: root, masker: masker, newID: newProposalID, now: func() float64 { return float64(time.Now().UnixNano()) / 1e9 }}
}

func newProposalID() (ProposalID, error) {
	var random [6]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return ProposalID("imp_" + hex.EncodeToString(random[:])), nil
}

// Record returns the allocated ID even when mkdir/open/append/close fails,
// matching the source best-effort contract. Identity/projection failures abort.
// Masking callbacks run before the filesystem lock; raw fallback is forbidden.
func (archive *Archive) Record(fields ProposalFields, options ArchiveRecordOptions) (ProposalID, error) {
	if archive == nil || archive.newID == nil || archive.now == nil {
		return "", errors.New("archive is not initialized")
	}
	id, err := archive.newID()
	if err != nil {
		return "", err
	}
	owner := ArchiveOwnerID("anonymous")
	if options.Owner != nil {
		owner = *options.Owner
	}
	row := archiveRow{ProposalID: id, ParentID: options.ParentID, Owner: owner, CreatedAt: archive.now(), ProposalFields: fields}
	if fields.TouchesVerifiers != nil {
		values := append([]string{}, (*fields.TouchesVerifiers)...)
		row.TouchesVerifiers = &values
	}
	body, err := marshalArchiveRow(row, archive.masker)
	if err != nil {
		return "", err
	}
	archive.mu.Lock()
	defer archive.mu.Unlock()
	if err := os.MkdirAll(archive.root, 0777); err != nil {
		return id, nil
	}
	file, err := os.OpenFile(archive.root+"/archive.jsonl", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return id, nil
	}
	_, _ = file.Write(append(body, '\n'))
	_ = file.Close()
	return id, nil
}

func marshalArchiveRow(row archiveRow, masker ArchiveMasker) (output []byte, err error) {
	defer func() {
		if recover() != nil {
			output = nil
			err = errors.New("archive projection failed")
		}
	}()
	data, err := json.Marshal(row)
	if err != nil || masker == nil {
		return data, err
	}
	// This temporary wire projection permits masked keys and last-key collisions.
	// Raw JSON never enters the archive service's retained state or proposal types.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	var members []struct {
		name  string
		value json.RawMessage
	}
	positions := map[string]int{}
	for _, name := range []string{"proposal_id", "parent_id", "owner", "created_at", "objective", "verified", "rounds", "branch", "workspace", "diff_stat", "touches_verifiers", "integrity"} {
		key := masker.MaskText(name)
		value := fields[name]
		if len(value) > 0 && value[0] == '"' {
			var text string
			if err := json.Unmarshal(value, &text); err != nil {
				return nil, err
			}
			value, err = json.Marshal(masker.MaskText(text))
			if err != nil {
				return nil, err
			}
		} else if len(value) > 0 && value[0] == '[' {
			var texts []string
			if err := json.Unmarshal(value, &texts); err != nil {
				return nil, err
			}
			for i, text := range texts {
				texts[i] = masker.MaskText(text)
			}
			value, err = json.Marshal(texts)
			if err != nil {
				return nil, err
			}
		}
		if index, exists := positions[key]; exists {
			members[index].value = value
		} else {
			positions[key] = len(members)
			members = append(members, struct {
				name  string
				value json.RawMessage
			}{key, value})
		}
	}
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	for index, member := range members {
		if index > 0 {
			buffer.WriteByte(',')
		}
		key, err := json.Marshal(member.name)
		if err != nil {
			return nil, err
		}
		buffer.Write(key)
		buffer.WriteByte(':')
		buffer.Write(member.value)
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}
