package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
	secretpkg "github.com/luoyjx/mini-loop/go/secrets"
)

type controlRow struct {
	Type  SessionEventKind `json:"type"`
	Count int              `json:"count"`
	Text  string           `json:"text"`
}
type controlScenario struct {
	CapabilityModes      []PermissionMode `json:"capability_modes"`
	Name                 string           `json:"name"`
	QueuedBefore         int              `json:"queued_before"`
	PendingAfter         int              `json:"pending_after"`
	Mode                 PermissionMode   `json:"mode"`
	Interjections        []string         `json:"interjections"`
	Postures             []string         `json:"postures"`
	Events               []controlRow     `json:"events"`
	RequestInterjections []int            `json:"request_interjections"`
	RequestPostures      []int            `json:"request_postures"`
	FileExists           bool             `json:"file_exists"`
}
type controlFixture struct {
	MaxChars  int               `json:"max_chars"`
	MaxQueue  int               `json:"max_queue"`
	Scenarios []controlScenario `json:"scenarios"`
}

func readControlFixture(t *testing.T) controlFixture {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-controls.json")
	if err != nil {
		t.Fatal(err)
	}
	var v controlFixture
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func controlWrappers(messages []protocol.Message, tag string) []string {
	result := []string{}
	for _, m := range messages {
		if text, ok := m.Content.Plain(); ok && strings.HasPrefix(text, "<"+tag+">") {
			result = append(result, text)
		}
	}
	return result
}

type controlProvider struct {
	name     string
	session  *ManagedSession
	requests [][]protocol.Message
}

func (p *controlProvider) Complete(ctx context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
	p.requests = append(p.requests, r.Messages)
	if p.name == "mid-round" && len(p.requests) == 1 {
		if _, err := p.session.Steer("actually, use staging"); err != nil {
			return protocol.ModelReply{}, err
		}
		if _, err := p.session.ChangePermissionMode(ModeReadonly); err != nil {
			return protocol.ModelReply{}, err
		}
		return fakeReply([]protocol.Block{protocol.NewToolUse("write", protocol.WriteFileToolInput(protocol.WriteFileInput{Path: "landed.txt", Content: "landed"}))}, protocol.StopToolUse), nil
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
}
func TestControlsMatchActualPythonTurns(t *testing.T) {
	source := readControlFixture(t)
	if source.MaxChars != MaxSteerChars || source.MaxQueue != MaxSteerQueue {
		t.Fatal("control bounds differ")
	}
	for _, expected := range source.Scenarios {
		t.Run(expected.Name, func(t *testing.T) {
			p := &controlProvider{name: expected.Name}
			root := t.TempDir()
			s, err := NewManagedSession(runtimeConfig(root, p))
			if err != nil {
				t.Fatal(err)
			}
			p.session = s
			switch expected.Name {
			case "ordered":
				s.Steer("first")
				s.Steer("second")
			case "unicode":
				s.Steer(strings.Repeat("🙂", 16001))
			case "overflow":
				for i := 0; i < 102; i++ {
					s.Steer(fmt.Sprintf("input-%d", i))
				}
			case "pre-first":
				s.ChangePermissionMode(ModeAuto)
			}
			got := controlScenario{Name: expected.Name, QueuedBefore: s.Info().PendingSteering, Events: []controlRow{}}
			if _, err := s.Run(context.Background(), "go"); err != nil {
				t.Fatal(err)
			}
			if expected.Name == "posture-batch" {
				for _, mode := range []PermissionMode{ModeAuto, ModeReadonly, ModeInteractive, ModeInteractive} {
					if _, err := s.ChangePermissionMode(mode); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := s.Run(context.Background(), "again"); err != nil {
				t.Fatal(err)
			}
			got.PendingAfter, got.Mode = s.Info().PendingSteering, s.Info().PermissionMode
			got.Interjections = controlWrappers(s.Messages(), "user_interjection")
			got.Postures = controlWrappers(s.Messages(), "posture_update")
			_, err = os.Stat(filepath.Join(root, "landed.txt"))
			got.FileExists = err == nil
			for _, r := range p.requests {
				got.RequestInterjections = append(got.RequestInterjections, len(controlWrappers(r, "user_interjection")))
				got.RequestPostures = append(got.RequestPostures, len(controlWrappers(r, "posture_update")))
			}
			for _, record := range s.Events() {
				if plan, ok := record.Event.CapabilityPlan(); ok {
					got.CapabilityModes = append(got.CapabilityModes, plan.PermissionMode)
				}
				if record.Event.Kind() != EventSteeringDelivered && record.Event.Kind() != EventPostureUpdate {
					continue
				}
				data, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				var row controlRow
				if err := json.Unmarshal(data, &row); err != nil {
					t.Fatal(err)
				}
				got.Events = append(got.Events, row)
			}
			if !reflect.DeepEqual(got, expected) {
				t.Fatalf("control scenario differs: actual %+v source %+v", got, expected)
			}
		})
	}
}

type modeChangingBefore struct{ session *ManagedSession }

func (h *modeChangingBefore) BeforeTool(context.Context, ToolAuthority, ToolCall) (BeforeDecision, error) {
	_, err := h.session.ChangePermissionMode(ModeReadonly)
	return KeepToolCall(), err
}
func TestModeRefreshesAfterBeforeHookWithoutChangingCallerAuthority(t *testing.T) {
	root := t.TempDir()
	// The hook changes mode after the dispatch snapshot, before permission.
	provider := &fakeSequenceProvider{replies: []protocol.ModelReply{fakeReply([]protocol.Block{protocol.NewToolUse("write", protocol.WriteFileToolInput(protocol.WriteFileInput{Path: "landed.txt", Content: "landed"}))}, protocol.StopToolUse), fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn)}}
	hook := &modeChangingBefore{}
	config := runtimeConfig(root, provider)
	config.Mode = ModeAuto
	config.Hooks.Before = []BeforeHook{hook}
	s, err := NewManagedSession(config)
	if err != nil {
		t.Fatal(err)
	}
	hook.session = s
	if _, err := s.Run(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "landed.txt")); !os.IsNotExist(err) {
		t.Fatal("stale pre-hook mode allowed a write", err)
	}
	if len(controlWrappers(s.Messages(), "posture_update")) != 1 {
		t.Fatal("changed posture not delivered")
	}
	if mode, err := s.ChangePermissionMode("bad"); err == nil || mode != "" || s.Info().PermissionMode != ModeReadonly {
		t.Fatal("invalid mode changed posture")
	}
}

type fakeSequenceProvider struct{ replies []protocol.ModelReply }

func (p *fakeSequenceProvider) Complete(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
	r := p.replies[0]
	p.replies = p.replies[1:]
	return r, nil
}
func TestControlsMaskRecordedTextAndKeepLiveInputRaw(t *testing.T) {
	const value = "private-credential"
	registry := secretpkg.New(secretpkg.Config{})
	registry.RegisterValue("API_TOKEN", value)
	p := &controlProvider{}
	config := runtimeConfig(t.TempDir(), p)
	config.Secrets = registry
	s, err := NewManagedSession(config)
	if err != nil {
		t.Fatal(err)
	}
	s.Steer(value)
	s.Run(context.Background(), "go")
	if !strings.Contains(strings.Join(controlWrappers(s.Messages(), "user_interjection"), ""), value) {
		t.Fatal("live steer was masked")
	}
	found := false
	for _, r := range s.Events() {
		if e, ok := r.Event.SteeringDelivered(); ok {
			found = true
			if e.Text != secretpkg.Mask {
				t.Fatal("unmasked recording", e)
			}
		}
	}
	if !found {
		t.Fatal("missing delivery event")
	}
}
func TestConcurrentControlUpdatesDoNotWaitForModelAndRemainBounded(t *testing.T) {
	p := &parkedProvider{started: make(chan struct{})}
	s := managedForTest(t, p)
	done := make(chan error, 1)
	go func() { _, err := s.Run(context.Background(), "go"); done <- err }()
	receiveSignal(t, p.started)
	var workers sync.WaitGroup
	for i := 0; i < 250; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			s.Steer(fmt.Sprint(i))
			s.ChangePermissionMode(ModeReadonly)
			s.Info()
		}(i)
	}
	workers.Wait()
	if v := s.Info(); v.PendingSteering != MaxSteerQueue || v.PermissionMode != ModeReadonly || !v.Busy {
		t.Fatal(v)
	}
	s.Cancel(context.Background(), "test")
	<-done
	s.StopAccepting("closed")
	if _, err := s.Steer("late"); err == nil {
		t.Fatal("closed control accepted")
	}
	if _, err := s.SubmitSteering("late"); err == nil {
		t.Fatal("closed wakeup accepted")
	}
}

type childControlProvider struct {
	session  *ManagedSession
	requests [][]protocol.Message
}

func (p *childControlProvider) Complete(_ context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
	p.requests = append(p.requests, r.Messages)
	if len(p.requests) == 1 {
		if _, err := p.session.Steer("parent-only"); err != nil {
			return protocol.ModelReply{}, err
		}
		return fakeReply([]protocol.Block{protocol.NewToolUse("child", protocol.TaskToolInput(protocol.TaskInput{Prompt: "look around"}))}, protocol.StopToolUse), nil
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
}
func TestChildDoesNotConsumeParentControls(t *testing.T) {
	p := &childControlProvider{}
	s := managedForTest(t, p)
	p.session = s
	if _, err := s.Run(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if len(p.requests) != 3 {
		t.Fatal("child did not run", len(p.requests))
	}
	if len(controlWrappers(p.requests[1], "user_interjection")) != 0 || len(controlWrappers(p.requests[2], "user_interjection")) != 1 {
		t.Fatal("parent queue crossed child boundary")
	}
}

type completionBoundaryProvider struct {
	first, release, next chan struct{}
	calls                int
}

func (p *completionBoundaryProvider) Complete(ctx context.Context, _ protocol.ModelRequest) (protocol.ModelReply, error) {
	p.calls++
	if p.calls == 1 {
		close(p.first)
		select {
		case <-p.release:
		case <-ctx.Done():
			return protocol.ModelReply{}, ctx.Err()
		}
		return fakeReply([]protocol.Block{protocol.NewTextBlock("first")}, protocol.StopEndTurn), nil
	}
	close(p.next)
	<-ctx.Done()
	return protocol.ModelReply{}, ctx.Err()
}
func TestCompletedTurnSnapshotPrecedesNextAdmission(t *testing.T) {
	p := &completionBoundaryProvider{first: make(chan struct{}), release: make(chan struct{}), next: make(chan struct{})}
	s := managedForTest(t, p)
	result := make(chan ManagedTurnResult, 1)
	fault := make(chan error, 1)
	run, err := DefaultRunContext()
	if err != nil {
		t.Fatal(err)
	}
	go func() { v, err := s.TryRunWithSnapshot(context.Background(), "first", run); result <- v; fault <- err }()
	receiveSignal(t, p.first)
	queued := &admissionProbeContext{Context: context.Background(), waiting: make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, err := s.Run(queued, "second"); done <- err }()
	receiveSignal(t, queued.waiting)
	close(p.release)
	first := <-result
	if err := <-fault; err != nil {
		t.Fatal(err)
	}
	receiveSignal(t, p.next)
	if first.Final != "first" || first.Info.RunCount != 1 || first.Info.Busy || first.Info.Status != StatusIdle {
		t.Fatal("completion snapshot changed", first)
	}
	if current := s.Info(); current.RunCount != 2 || !current.Busy {
		t.Fatal("next holder not active", current)
	}
	s.Cancel(context.Background(), "test")
	<-done
}
