package provider

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// This opt-in diagnostic records differences; it is not a parity acceptance test.
// Replace it with required cross-Source assertions when the native slice is ready.
func TestUsageMetadataShapeAudit(t *testing.T) {
	path := os.Getenv("MINILOOP_USAGE_METADATA_AUDIT")
	if path == "" {
		t.Skip("set MINILOOP_USAGE_METADATA_AUDIT to capture current differences")
	}
	data, err := os.ReadFile("../testdata/python-provider-usage-metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Mode, SSE string
			Response        string          `json:"response_json"`
			Expected        jsonvalue.Value `json:"event_usage"`
			Consumer        struct {
				Prompt *int `json:"prompt_tokens"`
			} `json:"consumer"`
			Stages []struct {
				Event string
				Usage jsonvalue.Value
			} `json:"usage_stages"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 156 {
		t.Fatal("Source inventory changed")
	}
	type stageObservation struct {
		Event    string          `json:"event"`
		Expected jsonvalue.Value `json:"expected"`
		Actual   jsonvalue.Value `json:"actual"`
		Matches  bool            `json:"matches"`
	}
	type observation struct {
		Name           string             `json:"name"`
		Mode           string             `json:"mode"`
		Error          string             `json:"error,omitempty"`
		Phase          string             `json:"phase"`
		Stopped        bool               `json:"stopped"`
		Stages         []stageObservation `json:"stages"`
		Expected       jsonvalue.Value    `json:"expected"`
		Actual         jsonvalue.Value    `json:"actual"`
		FinalMatches   bool               `json:"final_matches"`
		ExpectedPrompt *int               `json:"expected_prompt"`
		ActualPrompt   *int               `json:"actual_prompt"`
		PromptMatches  bool               `json:"prompt_matches"`
	}
	rows := []observation{}
	client, err := New(Config{APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	encode := func(usage protocol.TokenUsage) jsonvalue.Value {
		encoded, err := json.Marshal(usage.WithSDKDefaults())
		if err != nil {
			t.Fatal(err)
		}
		value, err := jsonvalue.Decode(string(encoded))
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	for _, row := range fixture.Cases {
		result := observation{Name: row.Name, Mode: row.Mode, Expected: row.Expected,
			ExpectedPrompt: row.Consumer.Prompt, Stages: []stageObservation{}}
		var reply protocol.ModelReply
		if row.Mode == "direct" {
			result.Phase = "direct"
			reply, err = decodeReply([]byte(row.Response))
			result.Stopped = err == nil
		} else {
			state := streamAssembly{}
			stage := 0
			for _, frame := range strings.Split(row.SSE, "\n\n") {
				lines := strings.Split(frame, "\n")
				if len(lines) < 2 {
					continue
				}
				event := strings.TrimPrefix(lines[0], "event: ")
				result.Phase = event
				err = client.consumeStream(&state, event, []byte(strings.TrimPrefix(lines[1], "data: ")), nil)
				if err != nil {
					break
				}
				if event == "message_start" || event == "message_delta" {
					if stage >= len(row.Stages) || row.Stages[stage].Event != event {
						t.Fatal("stage inventory changed")
					}
					actual := encode(state.reply.Usage)
					result.Stages = append(result.Stages, stageObservation{event, row.Stages[stage].Usage, actual,
						sameUsageShape(actual, row.Stages[stage].Usage)})
					stage++
				}
			}
			reply = state.reply
			result.Stopped = state.stopped
		}
		if err != nil {
			result.Error = err.Error()
		} else {
			result.Actual = encode(reply.Usage)
			result.FinalMatches = sameUsageShape(result.Actual, result.Expected)
			if value, present := agent.PromptTokens(reply.Usage); present {
				result.ActualPrompt = &value
			}
			result.PromptMatches = (result.ActualPrompt == nil && result.ExpectedPrompt == nil) ||
				(result.ActualPrompt != nil && result.ExpectedPrompt != nil && *result.ActualPrompt == *result.ExpectedPrompt)
		}
		rows = append(rows, result)
	}
	encoded, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
