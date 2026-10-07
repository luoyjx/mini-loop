package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/skills"
	"github.com/luoyjx/mini-loop/go/userresources"
)

func addCommitDraft(t *testing.T, manager *SessionManager, session *ManagedSession) userresources.DraftPreview {
	t.Helper()
	draft, err := manager.skillDrafts.Add(userresources.DraftInput{Owner: userresources.OwnerID(session.Owner()), Session: userresources.DraftSessionID(session.ID()), SkillFields: userresources.SkillFields{Name: "recipe", Description: "recipe", Body: "procedure"}, EvidenceIndexes: []int{0}, Coverage: userresources.AuthenticatedTurns})
	if err != nil {
		t.Fatal(err)
	}
	return draft.Preview()
}
func TestManagerSkillCommitMatchesActualPython(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-manager-skill-commit.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name     string
			Retained bool
			Expected struct {
				Error      PersonalSkillCode
				Status     int
				Idempotent bool
				Activation userresources.Activation
				Source     string
			}
		}
	}
	if err = json.Unmarshal(raw, &fixture); err != nil || len(fixture.Cases) != 6 {
		t.Fatal(err, len(fixture.Cases))
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			manager, session := skillPreviewManager(t, &FakeProvider{})
			resolver := manager.config.Services.UserResources
			ctx := context.Background()
			if row.Name == "readonly" {
				if _, err := session.ChangePermissionMode(ModeReadonly); err != nil {
					t.Fatal(err)
				}
			}
			if row.Name == "conflict" || row.Name == "idempotent" {
				body := "procedure"
				if row.Name == "conflict" {
					body = "different"
				}
				publishOwnerSkill(t, resolver, "alice", "recipe", "recipe", body)
			}
			draft := addCommitDraft(t, manager, session)
			owner := OwnerID("alice")
			digest := draft.Digest
			if row.Name == "foreign" {
				owner = "foreign"
			}
			if row.Name == "wrong-digest" {
				digest = "wrong"
			}
			receipt, err := manager.CommitPersonalSkill(ctx, owner, session.ID(), draft.ID, digest)
			code := PersonalSkillCode("")
			status := 0
			if err != nil {
				var policy *PersonalSkillError
				var failure *userresources.DraftError
				if errors.As(err, &policy) {
					code = policy.Code()
					status = policy.StatusCode()
				} else if errors.As(err, &failure) {
					code = PersonalSkillCode(failure.Code())
					status = failure.StatusCode()
				} else {
					t.Fatal(err)
				}
			}
			if code != row.Expected.Error || status != row.Expected.Status || receipt.Idempotent != row.Expected.Idempotent || receipt.Activation != row.Expected.Activation || string(receipt.Source) != row.Expected.Source {
				t.Fatal(receipt, err, row.Expected)
			}
			_, err = manager.skillDrafts.Peek(userresources.DraftQuery{ID: draft.ID, Owner: "alice", Session: userresources.DraftSessionID(session.ID())})
			if (err == nil) != row.Retained {
				t.Fatal("retention order", err, row.Retained)
			}
			if code == "" {
				if receipt.Digest != draft.Digest || receipt.DraftID != draft.ID || receipt.Session != session.ID() {
					t.Fatal(receipt)
				}
				// Existing immutable snapshots remain unchanged; a new session gets publication.
				if strings.Contains(session.core.skills.Descriptions(), "user:recipe") {
					t.Fatal("live resources changed")
				}
				fresh := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
				if !strings.Contains(fresh.core.skills.Descriptions(), "user:recipe") {
					t.Fatal("future snapshot missed publication", fresh.core.skills.Descriptions())
				}
				_, err = manager.CommitPersonalSkill(ctx, "alice", session.ID(), draft.ID, draft.Digest)
				var failure *userresources.DraftError
				if !errors.As(err, &failure) || failure.Code() != userresources.DraftNotFound {
					t.Fatal("committed draft reused", err)
				}
			}
		})
	}
}
func TestManagerSkillCommitScreensAfterPreviewAndKeepsDraft(t *testing.T) {
	registry := secrets.New(secrets.Config{})
	cfg := managerTestConfig(t.TempDir(), &FakeProvider{})
	resolver, err := userresources.NewResolver(context.Background(), t.TempDir(), skills.EmptyCatalog(), registry)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Services.UserResources = resolver
	manager := makeManager(t, cfg)
	session := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	draft := addCommitDraft(t, manager, session)
	registry.RegisterValue("LATER", "procedure")
	_, err = manager.CommitPersonalSkill(context.Background(), "alice", session.ID(), draft.ID, draft.Digest)
	requireSkillPolicy(t, err, PersonalSkillCode(userresources.SecretDetected), 422)
	if _, err = manager.skillDrafts.Peek(userresources.DraftQuery{ID: draft.ID, Owner: "alice", Session: userresources.DraftSessionID(session.ID())}); err != nil {
		t.Fatal("failed publication consumed draft", err)
	}
}

type commitGateMasker struct {
	block            bool
	entered, release chan struct{}
	once             sync.Once
}

func (masker *commitGateMasker) MaskText(text string) string {
	if masker.block {
		masker.once.Do(func() { close(masker.entered) })
		<-masker.release
	}
	return text
}
func (*commitGateMasker) Names() []secrets.Name { return nil }
func TestManagerSkillCommitExpiryDuringPublicationPreservesSuccess(t *testing.T) {
	gate := &commitGateMasker{entered: make(chan struct{}), release: make(chan struct{})}
	cfg := managerTestConfig(t.TempDir(), &FakeProvider{})
	resolver, err := userresources.NewResolver(context.Background(), t.TempDir(), skills.EmptyCatalog(), gate)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Services.UserResources = resolver
	manager := makeManager(t, cfg)
	session := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	var seconds atomic.Int64
	seconds.Store(100)
	config := userresources.DefaultDraftStoreConfig()
	config.Clock = func() time.Time { return time.Unix(seconds.Load(), 0) }
	manager.skillDrafts, err = userresources.NewDraftStore(config)
	if err != nil {
		t.Fatal(err)
	}
	draft := addCommitDraft(t, manager, session)
	gate.block = true
	result := make(chan error, 1)
	go func() {
		receipt, err := manager.CommitPersonalSkill(context.Background(), "alice", session.ID(), draft.ID, draft.Digest)
		if err == nil && receipt.DraftID != draft.ID {
			err = errors.New("wrong receipt")
		}
		result <- err
	}()
	select {
	case <-gate.entered:
	case <-time.After(time.Second):
		t.Fatal("publication not entered")
	}
	seconds.Store(2000)
	close(gate.release)
	if err := <-result; err != nil {
		t.Fatal("expiry changed committed publication to failure", err)
	}
	_, err = manager.skillDrafts.Peek(userresources.DraftQuery{ID: draft.ID, Owner: "alice", Session: userresources.DraftSessionID(session.ID())})
	var failure *userresources.DraftError
	if !errors.As(err, &failure) || failure.Code() != userresources.DraftNotFound {
		t.Fatal("exact committed identity not removed", err)
	}
}
