package selfaudit

import (
	"crypto/sha256"
	"fmt"
	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"sort"
	"strconv"
	"strings"
)

type ObservationStage string

const (
	SessionsObservation ObservationStage = "sessions"
	ProblemsObservation ObservationStage = "problems"
)

type ObservationError struct {
	Stage ObservationStage
	Class string
}

func (e *ObservationError) Error() string { return "self-audit " + string(e.Stage) + ": " + e.Class }

type namedLedger struct {
	name   string
	ledger *Ledger
}

func recent(observations Observations, owner *string) ([]Session, error) {
	if observations.SessionsFailure != nil {
		return nil, &ObservationError{SessionsObservation, observations.SessionsFailure.Class}
	}
	sessions := make([]Session, 0, len(observations.Sessions))
	for _, session := range observations.Sessions {
		if owner == nil || session.Owner == *owner {
			sessions = append(sessions, session)
		}
	}
	sort.SliceStable(sessions, func(i, j int) bool { return sessions[i].CreatedAt > sessions[j].CreatedAt })
	if len(sessions) > MaxSessionsScanned {
		sessions = sessions[:MaxSessionsScanned]
	}
	return sessions, nil
}
func ledgers(observations Observations, sessions []Session, global bool) []namedLedger {
	result := []namedLedger{}
	add := func(name string, ledger *Ledger, requireEntries bool) {
		if ledger != nil && (!requireEntries || len(ledger.Entries) > 0) {
			result = append(result, namedLedger{name, ledger})
		}
	}
	if global {
		add("cron", observations.Problems.Cron, false)
		add("trajectories", observations.Problems.Trajectories, false)
		add("approvals", observations.Problems.Approvals, false)
		add("skills", observations.Problems.Skills, false)
		add("actions", observations.Problems.Actions, false)
	}
	for _, session := range sessions {
		if session.AgentPresent {
			add("registry["+session.ID+"]", session.Problems.Registry, true)
			add("tasks["+session.ID+"]", session.Problems.Tasks, true)
			add("teams["+session.ID+"]", session.Problems.Teams, true)
			add("memory["+session.ID+"]", session.Problems.Memory, true)
		}
	}
	return result
}
func recordings(observations Observations, owner *string, usage bool) ([]Recording, *Failure) {
	store := observations.Trajectories
	if store == nil {
		return nil, nil
	}
	failure := store.TrendsFailure
	if usage {
		failure = store.UsageFailure
	}
	if owner == nil {
		if failure != nil {
			return nil, failure
		}
		return store.Global[:min(len(store.Global), MaxTrajectoriesScanned)], nil
	}
	sessions, err := recent(observations, owner)
	if err != nil {
		return nil, observations.SessionsFailure
	}
	if failure != nil {
		return nil, failure
	}
	result := []Recording{}
	for _, session := range sessions[:min(len(sessions), 20)] {
		rows := store.BySession[session.ID]
		result = append(result, rows[:min(len(rows), 10)]...)
		if len(result) >= MaxTrajectoriesScanned {
			break
		}
	}
	return result[:min(len(result), MaxTrajectoriesScanned)], nil
}
func section(out []string, title string, lines []string) []string {
	out = append(out, "## "+title)
	if len(lines) == 0 {
		out = append(out, "(nothing)")
	} else {
		out = append(out, lines...)
	}
	return append(out, "")
}
func unreadable(failure *Failure) []string { return []string{"unreadable: " + failure.Class} }
func distribution(counts map[string]int) []string {
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	lines := make([]string, 0, len(names))
	for _, name := range names {
		lines = append(lines, fmt.Sprintf("%d %s", counts[name], name))
	}
	return lines
}
func head(value string, n int) string {
	characters := []rune(value)
	if len(characters) > n {
		characters = characters[:n]
	}
	return string(characters)
}

// BuildReport consumes observations without launching work or reading storage.
// Section failures stay visible; private exception messages are never rendered.
func BuildReport(observations Observations, scope Scope) string {
	out := []string{"# self-audit"}
	sessions, err := recent(observations, scope.Owner)
	if err != nil {
		out = section(out, "sessions", unreadable(observations.SessionsFailure))
	} else {
		total := len(observations.Sessions)
		if scope.Owner != nil {
			total = len(sessions)
		}
		counts := map[string]int{}
		for _, session := range sessions {
			activity := "unknown"
			if session.Activity != nil {
				activity = *session.Activity
			}
			if session.InspectionFailure != nil {
				activity = "uninspectable"
			}
			counts[activity]++
		}
		lines := distribution(counts)
		if total > len(sessions) {
			lines = append(lines, fmt.Sprintf("(distribution over the %d most recent of %d sessions)", len(sessions), total))
		}
		out = section(out, fmt.Sprintf("sessions (%d)", total), lines)
	}
	if observations.SessionsFailure != nil {
		out = section(out, "problems", []string{"unreadable: UnboundLocalError"})
	} else if observations.ProblemsFailure != nil {
		out = section(out, "problems", unreadable(observations.ProblemsFailure))
	} else {
		lines := []string{}
		for _, source := range ledgers(observations, sessions, scope.IncludeGlobal) {
			ledger := source.ledger
			if ledger.Failure != nil {
				lines = append(lines, "### "+source.name+": unreadable ("+ledger.Failure.Class+")")
				continue
			}
			summary := ledger.Entries
			if ledger.Summary != nil {
				summary = *ledger.Summary
			}
			if len(summary) == 0 {
				continue
			}
			total := len(summary)
			if ledger.Total != nil {
				total = *ledger.Total
			}
			churn := ""
			if ledger.Churning {
				churn = " (churning: counts are lower bounds)"
			}
			lines = append(lines, fmt.Sprintf("### %s: %d reported%s", source.name, total, churn))
			for _, line := range summary {
				lines = append(lines, "- "+line)
			}
		}
		out = section(out, "problems", lines)
	}
	if observations.Trajectories == nil {
		out = section(out, "trajectories", []string{"(recording disabled)"})
	} else {
		summaries, failure := recordings(observations, scope.Owner, false)
		if failure != nil {
			out = section(out, "trajectories", unreadable(failure))
		} else {
			counts := map[string]int{}
			durations := []Recording{}
			for _, row := range summaries {
				status := "unknown"
				if row.Status != nil {
					status = *row.Status
				}
				if row.Partial {
					status += "+partial"
				}
				counts[status]++
				if row.DurationMilliseconds != nil {
					durations = append(durations, row)
				}
			}
			sort.SliceStable(durations, func(i, j int) bool {
				a, b := *durations[i].DurationMilliseconds, *durations[j].DurationMilliseconds
				if a != b {
					return a > b
				}
				return durations[i].ID > durations[j].ID
			})
			lines := distribution(counts)
			if len(durations) > 0 {
				slow := []string{}
				for _, row := range durations[:min(len(durations), 3)] {
					slow = append(slow, row.ID+" ("+strconv.FormatFloat(*row.DurationMilliseconds/1000, 'f', 1, 64)+"s)")
				}
				lines = append(lines, "slowest: "+strings.Join(slow, ", "))
			}
			lines = append(lines, fmt.Sprintf("(the %d most recent recordings)", len(summaries)))
			out = section(out, "trajectories", lines)
		}
	}
	if observations.Trajectories != nil && observations.Trajectories.HasEvents {
		summaries, failure := recordings(observations, scope.Owner, true)
		type usage struct{ loads, bad int }
		counts := map[string]usage{}
		if failure == nil {
			for _, row := range summaries {
				if row.EventFailure != nil {
					failure = row.EventFailure
					break
				}
				bad := row.Status != nil && (*row.Status == "error" || *row.Status == "interrupted")
				for _, event := range row.ToolUses[:min(len(row.ToolUses), 200)] {
					if event.Name != "load_skill" {
						continue
					}
					name := "?"
					if event.Skill != nil {
						name = *event.Skill
					}
					count := counts[name]
					count.loads++
					if bad {
						count.bad++
					}
					counts[name] = count
				}
			}
		}
		if failure != nil {
			out = section(out, "skill usage", unreadable(failure))
		} else {
			names := []string{}
			for name := range counts {
				names = append(names, name)
			}
			sort.Strings(names)
			lines := []string{}
			for _, name := range names {
				count := counts[name]
				lines = append(lines, fmt.Sprintf("%s: %d load(s), %d in turns that ended error/interrupted", name, count.loads, count.bad))
			}
			if len(lines) > 0 {
				lines = append(lines, "(correlation, not causation: a skill loaded in a bad turn is a lead, not a verdict)")
			}
			out = section(out, "skill usage", lines)
		}
	}
	if scope.IncludeGlobal {
		if observations.Cron.Failure != nil {
			out = section(out, "cron", unreadable(observations.Cron.Failure))
		} else {
			armed := map[string]bool{}
			for _, id := range observations.Cron.Armed {
				armed[id] = true
			}
			disarmed := []string{}
			for _, id := range observations.Cron.Jobs {
				if !armed[id] {
					disarmed = append(disarmed, id)
				}
			}
			sort.Strings(disarmed)
			lines := []string{fmt.Sprintf("%d scheduled, %d disarmed", len(observations.Cron.Jobs), len(disarmed))}
			if len(disarmed) > 0 {
				lines = append(lines, "disarmed (restored, awaiting operator arm): "+strings.Join(disarmed[:min(len(disarmed), 10)], ", "))
			}
			out = section(out, "cron", lines)
		}
	}
	report := strings.TrimRightFunc(strings.Join(out, "\n"), pytext.IsSpace)
	if len([]rune(report)) > MaxReportCharacters {
		return head(report, MaxReportCharacters) + "\n[report truncated at the cap]"
	}
	return report
}

func problems(observations Observations, owner *string, limit int) ([]Suggestion, error) {
	sessions, err := recent(observations, owner)
	if err != nil {
		return nil, err
	}
	if observations.ProblemsFailure != nil {
		return nil, &ObservationError{ProblemsObservation, observations.ProblemsFailure.Class}
	}
	result := []Suggestion{}
	seen := map[string]bool{}
	for _, source := range ledgers(observations, sessions, owner == nil) {
		entries := source.ledger.Entries
		for _, entry := range entries[max(0, len(entries)-3):] {
			problem := head(pytext.Strip(entry), 300)
			if problem == "" || seen[problem] {
				continue
			}
			seen[problem] = true
			result = append(result, Suggestion{source.name, problem, "Find and eliminate the failure mode behind this recurring runtime problem: " + problem})
			if len(result) >= max(1, limit) {
				return result, nil
			}
		}
	}
	return result, nil
}
func SuggestObjectives(observations Observations, owner *string, limit int) ([]Suggestion, error) {
	return problems(observations, owner, limit)
}
func SuggestBenchTasks(observations Observations, owner *string, limit int) ([]BenchTaskDraft, error) {
	suggestions, err := problems(observations, owner, limit)
	if err != nil {
		return nil, err
	}
	result := make([]BenchTaskDraft, 0, len(suggestions))
	for _, suggestion := range suggestions {
		digest := sha256.Sum256([]byte(suggestion.Problem))
		result = append(result, BenchTaskDraft{Source: suggestion.Source, Problem: suggestion.Problem, Name: fmt.Sprintf("%s%x", DraftTaskPrefix, digest[:4]), PromptDraft: "Reproduce the workload behind this recorded friction and complete it: " + suggestion.Problem, Note: "a human authors the expect predicate and admits the task by editing benchmark.py; admission is a judge-side change, never automatic"})
	}
	return result, nil
}
