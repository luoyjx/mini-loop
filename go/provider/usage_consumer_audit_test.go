package provider

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// Explicit diagnostic observations, not a parity gate accepting mismatches.
func TestProviderUsageConsumerAudit(t *testing.T) {
	path := os.Getenv("MINILOOP_USAGE_AUDIT_OUTPUT")
	if path == "" {
		t.Skip("usage consumer diagnostic output was not requested")
	}
	baseline := os.Getenv("MINILOOP_USAGE_AUDIT_BASELINE")
	if baseline == "" {
		t.Fatal("explicit production baseline required")
	}
	data, err := os.ReadFile("../testdata/python-provider-usage-consumer.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		SDK      string `json:"sdk_version"`
		Pydantic string `json:"pydantic_version"`
		Cases    []struct {
			Name, Mode, SSE string
			Response        string          `json:"response_json"`
			Usage           jsonvalue.Value `json:"event_usage"`
			Consumer        struct {
				Prompt *int `json:"prompt_tokens"`
			} `json:"consumer"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 34 {
		t.Fatal("Source consumer inventory changed")
	}
	type observation struct {
		Name           string               `json:"name"`
		Mode           string               `json:"mode"`
		Expected       jsonvalue.Value      `json:"expected_usage"`
		Native         *protocol.TokenUsage `json:"native_usage,omitempty"`
		NativeError    string               `json:"native_error,omitempty"`
		ExpectedPrompt *int                 `json:"expected_prompt_tokens"`
		NativePrompt   *int                 `json:"native_prompt_tokens"`
		SameEvent      bool                 `json:"same_event"`
		SamePrompt     bool                 `json:"same_prompt"`
	}
	rows := []observation{}
	client, err := New(Config{APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		o := observation{Name: row.Name, Mode: row.Mode, Expected: row.Usage, ExpectedPrompt: row.Consumer.Prompt}
		var reply protocol.ModelReply
		if row.Mode == "stream" {
			reply, err = client.readStream(context.Background(), strings.NewReader(row.SSE), nil)
		} else {
			reply, err = decodeReply([]byte(row.Response))
		}
		if err != nil {
			o.NativeError = err.Error()
		} else {
			usage := reply.Usage.WithSDKDefaults()
			o.Native = &usage
			encoded, e := json.Marshal(usage)
			if e != nil {
				t.Fatal(e)
			}
			actual, e := jsonvalue.Decode(string(encoded))
			if e != nil {
				t.Fatal(e)
			}
			o.SameEvent = actual.PythonEqual(row.Usage)
			// Numeric equality alone hides bool/float/integer event kinds.
			for _, key := range []string{"input_tokens", "output_tokens", "cache_read_input_tokens", "cache_creation_input_tokens"} {
				a, _ := actual.Lookup(key)
				b, _ := row.Usage.Lookup(key)
				o.SameEvent = o.SameEvent && a.Kind() == b.Kind()
			}
			if count, ok := agent.PromptTokens(usage); ok {
				o.NativePrompt = &count
			}
			o.SamePrompt = (o.NativePrompt == nil && o.ExpectedPrompt == nil) || (o.NativePrompt != nil && o.ExpectedPrompt != nil && *o.NativePrompt == *o.ExpectedPrompt)
		}
		rows = append(rows, o)
	}
	report := struct {
		Baseline string        `json:"go_baseline"`
		SDK      string        `json:"sdk_version"`
		Pydantic string        `json:"pydantic_version"`
		Cases    []observation `json:"cases"`
	}{baseline, fixture.SDK, fixture.Pydantic, rows}
	output, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(output, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
