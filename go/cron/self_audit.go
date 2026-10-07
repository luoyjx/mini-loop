package cron

import (
	"github.com/luoyjx/mini-loop/go/problems"
	"github.com/luoyjx/mini-loop/go/selfaudit"
)

func (s *Scheduler) SelfAuditProblems() selfaudit.Ledger {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := problems.Snapshot{Total: problems.FromUint64(s.problems.Total), Churning: s.problems.Dropped > 50}
	for _, entry := range s.problems.Entries {
		snapshot.Entries = append(snapshot.Entries, problems.Entry{Text: entry.Text, Count: problems.FromUint64(entry.Count)})
	}
	return selfaudit.FromProblems(snapshot)
}

// SelfAuditCron reads jobs and arming together, never retaining prompts/targets.
func (s *Scheduler) SelfAuditCron() selfaudit.Cron {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := selfaudit.Cron{Jobs: []string{}, Armed: []string{}}
	for _, job := range s.jobs {
		result.Jobs = append(result.Jobs, string(job.ID))
		if s.armed[job.ID] {
			result.Armed = append(result.Armed, string(job.ID))
		}
	}
	return result
}
