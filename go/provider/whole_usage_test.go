package provider

import (
	"encoding/json"
	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"os"
	"strings"
	"testing"
)

func TestWholeUsageActualSDKStagesAndDeltaFaults(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-whole-usage.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Mode, Outcome, Phase, SSE string
			Response                        string          `json:"response_json"`
			Expected                        jsonvalue.Value `json:"event_usage"`
			Error                           struct{ Class, Message string }
			Consumer                        struct {
				Prompt *int `json:"prompt_tokens"`
			}
			Stages []struct {
				Event string
				Usage jsonvalue.Value `json:"event_usage"`
			} `json:"usage_stages"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 86 {
		t.Fatal("Source inventory changed")
	}
	client, err := New(Config{APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name+"/"+row.Mode, func(t *testing.T) {
			var usage protocol.TokenUsage
			if row.Mode == "direct" {
				reply, err := decodeReply([]byte(row.Response))
				if err != nil {
					t.Fatal(err)
				}
				usage = reply.Usage
				encoded, err := json.Marshal(reply)
				if err != nil {
					t.Fatal(err)
				}
				var restored protocol.ModelReply
				if err := json.Unmarshal(encoded, &restored); err != nil {
					t.Fatal(err)
				}
				assertSDKUsageShape(t, restored.Usage, row.Expected)
			} else {
				state := streamAssembly{}
				stage := 0
				failed := false
				for _, frame := range strings.Split(row.SSE, "\n\n") {
					lines := strings.Split(frame, "\n")
					if len(lines) < 2 {
						continue
					}
					event := strings.TrimPrefix(lines[0], "event: ")
					body := strings.TrimPrefix(lines[1], "data: ")
					err := client.consumeStream(&state, event, []byte(body), nil)
					if err != nil {
						if row.Outcome != "error" || event != row.Phase || row.Error.Class != "AttributeError" || !strings.Contains(err.Error(), row.Error.Message) {
							t.Fatal("wrong failure stage", event, err)
						}
						failed = true
						break
					}
					if event == "message_start" || event == "message_delta" {
						if stage >= len(row.Stages) || row.Stages[stage].Event != event {
							t.Fatal("stage inventory differs")
						}
						assertSDKUsageShape(t, state.reply.Usage, row.Stages[stage].Usage)
						stage++
					}
				}
				if stage != len(row.Stages) || failed != (row.Outcome == "error") {
					t.Fatal("terminal outcome differs")
				}
				if failed {
					if state.stopped {
						t.Fatal("failed stream completed")
					}
					return
				}
				if !state.stopped {
					t.Fatal("stream not stopped")
				}
				usage = state.reply.Usage
			}
			assertSDKUsageShape(t, usage, row.Expected)
			count, present := agent.PromptTokens(usage)
			if present != (row.Consumer.Prompt != nil) || (present && count != *row.Consumer.Prompt) {
				t.Fatal("meter projection differs", count, present)
			}
		})
	}
}
