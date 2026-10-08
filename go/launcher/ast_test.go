//go:build darwin || linux

package launcher

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/httpapi"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type astInvocation struct {
	Args []string
	Cwd  string
}

// A copied native test executable acts as the CLI without production environment
// passthrough. Raw exit avoids the race runtime's one-second exit delay.
func init() {
	const prefix = "mini-loop-launcher-ast-"
	name := filepath.Base(os.Args[0])
	if !strings.HasPrefix(name, prefix) {
		return
	}
	mode := strings.TrimPrefix(name, prefix)
	cwd, err := os.Getwd()
	if err != nil {
		syscall.Exit(80)
	}
	file, err := os.OpenFile(os.Args[0]+".calls", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		syscall.Exit(81)
	}
	if json.NewEncoder(file).Encode(astInvocation{os.Args[1:], cwd}) != nil {
		syscall.Exit(82)
	}
	file.Close()
	if reflect.DeepEqual(os.Args[1:], []string{"--version"}) {
		version := "1.9.0"
		if mode == "version" {
			version = "1.10.0"
		}
		fmt.Fprintf(os.Stdout, "ast-outline %s\nhomepage: https://github.com/ast-outline/ast-outline\n", version)
		syscall.Exit(0)
	}
	switch mode {
	case "timeout":
		time.Sleep(3 * time.Second)
	case "invalid":
		fmt.Fprint(os.Stdout, "{x:1}")
		syscall.Exit(0)
	case "array":
		fmt.Fprint(os.Stdout, "[]")
		syscall.Exit(0)
	case "overflow":
		fmt.Fprint(os.Stdout, strings.Repeat("x", 1000))
		syscall.Exit(0)
	case "nonzero":
		fmt.Fprint(os.Stdout, "output")
		fmt.Fprint(os.Stderr, "  cli failure  ")
		syscall.Exit(7)
	}
	fmt.Fprintf(os.Stdout, `{"tool":"ast-outline","schema_version":1,"command":%q,"files":[],"results":[]}`, os.Args[1])
	syscall.Exit(0)
}

func TestASTLauncherMatchesSourceHTTP(t *testing.T) {
	var fixture struct {
		HTTP []struct {
			Case struct {
				Name      string
				Enabled   bool
				Operation protocol.ToolName
				Input     json.RawMessage
				Mode      string
				OutputCap int `json:"output_cap"`
				Timeout   float64
			}
			Output  string
			Names   [][]protocol.ToolName
			Results []string
			Calls   []astInvocation
		}
	}
	data, err := os.ReadFile("../testdata/python-ast-runtime.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(child))
	for _, row := range fixture.HTTP {
		t.Run(row.Case.Name, func(t *testing.T) {
			mode := row.Case.Mode
			if mode == "" {
				mode = "normal"
			}
			binary := filepath.Join(t.TempDir(), "mini-loop-launcher-ast-"+mode)
			if err := os.WriteFile(binary, child, 0700); err != nil {
				t.Fatal(err)
			}
			pin := digest
			if mode == "wrong-digest" {
				pin = strings.Repeat("0", 64)
			}
			requests := make(chan launcherMCPRequest, 2)
			var turns atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request launcherMCPRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				requests <- request
				content, reason := `[{"type":"text","text":"done"}]`, "end_turn"
				if turns.Add(1) == 1 && row.Case.Enabled {
					content = fmt.Sprintf(`[{"type":"tool_use","id":"ast","name":%q,"input":%s}]`, row.Case.Operation, row.Case.Input)
					reason = "tool_use"
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"id":"ast-reply","type":"message","role":"assistant","model":"configured","content":%s,"stop_reason":%q,"usage":{"input_tokens":5,"output_tokens":3}}`, content, reason)
			}))
			defer upstream.Close()
			env := map[string]string{"MINILOOP_FAKE_LLM": "0", "ANTHROPIC_API_KEY": "fixture-key", "ANTHROPIC_BASE_URL": upstream.URL, "MODEL_ID": "configured", "MINILOOP_AST_OUTLINE_ENABLED": strconv.FormatBool(row.Case.Enabled), "MINILOOP_AST_OUTLINE_BINARY": binary, "MINILOOP_AST_OUTLINE_SHA256": pin}
			if row.Case.Timeout != 0 {
				env["MINILOOP_AST_OUTLINE_TIMEOUT"] = strconv.FormatFloat(row.Case.Timeout, 'g', -1, 64)
			}
			if row.Case.OutputCap != 0 {
				env["MINILOOP_AST_OUTLINE_MAX_OUTPUT_BYTES"] = strconv.Itoa(row.Case.OutputCap)
			}
			settings := settingsFor(t, env)
			if mode == "missing" {
				if err := os.Remove(binary); err != nil {
					t.Fatal(err)
				}
			}
			app, err := New(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer stopApp(t, app)
			if _, err := os.Stat(binary + ".calls"); !os.IsNotExist(err) {
				t.Fatal("constructor ran AST CLI", err)
			}
			base, cancel, done := startApp(t, app)
			defer cancel()
			client := &http.Client{Timeout: 10 * time.Second}
			defer client.CloseIdleConnections()
			code, body := exchange(t, client, "POST", base+"/sessions", `{"mode":"readonly"}`, "")
			if code != 200 {
				t.Fatal(code, string(body))
			}
			info := decode[httpapi.SessionInfo](t, body)
			if err := os.WriteFile(filepath.Join(info.Workspace, "code.py"), []byte("class One: pass\n"), 0600); err != nil {
				t.Fatal(err)
			}
			code, body = exchange(t, client, "POST", base+"/sessions/"+string(info.ID)+"/messages", `{"message":"inspect source"}`, "")
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
					for _, result := range launcherMCPResults(request.Messages) {
						results = append(results, strings.ReplaceAll(strings.ReplaceAll(result, info.Workspace, "<workspace>"), binary, "<binary>"))
					}
				case <-time.After(5 * time.Second):
					t.Fatal("missing model request")
				}
			}
			if !reflect.DeepEqual(names, row.Names) || !reflect.DeepEqual(results, row.Results) {
				t.Fatalf("catalog/result mismatch: %v\n%v\nwant %v", names, results, row.Results)
			}
			calls := []astInvocation{}
			if data, err := os.ReadFile(binary + ".calls"); err == nil {
				for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
					var call astInvocation
					if err := json.Unmarshal([]byte(line), &call); err != nil {
						t.Fatal(err)
					}
					if call.Cwd != info.Workspace {
						t.Fatal("wrong CLI cwd", call.Cwd, info.Workspace)
					}
					call.Cwd = "<workspace>"
					for i, arg := range call.Args {
						if strings.Contains(arg, "/mini-loop-ast-") {
							if _, err := os.Stat(arg); !os.IsNotExist(err) {
								t.Fatal("snapshot not removed", arg, err)
							}
							_, suffix, ok := strings.Cut(arg, "/workspace")
							if !ok {
								t.Fatal("unexpected snapshot path", arg)
							}
							call.Args[i] = "<snapshot>" + suffix
						}
					}
					calls = append(calls, call)
				}
			} else if !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(calls, row.Calls) {
				t.Fatal("CLI calls", calls, "want", row.Calls)
			}
			cancel()
			stopped(t, done)
		})
	}
}

func TestASTLauncherConfigurationIsPureAndUsesSourceDefaults(t *testing.T) {
	settings := settingsFor(t, nil)
	ast, err := configuredAST(settings)
	if err != nil || ast != nil || Inspect(settings, config.ServerSettings{}, nil).ASTTools {
		t.Fatal(ast, err)
	}
	settings.ASTOutlineEnabled = true
	settings.ASTOutlineBinary = filepath.Join(t.TempDir(), "missing")
	pin := strings.Repeat("a", 64)
	settings.ASTOutlineSHA256 = &pin
	ast, err = configuredAST(settings)
	if err != nil {
		t.Fatal(err)
	}
	p := ast.Parameters()
	if p.TimeoutSeconds != 10 || p.MaxOutputBytes != 1000000 || len(p.EnvPassthrough) != 0 {
		t.Fatal(p)
	}
	pin = strings.Repeat("b", 64)
	if *ast.Parameters().ExpectedSHA256 == pin {
		t.Fatal("configuration retained digest alias")
	}
	if !Inspect(settings, config.ServerSettings{}, nil).ASTTools || len(settings.Unsupported()) != 0 {
		t.Fatal("AST remains unsupported")
	}
}
