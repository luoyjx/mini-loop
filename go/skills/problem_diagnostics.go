package skills

import (
	"github.com/luoyjx/mini-loop/go/problems"
	"github.com/luoyjx/mini-loop/go/selfaudit"
)

func (catalog *Catalog) SelfAuditProblems() selfaudit.Ledger {
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	snapshot := problems.Snapshot{Total: problems.FromUint64(catalog.problemTotal), Churning: catalog.problemDropped > MaxProblems}
	for _, entry := range catalog.problems {
		snapshot.Entries = append(snapshot.Entries, problems.Entry{Text: entry.Message, Count: problems.FromUint64(uint64(entry.Count))})
	}
	return selfaudit.FromProblems(snapshot)
}
func (catalog *LayeredCatalog) SelfAuditProblems() selfaudit.Ledger {
	return catalog.diagnostics.SelfAuditProblems()
}
