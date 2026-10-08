package mcp

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/secrets"
)

// The peer is a native subprocess of this test executable. Go runtime tests do
// not invoke Python; the fixture was exported from the real Python client.
func TestStdioPeer(t *testing.T) {
	if os.Getenv("MINILOOP_MCP_TEST_PEER") != "1" {
		return
	}
	scenario := os.Getenv("MINILOOP_MCP_TEST_SCENARIO")
	var recipe struct {
		ToolsJSON      string `json:"tools_json"`
		ResultJSON     string `json:"result_json"`
		RepeatText     string `json:"repeat_text"`
		RepeatCount    int    `json:"repeat_count"`
		RepeatLocation string `json:"repeat_location"`
	}
	if path := os.Getenv("MINILOOP_MCP_TEST_RECIPE"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			os.Exit(9)
		}
		if json.Unmarshal(data, &recipe) != nil {
			os.Exit(9)
		}
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 65536), MaxRPCLine+2)
	for scanner.Scan() {
		request, err := jsonvalue.Decode(scanner.Text())
		if err != nil {
			os.Exit(8)
		}
		if path := os.Getenv("MINILOOP_MCP_TEST_LOG"); path != "" {
			f, _ := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
			fmt.Fprintln(f, scanner.Text())
			f.Close()
		}
		id, exists := request.Lookup("id")
		if !exists {
			continue
		}
		methodValue, _ := request.Lookup("method")
		method, _ := methodValue.Text()
		if scenario == "hang-start" && method == "initialize" {
			time.Sleep(time.Hour)
		}
		result := object()
		if method == "tools/list" {
			if scenario == "hang-list" {
				time.Sleep(time.Second)
				continue
			}
			if recipe.ToolsJSON != "" {
				result, err = jsonvalue.Decode(recipe.ToolsJSON)
			}
		}
		if method == "tools/call" {
			params, _ := request.Lookup("params")
			nameValue, _ := params.Lookup("name")
			name, _ := nameValue.Text()
			switch name {
			case "crash":
				os.Exit(7)
			case "rpc-error":
				data, _ := jsonvalue.AppendLegacyDefault(object(field("id", id), field("error", object(field("code", jsonvalue.IntegerValue(-1)), field("message", jsonvalue.TextValue("fault"))))))
				fmt.Println(string(data))
				continue
			case "oversize":
				fmt.Println(strings.Repeat("x", MaxRPCLine+1))
				continue
			case "late":
				time.Sleep(120 * time.Millisecond)
				result = object(field("content", jsonvalue.ArrayValue([]jsonvalue.Value{object(field("text", jsonvalue.TextValue("late")))})))
			case "probe":
				args, _ := params.Lookup("arguments")
				data, _ := jsonvalue.AppendLegacyDefault(object(field("pid", jsonvalue.IntegerValue(int64(os.Getpid()))), field("id", id), field("arguments", args), field("secret", jsonvalue.TextValue(os.Getenv("MINILOOP_MCP_TEST_SECRET"))), field("passed", jsonvalue.TextValue(os.Getenv("MINILOOP_MCP_TEST_PASSED")))))
				result = object(field("content", jsonvalue.ArrayValue([]jsonvalue.Value{object(field("text", jsonvalue.TextValue(string(data))))})))
			default:
				if recipe.ResultJSON != "" {
					result, err = jsonvalue.Decode(recipe.ResultJSON)
					if recipe.RepeatCount > 0 {
						text := jsonvalue.TextValue(strings.Repeat(recipe.RepeatText, recipe.RepeatCount))
						if recipe.RepeatLocation == "content" {
							result = object(field("content", jsonvalue.ArrayValue([]jsonvalue.Value{object(field("text", text))})))
						} else {
							result = object(field("value", text))
						}
					}
				}
			}
		}
		if err != nil {
			os.Exit(9)
		}
		fmt.Println(`{"jsonrpc":"2.0","method":"probe/notice"}`)
		fmt.Println(`{"jsonrpc":"2.0","id":-1,"result":{}}`)
		data, _ := jsonvalue.AppendLegacyDefault(object(field("jsonrpc", jsonvalue.TextValue("2.0")), field("id", id), field("result", result)))
		fmt.Println(string(data))
	}
	os.Exit(0)
}

func peer(t *testing.T, timeout time.Duration) *StdioClient {
	t.Helper()
	t.Setenv("MINILOOP_MCP_TEST_PEER", "1")
	client, err := NewStdio(StdioConfig{Name: "fixture", Command: []string{os.Args[0], "-test.run=^TestStdioPeer$"}, Timeout: timeout})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func TestPythonStdioContracts(t *testing.T) {
	var fixture struct {
		Rows []struct {
			Name           string `json:"name"`
			ToolsJSON      string `json:"tools_json"`
			ResultJSON     string `json:"result_json"`
			RepeatText     string `json:"repeat_text"`
			RepeatCount    int    `json:"repeat_count"`
			RepeatLocation string `json:"repeat_location"`
			Discovered     []struct {
				Name        string          `json:"name"`
				Description jsonvalue.Value `json:"description"`
				InputSchema jsonvalue.Value `json:"input_schema"`
				Annotations jsonvalue.Value `json:"annotations"`
			} `json:"discovered"`
			Output           string `json:"output"`
			OutputSHA256     string `json:"output_sha256"`
			OutputCharacters int    `json:"output_characters"`
			Error            string `json:"error"`
		} `json:"rows"`
	}
	data, err := os.ReadFile("../testdata/python-mcp-stdio.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			recipe, err := json.Marshal(row)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "recipe.json")
			if err = os.WriteFile(path, recipe, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("MINILOOP_MCP_TEST_RECIPE", path)
			client := peer(t, time.Second)
			discovered, err := client.ListTools(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(discovered) != len(row.Discovered) {
				t.Fatalf("discovery: %v", discovered)
			}
			for i, expected := range row.Discovered {
				actual := discovered[i]
				a, _ := json.Marshal(actual)
				b, _ := json.Marshal(ToolDescription{expected.Name, expected.Description, expected.InputSchema, expected.Annotations})
				av, _ := jsonvalue.Decode(string(a))
				bv, _ := jsonvalue.Decode(string(b))
				a, _ = av.Sorted().MarshalJSON()
				b, _ = bv.Sorted().MarshalJSON()
				if string(a) != string(b) {
					t.Fatalf("discovery mismatch: %s != %s", a, b)
				}
			}
			output, err := client.CallTool(context.Background(), "echo", object(field("value", jsonvalue.TextValue("中文"))))
			if row.Error != "" {
				if err == nil {
					t.Fatalf("expected source error %s", row.Error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprintf("%x", sha256.Sum256([]byte(output))) != row.OutputSHA256 || jsonvalue.RuneCount(output) != row.OutputCharacters || (row.Output != "" && output != row.Output) {
				t.Fatalf("result differs: lengths %d/%d", len(output), len(row.Output))
			}
		})
	}
}

func probe(t *testing.T, client *StdioClient) jsonvalue.Value {
	t.Helper()
	result, err := client.CallTool(context.Background(), "probe", object())
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonvalue.Decode(result)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func integer(value jsonvalue.Value, key string) string {
	item, _ := value.Lookup(key)
	text, _ := item.Integer()
	return text
}

func TestLifecycleRestartWithoutReplay(t *testing.T) {
	log := filepath.Join(t.TempDir(), "requests.jsonl")
	t.Setenv("MINILOOP_MCP_TEST_LOG", log)
	client := peer(t, time.Second)
	first := probe(t, client)
	second := probe(t, client)
	if integer(first, "pid") != integer(second, "pid") {
		t.Fatal("live child respawned")
	}
	if _, err := client.CallTool(context.Background(), "crash", object()); err == nil {
		t.Fatal("crash must fail")
	}
	client.mu.Lock()
	old := client.child
	client.mu.Unlock()
	select {
	case <-old.done:
	case <-time.After(time.Second):
		t.Fatal("crashed child not reaped")
	}
	third := probe(t, client)
	if integer(first, "pid") == integer(third, "pid") {
		t.Fatal("dead child reused")
	}
	if integer(third, "id") != "6" {
		t.Fatalf("request IDs restarted: %s", integer(third, "id"))
	}
	client.Close()
	fourth := probe(t, client)
	if integer(third, "pid") == integer(fourth, "pid") {
		t.Fatal("close did not terminate child")
	}
	client.Close()
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), `"name": "crash"`) != 1 {
		t.Fatal("failed effect retried")
	}
	if strings.Count(string(data), `"protocolVersion": "2024-11-05"`) != 3 || strings.Count(string(data), `"notifications/initialized"`) != 3 {
		t.Fatal("handshake mismatch")
	}
}

func TestSecretsAndDetachedConfiguration(t *testing.T) {
	t.Setenv("MINILOOP_MCP_TEST_PEER", "1")
	t.Setenv("MINILOOP_MCP_TEST_SECRET", "hidden-value")
	t.Setenv("MINILOOP_MCP_TEST_PASSED", "allowed-value")
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("MINILOOP_MCP_TEST_SECRET", "hidden-value")
	registry.RegisterValue("MINILOOP_MCP_TEST_PASSED", "allowed-value")
	registry.RegisterValue("ABSENT_SECRET", "absent-value")
	command := []string{os.Args[0], "-test.run=^TestStdioPeer$"}
	passed := []secrets.Name{"MINILOOP_MCP_TEST_PASSED"}
	client, err := NewStdio(StdioConfig{Name: "fixture", Command: command, Secrets: registry, EnvironmentPassthrough: passed, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	command[0] = "missing"
	passed[0] = "MINILOOP_MCP_TEST_SECRET"
	value := probe(t, client)
	hidden, _ := value.Lookup("secret")
	allowed, _ := value.Lookup("passed")
	if text, _ := hidden.Text(); text != "" {
		t.Fatal("secret leaked")
	}
	if text, _ := allowed.Text(); text != "allowed-value" {
		t.Fatal("explicit passthrough lost")
	}
	withheld := client.Withheld()
	if fmt.Sprint(withheld) != "[ABSENT_SECRET MINILOOP_MCP_TEST_SECRET]" {
		t.Fatalf("withheld: %v", withheld)
	}
	withheld[0] = "changed"
	if client.Withheld()[0] != "ABSENT_SECRET" {
		t.Fatal("mutable withheld snapshot")
	}
}

func TestTimeoutAndLateResponse(t *testing.T) {
	client := peer(t, time.Second)
	initial := probe(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := client.CallTool(ctx, "late", object()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
	next := probe(t, client)
	if integer(initial, "pid") != integer(next, "pid") {
		t.Fatal("read timeout restarted live child")
	}
	if integer(next, "id") != "4" {
		t.Fatal("late response consumed as current")
	}
}

func TestStartupAndListTimeout(t *testing.T) {
	for _, scenario := range []string{"hang-start", "hang-list"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("MINILOOP_MCP_TEST_SCENARIO", scenario)
			client := peer(t, 100*time.Millisecond)
			if _, err := client.ListTools(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("timeout: %v", err)
			}
			client.mu.Lock()
			p := client.child
			client.mu.Unlock()
			if scenario == "hang-start" && p != nil {
				t.Fatal("failed handshake retained process")
			}
			if scenario == "hang-list" && !alive(p) {
				t.Fatal("list read timeout killed process")
			}
		})
	}
}

func TestRPCFaultAndLineLimit(t *testing.T) {
	client := peer(t, time.Second)
	if _, err := client.CallTool(context.Background(), "rpc-error", object()); err == nil || err.Error() != "MCP error: {'code': -1, 'message': 'fault'}" {
		t.Fatalf("fault: %v", err)
	}
	if _, err := client.CallTool(context.Background(), "oversize", object()); err == nil || !strings.Contains(err.Error(), "8,388,608") {
		t.Fatalf("oversize: %v", err)
	}
	probe(t, client)
}

func TestSerializedConcurrentCalls(t *testing.T) {
	client := peer(t, time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := object(field("index", jsonvalue.IntegerValue(int64(i))))
			text, err := client.CallTool(context.Background(), "probe", args)
			if err != nil {
				t.Error(err)
				return
			}
			result, err := jsonvalue.Decode(text)
			if err != nil {
				t.Error(err)
				return
			}
			actual, _ := result.Lookup("arguments")
			if integer(actual, "index") != fmt.Sprint(i) {
				t.Error("response crossed RPC ownership")
			}
		}(i)
	}
	wg.Wait()
}
