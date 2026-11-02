package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// A mail address names the person, not the employer in its domain.
func TestMentionedTreatsAnAddressAsItsOwnEntity(t *testing.T) {
	index := &factIndex{aliases: map[int64][]string{1: {"alice", "alice@acme.example"}, 2: {"acme", "acme.example"}}}
	for text, want := range map[string][]int64{
		"alice <alice@acme.example> 今晚能把方案发我吗": {1},
		"acme 的年会":             {2},
		"dave@acme.example 来信": nil,
		"alice 和 acme":         {1, 2},
	} {
		got := []int64{}
		for id := range index.mentioned(text) {
			got = append(got, id)
		}
		slices.Sort(got)
		if !slices.Equal(got, append([]int64{}, want...)) {
			t.Errorf("mentioned(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestMentionsNeedsWordBoundariesOnlyForLatin(t *testing.T) {
	for _, c := range []struct {
		text, word string
		want       bool
	}{
		{"alice 的邮件", "alice", true},
		{"malice", "alice", false},
		{"what is carol's role", "carol", true},
		{"我的经理是谁", "经理", true},
		{"alice@acme.example", "acme", true},
		{"acmecorp", "acme", false},
		{"bob和alice", "bob", true},
	} {
		if got := mentions(c.text, c.word); got != c.want {
			t.Errorf("mentions(%q, %q) = %v", c.text, c.word, got)
		}
	}
}

// A single-valued fact keeps a timeline: a later value closes the current
// one, an earlier one slots in between, re-asserting changes nothing, and a
// newer value from a weaker source does not replace a stronger current fact.
func TestFactsKeepATimelineAndPreferStrongerSources(t *testing.T) {
	_, _, store := openSearch(t)
	ctx := context.Background()
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.Local)
	day := func(n int) time.Time { return start.AddDate(0, 0, n) }
	db := store.db
	entity := func(kind EntityKind, name string, aliases ...string) int64 {
		t.Helper()
		id, err := ensureEntity(ctx, db, kind, name, aliases, SourceContacts, start)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	owner, err := ownerEntity(ctx, db, start)
	if err != nil {
		t.Fatal(err)
	}
	alice, carol, bob := entity(EntityPerson, "Alice", "alice@acme.example"), entity(EntityPerson, "Carol"), entity(EntityPerson, "Bob")
	if again := entity(EntityPerson, "ALICE"); again != alice {
		t.Fatalf("aliases are case-insensitive: %d %d", again, alice)
	}
	assert := func(in factInput) bool {
		t.Helper()
		changed, err := assertFact(ctx, db, in, day(60))
		if err != nil {
			t.Fatal(err)
		}
		return changed
	}
	manager := func(object int64, at int, source string) factInput {
		return factInput{subject: owner, predicate: PredManager, object: object, validFrom: day(at), source: source}
	}
	if !assert(manager(alice, 0, SourceContacts)) || !assert(manager(carol, 40, SourceChat)) {
		t.Fatal("both managers are recorded")
	}
	if assert(manager(alice, 0, SourceContacts)) || assert(manager(carol, 40, SourceChat)) {
		t.Fatal("re-asserting a recorded fact writes nothing")
	}
	if !assert(manager(bob, 20, SourceChat)) {
		t.Fatal("an earlier fact slots into the timeline")
	}

	dave, acme, globex, initech, hooli := entity(EntityPerson, "Dave"), entity(EntityOrg, "Acme"), entity(EntityOrg, "Globex"), entity(EntityOrg, "Initech"), entity(EntityOrg, "Hooli")
	works := func(object int64, at int, source string) factInput {
		return factInput{subject: dave, predicate: PredWorksAt, object: object, validFrom: day(at), source: source}
	}
	if !assert(works(acme, 0, SourceDomain)) || !assert(works(globex, 45, SourceChat)) {
		t.Fatal("the user's word replaces the domain's guess")
	}
	if assert(works(initech, 50, SourceDomain)) {
		t.Fatal("a newer domain guess does not replace what the user said")
	}
	if !assert(works(hooli, 55, SourceChat)) {
		t.Fatal("the user's newer word replaces the older one")
	}

	facts, err := store.Facts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{}
	for _, fact := range facts {
		lines = append(lines, fact.line())
	}
	want := []string{
		"我的经理：Carol（自 2026-10-11 起；你在信道里说的）",
		"Dave 的公司：Hooli（自 2026-10-26 起；你在信道里说的）",
		"我的经理：Bob（已过期：2026-09-21 至 2026-10-11，之后是 Carol；你在信道里说的）",
		"我的经理：Alice <alice@acme.example>（已过期：2026-09-01 至 2026-09-21，之后是 Bob；联系人表）",
		"Dave 的公司：Globex（已过期：2026-10-16 至 2026-10-26，之后是 Hooli；你在信道里说的）",
		"Dave 的公司：Acme（已过期：2026-09-01 至 2026-10-16，之后是 Globex；邮箱域名）",
	}
	if !slices.Equal(lines, want) {
		t.Fatalf("timeline:\n%s", strings.Join(lines, "\n"))
	}
	if entry := profileManager(facts); entry.Value != "Carol（自 10/11 起）；之前是 Bob" || entry.Source != "事实表（你在信道里说的）" {
		t.Fatalf("profile line: %+v", entry)
	}
}

func TestParseFactsKeepsTheFixedFormat(t *testing.T) {
	reply := protocol.ModelReply{Content: []protocol.Block{protocol.NewTextBlock("1 | 我 | manager | Carol\n2 | Dave | works_at | Globex\n3 | Carol | ROLE | 产品总监\n9 | X | role | Y\n2 | Dave | likes | tea\n无\n- 2 | Erin | works_at | Initech\n1 | 我 | manager |")}}
	got := []string{}
	for _, fact := range parseFacts(reply, 3) {
		got = append(got, fmt.Sprint(fact.line, fact.subject, fact.predicate, fact.object))
	}
	if want := []string{"1我managerCarol", "2Daveworks_atGlobex", "3Carolrole产品总监", "2Erinworks_atInitech"}; !slices.Equal(got, want) {
		t.Fatalf("parsed: %v", got)
	}
}

func TestScriptedFactRules(t *testing.T) {
	said := []string{"记住 我的经理换成 Carol 了", "我的经理是谁呀", "Alice 是我的经理", "Dave 跳槽去了 Globex", "Carol 现在是产品总监", "my manager is Erin", "Frank joined Initech", "Bob 在公园散步"}
	var b strings.Builder
	fmt.Fprintf(&b, "%scount=%d]\n<chat>\n", factsHeaderStart, len(said))
	for i, text := range said {
		fmt.Fprintf(&b, "%d 11/02 09:%02d %s\n", i+1, i, text)
	}
	b.WriteString("</chat>")
	want := "1 | 我 | manager | Carol\n3 | 我 | manager | Alice\n4 | Dave | works_at | Globex\n5 | Carol | role | 产品总监\n6 | 我 | manager | Erin\n7 | Frank | works_at | Initech"
	if got := scriptedFacts(b.String()); got != want {
		t.Fatalf("scripted facts:\n%s", got)
	}
}

// The nightly consolidation extracts what the user said, the replaced manager
// expires, the profile and the chat answer follow, memories stating the old
// value are marked, and a mail from the former manager carries the history.
func TestNightlyExtractsFactsAndTheProfileFollows(t *testing.T) {
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
	ask("记住 我的经理换成 Carol 了")
	ask("记住 Dave 跳槽去了 Globex")
	night := func() {
		t.Helper()
		clock.Set(clock.Now().Add(time.Minute))
		if _, err := app.Scheduler.RunNow(ctx, NightlyRoutine.ID); err != nil {
			t.Fatal(err)
		}
		settle(t, app.Store)
	}
	night()
	facts, err := app.Store.Facts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	current := map[string]string{}
	replaced := map[string]string{}
	for _, fact := range facts {
		key := fact.Subject + " " + fact.Predicate
		if fact.ValidTo == nil {
			current[key] = fact.Object + "/" + fact.Source
		} else {
			replaced[key+" "+fact.Object] = fact.ReplacedBy
		}
	}
	if current["我 manager"] != "Carol/"+SourceChat || replaced["我 manager Alice"] != "Carol" ||
		current["Dave works_at"] != "Globex/"+SourceChat || replaced["Dave works_at Acme"] != "Globex" || current["Alice works_at"] != "Acme/"+SourceDomain {
		t.Fatalf("facts: current %v replaced %v", current, replaced)
	}
	if profile, _ := app.Store.Profile(ctx); len(profile) != 4 || profile[0].Value != "Carol（自 11/02 起）；之前是 Alice" || strings.Contains(profile[1].Value, "（经理）") {
		t.Fatalf("the profile shows the current manager: %+v", profile)
	}

	ask("我老板是谁")
	if !channelHas(t, app, "我记得：我的经理：Carol（自 2026-11-02 起；你在信道里说的）") {
		t.Fatal("the chat answers with the current fact")
	}
	found := searchPage(t, app, "我的经理是谁")
	if len(found.Facts) < 2 || found.Facts[0].Object != "Carol" || found.Facts[0].ValidTo != nil || found.Facts[1].Object != "Alice" {
		t.Fatalf("the current fact comes first, then the replaced one: %+v", found.Facts)
	}
	marked := false
	for _, hit := range found.Hits {
		if hit.Name == "manager" {
			marked = strings.Contains(hit.Outdated, "我的经理自 2026-11-02 起是 Carol，不再是 Alice")
		} else if hit.Outdated != "" {
			t.Fatalf("only the memory stating the old manager is marked: %+v", hit)
		}
	}
	if !marked {
		t.Fatalf("the old manager memory is marked: %+v", found.Hits)
	}

	night()
	if !ledgerHas(t, app, LedgerMemory, "新事实 0 条") {
		t.Fatal("a re-run converges")
	}

	deliver(t, app, PresetManager)
	settle(t, app.Store)
	prompt := recorder.last("新邮件到达")
	if !strings.Contains(prompt, "相关的人物事实") || !strings.Contains(prompt, "· 我的经理：Alice <"+ManagerAddress+">（已过期") ||
		!strings.Contains(prompt, "（可能过期：我的经理自 2026-11-02 起是 Carol，不再是 Alice）") {
		t.Fatalf("a mail from the former manager carries the history:\n%s", prompt)
	}
	code, body := apiCall(t, app, "GET", "/api/state", "")
	if code != 200 || !strings.Contains(body, `"replaced_by":"Carol"`) {
		t.Fatalf("state lists the facts: %d", code)
	}
}

// factOutage fails the fact extraction requests only.
type factOutage struct {
	next     *ScriptedModel
	failures int
}

func (m *factOutage) Complete(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	if text, ok := messageText(request.Messages[len(request.Messages)-1]); ok && strings.HasPrefix(text, factsHeaderStart) && m.failures > 0 {
		m.failures--
		return protocol.ModelReply{}, fmt.Errorf("model outage")
	}
	return m.next.Complete(ctx, request)
}

// A failed extraction is reported and the chat is asked about again the next
// night; the rest of the consolidation goes on.
func TestFailedFactExtractionIsRetriedTheNextNight(t *testing.T) {
	clock := newClock()
	app, _ := launchApp(t, t.TempDir(), clock, appOptions{provider: &factOutage{next: &ScriptedModel{Now: clock.Now}, failures: 1}})
	ctx := context.Background()
	if _, err := app.Store.ChatFromUser(ctx, "记住 我的经理换成 Carol 了", clock.Now()); err != nil {
		t.Fatal(err)
	}
	app.Router.Wake()
	settle(t, app.Store)
	managers := func() string {
		t.Helper()
		facts, err := app.Store.Facts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, fact := range facts {
			if fact.Subject == ownerName && fact.Predicate == PredManager && fact.ValidTo == nil {
				return fact.Object
			}
		}
		return ""
	}
	for night, want := range []string{"Alice", "Carol"} {
		clock.Set(clock.Now().Add(time.Minute))
		if _, err := app.Scheduler.RunNow(ctx, NightlyRoutine.ID); err != nil {
			t.Fatal(err)
		}
		settle(t, app.Store)
		if got := managers(); got != want {
			t.Fatalf("night %d: manager %q, want %q", night+1, got, want)
		}
	}
	if !ledgerHas(t, app, LedgerMemory, "事实提取失败，下次整理再试") {
		t.Fatal("the failed night is reported")
	}
}

// factsEval is testdata/facts_eval.json.
type factsEval struct {
	Contacts []Contact `json:"contacts"`
	Chat     []struct {
		Day  int    `json:"day"`
		Text string `json:"text"`
	} `json:"chat"`
	Memories []struct {
		Name string `json:"name"`
		Body string `json:"body"`
	} `json:"memories"`
	Queries []struct {
		Query   string   `json:"query"`
		Current string   `json:"current"`
		Stale   []string `json:"stale"`
	} `json:"queries"`
}

// factsScore is the result on the time-sensitive set.
type factsScore struct {
	MemoryCurrent int      `json:"memory_top1_current"`
	MemoryStale   int      `json:"memory_top1_stale"`
	FactCurrent   int      `json:"fact_first_current"`
	StaleInTop3   int      `json:"stale_memories_in_top3"`
	StaleMarked   int      `json:"stale_memories_marked"`
	Queries       int      `json:"queries"`
	Wrong         []string `json:"fact_wrong"`
}

// TestFactsEvaluation measures time-sensitive questions: how often the top
// memory states the current value, how often the first fact does, and how
// many stale memories in the top 3 carry an outdated note. Set
// MINILOOP_ASSISTANT_FACTS_REPORT to a path to write the scores as JSON.
func TestFactsEvaluation(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "facts_eval.json"))
	if err != nil {
		t.Fatal(err)
	}
	var eval factsEval
	if err := json.Unmarshal(raw, &eval); err != nil {
		t.Fatal(err)
	}
	search, owned, store := openSearch(t)
	ctx := context.Background()
	start := time.Date(2026, 9, 1, 9, 0, 0, 0, time.Local)
	for _, contact := range eval.Contacts {
		if _, err := store.db.ExecContext(ctx, `INSERT INTO contacts(address,name,relation,source,created_at) VALUES(?,?,?,?,?)`, contact.Address, contact.Name, contact.Relation, "评估", millis(start)); err != nil {
			t.Fatal(err)
		}
	}
	for _, line := range eval.Chat {
		if _, err := store.ChatFromUser(ctx, line.Text, start.AddDate(0, 0, line.Day)); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range eval.Memories {
		if _, err := owned.Write(ctx, memory.Input{Name: m.Name, Type: memory.User, Description: truncateRunes(m.Body, 20), Body: m.Body, Origin: memory.Explicit}); err != nil {
			t.Fatal(err)
		}
	}
	now := start.AddDate(0, 0, 60)
	contacts, err := contactsIn(ctx, store.db)
	if err != nil {
		t.Fatal(err)
	}
	chat, err := store.chatAfter(ctx, 0, maxFactChat)
	if err != nil {
		t.Fatal(err)
	}
	extracted, err := (&FactExtractor{provider: &ScriptedModel{Now: func() time.Time { return now }}, model: ScriptedModelID}).Extract(ctx, chat)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := updateFacts(ctx, store.db, contacts, nil, nil, chat, extracted, now); err != nil {
		t.Fatal(err)
	}
	states := func(text, value string) bool { return value != "" && strings.Contains(text, value) }
	score := factsScore{Queries: len(eval.Queries), Wrong: []string{}}
	for _, q := range eval.Queries {
		lexical, err := search.search(ctx, q.Query, 3, legSet{keyword: true, alias: true, substring: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(lexical) > 0 {
			top := lexical[0].Record.Body
			if states(top, q.Current) && !slices.ContainsFunc(q.Stale, func(v string) bool { return states(top, v) }) {
				score.MemoryCurrent++
			} else if slices.ContainsFunc(q.Stale, func(v string) bool { return states(top, v) }) {
				score.MemoryStale++
			}
		}
		facts, err := search.Facts(ctx, q.Query)
		if err != nil {
			t.Fatal(err)
		}
		if (q.Current == "" && len(facts) == 0) || (len(facts) > 0 && facts[0].ValidTo == nil && facts[0].Object == q.Current) {
			score.FactCurrent++
		} else {
			score.Wrong = append(score.Wrong, q.Query)
		}
		hits, err := search.Search(ctx, q.Query, 3)
		if err != nil {
			t.Fatal(err)
		}
		for _, hit := range hits {
			if slices.ContainsFunc(q.Stale, func(v string) bool { return states(hit.Record.Body, v) }) && !states(hit.Record.Body, q.Current) {
				score.StaleInTop3++
				if hit.Outdated != "" {
					score.StaleMarked++
				}
			}
		}
	}
	t.Logf("memory top-1 current %d/%d, stale %d; first fact current %d/%d (wrong %v); stale memories in top 3 marked %d/%d",
		score.MemoryCurrent, score.Queries, score.MemoryStale, score.FactCurrent, score.Queries, score.Wrong, score.StaleMarked, score.StaleInTop3)
	if path := os.Getenv("MINILOOP_ASSISTANT_FACTS_REPORT"); path != "" {
		data, _ := json.MarshalIndent(score, "", "  ")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if score.MemoryCurrent != 5 || score.MemoryStale != 5 || score.FactCurrent != score.Queries || score.StaleInTop3 != 8 || score.StaleMarked != score.StaleInTop3 {
		t.Fatalf("pinned scores changed: %+v", score)
	}
}

// A contact marked 经理 who is no longer the manager shows as 前经理.
func TestProfileContactsFollowTheCurrentManager(t *testing.T) {
	contacts := []Contact{{Address: ManagerAddress, Name: "Alice", Relation: "经理", Interactions: 2}}
	for _, c := range []struct {
		manager *Fact
		want    string
	}{
		{&Fact{Object: "Alice", Address: ManagerAddress}, "Alice（经理） 往来 2 次"},
		{&Fact{Object: "Carol"}, "Alice（前经理） 往来 2 次"},
		{nil, "Alice（经理） 往来 2 次"},
	} {
		if got := profileFromContacts(contacts, c.manager).Value; got != c.want {
			t.Errorf("manager %+v: %q, want %q", c.manager, got, c.want)
		}
	}
}
