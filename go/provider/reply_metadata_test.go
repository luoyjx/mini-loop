package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestCompletedReplyMetadataMatchesSDKDirectAndStream(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-reply-metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Mode string
			Message    jsonvalue.Value
			SSE        string
			Expected   jsonvalue.Value `json:"expected_metadata"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 44 {
		t.Fatal("Source inventory changed", len(fixture.Cases))
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name+"/"+row.Mode, func(t *testing.T) {
			calls := 0
			httpClient := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				body, err := row.Message.MarshalJSON()
				if err != nil {
					t.Fatal(err)
				}
				headers := http.Header{}
				if row.Mode == "stream" {
					body = []byte(row.SSE)
					headers.Set("Content-Type", "text/event-stream")
				}
				return &http.Response{StatusCode: 200, Header: headers, Body: io.NopCloser(strings.NewReader(string(body))), Request: r}, nil
			})}
			retries := 0
			cfg := Config{APIKey: "fixture-key", BaseURL: "https://fixture.invalid", HTTPClient: httpClient, MaxRetries: &retries}
			request := protocol.ModelRequest{Model: "fixture-model", MaxTokens: 8, Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("hello")}}, Purpose: protocol.PurposeAgentTurn}
			var reply protocol.ModelReply
			if row.Mode == "stream" {
				client, err := NewStreaming(cfg)
				if err != nil {
					t.Fatal(err)
				}
				reply, err = client.CompleteStream(context.Background(), request, nil)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				client, err := New(cfg)
				if err != nil {
					t.Fatal(err)
				}
				reply, err = client.Complete(context.Background(), request)
				if err != nil {
					t.Fatal(err)
				}
			}
			if calls != 1 {
				t.Fatal("metadata decoding retried", calls)
			}
			for _, candidate := range []protocol.ModelReply{reply, reply.Clone()} {
				encoded, err := json.Marshal(candidate)
				if err != nil {
					t.Fatal(err)
				}
				actual, err := jsonvalue.Decode(string(encoded))
				if err != nil {
					t.Fatal(err)
				}
				for _, key := range row.Expected.Keys() {
					expected, _ := row.Expected.Lookup(key)
					found, present := actual.Lookup(key)
					if !present || !expected.PythonEqual(found) {
						t.Fatalf("%s differs: %s", key, encoded)
					}
				}
				var restored protocol.ModelReply
				if err := json.Unmarshal(encoded, &restored); err != nil {
					t.Fatal(err)
				}
				roundtrip, err := json.Marshal(restored)
				if err != nil {
					t.Fatal(err)
				}
				recovered, err := jsonvalue.Decode(string(roundtrip))
				if err != nil || !recovered.PythonEqual(actual) {
					t.Fatal("public reply roundtrip differs", string(roundtrip), err)
				}
			}
		})
	}
}
