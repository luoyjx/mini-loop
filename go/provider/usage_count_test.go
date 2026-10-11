package provider

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func sameUsageShape(a, b jsonvalue.Value) bool {
	if a.Kind() != b.Kind() {
		return false
	}
	if a.Kind() == jsonvalue.Array {
		x, _ := a.Array()
		y, _ := b.Array()
		if len(x) != len(y) {
			return false
		}
		for i := range x {
			if !sameUsageShape(x[i], y[i]) {
				return false
			}
		}
		return true
	}
	if a.Kind() == jsonvalue.Object {
		if len(a.Keys()) != len(b.Keys()) {
			return false
		}
		for _, key := range a.Keys() {
			x, _ := a.Lookup(key)
			y, ok := b.Lookup(key)
			if !ok || !sameUsageShape(x, y) {
				return false
			}
		}
		return true
	}
	return a.PythonEqual(b)
}
func assertSDKUsageShape(t *testing.T, usage protocol.TokenUsage, expected jsonvalue.Value) {
	t.Helper()
	encoded, err := json.Marshal(usage.WithSDKDefaults())
	if err != nil {
		t.Fatal(err)
	}
	actual, err := jsonvalue.Decode(string(encoded))
	if err != nil || !sameUsageShape(actual, expected) {
		t.Fatal("SDK usage value or kind differs", string(encoded))
	}
}
func TestUsageCountsMatchActualSDKDirectAndStreamStages(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-usage-consumer.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Mode, SSE string
			Response        string          `json:"response_json"`
			Expected        jsonvalue.Value `json:"event_usage"`
			Stages          []struct {
				Event string
				Usage jsonvalue.Value
			} `json:"usage_stages"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 34 {
		t.Fatal("Source inventory changed")
	}
	client, err := New(Config{APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name+"/"+row.Mode, func(t *testing.T) {
			if row.Mode == "direct" {
				reply, err := decodeReply([]byte(row.Response))
				if err != nil {
					t.Fatal(err)
				}
				assertSDKUsageShape(t, reply.Usage, row.Expected)
				return
			}
			state := streamAssembly{}
			stage := 0
			for _, frame := range strings.Split(row.SSE, "\n\n") {
				lines := strings.Split(frame, "\n")
				if len(lines) < 2 {
					continue
				}
				event := strings.TrimPrefix(lines[0], "event: ")
				data := strings.TrimPrefix(lines[1], "data: ")
				if err := client.consumeStream(&state, event, []byte(data), nil); err != nil {
					t.Fatal(err)
				}
				if event == "message_start" || event == "message_delta" {
					if stage >= len(row.Stages) || row.Stages[stage].Event != event {
						t.Fatal("SDK stage inventory differs")
					}
					assertSDKUsageShape(t, state.reply.Usage, row.Stages[stage].Usage)
					stage++
				}
			}
			if !state.stopped || stage != len(row.Stages) {
				t.Fatal("stream did not finish every captured stage")
			}
			assertSDKUsageShape(t, state.reply.Usage, row.Expected)
		})
	}
}
