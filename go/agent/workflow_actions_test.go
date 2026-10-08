package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestWorkflowActionsMatchSourceMemoryAndSQLite(t *testing.T) {
	var fixture struct {
		Inputs []struct {
			Tool      protocol.ToolName
			Input     string    `json:"input_json"`
			InputHash InputHash `json:"input_hash"`
			ActionID  ActionID  `json:"action_id"`
		}
		Runs []struct {
			Backing, Mode string
			Input         string `json:"input_json"`
			Replay        string `json:"replay_json"`
			Records       []ActionRecord
			Errors        []string
		}
	}
	data, err := os.ReadFile(filepath.Join("..", "testdata", "python-workflow-tools.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, row := range fixture.Inputs {
		input, err := protocol.DecodeToolInput(row.Tool, []byte(row.Input))
		if err != nil {
			t.Fatal(err)
		}
		id, err := ToolActionID("s", actionContext(t), ToolCall{ID: "u", Input: input})
		if err != nil || id != row.ActionID {
			t.Fatal("action identity mismatch", id, err)
		}
		journal, _ := NewInMemoryActionJournal(DefaultResultsRetained)
		record, err := journal.Begin(ctx, ActionRequest{ActionID: id, SessionID: "s", MessageID: "m", ToolUseID: "u", Input: input})
		if err != nil || record.InputHash != row.InputHash {
			t.Fatal("workflow input hash mismatch", record, err)
		}
	}
	for _, row := range fixture.Runs {
		t.Run(row.Backing+"/"+row.Mode, func(t *testing.T) {
			var journal ActionJournal
			if row.Backing == "memory" {
				journal, _ = NewInMemoryActionJournal(DefaultResultsRetained)
			} else {
				// Native injected-store transitions are compared to actual Python
				// SQLite effects; this test does not establish a native SQLite driver.
				journal, _ = NewStoredActionJournal(newTestActionStore())
			}
			input, err := protocol.DecodeToolInput(protocol.ToolWorkflow, []byte(row.Input))
			if err != nil {
				t.Fatal(err)
			}
			request := ActionRequest{ActionID: "action", SessionID: "s", MessageID: "m", ToolUseID: "u", Input: input}
			if row.Mode == "fallback-id" {
				request.ToolUseID = "action"
			}
			records := []ActionRecord{}
			appendRecord := func(record ActionRecord, err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
				records = append(records, normalizedAction(record))
			}
			appendRecord(journal.Begin(ctx, request))
			appendRecord(journal.AttachWorkflow(ctx, "action", "run"))
			replay := request
			replay.Input, err = protocol.DecodeToolInput(protocol.ToolWorkflow, []byte(row.Replay))
			if err != nil {
				t.Fatal(err)
			}
			switch row.Mode {
			case "changed-tool-use":
				replay.ToolUseID = "other"
			case "changed-session":
				replay.SessionID = "other"
			case "changed-message":
				replay.MessageID = "other"
			case "finish-then-attach":
				appendRecord(journal.Finish(ctx, ActionSettlement{ActionID: "action", Status: ActionCompleted, Result: stringPointer("async_launched")}))
			}
			outcomes := []string{}
			operations := []func() (ActionRecord, error){
				func() (ActionRecord, error) { return journal.Begin(ctx, replay) },
				func() (ActionRecord, error) { return journal.AttachWorkflow(ctx, "action", "run") },
				func() (ActionRecord, error) { return journal.AttachWorkflow(ctx, "action", "other") },
				func() (ActionRecord, error) { return journal.AttachWorkflow(ctx, "missing", "run") },
			}
			for index, operation := range operations {
				record, err := operation()
				outcome := ""
				if err == nil {
					appendRecord(record, nil)
				} else {
					var conflict *ActionJournalConflict
					if errors.As(err, &conflict) {
						outcome = "ActionJournalConflict"
					} else if index == 3 {
						outcome = "KeyError"
					} else {
						t.Fatal(err)
					}
				}
				outcomes = append(outcomes, outcome)
			}
			record, exists, err := journal.Get(ctx, "action")
			if !exists {
				t.Fatal("journal lost action")
			}
			appendRecord(record, err)
			if !reflect.DeepEqual(outcomes, row.Errors) || !reflect.DeepEqual(records, row.Records) {
				t.Fatalf("source journal mismatch\nrecords %+v\nwant %+v\nerrors %v want %v", records, row.Records, outcomes, row.Errors)
			}
			if record.WorkflowRunID == nil || *record.WorkflowRunID != "run" {
				t.Fatal("binding changed")
			}
			*record.WorkflowRunID = "mutated"
			retained, _, err := journal.Get(ctx, "action")
			if err != nil || *retained.WorkflowRunID != "run" {
				t.Fatal("detached read aliases binding")
			}
		})
	}
}
