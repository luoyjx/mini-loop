package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type managedMemoryProvider struct {
	mu           sync.Mutex
	stage, reply string
	budgets      []int
}

func (p *managedMemoryProvider) Complete(_ context.Context, req protocol.ModelRequest) (protocol.ModelReply, error) {
	p.mu.Lock()
	p.budgets = append(p.budgets, req.MaxTokens)
	p.mu.Unlock()
	text := "done"
	switch req.MaxTokens {
	case 200:
		text = "[0]"
	case 1500:
		if p.stage == "extraction" {
			text = p.reply
		} else {
			text = "[]"
		}
	case 2500:
		text = p.reply
	}
	return protocol.ModelReply{ID: "reply", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Content: []protocol.Block{protocol.NewTextBlock(text)}, StopReason: protocol.StopEndTurn, Model: "served-memory", Usage: protocol.TokenUsage{InputTokens: protocol.IntegerUsageCount(1234), OutputTokens: protocol.IntegerUsageCount(2)}}, nil
}

func TestManagedMemoryHTTPMatchesActualPythonSource(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-memory-managed-http.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Mode, Stage, Reply string
			Depth, Status            int
			ForeignStatus            int `json:"foreign_status"`
			Final                    string
			Busy                     bool
			SessionStatus            agent.SessionStatus `json:"session_status"`
			RunCount                 int                 `json:"run_count"`
			Budgets                  []int
			Records, Foreign         []memory.Record
			Memory                   []agent.MemoryEvent
			CaptureErrors            int                 `json:"capture_errors"`
			WireFinal                string              `json:"wire_final"`
			WireMemory               []agent.MemoryEvent `json:"wire_memory"`
		}
	}
	if err = json.Unmarshal(raw, &fixture); err != nil || len(fixture.Cases) != 64 {
		t.Fatal(err, len(fixture.Cases))
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			ctx, root := context.Background(), t.TempDir()
			shared, err := memory.NewStore(ctx, filepath.Join(root, "memory"), nil)
			if err != nil {
				t.Fatal(err)
			}
			provider := &managedMemoryProvider{stage: row.Stage, reply: row.Reply}
			manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: filepath.Join(root, "workspaces"), Services: agent.ManagerServices{Provider: provider, Memory: shared, MemoryTools: true, CachePolicy: agent.NullCachePolicy{}, Recovery: agent.DirectRecovery{}}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				stop, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if err := manager.Stop(stop); err != nil {
					t.Error(err)
				}
			})
			server := testServer(t, Config{Manager: manager, Auth: tokenAuth(t)})
			created := request(server, "POST", "/sessions", `{"mode":"auto"}`, "token-a")
			if created.Code != 200 {
				t.Fatal(created.Code, created.Body.String())
			}
			id := decode[SessionInfo](t, created.Body.Bytes()).ID
			session, err := manager.Get("alice", id)
			if err != nil {
				t.Fatal(err)
			}
			owner, _ := memory.Bind(shared, "alice")
			foreign, _ := memory.Bind(shared, "bob")
			if _, err := foreign.Write(ctx, memory.Input{Name: "foreign", Type: memory.Project, Description: "foreign description", Body: "foreign secret"}); err != nil {
				t.Fatal(err)
			}
			if row.Stage == "consolidation" {
				for i := 0; i < 10; i++ {
					name := "seed-" + string(rune('0'+i))
					if _, err := owner.Write(ctx, memory.Input{Name: name, Type: memory.Project, Description: name, Body: "original", Origin: memory.Explicit}); err != nil {
						t.Fatal(err)
					}
				}
			}
			path := "/sessions/" + string(id) + "/messages"
			if row.Mode == "stream" {
				path += "/stream"
			}
			response := request(server, "POST", path, `{"message":"learn"}`, "token-a")
			if response.Code != row.Status {
				t.Fatal("status", response.Code, row.Status, response.Body.String())
			}
			wireFinal := ""
			wireMemory := []agent.MemoryEvent{}
			if row.Mode == "stream" {
				for _, line := range strings.Split(response.Body.String(), "\n") {
					if !strings.HasPrefix(line, "data: ") {
						continue
					}
					var event agent.SessionEventRecord
					if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
						t.Fatal("SSE decode", err)
					}
					if m, ok := event.Event.Memory(); ok {
						wireMemory = append(wireMemory, m)
					}
					if done, ok := event.Event.Done(); ok {
						wireFinal = done.Text
					}
				}
			} else {
				wireFinal = decode[MessageResponse](t, response.Body.Bytes()).Final
			}
			if wireFinal != row.WireFinal || !reflect.DeepEqual(wireMemory, row.WireMemory) {
				t.Fatal("wire events differ", wireFinal, wireMemory, row.WireFinal, row.WireMemory)
			}
			info := session.Info()
			if info.Busy != row.Busy || info.Status != row.SessionStatus || info.RunCount != row.RunCount {
				t.Fatal("terminal state differs", info, row.SessionStatus, row.RunCount)
			}
			records, err := owner.List(ctx)
			if err != nil || !reflect.DeepEqual(records, row.Records) {
				t.Fatalf("records differ: %#v want %#v err %v", records, row.Records, err)
			}
			records, err = foreign.List(ctx)
			if err != nil || !reflect.DeepEqual(records, row.Foreign) {
				t.Fatal("foreign records changed", records, err)
			}
			forbidden := request(server, "GET", "/sessions/"+string(id)+"/memory", "", "token-b")
			if forbidden.Code != row.ForeignStatus {
				t.Fatal("foreign HTTP status", forbidden.Code, row.ForeignStatus)
			}
			provider.mu.Lock()
			budgets := append([]int(nil), provider.budgets...)
			provider.mu.Unlock()
			if !reflect.DeepEqual(budgets, row.Budgets) {
				t.Fatal("budgets", budgets, row.Budgets)
			}
			projected := []agent.MemoryEvent{}
			captureErrors := 0
			final := ""
			for _, record := range session.Events() {
				if e, ok := record.Event.Memory(); ok {
					projected = append(projected, e)
				}
				if _, ok := record.Event.MemoryCaptureError(); ok {
					captureErrors++
				}
				if e, ok := record.Event.Done(); ok {
					final = e.Text
				}
			}
			if !reflect.DeepEqual(projected, row.Memory) || captureErrors != row.CaptureErrors || final != row.Final {
				t.Fatal("events differ", projected, captureErrors, final, row.Memory, row.CaptureErrors, row.Final)
			}
		})
	}
}
