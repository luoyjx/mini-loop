package sqlitestate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/httpapi"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/provider"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/trajectory"
)

// Compose independently captured SDK variants, extension members, an unknown
// discriminator and an arbitrary-size index. The integration server is local;
// it is not new SDK evidence or a production-provider deployment check.
func citationRestartPayloads(t *testing.T, streamed bool) ([]byte, []byte, string) {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-provider-citations.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Completed []struct {
			Name              string
			Input, Normalized json.RawMessage
		}
		Streams []struct {
			Name   string
			Events []json.RawMessage
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	var input, normalized, extra []json.RawMessage
	for _, name := range []string{"all_variants", "extra_members", "unknown_variant", "unbounded_index"} {
		found := false
		for _, c := range fixture.Completed {
			if c.Name != name {
				continue
			}
			found = true
			var raw, expected struct{ Citations []json.RawMessage }
			if err := json.Unmarshal(c.Input, &raw); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(c.Normalized, &expected); err != nil {
				t.Fatal(err)
			}
			input = append(input, raw.Citations...)
			normalized = append(normalized, expected.Citations...)
			if name != "all_variants" {
				extra = append(extra, raw.Citations...)
			}
		}
		if !found {
			t.Fatal("missing captured citation", name)
		}
	}
	block := func(citations []json.RawMessage, parsed bool) []byte {
		value := struct {
			Type, Text string
			Citations  []json.RawMessage
		}{"text", "prefix tail", citations}
		// Explicit wire field names keep this fixture independent of protocol encoding.
		encoded, err := json.Marshal(struct {
			Type      string            `json:"type"`
			Text      string            `json:"text"`
			Citations []json.RawMessage `json:"citations"`
		}{value.Type, value.Text, value.Citations})
		if err != nil {
			t.Fatal(err)
		}
		if parsed {
			encoded = append(encoded[:len(encoded)-1], []byte(`,"parsed_output":null}`)...)
		}
		return encoded
	}
	var wire strings.Builder
	if streamed {
		for _, stream := range fixture.Streams {
			if stream.Name != "null_then_deltas" {
				continue
			}
			write := func(event string, raw []byte) {
				var compact bytes.Buffer
				if err := json.Compact(&compact, raw); err != nil {
					t.Fatal(err)
				}
				fmt.Fprintf(&wire, "event: %s\ndata: %s\n\n", event, compact.Bytes())
			}
			for _, raw := range stream.Events {
				var tag struct{ Type string }
				if err := json.Unmarshal(raw, &tag); err != nil {
					t.Fatal(err)
				}
				if tag.Type == "content_block_stop" {
					for _, citation := range extra {
						delta := fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"citations_delta","citation":%s}}`, citation)
						write("content_block_delta", []byte(delta))
					}
				}
				write(tag.Type, raw)
			}
		}
		if wire.Len() == 0 {
			t.Fatal("missing captured stream")
		}
	}
	return block(input, false), block(normalized, streamed), wire.String()
}

func citationRestartEqual(t *testing.T, got, want []byte) {
	t.Helper()
	decode := func(data []byte) any {
		var value any
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	if !reflect.DeepEqual(decode(got), decode(want)) {
		t.Fatalf("citation metadata changed:\n%s\nwant:\n%s", got, want)
	}
}
func citationRestartAssistant(t *testing.T, messages []protocol.Message, expected []byte, count int) {
	t.Helper()
	found := 0
	for _, message := range messages {
		if message.Role != protocol.RoleAssistant {
			continue
		}
		blocks, ok := message.Content.Blocks()
		if !ok || len(blocks) != 1 {
			t.Fatal("assistant block changed")
		}
		encoded, err := json.Marshal(blocks[0])
		if err != nil {
			t.Fatal(err)
		}
		citationRestartEqual(t, encoded, expected)
		text, _ := blocks[0].Text()
		citations, ok := text.Citations().Items()
		if !ok || len(citations) != 8 {
			t.Fatal("citation variants lost")
		}
		index, ok := citations[7].Char()
		if !ok || index.DocumentIndex.Decimal() != "1"+strings.Repeat("0", 80) {
			t.Fatal("stored integer truncated")
		}
		found++
	}
	if found != count {
		t.Fatal("assistant history count", found, count)
	}
}

func TestCitationNativeSQLiteHTTPRecordingAndReopen(t *testing.T) {
	for _, streamed := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", streamed), func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			path := filepath.Join(root, "state.db")
			raw, expected, sse := citationRestartPayloads(t, streamed)
			registry := secrets.New(secrets.Config{})
			registry.RegisterValue("CITATION_VALUE", "fixture-secret")
			registry.RegisterValue("CITATION_KEY", "secret-key")
			masked, err := protocol.MaskedPythonJSON(json.RawMessage(expected), registry.MaskText, false, true)
			if err != nil {
				t.Fatal(err)
			}
			expectedMasked := []byte(masked)
			if bytes.Contains(expectedMasked, []byte("fixture-secret")) || bytes.Contains(expectedMasked, []byte("secret-key")) {
				t.Fatal("expected mask missing")
			}
			var mu sync.Mutex
			var requests [][]byte
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					http.Error(w, "read failed", 500)
					return
				}
				if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "fixture-key" {
					t.Error("provider request identity changed")
				}
				mu.Lock()
				requests = append(requests, bytes.Clone(body))
				mu.Unlock()
				if streamed {
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, sse)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"id":"citation-restart","type":"message","role":"assistant","model":"citation-model","content":[%s],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":2}}`, raw)
			}))
			defer upstream.Close()
			zero := 0
			cfg := provider.Config{BaseURL: upstream.URL, APIKey: "fixture-key", Timeout: 3 * time.Second, MaxRetries: &zero}
			makeProvider := func() agent.Provider {
				if streamed {
					value, err := provider.NewStreaming(cfg)
					if err != nil {
						t.Fatal(err)
					}
					return value
				}
				value, err := provider.New(cfg)
				if err != nil {
					t.Fatal(err)
				}
				return value
			}
			open := func() *Store {
				store, err := Open(ctx, path)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { store.Close() })
				return store
			}
			recordings, err := trajectory.New(trajectory.Config{Root: filepath.Join(root, "recordings"), CaptureContent: true})
			if err != nil {
				t.Fatal(err)
			}
			makeManager := func(store *Store) *agent.SessionManager {
				manager, err := agent.NewSessionManager(agent.ManagerConfig{
					WorkspaceRoot: filepath.Join(root, "workspaces"), Defaults: agent.SessionDefaults{Model: "citation-model", MaxTokens: 64, MaxRounds: 2, PermissionMode: agent.ModeAuto},
					Services: agent.ManagerServices{Provider: makeProvider(), Recovery: agent.DirectRecovery{}, StateStore: store, Secrets: registry, Trajectories: recordings},
				})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { stopRestartManager(t, manager) })
				return manager
			}
			auth, err := httpapi.NewTokenAuth([]httpapi.TokenBinding{{Token: "alice-token", Principal: "alice"}, {Token: "bob-token", Principal: "bob"}})
			if err != nil {
				t.Fatal(err)
			}
			serve := func(manager *agent.SessionManager) *httptest.Server {
				api, err := httpapi.New(httpapi.Config{Manager: manager, Auth: auth})
				if err != nil {
					t.Fatal(err)
				}
				server := httptest.NewServer(api)
				t.Cleanup(server.Close)
				return server
			}
			client := &http.Client{Timeout: 5 * time.Second}
			defer client.CloseIdleConnections()
			exchange := func(server *httptest.Server, method, route, body, token string, expectedCode int) []byte {
				r, err := http.NewRequest(method, server.URL+route, strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				r.Header.Set("Authorization", "Bearer "+token)
				r.Header.Set("Content-Type", "application/json")
				response, err := client.Do(r)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				data, err := io.ReadAll(response.Body)
				if err != nil {
					t.Fatal(err)
				}
				if response.StatusCode != expectedCode {
					t.Fatalf("%s %s: %d %s", method, route, response.StatusCode, data)
				}
				return data
			}
			store := open()
			manager := makeManager(store)
			api := serve(manager)
			var created httpapi.SessionInfo
			if err := json.Unmarshal(exchange(api, "POST", "/sessions", `{}`, "alice-token", 200), &created); err != nil {
				t.Fatal(err)
			}
			route := "/sessions/" + string(created.ID)
			run := func(server *httptest.Server, prompt string) {
				exchange(server, "POST", route+"/messages", `{"message":"`+prompt+`"}`, "alice-token", 200)
			}
			run(api, "first")
			session, err := manager.Get("alice", created.ID)
			if err != nil {
				t.Fatal(err)
			}
			citationRestartAssistant(t, session.Messages(), expected, 1)
			run(api, "second")
			citationRestartAssistant(t, session.Messages(), expected, 2)
			var history agent.TranscriptSnapshot
			if err := json.Unmarshal(exchange(api, "GET", route+"/transcript", "", "alice-token", 200), &history); err != nil {
				t.Fatal(err)
			}
			citationRestartAssistant(t, history.Messages, expectedMasked, 2)
			exchange(api, "GET", route+"/transcript", "", "bob-token", 404)
			// Inspect actual SQL payloads, not just the typed read projection.
			rows, err := store.conn.QueryContext(ctx, "SELECT payload FROM messages WHERE session_id=?", created.ID)
			if err != nil {
				t.Fatal(err)
			}
			storedRows := 0
			for rows.Next() {
				var payload string
				if err := rows.Scan(&payload); err != nil {
					t.Fatal(err)
				}
				storedRows++
				if strings.Contains(payload, "fixture-secret") || strings.Contains(payload, "secret-key") {
					t.Fatal("raw citation reached SQL")
				}
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			if storedRows != len(history.Messages) {
				t.Fatal("SQL rows missing")
			}
			api.Close()
			stopRestartManager(t, manager)
			if _, held, err := store.LeaseHolder(ctx, created.ID); err != nil || held {
				t.Fatal("stop retained lease", err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := store.LoadMessages(ctx, created.ID, nil); err != ErrClosed {
				t.Fatal("old store still readable", err)
			}
			store = open()
			manager = makeManager(store)
			api = serve(manager)
			if _, err := manager.Get("alice", created.ID); err == nil {
				t.Fatal("constructor restored implicitly")
			}
			restored, err := manager.RestoreSessions(ctx)
			if err != nil || len(restored) != 1 {
				t.Fatal("explicit recovery", err)
			}
			if restored[0].Owner() != "alice" || restored[0].ID() != created.ID {
				t.Fatal("restored identity changed")
			}
			citationRestartAssistant(t, restored[0].Messages(), expectedMasked, 2)
			exchange(api, "GET", route+"/transcript", "", "bob-token", 404)
			run(api, "resumed")
			if status := restored[0].PersistenceStatus(); status.Error != nil || !status.Restored || !status.LeaseConfirmed {
				t.Fatal("restored writes degraded", status)
			}
			// Live second-turn requests carry original data; restored requests carry the
			// durable masked projection. Strip only cache annotations at the test boundary.
			mu.Lock()
			captured := append([][]byte{}, requests...)
			mu.Unlock()
			if len(captured) != 3 {
				t.Fatal("unexpected provider calls", len(captured))
			}
			for phase, index := range []int{1, 2} {
				var envelope struct {
					Messages []struct {
						Role    string
						Content json.RawMessage
					}
				}
				if err := json.Unmarshal(captured[index], &envelope); err != nil {
					t.Fatal(err)
				}
				found := 0
				for _, message := range envelope.Messages {
					if message.Role != "assistant" {
						continue
					}
					var blocks []map[string]json.RawMessage
					if err := json.Unmarshal(message.Content, &blocks); err != nil {
						t.Fatal(err)
					}
					for _, block := range blocks {
						delete(block, "cache_control")
						actual, _ := json.Marshal(block)
						want := expected
						if phase == 1 {
							want = expectedMasked
						}
						citationRestartEqual(t, actual, want)
						found++
					}
				}
				if found != index {
					t.Fatal("forwarded assistant rows missing", found, index)
				}
			}
			api.Close()
			stopRestartManager(t, manager)
			summaries, err := recordings.List(agent.TrajectoryQuery{Session: &created.ID, Limit: 10})
			if err != nil || len(summaries) != 3 {
				t.Fatal("recordings missing", err, len(summaries))
			}
			files, err := filepath.Glob(filepath.Join(root, "recordings", "*.jsonl"))
			if err != nil || len(files) != 3 {
				t.Fatal("raw trajectory files missing", err, len(files))
			}
			for _, file := range files {
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(data, []byte("fixture-secret")) || bytes.Contains(data, []byte("secret-key")) {
					t.Fatal("raw citation reached trajectory file")
				}
				if !bytes.Contains(data, []byte(`"model_output"`)) || !bytes.Contains(data, []byte(`"citations"`)) {
					t.Fatal("raw trajectory omitted citation output")
				}
				info, err := os.Stat(file)
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatal("recording permissions", err)
				}
			}
			for _, summary := range summaries {
				encoded, err := recordings.JSON(summary.ID, 8*1024*1024)
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(encoded, []byte("fixture-secret")) || bytes.Contains(encoded, []byte("secret-key")) {
					t.Fatal("citation leaked into recording")
				}
				var recorded struct {
					Events []struct {
						ModelOutput json.RawMessage `json:"model_output"`
					}
				}
				if err := json.Unmarshal(encoded, &recorded); err != nil {
					t.Fatal(err)
				}
				outputs := 0
				for _, event := range recorded.Events {
					if len(event.ModelOutput) > 0 {
						outputs++
						citationRestartEqual(t, event.ModelOutput, append(append([]byte("["), expectedMasked...), ']'))
					}
				}
				if outputs != 1 {
					t.Fatal("recording lost citation model output", outputs, string(encoded))
				}
			}
		})
	}
}
