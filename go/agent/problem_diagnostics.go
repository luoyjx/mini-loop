package agent

import (
	"github.com/luoyjx/mini-loop/go/problems"
	"github.com/luoyjx/mini-loop/go/selfaudit"
)

// Called under the holder's mutex. These private logs always use a positive
// source limit, so an empty-eviction error indicates an internal invariant fault.
func recordProblemOccurrence(log **problems.Log, message string) {
	if *log == nil {
		*log = problems.New(problems.MaxDistinctProblems)
	}
	if err := (*log).Append(message); err != nil {
		panic("invalid internal problem-log limit")
	}
}
func problemObservation(log *problems.Log) selfaudit.Ledger {
	if log == nil {
		var empty problems.Log
		return selfaudit.FromProblems(empty.Snapshot())
	}
	return selfaudit.FromProblems(log.Snapshot())
}
func (broker *ApprovalBroker) SelfAuditProblems() selfaudit.Ledger {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	return problemObservation(broker.problemOccurrences)
}
func (journal *InMemoryActionJournal) SelfAuditProblems() selfaudit.Ledger {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	return problemObservation(journal.problemOccurrences)
}
