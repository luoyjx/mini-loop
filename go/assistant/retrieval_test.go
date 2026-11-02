package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestSearchTokensSplitChineseIntoBigrams(t *testing.T) {
	for text, want := range map[string][]string{
		"哪天不能开会": {"哪天", "天不", "不能", "能开", "开会"},
		"周三 开会":  {"周三", "开会"},
		"我的经理是 Alice（alice@acme.example）": {"我的", "的经", "经理", "理是", "alice", "alice", "acme", "example"},
		"茶":                              {"茶"},
		"Prefers replies in the English": {"prefers", "replies", "english"},
		"Q4 a 12 月":                      {"q4", "12", "月"},
	} {
		if got := searchTokens(text); !reflect.DeepEqual(got, want) {
			t.Errorf("searchTokens(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestBM25AndFusion(t *testing.T) {
	texts := []string{"日程冲突的邀约婉拒", "周三下午不开会", "早上第一件事是处理邮件，十点前不排会"}
	if got := newBM25(texts).rank(searchTokens("哪天不能开会")); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("only the text sharing a bigram ranks: %v", got)
	}
	if got := newBM25(texts).rank(searchTokens("boss")); len(got) != 0 {
		t.Fatalf("no shared term, no rank: %v", got)
	}
	fused := fuse([]rankedLeg{{"a", []int{2, 0}}, {"b", []int{0}}})
	if len(fused) != 2 || fused[0].index != 0 || !reflect.DeepEqual(fused[0].legs, []string{"a", "b"}) || fused[1].index != 2 {
		t.Fatalf("agreement between legs ranks first: %+v", fused)
	}
}

func TestParseAliasesKeepsAskedMemoriesAndBounds(t *testing.T) {
	records := []memory.Record{{Name: "manager", Body: "我的经理是 Alice"}, {Name: "travel", Body: "靠窗"}}
	answer := `好的：{"manager":["老板"," 老板 ","boss","Manager","manager","一二三四五六七八九十一二三四五六七八九十一二三四五","上司","领导","supervisor","head","lead","chief"],"other":["x"]}`
	aliases, err := parseAliases(protocol.ModelReply{Content: []protocol.Block{protocol.NewTextBlock(answer)}}, records)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"老板", "boss", "上司", "领导", "supervisor", "head", "lead", "chief"}
	if len(aliases) != 1 || aliases[0].Name != "manager" || !reflect.DeepEqual(aliases[0].Aliases, want) || aliases[0].Hash != aliasHash(records[0]) {
		t.Fatalf("aliases: %+v", aliases)
	}
	if _, err := parseAliases(protocol.ModelReply{Content: []protocol.Block{protocol.NewTextBlock("没有")}}, records); err == nil {
		t.Fatal("an answer without JSON is an error")
	}
}

// openSearch builds a memory store and an assistant store for search tests.
func openSearch(t *testing.T) (*MemorySearch, *memory.ScopedStore, *Store) {
	t.Helper()
	ctx := context.Background()
	memories, err := memory.NewStore(ctx, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := memory.Bind(memories, memory.OwnerID(Owner))
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(ctx, filepath.Join(t.TempDir(), "assistant.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return NewMemorySearch(owned, store), owned, store
}

func setAliases(t *testing.T, store *Store, owned *memory.ScopedStore, aliases map[string][]string) {
	t.Helper()
	ctx := context.Background()
	records, err := owned.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rows := []MemoryAlias{}
	for _, record := range records {
		if words, ok := aliases[record.Name]; ok {
			rows = append(rows, MemoryAlias{Name: record.Name, Hash: aliasHash(record), Aliases: words})
		}
	}
	if err := writeAliases(ctx, store.db, rows, records, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func names(hits []MemoryHit) []string {
	result := []string{}
	for _, hit := range hits {
		result = append(result, hit.Record.Name)
	}
	return result
}

func TestMemorySearchUsesAliasesForTheCurrentText(t *testing.T) {
	ctx := context.Background()
	search, owned, store := openSearch(t)
	for _, input := range []memory.Input{
		{Name: "manager", Type: memory.User, Description: "我的经理", Body: "我的经理是 Alice（alice@acme.example）。"},
		{Name: "travel", Type: memory.Feedback, Description: "出差偏好", Body: "出差优先选靠窗座位。"},
	} {
		if _, err := owned.Write(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	if hits, err := search.Search(ctx, "我老板是谁", 3); err != nil || len(hits) != 0 {
		t.Fatalf("no alias, no hit: %v %v", names(hits), err)
	}
	setAliases(t, store, owned, map[string][]string{"manager": {"老板", "boss"}, "gone": {"x"}})
	hits, err := search.Search(ctx, "我老板是谁", 3)
	if err != nil || len(hits) != 1 || hits[0].Record.Name != "manager" || !reflect.DeepEqual(hits[0].Legs, []string{LegAlias}) {
		t.Fatalf("the alias finds the memory: %+v %v", hits, err)
	}
	if block := memoryHitBlock(hits[0]); !strings.Contains(block, `matched="别名"`) || !strings.Contains(block, "我的经理是 Alice") {
		t.Fatalf("hit block: %s", block)
	}
	if hits, _ := search.Search(ctx, "出差 alice", 3); !reflect.DeepEqual(names(hits), []string{"manager", "travel"}) && !reflect.DeepEqual(names(hits), []string{"travel", "manager"}) {
		t.Fatalf("both keyword matches: %v", names(hits))
	}
	if hits, _ := search.Search(ctx, "Ali", 3); len(hits) != 1 || !reflect.DeepEqual(hits[0].Legs, []string{LegSubstring}) {
		t.Fatalf("the substring leg finds part of a word: %+v", hits)
	}
	if listed, _ := search.Search(ctx, "", 1); len(listed) != 1 || len(listed[0].Legs) != 0 {
		t.Fatalf("an empty query lists: %+v", listed)
	}
	if _, err := owned.Write(ctx, memory.Input{Name: "manager", Type: memory.User, Description: "我的经理", Body: "我的经理现在是 Carol。"}); err != nil {
		t.Fatal(err)
	}
	if hits, _ := search.Search(ctx, "我老板是谁", 3); len(hits) != 0 {
		t.Fatalf("aliases for an older text do not count: %v", names(hits))
	}
}

// recallEval is testdata/recall_eval.json.
type recallEval struct {
	Memories []struct {
		Name        string      `json:"name"`
		Type        memory.Type `json:"type"`
		Description string      `json:"description"`
		Body        string      `json:"body"`
		Aliases     []string    `json:"aliases"`
	} `json:"memories"`
	Queries []struct {
		Category string `json:"category"`
		Query    string `json:"query"`
		Want     string `json:"want"`
	} `json:"queries"`
	Negatives []string `json:"negatives"`
}

// recallScore is one configuration's result on the evaluation set.
type recallScore struct {
	Config     string         `json:"config"`
	Hits       map[string]int `json:"hits_at_3"`
	Total      int            `json:"total_hits_at_3"`
	MRR        float64        `json:"mrr_at_3"`
	Missed     []string       `json:"missed"`
	Categories map[string]int `json:"queries_per_category"`
}

var recallCategories = []string{"topic", "split", "question", "paraphrase", "cross-language"}

// loadRecallEval writes the evaluation memories and their aliases.
func loadRecallEval(t *testing.T) (recallEval, *MemorySearch) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "recall_eval.json"))
	if err != nil {
		t.Fatal(err)
	}
	var eval recallEval
	if err := json.Unmarshal(raw, &eval); err != nil {
		t.Fatal(err)
	}
	search, owned, store := openSearch(t)
	aliases := map[string][]string{}
	for _, m := range eval.Memories {
		if _, err := owned.Write(context.Background(), memory.Input{Name: m.Name, Type: m.Type, Description: m.Description, Body: m.Body}); err != nil {
			t.Fatal(err)
		}
		aliases[m.Name] = m.Aliases
	}
	setAliases(t, store, owned, aliases)
	return eval, search
}

func scoreRecall(t *testing.T, eval recallEval, config string, run func(query string) ([]MemoryHit, error)) recallScore {
	t.Helper()
	score := recallScore{Config: config, Hits: map[string]int{}, Missed: []string{}, Categories: map[string]int{}}
	for _, q := range eval.Queries {
		score.Categories[q.Category]++
		hits, err := run(q.Query)
		if err != nil {
			t.Fatal(err)
		}
		rank := slices.Index(names(hits), q.Want)
		if rank < 0 {
			score.Missed = append(score.Missed, q.Query)
			continue
		}
		score.Hits[q.Category]++
		score.Total++
		score.MRR += 1 / float64(rank+1)
	}
	score.MRR = float64(int(score.MRR/float64(len(eval.Queries))*1000+0.5)) / 1000
	return score
}

func (score recallScore) line() string {
	parts := []string{}
	for _, category := range recallCategories {
		parts = append(parts, fmt.Sprintf("%s %d/%d", category, score.Hits[category], score.Categories[category]))
	}
	return fmt.Sprintf("%-14s hit@3 %2d/%d  MRR@3 %.3f  %s", score.Config, score.Total, sum(score.Categories), score.MRR, strings.Join(parts, " · "))
}

func sum(counts map[string]int) int {
	total := 0
	for _, n := range counts {
		total += n
	}
	return total
}

// TestMemoryRetrievalEvaluation measures each retrieval configuration on the
// fixed evaluation set: hits in the top 3 per category and MRR@3. The pinned
// totals make any change visible; set MINILOOP_ASSISTANT_RECALL_REPORT to a
// path to write the scores as JSON.
func TestMemoryRetrievalEvaluation(t *testing.T) {
	ctx := context.Background()
	eval, search := loadRecallEval(t)
	configs := []struct {
		name string
		use  legSet
		want int
	}{
		{"子串（旧 recall）", legSet{substring: true}, 12},
		{"关键词 BM25", legSet{keyword: true}, 22},
		{"关键词+子串", legSet{keyword: true, substring: true}, 22},
		{"三路（+别名）", allLegs, 39},
	}
	scores := []recallScore{}
	for _, config := range configs {
		score := scoreRecall(t, eval, config.name, func(query string) ([]MemoryHit, error) { return search.search(ctx, query, 3, config.use) })
		t.Log(score.line())
		t.Logf("%s missed: %v", config.name, score.Missed)
		if score.Total != config.want {
			t.Errorf("%s: %d hits in the top 3, pinned %d; update the evaluation record if this changed on purpose", config.name, score.Total, config.want)
		}
		scores = append(scores, score)
	}
	old := func(query string) ([]MemoryHit, error) {
		records, err := search.memories.Search(ctx, query, 3)
		hits := []MemoryHit{}
		for _, record := range records {
			hits = append(hits, MemoryHit{Record: record})
		}
		return hits, err
	}
	if baseline := scoreRecall(t, eval, "memory.Search", old); baseline.Total != scores[0].Total {
		t.Errorf("the substring leg alone ranks like memory.Search: %d vs %d", scores[0].Total, baseline.Total)
	}
	if path := os.Getenv("MINILOOP_ASSISTANT_RECALL_REPORT"); path != "" {
		raw, err := json.MarshalIndent(scores, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Before the nightly consolidation a question in other words reaches the
// manager memory only through the contact table's fact (the entity leg); the
// nightly aliases add the alias leg. Workers get memory_search instead of
// recall, and each turn's prompt carries the facts and memories related to
// its events.
func TestAliasesLetAQuestionInOtherWordsFindTheMemory(t *testing.T) {
	clock := newClock()
	recorder := &promptRecorder{next: &ScriptedModel{Now: clock.Now}}
	app, _ := launchApp(t, t.TempDir(), clock, appOptions{provider: recorder})
	ctx := context.Background()
	ask := func(text string) {
		t.Helper()
		if _, err := app.Store.ChatFromUser(ctx, text, clock.Now()); err != nil {
			t.Fatal(err)
		}
		app.Router.Wake()
		settle(t, app.Store)
	}
	ask("我老板是谁")
	if !channelHas(t, app, "我记得：我的经理：Alice <"+ManagerAddress+">") {
		t.Fatal("before the nightly consolidation the contact table's fact answers")
	}
	if found := searchPage(t, app, "我老板是谁"); len(found.Hits) == 0 || found.Hits[0].Name != "manager" || !slices.Equal(found.Hits[0].Legs, []string{LegEntity}) || len(found.Facts) != 1 {
		t.Fatalf("before the nightly run only the entity leg finds the manager memory: %+v", found)
	}
	if tools := recorder.lastTools(); !slices.Contains(tools, ToolMemorySearch) || slices.Contains(tools, protocol.ToolRecall) || !slices.Contains(tools, protocol.ToolRemember) {
		t.Fatalf("workers search with memory_search, not recall: %v", tools)
	}

	clock.Set(clock.Now().Add(time.Minute))
	id, err := app.Scheduler.RunNow(ctx, NightlyRoutine.ID)
	if err != nil {
		t.Fatal(err)
	}
	if nightly := triggerByID(t, settle(t, app.Store), id); nightly.Status != TriggerQuiet {
		t.Fatalf("nightly: %+v", nightly)
	}
	if !ledgerHas(t, app, LedgerMemory, "补检索别名 2 条") {
		t.Fatal("the nightly consolidation writes aliases for both seeded memories")
	}
	if prompt := recorder.last("守护进程已经完成的整理"); prompt == "" || strings.Contains(prompt, "可能相关的记忆") {
		t.Fatalf("a routine has no subject and prefetches nothing:\n%s", prompt)
	}
	aliases, err := app.Store.MemoryAliases(ctx)
	if err != nil || !slices.Contains(aliases["manager"].Aliases, "老板") {
		t.Fatalf("aliases: %+v %v", aliases, err)
	}

	for _, query := range []string{"我老板是谁", "boss"} {
		if found := searchPage(t, app, query); len(found.Hits) == 0 || found.Hits[0].Name != "manager" || !slices.Contains(found.Hits[0].Legs, LegAlias) {
			t.Fatalf("after the nightly consolidation the alias leg finds the manager for %q: %+v", query, found.Hits)
		}
	}
	code, body := apiCall(t, app, "GET", "/api/state", "")
	if code != 200 || !strings.Contains(body, `"aliases":["老板"`) || !strings.Contains(body, "语义检索未启用") {
		t.Fatalf("state lists aliases and the retrieval mode: %d", code)
	}

	// A re-run converges: nothing changed, nothing is asked again.
	clock.Set(clock.Now().Add(time.Minute))
	if _, err := app.Scheduler.RunNow(ctx, NightlyRoutine.ID); err != nil {
		t.Fatal(err)
	}
	settle(t, app.Store)
	if !ledgerHas(t, app, LedgerMemory, "补检索别名 0 条") {
		t.Fatal("a re-run writes no aliases for unchanged memories")
	}

	deliver(t, app, PresetManager)
	settle(t, app.Store)
	if prompt := recorder.last("新邮件到达"); !strings.Contains(prompt, "可能相关的记忆") || !strings.Contains(prompt, "· manager：我的经理是 Alice") ||
		!strings.Contains(prompt, "相关的人物事实") || !strings.Contains(prompt, "· 我的经理：Alice <"+ManagerAddress+">") {
		t.Fatalf("the worker prompt carries the related facts and memories:\n%s", prompt)
	}
}

// searchPage runs the page's memory search.
func searchPage(t *testing.T, app *App, query string) MemorySearchView {
	t.Helper()
	code, body := apiCall(t, app, "GET", "/api/memory/search?q="+url.QueryEscape(query), "")
	var found MemorySearchView
	if code != 200 || json.Unmarshal([]byte(body), &found) != nil {
		t.Fatalf("page search %q: %d %s", query, code, body)
	}
	return found
}

// A failed alias request is reported in the ledger and retried the next
// night; the rest of the consolidation goes on.
func TestFailedAliasRequestIsRetriedTheNextNight(t *testing.T) {
	clock := newClock()
	failing := &aliasOutage{next: &ScriptedModel{Now: clock.Now}, failures: 1}
	app, _ := launchApp(t, t.TempDir(), clock, appOptions{provider: failing})
	ctx := context.Background()
	for range 2 {
		clock.Set(clock.Now().Add(time.Minute))
		if _, err := app.Scheduler.RunNow(ctx, NightlyRoutine.ID); err != nil {
			t.Fatal(err)
		}
		settle(t, app.Store)
	}
	if !ledgerHas(t, app, LedgerMemory, "补检索别名 0 条，新事实") || !ledgerHas(t, app, LedgerMemory, "别名生成失败，下次整理再试") {
		t.Fatal("the first night reports the failure")
	}
	if !ledgerHas(t, app, LedgerMemory, "补检索别名 2 条") {
		t.Fatal("the next night writes the aliases")
	}
}

// aliasOutage fails the first alias requests.
type aliasOutage struct {
	next     *ScriptedModel
	failures int
}

func (m *aliasOutage) Complete(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	if request.Purpose == protocol.PurposeMemoryConsolidation && m.failures > 0 {
		m.failures--
		return protocol.ModelReply{}, fmt.Errorf("model outage")
	}
	return m.next.Complete(ctx, request)
}
