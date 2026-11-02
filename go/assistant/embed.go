package assistant

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/luoyjx/mini-loop/go/memory"
)

// Local embeddings are the fourth retrieval leg. An Embedder turns text into
// vectors; VectorIndex keeps one normalized vector per memory, in memory and
// in assistant.db, keyed by memory name, a hash of its text and the model.
// New or changed memories are embedded just before a dense search, so a
// memory written a moment ago is already searchable. Any embedder failure
// skips the leg for that search: the lexical legs still answer, and the
// status, the page and the health board say why.

const (
	EmbedOff          = "off"
	EmbedOllama       = "ollama"
	DefaultEmbedURL   = "http://127.0.0.1:11434"
	DefaultEmbedModel = "qwen3-embedding:0.6b"

	embedBatch        = 16
	maxEmbedRunes     = 1000
	embedQueryTimeout = 15 * time.Second
	embedSyncTimeout  = 60 * time.Second
	maxEmbedResponse  = 64 << 20

	// denseFloor drops dense candidates below this cosine similarity, so an
	// unrelated question does not return the least unrelated memory;
	// denseCandidates caps how many enter the fusion. prefetchDenseFloor is
	// the stricter bar a hit without a keyword or alias match must pass to be
	// put into a prompt unasked. Both were chosen on the evaluation set with
	// qwen3-embedding:0.6b; see TestMemoryRetrievalWithOllama.
	denseFloor         = 0.35
	denseCandidates    = 10
	prefetchDenseFloor = 0.45
)

// LegDense names the embedding leg.
const LegDense = "语义"

// Embedder turns texts into vectors of one model.
type Embedder interface {
	Model() string
	Endpoint() string
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// OllamaEmbedder calls a local Ollama server's /api/embed with the standard
// library; it adds no dependency.
type OllamaEmbedder struct {
	endpoint string
	model    string
	client   *http.Client
}

func NewOllamaEmbedder(endpoint, model string) (*OllamaEmbedder, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("embedding endpoint must be an http(s) URL: %q", endpoint)
	}
	if strings.TrimSpace(model) == "" {
		return nil, errors.New("embedding model is required")
	}
	return &OllamaEmbedder{endpoint: strings.TrimRight(endpoint, "/"), model: model, client: &http.Client{}}, nil
}

func (e *OllamaEmbedder) Model() string    { return e.model }
func (e *OllamaEmbedder) Endpoint() string { return e.endpoint }

func (e *OllamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(map[string]any{"model": e.model, "input": texts, "truncate": true})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := e.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("ollama unreachable at %s: %w", e.endpoint, errors.Unwrap(err))
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxEmbedResponse))
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &failure)
		return nil, fmt.Errorf("ollama %s: %s", response.Status, truncateRunes(failure.Error, 120))
	}
	var answer struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return nil, fmt.Errorf("ollama answer: %w", err)
	}
	if len(answer.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama returned %d vectors for %d texts", len(answer.Embeddings), len(texts))
	}
	for _, vector := range answer.Embeddings {
		if len(vector) == 0 || len(vector) != len(answer.Embeddings[0]) {
			return nil, errors.New("ollama returned vectors of different sizes")
		}
	}
	return answer.Embeddings, nil
}

// queryText adds the instruction the Qwen3 embedding models expect on the
// query side; documents are embedded as they are.
func queryText(model, query string) string {
	if strings.HasPrefix(model, "qwen3-embedding") {
		return "Instruct: Given a question or keywords, retrieve the personal memory note that answers it\nQuery: " + query
	}
	return query
}

// documentText is what is embedded for a memory.
func documentText(record memory.Record) string {
	return truncateRunes(record.Name+"\n"+record.Description+"\n"+strings.TrimSpace(record.Body), maxEmbedRunes)
}

// vectorHash ties a vector to the memory text it was computed from.
func vectorHash(record memory.Record) string {
	h := fnv.New64a()
	for _, part := range []string{record.Name, record.Description, strings.TrimSpace(record.Body)} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%016x", h.Sum64())
}

func normalize(vector []float32) []float32 {
	sum := 0.0
	for _, x := range vector {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return vector
	}
	norm := float32(math.Sqrt(sum))
	out := make([]float32, len(vector))
	for i, x := range vector {
		out[i] = x / norm
	}
	return out
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	dot := float32(0)
	for i := range a {
		dot += a[i] * b[i]
	}
	return float64(dot)
}

func encodeVector(vector []float32) []byte {
	raw := make([]byte, 4*len(vector))
	for i, x := range vector {
		binary.LittleEndian.PutUint32(raw[4*i:], math.Float32bits(x))
	}
	return raw
}

func decodeVector(raw []byte) []float32 {
	vector := make([]float32, len(raw)/4)
	for i := range vector {
		vector[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[4*i:]))
	}
	return vector
}

// VectorStatus is what the page shows about the dense leg.
type VectorStatus struct {
	Model     string     `json:"model"`
	Endpoint  string     `json:"endpoint"`
	Indexed   int        `json:"indexed"`
	Total     int        `json:"total"`
	Available bool       `json:"available"`
	LastError string     `json:"last_error,omitempty"`
	CheckedAt *time.Time `json:"checked_at,omitempty"`
}

type memoryVector struct {
	hash   string
	vector []float32
}

// VectorIndex is the dense leg over the owner's memories.
type VectorIndex struct {
	store    *Store
	embedder Embedder
	health   *Health
	now      clock
	floor    float64

	// mu serializes syncs and searches; statusMu guards status alone, so the
	// page never waits behind an embedding call.
	mu       sync.Mutex
	loaded   bool
	vectors  map[string]memoryVector
	statusMu sync.Mutex
	status   VectorStatus
}

func NewVectorIndex(store *Store, embedder Embedder, now clock) *VectorIndex {
	return &VectorIndex{store: store, embedder: embedder, now: now, floor: denseFloor, vectors: map[string]memoryVector{},
		status: VectorStatus{Model: embedder.Model(), Endpoint: embedder.Endpoint()}}
}

// Status reports the last outcome; Available stays false until a search or
// the start-up sync reaches the embedder.
func (v *VectorIndex) Status() VectorStatus {
	v.statusMu.Lock()
	defer v.statusMu.Unlock()
	return v.status
}

// record notes one embedder outcome in the status and the health board.
func (v *VectorIndex) record(err error) {
	now := v.now()
	v.statusMu.Lock()
	defer v.statusMu.Unlock()
	v.status.CheckedAt = &now
	v.status.Available = err == nil
	v.status.LastError = ""
	if err != nil {
		v.status.LastError = truncateRunes(err.Error(), 200)
	}
	v.health.Record(HealthEmbed, err, now)
}

// Sync embeds new or changed memories and forgets removed ones.
func (v *VectorIndex) Sync(ctx context.Context, records []memory.Record) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.sync(ctx, records)
}

func (v *VectorIndex) sync(ctx context.Context, records []memory.Record) error {
	model := v.embedder.Model()
	if !v.loaded {
		// Vectors of another model are useless for this one.
		if _, err := v.store.db.ExecContext(ctx, `DELETE FROM memory_vectors WHERE model<>?`, model); err != nil {
			return err
		}
		stored, err := v.store.memoryVectors(ctx, model)
		if err != nil {
			return err
		}
		v.vectors, v.loaded = stored, true
	}
	keep := map[string]bool{}
	stale := []memory.Record{}
	for _, record := range records {
		keep[record.Name] = true
		if have, ok := v.vectors[record.Name]; !ok || have.hash != vectorHash(record) {
			stale = append(stale, record)
		}
	}
	removed := []string{}
	for name := range v.vectors {
		if !keep[name] {
			removed = append(removed, name)
		}
	}
	written := map[string]memoryVector{}
	var embedErr error
	if len(stale) > 0 {
		bounded, cancel := context.WithTimeout(ctx, embedSyncTimeout)
		for start := 0; start < len(stale) && embedErr == nil; start += embedBatch {
			batch := stale[start:min(start+embedBatch, len(stale))]
			texts := make([]string, len(batch))
			for i, record := range batch {
				texts[i] = documentText(record)
			}
			vectors, err := v.embedder.Embed(bounded, texts)
			if err != nil {
				embedErr = err
				break
			}
			for i, record := range batch {
				written[record.Name] = memoryVector{hash: vectorHash(record), vector: normalize(vectors[i])}
			}
		}
		cancel()
	}
	if len(written) > 0 || len(removed) > 0 {
		if err := v.store.inTx(ctx, func(tx *sql.Tx) error { return writeVectors(ctx, tx, model, written, removed, v.now()) }); err != nil {
			return err
		}
		for name, vector := range written {
			v.vectors[name] = vector
		}
		for _, name := range removed {
			delete(v.vectors, name)
		}
	}
	indexed := 0
	for _, record := range records {
		if have, ok := v.vectors[record.Name]; ok && have.hash == vectorHash(record) {
			indexed++
		}
	}
	v.statusMu.Lock()
	v.status.Indexed, v.status.Total = indexed, len(records)
	v.statusMu.Unlock()
	if len(stale) > 0 {
		v.record(embedErr)
	}
	return embedErr
}

// Probe checks the embedder once when nothing has reached it yet, so the
// status is true after a restart whose vectors were all stored.
func (v *VectorIndex) Probe(ctx context.Context) error {
	if v.Status().CheckedAt != nil {
		return nil
	}
	bounded, cancel := context.WithTimeout(ctx, embedQueryTimeout)
	defer cancel()
	_, err := v.embedder.Embed(bounded, []string{queryText(v.embedder.Model(), "ping")})
	v.record(err)
	return err
}

// denseHit is one memory the dense leg ranked.
type denseHit struct {
	index int
	score float64
}

// rank embeds the query and ranks the memories at or above the floor, best
// first, at most denseCandidates.
func (v *VectorIndex) rank(ctx context.Context, query string, records []memory.Record) ([]denseHit, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.sync(ctx, records); err != nil {
		return nil, err
	}
	bounded, cancel := context.WithTimeout(ctx, embedQueryTimeout)
	defer cancel()
	vectors, err := v.embedder.Embed(bounded, []string{queryText(v.embedder.Model(), query)})
	v.record(err)
	if err != nil {
		return nil, err
	}
	asked := normalize(vectors[0])
	hits := []denseHit{}
	for i, record := range records {
		have, ok := v.vectors[record.Name]
		if !ok || have.hash != vectorHash(record) {
			continue
		}
		if score := cosine(asked, have.vector); score >= v.floor {
			hits = append(hits, denseHit{i, score})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool { return hits[a].score > hits[b].score })
	return hits[:min(len(hits), denseCandidates)], nil
}

// memoryVectors loads the stored vectors of one model; rows of another model
// are left for the sync to replace.
func (s *Store) memoryVectors(ctx context.Context, model string) (map[string]memoryVector, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name,hash,vector FROM memory_vectors WHERE model=?`, model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]memoryVector{}
	for rows.Next() {
		var name, hash string
		var raw []byte
		if err := rows.Scan(&name, &hash, &raw); err != nil {
			return nil, err
		}
		result[name] = memoryVector{hash: hash, vector: decodeVector(raw)}
	}
	return result, rows.Err()
}

func writeVectors(ctx context.Context, q queryer, model string, written map[string]memoryVector, removed []string, now time.Time) error {
	for name, vector := range written {
		if _, err := q.ExecContext(ctx, `INSERT INTO memory_vectors(name,hash,model,dims,vector,updated_at) VALUES(?,?,?,?,?,?)
			ON CONFLICT(name) DO UPDATE SET hash=excluded.hash, model=excluded.model, dims=excluded.dims, vector=excluded.vector, updated_at=excluded.updated_at`,
			name, vector.hash, model, len(vector.vector), encodeVector(vector.vector), millis(now)); err != nil {
			return err
		}
	}
	for _, name := range removed {
		if _, err := q.ExecContext(ctx, `DELETE FROM memory_vectors WHERE name=?`, name); err != nil {
			return err
		}
	}
	return nil
}
