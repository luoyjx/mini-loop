package trajectory

import (
	"github.com/luoyjx/mini-loop/go/problems"
	"github.com/luoyjx/mini-loop/go/selfaudit"
)

func (s *Store) SelfAuditProblems() selfaudit.Ledger {
	entries, total, dropped := s.ProblemDiagnostics()
	snapshot := problems.Snapshot{Total: problems.FromUint64(total), Churning: dropped > problems.MaxDistinctProblems}
	for _, entry := range entries {
		snapshot.Entries = append(snapshot.Entries, problems.Entry{Text: entry.Message, Count: problems.FromUint64(entry.Count)})
	}
	return selfaudit.FromProblems(snapshot)
}
