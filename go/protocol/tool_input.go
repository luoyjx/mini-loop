package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

const (
	ToolGoalCreate      ToolName = "goal_create"
	ToolGoalStatus      ToolName = "goal_status"
	ToolGoalComplete    ToolName = "goal_complete"
	ToolGoalBlock       ToolName = "goal_block"
	ToolGoalResume      ToolName = "goal_resume"
	ToolEnterPlanMode   ToolName = "enter_plan_mode"
	ToolExitPlanMode    ToolName = "exit_plan_mode"
	ToolScheduleCron    ToolName = "schedule_cron"
	ToolListCrons       ToolName = "list_crons"
	ToolCancelCron      ToolName = "cancel_cron"
	ToolReadFile        ToolName = "read_file"
	ToolWriteFile       ToolName = "write_file"
	ToolEditFile        ToolName = "edit_file"
	ToolGlob            ToolName = "glob"
	ToolTodoWrite       ToolName = "TodoWrite"
	ToolTask            ToolName = "task"
	ToolLoadSkill       ToolName = "load_skill"
	ToolCompress        ToolName = "compress"
	ToolAskUser         ToolName = "ask_user"
	ToolCreateTask      ToolName = "create_task"
	ToolListTasks       ToolName = "list_tasks"
	ToolGetTask         ToolName = "get_task"
	ToolClaimTask       ToolName = "claim_task"
	ToolCompleteTask    ToolName = "complete_task"
	ToolBackgroundRun   ToolName = "background_run"
	ToolCheckBackground ToolName = "check_background"
	ToolCreateWorktree  ToolName = "create_worktree"
	ToolRemoveWorktree  ToolName = "remove_worktree"
	ToolKeepWorktree    ToolName = "keep_worktree"
	ToolListWorktrees   ToolName = "list_worktrees"
	ToolEnterWorktree   ToolName = "enter_worktree"
)

// DefaultToolNames is the complete Python default_registry inventory at the
// pinned baseline. The contract fixture test fails if Python adds or removes a
// default name without an explicit Go variant.
var defaultToolNames = [...]ToolName{
	ToolBash, ToolReadFile, ToolWriteFile, ToolEditFile, ToolGlob,
	ToolTodoWrite, ToolTask, ToolLoadSkill, ToolCompress, ToolAskUser,
}

func DefaultToolNames() []ToolName {
	return append([]ToolName(nil), defaultToolNames[:]...)
}

type BashInput struct {
	Command         string    `json:"command"`
	RunInBackground *bool     `json:"run_in_background,omitempty"`
	ApprovalPrefix  *[]string `json:"approval_prefix,omitempty"`
}

type ReadFileInput struct {
	Path   string `json:"path"`
	Limit  *int   `json:"limit,omitempty"`
	Offset *int   `json:"offset,omitempty"`
}

type WriteFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type EditFileInput struct {
	Path    string `json:"path"`
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

type GlobInput struct {
	Pattern string `json:"pattern"`
}

type TodoStatus string

const (
	TodoPending    TodoStatus = "pending"
	TodoInProgress TodoStatus = "in_progress"
	TodoCompleted  TodoStatus = "completed"
)

type TodoItem struct {
	Content    string     `json:"content"`
	Status     TodoStatus `json:"status"`
	ActiveForm string     `json:"activeForm"`
}

type TodoWriteInput struct {
	Items []TodoItem `json:"items"`
}

type AgentType string

const (
	AgentExplore        AgentType = "Explore"
	AgentGeneralPurpose AgentType = "general-purpose"
)

type TaskInput struct {
	Prompt    string     `json:"prompt"`
	AgentType *AgentType `json:"agent_type,omitempty"`
}

type SkillScope string

const (
	ScopeAgent SkillScope = "agent"
	ScopeUser  SkillScope = "user"
)

type LoadSkillInput struct {
	Name  string      `json:"name"`
	Scope *SkillScope `json:"scope,omitempty"`
}

type CompressInput struct{}

type AskUserInput struct {
	Question string `json:"question"`
}

// ToolInput is a closed union: the name chooses one concrete payload. The
// unused fields are private and cannot be populated by a runtime caller.
type ToolInput struct {
	createGoal      CreateGoalInput
	goalRef         GoalReferenceInput
	blockGoal       BlockGoalInput
	exitPlanMode    ExitPlanModeInput
	name            ToolName
	nulls           inputNullFields
	bash            BashInput
	readFile        ReadFileInput
	writeFile       WriteFileInput
	editFile        EditFileInput
	glob            GlobInput
	todoWrite       TodoWriteInput
	task            TaskInput
	loadSkill       LoadSkillInput
	compress        CompressInput
	askUser         AskUserInput
	createTask      CreateTaskInput
	taskRef         TaskReferenceInput
	createWorktree  CreateWorktreeInput
	removeWorktree  RemoveWorktreeInput
	backgroundRun   BackgroundRunInput
	checkBackground CheckBackgroundInput
	worktreeName    WorktreeNameInput
	scheduleCron    ScheduleCronInput
	cancelCron      CancelCronInput
}

func BashToolInput(value BashInput) ToolInput {
	return ToolInput{name: ToolBash, bash: cloneBash(value)}
}
func ReadFileToolInput(value ReadFileInput) ToolInput {
	return ToolInput{name: ToolReadFile, readFile: cloneReadFile(value)}
}
func WriteFileToolInput(value WriteFileInput) ToolInput {
	return ToolInput{name: ToolWriteFile, writeFile: value}
}
func EditFileToolInput(value EditFileInput) ToolInput {
	return ToolInput{name: ToolEditFile, editFile: value}
}
func GlobToolInput(value GlobInput) ToolInput { return ToolInput{name: ToolGlob, glob: value} }
func TodoWriteToolInput(value TodoWriteInput) ToolInput {
	return ToolInput{name: ToolTodoWrite, todoWrite: TodoWriteInput{Items: append([]TodoItem{}, value.Items...)}}
}
func TaskToolInput(value TaskInput) ToolInput {
	return ToolInput{name: ToolTask, task: cloneTask(value)}
}
func LoadSkillToolInput(value LoadSkillInput) ToolInput {
	return ToolInput{name: ToolLoadSkill, loadSkill: cloneLoadSkill(value)}
}
func CompressToolInput() ToolInput { return ToolInput{name: ToolCompress} }
func AskUserToolInput(value AskUserInput) ToolInput {
	return ToolInput{name: ToolAskUser, askUser: value}
}

func CreateTaskToolInput(value CreateTaskInput) ToolInput {
	return ToolInput{name: ToolCreateTask, createTask: cloneCreateTask(value)}
}
func ListTasksToolInput() ToolInput { return ToolInput{name: ToolListTasks} }
func GetTaskToolInput(value TaskReferenceInput) ToolInput {
	return ToolInput{name: ToolGetTask, taskRef: value}
}
func ClaimTaskToolInput(value TaskReferenceInput) ToolInput {
	return ToolInput{name: ToolClaimTask, taskRef: value}
}
func CompleteTaskToolInput(value TaskReferenceInput) ToolInput {
	return ToolInput{name: ToolCompleteTask, taskRef: value}
}
func (input ToolInput) CreateTask() (CreateTaskInput, bool) {
	return cloneCreateTask(input.createTask), input.name == ToolCreateTask
}
func (input ToolInput) TaskReference() (TaskReferenceInput, bool) {
	return input.taskRef, input.name == ToolGetTask || input.name == ToolClaimTask || input.name == ToolCompleteTask
}
func cloneCreateTask(value CreateTaskInput) CreateTaskInput {
	if value.Description != nil {
		v := *value.Description
		value.Description = &v
	}
	if value.BlockedBy != nil {
		v := append([]string{}, (*value.BlockedBy)...)
		value.BlockedBy = &v
	}
	if value.Worktree != nil {
		v := *value.Worktree
		value.Worktree = &v
	}
	return value
}

func (input ToolInput) Name() ToolName { return input.name }

func cloneBash(value BashInput) BashInput {
	if value.RunInBackground != nil {
		flag := *value.RunInBackground
		value.RunInBackground = &flag
	}
	if value.ApprovalPrefix != nil {
		prefix := append([]string{}, (*value.ApprovalPrefix)...)
		value.ApprovalPrefix = &prefix
	}
	return value
}

func cloneReadFile(value ReadFileInput) ReadFileInput {
	if value.Limit != nil {
		limit := *value.Limit
		value.Limit = &limit
	}
	if value.Offset != nil {
		offset := *value.Offset
		value.Offset = &offset
	}
	return value
}

func cloneTask(value TaskInput) TaskInput {
	if value.AgentType != nil {
		agentType := *value.AgentType
		value.AgentType = &agentType
	}
	return value
}

func cloneLoadSkill(value LoadSkillInput) LoadSkillInput {
	if value.Scope != nil {
		scope := *value.Scope
		value.Scope = &scope
	}
	return value
}

func (input ToolInput) Bash() (BashInput, bool) {
	if input.name != ToolBash {
		return BashInput{}, false
	}
	return cloneBash(input.bash), true
}

func (input ToolInput) ReadFile() (ReadFileInput, bool) {
	if input.name != ToolReadFile {
		return ReadFileInput{}, false
	}
	return cloneReadFile(input.readFile), true
}

func (input ToolInput) WriteFile() (WriteFileInput, bool) {
	return input.writeFile, input.name == ToolWriteFile
}

func (input ToolInput) EditFile() (EditFileInput, bool) {
	return input.editFile, input.name == ToolEditFile
}

func (input ToolInput) Glob() (GlobInput, bool) { return input.glob, input.name == ToolGlob }

func (input ToolInput) TodoWrite() (TodoWriteInput, bool) {
	if input.name != ToolTodoWrite {
		return TodoWriteInput{}, false
	}
	return TodoWriteInput{Items: append([]TodoItem{}, input.todoWrite.Items...)}, true
}

func (input ToolInput) Task() (TaskInput, bool) {
	if input.name != ToolTask {
		return TaskInput{}, false
	}
	return cloneTask(input.task), true
}

func (input ToolInput) LoadSkill() (LoadSkillInput, bool) {
	if input.name != ToolLoadSkill {
		return LoadSkillInput{}, false
	}
	return cloneLoadSkill(input.loadSkill), true
}

func (input ToolInput) Compress() (CompressInput, bool) {
	return input.compress, input.name == ToolCompress
}

func (input ToolInput) AskUser() (AskUserInput, bool) {
	return input.askUser, input.name == ToolAskUser
}

func (input ToolInput) clone() (result ToolInput) {
	defer func() { result.nulls = input.nulls }()
	switch input.name {
	case ToolGoalCreate:
		return CreateGoalToolInput(input.createGoal)
	case ToolScheduleCron:
		return ScheduleCronToolInput(input.scheduleCron)
	case ToolBackgroundRun:
		return BackgroundRunToolInput(input.backgroundRun)
	case ToolCheckBackground:
		return CheckBackgroundToolInput(input.checkBackground)
	case ToolBash:
		value, _ := input.Bash()
		return BashToolInput(value)
	case ToolReadFile:
		value, _ := input.ReadFile()
		return ReadFileToolInput(value)
	case ToolTodoWrite:
		value, _ := input.TodoWrite()
		return TodoWriteToolInput(value)
	case ToolTask:
		value, _ := input.Task()
		return TaskToolInput(value)
	case ToolCreateTask:
		return CreateTaskToolInput(input.createTask)
	case ToolCreateWorktree:
		return CreateWorktreeToolInput(input.createWorktree)
	case ToolRemoveWorktree:
		return RemoveWorktreeToolInput(input.removeWorktree)
	case ToolLoadSkill:
		value, _ := input.LoadSkill()
		return LoadSkillToolInput(value)
	default:
		return input
	}
}

func (input ToolInput) Validate() error {
	switch input.name {
	case ToolGoalCreate, ToolGoalStatus, ToolGoalComplete, ToolGoalBlock, ToolGoalResume, ToolEnterPlanMode, ToolExitPlanMode, ToolScheduleCron, ToolListCrons, ToolCancelCron, ToolBackgroundRun, ToolCheckBackground, ToolBash, ToolReadFile, ToolWriteFile, ToolEditFile, ToolGlob,
		ToolCompress, ToolAskUser, ToolCreateTask, ToolListTasks, ToolGetTask, ToolClaimTask, ToolCompleteTask,
		ToolCreateWorktree, ToolRemoveWorktree, ToolKeepWorktree, ToolListWorktrees, ToolEnterWorktree:
		return nil
	case ToolTodoWrite:
		for i, item := range input.todoWrite.Items {
			if item.Status != TodoPending && item.Status != TodoInProgress && item.Status != TodoCompleted {
				return fmt.Errorf("items.%d: invalid status %q", i, item.Status)
			}
		}
		return nil
	case ToolTask:
		if input.task.AgentType != nil && *input.task.AgentType != AgentExplore && *input.task.AgentType != AgentGeneralPurpose {
			return fmt.Errorf("invalid agent_type %q", *input.task.AgentType)
		}
		return nil
	case ToolLoadSkill:
		if input.loadSkill.Scope != nil && *input.loadSkill.Scope != ScopeAgent && *input.loadSkill.Scope != ScopeUser {
			return fmt.Errorf("invalid skill scope %q", *input.loadSkill.Scope)
		}
		return nil
	default:
		return fmt.Errorf("unsupported tool %q", input.name)
	}
}

func (input ToolInput) MarshalJSON() ([]byte, error) {
	switch input.name {
	case ToolGoalCreate, ToolBackgroundRun, ToolCheckBackground, ToolBash, ToolReadFile, ToolTask, ToolLoadSkill, ToolCreateTask, ToolCreateWorktree, ToolRemoveWorktree:
		return input.marshalOptionalJSON()
	}
	if err := input.Validate(); err != nil {
		return nil, err
	}
	switch input.name {
	case ToolBash:
		return json.Marshal(input.bash)
	case ToolReadFile:
		return json.Marshal(input.readFile)
	case ToolWriteFile:
		return json.Marshal(input.writeFile)
	case ToolEditFile:
		return json.Marshal(input.editFile)
	case ToolGlob:
		return json.Marshal(input.glob)
	case ToolTodoWrite:
		return json.Marshal(input.todoWrite)
	case ToolTask:
		return json.Marshal(input.task)
	case ToolLoadSkill:
		return json.Marshal(input.loadSkill)
	case ToolCompress:
		return json.Marshal(input.compress)
	case ToolScheduleCron:
		return json.Marshal(input.scheduleCron)
	case ToolGoalComplete, ToolGoalResume:
		return json.Marshal(input.goalRef)
	case ToolGoalBlock:
		return json.Marshal(input.blockGoal)
	case ToolExitPlanMode:
		return json.Marshal(input.exitPlanMode)
	case ToolCancelCron:
		return json.Marshal(input.cancelCron)
	case ToolGoalStatus, ToolEnterPlanMode, ToolListCrons, ToolListTasks, ToolListWorktrees:
		return []byte("{}"), nil
	case ToolKeepWorktree, ToolEnterWorktree:
		return json.Marshal(input.worktreeName)
	case ToolGetTask, ToolClaimTask, ToolCompleteTask:
		return json.Marshal(input.taskRef)
	case ToolAskUser:
		return json.Marshal(input.askUser)
	default:
		return nil, fmt.Errorf("unsupported tool %q", input.name)
	}
}

// A tool input cannot be decoded without its tool name. Block.UnmarshalJSON
// reads the name first and calls DecodeToolInput with the matching payload.
func (input *ToolInput) UnmarshalJSON([]byte) error {
	return errors.New("tool input requires a name; use DecodeToolInput")
}

func decodeToolObject[T any](data []byte, target *T) error {
	if trimmed := bytes.TrimSpace(data); len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("tool input must be a JSON object")
	}
	return decodeStrict(data, target)
}

// DecodeToolInput is the provider boundary. No raw JSON or open-ended map
// survives it; a new default tool must add a concrete case here.
func DecodeToolInput(name ToolName, data []byte) (ToolInput, error) {
	var result ToolInput
	switch name {
	case ToolGoalCreate:
		var wire struct {
			Objective *string `json:"objective"`
			MaxRounds *int    `json:"max_rounds"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.Objective == nil {
			return result, errors.New("goal_create requires objective")
		}
		result = CreateGoalToolInput(CreateGoalInput{Objective: *wire.Objective, MaxRounds: wire.MaxRounds})
	case ToolGoalStatus:
		var wire struct{}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		result = GoalStatusToolInput()
	case ToolGoalComplete, ToolGoalResume:
		var wire struct {
			Revision *GoalRevision `json:"revision"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.Revision == nil {
			return result, errors.New("goal reference requires revision")
		}
		result = ToolInput{name: name, goalRef: GoalReferenceInput{Revision: *wire.Revision}}
	case ToolGoalBlock:
		var wire struct {
			Revision *GoalRevision `json:"revision"`
			Code     *string       `json:"code"`
			Message  *string       `json:"message"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.Revision == nil || wire.Code == nil || wire.Message == nil {
			return result, errors.New("goal_block requires revision, code and message")
		}
		result = BlockGoalToolInput(BlockGoalInput{Revision: *wire.Revision, Code: *wire.Code, Message: *wire.Message})

	case ToolEnterPlanMode:
		var wire struct{}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		result = EnterPlanModeToolInput()
	case ToolExitPlanMode:
		var wire struct {
			Plan *string `json:"plan"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.Plan == nil {
			return result, errors.New("exit_plan_mode requires plan")
		}
		result = ExitPlanModeToolInput(ExitPlanModeInput{Plan: *wire.Plan})
	case ToolScheduleCron:
		var wire struct {
			Cron      *string `json:"cron"`
			Prompt    *string `json:"prompt"`
			Recurring *bool   `json:"recurring"`
			Durable   *bool   `json:"durable"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.Cron == nil || wire.Prompt == nil {
			return result, errors.New("schedule_cron requires cron and prompt")
		}
		result = ScheduleCronToolInput(ScheduleCronInput{*wire.Cron, *wire.Prompt, wire.Recurring, wire.Durable})
	case ToolCancelCron:
		var wire struct {
			JobID *string `json:"job_id"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.JobID == nil {
			return result, errors.New("cancel_cron requires job_id")
		}
		result = CancelCronToolInput(CancelCronInput{*wire.JobID})
	case ToolListCrons:
		var wire struct{}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		result = ListCronsToolInput()
	case ToolBackgroundRun:
		var wire struct {
			Command        *string   `json:"command"`
			Timeout        *int      `json:"timeout"`
			ApprovalPrefix *[]string `json:"approval_prefix"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.Command == nil {
			return result, errors.New("background_run requires command")
		}
		result = BackgroundRunToolInput(BackgroundRunInput{*wire.Command, wire.Timeout, wire.ApprovalPrefix})
	case ToolCheckBackground:
		var wire CheckBackgroundInput
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		result = CheckBackgroundToolInput(wire)

	case ToolCreateWorktree:
		var wire struct {
			Name   *string `json:"name"`
			TaskID *string `json:"task_id"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.Name == nil {
			return result, errors.New("worktree tool requires name")
		}
		result = CreateWorktreeToolInput(CreateWorktreeInput{*wire.Name, wire.TaskID})
	case ToolRemoveWorktree:
		var wire struct {
			Name           *string `json:"name"`
			DiscardChanges *bool   `json:"discard_changes"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.Name == nil {
			return result, errors.New("worktree tool requires name")
		}
		result = RemoveWorktreeToolInput(RemoveWorktreeInput{*wire.Name, wire.DiscardChanges})
	case ToolKeepWorktree, ToolEnterWorktree:
		var wire struct {
			Name *string `json:"name"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.Name == nil {
			return result, errors.New("worktree tool requires name")
		}
		result = ToolInput{name: name, worktreeName: WorktreeNameInput{*wire.Name}}
	case ToolListWorktrees:
		var wire struct{}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		result = ListWorktreesToolInput()
	case ToolBash:
		var wire struct {
			Command         *string   `json:"command"`
			RunInBackground *bool     `json:"run_in_background"`
			ApprovalPrefix  *[]string `json:"approval_prefix"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.Command == nil {
			return result, errors.New("bash input requires command")
		}
		result = BashToolInput(BashInput{*wire.Command, wire.RunInBackground, wire.ApprovalPrefix})
	case ToolReadFile:
		var wire struct {
			Path   *string `json:"path"`
			Limit  *int    `json:"limit"`
			Offset *int    `json:"offset"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.Path == nil {
			return result, errors.New("read_file input requires path")
		}
		result = ReadFileToolInput(ReadFileInput{*wire.Path, wire.Limit, wire.Offset})
	case ToolWriteFile:
		var wire struct {
			Path    *string `json:"path"`
			Content *string `json:"content"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.Path == nil || wire.Content == nil {
			return result, errors.New("write_file input requires path and content")
		}
		result = WriteFileToolInput(WriteFileInput{*wire.Path, *wire.Content})
	case ToolEditFile:
		var wire struct {
			Path    *string `json:"path"`
			OldText *string `json:"old_text"`
			NewText *string `json:"new_text"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.Path == nil || wire.OldText == nil || wire.NewText == nil {
			return result, errors.New("edit_file input requires path, old_text, and new_text")
		}
		result = EditFileToolInput(EditFileInput{*wire.Path, *wire.OldText, *wire.NewText})
	case ToolGlob:
		var wire struct {
			Pattern *string `json:"pattern"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.Pattern == nil {
			return result, errors.New("glob input requires pattern")
		}
		result = GlobToolInput(GlobInput{*wire.Pattern})
	case ToolTodoWrite:
		var wire struct {
			Items *[]json.RawMessage `json:"items"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.Items == nil {
			return result, errors.New("TodoWrite input requires items")
		}
		items := make([]TodoItem, 0, len(*wire.Items))
		for i, raw := range *wire.Items {
			var item struct {
				Content    *string     `json:"content"`
				Status     *TodoStatus `json:"status"`
				ActiveForm *string     `json:"activeForm"`
			}
			if err := decodeToolObject(raw, &item); err != nil {
				return result, fmt.Errorf("items.%d: %w", i, err)
			}
			if item.Content == nil || item.Status == nil || item.ActiveForm == nil {
				return result, fmt.Errorf("items.%d: content, status, activeForm required", i)
			}
			items = append(items, TodoItem{*item.Content, *item.Status, *item.ActiveForm})
		}
		result = TodoWriteToolInput(TodoWriteInput{Items: items})
	case ToolTask:
		var wire struct {
			Prompt    *string    `json:"prompt"`
			AgentType *AgentType `json:"agent_type"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.Prompt == nil {
			return result, errors.New("task input requires prompt")
		}
		result = TaskToolInput(TaskInput{*wire.Prompt, wire.AgentType})
	case ToolLoadSkill:
		var wire struct {
			Name  *string     `json:"name"`
			Scope *SkillScope `json:"scope"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.Name == nil {
			return result, errors.New("load_skill input requires name")
		}
		result = LoadSkillToolInput(LoadSkillInput{*wire.Name, wire.Scope})
	case ToolCompress:
		var wire struct{}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		result = CompressToolInput()
	case ToolCreateTask:
		var wire struct {
			Subject     *string   `json:"subject"`
			Description *string   `json:"description"`
			BlockedBy   *[]string `json:"blockedBy"`
			Worktree    *string   `json:"worktree"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.Subject == nil {
			return result, errors.New("create_task requires subject")
		}
		result = CreateTaskToolInput(CreateTaskInput{*wire.Subject, wire.Description, wire.BlockedBy, wire.Worktree})
	case ToolListTasks:
		var wire struct{}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		result = ListTasksToolInput()
	case ToolGetTask, ToolClaimTask, ToolCompleteTask:
		var wire struct {
			TaskID *string `json:"task_id"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.TaskID == nil {
			return result, errors.New("task tool requires task_id")
		}
		result = ToolInput{name: name, taskRef: TaskReferenceInput{*wire.TaskID}}
	case ToolAskUser:
		var wire struct {
			Question *string `json:"question"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.Question == nil {
			return result, errors.New("ask_user input requires question")
		}
		result = AskUserToolInput(AskUserInput{*wire.Question})
	default:
		return result, fmt.Errorf("unsupported tool %q", name)
	}
	if err := json.Unmarshal(data, &result.nulls); err != nil {
		return ToolInput{}, err
	}
	if result.name == ToolScheduleCron && (result.nulls.CronRecurring || result.nulls.CronDurable) {
		return ToolInput{}, errors.New("cron recurring and durable must be booleans")
	}
	if result.name == ToolCreateTask && result.nulls.TaskDescription {
		return ToolInput{}, errors.New("task description must be a string")
	}
	if err := result.Validate(); err != nil {
		return ToolInput{}, err
	}
	return result, nil
}
