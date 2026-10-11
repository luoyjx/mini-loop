package tasks

import (
	"github.com/luoyjx/mini-loop/go/problems"
	"github.com/luoyjx/mini-loop/go/selfaudit"
)

func (s *Store) SelfAuditProblems() selfaudit.Ledger {
	s.diagnosticsMu.Lock()
	defer s.diagnosticsMu.Unlock()
	snapshot := problems.Snapshot{Total: problems.FromUint64(s.diagnostics.Total), Churning: s.diagnostics.Dropped > MaxProblems}
	for _, entry := range s.diagnostics.Problems {
		snapshot.Entries = append(snapshot.Entries, problems.Entry{Text: entry.Message, Count: problems.FromUint64(entry.Count)})
	}
	return selfaudit.FromProblems(snapshot)
}
