package userresources

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDraftStoreMatchesActualPythonOperations(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-skill-drafts.json")
	if err != nil {
		t.Fatal(err)
	}
	type configRow struct {
		Name       string
		TTL        float64
		MaxItems   int  `json:"max_items"`
		MaxOwner   *int `json:"max_per_owner"`
		MaxSession int  `json:"max_per_session"`
		Start      float64
	}
	var fixture struct {
		Defaults configRow
		Cases    []struct {
			Config configRow
			Steps  []struct {
				Action  string
				Ref     DraftID
				Owner   OwnerID
				Session DraftSessionID
				Input   struct {
					SkillFields
					Evidence  []int `json:"evidence_indexes"`
					Coverage  DraftCoverage
					Omitted   int
					Compacted bool `json:"compacted_history_excluded"`
				}
				Preview   *DraftPreview
				Discarded *bool
				Error     *struct {
					Code    DraftCode
					Status  int
					Message string
				}
			}
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || len(fixture.Cases) != 2 {
		t.Fatal(err)
	}
	defaults, err := NewDraftStore(DefaultDraftStoreConfig())
	if err != nil || defaults.ttl.Seconds() != fixture.Defaults.TTL || defaults.maxItems != fixture.Defaults.MaxItems || defaults.maxOwner != *fixture.Defaults.MaxOwner || defaults.maxSession != fixture.Defaults.MaxSession {
		t.Fatal("defaults differ", err)
	}
	count := 0
	for _, recipe := range fixture.Cases {
		t.Run(recipe.Config.Name, func(t *testing.T) {
			now := recipe.Config.Start
			store, err := NewDraftStore(DraftStoreConfig{TTL: time.Duration(recipe.Config.TTL * float64(time.Second)), MaxItems: recipe.Config.MaxItems, MaxPerOwner: recipe.Config.MaxOwner, MaxPerSession: recipe.Config.MaxSession, Clock: func() time.Time { return time.Unix(0, int64(now*1e9)) }})
			if err != nil {
				t.Fatal(err)
			}
			handles := make(map[DraftID]Draft)
			for i, step := range recipe.Steps {
				count++
				var draft Draft
				var err error
				query := DraftQuery{ID: step.Ref, Owner: step.Owner, Session: step.Session}
				if saved, ok := handles[step.Ref]; ok {
					query.ID = saved.Preview().ID
				}
				switch step.Action {
				case "add", "invalid-name", "invalid-coverage", "negative-omitted":
					draft, err = store.Add(DraftInput{Owner: step.Owner, Session: step.Session, SkillFields: step.Input.SkillFields, EvidenceIndexes: step.Input.Evidence, Coverage: step.Input.Coverage, Omitted: step.Input.Omitted, CompactedHistoryExcluded: step.Input.Compacted})
					if err == nil {
						handles[step.Ref] = draft
					}
				case "get":
					draft, err = store.Get(query)
				case "peek":
					draft, err = store.Peek(query)
				case "wrong-digest":
					bad := DraftDigest(strings.Repeat("0", 64))
					query.Digest = &bad
					draft, err = store.Get(query)
				case "consume":
					draft, err = store.Consume(query, handles[step.Ref].Preview().Digest)
				case "wrong-consume":
					draft, err = store.Consume(query, DraftDigest(strings.Repeat("0", 64)))
				case "advance":
					now += recipe.Config.TTL
				case "discard", "discard-clone":
					handle := handles[step.Ref]
					if step.Action == "discard-clone" {
						copy := *handle.record
						handle = Draft{&copy}
					}
					if step.Discarded == nil || store.DiscardCommitted(handle) != *step.Discarded {
						t.Fatal(i, "identity cleanup differs")
					}
				default:
					t.Fatal("unknown action", step.Action)
				}
				if step.Error != nil {
					var named *DraftError
					if !errors.As(err, &named) || named.Code() != step.Error.Code || named.StatusCode() != step.Error.Status || named.Error() != step.Error.Message {
						t.Fatal(i, step.Action, "error differs", err, step.Error)
					}
					continue
				}
				if err != nil {
					t.Fatal(i, err)
				}
				if step.Preview != nil {
					public := draft.Preview()
					if len(public.ID) != 32 || public.ID[12] != '4' || !strings.ContainsRune("89ab", rune(public.ID[16])) {
						t.Fatal("not UUID4 hex", public.ID)
					}
					public.ID = step.Ref
					if !reflect.DeepEqual(public, *step.Preview) {
						t.Fatal(i, "preview differs", public, *step.Preview)
					}
				}
			}
		})
	}
	if count != 40 {
		t.Fatal("incomplete recipe", count)
	}
}

func TestDraftHandlesDetachEvidenceAndHideAuthority(t *testing.T) {
	store, err := NewDraftStore(DefaultDraftStoreConfig())
	if err != nil {
		t.Fatal(err)
	}
	input := DraftInput{Owner: "private-owner", Session: "private-session", SkillFields: SkillFields{"safe-name", "Safe", "Reviewed body"}, EvidenceIndexes: []int{1}, Coverage: CurrentEpoch}
	draft, err := store.Add(input)
	if err != nil {
		t.Fatal(err)
	}
	input.EvidenceIndexes[0] = 99
	public := draft.Preview()
	public.EvidenceIndexes[0] = 98
	public.Body = "changed"
	if current := draft.Preview(); current.EvidenceIndexes[0] != 1 || current.Body != "Reviewed body" {
		t.Fatal("mutable preview escaped", current)
	}
	raw, err := json.Marshal(draft.Preview())
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"private-owner", "private-session", "\"owner\"", "\"session_id\""} {
		if strings.Contains(string(raw), private) {
			t.Fatal("authority exposed", string(raw))
		}
	}
	if raw, err := json.Marshal(draft); err != nil || string(raw) != "{}" {
		t.Fatal("identity handle exposed private data", string(raw), err)
	}
	if store.DiscardCommitted(Draft{}) {
		t.Fatal("zero handle discarded a record")
	}
}

func TestDraftConcurrentConsumptionHasOneWinner(t *testing.T) {
	store, err := NewDraftStore(DefaultDraftStoreConfig())
	if err != nil {
		t.Fatal(err)
	}
	draft, err := store.Add(DraftInput{Owner: "alice", Session: "session", SkillFields: SkillFields{"safe-name", "Safe", "Reviewed body"}, Coverage: AuthenticatedTurns})
	if err != nil {
		t.Fatal(err)
	}
	public := draft.Preview()
	var winners atomic.Int32
	var wait sync.WaitGroup
	for i := 0; i < 32; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			d, err := store.Consume(DraftQuery{ID: public.ID, Owner: "alice", Session: "session"}, public.Digest)
			if err == nil {
				if d.record != draft.record {
					t.Error("identity changed")
				}
				winners.Add(1)
				return
			}
			var named *DraftError
			if !errors.As(err, &named) || named.Code() != DraftNotFound {
				t.Error(err)
			}
		}()
	}
	wait.Wait()
	if winners.Load() != 1 {
		t.Fatal("multiple consume winners", winners.Load())
	}
}

func TestDraftConstructorValidatesAndSnapshotsLimits(t *testing.T) {
	for _, change := range []func(*DraftStoreConfig){func(c *DraftStoreConfig) { c.TTL = 0 }, func(c *DraftStoreConfig) { c.MaxItems = 0 }, func(c *DraftStoreConfig) { v := 0; c.MaxPerOwner = &v }, func(c *DraftStoreConfig) { v := 65; c.MaxPerOwner = &v }, func(c *DraftStoreConfig) { c.MaxPerSession = 0 }, func(c *DraftStoreConfig) { c.MaxPerSession = 17 }, func(c *DraftStoreConfig) { c.MaxItems = 2 }} {
		cfg := DefaultDraftStoreConfig()
		change(&cfg)
		if _, err := NewDraftStore(cfg); err == nil {
			t.Fatal("invalid limits admitted", cfg)
		}
	}
	owner := 4
	cfg := DefaultDraftStoreConfig()
	cfg.MaxPerOwner = &owner
	store, err := NewDraftStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	owner = 1
	if store.maxOwner != 4 {
		t.Fatal("caller rebound owner limit")
	}
}
