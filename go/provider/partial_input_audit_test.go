package provider

import (
	"context"
	"encoding/json"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"os"
	"strings"
	"testing"
)

// TestProviderPartialInputAudit emits diagnostic observations, not a parity gate.
// Set MINILOOP_PROVIDER_AUDIT_OUTPUT to a report path to run it explicitly.
func TestProviderPartialInputAudit(t *testing.T) {
	outputPath := os.Getenv("MINILOOP_PROVIDER_AUDIT_OUTPUT")
	if outputPath == "" {
		t.Skip("diagnostic provider audit output was not requested")
	}
	baseline := os.Getenv("MINILOOP_PROVIDER_AUDIT_BASELINE")
	if baseline == "" {
		t.Fatal("diagnostic audit requires an explicit production baseline")
	}

	data, err := os.ReadFile("../testdata/python-provider-partial-input.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		SDK   string `json:"sdk_version"`
		Jiter string `json:"jiter_version"`
		Cases []struct {
			Kind, Name, SSE string
			Expected        string `json:"expected_json"`
			Error           *struct{ Class, Message string }
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	type observation struct {
		Kind               string `json:"kind"`
		Name               string `json:"name"`
		SDKError           bool   `json:"sdk_error"`
		NativeError        string `json:"native_error,omitempty"`
		NativeFailureEvent string `json:"native_failure_event,omitempty"`
		NativeDeltaCount   int    `json:"native_delta_count"`
		SameOutcome        bool   `json:"same_outcome"`
		SDKJSON            string `json:"sdk_json,omitempty"`
		NativeJSON         string `json:"native_json,omitempty"`
	}
	rows := []observation{}
	client, err := New(Config{APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		o := observation{Kind: row.Kind, Name: row.Name, SDKError: row.Error != nil, SDKJSON: row.Expected}
		reply, err := client.readStream(context.Background(), strings.NewReader(row.SSE), nil)
		if err != nil {
			o.NativeError = err.Error()
			state := streamAssembly{}
			for _, frame := range strings.Split(row.SSE, "\n\n") {
				lines := strings.Split(frame, "\n")
				if len(lines) < 2 {
					continue
				}
				event := strings.TrimPrefix(lines[0], "event: ")
				data := strings.TrimPrefix(lines[1], "data: ")
				if event == "content_block_delta" {
					o.NativeDeltaCount++
				}
				if client.consumeStream(&state, event, []byte(data), nil) != nil {
					o.NativeFailureEvent = event
					break
				}
			}
			o.SameOutcome = o.SDKError
		} else {
			encoded, err := protocol.MarshalObservedBlocks(reply.Content)
			if err != nil {
				t.Fatal(err)
			}
			o.NativeJSON = string(encoded)
			a, err := jsonvalue.Decode(o.NativeJSON)
			if err != nil {
				t.Fatal(err)
			}
			b, err := jsonvalue.Decode(row.Expected)
			o.SameOutcome = err == nil && a.PythonEqual(b)
			if err == nil && !o.SameOutcome {
				actual, actualErr := a.Sorted().MarshalLegacyUTF8()
				expected, expectedErr := b.Sorted().MarshalLegacyUTF8()
				o.SameOutcome = actualErr == nil && expectedErr == nil && string(actual) == string(expected)
			}
		}
		rows = append(rows, o)
	}
	report := struct {
		Baseline string        `json:"go_baseline"`
		SDK      string        `json:"sdk_version"`
		Jiter    string        `json:"jiter_version"`
		Cases    []observation `json:"cases"`
	}{baseline, fixture.SDK, fixture.Jiter, rows}
	output, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outputPath, append(output, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
