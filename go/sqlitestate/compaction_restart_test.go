package sqlitestate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// Exercise the actual tool batch, archive writer, epoch rewrite, database close,
// manager restore and subsequent provider request together, without a spy store.
func TestCompactionNativeSQLiteRecovery(t *testing.T) {
	summaryFailure := errors.New("summary provider unavailable")
	for _, fault := range []error{nil, summaryFailure, context.Canceled} {
		name := "summary"
		if fault != nil {
			name = fault.Error()
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			path := filepath.Join(root, "state.db")
			store := openStore(t, path)
			var requests []protocol.ModelRequest
			turns := 0
			provider := restartProvider(func(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
				if err := request.Validate(); err != nil {
					return protocol.ModelReply{}, err
				}
				requests = append(requests, request.Clone())
				reply := protocol.ModelReply{ID: "reply", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: request.Model, StopReason: protocol.StopEndTurn}
				if request.Purpose == protocol.PurposeCompaction {
					if fault != nil {
						return protocol.ModelReply{}, fault
					}
					reply.Content = []protocol.Block{protocol.NewTextBlock("durable handoff")}
					reply.Usage.InputTokens = 9999
					return reply, nil
				}
				turns++
				if turns == 1 {
					reply.Content = []protocol.Block{
						protocol.NewToolUse("compress", protocol.CompressToolInput()),
						protocol.NewToolUse("read", protocol.ReadFileToolInput(protocol.ReadFileInput{Path: "proof"})),
					}
					reply.StopReason = protocol.StopToolUse
				} else {
					reply.Content = []protocol.Block{protocol.NewTextBlock("done")}
				}
				return reply, nil
			})
			manager := restartManager(t, root, store, provider)
			session, err := manager.Create(ctx, agent.CreateSessionRequest{Owner: "tenant"})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(session.Info().Workspace, "proof"), []byte("paired proof content"), 0600); err != nil {
				t.Fatal(err)
			}
			output, err := session.Run(ctx, "compress then continue")
			if !errors.Is(err, fault) || fault == nil && output != "done" {
				t.Fatalf("run: %q %v; want %v", output, err, fault)
			}
			wantRequests := 2
			if fault == nil {
				wantRequests = 3
			}
			if len(requests) != wantRequests || requests[1].Purpose != protocol.PurposeCompaction {
				t.Fatal("compaction request order", requests)
			}
			archives, err := filepath.Glob(filepath.Join(session.Info().Workspace, ".transcripts", "*.jsonl"))
			if err != nil || len(archives) != 1 {
				t.Fatal("archive written before summary outcome", archives, err)
			}
			archive, err := os.ReadFile(archives[0])
			if err != nil {
				t.Fatal(err)
			}
			var archived []protocol.Message
			for _, line := range bytes.Split(bytes.TrimSpace(archive), []byte("\n")) {
				var message protocol.Message
				if err := json.Unmarshal(line, &message); err != nil {
					t.Fatal(err)
				}
				archived = append(archived, message)
			}
			if len(archived) != 3 || protocol.ValidateTranscript(archived) != nil {
				t.Fatal("archive pairing", archived)
			}
			results, _ := archived[2].Content.Blocks()
			if len(results) != 2 {
				t.Fatal("compaction preceded complete tool batch", results)
			}
			read, ok := results[1].ToolResult()
			if !ok || !strings.Contains(read.Content, "paired proof content") {
				t.Fatal("archive lost later tool result", read)
			}
			before := session.Info()
			history := session.Messages()
			wantEpoch := agent.TranscriptEpoch(1)
			if fault == nil {
				wantEpoch = 2
				if len(history) != 2 {
					t.Fatal("current summary head length", history)
				}
				text, _ := history[0].Content.Plain()
				if !strings.Contains(text, "durable handoff") {
					t.Fatal("current summary head", history)
				}
			} else if !sameRestartMessages(history, archived) {
				t.Fatal("failed summary rewrote canonical history", history)
			}
			assertCompactionEpochs(t, session, wantEpoch, history, archived)
			storedEvents, err := store.LoadEvents(ctx, session.ID(), 0, nil)
			if err != nil {
				t.Fatal(err)
			}
			summaries := 0
			for _, record := range storedEvents {
				if event, ok := record.Event.Compaction(); ok {
					if receipt, ok := event.Summary(); ok {
						summaries++
						if record.TranscriptEpoch != 1 || receipt.Transcript != archives[0] || receipt.ReplacedMessages != 3 || receipt.InputTokens != 9999 {
							t.Fatal("summary receipt epoch/archive", record, receipt)
						}
					}
				}
			}
			if fault == nil && summaries != 1 || fault != nil && summaries != 0 {
				t.Fatal("unexpected persisted summary receipt", summaries)
			}
			stopRestartManager(t, manager)
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store = openStore(t, path)
			manager = restartManager(t, root, store, provider)
			session = restoreRestartSession(t, manager)
			if session.ID() != before.ID || session.Owner() != "tenant" || session.Info().CreatedAt != before.CreatedAt || session.Info().RunCount != before.RunCount || !sameRestartMessages(session.Messages(), history) {
				t.Fatal("restore changed durable identity/history", before, session.Info(), session.Messages())
			}
			assertCompactionEpochs(t, session, wantEpoch, history, archived)
			restoredEvents, err := store.LoadEvents(ctx, session.ID(), 0, nil)
			if err != nil || len(restoredEvents) < len(storedEvents) {
				t.Fatal("lost persisted events", restoredEvents, err)
			}
			oldJSON, _ := json.Marshal(storedEvents)
			newJSON, _ := json.Marshal(restoredEvents[:len(storedEvents)])
			if !bytes.Equal(oldJSON, newJSON) {
				t.Fatal("restore rewrote persisted event prefix")
			}
			if output, err := session.Run(ctx, "resume after restart"); err != nil || output != "done" {
				t.Fatal("resume", output, err)
			}
			request := requests[len(requests)-1]
			if len(request.Messages) != len(history)+1 || !sameRestartMessages(request.Messages[:len(history)], history) {
				t.Fatal("resumed provider received wrong epoch prefix", request.Messages, history)
			}
			if status := session.PersistenceStatus(); status.Error != nil || !status.LeaseConfirmed || status.RestorePending {
				t.Fatal("degraded restored persistence", status)
			}
			retained, err := os.ReadFile(archives[0])
			if err != nil || !bytes.Equal(retained, archive) {
				t.Fatal("restart/resume changed archive", err)
			}
			if fault == nil {
				old, err := session.ReadTranscript(ctx, agent.SelectTranscriptEpoch(1))
				if err != nil || !sameRestartMessages(old.Messages, archived) {
					t.Fatal("resume changed superseded epoch", old, err)
				}
			}
		})
	}
}

func assertCompactionEpochs(t *testing.T, session *agent.ManagedSession, epoch agent.TranscriptEpoch, history, archived []protocol.Message) {
	t.Helper()
	current, err := session.ReadTranscript(context.Background(), agent.TranscriptSelection{})
	if err != nil || current.Epoch != epoch || current.Epochs != epoch || !sameRestartMessages(current.Messages, history) {
		t.Fatal("current durable epoch", current, err)
	}
	old, err := session.ReadTranscript(context.Background(), agent.SelectTranscriptEpoch(1))
	if err != nil || !sameRestartMessages(old.Messages, archived) {
		t.Fatal("original paired epoch", old, err)
	}
}
