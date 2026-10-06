package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/userresources"
)

func TestManagerSkillDraftPoolMatchesActualPython(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-manager-skill-drafts.json")
	if err != nil {
		t.Fatal(err)
	}
	type frame struct {
		Name   string
		Shared bool
	}
	var fixture struct {
		Frames                []frame
		RestartDiscardsDrafts bool `json:"restart_discards_drafts"`
		ResourcesDisabled     bool `json:"resources_disabled"`
	}
	if err = json.Unmarshal(raw, &fixture); err != nil || !fixture.RestartDiscardsDrafts || !fixture.ResourcesDisabled {
		t.Fatal(err, fixture)
	}
	ctx := context.Background()
	cfg := managerTestConfig(t.TempDir(), &FakeProvider{})
	cfg.Services.StateStore = newRuntimeStateStore()
	manager := makeManager(t, cfg)
	var got []frame
	capture := func(name string, fleet *SessionManager, session *ManagedSession) {
		got = append(got, frame{name, fleet.skillDrafts != nil && session.core.skillDrafts == fleet.skillDrafts})
	}
	alice := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	capture("alice", manager, alice)
	capture("bob", manager, createManaged(t, manager, CreateSessionRequest{Owner: "bob"}))
	capture("anonymous", manager, createManaged(t, manager, CreateSessionRequest{Owner: "anonymous"}))
	fork, err := manager.Fork(ctx, "alice", alice.ID())
	if err != nil {
		t.Fatal(err)
	}
	capture("fork", manager, fork)
	draft, err := manager.skillDrafts.Add(userresources.DraftInput{Owner: "alice", Session: userresources.DraftSessionID(alice.ID()), SkillFields: userresources.SkillFields{Name: "recipe", Description: "recipe", Body: "procedure"}, EvidenceIndexes: []int{0}, Coverage: userresources.CurrentEpoch})
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	for _, scheduled := range []bool{false, true} {
		fleet := makeManager(t, cfg)
		if fleet.skillDrafts == manager.skillDrafts {
			t.Fatal("manager restart reused process-local pool")
		}
		if scheduled {
			row, err := fleet.RestoreScheduledSession(ctx, alice.ID())
			if err != nil {
				t.Fatal(err)
			}
			capture("scheduled-alice", fleet, row)
			row, err = fleet.RestoreScheduledSession(ctx, "missing")
			if err != nil {
				t.Fatal(err)
			}
			capture("scheduled-missing", fleet, row)
		} else {
			rows, err := fleet.RestoreSessions(ctx)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, row := range rows {
				if row.ID() == alice.ID() {
					capture("restored-alice", fleet, row)
					found = true
				}
			}
			if !found {
				t.Fatal("restored session missing")
			}
		}
		_, err = fleet.skillDrafts.Peek(userresources.DraftQuery{ID: draft.Preview().ID, Owner: "alice", Session: userresources.DraftSessionID(alice.ID())})
		var failure *userresources.DraftError
		if !errors.As(err, &failure) || failure.Code() != userresources.DraftNotFound {
			t.Fatal("draft survived restart", err)
		}
		if err = fleet.Stop(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(got, fixture.Frames) {
		t.Fatal(got, fixture.Frames)
	}
}

func TestManagerSkillPreviewRetainsInFleetPool(t *testing.T) {
	provider := &nativePreviewProvider{responses: []string{previewCandidate, previewCandidate}}
	manager := makeManager(t, managerTestConfig(t.TempDir(), provider))
	first := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	second := createManaged(t, manager, CreateSessionRequest{Owner: "bob"})
	for _, session := range []*ManagedSession{first, second} {
		session.core.messages = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("evidence")}}
		// Internal core seam only: no managed preview entrypoint/authority is exposed.
		draft, err := session.core.PreviewPersonalSkill(context.Background(), "recipe", "")
		if err != nil {
			t.Fatal(err)
		}
		query := userresources.DraftQuery{ID: draft.Preview().ID, Owner: userresources.OwnerID(session.Owner()), Session: userresources.DraftSessionID(session.ID())}
		if _, err = manager.skillDrafts.Peek(query); err != nil {
			t.Fatal("preview used a private pool", err)
		}
		query.Owner = "foreign"
		var failure *userresources.DraftError
		if _, err = manager.skillDrafts.Peek(query); !errors.As(err, &failure) || failure.Code() != userresources.DraftNotFound {
			t.Fatal("foreign authority found draft", err)
		}
	}
}

func TestManagerSkillDraftGlobalCapacityPreservesForeignOwners(t *testing.T) {
	manager := makeManager(t, managerTestConfig(t.TempDir(), &FakeProvider{}))
	var queries []userresources.DraftQuery
	for ownerIndex := 0; ownerIndex < 4; ownerIndex++ {
		owner := OwnerID(fmt.Sprintf("owner-%d", ownerIndex))
		for sessionIndex := 0; sessionIndex < 4; sessionIndex++ {
			session := createManaged(t, manager, CreateSessionRequest{Owner: owner})
			for item := 0; item < 4; item++ {
				draft, err := session.core.skillDrafts.Add(userresources.DraftInput{Owner: userresources.OwnerID(owner), Session: userresources.DraftSessionID(session.ID()), SkillFields: userresources.SkillFields{Name: "recipe", Description: "recipe", Body: "procedure"}, EvidenceIndexes: []int{0}, Coverage: userresources.CurrentEpoch})
				if err != nil {
					t.Fatal(err)
				}
				queries = append(queries, userresources.DraftQuery{ID: draft.Preview().ID, Owner: userresources.OwnerID(owner), Session: userresources.DraftSessionID(session.ID())})
			}
		}
	}
	outsider := createManaged(t, manager, CreateSessionRequest{Owner: "outsider"})
	_, err := outsider.core.skillDrafts.Add(userresources.DraftInput{Owner: "outsider", Session: userresources.DraftSessionID(outsider.ID()), SkillFields: userresources.SkillFields{Name: "recipe", Description: "recipe", Body: "procedure"}, EvidenceIndexes: []int{0}, Coverage: userresources.CurrentEpoch})
	var failure *userresources.DraftError
	if !errors.As(err, &failure) || failure.Code() != userresources.DraftCapacity || failure.StatusCode() != 429 {
		t.Fatal("fleet global quota bypassed", err)
	}
	for _, query := range queries {
		if _, err = manager.skillDrafts.Peek(query); err != nil {
			t.Fatal("capacity refusal evicted foreign draft", err)
		}
	}
}
