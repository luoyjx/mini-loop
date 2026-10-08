package astcontext

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestASTTextProjectionAndStrictBytesMatchSourceTool(t *testing.T) {
	var fixture struct {
		Invocations []struct {
			Name, Stdout string
			ResultJSON   string `json:"result_json"`
			TextHex      string `json:"text_hex"`
			UTF8         bool
			Calls        []operationAudit
		}
	}
	raw, err := os.ReadFile("../testdata/python-ast-text.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Invocations) != 8 {
		t.Fatal("Source text inventory changed")
	}
	p, workspace := nativeAdapter(t)
	root, err := astWorkspace(context.Background(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "code.py"), []byte("class One: pass\n"), 0600); err != nil {
		t.Fatal(err)
	}
	calls, payload := filepath.Join(t.TempDir(), "calls"), filepath.Join(t.TempDir(), "stdout")
	p.EnvPassthrough = append(p.EnvPassthrough, "AST_CALLS", "AST_PAYLOAD_FILE")
	t.Setenv("AST_MODE", "operation")
	t.Setenv("AST_CALLS", calls)
	t.Setenv("AST_PAYLOAD_FILE", payload)
	a := adapter(t, p)
	for _, row := range fixture.Invocations {
		t.Run(row.Name, func(t *testing.T) {
			if err := os.WriteFile(payload, []byte(row.Stdout), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(calls); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			result, err := a.FileOutline(context.Background(), root, FileOutlineRequest{Paths: []string{"code.py"}})
			if err != nil {
				t.Fatal(err)
			}
			text, err := result.ToJSON()
			if err != nil {
				t.Fatal(err)
			}
			want, err := hex.DecodeString(row.TextHex)
			if err != nil {
				t.Fatal(err)
			}
			if text != string(want) || utf8.ValidString(text) != row.UTF8 {
				t.Fatal("Source text was replaced or rejected")
			}
			value, err := result.Value()
			if err != nil {
				t.Fatal(err)
			}
			ascii, err := jsonvalue.AppendLegacy(nil, value)
			if err != nil || string(ascii) != row.ResultJSON {
				t.Fatal("ASCII result differs", err)
			}
			wire, err := value.MarshalLegacyUTF8()
			if row.UTF8 {
				if err != nil || string(wire) != text {
					t.Fatal("UTF-8 projection differs", err)
				}
			} else if !errors.Is(err, jsonvalue.ErrSurrogate) {
				t.Fatal("strict bytes accepted surrogate", err)
			}
			if _, err = json.Marshal(result); !row.UTF8 && !errors.Is(err, jsonvalue.ErrSurrogate) {
				t.Fatal("standard JSON accepted surrogate", err)
			}
			data, err := os.ReadFile(calls)
			if err != nil {
				t.Fatal(err)
			}
			observed := []operationAudit{}
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				var call operationAudit
				if err = json.Unmarshal([]byte(line), &call); err != nil {
					t.Fatal(err)
				}
				if call.Cwd != root {
					t.Fatal("wrong cwd")
				}
				call.Cwd = "<workspace>"
				for i, arg := range call.Args {
					if strings.Contains(arg, "/mini-loop-ast-") {
						if _, err = os.Stat(arg); !os.IsNotExist(err) {
							t.Fatal("snapshot survives text result", err)
						}
						_, suffix, ok := strings.Cut(arg, "/workspace")
						if !ok {
							t.Fatal(arg)
						}
						call.Args[i] = "<snapshot>" + suffix
					}
				}
				observed = append(observed, call)
			}
			actual, _ := json.Marshal(observed)
			expected, _ := json.Marshal(row.Calls)
			if string(actual) != string(expected) {
				t.Fatal("Source tool child calls differ", string(actual))
			}
		})
	}
}
