package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestMemoryConsolidationMatchesActualPythonSource(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-memory-consolidation.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Reply      string
			Fault            bool
			Seed, Count      int
			Calls            []protocol.ModelRequest
			Records, Foreign []memory.Record
			Purposes         []protocol.RequestPurpose
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || len(fixture.Cases) != 10 {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			ctx := context.Background()
			store, err := memory.NewStore(ctx, t.TempDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			bound, _ := memory.Bind(store, "owner")
			foreign, _ := memory.Bind(store, "foreign")
			if _, err := foreign.Write(ctx, memory.Input{Name: "foreign", Type: memory.Project, Description: "foreign description", Body: "foreign secret"}); err != nil {
				t.Fatal(err)
			}
			origins := []memory.Origin{memory.Explicit, memory.AutoExtracted, memory.Imported, memory.Consolidated}
			for i := 0; i < row.Seed; i++ {
				if _, err := bound.Write(ctx, memory.Input{Name: fmt.Sprintf("fact-%d", i), Type: memory.Project, Description: fmt.Sprintf("description-%d 中文", i), Body: fmt.Sprintf("body-%d café", i), Origin: origins[i%4]}); err != nil {
					t.Fatal(err)
				}
			}
			provider := &memoryExtractionProvider{output: row.Reply, fault: row.Fault}
			cfg := runtimeConfig(t.TempDir(), provider)
			cfg.Memory, cfg.Recovery, cfg.CachePolicy = bound, DirectRecovery{}, NullCachePolicy{}
			session, err := NewRuntimeSession(cfg)
			if err != nil {
				t.Fatal(err)
			}
			before := session.meter.Snapshot()
			count, err := session.consolidateMemories(ctx)
			if err != nil || count != row.Count {
				t.Fatal(count, err, row.Count)
			}
			if !reflect.DeepEqual(before, session.meter.Snapshot()) {
				t.Fatal("consolidation changed live meter")
			}
			records, err := bound.List(ctx)
			if err != nil || !reflect.DeepEqual(records, row.Records) {
				t.Fatalf("records differ: %#v want %#v err %v", records, row.Records, err)
			}
			records, err = foreign.List(ctx)
			if err != nil || !reflect.DeepEqual(records, row.Foreign) {
				t.Fatal("foreign records changed", records, err)
			}
			if len(provider.requests) != len(row.Calls) {
				t.Fatal("threshold request count", len(provider.requests), len(row.Calls))
			}
			for i, request := range provider.requests {
				expected := row.Calls[i]
				if request.Purpose != protocol.PurposeMemoryConsolidation || request.Model != expected.Model || request.MaxTokens != expected.MaxTokens || request.System != nil || len(request.Tools) != 0 || !reflect.DeepEqual(request.Messages, expected.Messages) {
					t.Fatal("consolidation request differs", request, expected)
				}
			}
			purposes := make([]protocol.RequestPurpose, 0)
			for _, record := range session.Events() {
				if v, ok := record.Event.ModelStart(); ok {
					purposes = append(purposes, v.Purpose)
				}
			}
			if !reflect.DeepEqual(purposes, row.Purposes) {
				t.Fatal("consolidation event purpose differs", purposes, row.Purposes)
			}

		})
	}
}

type captureMemoryProvider struct {
	name     string
	root     string
	requests []protocol.ModelRequest
	restore  func()
	cancel   context.CancelFunc
}

func (p *captureMemoryProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	p.requests = append(p.requests, request.Clone())
	var blocks []protocol.Block
	reason := protocol.StopEndTurn
	switch request.MaxTokens {
	case 200:
		blocks = []protocol.Block{protocol.NewTextBlock("[0]")}
	case 1500:
		blocks = []protocol.Block{protocol.NewTextBlock(`[{"name":"captured","body":"fact"}]`)}
	case 2500:
		blocks = []protocol.Block{protocol.NewTextBlock(`[{"name":"merged","body":"fact"}]`)}
	default:
		if p.name == "provider-error" {
			return protocol.ModelReply{}, errors.New("main provider failed")
		}
		if p.name == "cancel" {
			p.cancel()
			return protocol.ModelReply{}, context.Canceled
		}
		if p.name == "capture-error" {
			backup := p.root + "-saved"
			if err := os.Rename(p.root, backup); err != nil {
				return protocol.ModelReply{}, err
			}
			if err := os.WriteFile(p.root, []byte("not a directory"), 0600); err != nil {
				_ = os.Rename(backup, p.root)
				return protocol.ModelReply{}, err
			}
			p.restore = func() { _ = os.Remove(p.root); _ = os.Rename(backup, p.root) }
		}
		if p.name == "exhaustion" || p.name == "tool-halt" {
			blocks = []protocol.Block{protocol.NewBashUse(fmt.Sprintf("u%d", len(p.requests)), "same")}
			reason = protocol.StopToolUse
		} else {
			blocks = []protocol.Block{protocol.NewTextBlock("done")}
		}
	}
	reply := fakeReply(blocks, reason)
	reply.Model, reply.Usage.InputTokens, reply.Usage.OutputTokens = "served-memory", 1234, 2
	return reply, nil
}

func TestMemoryCaptureEndpointsMatchActualPythonSource(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-memory-capture.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name             string
			Budgets          []int
			Error            bool
			Records, Foreign []memory.Record
			Memory           []MemoryEvent
			CaptureErrors    int `json:"capture_errors"`
			Purposes         []protocol.RequestPurpose
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || len(fixture.Cases) != 11 {
		t.Fatal(err, len(fixture.Cases))
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			root := t.TempDir()
			store, err := memory.NewStore(ctx, root, nil)
			if err != nil {
				t.Fatal(err)
			}
			bound, _ := memory.Bind(store, "owner")
			foreign, _ := memory.Bind(store, "foreign")
			if _, err := foreign.Write(ctx, memory.Input{Name: "foreign", Type: memory.Project, Description: "foreign description", Body: "foreign secret"}); err != nil {
				t.Fatal(err)
			}
			if row.Name == "threshold-after-extract" {
				for i := 0; i < 9; i++ {
					name := fmt.Sprintf("seed-%d", i)
					if _, err := bound.Write(ctx, memory.Input{Name: name, Type: memory.Project, Description: name, Body: "original", Origin: memory.Explicit}); err != nil {
						t.Fatal(err)
					}
				}
			}
			provider := &captureMemoryProvider{name: row.Name, root: root, cancel: cancel}
			defer func() {
				if provider.restore != nil {
					provider.restore()
				}
			}()
			cfg := runtimeConfig(t.TempDir(), provider)
			cfg.Memory, cfg.MemoryTools, cfg.CachePolicy, cfg.Recovery, cfg.StuckDetector, cfg.Mode = bound, row.Name != "no-pair", NullCachePolicy{}, DirectRecovery{}, NullStuckDetector{}, ModeAuto
			if row.Name == "readonly" {
				cfg.Mode = ModeReadonly
			}
			if row.Name == "disabled" {
				disabled := false
				cfg.MemoryAuto = &disabled
			}
			if row.Name == "resume-halt" || row.Name == "tool-halt" {
				thresholds := DefaultStuckThresholds()
				thresholds.Monologue, thresholds.RepeatActionResult, thresholds.MaxNudges = 2, 2, 0
				detector, err := NewStuckDetector(thresholds)
				if err != nil {
					t.Fatal(err)
				}
				cfg.StuckDetector = detector
			}
			if row.Name == "resume-halt" {
				cfg.StopHooks = []StopHook{resumeStopHook{}}
			}
			session, err := NewRuntimeSession(cfg)
			if err != nil {
				t.Fatal(err)
			}
			_, err = session.Run(ctx, "learn")
			if (err != nil) != row.Error {
				t.Fatal("endpoint error differs", err, row.Error)
			}
			if provider.restore != nil {
				provider.restore()
				provider.restore = nil
			}
			records, listErr := bound.List(context.Background())
			if listErr != nil || !reflect.DeepEqual(records, row.Records) {
				t.Fatalf("records differ: %#v want %#v err %v", records, row.Records, listErr)
			}
			records, listErr = foreign.List(context.Background())
			if listErr != nil || !reflect.DeepEqual(records, row.Foreign) {
				t.Fatal("foreign records changed", records, listErr)
			}
			budgets := make([]int, 0, len(provider.requests))
			for _, request := range provider.requests {
				budgets = append(budgets, request.MaxTokens)
			}
			if !reflect.DeepEqual(budgets, row.Budgets) {
				t.Fatal("endpoint query count differs", budgets, row.Budgets)
			}
			projected := make([]MemoryEvent, 0)
			purposes := make([]protocol.RequestPurpose, 0)
			captureErrors := 0
			for _, record := range session.Events() {
				if v, ok := record.Event.ModelStart(); ok {
					purposes = append(purposes, v.Purpose)
				}
				if v, ok := record.Event.Memory(); ok {
					projected = append(projected, v)
					wire, err := json.Marshal(record)
					if err != nil {
						t.Fatal(err)
					}
					var restored SessionEventRecord
					if err := json.Unmarshal(wire, &restored); err != nil {
						t.Fatal(err)
					}
					copy, ok := restored.Event.Memory()
					if !ok || !reflect.DeepEqual(copy, v) {
						t.Fatal("archived memory event changed")
					}
				}
				if v, ok := record.Event.MemoryCaptureError(); ok {
					captureErrors++
					if len([]rune(v.Detail)) > 200 {
						t.Fatal("unbounded capture error")
					}
					wire, err := json.Marshal(record)
					if err != nil {
						t.Fatal(err)
					}
					var restored SessionEventRecord
					if err := json.Unmarshal(wire, &restored); err != nil {
						t.Fatal(err)
					}
					copy, ok := restored.Event.MemoryCaptureError()
					if !ok || copy != v {
						t.Fatal("archived capture error changed")
					}
				}
			}
			if !reflect.DeepEqual(projected, row.Memory) || captureErrors != row.CaptureErrors || !reflect.DeepEqual(purposes, row.Purposes) {
				t.Fatal("endpoint events differ", projected, captureErrors, purposes, row.Memory, row.CaptureErrors, row.Purposes)
			}
		})
	}
}

type blockedCaptureProvider struct {
	entered chan struct{}
	release chan struct{}
}

func (p blockedCaptureProvider) Complete(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	if request.MaxTokens == 1500 {
		close(p.entered)
		select {
		case <-ctx.Done():
			return protocol.ModelReply{}, ctx.Err()
		case <-p.release:
		}
		return fakeReply([]protocol.Block{protocol.NewTextBlock(`[{"name":"captured","body":"fact"}]`)}, protocol.StopEndTurn), nil
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
}
func TestMemoryCaptureHoldsLifecycleAcrossQueryAndCancelledRemember(t *testing.T) {
	ctx := context.Background()
	store, err := memory.NewStore(ctx, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	bound, _ := memory.Bind(store, "owner")
	p := blockedCaptureProvider{make(chan struct{}), make(chan struct{})}
	cfg := runtimeConfig(t.TempDir(), p)
	cfg.Memory, cfg.MemoryTools = bound, true
	session, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := session.Run(ctx, "learn"); done <- err }()
	<-p.entered
	waiting, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	input, err := protocol.DecodeToolInput(protocol.ToolRemember, []byte(`{"name":"explicit","content":"must not persist"}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = session.gate.Dispatch(waiting, ToolAuthority{SessionID: session.id, OwnerID: "owner", Workspace: session.executionRoot(), Mode: ModeAuto}, ToolCall{ID: "blocked", Input: input})
	close(p.release)
	if err != context.DeadlineExceeded {
		t.Fatal("remember bypassed capture lifecycle", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("capture failed to release lifecycle")
	}
	records, err := bound.List(ctx)
	if err != nil || len(records) != 1 || records[0].Name != "captured" {
		t.Fatal(records, err)
	}
}
func TestMemoryCaptureCancellationPropagatesAndReleasesLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store, err := memory.NewStore(ctx, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	bound, _ := memory.Bind(store, "owner")
	p := blockedCaptureProvider{make(chan struct{}), make(chan struct{})}
	cfg := runtimeConfig(t.TempDir(), p)
	cfg.Memory, cfg.MemoryTools = bound, true
	session, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := session.Run(ctx, "learn"); done <- err }()
	<-p.entered
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatal("capture swallowed cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled capture remained live")
	}
	for _, record := range session.Events() {
		if record.Event.Kind() == EventMemoryCaptureError {
			t.Fatal("cancel became contained capture failure")
		}
		if event, ok := record.Event.Memory(); ok && event.Action == MemoryExtract {
			t.Fatal("cancel emitted capture success")
		}
	}
	after, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := bound.WithLifecycle(after, func() error { return nil }); err != nil {
		t.Fatal("cancel retained lifecycle", err)
	}
	records, err := bound.List(after)
	if err != nil || len(records) != 0 {
		t.Fatal("cancelled capture wrote facts", records, err)
	}
}
func TestMemoryCaptureEventsDetachValidateAndMask(t *testing.T) {
	consolidated := 1
	original := SessionEvent{kind: EventMemory, memory: MemoryEvent{Action: MemoryExtract, Count: 2, Consolidated: &consolidated}}
	cloned := original.clone()
	*cloned.memory.Consolidated = 9
	value, ok := original.Memory()
	if !ok || *value.Consolidated != 1 {
		t.Fatal("event clone retained pointer")
	}
	*value.Consolidated = 10
	again, _ := original.Memory()
	if *again.Consolidated != 1 {
		t.Fatal("event accessor retained pointer")
	}
	negative := -1
	for _, event := range []MemoryEvent{{Action: MemoryLoad, Count: -1}, {Action: MemoryLoad, Consolidated: &consolidated}, {Action: MemoryExtract}, {Action: MemoryExtract, Consolidated: &negative}, {Action: "unknown"}} {
		if event.Validate() == nil {
			t.Fatal("invalid memory event accepted", event)
		}
	}
	masked := maskedEvent(runtimeSecrets(), SessionEvent{kind: EventMemoryCaptureError, memoryCaptureError: MemoryCaptureErrorEvent{Detail: "failure: " + runtimeCanary}})
	detail, _ := masked.MemoryCaptureError()
	if strings.Contains(detail.Detail, runtimeCanary) {
		t.Fatal("capture error leaked registry value")
	}
}
