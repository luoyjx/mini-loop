package agent

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestASTModelToolRetainsSourceTextUntilWireEncoding(t *testing.T) {
	var fixture struct {
		Invocations []struct {
			Name, Stdout string
			TextHex      string `json:"text_hex"`
		}
	}
	data, err := os.ReadFile("../testdata/python-ast-text.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Invocations {
		t.Run(row.Name, func(t *testing.T) {
			ast, _ := nativeASTConfig(t)
			payload := filepath.Join(t.TempDir(), "stdout")
			if err := os.WriteFile(payload, []byte(row.Stdout), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("AST_AGENT_PAYLOAD", payload)
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "code.py"), []byte("class One: pass\n"), 0600); err != nil {
				t.Fatal(err)
			}
			input, err := protocol.DecodeToolInput(protocol.ToolFileOutline, []byte(`{"paths":["code.py"]}`))
			if err != nil {
				t.Fatal(err)
			}
			config := runtimeConfig(root, resourceProvider{[]protocol.Block{protocol.NewToolUse("ast", input)}})
			config.ASTConfig = &ast
			config.Mode = ModeReadonly
			session, err := NewRuntimeSession(config)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = session.Run(context.Background(), "inspect"); err != nil {
				t.Fatal(err)
			}
			want, err := hex.DecodeString(row.TextHex)
			if err != nil {
				t.Fatal(err)
			}
			if diagnosticResult(t, session, "ast") != string(want) {
				t.Fatal("model tool text differs from Source")
			}
		})
	}
}
