package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workspace"
)

type budgetTextFixture struct {
	Cases []struct {
		Name                   string
		IDs, Texts             [][]string
		Maximum, Preview       int
		Older, Blocked         bool
		Mask                   string
		Error                  *struct{ Kind, Message string }
		Count                  int
		Trace                  []string
		DirectoryAtClock       []bool `json:"directory_at_clock"`
		OutputDirectory        bool   `json:"output_directory"`
		Files                  map[string]string
		MessageReplaced        bool       `json:"message_replaced"`
		AfterContentJSON       [][]string `json:"after_content_json"`
		BeforeUnchangedOnError bool       `json:"before_unchanged_on_error"`
	}
}

func budgetTextParts(t *testing.T, pieces []string) string {
	t.Helper()
	var text strings.Builder
	for _, raw := range pieces {
		value, err := jsonvalue.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		part, ok := value.Text()
		if !ok {
			t.Fatal("fixture part is not text")
		}
		text.WriteString(part)
	}
	return text.String()
}

type budgetTextMasker struct {
	mode  string
	calls int
	trace *[]string
}

func (mask *budgetTextMasker) MaskText(text string) string {
	mask.calls++
	*mask.trace = append(*mask.trace, "mask")
	stage := "first"
	if mask.calls > 1 {
		stage = "second"
	}
	if mask.mode == "raise-"+stage {
		panic(errors.New("mask callback fault"))
	}
	if mask.mode == "invalid-"+stage {
		return string([]byte{0xed, 0xa0, 0x80})
	}
	if mask.mode == "replace" {
		return "masked"
	}
	return text
}

func callBudgetText(ctx context.Context, value agent.ToolResultBudgetContext, options agent.ToolResultBudgetOptions) (result agent.ToolResultBudgetResult, err error) {
	result.Messages = append([]protocol.Message(nil), value.Messages...)
	defer func() {
		if failure := recover(); failure != nil {
			cause, ok := failure.(error)
			if !ok {
				panic(failure)
			}
			err = cause
		}
	}()
	return agent.ToolResultBudget(ctx, value, options)
}

func TestPublicBudgetTextMatchesSourceEncodingAndEffects(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-public-budget-text.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture budgetTextFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 25 {
		t.Fatal("required text profiles changed", len(fixture.Cases))
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			root := t.TempDir()
			files, err := workspace.NewFiles(root)
			if err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(files.Root(), ".task_outputs", "tool-results")
			if row.Blocked {
				if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(output, []byte("blocking file"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			blocks := make([]protocol.Block, len(row.IDs))
			ids := make([]string, len(row.IDs))
			for i := range row.IDs {
				ids[i] = budgetTextParts(t, row.IDs[i])
				blocks[i] = protocol.NewToolResult(ids[i], budgetTextParts(t, row.Texts[i]), false)
			}
			messages := []protocol.Message{{Role: protocol.RoleUser, Content: protocol.BlockContent(blocks...)}}
			if row.Older {
				messages = append([]protocol.Message{{Role: protocol.RoleUser, Content: protocol.BlockContent(protocol.NewToolResult("older", string([]byte{0xed, 0xa0, 0x80}), false))}}, messages...)
			}
			original := messages[len(messages)-1].Content.Clone()
			trace := []string{}
			atClock := []bool{}
			mask := &budgetTextMasker{mode: row.Mask, trace: &trace}
			result, err := callBudgetText(context.Background(), agent.ToolResultBudgetContext{Messages: messages, Files: files, Secrets: mask, Now: func() int64 {
				trace = append(trace, "clock")
				info, statErr := os.Stat(output)
				atClock = append(atClock, statErr == nil && info.IsDir())
				return 123456
			}}, agent.ToolResultBudgetOptions{MaxBytes: row.Maximum, PreviewChars: row.Preview})
			if (err == nil) != (row.Error == nil) || result.Persisted != row.Count {
				t.Fatal("encoding error/count differs", err, result.Persisted, row.Error, row.Count)
			}
			if !reflect.DeepEqual(trace, row.Trace) || !reflect.DeepEqual(atClock, row.DirectoryAtClock) {
				t.Fatal("callback/directory order differs", trace, row.Trace, atClock, row.DirectoryAtClock)
			}
			info, statErr := os.Stat(output)
			exists := statErr == nil && info.IsDir()
			if exists != row.OutputDirectory {
				t.Fatal("directory effect differs", exists, row.OutputDirectory)
			}
			stored := map[string]string{}
			paths, globErr := filepath.Glob(filepath.Join(output, "*.txt"))
			if globErr != nil {
				t.Fatal(globErr)
			}
			for _, path := range paths {
				data, readErr := os.ReadFile(path)
				if readErr != nil {
					t.Fatal(readErr)
				}
				stored[filepath.Base(path)] = string(data)
			}
			if !reflect.DeepEqual(stored, row.Files) {
				t.Fatal("file names or contents differ", stored, row.Files)
			}
			index := len(messages) - 1
			if len(result.Messages) != len(messages) {
				t.Fatal("message count changed")
			}
			if replaced := !result.Messages[index].Content.SameStorage(messages[index].Content); replaced != row.MessageReplaced {
				t.Fatal("message replacement differs", replaced, row.MessageReplaced)
			}
			results, ok := result.Messages[index].Content.Blocks()
			if !ok || len(results) != len(row.AfterContentJSON) {
				t.Fatal("result shape differs")
			}
			for i, block := range results {
				output, ok := block.ToolResult()
				if !ok {
					t.Fatal("result kind changed")
				}
				want := budgetTextParts(t, row.AfterContentJSON[i])
				actual := strings.ReplaceAll(output.Content, files.Root(), "<workspace>")
				if actual != want || output.ToolUseID != ids[i] {
					t.Fatal("result content/identity differs", i, actual, want)
				}
			}
			if !reflect.DeepEqual(messages[index].Content, original) || !row.BeforeUnchangedOnError {
				t.Fatal("caller content was mutated")
			}
		})
	}
}
