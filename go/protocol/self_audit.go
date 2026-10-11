package protocol

// SelfAuditInput has no model-controlled scope or collection options.
// Visibility must be bound by the trusted runtime when the tool is installed.
type SelfAuditInput struct{}

func SelfAuditToolInput() ToolInput { return ToolInput{name: ToolSelfAudit} }

func (input ToolInput) SelfAudit() (SelfAuditInput, bool) {
	return SelfAuditInput{}, input.name == ToolSelfAudit
}

// SelfAuditSchema describes the optional tool without installing it in the
// default catalogue. Each call returns independent schema storage.
func SelfAuditSchema() ToolSchema {
	empty := SchemaProperties{}
	return ToolSchema{
		Name: ToolSelfAudit,
		Description: "One bounded report of the runtime's own state: per-subsystem " +
			"problem ledgers, session activity, recent trajectory outcomes, " +
			"and scheduled work. Read-only.",
		InputSchema: InputSchema{Type: SchemaObject, Properties: &empty},
	}
}
