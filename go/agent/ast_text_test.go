package agent

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type astLoopProvider struct {
	input protocol.ToolInput
	calls int
}

func (provider *astLoopProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	provider.calls++
	if provider.calls == 1 {
		return fakeReply([]protocol.Block{protocol.NewToolUse("ast", provider.input)}, protocol.StopToolUse), nil
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
}

func TestASTModelToolMatchesSourceTextAndLoopFailureStage(t *testing.T) {
	var fixture struct {
		Invocations []struct {
			Name, Stdout string
			TextHex      string `json:"text_hex"`
			Agent        struct {
				Error, Output      string
				ProviderCalls      int     `json:"provider_calls"`
				ModelStarts        int     `json:"model_starts"`
				TranscriptMessages int     `json:"transcript_messages"`
				BudgetDirectory    bool    `json:"budget_directory"`
				ResultTextHex      *string `json:"result_text_hex"`
			}
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
			provider := &astLoopProvider{input: input}
			config := runtimeConfig(root, provider)
			config.ASTConfig = &ast
			config.Mode = ModeReadonly
			session, err := NewRuntimeSession(config)
			if err != nil {
				t.Fatal(err)
			}
			output, runErr := session.Run(context.Background(), "inspect")
			if row.Agent.Error == "" {
				if runErr != nil || output != row.Agent.Output {
					t.Fatal("Source successful loop differs", output, runErr, row.Agent)
				}
			} else if row.Agent.Error != "UnicodeEncodeError" || !errors.Is(runErr, protocol.ErrResultUTF8) {
				t.Fatal("Source hash error differs", runErr, row.Agent.Error)
			}
			if provider.calls != row.Agent.ProviderCalls || len(session.Messages()) != row.Agent.TranscriptMessages {
				t.Fatal("Source model/transcript failure stage differs", provider.calls, len(session.Messages()), row.Agent, session.Messages())
			}
			modelStarts := 0
			var eventText *string
			for _, record := range session.Events() {
				event := record.Event
				if _, ok := event.ModelStart(); ok {
					modelStarts++
				}
				if result, ok := event.ToolResult(); ok && result.ID == "ast" {
					text := result.Output
					eventText = &text
				}
			}
			if modelStarts != row.Agent.ModelStarts {
				t.Fatal("Source model event count differs", modelStarts, row.Agent.ModelStarts)
			}
			info, statErr := os.Stat(filepath.Join(root, ".task_outputs", "tool-results"))
			if (statErr == nil && info.IsDir()) != row.Agent.BudgetDirectory {
				t.Fatal("Source budget directory effect differs", statErr, row.Agent.BudgetDirectory)
			}
			want, err := hex.DecodeString(row.TextHex)
			if err != nil {
				t.Fatal(err)
			}
			if eventText == nil || *eventText != string(want) {
				t.Fatal("model result event text differs from Source")
			}
			if row.Agent.ResultTextHex != nil {
				if diagnosticResult(t, session, "ast") != string(want) {
					t.Fatal("model tool text differs from Source")
				}
			} else {
				for _, message := range session.Messages() {
					blocks, _ := message.Content.Blocks()
					for _, block := range blocks {
						if result, ok := block.ToolResult(); ok && result.ToolUseID == "ast" {
							t.Fatal("ordinary hash failure unexpectedly repaired Source transcript")
						}
					}
				}
				if len(session.recentSteps) != 0 {
					t.Fatal("failed hash entered ledger")
				}
			}
		})
	}
}
