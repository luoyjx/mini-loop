package protocol

type BackgroundRunInput struct {
	Command        string    `json:"command"`
	Timeout        *int      `json:"timeout,omitempty"`
	ApprovalPrefix *[]string `json:"approval_prefix,omitempty"`
}
type CheckBackgroundInput struct {
	ID *string `json:"bg_id,omitempty"`
}

func cloneBackgroundRun(v BackgroundRunInput) BackgroundRunInput {
	if v.Timeout != nil {
		value := *v.Timeout
		v.Timeout = &value
	}
	if v.ApprovalPrefix != nil {
		values := append([]string{}, (*v.ApprovalPrefix)...)
		v.ApprovalPrefix = &values
	}
	return v
}
func cloneCheckBackground(v CheckBackgroundInput) CheckBackgroundInput {
	if v.ID != nil {
		value := *v.ID
		v.ID = &value
	}
	return v
}
func BackgroundRunToolInput(v BackgroundRunInput) ToolInput {
	return ToolInput{name: ToolBackgroundRun, backgroundRun: cloneBackgroundRun(v)}
}
func CheckBackgroundToolInput(v CheckBackgroundInput) ToolInput {
	return ToolInput{name: ToolCheckBackground, checkBackground: cloneCheckBackground(v)}
}
func (v ToolInput) BackgroundRun() (BackgroundRunInput, bool) {
	return cloneBackgroundRun(v.backgroundRun), v.name == ToolBackgroundRun
}
func (v ToolInput) CheckBackground() (CheckBackgroundInput, bool) {
	return cloneCheckBackground(v.checkBackground), v.name == ToolCheckBackground
}

// ShellCommand and ShellApprovalPrefix expose only the two concrete shell variants.
func (v ToolInput) ShellCommand() (string, bool) {
	switch v.name {
	case ToolBash:
		return v.bash.Command, true
	case ToolBackgroundRun:
		return v.backgroundRun.Command, true
	}
	return "", false
}
func (v ToolInput) ShellApprovalPrefix() *[]string {
	if b, ok := v.Bash(); ok {
		return b.ApprovalPrefix
	}
	if b, ok := v.BackgroundRun(); ok {
		return b.ApprovalPrefix
	}
	return nil
}
func BackgroundSchemas() []ToolSchema {
	props := SchemaProperties{
		"command": {Type: SchemaString}, "timeout": {Type: SchemaInteger},
		"approval_prefix": {Type: SchemaArray, Items: &InputSchema{Type: SchemaString}, Description: "Optional: propose the command prefix the human may remember for the session if this needs approval (see bash)."},
	}
	check := SchemaProperties{"bg_id": {Type: SchemaString}}
	return []ToolSchema{
		{Name: ToolBackgroundRun, Description: "Run a slow shell command in the background; returns a bg_id immediately. Results arrive later as a <task_notification>.", InputSchema: InputSchema{Type: SchemaObject, Properties: &props, Required: []string{"command"}}},
		{Name: ToolCheckBackground, Description: "Check background task status (all, or one bg_id).", InputSchema: InputSchema{Type: SchemaObject, Properties: &check}},
	}
}
