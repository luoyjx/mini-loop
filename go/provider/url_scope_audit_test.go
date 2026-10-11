package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

// Diagnostic only: this inventories differences without treating them as parity.
func TestProviderURLScopeAudit(t *testing.T) {
	path := os.Getenv("MINILOOP_PROVIDER_URL_AUDIT_OUTPUT")
	if path == "" {
		t.Skip("URL scope diagnostic output was not requested")
	}
	baseline := os.Getenv("MINILOOP_PROVIDER_URL_AUDIT_BASELINE")
	if baseline == "" {
		t.Fatal("explicit production baseline required")
	}
	type call struct {
		URL           string  `json:"url"`
		RawPath       string  `json:"raw_path"`
		Method        string  `json:"method"`
		APIKey        *string `json:"api_key"`
		Authorization *string `json:"authorization"`
	}
	type fault struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type source struct {
		Name     string      `json:"name"`
		BaseURL  *string     `json:"base_url"`
		Mode     string      `json:"mode"`
		Identity Description `json:"identity"`
		Stage    string      `json:"stage"`
		Error    *fault      `json:"error"`
		Calls    []call      `json:"calls"`
		Text     string      `json:"text,omitempty"`
	}
	var fixture struct {
		SDK     string          `json:"sdk_version"`
		HTTPX   string          `json:"httpx_version"`
		Message json.RawMessage `json:"message"`
		SSE     string          `json:"sse"`
		Cases   []source        `json:"cases"`
	}
	data, err := os.ReadFile("../testdata/python-provider-url.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 68 {
		t.Fatal("Source URL inventory changed")
	}
	type observation struct {
		Source       source `json:"source"`
		Native       source `json:"native"`
		SameStage    bool   `json:"same_stage"`
		SameCalls    bool   `json:"same_calls"`
		SameIdentity bool   `json:"same_identity"`
	}
	rows := []observation{}
	for _, row := range fixture.Cases {
		native := source{Name: row.Name, BaseURL: row.BaseURL, Mode: row.Mode, Stage: "constructor", Calls: []call{}}
		cfg := Config{APIKey: "fixture-key"}
		if row.BaseURL != nil {
			cfg.BaseURL = *row.BaseURL
		}
		cfg.HTTPClient = &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
			key := req.Header.Get("x-api-key")
			c := call{URL: req.URL.String(), RawPath: req.URL.EscapedPath(), Method: req.Method, APIKey: &key}
			if auth := req.Header.Get("authorization"); auth != "" {
				c.Authorization = &auth
			}
			native.Calls = append(native.Calls, c)
			body := string(fixture.Message)
			header := http.Header{"Content-Type": []string{"application/json"}}
			if row.Mode == "stream" {
				body = fixture.SSE
				header.Set("Content-Type", "text/event-stream")
			}
			return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		})}
		request := protocol.ModelRequest{Model: "fixture-model", MaxTokens: 8, Purpose: protocol.PurposeAgentTurn, Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("hello")}}}
		var reply protocol.ModelReply
		if row.Mode == "stream" {
			var c *StreamingClient
			c, err = NewStreaming(cfg)
			if err == nil {
				native.Identity = c.Describe()
				native.Stage = "request"
				reply, err = c.CompleteStream(context.Background(), request, nil)
			}
		} else {
			var c *Client
			c, err = New(cfg)
			if err == nil {
				native.Identity = c.Describe()
				native.Stage = "request"
				reply, err = c.Complete(context.Background(), request)
			}
		}
		if err != nil {
			native.Error = &fault{Type: reflect.TypeOf(err).String(), Text: err.Error()}
		} else {
			native.Stage = "complete"
			if len(reply.Content) != 1 {
				t.Fatal("diagnostic response shape changed")
			}
			text, ok := reply.Content[0].Text()
			if !ok {
				t.Fatal("diagnostic response is not text")
			}
			native.Text = text.Text
		}
		rows = append(rows, observation{row, native, row.Stage == native.Stage, reflect.DeepEqual(row.Calls, native.Calls), row.Identity == native.Identity})
	}
	report := struct {
		Baseline string        `json:"go_baseline"`
		SDK      string        `json:"sdk_version"`
		HTTPX    string        `json:"httpx_version"`
		Cases    []observation `json:"cases"`
	}{baseline, fixture.SDK, fixture.HTTPX, rows}
	output, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, append(output, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
