package agent

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/efficiency"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type managerEfficiencyProbe struct {
	descriptor                efficiency.Descriptor
	mu                        sync.Mutex
	initialize, close         int
	failInitialize, failClose bool
	stores                    []*efficiency.RawStore
	allClosed                 bool
	entered                   chan struct{}
	cancelled                 bool
}

func (p *managerEfficiencyProbe) Descriptor() efficiency.Descriptor { return p.descriptor }
func (p *managerEfficiencyProbe) Initialize(context.Context, efficiency.LifecycleServices) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.initialize++
	if p.failInitialize {
		return &efficiency.ComponentFault{Class: "ValueError"}
	}
	return nil
}
func (p *managerEfficiencyProbe) Close(_ context.Context, options efficiency.LifecycleCloseOptions) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.close++
	p.allClosed = options.DeadlineSeconds == nil
	for _, store := range p.stores {
		p.allClosed = p.allClosed && store.Closed()
	}
	if p.failClose {
		return &efficiency.ComponentFault{Class: "RuntimeError"}
	}
	return nil
}
func (p *managerEfficiencyProbe) Reduce(ctx context.Context, _ efficiency.MaskedObservation, _ efficiency.ReductionOptions) (efficiency.ObservationReduction, error) {
	if p.entered != nil {
		close(p.entered)
		<-ctx.Done()
		p.mu.Lock()
		p.cancelled = true
		p.mu.Unlock()
		return efficiency.ObservationReduction{}, ctx.Err()
	}
	return efficiency.NewReduction(efficiency.ReductionConfig{Content: "short"})
}

type managerEfficiencySourceSession struct {
	Permission       PermissionMode `json:"permission"`
	RawStore         bool           `json:"raw_store"`
	RecoveryTool     bool           `json:"recovery_tool"`
	TemplateStore    bool           `json:"template_store"`
	WorkspaceMatches bool           `json:"workspace_matches"`
	RawContent       *string        `json:"raw_content"`
}
type managerEfficiencySourceRow struct {
	Name                   string                               `json:"name"`
	Mode                   efficiency.Mode                      `json:"mode"`
	Style                  EfficiencyResponseStyle              `json:"style"`
	Persist                bool                                 `json:"persist"`
	Injected               bool                                 `json:"injected"`
	Template               bool                                 `json:"template"`
	Shared                 bool                                 `json:"shared"`
	SuppliedCatalog        bool                                 `json:"supplied_catalog"`
	FailInitialize         bool                                 `json:"fail_initialize"`
	FailClose              bool                                 `json:"fail_close"`
	Live                   bool                                 `json:"live"`
	Sessions               []managerEfficiencySourceSession     `json:"sessions"`
	StoresShared           bool                                 `json:"stores_shared"`
	FirstClosedAfterDelete *bool                                `json:"first_closed_after_delete"`
	LiveClosedAfterDelete  *bool                                `json:"live_closed_after_delete"`
	Initialize             efficiency.LifecycleReportProjection `json:"initialize"`
	Close                  efficiency.LifecycleReportProjection `json:"close"`
	Components             struct {
		DefaultMode efficiency.Mode `json:"default_mode"`
		Observation []struct {
			Descriptor efficiency.DescriptorConfig `json:"descriptor"`
		} `json:"observation"`
	} `json:"components"`
}

func normalizeManagerReport(p *efficiency.LifecycleReportProjection) {
	for i := range p.Components {
		p.Components[i].ElapsedMS = 0
	}
}

func TestManagerEfficiencyMatchesSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-efficiency-manager.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []managerEfficiencySourceRow `json:"rows"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Rows) != 14 {
		t.Fatal("source inventory changed")
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			cfg := managerTestConfig(filepath.Join(t.TempDir(), "fleet"), &FakeProvider{})
			selection := DefaultManagerEfficiencyConfig()
			selection.Mode, selection.ResponseStyle, selection.PersistRaw = row.Mode, row.Style, row.Persist
			selection.RawMinBytes = big.NewInt(1)
			cfg.Efficiency = &selection
			workspace := t.TempDir()
			var template *efficiency.RawStore
			var probe *managerEfficiencyProbe
			if row.Injected {
				d, err := efficiency.NewDescriptor(row.Components.Observation[0].Descriptor)
				if err != nil {
					t.Fatal(err)
				}
				probe = &managerEfficiencyProbe{descriptor: d, failInitialize: row.FailInitialize, failClose: row.FailClose}
				if row.Live {
					probe.entered = make(chan struct{})
				}
				var registry efficiency.Registry
				if _, err := registry.RegisterObservation(probe, nil); err != nil {
					t.Fatal(err)
				}
				c := efficiency.DefaultRuntimeConfig()
				c.DefaultMode = row.Components.DefaultMode
				if row.Template {
					root := workspace
					if !row.Shared {
						root = t.TempDir()
					}
					template, err = efficiency.NewRawStore(efficiency.DefaultRawStoreConfig(root))
					if err != nil {
						t.Fatal(err)
					}
					c.RawStore = template
					probe.stores = append(probe.stores, template)
				}
				cfg.Services.TokenEfficiency, err = registry.Runtime(c)
				if err != nil {
					t.Fatal(err)
				}
			}
			if row.Shared {
				cfg.WorkspaceFactory = workspaceFactoryFunc(func(context.Context, SessionID) (string, error) { return workspace, nil })
			}
			if row.SuppliedCatalog {
				cfg.Services.Tools, err = NewToolCatalog()
				if err != nil {
					t.Fatal(err)
				}
			}
			if row.Live {
				cfg.Services.Provider = stateProviderFunc(func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
					return fakeReply([]protocol.Block{protocol.NewToolUse("command", protocol.BashToolInput(protocol.BashInput{Command: "printf authority"}))}, protocol.StopToolUse), nil
				})
				cfg.Services.BashFactory = bashFactoryFunc(func(context.Context, SessionBinding) (BashExecutor, error) {
					return observationLiveExecutor("masked authority"), nil
				})
			}
			manager := makeManager(t, cfg)
			if err := manager.Start(); err != nil {
				t.Fatal(err)
			}
			if err := manager.Start(); err != nil {
				t.Fatal(err)
			}
			var sessions []*ManagedSession
			var stores []*efficiency.RawStore
			var got []managerEfficiencySourceSession
			for _, expected := range row.Sessions {
				session := createManaged(t, manager, CreateSessionRequest{Owner: "alice", PermissionMode: expected.Permission})
				sessions = append(sessions, session)
				store := session.core.tokenEfficiency.RawStore()
				_, recovery := session.core.gate.catalog.Lookup(protocol.ToolReadTokenArtifact)
				result := managerEfficiencySourceSession{Permission: expected.Permission, RawStore: store != nil, RecoveryTool: recovery, TemplateStore: store != nil && store == template}
				if store != nil {
					result.WorkspaceMatches = store.Workspace() == session.core.workspace
					pointer, err := store.PutMasked("masked authority")
					if err != nil {
						t.Fatal(err)
					}
					metadata, err := pointer.Snapshot()
					if err != nil {
						t.Fatal(err)
					}
					content, err := store.GetMasked(metadata.Ref)
					if err != nil {
						t.Fatal(err)
					}
					result.RawContent = &content
					stores = append(stores, store)
				}
				got = append(got, result)
			}
			if !reflect.DeepEqual(got, row.Sessions) {
				t.Fatalf("sessions:\n%+v\nwant %+v", got, row.Sessions)
			}
			shared := len(stores) > 1
			for _, store := range stores {
				shared = shared && store == stores[0]
			}
			if shared != row.StoresShared {
				t.Fatal("store identity differs")
			}
			if row.SuppliedCatalog && (len(cfg.Services.Tools.ordered) != 0 || len(sessions[0].core.gate.catalog.ordered) != 0) {
				t.Fatal("caller catalogue widened")
			}
			if probe != nil {
				probe.mu.Lock()
				probe.stores = append(probe.stores, stores...)
				probe.mu.Unlock()
			}
			deleted, err := manager.Delete("alice", sessions[0].ID(), DeleteSessionOptions{PreserveWorkspace: true})
			if err != nil || !deleted {
				t.Fatal(deleted, err)
			}
			if err := manager.WaitCleanup(context.Background()); err != nil {
				t.Fatal(err)
			}
			if row.FirstClosedAfterDelete != nil && stores[0].Closed() != *row.FirstClosedAfterDelete {
				t.Fatal("deleted store lifetime")
			}
			if row.LiveClosedAfterDelete != nil && stores[len(stores)-1].Closed() != *row.LiveClosedAfterDelete {
				t.Fatal("surviving store closed")
			}
			var runDone chan error
			if row.Live {
				runDone = make(chan error, 1)
				go func() { _, err := sessions[2].Run(context.Background(), "run"); runDone <- err }()
				select {
				case <-probe.entered:
				case <-time.After(3 * time.Second):
					t.Fatal("reducer did not enter")
				}
			}
			if err := manager.Stop(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := manager.Stop(context.Background()); err != nil {
				t.Fatal(err)
			}
			if row.Live {
				select {
				case <-runDone:
				case <-time.After(time.Second):
					t.Fatal("live consumer not joined")
				}
			}
			for _, store := range stores {
				if !store.Closed() {
					t.Fatal("owned store retained")
				}
			}
			if template != nil && !template.Closed() {
				t.Fatal("template retained")
			}
			status := manager.EfficiencyStatus()
			if status.Initialize == nil || status.Close == nil || status.InitializeError != nil || status.CloseError != nil {
				t.Fatal(status)
			}
			normalizeManagerReport(status.Initialize)
			normalizeManagerReport(status.Close)
			if !reflect.DeepEqual(*status.Initialize, row.Initialize) || !reflect.DeepEqual(*status.Close, row.Close) {
				t.Fatalf("lifecycle %+v %+v; want %+v %+v", status.Initialize, status.Close, row.Initialize, row.Close)
			}
			if probe != nil {
				probe.mu.Lock()
				defer probe.mu.Unlock()
				if probe.initialize != 1 || probe.close != 1 || !probe.allClosed || (row.Live && !probe.cancelled) {
					t.Fatal("lifecycle ordering/idempotency", probe.initialize, probe.close, probe.allClosed, probe.cancelled)
				}
			}
			if _, err := manager.Create(context.Background(), CreateSessionRequest{Owner: "alice"}); !errors.Is(err, ErrManagerStopped) {
				t.Fatal("stopped admission", err)
			}
		})
	}
}

func TestManagerEfficiencyFilteredCatalogueDoesNotAllocate(t *testing.T) {
	cfg := managerTestConfig(t.TempDir(), &FakeProvider{})
	c := DefaultManagerEfficiencyConfig()
	c.Mode = efficiency.Enforce
	cfg.Efficiency = &c
	m := makeManager(t, cfg)
	s := createManaged(t, m, CreateSessionRequest{Owner: "alice", ToolSelection: SelectTools()})
	if s.core.tokenEfficiency.RawStore() != nil || len(s.core.gate.catalog.ordered) != 0 {
		t.Fatal("filtered capability allocated/widened")
	}
}

type efficiencyFailingState struct {
	*runtimeStateStore
	manager  *SessionManager
	captured []*efficiency.RawStore
}

func (s *efficiencyFailingState) AcquireLease(context.Context, SessionID, LeaseOwner, time.Duration) (bool, error) {
	s.manager.efficiencyState.mu.Lock()
	for store := range s.manager.efficiencyState.stores {
		s.captured = append(s.captured, store)
	}
	s.manager.efficiencyState.mu.Unlock()
	return false, errors.New("lease acquisition failed")
}
func TestManagerEfficiencyFailedPublicationRevokesStore(t *testing.T) {
	cfg := managerTestConfig(t.TempDir(), &FakeProvider{})
	c := DefaultManagerEfficiencyConfig()
	c.Mode = efficiency.Enforce
	cfg.Efficiency = &c
	store := &efficiencyFailingState{runtimeStateStore: newRuntimeStateStore()}
	cfg.Services.StateStore = store
	m := makeManager(t, cfg)
	store.manager = m
	if _, err := m.Create(context.Background(), CreateSessionRequest{Owner: "alice"}); err == nil {
		t.Fatal("publication unexpectedly succeeded")
	}
	if len(store.captured) != 1 || !store.captured[0].Closed() {
		t.Fatal("unpublished store retained")
	}
	m.efficiencyState.mu.Lock()
	defer m.efficiencyState.mu.Unlock()
	if len(m.efficiencyState.stores) != 0 {
		t.Fatal("unpublished binding retained")
	}
}
func TestManagerEfficiencyRestoreOwnsFreshStore(t *testing.T) {
	store := newRuntimeStateStore()
	seedRestore(t, store, []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("saved")}})
	cfg := managerTestConfig(t.TempDir(), &FakeProvider{})
	c := DefaultManagerEfficiencyConfig()
	c.Mode = efficiency.Enforce
	cfg.Efficiency = &c
	cfg.Services.StateStore = store
	m := makeManager(t, cfg)
	s := restoreOne(t, m)
	raw := s.core.tokenEfficiency.RawStore()
	if raw == nil || raw.Workspace() != s.core.workspace {
		t.Fatal("restore scope missing")
	}
	if _, err := m.Delete(s.Owner(), s.ID(), DeleteSessionOptions{PreserveWorkspace: true}); err != nil {
		t.Fatal(err)
	}
	if err := m.WaitCleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !raw.Closed() {
		t.Fatal("restored store retained")
	}
}
