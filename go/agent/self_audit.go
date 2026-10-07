package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/selfaudit"
)

// ObserveSelfAudit is a read-only embedding surface. The caller establishes
// authority; authenticated frontends must pair an owner with IncludeGlobal=false.
// No manager/session lock spans storage IO or an injected service callback.
func (manager *SessionManager) ObserveSelfAudit(ctx context.Context, scope selfaudit.Scope) selfaudit.Observations {
	return manager.observeSelfAudit(ctx, scope, auditReportCollection)
}

// ObserveSelfAuditProblems reads only existing ledgers. Suggestions/drafts do not
// inspect activity, enumerate cron jobs or read trajectory summaries/events.
func (manager *SessionManager) ObserveSelfAuditProblems(scope selfaudit.Scope) selfaudit.Observations {
	return manager.observeSelfAudit(context.Background(), scope, auditProblemCollection)
}

type auditCollection uint8

const (
	auditReportCollection auditCollection = iota
	auditProblemCollection
)

func (manager *SessionManager) observeSelfAudit(ctx context.Context, scope selfaudit.Scope, collection auditCollection) selfaudit.Observations {
	var owner *string
	if scope.Owner != nil {
		value := *scope.Owner
		owner = &value
	}
	manager.mu.Lock()
	total := len(manager.sessions)
	handles := make([]*ManagedSession, 0, len(manager.sessions))
	for _, id := range manager.order {
		if handle := manager.sessions[id]; handle != nil && (owner == nil || string(handle.Owner()) == *owner) {
			handles = append(handles, handle)
		}
	}
	manager.mu.Unlock()
	// createdAt is immutable after publication. Stable ties preserve manager order.
	sort.SliceStable(handles, func(i, j int) bool { return handles[i].createdAt > handles[j].createdAt })
	if len(handles) > selfaudit.MaxSessionsScanned {
		handles = handles[:selfaudit.MaxSessionsScanned]
	}
	if owner != nil {
		total = len(handles)
	}
	result := selfaudit.Observations{TotalSessions: &total, Sessions: make([]selfaudit.Session, 0, len(handles))}
	for _, handle := range handles {
		session := observeSessionProblems(handle)
		if collection == auditReportCollection {
			session.Activity, session.InspectionFailure = observeActivity(handle)
		}
		result.Sessions = append(result.Sessions, session)
	}
	if scope.IncludeGlobal {
		if scheduler := manager.cron; scheduler != nil {
			result.Problems.Cron = observeProblemSource(scheduler)
			if collection == auditReportCollection {
				result.Cron = scheduler.SelfAuditCron()
			}
		}
		if source, ok := manager.config.Services.Trajectories.(selfaudit.ProblemSource); ok {
			result.Problems.Trajectories = observeProblemSource(source)
		}
		if source := manager.config.Services.Approvals; source != nil {
			result.Problems.Approvals = observeProblemSource(source)
		}
		if source, ok := manager.config.Services.Skills.(selfaudit.ProblemSource); ok {
			result.Problems.Skills = observeProblemSource(source)
		}
		if source, ok := manager.config.Services.ActionJournal.(selfaudit.ProblemSource); ok {
			result.Problems.Actions = observeProblemSource(source)
		}
	}
	// Raw shared memory is allowed only in a fleet view. Owner views use each
	// immutable binding's attributed ledger, never the backing store's filenames.
	if owner == nil && scope.IncludeGlobal && manager.config.Services.UserResources == nil && manager.config.Services.Memory != nil {
		for i, handle := range handles {
			if handle.core.memory != nil {
				result.Sessions[i].Problems.Memory = observeProblemSource(manager.config.Services.Memory)
			}
		}
	}
	if collection == auditReportCollection {
		result.Trajectories = manager.observeAuditTrajectories(ctx, owner, handles)
	} else {
		// A failed ledger collection is not an empty suggestion set. Report mode
		// renders per-ledger failure lines; problem-only callers receive an error.
		ledgers := []*selfaudit.Ledger{result.Problems.Cron, result.Problems.Trajectories, result.Problems.Approvals, result.Problems.Skills, result.Problems.Actions}
		for _, session := range result.Sessions {
			ledgers = append(ledgers, session.Problems.Registry, session.Problems.Tasks, session.Problems.Teams, session.Problems.Memory)
		}
		for _, ledger := range ledgers {
			if ledger != nil && ledger.Failure != nil {
				result.ProblemsFailure = ledger.Failure
				break
			}
		}
	}
	return result
}

func observeProblemSource(source selfaudit.ProblemSource) (result *selfaudit.Ledger) {
	defer func() {
		if recover() != nil {
			result = &selfaudit.Ledger{Failure: &selfaudit.Failure{Class: "RuntimeError"}}
		}
	}()
	ledger := source.SelfAuditProblems()
	return &ledger
}
func observeSessionProblems(handle *ManagedSession) (result selfaudit.Session) {
	result = selfaudit.Session{ID: string(handle.ID()), Owner: string(handle.Owner()), CreatedAt: handle.createdAt, AgentPresent: true}
	if gate := handle.core.gate; gate != nil {
		ledger := observeProblemSource(gate)
		if ledger.Failure != nil || len(ledger.Entries) > 0 {
			result.Problems.Registry = ledger
		}
	}
	if store := handle.core.taskDiagnostics.Load(); store != nil {
		result.Problems.Tasks = observeProblemSource(store)
	}
	if store := handle.core.memory; store != nil {
		result.Problems.Memory = observeProblemSource(store)
	}
	return result
}
func observeActivity(handle *ManagedSession) (activity *string, failure *selfaudit.Failure) {
	defer func() {
		if recover() != nil {
			failure = &selfaudit.Failure{Class: "RuntimeError"}
		}
	}()
	info := handle.Info()
	value := string(info.Activity)
	return &value, nil
}

var errAuditObservationLimit = errors.New("self-audit event observation budget exceeded")

func auditFailure(err error) *selfaudit.Failure {
	class := "OSError"
	if errors.Is(err, errAuditObservationLimit) {
		class = "ObservationLimitError"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		class = "CancelledError"
	}
	var syntax *json.SyntaxError
	var shape *json.UnmarshalTypeError
	if errors.As(err, &syntax) || errors.As(err, &shape) {
		class = "ValueError"
	}
	return &selfaudit.Failure{Class: class}
}

func (manager *SessionManager) observeAuditTrajectories(ctx context.Context, owner *string, handles []*ManagedSession) (result *selfaudit.Trajectories) {
	store := manager.config.Services.Trajectories
	if store == nil {
		return nil
	}
	result = &selfaudit.Trajectories{HasEvents: true, BySession: map[string][]selfaudit.Recording{}}
	defer func() {
		if recover() != nil {
			failure := &selfaudit.Failure{Class: "RuntimeError"}
			result.TrendsFailure = failure
			result.UsageFailure = failure
		}
	}()
	budget := 64 * 1024 * 1024
	collect := func(query TrajectoryQuery, limit int) ([]selfaudit.Recording, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		summaries, err := store.List(query)
		if err != nil {
			return nil, err
		}
		recordings := make([]selfaudit.Recording, 0, limit)
		for _, summary := range summaries {
			if len(recordings) >= limit {
				break
			}
			if owner != nil && (query.Session == nil || summary.Session != *query.Session || (summary.Owner != nil && string(*summary.Owner) != *owner)) {
				continue
			}
			status := string(summary.Status)
			recording := selfaudit.Recording{ID: string(summary.ID), Status: &status, Partial: summary.Partial, DurationMilliseconds: clonePointer(summary.DurationMS)}
			recording.ToolUses, recording.EventFailure = observeAuditEvents(ctx, store, summary.ID, &budget)
			recordings = append(recordings, recording)
		}
		return recordings, nil
	}
	if owner == nil {
		recordings, err := collect(TrajectoryQuery{Limit: selfaudit.MaxTrajectoriesScanned}, selfaudit.MaxTrajectoriesScanned)
		result.Global = recordings
		if err != nil {
			result.TrendsFailure = auditFailure(err)
			result.UsageFailure = auditFailure(err)
		}
	} else {
		total := 0
		for i, handle := range handles {
			if i >= 20 || total >= selfaudit.MaxTrajectoriesScanned {
				break
			}
			id := handle.ID()
			recordings, err := collect(TrajectoryQuery{Session: &id, Limit: 10}, min(10, selfaudit.MaxTrajectoriesScanned-total))
			if err != nil {
				result.TrendsFailure = auditFailure(err)
				result.UsageFailure = auditFailure(err)
				break
			}
			if len(recordings) > selfaudit.MaxTrajectoriesScanned-total {
				recordings = recordings[:selfaudit.MaxTrajectoriesScanned-total]
			}
			result.BySession[string(id)] = recordings
			total += len(recordings)
		}
	}
	return result
}

func observeAuditEvents(ctx context.Context, store TrajectoryReader, id TrajectoryID, budget *int) (uses []selfaudit.ToolUse, failure *selfaudit.Failure) {
	defer func() {
		if recover() != nil {
			failure = &selfaudit.Failure{Class: "RuntimeError"}
		}
	}()
	count := 0
	err := store.VisitRecords(ctx, id, TrajectoryEventQuery{Types: []SessionEventKind{EventToolUse}, Limit: 200}, func(data []byte) error {
		if count >= 200 {
			return nil
		}
		count++
		// RawMessage exists only while decoding this capped wire record.
		if len(data) > *budget {
			return errAuditObservationLimit
		}
		*budget -= len(data)
		var event struct {
			Name  protocol.ToolName `json:"name"`
			Input json.RawMessage   `json:"input"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			return err
		}
		use := selfaudit.ToolUse{Name: event.Name}
		if event.Name == protocol.ToolLoadSkill {
			var input struct {
				Name *string `json:"name"`
			}
			if err := json.Unmarshal(event.Input, &input); err != nil {
				return err
			}
			use.Skill = input.Name
		}
		uses = append(uses, use)
		return nil
	})
	if err != nil {
		return uses, auditFailure(err)
	}
	return uses, nil
}
