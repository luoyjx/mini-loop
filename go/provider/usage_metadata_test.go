package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestProviderUsageMatchesActualSDKDirectAndStream(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-usage.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Mode string
			Wire       jsonvalue.Value `json:"wire_usage"`
			Expected   jsonvalue.Value `json:"event_usage"`
			Request    jsonvalue.Value
			Delta      jsonvalue.Value `json:"delta_usage"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 9 {
		t.Fatal("Source inventory changed")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name+"/"+row.Mode, func(t *testing.T) {
			calls := 0
			httpClient := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				sent, _ := io.ReadAll(r.Body)
				value, err := jsonvalue.Decode(string(sent))
				if err != nil || !value.PythonEqual(row.Request) {
					t.Fatal("SDK request differs", string(sent))
				}
				usage, _ := row.Wire.MarshalJSON()
				body := `{"id":"fixture","type":"message","role":"assistant","model":"fixture-model","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","stop_sequence":null,"usage":` + string(usage) + `}`
				headers := http.Header{}
				if row.Mode == "stream" {
					fields := []jsonvalue.Field{}
					for _, key := range row.Wire.Keys() {
						v, _ := row.Wire.Lookup(key)
						fields = append(fields, jsonvalue.Field{Name: key, Value: v})
					}
					fields = append(fields, jsonvalue.Field{Name: "output_tokens", Value: jsonvalue.IntegerValue(0)})
					startUsage, _ := jsonvalue.ObjectValue(fields).MarshalJSON()
					delta := row.Delta
					if delta.Kind() == jsonvalue.Null {
						delta = jsonvalue.ObjectValue([]jsonvalue.Field{{Name: "output_tokens", Value: jsonvalue.IntegerValue(5)}})
					}
					deltaWire, _ := delta.MarshalJSON()
					body = "event: message_start\ndata: " + `{"type":"message_start","message":{"id":"fixture","type":"message","role":"assistant","model":"fixture-model","content":[],"usage":` + string(startUsage) + `}}` + "\n\n" +
						"event: content_block_start\ndata: " + `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n" +
						"event: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}` + "\n\n" +
						"event: content_block_stop\ndata: " + `{"type":"content_block_stop","index":0}` + "\n\n" +
						"event: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":` + string(deltaWire) + `}` + "\n\n" +
						"event: message_stop\ndata: " + `{"type":"message_stop"}` + "\n\n"
					headers.Set("Content-Type", "text/event-stream")
				}
				return &http.Response{StatusCode: 200, Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			retries := 0
			cfg := Config{APIKey: "fixture-key", BaseURL: "https://fixture.invalid", HTTPClient: httpClient, MaxRetries: &retries}
			request := protocol.ModelRequest{Model: "fixture-model", MaxTokens: 8, Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("hello")}}, Purpose: protocol.PurposeAgentTurn}
			var reply protocol.ModelReply
			if row.Mode == "stream" {
				client, e := NewStreaming(cfg)
				if e != nil {
					t.Fatal(e)
				}
				reply, err = client.CompleteStream(context.Background(), request, nil)
			} else {
				client, e := New(cfg)
				if e != nil {
					t.Fatal(e)
				}
				reply, err = client.Complete(context.Background(), request)
			}
			if calls != 1 {
				t.Fatal("usage parsing retried HTTP", calls)
			}
			if row.Name == "scalar-strings" {
				var failure *Failure
				if !errors.As(err, &failure) || failure.Kind != FailureProtocol {
					t.Fatal("native integer adaptation changed", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(reply.Usage)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := jsonvalue.Decode(string(encoded))
			if err != nil || !actual.PythonEqual(row.Expected) {
				t.Fatal("SDK usage projection differs", string(encoded))
			}
		})
	}
}
