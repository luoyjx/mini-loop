package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

type selectionProfileHTTPProvider struct {
	mu       sync.Mutex
	reply    string
	requests []protocol.ModelRequest
}

func (p *selectionProfileHTTPProvider) Complete(_ context.Context, req protocol.ModelRequest) (protocol.ModelReply, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req.Clone())
	p.mu.Unlock()
	text := "done"
	if req.Purpose == protocol.PurposeMemorySelection {
		text = p.reply
	}
	return protocol.ModelReply{ID: "reply", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Content: []protocol.Block{protocol.NewTextBlock(text)}, StopReason: protocol.StopEndTurn, Model: "served-memory", Usage: protocol.TokenUsage{InputTokens: protocol.IntegerUsageCount(777), OutputTokens: protocol.IntegerUsageCount(3)}}, nil
}

func TestMemorySelectionHTTPCallerProfilesMatchSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-memory-selection-profiles.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Mode, Shape, Prepared string
			Depth, Status               int
			ReplySHA256                 string `json:"reply_sha256"`
			ForeignStatus               int    `json:"foreign_status"`
			Final                       string
			Budgets                     []int
			Memory                      []agent.MemoryEvent
			Records, Foreign            []memory.Record
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Cases) != 300 {
		t.Fatal("Source profile inventory changed", err, len(fixture.Cases))
	}
	seen := 0
	for _, row := range fixture.Cases {
		if row.Mode != "post" && row.Mode != "stream" {
			continue
		}
		seen++
		t.Run(row.Name, func(t *testing.T) {
			ctx, root := context.Background(), t.TempDir()
			nested := strings.Repeat("[", row.Depth) + "0" + strings.Repeat("]", row.Depth)
			if row.Shape == "object" {
				nested = strings.Repeat(`{"x":`, row.Depth) + "0" + strings.Repeat("}", row.Depth)
			}
			reply := "[2," + nested + "]"
			hash := sha256.Sum256([]byte(reply))
			if hex.EncodeToString(hash[:]) != row.ReplySHA256 {
				t.Fatal("Source reply bytes changed")
			}
			provider := &selectionProfileHTTPProvider{reply: reply}
			store, err := memory.NewStore(ctx, filepath.Join(root, "memory"), nil)
			if err != nil {
				t.Fatal(err)
			}
			manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: filepath.Join(root, "workspaces"), Defaults: agent.SessionDefaults{MaxTokens: 8000}, Services: agent.ManagerServices{Provider: provider, Memory: store, MemoryTools: true, CachePolicy: agent.NullCachePolicy{}, Recovery: agent.DirectRecovery{}}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := manager.Stop(context.Background()); err != nil {
					t.Error(err)
				}
			})
			auth, err := NewTokenAuth([]TokenBinding{{Token: "token", Principal: "owner"}, {Token: "foreign-token", Principal: "foreign"}})
			if err != nil {
				t.Fatal(err)
			}
			server := testServer(t, Config{Manager: manager, Auth: auth})
			created := request(server, "POST", "/sessions", `{"mode":"readonly"}`, "token")
			if created.Code != 200 {
				t.Fatal(created.Code, created.Body.String())
			}
			id := decode[SessionInfo](t, created.Body.Bytes()).ID
			session, err := manager.Get("owner", id)
			if err != nil {
				t.Fatal(err)
			}
			owner, _ := memory.Bind(store, "owner")
			foreign, _ := memory.Bind(store, "foreign")
			if _, err := foreign.Write(ctx, memory.Input{Name: "foreign", Type: memory.Project, Description: "foreign description", Body: "foreign secret"}); err != nil {
				t.Fatal(err)
			}
			for _, input := range []memory.Input{
				{Name: "alpha", Type: memory.Project, Description: "alpha facts", Body: "alpha body", Origin: memory.Explicit},
				{Name: "beta", Type: memory.Feedback, Description: "beta facts", Body: "beta body", Origin: memory.Imported},
				{Name: "gamma", Type: memory.Reference, Description: "gamma facts", Body: "gamma body", Origin: memory.AutoExtracted},
			} {
				if _, err := owner.Write(ctx, input); err != nil {
					t.Fatal(err)
				}
			}
			path := "/sessions/" + string(id) + "/messages"
			if row.Mode == "stream" {
				path += "/stream"
			}
			response := request(server, "POST", path, `{"message":"alpha"}`, "token")
			if response.Code != row.Status {
				t.Fatal(response.Code, response.Body.String())
			}
			wireFinal := ""
			if row.Mode == "stream" {
				for _, line := range strings.Split(response.Body.String(), "\n") {
					if !strings.HasPrefix(line, "data: ") {
						continue
					}
					var event agent.SessionEventRecord
					if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
						t.Fatal(err)
					}
					if done, ok := event.Event.Done(); ok {
						wireFinal = done.Text
					}
				}
			} else {
				wireFinal = decode[MessageResponse](t, response.Body.Bytes()).Final
			}
			if wireFinal != row.Final {
				t.Fatal("wire final differs", wireFinal, row.Final)
			}
			provider.mu.Lock()
			requests := append([]protocol.ModelRequest(nil), provider.requests...)
			provider.mu.Unlock()
			var budgets []int
			prepared := ""
			for _, req := range requests {
				budgets = append(budgets, req.MaxTokens)
				if req.Purpose == protocol.PurposeMemorySelection {
					continue
				}
				for _, message := range req.Messages {
					if text, ok := message.Content.Plain(); message.Role == protocol.RoleUser && ok {
						prepared = text
						break
					}
				}
			}
			if prepared != row.Prepared || !reflect.DeepEqual(budgets, row.Budgets) {
				t.Fatalf("Source selection differs at depth %d: %q want %q, budgets %v want %v", row.Depth, prepared, row.Prepared, budgets, row.Budgets)
			}
			events := []agent.MemoryEvent{}
			for _, record := range session.Events() {
				if event, ok := record.Event.Memory(); ok {
					events = append(events, event)
				}
			}
			if !reflect.DeepEqual(events, row.Memory) {
				t.Fatal("memory events differ", events, row.Memory)
			}
			for _, pair := range []struct {
				store *memory.ScopedStore
				want  []memory.Record
			}{{owner, row.Records}, {foreign, row.Foreign}} {
				got, err := pair.store.List(ctx)
				if err != nil || !reflect.DeepEqual(got, pair.want) {
					t.Fatal("owner records differ", got, pair.want, err)
				}
			}
			denied := request(server, "GET", "/sessions/"+string(id)+"/memory", "", "foreign-token")
			if denied.Code != row.ForeignStatus {
				t.Fatal("foreign status differs", denied.Code, row.ForeignStatus)
			}
		})
	}
	if seen != 120 {
		t.Fatal("incomplete HTTP profile comparison", seen)
	}
}
