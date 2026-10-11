package protocol

// Cron inputs never accept session or owner identity from the model.
type ScheduleCronInput struct {
	Cron      string `json:"cron"`
	Prompt    string `json:"prompt"`
	Recurring *bool  `json:"recurring,omitempty"`
	Durable   *bool  `json:"durable,omitempty"`
}
type CancelCronInput struct {
	JobID string `json:"job_id"`
}

func cloneScheduleCron(v ScheduleCronInput) ScheduleCronInput {
	if v.Recurring != nil {
		b := *v.Recurring
		v.Recurring = &b
	}
	if v.Durable != nil {
		b := *v.Durable
		v.Durable = &b
	}
	return v
}
func ScheduleCronToolInput(v ScheduleCronInput) ToolInput {
	return ToolInput{name: ToolScheduleCron, scheduleCron: cloneScheduleCron(v)}
}
func ListCronsToolInput() ToolInput { return ToolInput{name: ToolListCrons} }
func CancelCronToolInput(v CancelCronInput) ToolInput {
	return ToolInput{name: ToolCancelCron, cancelCron: v}
}
func (v ToolInput) ScheduleCron() (ScheduleCronInput, bool) {
	return cloneScheduleCron(v.scheduleCron), v.name == ToolScheduleCron
}
func (v ToolInput) CancelCron() (CancelCronInput, bool) {
	return v.cancelCron, v.name == ToolCancelCron
}

func CronSchemas() []ToolSchema {
	schedule := SchemaProperties{"cron": {Type: SchemaString, Description: "5-field cron: minute hour day-of-month month day-of-week"}, "prompt": {Type: SchemaString}, "recurring": {Type: SchemaBoolean}, "durable": {Type: SchemaBoolean}}
	cancel := SchemaProperties{"job_id": {Type: SchemaString}}
	empty := SchemaProperties{}
	return []ToolSchema{
		{Name: ToolScheduleCron, Description: "Schedule a prompt to run on a cron schedule (wakes this session).", InputSchema: InputSchema{Type: SchemaObject, Properties: &schedule, Required: []string{"cron", "prompt"}}},
		{Name: ToolListCrons, Description: "List this session's scheduled cron jobs.", InputSchema: InputSchema{Type: SchemaObject, Properties: &empty}},
		{Name: ToolCancelCron, Description: "Cancel a scheduled cron job by id.", InputSchema: InputSchema{Type: SchemaObject, Properties: &cancel, Required: []string{"job_id"}}},
	}
}
