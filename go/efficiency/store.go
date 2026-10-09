package efficiency

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// ArtifactFault identifies the Source exception hierarchy without untyped payloads.
type ArtifactFault string

const (
	ArtifactStoreFault       ArtifactFault = "RawArtifactStoreError"
	ArtifactNotFound         ArtifactFault = "RawArtifactNotFound"
	ArtifactExpired          ArtifactFault = "RawArtifactExpired"
	ArtifactTooLarge         ArtifactFault = "RawArtifactTooLarge"
	ArtifactCapacityExceeded ArtifactFault = "RawArtifactCapacityExceeded"
)

type ArtifactError struct {
	Kind    ArtifactFault
	Message string
}

func (e *ArtifactError) Error() string { return e.Message }
func (e *ArtifactError) Is(target error) bool {
	t, ok := target.(*ArtifactError)
	return ok && (e.Kind == t.Kind || t.Kind == ArtifactStoreFault || t.Kind == ArtifactNotFound && e.Kind == ArtifactExpired)
}

var (
	ErrArtifactStore    = &ArtifactError{Kind: ArtifactStoreFault}
	ErrArtifactNotFound = &ArtifactError{Kind: ArtifactNotFound}
	ErrArtifactExpired  = &ArtifactError{Kind: ArtifactExpired}
	ErrArtifactTooLarge = &ArtifactError{Kind: ArtifactTooLarge}
	ErrArtifactCapacity = &ArtifactError{Kind: ArtifactCapacityExceeded}
)

func artifactError(kind ArtifactFault, message string) error { return &ArtifactError{kind, message} }

// ArtifactPointer owns immutable metadata; it carries no raw content or path.
type ArtifactPointer struct {
	config ArtifactPointerConfig
	valid  bool
}
type ArtifactPointerConfig struct {
	Ref       string   `json:"ref"`
	Digest    string   `json:"digest"`
	SizeBytes *big.Int `json:"size_bytes"`
	ExpiresAt float64  `json:"expires_at"`
}

func NewArtifactPointer(config ArtifactPointerConfig) (ArtifactPointer, error) {
	if !rawReference.MatchString(config.Ref) {
		return ArtifactPointer{}, errors.New("artifact pointer ref is invalid")
	}
	if !sha256Digest.MatchString(config.Digest) {
		return ArtifactPointer{}, errors.New("artifact pointer digest must be sha256")
	}
	if config.SizeBytes == nil {
		return ArtifactPointer{}, errors.New("artifact pointer size must be an integer")
	}
	if config.SizeBytes.Sign() < 0 {
		return ArtifactPointer{}, errors.New("artifact pointer size cannot be negative")
	}
	if math.IsNaN(config.ExpiresAt) || math.IsInf(config.ExpiresAt, 0) {
		return ArtifactPointer{}, errors.New("artifact pointer expiry must be finite")
	}
	config.SizeBytes = copyInteger(config.SizeBytes)
	return ArtifactPointer{config, true}, nil
}
func (p ArtifactPointer) Snapshot() (ArtifactPointerConfig, error) {
	if !p.valid {
		return ArtifactPointerConfig{}, errors.New("artifact pointer is not initialized")
	}
	c := p.config
	c.SizeBytes = copyInteger(c.SizeBytes)
	return c, nil
}
func (p ArtifactPointer) MarshalJSON() ([]byte, error) {
	c, err := p.Snapshot()
	if err != nil {
		return nil, err
	}
	return json.Marshal(c)
}

// ArtifactClock is a trusted seconds clock. DefaultRawStoreConfig uses monotonic time.
type ArtifactClock func() (float64, error)
type RawStoreConfig struct {
	Workspace        string        `json:"-"`
	Directory        string        `json:"directory"`
	TTLSeconds       float64       `json:"ttl_seconds"`
	MaxArtifactBytes *big.Int      `json:"max_artifact_bytes"`
	MaxTotalBytes    *big.Int      `json:"max_total_bytes"`
	Clock            ArtifactClock `json:"-"`
}

func DefaultRawStoreConfig(workspace string) RawStoreConfig {
	origin := time.Now()
	limits := DefaultArtifactLimits()
	return RawStoreConfig{Workspace: workspace, Directory: ".token-efficiency/raw", TTLSeconds: limits.TTLSeconds,
		MaxArtifactBytes: limits.MaxArtifactBytes, MaxTotalBytes: limits.MaxTotalBytes,
		Clock: func() (float64, error) { return time.Since(origin).Seconds(), nil }}
}

type artifactRecord struct {
	payload string
	digest  string
	size    *big.Int
	expiry  float64
}

// RawStore stores already-masked text in RAM. PutMasked is a caller assertion of
// redaction, not a masking implementation. Workspace/directory are provenance only.
// Each store owns its references; nothing is written to the workspace.
type RawStore struct {
	mu                sync.Mutex
	workspace         string
	ttl               float64
	maxItem, maxTotal *big.Int
	clock             ArtifactClock
	entropy           io.Reader
	records           map[string]artifactRecord
	closed            bool
}

func NewRawStore(c RawStoreConfig) (*RawStore, error) {
	if err := (ArtifactLimits{TTLSeconds: c.TTLSeconds, MaxArtifactBytes: c.MaxArtifactBytes, MaxTotalBytes: c.MaxTotalBytes}).Validate(); err != nil {
		return nil, err
	}
	if c.Clock == nil {
		return nil, errors.New("clock must be callable")
	}
	if filepath.IsAbs(c.Directory) || filepath.Clean(c.Directory) == "." {
		return nil, errors.New("artifact directory must be a contained relative path")
	}
	for _, part := range strings.Split(c.Directory, "/") {
		if part == ".." {
			return nil, errors.New("artifact directory must be a contained relative path")
		}
	}
	path := c.Workspace
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	path, err := filepath.Abs(path)
	if err == nil {
		path, err = filepath.EvalSymlinks(path)
	}
	if err == nil {
		var info os.FileInfo
		info, err = os.Stat(path)
		if err == nil && !info.IsDir() {
			err = errors.New("not a directory")
		}
	}
	if err != nil {
		return nil, errors.New("workspace must be an existing directory")
	}
	return &RawStore{workspace: path, ttl: c.TTLSeconds, maxItem: copyInteger(c.MaxArtifactBytes), maxTotal: copyInteger(c.MaxTotalBytes), clock: c.Clock, entropy: rand.Reader, records: make(map[string]artifactRecord)}, nil
}
func (s *RawStore) Workspace() string { return s.workspace }
func (s *RawStore) Closed() bool      { s.mu.Lock(); defer s.mu.Unlock(); return s.closed }
func (s *RawStore) ensureOpen() error {
	if s.closed || s.records == nil {
		return artifactError(ArtifactStoreFault, "raw artifact store is closed")
	}
	return nil
}
func validateArtifactRef(ref string) error {
	if !rawReference.MatchString(ref) {
		return artifactError(ArtifactNotFound, "invalid raw artifact reference")
	}
	return nil
}
func (s *RawStore) now() (float64, error) {
	if s.clock == nil {
		return 0, artifactError(ArtifactStoreFault, "raw artifact clock failed")
	}
	n, err := s.clock()
	if err != nil {
		return 0, artifactError(ArtifactStoreFault, "raw artifact clock failed")
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, artifactError(ArtifactStoreFault, "raw artifact clock must be finite")
	}
	return n, nil
}
func (s *RawStore) expire(now float64) int {
	n := 0
	for ref, record := range s.records {
		if now >= record.expiry {
			delete(s.records, ref)
			n++
		}
	}
	return n
}
func (s *RawStore) Sweep() (int, error) {
	now, err := s.now()
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.ensureOpen(); err != nil {
		return 0, err
	}
	return s.expire(now), nil
}
func (s *RawStore) Clear() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.records)
	clear(s.records)
	return n
}
func (s *RawStore) Close() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.records)
	clear(s.records)
	s.closed = true
	return n
}
func (s *RawStore) Discard(ref string) (bool, error) {
	if err := validateArtifactRef(ref); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureOpen(); err != nil {
		return false, err
	}
	_, ok := s.records[ref]
	delete(s.records, ref)
	return ok, nil
}
func artifactDigest(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return "sha256:" + hex.EncodeToString(sum[:])
}
func (s *RawStore) PutMasked(content string) (ArtifactPointer, error) {
	if !utf8.ValidString(content) {
		return ArtifactPointer{}, errors.New("masked_content must be valid UTF-8")
	}
	size := new(big.Int).SetUint64(uint64(len(content)))
	if s.maxItem != nil && size.Cmp(s.maxItem) > 0 {
		return ArtifactPointer{}, artifactError(ArtifactTooLarge, fmt.Sprintf("masked artifact is %s bytes; limit is %s", size, s.maxItem))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureOpen(); err != nil {
		return ArtifactPointer{}, err
	}
	now, err := s.now()
	if err != nil {
		return ArtifactPointer{}, err
	}
	s.expire(now)
	total := new(big.Int).Set(size)
	for _, r := range s.records {
		total.Add(total, r.size)
	}
	if total.Cmp(s.maxTotal) > 0 {
		return ArtifactPointer{}, artifactError(ArtifactCapacityExceeded, "masked artifact store is at capacity")
	}
	for i := 0; i < 8; i++ {
		var bits [32]byte
		if _, err := io.ReadFull(s.entropy, bits[:]); err != nil {
			return ArtifactPointer{}, artifactError(ArtifactStoreFault, "raw artifact entropy failed")
		}
		ref := "raw_" + base64.RawURLEncoding.EncodeToString(bits[:])
		if _, exists := s.records[ref]; exists {
			continue
		}
		digest := artifactDigest(content)
		pointer, err := NewArtifactPointer(ArtifactPointerConfig{ref, digest, size, now + s.ttl})
		if err != nil {
			return ArtifactPointer{}, err
		}
		s.records[ref] = artifactRecord{content, digest, copyInteger(size), now + s.ttl}
		return pointer, nil
	}
	return ArtifactPointer{}, artifactError(ArtifactStoreFault, "could not allocate an unpredictable artifact reference")
}
func (s *RawStore) GetMasked(ref string) (string, error) {
	if err := validateArtifactRef(ref); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureOpen(); err != nil {
		return "", err
	}
	r, ok := s.records[ref]
	if !ok {
		return "", artifactError(ArtifactNotFound, "raw artifact does not belong to this session")
	}
	now, err := s.now()
	if err != nil {
		return "", err
	}
	if now >= r.expiry {
		delete(s.records, ref)
		return "", artifactError(ArtifactExpired, "raw artifact has expired")
	}
	size := new(big.Int).SetUint64(uint64(len(r.payload)))
	if size.Cmp(s.maxItem) > 0 {
		return "", artifactError(ArtifactTooLarge, "raw artifact exceeds this store's read limit")
	}
	if size.Cmp(r.size) != 0 || subtle.ConstantTimeCompare([]byte(artifactDigest(r.payload)), []byte(r.digest)) != 1 {
		return "", artifactError(ArtifactNotFound, "raw artifact failed its content integrity check")
	}
	if !utf8.ValidString(r.payload) {
		return "", errors.New("raw artifact payload must be valid UTF-8")
	}
	return r.payload, nil
}
