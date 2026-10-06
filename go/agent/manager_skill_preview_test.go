package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/skills"
	"github.com/luoyjx/mini-loop/go/userresources"
)

func skillPreviewManager(t *testing.T, provider Provider) (*SessionManager, *ManagedSession) {
	cfg := managerTestConfig(t.TempDir(), provider)
	cfg.Services.UserResources = ownerResourceResolver(t, t.TempDir(), skills.EmptyCatalog())
	manager := makeManager(t, cfg)
	return manager, createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
}
func requireSkillPolicy(t *testing.T, err error, code PersonalSkillCode, status int) {
	t.Helper()
	var failure *PersonalSkillError
	if !errors.As(err, &failure) || failure.Code() != code || failure.StatusCode() != status {
		t.Fatal(err, code, status)
	}
}
func TestManagerSkillPreviewAuthorityAndLedger(t *testing.T) {
	provider := &nativePreviewProvider{responses: []string{previewCandidate}}
	manager, session := skillPreviewManager(t, provider)
	ctx := context.Background()
	for _, owner := range []OwnerID{"foreign", "", "anonymous"} {
		_, err := manager.PreviewPersonalSkill(ctx, owner, session.ID(), "recipe", "")
		requireSkillPolicy(t, err, SkillSessionNotFound, 404)
	}
	anonymous := createManaged(t, manager, CreateSessionRequest{Owner: "anonymous"})
	_, err := manager.PreviewPersonalSkill(ctx, "anonymous", anonymous.ID(), "recipe", "")
	requireSkillPolicy(t, err, SkillAuthenticatedOwnerRequired, 403)
	session.core.messages = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("restored evidence")}}
	session.core.publishLive()
	_, err = manager.PreviewPersonalSkill(ctx, "alice", session.ID(), "recipe", "")
	var refusal *userresources.DraftError
	if !errors.As(err, &refusal) || refusal.Code() != userresources.DraftEmptyTranscript || len(provider.requests) != 0 {
		t.Fatal("empty ledger fell back", err)
	}
	session.skillCapture.Record("human evidence", "assistant evidence", nil, nil)
	before := session.Info()
	history := session.Messages()
	meter := session.core.meter.Snapshot()
	preview, err := manager.PreviewPersonalSkill(ctx, "alice", session.ID(), "recipe", "")
	if err != nil || preview.Coverage != userresources.AuthenticatedTurns {
		t.Fatal(preview, err)
	}
	if !reflect.DeepEqual(history, session.Messages()) || !reflect.DeepEqual(meter, session.core.meter.Snapshot()) || session.Info().RunCount != before.RunCount || session.Info().Busy {
		t.Fatal("preview became a live turn")
	}
	if _, err = manager.skillDrafts.Peek(userresources.DraftQuery{ID: preview.ID, Owner: "alice", Session: userresources.DraftSessionID(session.ID())}); err != nil {
		t.Fatal(err)
	}
	disabled := makeManager(t, managerTestConfig(t.TempDir(), &FakeProvider{}))
	target := createManaged(t, disabled, CreateSessionRequest{Owner: "alice"})
	_, err = disabled.PreviewPersonalSkill(ctx, "alice", target.ID(), "recipe", "")
	requireSkillPolicy(t, err, SkillDisabled, 404)
}

type blockedSkillProvider struct{ entered, returned chan struct{} }

func (p blockedSkillProvider) Complete(ctx context.Context, _ protocol.ModelRequest) (protocol.ModelReply, error) {
	close(p.entered)
	<-ctx.Done()
	close(p.returned)
	return protocol.ModelReply{}, ctx.Err()
}
func TestManagerSkillPreviewDeleteAndStopJoin(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(map[bool]string{false: "delete", true: "stop"}[stop], func(t *testing.T) {
			provider := blockedSkillProvider{make(chan struct{}), make(chan struct{})}
			manager, session := skillPreviewManager(t, provider)
			session.skillCapture.Record("human evidence", "assistant evidence", nil, nil)
			result := make(chan error, 1)
			go func() {
				_, err := manager.PreviewPersonalSkill(context.Background(), "alice", session.ID(), "recipe", "")
				result <- err
			}()
			select {
			case <-provider.entered:
			case <-time.After(time.Second):
				t.Fatal("preview did not enter provider")
			}
			if session.Info().Busy {
				t.Fatal("preview changed turn status")
			}
			if changed, err := session.Cancel(context.Background(), "operator"); changed || err != nil {
				t.Fatal("turn cancel targeted preview", changed, err)
			}
			if _, err := manager.Fork(context.Background(), "alice", session.ID()); !errors.Is(err, ErrForkBusy) {
				t.Fatal("fork bypassed preview admission", err)
			}
			if stop {
				if err := manager.Stop(context.Background()); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := manager.Delete("alice", session.ID(), DeleteSessionOptions{}); err != nil {
					t.Fatal(err)
				}
				if err := manager.WaitCleanup(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-provider.returned:
			default:
				t.Fatal("cleanup did not join preview")
			}
			if err := <-result; !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if session.hasSkillPreview() {
				t.Fatal("preview lifetime retained")
			}
		})
	}
}
func TestManagerSkillPreviewQueuedDeleteCannotStart(t *testing.T) {
	provider := &nativePreviewProvider{responses: []string{previewCandidate}}
	manager, session := skillPreviewManager(t, provider)
	<-session.admission
	result := make(chan error, 1)
	waiter := &previewWaitContext{Context: context.Background(), entered: make(chan struct{})}
	go func() {
		_, err := manager.PreviewPersonalSkill(waiter, "alice", session.ID(), "recipe", "")
		result <- err
	}()
	<-waiter.entered
	if _, err := manager.Delete("alice", session.ID(), DeleteSessionOptions{}); err != nil {
		t.Fatal(err)
	}
	session.admission <- struct{}{}
	select {
	case err := <-result:
		requireSkillPolicy(t, err, SkillSessionNotFound, 404)
	case <-time.After(time.Second):
		t.Fatal("queued preview retained admission")
	}
	if len(provider.requests) != 0 {
		t.Fatal("deleted preview reached provider")
	}
}

func TestManagerSkillPreviewMatchesActualPython(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-manager-skill-preview.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name     string
			Calls    []protocol.RequestPurpose
			Expected struct {
				Error    string
				Status   int
				Coverage userresources.DraftCoverage
			}
		}
	}
	if err = json.Unmarshal(raw, &fixture); err != nil || len(fixture.Cases) != 8 {
		t.Fatal(err, len(fixture.Cases))
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			provider := &nativePreviewProvider{responses: []string{previewCandidate}}
			cfg := managerTestConfig(t.TempDir(), provider)
			if row.Name != "disabled" {
				cfg.Services.UserResources = ownerResourceResolver(t, t.TempDir(), skills.EmptyCatalog())
			}
			store := newRuntimeStateStore()
			cfg.Services.StateStore = store
			manager := makeManager(t, cfg)
			owner := OwnerID("alice")
			mode := ModeInteractive
			if row.Name == "anonymous" {
				owner = "anonymous"
			}
			if row.Name == "readonly" {
				mode = ModeReadonly
			}
			session := createManaged(t, manager, CreateSessionRequest{Owner: owner, PermissionMode: mode})
			session.core.messages = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("legacy evidence")}}
			session.core.publishLive()
			if row.Name != "empty" {
				session.skillCapture.Record("human evidence", "assistant evidence", nil, nil)
			}
			if row.Name == "closed" {
				session.StopAccepting("closed")
			}
			if row.Name == "lease-lost" {
				store.mu.Lock()
				store.holders[session.ID()] = "foreign-process"
				store.mu.Unlock()
			}
			if row.Name == "foreign" {
				owner = "foreign"
			}
			preview, err := manager.PreviewPersonalSkill(context.Background(), owner, session.ID(), "recipe", "")
			code := ""
			status := 0
			if err != nil {
				var policy *PersonalSkillError
				var draft *userresources.DraftError
				if errors.As(err, &policy) {
					code = string(policy.Code())
					status = policy.StatusCode()
				} else if errors.As(err, &draft) {
					code = string(draft.Code())
					status = draft.StatusCode()
				} else {
					t.Fatal(err)
				}
			}
			if code != row.Expected.Error || status != row.Expected.Status || preview.Coverage != row.Expected.Coverage {
				t.Fatal(code, status, preview, row.Expected)
			}
			calls := []protocol.RequestPurpose{}
			for _, request := range provider.requests {
				calls = append(calls, request.Purpose)
			}
			if !reflect.DeepEqual(calls, row.Calls) {
				t.Fatal(calls, row.Calls)
			}
		})
	}
}
func TestManagerSkillPreviewMidRequestLeaseLoss(t *testing.T) {
	cfg := managerTestConfig(t.TempDir(), &nativePreviewProvider{responses: []string{previewCandidate}})
	cfg.Services.UserResources = ownerResourceResolver(t, t.TempDir(), skills.EmptyCatalog())
	store := newRuntimeStateStore()
	cfg.Services.StateStore = store
	manager := makeManager(t, cfg)
	session := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	session.skillCapture.Record("evidence", "result", nil, nil)
	session.core.messages = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("live evidence")}}
	session.core.publishLive()
	store.mu.Lock()
	store.renewReject = true
	store.mu.Unlock()
	_, err := manager.PreviewPersonalSkill(context.Background(), "alice", session.ID(), "recipe", "")
	requireSkillPolicy(t, err, SkillLeaseLost, 409)
	if session.hasSkillPreview() {
		t.Fatal("lost lease retained operation")
	}
}

type previewWaitContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (ctx *previewWaitContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.entered) })
	return ctx.Context.Done()
}

type previewLeaseStore struct {
	*runtimeStateStore
	block   bool
	entered chan struct{}
}

func (store *previewLeaseStore) AcquireLease(ctx context.Context, id SessionID, owner LeaseOwner, ttl time.Duration) (bool, error) {
	if !store.block {
		return store.runtimeStateStore.AcquireLease(ctx, id, owner, ttl)
	}
	close(store.entered)
	<-ctx.Done()
	return false, ctx.Err()
}
func TestManagerSkillPreviewStopCancelsLeaseAcquisition(t *testing.T) {
	cfg := managerTestConfig(t.TempDir(), &FakeProvider{})
	cfg.Services.UserResources = ownerResourceResolver(t, t.TempDir(), skills.EmptyCatalog())
	store := &previewLeaseStore{runtimeStateStore: newRuntimeStateStore(), entered: make(chan struct{})}
	cfg.Services.StateStore = store
	manager := makeManager(t, cfg)
	session := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	store.block = true
	result := make(chan error, 1)
	go func() {
		_, err := manager.PreviewPersonalSkill(context.Background(), "alice", session.ID(), "recipe", "")
		result <- err
	}()
	select {
	case <-store.entered:
	case <-time.After(time.Second):
		t.Fatal("lease acquisition not entered")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.Stop(ctx); err != nil {
		t.Fatal("lease waiter escaped lifecycle", err)
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestManagerSkillPreviewCallerCancellationIsReusable(t *testing.T) {
	provider := blockedSkillProvider{make(chan struct{}), make(chan struct{})}
	manager, session := skillPreviewManager(t, provider)
	session.skillCapture.Record("evidence", "result", nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := manager.PreviewPersonalSkill(ctx, "alice", session.ID(), "recipe", "")
		result <- err
	}()
	<-provider.entered
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	session.core.provider = &nativePreviewProvider{responses: []string{previewCandidate}}
	if _, err := manager.PreviewPersonalSkill(context.Background(), "alice", session.ID(), "recipe", ""); err != nil {
		t.Fatal("cancelled operation retained admission", err)
	}
}
