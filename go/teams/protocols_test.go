package teams

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type protocolMembers map[Identity]MemberState

func (members protocolMembers) Member(id Identity) MemberState { return members[id] }
func protocolDigest(text string) string                        { return fmt.Sprintf("%x", sha256.Sum256([]byte(text))) }
func protocolLegacy(value Data) string {
	data, err := jsonvalue.AppendLegacy(nil, value)
	if err != nil {
		panic(err)
	}
	return string(data)
}
func protocolError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrProtocolMetadata):
		return "AttributeError"
	case errors.Is(err, ErrProtocolRequestID):
		return "TypeError"
	case strings.Contains(err.Error(), "sender must be text"):
		return "AttributeError"
	case errors.Is(err, syscall.EISDIR):
		return "IsADirectoryError"
	}
	return err.Error()
}
func TestCoordinatorMatchesActualPythonManager(t *testing.T) {
	var source struct {
		Cases []struct {
			Name   string
			Memory bool
			Steps  []struct {
				Operation struct {
					Action      string
					Team        TeamID
					Member      MemberName
					Content     string
					Repeat      int
					Request     int
					Approve     bool
					Count       int
					Resolve     bool
					MessageJSON string `json:"message_json"`
				}
				ResultHash        string `json:"result_hash"`
				RenderHash        string `json:"render_hash"`
				Error             string
				StatesHash        string `json:"states_hash"`
				StateCount        int    `json:"state_count"`
				ShutdownRequested bool   `json:"shutdown_requested"`
				Problems          []string
			}
		}
	}
	data, err := os.ReadFile("../testdata/python-team-protocols.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	if len(source.Cases) != 25 {
		t.Fatal("source census drift")
	}
	for _, recipe := range source.Cases {
		t.Run(recipe.Name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "teams")
			config := Config{Root: &root}
			if recipe.Memory {
				config.Root = nil
			}
			bus := New(config)
			bus.now = func() float64 { return 1000 }
			members := protocolMembers{Identity{"team", "bob"}: MemberReady, Identity{"team", "no-agent"}: MemberWithoutAgent, Identity{"team", "."}: MemberReady, Identity{"team", "\U0001fae8"}: MemberReady}
			c, err := NewCoordinator(CoordinatorConfig{bus, members})
			if err != nil {
				t.Fatal(err)
			}
			counter := 0
			c.newID = func() (RequestID, error) { counter++; return RequestID(fmt.Sprintf("req_%010x", counter)), nil }
			c.now = func() float64 { return 1000 }
			for index, step := range recipe.Steps {
				op := step.Operation
				team := op.Team
				if team == "" {
					team = "team"
				}
				member := op.Member
				if member == "" {
					member = Lead
					switch op.Action {
					case "submit", "shutdown", "request_plan", "block":
						member = "bob"
					}
				}
				repeat := op.Repeat
				if repeat == 0 {
					repeat = 1
				}
				content := strings.Repeat(op.Content, repeat)
				id := RequestID(fmt.Sprintf("req_%010x", op.Request))
				ctx := context.Background()
				var output Data
				var callErr error
				shutdown := false
				switch op.Action {
				case "submit":
					var out string
					out, callErr = c.SubmitPlan(ctx, Identity{team, member}, content)
					output = Text(out)
				case "shutdown":
					var out string
					out, callErr = c.RequestShutdown(ctx, team, member, content)
					output = Text(out)
				case "request_plan":
					var out string
					out, callErr = c.RequestPlan(ctx, team, member, content)
					output = Text(out)
				case "review":
					var out string
					out, callErr = c.ReviewPlan(ctx, team, id, op.Approve, content)
					output = Text(out)
				case "peek", "consume":
					var rows []Message
					if op.Action == "peek" {
						rows, callErr = bus.Peek(ctx, Key(team, member))
					} else {
						var result ConsumedInbox
						result, callErr = c.Consume(ctx, Identity{team, member})
						rows = result.Messages
						shutdown = result.ShutdownRequested
					}
					if callErr == nil {
						rendered, err := RenderMessages(rows)
						callErr = err
						if callErr == nil && protocolDigest(rendered) != step.RenderHash {
							t.Fatalf("step %d render drift\n%s", index, rendered)
						}
						values := make([]Data, len(rows))
						for i, row := range rows {
							values[i] = row.Data()
						}
						output = Object(Field{"messages", Array(values...)}, Field{"shutdown_requested", Bool(shutdown)})
					}
				case "inject":
					value, err := DecodeData(op.MessageJSON)
					if err != nil {
						t.Fatal(err)
					}
					if op.Request != 0 {
						metadata, _ := value.Lookup("metadata")
						fields := []Field{}
						for _, key := range metadata.Keys() {
							v, _ := metadata.Lookup(key)
							fields = append(fields, Field{key, v})
						}
						fields = append(fields, Field{"request_id", Text(string(id))})
						outer := []Field{}
						for _, key := range value.Keys() {
							v, _ := value.Lookup(key)
							if key == "metadata" {
								v = Object(fields...)
							}
							outer = append(outer, Field{key, v})
						}
						value = Object(outer...)
					}
					if recipe.Memory {
						bus.inboxes[Key(team, member)] = append(bus.inboxes[Key(team, member)], Message{value})
					} else {
						path, err := bus.path(Key(team, member))
						if err != nil {
							t.Fatal(err)
						}
						if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
							t.Fatal(err)
						}
						file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
						if err != nil {
							t.Fatal(err)
						}
						_, writeErr := file.WriteString(protocolLegacy(value) + "\n")
						closeErr := file.Close()
						if writeErr != nil || closeErr != nil {
							t.Fatal(writeErr, closeErr)
						}
					}
					output = Text("injected")
				case "block":
					path, err := bus.path(Key(team, member))
					if err != nil {
						t.Fatal(err)
					}
					if err := os.MkdirAll(path, 0700); err != nil {
						t.Fatal(err)
					}
					output = Text("blocked")
				case "batch":
					values := []Data{}
					for i := 0; i < op.Count; i++ {
						request, err := c.SubmitPlan(ctx, Identity{team, "bob"}, fmt.Sprintf("plan-%d", i))
						if err != nil {
							t.Fatal(err)
						}
						values = append(values, Text(request))
						if op.Resolve {
							result, err := c.ReviewPlan(ctx, team, RequestID(request), true, "")
							if err != nil {
								t.Fatal(err)
							}
							values = append(values, Text(result))
						}
					}
					output = Array(values...)
				default:
					t.Fatal("unknown operation", op.Action)
				}
				if got := protocolError(callErr); got != step.Error {
					t.Fatalf("step %d error %s != %s", index, got, step.Error)
				}
				if callErr == nil && protocolDigest(protocolLegacy(output)) != step.ResultHash {
					t.Fatalf("step %d result drift\n%s", index, protocolLegacy(output))
				}
				if shutdown != step.ShutdownRequested {
					t.Fatalf("step %d shutdown outcome drift", index)
				}
				states := c.Snapshot()
				rendered, err := RenderProtocols(states)
				if err != nil {
					t.Fatal(err)
				}
				if len(states) != step.StateCount || protocolDigest(rendered) != step.StatesHash {
					t.Fatalf("step %d state drift\n%s", index, rendered)
				}
				problems := bus.Problems().Messages()
				for i := range problems {
					problems[i] = strings.ReplaceAll(problems[i], root, "<root>")
				}
				if !reflect.DeepEqual(problems, step.Problems) {
					t.Fatalf("step %d problems differ: %v != %v", index, problems, step.Problems)
				}
			}
		})
	}
}

func TestCoordinatorDefaultsCopiesContextAndConcurrentReviews(t *testing.T) {
	if _, err := NewCoordinator(CoordinatorConfig{}); err == nil {
		t.Fatal("nil bus")
	}
	bus := New(Config{})
	c, err := NewCoordinator(CoordinatorConfig{Bus: bus})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if out, err := c.RequestShutdown(ctx, "team", "bob", ""); err != nil || out != "Error: no teammate bob" {
		t.Fatal(out, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	for _, call := range []func() (string, error){func() (string, error) { return c.SubmitPlan(canceled, Identity{"team", "bob"}, "p") }, func() (string, error) { return c.RequestPlan(canceled, "team", "bob", "p") }, func() (string, error) { return c.RequestShutdown(canceled, "team", "bob", "p") }, func() (string, error) { return c.ReviewPlan(canceled, "team", "r", true, "") }} {
		if _, err := call(); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
	if _, err := c.Consume(canceled, Identity{"team", Lead}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(c.Snapshot()) != 0 {
		t.Fatal("canceled operation published")
	}
	id, err := c.SubmitPlan(ctx, Identity{"team", "bob"}, "p")
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 14 || !strings.HasPrefix(id, "req_") {
		t.Fatal("source ID shape", id)
	}
	c.SubmitPlan(ctx, Identity{"other", "bob"}, "other")
	filtered := c.TeamProtocols("team")
	if len(filtered) != 1 || filtered[0].RequestID != RequestID(id) {
		t.Fatal(filtered)
	}
	filtered[0].Payload = "mutated"
	snapshot := c.Snapshot()
	snapshot[0].Feedback = "mutated"
	if c.TeamProtocols("team")[0].Payload != "p" || c.Snapshot()[0].Feedback != "" {
		t.Fatal("snapshot alias")
	}
	var group sync.WaitGroup
	outputs := make(chan string, 8)
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			out, err := c.ReviewPlan(ctx, "team", RequestID(id), true, "")
			if err != nil {
				outputs <- err.Error()
			} else {
				outputs <- out
			}
		}()
	}
	group.Wait()
	close(outputs)
	applied := 0
	for output := range outputs {
		if strings.HasPrefix(output, "Plan ") {
			applied++
		} else if !strings.Contains(output, "is already approved") {
			t.Fatal(output)
		}
	}
	rows, err := bus.Read(ctx, Key("team", "bob"))
	if err != nil || len(rows) != 1 || applied != 1 {
		t.Fatal(rows, applied, err)
	}
	c.newID = func() (RequestID, error) { return "", errors.New("entropy unavailable") }
	before := len(c.Snapshot())
	if _, err := c.SubmitPlan(ctx, Identity{"team", "bob"}, "new"); err == nil || len(c.Snapshot()) != before {
		t.Fatal("failed identity published", err)
	}
}

func TestCoordinatorInvalidDirectoryAndIdentityCollision(t *testing.T) {
	bus := New(Config{})
	members := protocolMembers{Identity{"team", "bob"}: MemberState(255)}
	c, err := NewCoordinator(CoordinatorConfig{bus, members})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := c.RequestShutdown(ctx, "team", "bob", ""); !errors.Is(err, ErrMemberState) {
		t.Fatal(err)
	}
	if _, err := c.RequestPlan(ctx, "team", "bob", ""); !errors.Is(err, ErrMemberState) {
		t.Fatal(err)
	}
	bus.Send(ctx, SendRequest{To: Key("team", "bob"), Content: "consumed before directory fault"})
	if result, err := c.Consume(ctx, Identity{"team", "bob"}); !errors.Is(err, ErrMemberState) || len(result.Messages) != 1 {
		t.Fatal(result, err)
	}
	if remaining, err := bus.Peek(ctx, Key("team", "bob")); err != nil || len(remaining) != 0 || len(c.Snapshot()) != 0 {
		t.Fatal("fault requeued or published", remaining, err)
	}
	c.newID = func() (RequestID, error) { return "req_same", nil }
	for _, plan := range []string{"first", "second"} {
		if _, err := c.SubmitPlan(ctx, Identity{"team", "bob"}, plan); err != nil {
			t.Fatal(err)
		}
	}
	states := c.Snapshot()
	if len(states) != 1 || states[0].Payload != "second" {
		t.Fatal("source duplicate ID replacement drift", states)
	}
}
