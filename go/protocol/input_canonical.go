package protocol

import "fmt"

// CanonicalJSON binds a validated input to Python's sorted, compact UTF-8 JSON.
// Each variant supplies concrete fields in key order; no dynamic JSON is stored.
func (input ToolInput) CanonicalJSON() (string, error) {
	if err := input.Validate(); err != nil {
		return "", err
	}
	switch input.Name() {
	case ToolBash:
		v, _ := input.Bash()
		return PythonJSON(struct {
			ApprovalPrefix  *[]string `json:"approval_prefix,omitempty"`
			Command         string    `json:"command"`
			RunInBackground *bool     `json:"run_in_background,omitempty"`
		}{v.ApprovalPrefix, v.Command, v.RunInBackground}, false, true)
	case ToolReadFile:
		v, _ := input.ReadFile()
		return PythonJSON(struct {
			Limit  *int   `json:"limit,omitempty"`
			Offset *int   `json:"offset,omitempty"`
			Path   string `json:"path"`
		}{v.Limit, v.Offset, v.Path}, false, true)
	case ToolWriteFile:
		v, _ := input.WriteFile()
		return PythonJSON(struct {
			Content string `json:"content"`
			Path    string `json:"path"`
		}{v.Content, v.Path}, false, true)
	case ToolEditFile:
		v, _ := input.EditFile()
		return PythonJSON(struct {
			NewText string `json:"new_text"`
			OldText string `json:"old_text"`
			Path    string `json:"path"`
		}{v.NewText, v.OldText, v.Path}, false, true)
	case ToolTodoWrite:
		v, _ := input.TodoWrite()
		type item struct {
			ActiveForm string     `json:"activeForm"`
			Content    string     `json:"content"`
			Status     TodoStatus `json:"status"`
		}
		items := make([]item, len(v.Items))
		for i, v := range v.Items {
			items[i] = item{v.ActiveForm, v.Content, v.Status}
		}
		return PythonJSON(struct {
			Items []item `json:"items"`
		}{items}, false, true)
	case ToolTask:
		v, _ := input.Task()
		return PythonJSON(struct {
			AgentType *AgentType `json:"agent_type,omitempty"`
			Prompt    string     `json:"prompt"`
		}{v.AgentType, v.Prompt}, false, true)
	case ToolGlob, ToolLoadSkill, ToolCompress, ToolAskUser:
		// These payloads already have sorted field order.
		return PythonJSON(input, false, true)
	default:
		return "", fmt.Errorf("unsupported canonical input %q", input.Name())
	}
}
