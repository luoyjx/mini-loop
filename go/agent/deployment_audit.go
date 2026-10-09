package agent

import (
	"context"
	"errors"
	"math/big"

	"github.com/luoyjx/mini-loop/go/problems"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/selfaudit"
	"github.com/luoyjx/mini-loop/go/shell"
)

var ErrDeploymentAuditObservation = errors.New("deployment audit observation failed")

type deploymentSecretNames interface{ Names() []secrets.Name }
type deploymentUnresolved interface{ Unresolved() []secrets.Name }
type deploymentShort interface{ ShortValues() []secrets.Name }

// ObserveDeploymentAudit is a privileged, fleet-wide local observation. It is
// not owner admission and must not be exposed on public health or model tools.
// Manager/template components are observed, not first-session overrides. No
// session, task board, provider call, secret lookup or historical IO is started.
// Callbacks run outside manager locks; individual ledgers own snapshot locking.
func (manager *SessionManager) ObserveDeploymentAudit(ctx context.Context) (result DeploymentAuditObservation, err error) {
	defer func() {
		if recover() != nil {
			result = DeploymentAuditObservation{}
			err = ErrDeploymentAuditObservation
		}
	}()
	if err := ctx.Err(); err != nil {
		return DeploymentAuditObservation{}, err
	}
	manager.mu.Lock()
	services := manager.config.Services
	workflows := manager.workflows != nil
	handles := make([]*ManagedSession, 0, len(manager.sessions))
	for _, id := range manager.order {
		if h := manager.sessions[id]; h != nil {
			handles = append(handles, h)
		}
	}
	manager.mu.Unlock()
	result = DeploymentAuditObservation{SecretNames: []secrets.Name{}, Unresolved: []secrets.Name{}, ShortValues: []secrets.Name{}, Channels: []DeploymentProblemChannel{}, StateNull: services.StateStore == nil, Workflows: workflows}
	if services.Tools != nil {
		result.Catalog = &DeploymentCatalogObservation{Tools: []DeploymentToolObservation{}}
		for _, name := range services.Tools.Names() {
			if definition, ok := services.Tools.Lookup(name); ok {
				result.Catalog.Tools = append(result.Catalog.Tools, DeploymentToolObservation{Name: name, Risk: definition.Risk(), ParallelSafe: definition.ParallelSafe()})
			}
		}
	}
	source, ok := services.BashFactory.(shell.SandboxPostureProvider)
	if !ok {
		return DeploymentAuditObservation{}, errors.New("sandbox posture observation is unavailable")
	}
	sandbox := source.SandboxPosture()
	if sandbox.Name == "" {
		return DeploymentAuditObservation{}, errors.New("sandbox posture observation is unavailable")
	}
	result.SandboxNull = sandbox.Name == "None" || sandbox.Name == "NullSandbox"
	result.SandboxReason = clonePointer(sandbox.Reason)
	switch services.Secrets.(type) {
	case nil, secrets.Null, *secrets.Null:
		result.SecretsNull = true
	}
	if registry, ok := services.Secrets.(deploymentSecretNames); ok {
		result.SecretNames = append([]secrets.Name{}, registry.Names()...)
	}
	if registry, ok := services.Secrets.(deploymentUnresolved); ok {
		result.Unresolved = append([]secrets.Name{}, registry.Unresolved()...)
	}
	if registry, ok := services.Secrets.(deploymentShort); ok {
		result.ShortValues = append([]secrets.Name{}, registry.ShortValues()...)
	}
	switch services.ActionJournal.(type) {
	case *InMemoryActionJournal:
		result.JournalMemory = true
	}
	empty := func() selfaudit.Ledger { var log problems.Log; return selfaudit.FromProblems(log.Snapshot()) }
	result.Cron = empty()
	result.Skills = empty()
	if manager.cron != nil {
		result.Cron = detachDeploymentLedger(*observeProblemSource(manager.cron))
	}
	if source, ok := services.Skills.(selfaudit.ProblemSource); ok {
		result.Skills = detachDeploymentLedger(*observeProblemSource(source))
	}
	// These are the manager-held native counterparts of Source's generic sweep.
	// Per-session task/gate/owner ledgers belong to owner-scoped self-audit, not
	// this Source deployment audit. No owner bundle or lazy task board is opened.
	addChannel := func(name DeploymentSubsystemName, source selfaudit.ProblemSource) {
		if source == nil {
			return
		}
		ledger := detachDeploymentLedger(*observeProblemSource(source))
		if ledger.Failure != nil || len(ledger.Entries) > 0 {
			result.Channels = append(result.Channels, DeploymentProblemChannel{Name: name, Ledger: ledger})
		}
	}
	if source, ok := services.ActionJournal.(selfaudit.ProblemSource); ok {
		addChannel("actions", source)
	}
	addChannel("approvals", services.Approvals)
	if manager.teams != nil {
		ledger := selfaudit.FromProblems(manager.teams.Problems())
		if len(ledger.Entries) > 0 {
			result.Channels = append(result.Channels, DeploymentProblemChannel{Name: "bus", Ledger: ledger})
		}
	}
	if services.Memory != nil {
		addChannel("memory", services.Memory)
	}
	if source, ok := services.Trajectories.(selfaudit.ProblemSource); ok {
		addChannel("trajectories", source)
	}
	snapshots := make([]problems.Snapshot, 0, len(handles))
	for _, handle := range handles {
		if err := ctx.Err(); err != nil {
			return DeploymentAuditObservation{}, err
		}
		if result.PersistenceError == nil {
			if fault := handle.PersistenceStatus().Error; fault != nil && *fault != "" {
				result.PersistenceError = clonePointer(fault)
			}
		}
		snapshots = append(snapshots, handle.core.MCPProblems())
	}
	result.MCP = selfaudit.FromProblems(mergeDeploymentMCP(snapshots))
	if err := ctx.Err(); err != nil {
		return DeploymentAuditObservation{}, err
	}
	return result, nil
}

func detachDeploymentLedger(ledger selfaudit.Ledger) selfaudit.Ledger {
	ledger.Entries = append([]string{}, ledger.Entries...)
	if ledger.Summary != nil {
		copy := append([]string{}, (*ledger.Summary)...)
		ledger.Summary = &copy
	}
	if ledger.Total != nil {
		copy, err := problems.FromBigInt(ledger.Total.BigInt())
		if err != nil {
			panic(err)
		}
		ledger.Total = &copy
	}
	if ledger.Failure != nil {
		copy := *ledger.Failure
		ledger.Failure = &copy
	}
	return ledger
}

// Source replays only each retained MCP entry's occurrence count across sessions
// in insertion order. Evicted source history is not replayed. Bulk addition is
// equivalent to that replay, including bounded retention, without O(count) work.
func mergeDeploymentMCP(snapshots []problems.Snapshot) problems.Snapshot {
	order := []string{}
	counts := make(map[string]*big.Int)
	total, dropped := new(big.Int), new(big.Int)
	for _, snapshot := range snapshots {
		for _, entry := range snapshot.Entries {
			count := entry.Count.BigInt()
			if count.Sign() == 0 {
				continue
			}
			total.Add(total, count)
			if prior := counts[entry.Text]; prior != nil {
				prior.Add(prior, count)
				continue
			}
			if len(order) >= problems.MaxDistinctProblems {
				delete(counts, order[0])
				order = order[1:]
				dropped.Add(dropped, big.NewInt(1))
			}
			order = append(order, entry.Text)
			counts[entry.Text] = new(big.Int).Set(count)
		}
	}
	exact := func(value *big.Int) problems.Counter {
		result, err := problems.FromBigInt(value)
		if err != nil {
			panic(err)
		}
		return result
	}
	result := problems.Snapshot{Entries: []problems.Entry{}, Total: exact(total), Dropped: exact(dropped), Limit: problems.MaxDistinctProblems, Churning: dropped.Cmp(big.NewInt(problems.MaxDistinctProblems)) > 0}
	for _, text := range order {
		result.Entries = append(result.Entries, problems.Entry{Text: text, Count: exact(counts[text])})
	}
	return result
}
