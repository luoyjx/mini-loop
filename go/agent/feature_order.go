package agent

import (
	"cmp"
	"slices"

	"github.com/luoyjx/mini-loop/go/protocol"
)

// Source installs full_registry groups first, then independently selected
// decisions, workflows and AST tools. Sorting only built-in catalogues preserves
// caller-owned order. Extra tools retain their relative order after these groups.
func orderBuiltinDefinitions(definitions []ToolDefinition) {
	names := protocol.DefaultToolNames()
	for _, group := range [][]protocol.ToolSchema{
		protocol.TaskBoardSchemas(), protocol.BackgroundSchemas(), protocol.MemorySchemas(),
		protocol.CronSchemas(), protocol.TeamSchemas(), protocol.WorktreeSchemas(),
		{protocol.ConnectMCPSchema(nil)}, protocol.PlanModeSchemas(), protocol.GoalSchemas(),
		{protocol.DiagnosticsSchema(), protocol.TranscriptSearchSchema(), protocol.SelfAuditSchema(), protocol.DecisionSchema()},
		protocol.WorkflowToolSchemas(), protocol.ASTToolSchemas(),
	} {
		for _, schema := range group {
			names = append(names, schema.Name)
		}
	}
	ranks := make(map[protocol.ToolName]int, len(names))
	for i, name := range names {
		ranks[name] = i
	}
	rank := func(name protocol.ToolName) int {
		if value, ok := ranks[name]; ok {
			return value
		}
		return len(names)
	}
	slices.SortStableFunc(definitions, func(a, b ToolDefinition) int {
		return cmp.Compare(rank(a.Name()), rank(b.Name()))
	})
}
