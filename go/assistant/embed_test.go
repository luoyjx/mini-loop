package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/memory"
)

// conceptEmbedder is a deterministic stand-in for an embedding model: each
// concept is one axis, and a text scores on the axes whose words it
// contains. It tests the plumbing, not retrieval quality.
type conceptEmbedder struct {
	model string
	mu    sync.Mutex
	calls int
	texts int
	fail  error
}

var concepts = [][]string{
	{"经理", "老板", "上司", "boss", "manager"},
	{"过敏", "花生", "吃", "peanut", "allergy"},
	{"账单", "invoice", "扣款", "bill"},
	{"出差", "飞机", "flight", "靠窗"},
}

func (e *conceptEmbedder) Model() string    { return e.model }
func (e *conceptEmbedder) Endpoint() string { return "test://concepts" }

func (e *conceptEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fail != nil {
		return nil, e.fail
	}
	e.calls++
	e.texts += len(texts)
	vectors := make([][]float32, len(texts))
	for i, text := range texts {
		vector := make([]float32, len(concepts)+1)
		lower := strings.ToLower(text)
		for axis, words := range concepts {
			for _, word := range words {
				if strings.Contains(lower, word) {
					vector[axis]++
				}
			}
		}
		vector[len(concepts)] = 0.3
		vectors[i] = vector
	}
	return vectors, nil
}

func (e *conceptEmbedder) counts() (int, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls, e.texts
}

func (e *conceptEmbedder) setFail(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.fail = err
}

func vectorRows(t *testing.T, store *Store) map[string]string {
	t.Helper()
	rows, err := store.db.Query(`SELECT name, model FROM memory_vectors`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	result := map[string]string{}
	for rows.Next() {
		var name, model string
		if err := rows.Scan(&name, &model); err != nil {
			t.Fatal(err)
		}
		result[name] = model
	}
	return result
}

func TestVectorIndexEmbedsLazilyAndKeepsTheIndexCurrent(t *testing.T) {
	ctx := context.Background()
	search, owned, store := openSearch(t)
	inputs := []memory.Input{
		{Name: "manager", Type: memory.User, Description: "我的经理", Body: "我的经理是 Alice。"},
		{Name: "allergy", Type: memory.User, Description: "过敏", Body: "对花生过敏，订餐时要避开。"},
		{Name: "travel", Type: memory.Feedback, Description: "出差偏好", Body: "出差优先选靠窗座位。"},
	}
	for _, input := range inputs {
		if _, err := owned.Write(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	embedder := &conceptEmbedder{model: "fake-a"}
	search.vectors = NewVectorIndex(store, embedder, time.Now)

	hits, err := search.Search(ctx, "不能吃什么", 3)
	if err != nil || len(hits) != 1 || hits[0].Record.Name != "allergy" || !reflect.DeepEqual(hits[0].Legs, []string{LegDense}) || hits[0].Dense < 0.9 {
		t.Fatalf("the dense leg finds a question with no shared word: %+v %v", hits, err)
	}
	if block := memoryHitBlock(hits[0]); !strings.Contains(block, `matched="语义" similarity="0.9`) {
		t.Fatalf("a dense hit shows its similarity: %s", block)
	}
	if calls, texts := embedder.counts(); calls != 2 || texts != 4 {
		t.Fatalf("first search embeds three memories and the query: %d calls, %d texts", calls, texts)
	}
	if hits, _ := search.Search(ctx, "我老板是谁", 3); len(hits) != 1 || hits[0].Record.Name != "manager" {
		t.Fatalf("manager: %+v", hits)
	}
	if _, texts := embedder.counts(); texts != 5 {
		t.Fatalf("a later search embeds only its query: %d texts", texts)
	}
	if status := search.vectors.Status(); !status.Available || status.Indexed != 3 || status.Total != 3 || status.Model != "fake-a" {
		t.Fatalf("status: %+v", status)
	}
	if mode := search.Mode(); !strings.Contains(mode, "四路按排名合并") || !strings.Contains(mode, "fake-a") {
		t.Fatalf("mode: %s", mode)
	}

	if _, err := owned.Write(ctx, memory.Input{Name: "travel", Type: memory.Feedback, Description: "出差偏好", Body: "出差优先选靠窗座位和早班飞机。"}); err != nil {
		t.Fatal(err)
	}
	if _, err := search.Search(ctx, "flight", 3); err != nil {
		t.Fatal(err)
	}
	if _, texts := embedder.counts(); texts != 7 {
		t.Fatalf("an edited memory is embedded again: %d texts", texts)
	}
	if err := owned.ReplaceAll(ctx, []memory.Input{inputs[0], inputs[2]}, memory.Consolidated); err != nil {
		t.Fatal(err)
	}
	if _, err := search.Search(ctx, "flight", 3); err != nil {
		t.Fatal(err)
	}
	if rows := vectorRows(t, store); len(rows) != 2 || rows["allergy"] != "" {
		t.Fatalf("a removed memory loses its vector: %v", rows)
	}

	records, err := owned.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	restarted := &conceptEmbedder{model: "fake-a"}
	again := NewVectorIndex(store, restarted, time.Now)
	if err := again.Sync(ctx, records); err != nil {
		t.Fatal(err)
	}
	if _, texts := restarted.counts(); texts != 0 || again.Status().CheckedAt != nil {
		t.Fatalf("stored vectors survive a restart: %d texts embedded", texts)
	}
	if err := again.Probe(ctx); err != nil || !again.Status().Available {
		t.Fatalf("the probe confirms the embedder: %v %+v", err, again.Status())
	}
	if err := again.Probe(ctx); err != nil {
		t.Fatal(err)
	}
	if calls, _ := restarted.counts(); calls != 1 {
		t.Fatalf("the probe runs once: %d calls", calls)
	}
	other := &conceptEmbedder{model: "fake-b"}
	if err := NewVectorIndex(store, other, time.Now).Sync(ctx, records); err != nil {
		t.Fatal(err)
	}
	if rows := vectorRows(t, store); !reflect.DeepEqual(rows, map[string]string{"manager": "fake-b", "travel": "fake-b"}) {
		t.Fatalf("another model replaces every vector: %v", rows)
	}
}

func TestDenseLegFallsBackWhenTheEmbedderFails(t *testing.T) {
	ctx := context.Background()
	search, owned, store := openSearch(t)
	if _, err := owned.Write(ctx, memory.Input{Name: "manager", Type: memory.User, Description: "我的经理", Body: "我的经理是 Alice。"}); err != nil {
		t.Fatal(err)
	}
	embedder := &conceptEmbedder{model: "fake-a", fail: errors.New("connection refused")}
	health := NewHealth()
	search.vectors = NewVectorIndex(store, embedder, time.Now)
	search.vectors.health = health
	if mode := search.Mode(); !strings.Contains(mode, "还没连上过") {
		t.Fatalf("before any attempt: %s", mode)
	}
	for range healthThreshold {
		hits, err := search.Search(ctx, "我的经理", 3)
		if err != nil || len(hits) != 1 || slices.Contains(hits[0].Legs, LegDense) {
			t.Fatalf("the lexical legs still answer: %+v %v", hits, err)
		}
	}
	status := search.vectors.Status()
	if status.Available || !strings.Contains(status.LastError, "connection refused") || !strings.Contains(search.Mode(), "暂时不可用") {
		t.Fatalf("status names the failure: %+v %s", status, search.Mode())
	}
	if problems := health.Problems(); len(problems) != 1 || problems[0].Component != HealthEmbed {
		t.Fatalf("three failures reach the health board: %+v", problems)
	}
	embedder.setFail(nil)
	hits, err := search.Search(ctx, "我老板是谁", 3)
	if err != nil || len(hits) != 1 || !slices.Contains(hits[0].Legs, LegDense) {
		t.Fatalf("the leg comes back with the embedder: %+v %v", hits, err)
	}
	if len(health.Problems()) != 0 || !search.vectors.Status().Available {
		t.Fatal("a success clears the problem")
	}
}

func TestPrefetchKeepsOnlyConfidentDenseHits(t *testing.T) {
	for _, c := range []struct {
		hit  MemoryHit
		want bool
	}{
		{MemoryHit{Legs: []string{LegDense}, Dense: prefetchDenseFloor - 0.01}, false},
		{MemoryHit{Legs: []string{LegDense}, Dense: prefetchDenseFloor}, true},
		{MemoryHit{Legs: []string{LegDense, LegKeyword}, Dense: denseFloor}, true},
		{MemoryHit{Legs: []string{LegAlias}}, true},
		{MemoryHit{Legs: []string{LegSubstring}}, false},
		{MemoryHit{Legs: []string{LegSubstring, LegDense}, Dense: prefetchDenseFloor}, true},
	} {
		if got := c.hit.prefetchable(); got != c.want {
			t.Errorf("%+v: prefetchable %v, want %v", c.hit, got, c.want)
		}
	}
}

func TestOllamaEmbedderSpeaksTheEmbedAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model    string   `json:"model"`
			Input    []string `json:"input"`
			Truncate bool     `json:"truncate"`
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/embed" || json.NewDecoder(r.Body).Decode(&request) != nil || !request.Truncate {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		switch request.Model {
		case "missing":
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":"model \"missing\" not found, try pulling it first"}`)
		case "short":
			fmt.Fprint(w, `{"embeddings":[[1,0]]}`)
		default:
			vectors := make([][]float32, len(request.Input))
			for i := range vectors {
				vectors[i] = []float32{float32(i + 1), 2}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": vectors})
		}
	}))
	defer server.Close()
	ctx := context.Background()
	embedder, err := NewOllamaEmbedder(server.URL+"/", "good")
	if err != nil {
		t.Fatal(err)
	}
	vectors, err := embedder.Embed(ctx, []string{"a", "b"})
	if err != nil || !reflect.DeepEqual(vectors, [][]float32{{1, 2}, {2, 2}}) || embedder.Endpoint() != server.URL {
		t.Fatalf("vectors: %v %v", vectors, err)
	}
	missing, _ := NewOllamaEmbedder(server.URL, "missing")
	if _, err := missing.Embed(ctx, []string{"a"}); err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("a refusal is reported: %v", err)
	}
	short, _ := NewOllamaEmbedder(server.URL, "short")
	if _, err := short.Embed(ctx, []string{"a", "b"}); err == nil {
		t.Fatal("a wrong vector count is an error")
	}
	server.Close()
	if _, err := embedder.Embed(ctx, []string{"a"}); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("an unreachable server is reported: %v", err)
	}
	for _, bad := range []string{"", "ftp://x", "127.0.0.1:11434"} {
		if _, err := NewOllamaEmbedder(bad, "m"); err == nil {
			t.Errorf("endpoint %q accepted", bad)
		}
	}
	if _, err := NewOllamaEmbedder(DefaultEmbedURL, " "); err == nil {
		t.Error("an empty model is accepted")
	}
}

// With embeddings on, a question in other words finds the memory before the
// nightly consolidation wrote any alias, and the page states the four legs.
func TestEmbeddingsFindAMemoryBeforeAnyAlias(t *testing.T) {
	clock := newClock()
	embedder := &conceptEmbedder{model: "fake-concepts"}
	app, _ := launchApp(t, t.TempDir(), clock, appOptions{embedder: embedder})
	ctx := context.Background()
	if _, err := app.Store.ChatFromUser(ctx, "我老板是谁", clock.Now()); err != nil {
		t.Fatal(err)
	}
	app.Router.Wake()
	settle(t, app.Store)
	if !channelHas(t, app, "我记得：我的经理是 Alice") {
		t.Fatal("the dense leg finds the manager without aliases")
	}
	code, body := apiCall(t, app, "GET", "/api/state", "")
	var state struct {
		Retrieval string        `json:"retrieval"`
		Vectors   *VectorStatus `json:"vectors"`
	}
	if code != 200 || json.Unmarshal([]byte(body), &state) != nil || state.Vectors == nil || state.Vectors.Model != "fake-concepts" || state.Vectors.Indexed != 2 || !strings.Contains(state.Retrieval, "四路按排名合并") {
		t.Fatalf("state: %d %+v", code, state)
	}
	code, body = apiCall(t, app, "GET", "/api/memory/search?q=boss", "")
	var hits []MemoryView
	if code != 200 || json.Unmarshal([]byte(body), &hits) != nil || len(hits) == 0 || hits[0].Name != "manager" || !slices.Contains(hits[0].Legs, LegDense) || hits[0].Similarity < denseFloor {
		t.Fatalf("page search shows the similarity: %d %s", code, body)
	}
}

// TestMemoryRetrievalWithOllama measures the dense leg with a real local
// model on the evaluation set. It needs a running Ollama server with the
// model pulled, so it only runs when MINILOOP_ASSISTANT_OLLAMA_URL is set;
// MINILOOP_ASSISTANT_OLLAMA_MODEL overrides the model and
// MINILOOP_ASSISTANT_OLLAMA_REPORT writes the scores as JSON.
func TestMemoryRetrievalWithOllama(t *testing.T) {
	endpoint := os.Getenv("MINILOOP_ASSISTANT_OLLAMA_URL")
	if endpoint == "" {
		t.Skip("set MINILOOP_ASSISTANT_OLLAMA_URL (for example http://127.0.0.1:11434) to measure local embeddings")
	}
	model := os.Getenv("MINILOOP_ASSISTANT_OLLAMA_MODEL")
	if model == "" {
		model = DefaultEmbedModel
	}
	embedder, err := NewOllamaEmbedder(endpoint, model)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	eval, search := loadRecallEval(t)
	search.vectors = NewVectorIndex(search.store, embedder, time.Now)
	type floorReport struct {
		Floor                float64       `json:"floor"`
		Scores               []recallScore `json:"scores"`
		NegativesWithHits    int           `json:"negatives_with_hits"`
		NegativesPrefetched  int           `json:"negatives_prefetched"`
		NegativesPrefetchHit []string      `json:"negatives_prefetched_queries"`
	}
	reports := []floorReport{}
	defaultAllLegs := 0
	for _, floor := range []float64{0.35, 0.40, 0.45} {
		search.vectors.floor = floor
		report := floorReport{Floor: floor, NegativesPrefetchHit: []string{}}
		for _, config := range []struct {
			name string
			use  legSet
		}{
			{"语义", legSet{dense: true}},
			{"关键词+语义", legSet{keyword: true, dense: true}},
			{"四路（无别名）", legSet{keyword: true, substring: true, dense: true}},
			{"四路（+别名）", allLegs},
		} {
			score := scoreRecall(t, eval, config.name, func(query string) ([]MemoryHit, error) { return search.search(ctx, query, 3, config.use) })
			t.Logf("floor %.2f %s; missed %v", floor, score.line(), score.Missed)
			report.Scores = append(report.Scores, score)
			if floor == denseFloor && config.name == "四路（+别名）" {
				defaultAllLegs = score.Total
			}
		}
		for _, query := range eval.Negatives {
			hits, err := search.Search(ctx, query, 3)
			if err != nil {
				t.Fatal(err)
			}
			if len(hits) > 0 {
				report.NegativesWithHits++
			}
			for _, hit := range hits {
				if hit.prefetchable() {
					report.NegativesPrefetched++
					report.NegativesPrefetchHit = append(report.NegativesPrefetchHit, query+" → "+hit.Record.Name)
					break
				}
			}
		}
		t.Logf("floor %.2f negatives: %d/%d return hits, %d/%d would prefetch %v", floor, report.NegativesWithHits, len(eval.Negatives), report.NegativesPrefetched, len(eval.Negatives), report.NegativesPrefetchHit)
		reports = append(reports, report)
	}
	if defaultAllLegs == 0 {
		t.Fatalf("the floors measured do not include the default %.2f", denseFloor)
	}
	if defaultAllLegs < 39 {
		t.Errorf("four legs at the default floor found %d, below the three lexical legs' 39", defaultAllLegs)
	}
	if path := os.Getenv("MINILOOP_ASSISTANT_OLLAMA_REPORT"); path != "" {
		raw, err := json.MarshalIndent(map[string]any{"model": model, "floors": reports}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
