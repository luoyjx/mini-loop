package memory

import (
	"github.com/luoyjx/mini-loop/go/problems"
	"github.com/luoyjx/mini-loop/go/selfaudit"
)

// SelfAuditProblems exposes the raw store only to privileged embedding callers.
func (store *Store) SelfAuditProblems() selfaudit.Ledger {
	return selfaudit.FromProblems(store.problemOccurrences.Snapshot())
}

// SelfAuditProblems includes only diagnostics produced by this fixed owner's
// writes. Unreadable shared files have no trustworthy owner and stay fleet-only.
// Bindings retain separate bounded ledgers; they do not aggregate other bindings.
func (s *ScopedStore) SelfAuditProblems() selfaudit.Ledger {
	return selfaudit.FromProblems(s.diagnostics.Snapshot())
}

func appendDiagnostic(log *problems.Log, message string) {
	if err := log.Append(message); err != nil {
		panic("invalid internal memory diagnostic limit")
	}
}
