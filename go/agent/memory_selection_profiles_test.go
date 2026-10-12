package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type memorySelectionProfileCase struct {
	Name, Mode, Shape, Prepared string
	Depth                       int
	ReplySHA256                 string `json:"reply_sha256"`
	Final                       *string
	Budgets                     []int
	Memory                      []MemoryEvent
	Records, Foreign            []memory.Record
}

func selectionProfileReply(t *testing.T, row memorySelectionProfileCase) string {
	t.Helper()
	nested := strings.Repeat("[", row.Depth) + "0" + strings.Repeat("]", row.Depth)
	if row.Shape == "object" {
		nested = strings.Repeat(`{"x":`, row.Depth) + "0" + strings.Repeat("}", row.Depth)
	}
	reply := "[2," + nested + "]"
	hash := sha256.Sum256([]byte(reply))
	if hex.EncodeToString(hash[:]) != row.ReplySHA256 {
		t.Fatal("Source reply recipe changed")
	}
	return reply
}

func seedSelectionProfile(t *testing.T, store *memory.Store) (*memory.ScopedStore, *memory.ScopedStore) {
	t.Helper()
	owner, _ := memory.Bind(store, "owner")
	foreign, _ := memory.Bind(store, "foreign")
	if _, err := foreign.Write(context.Background(), memory.Input{Name: "foreign", Type: memory.Project, Description: "foreign description", Body: "foreign secret"}); err != nil {
		t.Fatal(err)
	}
	for _, input := range []memory.Input{
		{Name: "alpha", Type: memory.Project, Description: "alpha facts", Body: "alpha body", Origin: memory.Explicit},
		{Name: "beta", Type: memory.Feedback, Description: "beta facts", Body: "beta body", Origin: memory.Imported},
		{Name: "gamma", Type: memory.Reference, Description: "gamma facts", Body: "gamma body", Origin: memory.AutoExtracted},
	} {
		if _, err := owner.Write(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}
	return owner, foreign
}

func TestMemorySelectionCallerProfilesMatchSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-memory-selection-profiles.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Cases []memorySelectionProfileCase }
	if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Cases) != 300 {
		t.Fatal("Source caller inventory changed", err, len(fixture.Cases))
	}
	seen := 0
	for _, row := range fixture.Cases {
		if row.Mode == "post" || row.Mode == "stream" {
			continue
		}
		seen++
		t.Run(row.Name, func(t *testing.T) {
			ctx, root := context.Background(), t.TempDir()
			store, err := memory.NewStore(ctx, filepath.Join(root, "memory"), nil)
			if err != nil {
				t.Fatal(err)
			}
			owner, foreign := seedSelectionProfile(t, store)
			provider := &memorySelectionProvider{output: selectionProfileReply(t, row)}
			var core *Session
			var final, prepared string
			if row.Mode == "managed" {
				cfg := managerTestConfig(filepath.Join(root, "workspaces"), provider)
				cfg.Defaults.MaxTokens = 8000
				cfg.Services.Memory, cfg.Services.MemoryTools = store, true
				cfg.Services.CachePolicy, cfg.Services.Recovery = NullCachePolicy{}, DirectRecovery{}
				manager := makeManager(t, cfg)
				managed := createManaged(t, manager, CreateSessionRequest{Owner: "owner", PermissionMode: ModeReadonly})
				core = managed.core
				final, err = managed.Run(ctx, "alpha")
			} else {
				cfg := runtimeConfig(filepath.Join(root, "workspace"), provider)
				cfg.Memory, cfg.MemoryTools, cfg.Mode, cfg.MaxTokens = owner, true, ModeReadonly, 8000
				cfg.CachePolicy, cfg.Recovery = NullCachePolicy{}, DirectRecovery{}
				core, err = NewRuntimeSession(cfg)
				if err != nil {
					t.Fatal(err)
				}
				if row.Mode == "direct" {
					before := core.meter.Snapshot()
					prepared, err = core.prepareMemoryContext(ctx, "alpha")
					if !reflect.DeepEqual(before, core.meter.Snapshot()) {
						t.Fatal("selection anchored live meter")
					}
				} else {
					final, err = core.Run(ctx, "alpha")
				}
			}
			if err != nil || row.Final != nil && final != *row.Final {
				t.Fatal("turn outcome differs", final, row.Final, err)
			}
			if row.Mode != "direct" {
				for _, message := range core.messages {
					if value, ok := message.Content.Plain(); message.Role == protocol.RoleUser && ok {
						prepared = value
						break
					}
				}
			}
			if prepared != row.Prepared {
				t.Fatalf("Source prepared prompt differs at depth %d: %q want %q", row.Depth, prepared, row.Prepared)
			}
			var budgets []int
			for _, request := range provider.requests {
				budgets = append(budgets, request.MaxTokens)
			}
			if !reflect.DeepEqual(budgets, row.Budgets) {
				t.Fatal("Source model budgets differ", budgets, row.Budgets)
			}
			events := []MemoryEvent{}
			for _, record := range core.Events() {
				if event, ok := record.Event.Memory(); ok {
					events = append(events, event)
				}
			}
			if !reflect.DeepEqual(events, row.Memory) {
				t.Fatal("Source memory events differ", events, row.Memory)
			}
			for _, pair := range []struct {
				store *memory.ScopedStore
				want  []memory.Record
			}{{owner, row.Records}, {foreign, row.Foreign}} {
				got, err := pair.store.List(ctx)
				if err != nil || !reflect.DeepEqual(got, pair.want) {
					t.Fatal("owner/foreign memory changed", got, pair.want, err)
				}
			}
		})
	}
	if seen != 180 {
		t.Fatal("incomplete SDK profile comparison", seen)
	}
}
