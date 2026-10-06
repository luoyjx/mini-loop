package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/userresources"
)

const previewCandidate = `{"schema":"mini-loop.personal-skill-draft/v1","decision":"create","description":"recipe","body":"procedure","evidence_indexes":[0]}`

type nativePreviewProvider struct {
	responses   []string
	fault, tool bool
	requests    []protocol.ModelRequest
}

func (p *nativePreviewProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	p.requests = append(p.requests, request.Clone())
	if p.fault {
		return protocol.ModelReply{}, errors.New("private preview provider failure")
	}
	text := p.responses[len(p.requests)-1]
	middle := len(text) / 2
	if index := strings.Index(text, "procedure"); index >= 0 {
		middle = index + 4
	}
	blocks := []protocol.Block{protocol.NewThinkingBlock("private ignored thinking", "signature"), protocol.NewTextBlock(text[:middle]), protocol.NewTextBlock(text[middle:])}
	reason := protocol.StopEndTurn
	if p.tool {
		blocks = append(blocks, protocol.NewBashUse("toolu_ignored", "touch NEVER"))
		reason = protocol.StopToolUse
	}
	reply := fakeReply(blocks, reason)
	reply.Model = "served-preview"
	reply.Usage.InputTokens = 777
	reply.Usage.OutputTokens = 3
	return reply, nil
}

func TestNativeSkillPreviewMatchesActualPythonSource(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-native-skill-preview.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name        string
			Fault, Tool bool
			Responses   []string
			Messages    []protocol.Message
			Calls       []protocol.ModelRequest
			Purposes    []protocol.RequestPurpose
			Ends        []ModelStatus
			Expected    struct {
				Error   userresources.DraftCode
				Status  int
				Preview *userresources.DraftPreview
			}
		}
	}
	if err = json.Unmarshal(raw, &fixture); err != nil || len(fixture.Cases) != 6 {
		t.Fatal(err, len(fixture.Cases))
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			provider := &nativePreviewProvider{responses: row.Responses, fault: row.Fault, tool: row.Tool}
			cfg := runtimeConfig(t.TempDir(), provider)
			cfg.CachePolicy = NullCachePolicy{}
			session, err := NewRuntimeSession(cfg)
			if err != nil {
				t.Fatal(err)
			}
			session.messages = append([]protocol.Message(nil), row.Messages...)
			before := session.meter.Snapshot()
			draft, err := session.PreviewPersonalSkill(context.Background(), "recipe", "")
			if row.Expected.Error != "" {
				var failure *userresources.DraftError
				if !errors.As(err, &failure) || failure.Code() != row.Expected.Error || failure.StatusCode() != row.Expected.Status {
					t.Fatalf("failure %v want %s/%d", err, row.Expected.Error, row.Expected.Status)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				got := draft.Preview()
				got.ID = ""
				got.CreatedAt = 0
				got.ExpiresAt = 0
				if !reflect.DeepEqual(got, *row.Expected.Preview) {
					t.Fatalf("preview %#v want %#v", got, *row.Expected.Preview)
				}
			}
			if !reflect.DeepEqual(session.messages, row.Messages) || !reflect.DeepEqual(before, session.meter.Snapshot()) {
				t.Fatal("preview mutated live history/meter")
			}
			if len(provider.requests) != len(row.Calls) {
				t.Fatal("calls", len(provider.requests), len(row.Calls))
			}
			for i, got := range provider.requests {
				want := row.Calls[i]
				if got.Purpose != protocol.PurposePersonalSkillPreview || got.Model != want.Model || got.MaxTokens != want.MaxTokens || !reflect.DeepEqual(got.System, want.System) || !reflect.DeepEqual(got.Messages, want.Messages) || got.Tools == nil || len(got.Tools) != 0 {
					t.Fatalf("request %#v want %#v", got, want)
				}
			}
			var purposes []protocol.RequestPurpose
			var ends []ModelStatus
			for _, event := range session.Events() {
				if event.Event.kind == EventToolUse || event.Event.kind == EventToolResult {
					t.Fatal("preview executed a returned tool")
				}
				if value, ok := event.Event.ModelStart(); ok {
					purposes = append(purposes, value.Purpose)
				}
				if value, ok := event.Event.ModelEnd(); ok {
					ends = append(ends, value.Status)
				}
			}
			if !reflect.DeepEqual(purposes, row.Purposes) || !reflect.DeepEqual(ends, row.Ends) {
				t.Fatal("events", purposes, ends, row.Purposes, row.Ends)
			}
			if _, err = os.Stat(cfg.Workspace + "/NEVER"); !os.IsNotExist(err) {
				t.Fatal("returned tool executed", err)
			}
		})
	}
}

func TestNativeSkillPreviewRecoveryAndEmptyLedger(t *testing.T) {
	provider := &nativePreviewProvider{responses: []string{previewCandidate}}
	recovery := &memoryHistoryRecovery{}
	cfg := runtimeConfig(t.TempDir(), provider)
	cfg.Recovery = recovery
	session, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	session.messages = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("live evidence")}}
	if _, err = session.PreviewPersonalSkill(context.Background(), "recipe", ""); err != nil {
		t.Fatal(err)
	}
	if recovery.input.LiveHistory != nil {
		t.Fatal("preview granted live history to recovery")
	}
	if provider.requests[0].Cache.System == nil {
		t.Fatal("preview bypassed configured cache policy")
	}
	session.mu.Lock()
	_, err = session.previewPersonalSkillLocked(context.Background(), "recipe", "", &userresources.CaptureLedger{})
	session.mu.Unlock()
	var failure *userresources.DraftError
	if !errors.As(err, &failure) || failure.Code() != userresources.DraftEmptyTranscript || len(provider.requests) != 1 {
		t.Fatal("empty ledger fell back to history", err, len(provider.requests))
	}
}

func TestNativeSkillPreviewCancelledAdmissionIsReusable(t *testing.T) {
	provider := &nativePreviewProvider{responses: []string{previewCandidate}}
	session, err := NewRuntimeSession(runtimeConfig(t.TempDir(), provider))
	if err != nil {
		t.Fatal(err)
	}
	session.messages = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("evidence")}}
	<-session.turn
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = session.PreviewPersonalSkill(ctx, "recipe", ""); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(provider.requests) != 0 {
		t.Fatal("cancelled waiter reached model")
	}
	session.turn <- struct{}{}
	if _, err = session.PreviewPersonalSkill(context.Background(), "recipe", ""); err != nil {
		t.Fatal("admission leaked", err)
	}
}

func TestNativeSkillPreviewLimiterCancellation(t *testing.T) {
	pool, _ := NewConcurrencyLimiter(1)
	lease, _ := pool.Acquire(context.Background())
	provider := &nativePreviewProvider{responses: []string{previewCandidate}}
	cfg := runtimeConfig(t.TempDir(), provider)
	cfg.ModelLimiter = pool
	session, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	session.messages = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("evidence")}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err = session.PreviewPersonalSkill(ctx, "recipe", ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if len(provider.requests) != 0 {
		t.Fatal("preview bypassed shared limiter")
	}
	lease.Release()
	if _, err = session.PreviewPersonalSkill(context.Background(), "recipe", ""); err != nil {
		t.Fatal("limiter/admission leaked", err)
	}
}

type cancelPreviewProvider struct{ cancel context.CancelFunc }

func (p cancelPreviewProvider) Complete(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
	p.cancel()
	return fakeReply([]protocol.Block{protocol.NewTextBlock(previewCandidate)}, protocol.StopEndTurn), nil
}
func TestNativeSkillPreviewCancelledReplyCannotRetainDraft(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := runtimeConfig(t.TempDir(), cancelPreviewProvider{cancel})
	session, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	session.messages = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("evidence")}}
	draft, err := session.PreviewPersonalSkill(ctx, "recipe", "")
	if !errors.Is(err, context.Canceled) || draft.Preview().ID != "" {
		t.Fatal(draft, err)
	}
	provider := &nativePreviewProvider{responses: []string{previewCandidate}}
	session.provider = provider
	if _, err = session.PreviewPersonalSkill(context.Background(), "recipe", ""); err != nil {
		t.Fatal("cancelled preview poisoned session", err)
	}
}
