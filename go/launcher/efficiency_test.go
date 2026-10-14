package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/httpapi"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type efficiencyHTTPResult struct {
	ID      string `json:"id"`
	Bytes   int    `json:"bytes"`
	ANSI    bool   `json:"ansi"`
	Marker  bool   `json:"marker"`
	FullRaw bool   `json:"full_raw"`
}
type efficiencyHTTPObservation struct {
	MaxTokens          int                   `json:"max_tokens"`
	RecoveryTool       bool                  `json:"recovery_tool"`
	ConciseInstruction bool                  `json:"concise_instruction"`
	Result             *efficiencyHTTPResult `json:"result"`
}
type efficiencyHTTPCase struct {
	Name, Mode, Style, Permission, Command, Output string
	Persist                                        bool
	Minimum                                        int
	Requests                                       []efficiencyHTTPObservation
	Receipts                                       []agent.OptimizationMetrics
	StorePresent                                   bool            `json:"store_present"`
	Limits                                         *[3]json.Number `json:"limits"`
	StoreClosed                                    bool            `json:"store_closed"`
}
type efficiencyHTTPRequest struct {
	guardianHTTPRequest
	MaxTokens int `json:"max_tokens"`
}

// Use the production provider, listener, manager, gate and native shell. Only the
// remote model is scripted; the recovery reference comes from live session events.
func TestEfficiencyLauncherMatchesSourceHTTP(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-efficiency-startup.json")
	if err != nil {
		t.Fatal(err)
	}
	fixture := decode[struct{ Rows []efficiencyHTTPCase }](t, data)
	if len(fixture.Rows) != 9 {
		t.Fatal("Source startup inventory drift")
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			requests := make(chan efficiencyHTTPObservation, 10)
			var session atomic.Pointer[agent.ManagedSession]
			var rounds atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request efficiencyHTTPRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					http.Error(w, "fixture request", 400)
					return
				}
				observation := efficiencyHTTPObservation{MaxTokens: request.MaxTokens, ConciseInstruction: strings.Contains(guardianWireText(request.System), "Answer concisely.")}
				for _, schema := range request.Tools {
					if schema.Name == protocol.ToolReadTokenArtifact {
						observation.RecoveryTool = true
					}
				}
				for _, message := range request.Messages {
					blocks, _ := message.Content.Array()
					for _, block := range blocks {
						kind, _ := block.Lookup("type")
						label, _ := kind.Text()
						if label != "tool_result" {
							continue
						}
						value, _ := block.Lookup("content")
						content, _ := value.Text()
						identifier, _ := block.Lookup("tool_use_id")
						id, _ := identifier.Text()
						observation.Result = &efficiencyHTTPResult{ID: id, Bytes: len(content), ANSI: strings.Contains(content, "\x1b["), Marker: strings.Contains(content, "read_token_artifact"), FullRaw: strings.Count(content, "NOTICE duplicate") == 300}
					}
				}
				requests <- observation
				content, reason := []protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn
				round := rounds.Add(1)
				if round == 1 {
					content, reason = []protocol.Block{protocol.NewToolUse("command", protocol.BashToolInput(protocol.BashInput{Command: row.Command}))}, protocol.StopToolUse
				} else if round == 2 {
					for _, record := range session.Load().Events() {
						if receipt, ok := record.Event.OptimizationReceipt(); ok && receipt.RawRef != nil {
							content, reason = []protocol.Block{protocol.NewToolUse("recover", protocol.ReadTokenArtifactToolInput(protocol.ReadTokenArtifactInput{RawRef: *receipt.RawRef}))}, protocol.StopToolUse
						}
					}
				}
				wire, err := json.Marshal(content)
				if err != nil {
					t.Error(err)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"id":"reply","type":"message","role":"assistant","model":"configured","content":%s,"stop_reason":%q,"usage":{"input_tokens":5,"output_tokens":3}}`, wire, reason)
			}))
			defer upstream.Close()
			settings := settingsFor(t, map[string]string{
				"MINILOOP_FAKE_LLM": "0", "ANTHROPIC_API_KEY": "fixture-key", "ANTHROPIC_BASE_URL": upstream.URL, "MODEL_ID": "configured",
				"MINILOOP_SKILLS_DIR": t.TempDir() + "/empty", "MINILOOP_MAX_TOKENS": "4000", "MINILOOP_MAX_TURNS": "4",
				"MINILOOP_TOKEN_EFFICIENCY_MODE": row.Mode, "MINILOOP_TOKEN_EFFICIENCY_RESPONSE_STYLE": row.Style,
				"MINILOOP_TOKEN_EFFICIENCY_PERSIST_RAW": strconv.FormatBool(row.Persist), "MINILOOP_TOKEN_EFFICIENCY_RAW_MIN_BYTES": strconv.Itoa(row.Minimum),
				"MINILOOP_TOKEN_EFFICIENCY_ARTIFACT_TTL_SECONDS": "7", "MINILOOP_TOKEN_EFFICIENCY_MAX_ARTIFACT_BYTES": "20000", "MINILOOP_TOKEN_EFFICIENCY_MAX_TOTAL_BYTES": "40000",
			})
			app, err := New(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer stopApp(t, app)
			base, cancel, done := startApp(t, app)
			defer cancel()
			client := &http.Client{Timeout: 10 * time.Second}
			defer client.CloseIdleConnections()
			code, body := exchange(t, client, "POST", base+"/sessions", fmt.Sprintf(`{"mode":%q}`, row.Permission), "")
			if code != 200 {
				t.Fatal(code, string(body))
			}
			info := decode[httpapi.SessionInfo](t, body)
			managed, err := app.manager.Get("anonymous", info.ID)
			if err != nil {
				t.Fatal(err)
			}
			session.Store(managed)
			code, body = exchange(t, client, "POST", base+"/sessions/"+string(info.ID)+"/messages", `{"message":"inspect command output"}`, "")
			if code != 200 || decode[httpapi.MessageResponse](t, body).Final != row.Output {
				t.Fatal(code, string(body))
			}
			observed := []efficiencyHTTPObservation{}
			for len(requests) > 0 {
				observed = append(observed, <-requests)
			}
			if !reflect.DeepEqual(observed, row.Requests) {
				t.Fatalf("HTTP requests differ\ngot %+v\nwant %+v", observed, row.Requests)
			}
			receipts := []agent.OptimizationMetrics{}
			for _, record := range managed.Events() {
				if receipt, ok := record.Event.OptimizationReceipt(); ok {
					receipt.ElapsedMS = 0
					if receipt.RawRef != nil {
						value := "<raw>"
						receipt.RawRef = &value
					}
					if receipt.ParentSpanID != nil {
						value := agent.SpanID("<tool>")
						receipt.ParentSpanID = &value
					}
					receipts = append(receipts, receipt)
				}
			}
			if !reflect.DeepEqual(receipts, row.Receipts) {
				got, _ := json.Marshal(receipts)
				want, _ := json.Marshal(row.Receipts)
				t.Fatalf("HTTP receipts differ\ngot %s\nwant %s", got, want)
			}
			storeStatus, present := managed.RawArtifactStoreStatus()
			if present != row.StorePresent || (row.Limits != nil) != present {
				t.Fatalf("effective store presence: native=%t Source=%t", present, row.StorePresent)
			}
			if present {
				ttl, err := row.Limits[0].Float64()
				maximum, validItem := new(big.Int).SetString(string(row.Limits[1]), 10)
				total, validTotal := new(big.Int).SetString(string(row.Limits[2]), 10)
				if err != nil || !validItem || !validTotal {
					t.Fatal("invalid Source artifact limits")
				}
				if storeStatus.Closed || storeStatus.Limits.TTLSeconds != ttl || storeStatus.Limits.MaxArtifactBytes.Cmp(maximum) != 0 || storeStatus.Limits.MaxTotalBytes.Cmp(total) != 0 {
					t.Fatalf("actual launcher store limits differ: native=%+v Source=%v", storeStatus, row.Limits)
				}
			}
			cancel()
			stopped(t, done)
			closedStatus, afterPresent := managed.RawArtifactStoreStatus()
			if afterPresent != present || (!afterPresent || closedStatus.Closed) != row.StoreClosed {
				t.Fatalf("actual store shutdown: native=%+v present=%t SourceClosed=%t", closedStatus, afterPresent, row.StoreClosed)
			}
			state := app.manager.EfficiencyStatus()
			if state.Initialize == nil || state.Close == nil || state.InitializeError != nil || state.CloseError != nil {
				t.Fatalf("manager lifecycle: %+v", state)
			}
		})
	}
}
