package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/httpapi"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type guardianHTTPCase struct {
	Name, Answer, Mode, Command, Output    string
	Enabled, Human, Hostile, Fault, Parked bool
	CanaryExists                           bool                  `json:"canary_exists"`
	ParentNames                            [][]protocol.ToolName `json:"parent_names"`
	ParentResults                          []string              `json:"parent_results"`
	ReviewNames                            [][]protocol.ToolName `json:"review_names"`
	ReviewResults                          []string              `json:"review_results"`
	ReviewPrompts                          []string              `json:"review_prompts"`
}

type guardianHTTPRequest struct {
	launcherMCPRequest
	System jsonvalue.Value
}

func guardianWireText(value jsonvalue.Value) string {
	if text, ok := value.Text(); ok {
		return text
	}
	blocks, _ := value.Array()
	var text string
	for _, block := range blocks {
		field, _ := block.Lookup("text")
		part, _ := field.Text()
		text += part
	}
	return text
}

func TestGuardianLauncherMatchesSourceHTTP(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-guardian-runtime.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ HTTP []guardianHTTPCase }
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.HTTP) != 11 {
		t.Fatal("Source HTTP inventory changed")
	}
	for _, row := range fixture.HTTP {
		t.Run(row.Name, func(t *testing.T) {
			requests := make(chan guardianHTTPRequest, 10)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request guardianHTTPRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					http.Error(w, "bad fixture request", 400)
					return
				}
				requests <- request
				results := launcherMCPResults(request.Messages)
				reviewing := strings.Contains(guardianWireText(request.System), "You are a security reviewer.")
				w.Header().Set("Content-Type", "application/json")
				if reviewing && row.Fault {
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprintf(w, `{"error":{"type":"fixture_error","message":%q}}`, row.Answer)
					return
				}
				content, reason := []protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn
				if reviewing {
					content = []protocol.Block{protocol.NewTextBlock(row.Answer)}
					if row.Hostile && len(results) == 0 {
						input, err := protocol.DecodeToolInput(protocol.ToolWriteFile, []byte(`{"path":"planted.txt","content":"x"}`))
						if err != nil {
							t.Error(err)
							return
						}
						content, reason = []protocol.Block{protocol.NewToolUse("review-write", input)}, protocol.StopToolUse
					}
				} else if len(results) == 0 {
					content, reason = []protocol.Block{protocol.NewToolUse("action", protocol.BashToolInput(protocol.BashInput{Command: row.Command}))}, protocol.StopToolUse
				}
				wire, err := json.Marshal(content)
				if err != nil {
					t.Error(err)
					return
				}
				fmt.Fprintf(w, `{"id":"guardian-reply","type":"message","role":"assistant","model":"configured","content":%s,"stop_reason":%q,"usage":{"input_tokens":5,"output_tokens":3}}`, wire, reason)
			}))
			defer upstream.Close()
			flag := "0"
			if row.Enabled {
				flag = "1"
			}
			settings := settingsFor(t, map[string]string{"MINILOOP_FAKE_LLM": "0", "ANTHROPIC_API_KEY": "fixture-key", "ANTHROPIC_BASE_URL": upstream.URL, "MODEL_ID": "configured", "MINILOOP_GUARDIAN": flag})
			app, err := New(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer stopApp(t, app)
			base, cancel, done := startApp(t, app)
			defer cancel()
			client := &http.Client{Timeout: 5 * time.Second}
			defer client.CloseIdleConnections()
			code, body := exchange(t, client, "POST", base+"/sessions", fmt.Sprintf(`{"mode":%q}`, row.Mode), "")
			if code != 200 {
				t.Fatal(code, string(body))
			}
			info := decode[httpapi.SessionInfo](t, body)
			canary := filepath.Join(info.Workspace, "fixture-canary.txt")
			if err := os.WriteFile(canary, []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			type turnResult struct {
				code int
				body []byte
			}
			finished := make(chan turnResult, 1)
			go func() {
				code, body := exchange(t, client, "POST", base+"/sessions/"+string(info.ID)+"/messages", `{"message":"review fixture action"}`, "")
				finished <- turnResult{code, body}
			}()
			var final turnResult
			parked := false
			deadline := time.NewTimer(5 * time.Second)
			defer deadline.Stop()
			waiting := true
			for waiting {
				select {
				case final = <-finished:
					waiting = false
				case <-deadline.C:
					t.Fatal("HTTP Guardian turn did not finish")
				case <-time.After(time.Millisecond):
					code, body := exchange(t, client, "GET", base+"/sessions/"+string(info.ID)+"/approvals", "", "")
					if code != 200 {
						t.Fatal(code, string(body))
					}
					var pending struct {
						Approvals []struct {
							ApprovalID agent.ApprovalID `json:"approval_id"`
						}
					}
					if err := json.Unmarshal(body, &pending); err != nil {
						t.Fatal(err)
					}
					if len(pending.Approvals) > 0 {
						if parked {
							t.Fatal("unexpected second human question")
						}
						parked = true
						decision := "deny"
						if row.Human {
							decision = "allow"
						}
						code, body = exchange(t, client, "POST", base+"/sessions/"+string(info.ID)+"/approvals/"+string(pending.Approvals[0].ApprovalID), fmt.Sprintf(`{"decision":%q}`, decision), "")
						if code != 200 {
							t.Fatal(code, string(body))
						}
					}
				}
			}
			if final.code != 200 || decode[httpapi.MessageResponse](t, final.body).Final != row.Output || parked != row.Parked {
				t.Fatal(final.code, string(final.body), parked)
			}
			_, err = os.Stat(canary)
			if (!os.IsNotExist(err)) != row.CanaryExists {
				t.Fatal("approval disagrees with actual workspace effect", err)
			}
			if _, err := os.Stat(filepath.Join(info.Workspace, "planted.txt")); !os.IsNotExist(err) {
				t.Fatal("auditor wrote a file", err)
			}
			parentNames, reviewNames := [][]protocol.ToolName{}, [][]protocol.ToolName{}
			parentResults, reviewResults, reviewPrompts := []string{}, []string{}, []string{}
			for len(requests) > 0 {
				request := <-requests
				names := []protocol.ToolName{}
				for _, schema := range request.Tools {
					names = append(names, schema.Name)
				}
				if strings.Contains(guardianWireText(request.System), "You are a security reviewer.") {
					reviewNames = append(reviewNames, names)
					if len(request.Messages) == 0 {
						t.Fatal("empty review request")
					}
					reviewPrompts = append(reviewPrompts, guardianWireText(request.Messages[0].Content))
					reviewResults = append(reviewResults, launcherMCPResults(request.Messages)...)
				} else {
					parentNames = append(parentNames, names)
					parentResults = append(parentResults, launcherMCPResults(request.Messages)...)
				}
			}
			if !reflect.DeepEqual(parentNames, row.ParentNames) || !reflect.DeepEqual(reviewNames, row.ReviewNames) || !reflect.DeepEqual(parentResults, row.ParentResults) || !reflect.DeepEqual(reviewResults, row.ReviewResults) || !reflect.DeepEqual(reviewPrompts, row.ReviewPrompts) {
				t.Fatal("model-visible HTTP observations differ", parentNames, reviewNames, parentResults, reviewResults, reviewPrompts)
			}
			if len(app.manager.Approvals().List(info.ID)) != 0 {
				t.Fatal("approval remained after turn")
			}
			cancel()
			stopped(t, done)
		})
	}
}

func TestGuardianInspectionDoesNotAllocateAndDefaultRemainsOff(t *testing.T) {
	settings := settingsFor(t, nil)
	if settings.GuardianEnabled || Inspect(settings, config.ServerSettings{}, nil).GuardianEnabled {
		t.Fatal("default Guardian enabled")
	}
	settings = settingsFor(t, map[string]string{"MINILOOP_GUARDIAN": "yes"})
	if !Inspect(settings, config.ServerSettings{}, nil).GuardianEnabled || settings.RequireSupported() != nil {
		t.Fatal("Guardian selection not reported")
	}
	if _, err := os.Stat(settings.WorkspaceRoot); !os.IsNotExist(err) {
		t.Fatal("inspection allocated workspace", err)
	}
	settings.EnableFeatures = true
	t.Setenv("PATH", t.TempDir())
	if _, err := New(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil); err == nil || !strings.Contains(err.Error(), "CPython") {
		t.Fatal("Guardian bypassed missing interpreter refusal", err)
	}
}
