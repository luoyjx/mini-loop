package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/diagnostics"
	"github.com/luoyjx/mini-loop/go/httpapi"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type diagnosticObservation struct {
	Root   string
	Target *string
}

type launcherDiagnostics struct {
	mu      sync.Mutex
	calls   []diagnosticObservation
	scope   string
	failure bool
	closed  atomic.Int32
}

func (p *launcherDiagnostics) Scope() string { return p.scope }
func (p *launcherDiagnostics) Close() error  { p.closed.Add(1); return nil }
func (p *launcherDiagnostics) Diagnose(_ context.Context, request diagnostics.Request) ([]diagnostics.Diagnostic, error) {
	call := diagnosticObservation{Root: request.Root()}
	if target, ok := request.Target(); ok {
		relative, err := filepath.Rel(request.Root(), target)
		if err != nil {
			return nil, err
		}
		call.Target = &relative
	}
	p.mu.Lock()
	p.calls = append(p.calls, call)
	p.mu.Unlock()
	if p.failure {
		return nil, errors.New("fixture diagnostic failure")
	}
	return []diagnostics.Diagnostic{{Path: "module.py", Line: 7, Message: "fixture type issue", Severity: diagnostics.SeverityWarning}}, nil
}

func TestDiagnosticsLauncherMatchesSourceHTTP(t *testing.T) {
	var fixture struct {
		HTTP []struct {
			Name             string
			Enabled, Failure bool
			Input            json.RawMessage
			Output, Scope    string
			Names            [][]protocol.ToolName
			Results          []string
			Calls            []diagnosticObservation
		}
	}
	data, err := os.ReadFile("../testdata/python-diagnostics-runtime.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.HTTP {
		t.Run(row.Name, func(t *testing.T) {
			requests := make(chan launcherMCPRequest, 2)
			var turns atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request launcherMCPRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				requests <- request
				content, reason := `[{"type":"text","text":"done"}]`, "end_turn"
				if turns.Add(1) == 1 && row.Enabled {
					content = fmt.Sprintf(`[{"type":"tool_use","id":"diagnostic","name":"diagnostics","input":%s}]`, row.Input)
					reason = "tool_use"
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"id":"diagnostics-reply","type":"message","role":"assistant","model":"configured","content":%s,"stop_reason":%q,"usage":{"input_tokens":5,"output_tokens":3}}`, content, reason)
			}))
			defer upstream.Close()
			settings := settingsFor(t, map[string]string{"MINILOOP_FAKE_LLM": "0", "ANTHROPIC_API_KEY": "fixture-key", "ANTHROPIC_BASE_URL": upstream.URL, "MODEL_ID": "configured"})
			provider := &launcherDiagnostics{scope: row.Scope, failure: row.Failure, calls: []diagnosticObservation{}}
			options := Options{}
			if row.Enabled {
				options.DiagnosticsProvider = provider
			}
			app, err := NewWithOptions(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil, options)
			if err != nil {
				t.Fatal(err)
			}
			defer stopApp(t, app)
			base, cancel, done := startApp(t, app)
			defer cancel()
			client := &http.Client{Timeout: 5 * time.Second}
			defer client.CloseIdleConnections()
			code, body := exchange(t, client, "POST", base+"/sessions", `{"mode":"readonly"}`, "")
			if code != 200 {
				t.Fatal(code, string(body))
			}
			info := decode[httpapi.SessionInfo](t, body)
			code, body = exchange(t, client, "POST", base+"/sessions/"+string(info.ID)+"/messages", `{"message":"diagnose workspace"}`, "")
			if code != 200 || decode[httpapi.MessageResponse](t, body).Final != row.Output {
				t.Fatal(code, string(body))
			}
			names, results := [][]protocol.ToolName{}, []string{}
			for range row.Names {
				select {
				case request := <-requests:
					catalog := []protocol.ToolName{}
					for _, schema := range request.Tools {
						catalog = append(catalog, schema.Name)
					}
					names = append(names, catalog)
					results = append(results, launcherMCPResults(request.Messages)...)
				case <-time.After(5 * time.Second):
					t.Fatal("missing model request")
				}
			}
			if !reflect.DeepEqual(names, row.Names) || !reflect.DeepEqual(results, row.Results) {
				t.Fatal(names, results)
			}
			provider.mu.Lock()
			observed := append([]diagnosticObservation{}, provider.calls...)
			provider.mu.Unlock()
			for i := range observed {
				if observed[i].Root != info.Workspace {
					t.Fatal("provider received wrong session root", observed[i].Root, info.Workspace)
				}
				observed[i].Root = "<workspace>"
			}
			if !reflect.DeepEqual(observed, row.Calls) {
				t.Fatal(observed, row.Calls)
			}
			cancel()
			stopped(t, done)
			if provider.closed.Load() != 0 {
				t.Fatal("launcher closed caller-owned provider")
			}
		})
	}
}

type inspectionDiagnostics struct{}

func (inspectionDiagnostics) Scope() string { panic("inspection called Scope") }
func (inspectionDiagnostics) Diagnose(context.Context, diagnostics.Request) ([]diagnostics.Diagnostic, error) {
	panic("inspection called Diagnose")
}

func TestDiagnosticsLauncherInspectionAndUnsupportedBundle(t *testing.T) {
	settings := settingsFor(t, nil)
	options := Options{DiagnosticsProvider: inspectionDiagnostics{}}
	report := InspectWithOptions(settings, config.ServerSettings{}, nil, options)
	if !report.DiagnosticsTools || Inspect(settings, config.ServerSettings{}, nil).DiagnosticsTools {
		t.Fatal("incorrect selection report")
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "inspectionDiagnostics") {
		t.Fatal("provider exposed in report")
	}
	if _, err := os.Stat(settings.WorkspaceRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("inspection allocated workspace", err)
	}
	settings.EnableFeatures = true
	if _, err := NewWithOptions(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil, options); err == nil || !strings.Contains(err.Error(), "MINILOOP_FEATURES") {
		t.Fatal("explicit provider bypassed bundle refusal", err)
	}
}
