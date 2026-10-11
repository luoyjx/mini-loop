package protocol

const ToolDiagnostics ToolName = "diagnostics"

// DiagnosticsInput selects only an optional workspace path, never a provider
// or a workspace root. Omitted and explicit-null paths execute a tree scan.
type DiagnosticsInput struct {
	Path *string `json:"path,omitempty"`
}

func DiagnosticsToolInput(value DiagnosticsInput) ToolInput {
	if value.Path != nil {
		path := *value.Path
		value.Path = &path
	}
	return ToolInput{name: ToolDiagnostics, diagnostics: value}
}

func (input ToolInput) Diagnostics() (DiagnosticsInput, bool) {
	value := DiagnosticsToolInput(input.diagnostics).diagnostics
	return value, input.name == ToolDiagnostics
}

func DiagnosticsSchema() ToolSchema {
	properties := SchemaProperties{"path": {Type: SchemaString, Description: "Optional workspace-relative file; omit to check the tree."}}
	return ToolSchema{Name: ToolDiagnostics,
		Description: "Run language diagnostics over the workspace (or one file). The result names the provider's scope; a clean report covers only that scope.",
		InputSchema: InputSchema{Type: SchemaObject, Properties: &properties},
	}
}
