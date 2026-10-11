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
)

type timedFragment struct {
	protocol.StreamDelta
	Seconds float64 `json:"seconds"`
}
type progressFixture struct {
	Progress []struct {
		Name     string
		Chars    *int
		Duration *float64
		Pieces   []timedFragment
		Events   []streamEventProjection
		Partial  string
		Failure  *string
	}
	Fake struct {
		Request protocol.ModelRequest
		Rows    []struct {
			Thinking bool
			Method   string
			Calls    uint64
			Reply    protocol.ModelReply
			Deltas   []protocol.StreamDelta
		}
	}
}

func readProgressFixture(t *testing.T) progressFixture {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-progress.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture progressFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	fixture.Fake.Request.Purpose = protocol.PurposeAgentTurn
	return fixture
}

type fragmentClock struct{ seconds float64 }

func (c *fragmentClock) Now() time.Time { return time.Unix(0, int64(c.seconds*float64(time.Second))) }

type timedProgressStream struct {
	pieces  []timedFragment
	clock   *fragmentClock
	failure *string
}

func (p timedProgressStream) CompleteStream(ctx context.Context, _ protocol.ModelRequest, emit func(protocol.StreamDelta) error) (protocol.ModelReply, error) {
	for _, piece := range p.pieces {
		p.clock.seconds = piece.Seconds
		if err := emit(piece.StreamDelta); err != nil {
			return protocol.ModelReply{}, err
		}
	}
	if p.failure != nil {
		return protocol.ModelReply{}, errors.New(*p.failure)
	}
	return fakeReply([]protocol.Block{}, protocol.StopEndTurn), ctx.Err()
}
func TestConfiguredProgressMatchesActualPythonTransport(t *testing.T) {
	for _, v := range readProgressFixture(t).Progress {
		t.Run(v.Name, func(t *testing.T) {
			s, err := NewSession("progress", "owner", &FakeProvider{}, echoExecutor{}, 2)
			if err != nil {
				t.Fatal(err)
			}
			clock := &fragmentClock{}
			config := StreamProgressConfig{CoalesceChars: v.Chars, Clock: clock}
			if v.Duration != nil {
				duration := time.Duration(*v.Duration * float64(time.Second))
				config.CoalesceDuration = &duration
			}
			s.streamProgress = streamProgress(config)
			s.events.secrets = streamMask{}
			live := s.events.subscribe(false)
			_, err = s.streamingComplete(context.Background(), timedProgressStream{v.Pieces, clock, v.Failure}, protocol.ModelRequest{})
			if (err == nil) != (v.Failure == nil) {
				t.Fatalf("unexpected failure: %v", err)
			}
			live.Close()
			actual := []streamEventProjection{}
			for record := range live.Events() {
				data, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				var row streamEventProjection
				if err := json.Unmarshal(data, &row); err != nil {
					t.Fatal(err)
				}
				row.StreamID = "<stream-id>"
				actual = append(actual, row)
			}
			if !reflect.DeepEqual(actual, v.Events) || s.streamedText != v.Partial {
				t.Fatalf("events/partial differ: %#v / %#v, %q / %q", actual, v.Events, s.streamedText, v.Partial)
			}
			if len(s.Events()) != 0 {
				t.Fatal("provisional progress reached replay")
			}
		})
	}
}
func TestStatefulFakeDirectAndStreamMatchActualPythonClient(t *testing.T) {
	fixture := readProgressFixture(t)
	clients := map[bool]*FakeProvider{}
	for _, v := range fixture.Fake.Rows {
		p := clients[v.Thinking]
		if p == nil {
			enabled := v.Thinking
			p = NewFakeProvider(FakeProviderConfig{Thinking: &enabled})
			clients[v.Thinking] = p
		}
		var reply protocol.ModelReply
		var err error
		deltas := []protocol.StreamDelta{}
		if v.Method == "direct" {
			reply, err = p.Complete(context.Background(), fixture.Fake.Request)
		} else {
			reply, err = p.Streaming().CompleteStream(context.Background(), fixture.Fake.Request, func(d protocol.StreamDelta) error { deltas = append(deltas, d); return nil })
		}
		if err != nil {
			t.Fatal(err)
		}
		actual, err := json.Marshal(reply)
		if err != nil {
			t.Fatal(err)
		}
		want, err := json.Marshal(v.Reply)
		if err != nil {
			t.Fatal(err)
		}
		if string(actual) != string(want) || !reflect.DeepEqual(deltas, v.Deltas) || p.Calls() != v.Calls {
			t.Fatalf("thinking=%v %s call %d drift:\n%s\n%s\n%#v / %#v", v.Thinking, v.Method, v.Calls, actual, want, deltas, v.Deltas)
		}
	}
}
func TestFakeClientSharesUniqueSequenceAndOwnsCancellation(t *testing.T) {
	p := &FakeProvider{}
	request := readProgressFixture(t).Fake.Request
	var group sync.WaitGroup
	results := make(chan protocol.ModelReply, 32)
	for i := 0; i < 32; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			reply, err := p.Complete(context.Background(), request)
			if err != nil {
				t.Error(err)
				return
			}
			results <- reply
		}()
	}
	group.Wait()
	close(results)
	ids, signatures := map[string]bool{}, map[string]bool{}
	for reply := range results {
		thinking, ok := reply.Content[0].Thinking()
		if !ok || ids[reply.ID] || signatures[thinking.Signature] {
			t.Fatal("shared fake reused identity or signature")
		}
		ids[reply.ID], signatures[thinking.Signature] = true, true
	}
	if len(ids) != 32 || p.Calls() != 32 {
		t.Fatal("lost fake calls")
	}
	request.MaxTokens = 64000
	if _, err := p.Complete(context.Background(), request); err == nil || p.Calls() != 32 {
		t.Fatal("preflight consumed sequence")
	}
	if _, err := p.Streaming().CompleteStream(context.Background(), request, nil); err != nil || p.Calls() != 33 {
		t.Fatal("stream did not lift ceiling", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Complete(ctx, request); !errors.Is(err, context.Canceled) || p.Calls() != 33 {
		t.Fatal("cancelled admission consumed sequence")
	}
	delayed := NewFakeProvider(FakeProviderConfig{Delay: time.Hour})
	request.MaxTokens = 8000
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := delayed.Complete(ctx, request); !errors.Is(err, context.DeadlineExceeded) || delayed.Calls() != 1 {
		t.Fatal("delay escaped context or lost admitted ordinal", err)
	}
	other, err := (&FakeProvider{}).Complete(context.Background(), request)
	if err != nil || other.ID != "msg_fake_000001" {
		t.Fatal("independent client inherited sequence", err)
	}
}

// Inspect publication while the provider is still inside its fragment callback:
// a final flush would otherwise conceal a lost constructor/child/fork setting.
type checkingProgressProvider struct {
	live     *EventSubscription
	delegate bool
	depths   []int
}

func (p *checkingProgressProvider) Complete(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
	return protocol.ModelReply{}, errors.New("expected streaming")
}
func (p *checkingProgressProvider) CompleteStream(ctx context.Context, _ protocol.ModelRequest, emit func(protocol.StreamDelta) error) (protocol.ModelReply, error) {
	if err := emit(protocol.StreamDelta{Kind: protocol.DeltaText, Text: "a"}); err != nil {
		return protocol.ModelReply{}, err
	}
	found := false
	for {
		select {
		case row := <-p.live.Events():
			if row.Event.Kind() == EventAssistantDelta {
				found = true
				p.depths = append(p.depths, row.Scope.Depth)
			}
		default:
			if !found {
				return protocol.ModelReply{}, errors.New("configured threshold was lost before final flush")
			}
			if p.delegate {
				p.delegate = false
				return fakeReply([]protocol.Block{protocol.NewToolUse("task", protocol.TaskToolInput(protocol.TaskInput{Prompt: "inspect"}))}, protocol.StopToolUse), nil
			}
			return streamedReply("done"), ctx.Err()
		}
	}
}
func TestConfiguredProgressReachesManagerForkAndFreshChild(t *testing.T) {
	chars := 1
	duration := time.Hour
	p := &checkingProgressProvider{delegate: true}
	config := managerTestConfig(t.TempDir(), p)
	config.Services.StreamProgress = StreamProgressConfig{CoalesceChars: &chars, CoalesceDuration: &duration}
	m := makeManager(t, config)
	// A manager owns a configuration snapshot, including thresholds used by later forks.
	chars, duration = 999, time.Hour*2
	s, err := m.Create(context.Background(), CreateSessionRequest{Owner: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	p.live = s.Subscribe(false)
	if text, err := s.Run(context.Background(), "delegate"); err != nil || text != "done" {
		t.Fatal(text, err)
	}
	p.live.Close()
	child, err := m.Fork(context.Background(), "owner", s.ID())
	if err != nil {
		t.Fatal(err)
	}
	p.live = child.Subscribe(false)
	if text, err := child.Run(context.Background(), "continue"); err != nil || text != "done" {
		t.Fatal(text, err)
	}
	p.live.Close()
	if !reflect.DeepEqual(p.depths, []int{0, 1, 0, 0}) {
		t.Fatal("missing parent/child/fork generations", p.depths)
	}
}
func TestFakeStreamingRunsToolsAndKeepsSignedThinkingInHistory(t *testing.T) {
	p := NewFakeProvider(FakeProviderConfig{Delay: time.Hour})
	config := runtimeConfig(t.TempDir(), p.Streaming())
	config.Mode = ModeAuto
	config.MaxTokens = 64000
	config.StreamProgress.CoalesceChars = new(int) // explicit zero publishes each fragment
	s, err := NewManagedSession(config)
	if err != nil {
		t.Fatal(err)
	}
	if text, err := s.Run(context.Background(), "go"); err != nil || text != "Done. Tool said: handled: go" {
		t.Fatal(text, err)
	}
	if p.Calls() != 2 {
		t.Fatal("fake stream did not execute a paired tool round")
	}
	history := s.Messages()
	if err := protocol.ValidateTranscript(history); err != nil {
		t.Fatal(err)
	}
	var signatures []string
	for _, message := range history {
		blocks, _ := message.Content.Blocks()
		for _, block := range blocks {
			if thinking, ok := block.Thinking(); ok {
				signatures = append(signatures, thinking.Signature)
			}
		}
	}
	if !reflect.DeepEqual(signatures, []string{"sig_fake_00000000", "sig_fake_00000001"}) {
		t.Fatal("fake thinking signature was not retained", signatures)
	}
	if s.core.streamedText != "" {
		t.Fatal("completed fake stream retained interrupted text")
	}
}
