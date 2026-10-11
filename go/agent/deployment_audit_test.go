package agent

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/problems"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/selfaudit"
	"github.com/luoyjx/mini-loop/go/skills"
)

func TestDeploymentAuditReadsManagerBindingsAndNeverResolvesSecrets(t *testing.T) {
	registry := secrets.New(secrets.Config{})
	registry.RegisterLookup("LAZY_TOKEN", func() (string, error) { t.Fatal("observation resolved secret"); return "", nil })
	cfg := managerTestConfig(t.TempDir(), &FakeProvider{})
	cfg.Services.Secrets = registry
	manager := makeManager(t, cfg)
	live := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
	live.core.secrets = secrets.Null{}
	result, err := manager.ObserveDeploymentAudit(context.Background())
	if err != nil || result.SecretsNull || !reflect.DeepEqual(result.SecretNames, []secrets.Name{"LAZY_TOKEN"}) || len(result.Unresolved) != 0 {
		t.Fatal(result, err)
	}
	result.SecretNames[0] = "changed"
	again, err := manager.ObserveDeploymentAudit(context.Background())
	if err != nil || again.SecretNames[0] != "LAZY_TOKEN" || manager.Summary().Sessions != 1 {
		t.Fatal(again, err)
	}
	if live.core.taskDiagnostics.Load() != nil {
		t.Fatal("observation initialized task diagnostics")
	}
}

func TestDeploymentAuditAggregatesLiveMCPAndGlobalDiagnostics(t *testing.T) {
	cfg := managerTestConfig(t.TempDir(), &FakeProvider{})
	cfg.Services.MCPTools = true
	manager := makeManager(t, cfg)
	for i := 0; i < 2; i++ {
		handle := createManaged(t, manager, CreateSessionRequest{Owner: OwnerID(fmt.Sprintf("owner%d", i))})
		for _, text := range []string{"MCP collision", "MCP collision", "MCP truncated"} {
			if err := handle.core.mcp.registry.problems.Append(text); err != nil {
				t.Fatal(err)
			}
		}
	}
	journal := manager.config.Services.ActionJournal.(*InMemoryActionJournal)
	journal.mu.Lock()
	recordProblemOccurrence(&journal.problemOccurrences, "unknown action")
	journal.mu.Unlock()
	manager.config.Services.Approvals.mu.Lock()
	recordProblemOccurrence(&manager.config.Services.Approvals.problemOccurrences, "approval fault")
	manager.config.Services.Approvals.mu.Unlock()
	result, err := manager.ObserveDeploymentAudit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.MCP.Total.String() != "6" || result.MCP.Summary == nil || (*result.MCP.Summary)[0] != "MCP collision (x4)" {
		t.Fatal(result.MCP)
	}
	channels := map[DeploymentSubsystemName]selfaudit.Ledger{}
	for _, channel := range result.Channels {
		channels[channel.Name] = channel.Ledger
	}
	if len(channels["actions"].Entries) != 1 || len(channels["approvals"].Entries) != 1 {
		t.Fatal(channels)
	}
	(*result.MCP.Summary)[0] = "changed"
	result.Channels[0].Ledger.Entries[0] = "changed"
	again, err := manager.ObserveDeploymentAudit(context.Background())
	if err != nil || (*again.MCP.Summary)[0] != "MCP collision (x4)" || again.Channels[0].Ledger.Entries[0] == "changed" {
		t.Fatal(again, err)
	}
}

func TestDeploymentAuditReportsActualPersistenceWriteFault(t *testing.T) {
	store := newRuntimeStateStore()
	cfg := managerTestConfig(t.TempDir(), &recordingProvider{})
	cfg.Services.StateStore = store
	manager := makeManager(t, cfg)
	handle := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
	store.mu.Lock()
	store.faults["event"] = errors.New("fixture disk unavailable")
	store.mu.Unlock()
	if _, err := handle.Run(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	result, err := manager.ObserveDeploymentAudit(context.Background())
	if err != nil || result.PersistenceError == nil || *result.PersistenceError != "*errors.errorString: fixture disk unavailable" || result.StateNull {
		t.Fatal(result, err)
	}
	*result.PersistenceError = "changed"
	again, err := manager.ObserveDeploymentAudit(context.Background())
	if err != nil || *again.PersistenceError == "changed" {
		t.Fatal(again, err)
	}
}

func TestDeploymentMCPMergeMatchesReplayAndKeepsHugeCounts(t *testing.T) {
	snapshots := []problems.Snapshot{}
	replay := problems.New(problems.MaxDistinctProblems)
	for pass := 0; pass < 3; pass++ {
		snapshot := problems.Snapshot{}
		for i := 0; i < 65; i++ {
			text := fmt.Sprintf("fault%d", i)
			count := uint64(i%3 + 1)
			snapshot.Entries = append(snapshot.Entries, problems.Entry{Text: text, Count: problems.FromUint64(count)})
			for n := uint64(0); n < count; n++ {
				if err := replay.Append(text); err != nil {
					t.Fatal(err)
				}
			}
		}
		snapshots = append(snapshots, snapshot)
	}
	if got, want := mergeDeploymentMCP(snapshots), replay.Snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatal(got, want)
	}
	value, _ := new(big.Int).SetString("123456789012345678901234567890", 10)
	count, err := problems.FromBigInt(value)
	if err != nil {
		t.Fatal(err)
	}
	got := mergeDeploymentMCP([]problems.Snapshot{{Entries: []problems.Entry{{Text: "huge", Count: count}}}})
	if got.Total.String() != value.String() || len(got.Entries) != 1 || got.Entries[0].Count.String() != value.String() {
		t.Fatal(got)
	}
}

func TestDeploymentAuditConcurrentSnapshotsAndCancellation(t *testing.T) {
	cfg := managerTestConfig(t.TempDir(), &FakeProvider{})
	cfg.Services.MCPTools = true
	manager := makeManager(t, cfg)
	handle := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				handle.core.mcp.registry.problems.Append("concurrent fault")
				if _, err := manager.ObserveDeploymentAudit(context.Background()); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	result, err := manager.ObserveDeploymentAudit(context.Background())
	if err != nil || result.MCP.Total.String() != "90" {
		t.Fatal(result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := manager.ObserveDeploymentAudit(ctx); !errors.Is(err, context.Canceled) || result.Catalog != nil {
		t.Fatal(result, err)
	}
}

func TestDeploymentAuditRegistryCallbackIsUnlockedAndPanicIsNotClean(t *testing.T) {
	registry := &observingRegistry{Registry: secrets.New(secrets.Config{})}
	registry.RegisterValue("REGISTERED_TOKEN", "fixture-value")
	cfg := managerTestConfig(t.TempDir(), &FakeProvider{})
	cfg.Services.Secrets = registry
	manager := makeManager(t, cfg)
	registry.manager = manager
	result, err := manager.ObserveDeploymentAudit(context.Background())
	if err != nil || registry.calls != 1 || len(result.SecretNames) != 1 {
		t.Fatal(result, err)
	}
	registry.fail = true
	result, err = manager.ObserveDeploymentAudit(context.Background())
	if !errors.Is(err, ErrDeploymentAuditObservation) || result.Catalog != nil || result.SecretNames != nil {
		t.Fatal("callback panic became observation", result, err)
	}
}

func TestDeploymentAuditRefusesUnobservableCustomFactory(t *testing.T) {
	cfg := managerTestConfig(t.TempDir(), &FakeProvider{})
	cfg.Services.BashFactory = bashFactoryFunc(func(context.Context, SessionBinding) (BashExecutor, error) {
		t.Fatal("observation bound executor")
		return nil, nil
	})
	manager := makeManager(t, cfg)
	if result, err := manager.ObserveDeploymentAudit(context.Background()); err == nil || result.SecretNames != nil {
		t.Fatal(result, err)
	}
}

type deploymentSkillLedger struct {
	*skills.Catalog
	ledger  selfaudit.Ledger
	manager *SessionManager
}

func (s *deploymentSkillLedger) SelfAuditProblems() selfaudit.Ledger {
	s.manager.Summary()
	return s.ledger
}

type deploymentTrajectoryLedger struct {
	*auditRecordingStore
	ledger selfaudit.Ledger
}

func (s *deploymentTrajectoryLedger) SelfAuditProblems() selfaudit.Ledger { return s.ledger }

func TestDeploymentAuditCollectsInjectedLedgersWithoutHistoricalIOAndDetaches(t *testing.T) {
	total := problems.FromUint64(2)
	summary := []string{"skill fault (x2)"}
	source := &deploymentSkillLedger{Catalog: skills.EmptyCatalog(), ledger: selfaudit.Ledger{Entries: []string{"skill fault"}, Summary: &summary, Total: &total}}
	recording := &deploymentTrajectoryLedger{auditRecordingStore: &auditRecordingStore{
		list: func(TrajectoryQuery) ([]TrajectorySummary, error) {
			t.Fatal("audit read historical recordings")
			return nil, nil
		},
		visit: func(context.Context, TrajectoryID, TrajectoryEventQuery, func([]byte) error) error {
			t.Fatal("audit read recording bodies")
			return nil
		},
	}, ledger: selfaudit.Ledger{Entries: []string{"recording fault"}}}
	cfg := managerTestConfig(t.TempDir(), &FakeProvider{})
	cfg.Services.Skills = source
	cfg.Services.Trajectories = recording
	manager := makeManager(t, cfg)
	source.manager = manager
	result, err := manager.ObserveDeploymentAudit(context.Background())
	if err != nil || len(result.Skills.Entries) != 1 || len(result.Channels) != 1 || result.Channels[0].Name != "trajectories" {
		t.Fatal(result, err)
	}
	result.Skills.Entries[0] = "changed"
	(*result.Skills.Summary)[0] = "changed"
	if source.ledger.Entries[0] != "skill fault" || summary[0] != "skill fault (x2)" {
		t.Fatal("injected ledger storage exposed")
	}
	source.ledger = selfaudit.Ledger{Failure: &selfaudit.Failure{Class: "OSError"}}
	result, err = manager.ObserveDeploymentAudit(context.Background())
	if err != nil || result.Skills.Failure == nil {
		t.Fatal("failed ledger lost", result, err)
	}
}
