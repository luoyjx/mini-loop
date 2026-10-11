package agent

import "github.com/luoyjx/mini-loop/go/protocol"

// ToolSelection is an immutable, optional catalogue reduction. Its zero value
// retains every installed tool; SelectTools() explicitly selects no tools.
// Names cannot enable optional tools, change permission modes, or grant authority.
// Like Python ToolRegistry.subset, unknown names are ignored and registry order
// is retained. This selection belongs to this construction, not stored state.
type ToolSelection struct {
	selected bool
	names    []protocol.ToolName
}

// SelectTools captures a detached whitelist, including an explicitly empty one.
func SelectTools(names ...protocol.ToolName) ToolSelection {
	return ToolSelection{selected: true, names: append([]protocol.ToolName(nil), names...)}
}

func (selection ToolSelection) filter(definitions []ToolDefinition) []ToolDefinition {
	if !selection.selected {
		return definitions
	}
	keep := make(map[protocol.ToolName]bool, len(selection.names))
	for _, name := range selection.names {
		keep[name] = true
	}
	filtered := make([]ToolDefinition, 0, len(definitions))
	for _, definition := range definitions {
		if keep[definition.Name()] {
			filtered = append(filtered, definition)
		}
	}
	return filtered
}
