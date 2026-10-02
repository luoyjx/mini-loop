package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

const (
	ToolReadFile  ToolName = "read_file"
	ToolWriteFile ToolName = "write_file"
	ToolEditFile  ToolName = "edit_file"
	ToolGlob      ToolName = "glob"
	ToolTodoWrite ToolName = "TodoWrite"
	ToolTask      ToolName = "task"
	ToolLoadSkill ToolName = "load_skill"
	ToolCompress  ToolName = "compress"
	ToolAskUser   ToolName = "ask_user"
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
	name      ToolName
	bash      BashInput
	readFile  ReadFileInput
	writeFile WriteFileInput
	editFile  EditFileInput
	glob      GlobInput
	todoWrite TodoWriteInput
	task      TaskInput
	loadSkill LoadSkillInput
	compress  CompressInput
	askUser   AskUserInput
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

func (input ToolInput) clone() ToolInput {
	switch input.name {
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
	case ToolLoadSkill:
		value, _ := input.LoadSkill()
		return LoadSkillToolInput(value)
	default:
		return input
	}
}

func (input ToolInput) Validate() error {
	switch input.name {
	case ToolBash, ToolReadFile, ToolWriteFile, ToolEditFile, ToolGlob,
		ToolCompress, ToolAskUser:
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
	if err := result.Validate(); err != nil {
		return ToolInput{}, err
	}
	return result, nil
}
