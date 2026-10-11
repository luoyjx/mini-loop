package protocol

// Plan text has no model-supplied identity or approval authority.
type ExitPlanModeInput struct {
	Plan string `json:"plan"`
}

func EnterPlanModeToolInput() ToolInput { return ToolInput{name: ToolEnterPlanMode} }
func ExitPlanModeToolInput(v ExitPlanModeInput) ToolInput {
	return ToolInput{name: ToolExitPlanMode, exitPlanMode: v}
}
func (v ToolInput) ExitPlanMode() (ExitPlanModeInput, bool) {
	return v.exitPlanMode, v.name == ToolExitPlanMode
}
func PlanModeSchemas() []ToolSchema {
	empty := SchemaProperties{}
	plan := SchemaProperties{"plan": {Type: SchemaString, Description: "The complete plan, markdown, starting with a # heading."}}
	return []ToolSchema{
		{Name: ToolEnterPlanMode, Description: "Switch to plan mode: investigate and design before mutating anything.", InputSchema: InputSchema{Type: SchemaObject, Properties: &empty}},
		{Name: ToolExitPlanMode, Description: "Present the finished plan for approval and leave plan mode. Only meaningful while plan mode is active.", InputSchema: InputSchema{Type: SchemaObject, Properties: &plan, Required: []string{"plan"}}},
	}
}
