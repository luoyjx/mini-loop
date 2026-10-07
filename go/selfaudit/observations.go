// Package selfaudit renders typed runtime observations. It launches no work and
// cannot admit a benchmark draft: the live manager/HTTP binding is separate.
package selfaudit

import (
	"bytes"
	"errors"
	"github.com/luoyjx/mini-loop/go/protocol"
)

const MaxReportCharacters = 8000
const MaxSessionsScanned = 100
const MaxTrajectoriesScanned = 50
const MaxSuggestions = 8
const DraftTaskPrefix = "ledger-"

// Failure retains the class of a failed observation, never its private message.
type Failure struct {
	Class string `json:"class"`
}
type Ledger struct {
	Entries  []string  `json:"entries"`
	Summary  *[]string `json:"summary,omitempty"`
	Total    *int      `json:"total,omitempty"`
	Churning bool      `json:"churning"`
	Failure  *Failure  `json:"failure,omitempty"`
}
type GlobalLedgers struct {
	Cron         *Ledger `json:"cron,omitempty"`
	Trajectories *Ledger `json:"trajectories,omitempty"`
	Approvals    *Ledger `json:"approvals,omitempty"`
	Skills       *Ledger `json:"skills,omitempty"`
	Actions      *Ledger `json:"actions,omitempty"`
}
type SessionLedgers struct {
	Registry *Ledger `json:"registry,omitempty"`
	Tasks    *Ledger `json:"tasks,omitempty"`
	Teams    *Ledger `json:"teams,omitempty"`
	Memory   *Ledger `json:"memory,omitempty"`
}
type Session struct {
	ID                string         `json:"id"`
	Owner             string         `json:"owner"`
	CreatedAt         float64        `json:"created_at"`
	Activity          *string        `json:"activity,omitempty"`
	InspectionFailure *Failure       `json:"inspection_failure,omitempty"`
	AgentPresent      bool           `json:"agent_present"`
	Problems          SessionLedgers `json:"problems"`
}

// ToolUse observations have already passed the trajectory type filter. Other
// tool uses still consume the 200-event scan budget, as they do in the source.
type ToolUse struct {
	Name  protocol.ToolName `json:"name"`
	Skill *string           `json:"skill,omitempty"`
}
type Recording struct {
	ID                   string    `json:"id"`
	Status               *string   `json:"status,omitempty"`
	Partial              bool      `json:"partial"`
	DurationMilliseconds *float64  `json:"duration_ms,omitempty"`
	ToolUses             []ToolUse `json:"tool_uses"`
	EventFailure         *Failure  `json:"event_failure,omitempty"`
}

// Lists retain the store's newest-first order. Owner reads use only BySession;
// unscoped reads use Global. Adapters must perform owner admission before IO.
type Trajectories struct {
	Global        []Recording            `json:"global"`
	BySession     map[string][]Recording `json:"by_session"`
	HasEvents     bool                   `json:"has_events"`
	TrendsFailure *Failure               `json:"trends_failure,omitempty"`
	UsageFailure  *Failure               `json:"usage_failure,omitempty"`
}
type Cron struct {
	Jobs    []string `json:"jobs"`
	Armed   []string `json:"armed"`
	Failure *Failure `json:"failure,omitempty"`
}
type Observations struct {
	Sessions        []Session     `json:"sessions"`
	SessionsFailure *Failure      `json:"sessions_failure,omitempty"`
	Problems        GlobalLedgers `json:"problems"`
	ProblemsFailure *Failure      `json:"problems_failure,omitempty"`
	Trajectories    *Trajectories `json:"trajectories,omitempty"`
	Cron            Cron          `json:"cron"`
}

// Owner narrows session-derived observations; IncludeGlobal separately selects
// fleet ledgers and cron. Authenticated HTTP must pair owner with false.
type Scope struct {
	Owner         *string
	IncludeGlobal bool
}
type Suggestion struct {
	Source    string `json:"source"`
	Problem   string `json:"problem"`
	Objective string `json:"objective"`
}

// NoExpectation has one value: JSON null. It holds no executable predicate.
type NoExpectation struct{}

func (NoExpectation) MarshalJSON() ([]byte, error) { return []byte("null"), nil }
func (*NoExpectation) UnmarshalJSON(data []byte) error {
	if !bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("a benchmark draft cannot contain an expectation")
	}
	return nil
}

type BenchTaskDraft struct {
	Source      string        `json:"source"`
	Problem     string        `json:"problem"`
	Name        string        `json:"name"`
	PromptDraft string        `json:"prompt_draft"`
	Expect      NoExpectation `json:"expect"`
	Note        string        `json:"note"`
}
