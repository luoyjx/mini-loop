package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type memoryExtractionProvider struct {
	output   string
	fault    bool
	requests []protocol.ModelRequest
}

func (p *memoryExtractionProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	p.requests = append(p.requests, request.Clone())
	if p.fault {
		return protocol.ModelReply{}, errors.New("extraction failed")
	}
	middle := len(p.output) / 2
	reply := fakeReply([]protocol.Block{protocol.NewTextBlock("prefix " + p.output[:middle]), protocol.NewTextBlock(p.output[middle:] + " suffix")}, protocol.StopEndTurn)
	reply.Model, reply.Usage.InputTokens, reply.Usage.OutputTokens = "served-memory", 777, 3
	return reply, nil
}

func TestMemoryExtractionMatchesActualPythonSource(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-memory-extraction.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Reply      string
			Fault            bool
			Messages         []protocol.Message
			Calls            []protocol.ModelRequest
			Count            int
			Records, Foreign []memory.Record
			Purposes         []protocol.RequestPurpose
		}
	}
	if err = json.Unmarshal(raw, &fixture); err != nil || len(fixture.Cases) != 13 {
		t.Fatal(err, len(fixture.Cases))
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			ctx := context.Background()
			rawStore, err := memory.NewStore(ctx, t.TempDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			store, _ := memory.Bind(rawStore, "owner")
			foreign, _ := memory.Bind(rawStore, "foreign")
			_, err = foreign.Write(ctx, memory.Input{Name: "foreign", Type: memory.Project, Description: "foreign description", Body: "foreign secret"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = store.Write(ctx, memory.Input{Name: "existing", Type: memory.Project, Description: "known", Body: "seed", Origin: memory.Explicit})
			if err != nil {
				t.Fatal(err)
			}
			provider := &memoryExtractionProvider{output: row.Reply, fault: row.Fault}
			cfg := runtimeConfig(t.TempDir(), provider)
			cfg.Memory, cfg.MemoryTools, cfg.CachePolicy, cfg.Recovery = store, true, NullCachePolicy{}, DirectRecovery{}
			session, err := NewRuntimeSession(cfg)
			if err != nil {
				t.Fatal(err)
			}
			session.messages = append([]protocol.Message(nil), row.Messages...)
			before := session.meter.Snapshot()
			count, err := session.extractMemories(ctx)
			if err != nil || count != row.Count {
				t.Fatal(count, err, row.Count)
			}
			if !reflect.DeepEqual(session.messages, row.Messages) {
				t.Fatal("extraction replaced the live conversation")
			}
			if !reflect.DeepEqual(before, session.meter.Snapshot()) {
				t.Fatal("extraction observed the live token meter")
			}
			records, err := store.List(ctx)
			if err != nil || !reflect.DeepEqual(records, row.Records) {
				t.Fatalf("records differ: %#v want %#v err %v", records, row.Records, err)
			}
			protected, err := foreign.List(ctx)
			if err != nil || !reflect.DeepEqual(protected, row.Foreign) {
				t.Fatal("extraction changed foreign memories", protected, err)
			}
			if len(provider.requests) != 1 || len(row.Calls) != 1 {
				t.Fatal("request count", len(provider.requests))
			}
			request, expected := provider.requests[0], row.Calls[0]
			if request.Purpose != protocol.PurposeAgentTurn || request.Model != expected.Model || request.MaxTokens != expected.MaxTokens || !reflect.DeepEqual(request.Messages, expected.Messages) || request.System != nil || len(request.Tools) != 0 {
				t.Fatalf("request differs: %#v want %#v", request, expected)
			}
			var purposes []protocol.RequestPurpose
			for _, event := range session.Events() {
				if v, ok := event.Event.ModelStart(); ok {
					purposes = append(purposes, v.Purpose)
				}
			}
			if !reflect.DeepEqual(purposes, row.Purposes) {
				t.Fatal(purposes, row.Purposes)
			}
		})
	}
}

func TestMemoryExtractionCancellationAndClosedFields(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cfg := runtimeConfig(t.TempDir(), &memoryExtractionProvider{output: "[]"})
	session, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err = session.extractMemories(ctx); err != context.Canceled {
		t.Fatal(err)
	}
	for _, text := range []string{`null`, `7`, `{}`, `{"name":null}`, `{"name":"x","body":7}`, `{"name":"x","description":null}`} {
		if _, err := extractedMemoryInput(json.RawMessage(text)); err == nil {
			t.Fatal("unrepresentable entry accepted", text)
		}
	}
}

type memoryHistoryRecovery struct{ input RecoveryInput }

func (r *memoryHistoryRecovery) Recover(ctx context.Context, in RecoveryInput, services RecoveryServices) (protocol.ModelReply, error) {
	r.input = in
	return services.Call(ctx, in.Request)
}
func TestMemoryExtractionRecoveryHasNoLiveHistory(t *testing.T) {
	ctx := context.Background()
	store, err := memory.NewStore(ctx, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	bound, _ := memory.Bind(store, "owner")
	recovery := &memoryHistoryRecovery{}
	cfg := runtimeConfig(t.TempDir(), &memoryExtractionProvider{output: "[]"})
	cfg.Memory, cfg.Recovery = bound, recovery
	session, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	session.messages = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("live")}}
	if _, err := session.extractMemories(ctx); err != nil {
		t.Fatal(err)
	}
	if recovery.input.LiveHistory != nil {
		t.Fatal("side extraction granted live history to recovery")
	}
}

func TestMemoryExtractionAuthorityFailuresRemainErrors(t *testing.T) {
	for _, failure := range []error{ErrStateTranscript, ErrSessionLeaseLost} {
		t.Run(failure.Error(), func(t *testing.T) {
			ctx := context.Background()
			store, err := memory.NewStore(ctx, t.TempDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			bound, _ := memory.Bind(store, "owner")
			cfg := runtimeConfig(t.TempDir(), memoryFailureProvider{failure})
			cfg.Memory, cfg.Recovery = bound, DirectRecovery{}
			session, err := NewRuntimeSession(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := session.extractMemories(ctx); !errors.Is(err, failure) {
				t.Fatal("authority fault swallowed", err)
			}
			lost, cancel := context.WithCancelCause(ctx)
			cancel(ErrSessionLeaseLost)
			if _, err := session.extractMemories(lost); !errors.Is(err, ErrSessionLeaseLost) {
				t.Fatal("lease-loss cause swallowed", err)
			}
		})
	}
}

type memoryFailureProvider struct{ failure error }

func (p memoryFailureProvider) Complete(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
	return protocol.ModelReply{}, p.failure
}

func TestMemoryExtractionCancellationDuringRequestCannotWrite(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store, err := memory.NewStore(ctx, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	bound, _ := memory.Bind(store, "owner")
	cfg := runtimeConfig(t.TempDir(), cancelExtractionProvider{cancel})
	cfg.Memory, cfg.Recovery = bound, DirectRecovery{}
	session, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.extractMemories(ctx); err != context.Canceled {
		t.Fatal("request cancellation swallowed", err)
	}
	records, err := bound.List(context.Background())
	if err != nil || len(records) != 0 {
		t.Fatal("cancelled request persisted facts", records, err)
	}
}

type cancelExtractionProvider struct{ cancel context.CancelFunc }

func (p cancelExtractionProvider) Complete(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
	p.cancel()
	return fakeReply([]protocol.Block{protocol.NewTextBlock(`[{"name":"must-not-write","body":"fact"}]`)}, protocol.StopEndTurn), nil
}
