package protocol

// Goal revisions are signed so stale negative model references remain readable.
// Stored/live revisions must be positive; the decoder owns overflow rejection.
type GoalRevision int64
type CreateGoalInput struct {
	Objective string `json:"objective"`
	MaxRounds *int   `json:"max_rounds,omitempty"`
}
type GoalReferenceInput struct {
	Revision GoalRevision `json:"revision"`
}
type BlockGoalInput struct {
	Revision GoalRevision `json:"revision"`
	Code     string       `json:"code"`
	Message  string       `json:"message"`
}

func cloneCreateGoal(v CreateGoalInput) CreateGoalInput {
	if v.MaxRounds != nil {
		n := *v.MaxRounds
		v.MaxRounds = &n
	}
	return v
}
func CreateGoalToolInput(v CreateGoalInput) ToolInput {
	return ToolInput{name: ToolGoalCreate, createGoal: cloneCreateGoal(v)}
}
func GoalStatusToolInput() ToolInput { return ToolInput{name: ToolGoalStatus} }
func CompleteGoalToolInput(v GoalReferenceInput) ToolInput {
	return ToolInput{name: ToolGoalComplete, goalRef: v}
}
func ResumeGoalToolInput(v GoalReferenceInput) ToolInput {
	return ToolInput{name: ToolGoalResume, goalRef: v}
}
func BlockGoalToolInput(v BlockGoalInput) ToolInput {
	return ToolInput{name: ToolGoalBlock, blockGoal: v}
}
func (v ToolInput) CreateGoal() (CreateGoalInput, bool) {
	return cloneCreateGoal(v.createGoal), v.name == ToolGoalCreate
}
func (v ToolInput) GoalReference() (GoalReferenceInput, bool) {
	return v.goalRef, v.name == ToolGoalComplete || v.name == ToolGoalResume
}
func (v ToolInput) BlockGoal() (BlockGoalInput, bool) { return v.blockGoal, v.name == ToolGoalBlock }
func GoalSchemas() []ToolSchema {
	create := SchemaProperties{"objective": {Type: SchemaString, Description: "The completion objective."}, "max_rounds": {Type: SchemaInteger, Description: "Continuation round cap."}}
	ref := SchemaProperties{"revision": {Type: SchemaInteger}}
	block := SchemaProperties{"revision": {Type: SchemaInteger}, "code": {Type: SchemaString, Description: "stable lower-kebab-case classification"}, "message": {Type: SchemaString}}
	empty := SchemaProperties{}
	return []ToolSchema{
		{Name: ToolGoalCreate, Description: "Create and arm this session's completion goal.", InputSchema: InputSchema{Type: SchemaObject, Properties: &create, Required: []string{"objective"}}},
		{Name: ToolGoalStatus, Description: "Read the current goal, its phase, revision and round budget.", InputSchema: InputSchema{Type: SchemaObject, Properties: &empty}},
		{Name: ToolGoalComplete, Description: "Mark the current goal complete (CAS by revision).", InputSchema: InputSchema{Type: SchemaObject, Properties: &ref, Required: []string{"revision"}}},
		{Name: ToolGoalBlock, Description: "Mark the goal blocked with a stable kebab-case code and an explanation.", InputSchema: InputSchema{Type: SchemaObject, Properties: &block, Required: []string{"revision", "code", "message"}}},
		{Name: ToolGoalResume, Description: "Resume a paused/blocked goal and re-arm continuation.", InputSchema: InputSchema{Type: SchemaObject, Properties: &ref, Required: []string{"revision"}}},
	}
}
