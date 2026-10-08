package workflows

import (
	"cmp"
	"slices"
)

type TerminalRunLimit int

const MaxTerminalRuns TerminalRunLimit = 500

// PruneTerminalRuns retains the newest terminal runs with no unread outbox.
// Nil selects MaxTerminalRuns. Source negative limits evict every eligible run.
// Returned IDs let a service remove its parallel per-run bookkeeping.
func (s *InMemoryStore) PruneTerminalRuns(keep *TerminalRunLimit) []RunID {
	limit := MaxTerminalRuns
	if keep != nil {
		limit = *keep
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pruned := []RunID{}
	if TerminalRunLimit(len(s.runs)) <= limit {
		return pruned
	}
	unread := map[RunID]bool{}
	for _, m := range s.outbox {
		if m.DeliveredAt == nil {
			unread[m.RunID] = true
		}
	}
	eligible := []WorkflowRun{}
	for _, r := range s.runs {
		if r.Terminal() && !unread[r.RunID] {
			eligible = append(eligible, r)
		}
	}
	if TerminalRunLimit(len(eligible)) <= limit {
		return pruned
	}
	slices.SortFunc(eligible, func(a, b WorkflowRun) int {
		if order := cmp.Compare(a.CreatedAt, b.CreatedAt); order != 0 {
			return order
		}
		return cmp.Compare(a.RunID, b.RunID)
	})
	count := len(eligible)
	// Avoid overflow for the native minimum int while preserving Python slicing.
	if limit >= 0 {
		count -= int(limit)
	}
	if count == 0 {
		return pruned
	}
	removed := map[RunID]bool{}
	for _, r := range eligible[:count] {
		removed[r.RunID] = true
		pruned = append(pruned, r.RunID)
		delete(s.runs, r.RunID)
	}
	s.attemptOrder = slices.DeleteFunc(s.attemptOrder, func(id AttemptID) bool { return removed[s.attempts[id].RunID] })
	s.outboxOrder = slices.DeleteFunc(s.outboxOrder, func(id OutboxID) bool { return removed[s.outbox[id].RunID] })
	// Cascade each owned map once, independent of the number of evicted runs.
	for key := range s.nodes {
		if removed[key.run] {
			delete(s.nodes, key)
		}
	}
	for id, a := range s.attempts {
		if removed[a.RunID] {
			delete(s.attempts, id)
		}
	}
	for id, a := range s.artifacts {
		if removed[a.Snapshot().RunID] {
			delete(s.artifacts, id)
		}
	}
	for id, m := range s.outbox {
		if removed[m.RunID] {
			delete(s.outbox, id)
		}
	}
	for key := range s.outboxKeys {
		if removed[key.run] {
			delete(s.outboxKeys, key)
		}
	}
	for key, launch := range s.launches {
		if removed[launch.run] {
			delete(s.launches, key)
		}
	}
	return pruned
}
