package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/luoyjx/mini-loop/go/efficiency"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type filterSourceRow struct {
	Name           string
	Mode           efficiency.Mode
	Selection      *[]protocol.ToolName
	BeforeNames    []protocol.ToolName `json:"before_names"`
	SetupNames     []protocol.ToolName `json:"setup_names"`
	AfterNames     []protocol.ToolName `json:"after_names"`
	ModelNames     []protocol.ToolName `json:"model_names"`
	BeforeStore    bool                `json:"before_store"`
	SetupSameStore bool                `json:"setup_same_store"`
	AfterSameStore bool                `json:"after_same_store"`
	Passed, Closed bool
}

func TestManagedPostConstructionSelectionMatchesSourceBenchmark(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-benchmark-efficiency-filter.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Rows []filterSourceRow }
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Rows) != 7 {
		t.Fatal("Source filtering inventory drift")
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			var names []protocol.ToolName
			provider := stateProviderFunc(func(_ context.Context, req protocol.ModelRequest) (protocol.ModelReply, error) {
				names = []protocol.ToolName{}
				for _, schema := range req.Tools {
					names = append(names, schema.Name)
				}
				return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
			})
			cfg := managerTestConfig(t.TempDir(), provider)
			selection := DefaultManagerEfficiencyConfig()
			selection.Mode = row.Mode
			cfg.Efficiency = &selection
			manager := makeManager(t, cfg)
			session := createManaged(t, manager, CreateSessionRequest{Owner: "anonymous", PermissionMode: ModeInteractive})
			store := session.core.tokenEfficiency.RawStore()
			original := session.core.gate.catalog
			if (store != nil) != row.BeforeStore || !slices.Equal(original.Names(), row.BeforeNames) {
				t.Fatal("construction drift", original.Names(), store)
			}
			if !slices.Equal(session.core.gate.catalog.Names(), row.SetupNames) || !row.SetupSameStore {
				t.Fatal("setup view drift")
			}
			if row.Selection != nil {
				if err := session.RestrictTools(context.Background(), SelectTools((*row.Selection)...)); err != nil {
					t.Fatal(err)
				}
			}
			if (session.core.tokenEfficiency.RawStore() == store) != row.AfterSameStore || !slices.Equal(session.core.gate.catalog.Names(), row.AfterNames) {
				t.Fatal("filtered inventory/store drift")
			}
			if !slices.Equal(original.Names(), row.BeforeNames) {
				t.Fatal("published catalogue was mutated")
			}
			final, err := session.Run(context.Background(), "finish")
			if err != nil || (final == "done") != row.Passed || !slices.Equal(names, row.ModelNames) {
				t.Fatal("model drift", final, err, names)
			}
			if err := manager.Stop(context.Background()); err != nil {
				t.Fatal(err)
			}
			if (store == nil || store.Closed()) != row.Closed {
				t.Fatal("filtered store escaped manager ownership")
			}
		})
	}
}

func TestManagedToolRestrictionCannotRestoreToolsOrRewriteUsedHistory(t *testing.T) {
	manager := makeManager(t, managerTestConfig(t.TempDir(), stateProviderFunc(func(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
		if len(request.Messages) == 1 {
			return fakeReply([]protocol.Block{protocol.NewToolUse("removed", protocol.WriteFileToolInput(protocol.WriteFileInput{Path: "removed-write", Content: "forbidden"}))}, protocol.StopToolUse), nil
		}
		return stateFinal(), nil
	})))
	session := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := session.core.gate.catalog
	if err := session.RestrictTools(ctx, SelectTools()); !errors.Is(err, context.Canceled) || session.core.gate.catalog != before {
		t.Fatal("cancelled selection mutated tools", err)
	}
	if err := session.RestrictTools(context.Background(), SelectTools(protocol.ToolReadFile)); err != nil {
		t.Fatal(err)
	}
	if err := session.RestrictTools(context.Background(), SelectTools(protocol.ToolReadFile, protocol.ToolBash)); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(session.core.gate.catalog.Names(), []protocol.ToolName{protocol.ToolReadFile}) {
		t.Fatal("restriction widened catalogue")
	}
	if _, err := session.Run(context.Background(), "finish"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(session.Info().Workspace, "removed-write")); !os.IsNotExist(err) {
		t.Fatal("removed tool executed an effect", err)
	}
	before = session.core.gate.catalog
	if err := session.RestrictTools(context.Background(), SelectTools()); !errors.Is(err, ErrToolSelectionStarted) || session.core.gate.catalog != before {
		t.Fatal("used history changed", err)
	}
	fresh := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	fresh.core.messages = []protocol.Message{protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent("restored history")}}
	if err := fresh.RestrictTools(context.Background(), SelectTools()); !errors.Is(err, ErrToolSelectionStarted) {
		t.Fatal("restored history was restricted", err)
	}
	fresh.StopAccepting("closed")
	if err := fresh.RestrictTools(context.Background(), SelectTools()); err == nil {
		t.Fatal("closed session accepted selection")
	}
}

func TestManagedToolRestrictionRefusesActiveTurn(t *testing.T) {
	entered := make(chan struct{})
	provider := stateProviderFunc(func(ctx context.Context, _ protocol.ModelRequest) (protocol.ModelReply, error) {
		close(entered)
		<-ctx.Done()
		return protocol.ModelReply{}, ctx.Err()
	})
	manager := makeManager(t, managerTestConfig(t.TempDir(), provider))
	session := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := session.Run(ctx, "wait"); done <- err }()
	<-entered
	if err := session.RestrictTools(context.Background(), SelectTools()); !errors.Is(err, ErrSessionBusy) {
		t.Fatal("active turn admitted restriction", err)
	}
	cancel()
	<-done
}
