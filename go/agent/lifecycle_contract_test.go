package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/luoyjx/mini-loop/go/protocol"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Raw JSON stays at the fixture/recording boundary. Runtime events are closed variants.
type lifecycleJSON map[string]json.RawMessage

func lifecycleField[T any](row lifecycleJSON, key string, value T) {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	row[key] = data
}
func canonicalLifecycle(data json.RawMessage) string {
	if len(data) > 0 && data[0] == '{' {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(data, &object); err != nil {
			panic(err)
		}
		for key, value := range object {
			object[key] = json.RawMessage(canonicalLifecycle(value))
		}
		encoded, _ := json.Marshal(object)
		return string(encoded)
	}
	if len(data) > 0 && data[0] == '[' {
		var list []json.RawMessage
		if err := json.Unmarshal(data, &list); err != nil {
			panic(err)
		}
		for i, value := range list {
			list[i] = json.RawMessage(canonicalLifecycle(value))
		}
		encoded, _ := json.Marshal(list)
		return string(encoded)
	}
	if len(data) > 0 && (data[0] == '-' || data[0] >= '0' && data[0] <= '9') {
		number, err := strconv.ParseFloat(string(data), 64)
		if err != nil {
			panic(err)
		}
		return strconv.FormatFloat(number, 'g', -1, 64)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		panic(err)
	}
	return compact.String()
}

type lifecycleContracts struct {
	Cases []struct {
		Name, Output string
		Cancelled    bool
		Events       []lifecycleJSON
		Messages     []struct {
			Role    protocol.Role
			Content json.RawMessage
		}
		Info struct {
			Status       SessionStatus
			Activity     SessionActivity
			Busy         bool
			MessageCount int `json:"message_count"`
			RunCount     int `json:"run_count"`
			Subscribers  int
		}
	}
	Titles []struct{ Input, Title *string }
	Labels []struct {
		Tool    protocol.ToolName
		Input   json.RawMessage
		Display struct{ Verb, Object string }
	}
	Bus struct {
		Backlog, Queue  int
		LiveCount       int `json:"live_count"`
		LiveFirst       int `json:"live_first"`
		LiveLast        int `json:"live_last"`
		ReplayCount     int `json:"replay_count"`
		ReplayFirst     int `json:"replay_first"`
		ReplayLast      int `json:"replay_last"`
		EphemeralLive   int `json:"ephemeral_live"`
		EphemeralReplay int `json:"ephemeral_replay"`
	}
}

func readLifecycleContracts(t *testing.T) lifecycleContracts {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-lifecycle.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture lifecycleContracts
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

type lifecycleProvider struct {
	name    string
	calls   int
	last    protocol.ModelRequest
	started chan struct{}
}

func (p *lifecycleProvider) Complete(ctx context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
	p.last = r
	p.calls++
	if p.name == "provider-error" {
		return protocol.ModelReply{}, errors.New("offline fail")
	}
	if p.name == "cancel-model" {
		close(p.started)
		<-ctx.Done()
		return protocol.ModelReply{}, ctx.Err()
	}
	blocks := []protocol.Block{protocol.NewTextBlock("complete")}
	reason := protocol.StopEndTurn
	if p.calls == 1 {
		switch p.name {
		case "completed", "denied", "failed-tool", "cancel-tool":
			blocks = []protocol.Block{protocol.NewTextBlock("Inspect files. More context."), protocol.NewToolUseWithoutCaller("u1", protocol.BashToolInput(protocol.BashInput{Command: "rg main ."}))}
			reason = protocol.StopToolUse
		case "pause":
			blocks = []protocol.Block{protocol.NewTextBlock("partial")}
			reason = protocol.StopPauseTurn
		case "refusal":
			blocks = []protocol.Block{}
			reason = protocol.StopRefusal
		case "unknown-stop":
			reason = protocol.StopReason("future_stop")
		}
	}
	reply := fakeReply(blocks, reason)
	reply.Model = r.Model
	tokens, err := FakePromptTokens(r)
	reply.Usage.InputTokens = tokens
	return reply, err
}

type lifecycleBash struct {
	name    string
	started chan struct{}
}

func (b lifecycleBash) ExecuteBash(ctx context.Context, _ protocol.BashInput) (string, error) {
	switch b.name {
	case "failed-tool":
		return "", errors.New("tool failed")
	case "cancel-tool":
		close(b.started)
		<-ctx.Done()
		return "", ctx.Err()
	}
	return "same", nil
}
func projectLifecycle(record SessionEventRecord) lifecycleJSON {
	data, err := json.Marshal(record)
	if err != nil {
		panic(err)
	}
	row := lifecycleJSON{}
	if err = json.Unmarshal(data, &row); err != nil {
		panic(err)
	}
	delete(row, "ts")
	delete(row, "duration_ms")
	return row
}
func normalizeLifecycleIDs(row lifecycleJSON, ids map[string]string, counts map[string]int) {
	for _, key := range []string{"span_id", "parent_span_id", "action_id", "activity_id", "message_id", "parent_message_id"} {
		data, ok := row[key]
		if !ok {
			continue
		}
		var value string
		_ = json.Unmarshal(data, &value)
		prefix := ""
		switch {
		case strings.HasPrefix(value, "model_"):
			prefix = "model"
		case strings.HasPrefix(value, "tool_"):
			prefix = "tool"
		case key == "action_id":
			prefix = "action"
		case key == "activity_id":
			prefix = "activity"
		case key == "message_id" || key == "parent_message_id":
			prefix = "msg"
		}
		if prefix == "" {
			continue
		}
		normalized, ok := ids[value]
		if !ok {
			counts[prefix]++
			normalized = fmt.Sprintf("%s_%d", prefix, counts[prefix])
			ids[value] = normalized
		}
		lifecycleField(row, key, normalized)
	}
	// Normalize only the language-specific exception label, retaining its exact detail.
	for _, key := range []string{"error", "text"} {
		if data, ok := row[key]; ok {
			var value string
			if json.Unmarshal(data, &value) == nil {
				value = strings.ReplaceAll(value, "*errors.errorString: offline fail", "RuntimeError: offline fail")
				lifecycleField(row, key, value)
			}
		}
	}
}
func TestManagedLifecycleMatchesActualPython(t *testing.T) {
	for _, fixture := range readLifecycleContracts(t).Cases {
		t.Run(fixture.Name, func(t *testing.T) {
			started := make(chan struct{})
			p := &lifecycleProvider{name: fixture.Name, started: started}
			config := runtimeConfig(t.TempDir(), p)
			config.ID = SessionID(fixture.Name)
			config.Label = fixture.Name
			config.Mode = ModeAuto
			config.MaxRounds = 12
			config.Bash = lifecycleBash{fixture.Name, started}
			config.SystemBuilder = FixedSystem("stable")
			if fixture.Name == "denied" {
				config.Hooks.Before = []BeforeHook{beforeHookFunc(func(context.Context, ToolAuthority, ToolCall) (BeforeDecision, error) {
					return DenyToolCall("blocked by policy"), nil
				})}
			}
			session, err := NewManagedSession(config)
			if err != nil {
				t.Fatal(err)
			}
			sub := session.Subscribe(false)
			var output string
			if strings.HasPrefix(fixture.Name, "cancel-") {
				done := make(chan error, 1)
				go func() { _, err := session.Run(context.Background(), "go"); done <- err }()
				select {
				case <-started:
				case <-time.After(3 * time.Second):
					t.Fatal("not started")
				}
				info := session.Info()
				if info.Status != StatusRunning || !info.Busy || info.Activity != ActivityRunning || info.RunCount != 1 {
					t.Fatal(info)
				}
				cancelled, err := session.Cancel(context.Background(), "stop now")
				if err != nil || !cancelled {
					t.Fatal(cancelled, err)
				}
				if err := <-done; !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else {
				output, err = session.Run(context.Background(), "go")
				if err != nil {
					t.Fatal(err)
				}
			}
			sub.Close()
			output = strings.ReplaceAll(output, "*errors.errorString: offline fail", "RuntimeError: offline fail")
			if output != fixture.Output {
				t.Fatalf("output %q != %q", output, fixture.Output)
			}
			info := session.Info()
			if info.Status != fixture.Info.Status || info.Activity != fixture.Info.Activity || info.Busy != fixture.Info.Busy || info.MessageCount != fixture.Info.MessageCount || info.RunCount != fixture.Info.RunCount || info.Subscribers != fixture.Info.Subscribers {
				t.Fatalf("info %+v expected %+v", info, fixture.Info)
			}
			records := session.Events()
			if len(records) != len(fixture.Events) {
				t.Fatalf("events %d != %d", len(records), len(fixture.Events))
			}
			ids, counts := map[string]string{}, map[string]int{}
			for i, record := range records {
				actual := projectLifecycle(record)
				normalizeLifecycleIDs(actual, ids, counts)
				a, _ := json.Marshal(actual)
				b, _ := json.Marshal(fixture.Events[i])
				if canonicalLifecycle(a) != canonicalLifecycle(b) {
					wire, _ := p.last.Wire()
					payload, _ := json.Marshal(wire.Messages)
					t.Fatalf("event %d\nactual %s\nsource %s\nwire %s", i, canonicalLifecycle(a), canonicalLifecycle(b), payload)
				}
			}
			expected := []protocol.Message{}
			for _, message := range fixture.Messages {
				var content protocol.Content
				// Python appends [] for empty refusal even though its request validator rejects it.
				// Normalize only the fixture boundary, retaining Go's strict transport domain.
				if string(message.Content) == "[]" {
					content = protocol.PlainContent("")
				} else if err := json.Unmarshal(message.Content, &content); err != nil {
					t.Fatal(err)
				}
				expected = append(expected, protocol.Message{Role: message.Role, Content: content})
			}
			a, _ := json.Marshal(session.Messages())
			b, _ := json.Marshal(expected)
			a = bytes.ReplaceAll(a, []byte("*errors.errorString: offline fail"), []byte("RuntimeError: offline fail"))
			if canonicalLifecycle(a) != canonicalLifecycle(b) {
				t.Fatalf("messages\n%s\n%s", a, b)
			}
		})
	}
}
func TestActivityContractsMatchPython(t *testing.T) {
	fixture := readLifecycleContracts(t)
	for _, row := range fixture.Titles {
		input := ""
		if row.Input != nil {
			input = *row.Input
		}
		title, ok := ActivityTitle(input)
		if ok != (row.Title != nil) || ok && title != *row.Title {
			t.Fatalf("title %q: %q %v != %v", input, title, ok, row.Title)
		}
	}
	for _, row := range fixture.Labels {
		// Label source probes omit fields unused by display. Supply valid typed
		// tool requirements only at this boundary; display inputs remain unchanged.
		var labelInput lifecycleJSON
		if err := json.Unmarshal(row.Input, &labelInput); err != nil {
			t.Fatal(err)
		}
		switch row.Tool {
		case protocol.ToolWriteFile:
			lifecycleField(labelInput, "content", "")
		case protocol.ToolEditFile:
			lifecycleField(labelInput, "old_text", "x")
			lifecycleField(labelInput, "new_text", "")
		}
		encoded, _ := json.Marshal(labelInput)
		input, err := protocol.DecodeToolInput(row.Tool, encoded)
		if err != nil {
			t.Fatal(err)
		}
		label := ToolLabel(input)
		if !reflect.DeepEqual(label, ToolDisplay{row.Display.Verb, row.Display.Object}) {
			t.Fatalf("label %+v != %+v", label, row.Display)
		}
	}
}
