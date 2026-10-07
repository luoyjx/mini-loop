package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/luoyjx/mini-loop/go/decisions"
	"io"
)

// MapToolInputStrings creates a recording copy of every default-tool payload.
// The concrete discriminator remains selected; this copy is never executed.
func MapToolInputStrings(input ToolInput, mask func(string) string) ToolInput {
	if mask == nil {
		return input.clone()
	}
	input = input.clone()
	switch input.name {
	case ToolSpawnTeammate:
		input.spawnTeammate.Name = mask(input.spawnTeammate.Name)
		input.spawnTeammate.Role = mask(input.spawnTeammate.Role)
		input.spawnTeammate.Prompt = mask(input.spawnTeammate.Prompt)
	case ToolSendMessage:
		input.sendMessage.To = mask(input.sendMessage.To)
		input.sendMessage.Content = mask(input.sendMessage.Content)
		if input.sendMessage.Type != nil {
			v := mask(*input.sendMessage.Type)
			input.sendMessage.Type = &v
		}
		if input.sendMessage.Metadata != nil {
			v := TeamMetadata{value: input.sendMessage.Metadata.data().MapStrings(mask)}
			input.sendMessage.Metadata = &v
		}
	case ToolBroadcast:
		input.broadcast.Content = mask(input.broadcast.Content)
	case ToolRequestShutdown:
		input.requestShutdown.Target = mask(input.requestShutdown.Target)
		if input.requestShutdown.Reason != nil {
			v := mask(*input.requestShutdown.Reason)
			input.requestShutdown.Reason = &v
		}
	case ToolRequestPlan:
		input.requestPlan.Teammate = mask(input.requestPlan.Teammate)
		input.requestPlan.Task = mask(input.requestPlan.Task)
	case ToolSubmitPlan:
		input.submitPlan.Plan = mask(input.submitPlan.Plan)
	case ToolReviewPlan:
		input.reviewPlan.RequestID = mask(input.reviewPlan.RequestID)
		if input.reviewPlan.Feedback != nil {
			v := mask(*input.reviewPlan.Feedback)
			input.reviewPlan.Feedback = &v
		}
	case ToolRemember:
		input.remember.Name = mask(input.remember.Name)
		input.remember.Content = mask(input.remember.Content)
		if input.remember.Description != nil {
			v := mask(*input.remember.Description)
			input.remember.Description = &v
		}
		if input.remember.Type != nil {
			v := MemoryType(mask(string(*input.remember.Type)))
			input.remember.Type = &v
		}
	case ToolRecall:
		if input.recall.Query != nil {
			v := mask(*input.recall.Query)
			input.recall.Query = &v
		}
	case ToolDecision:
		v := input.decision.Value()
		if input.decisionProjection != nil {
			v = input.decisionProjection.Clone()
		}
		v = v.MapStrings(mask)
		input.decisionProjection = &v
		input.decision = decisions.Request{}
	case ToolGoalCreate:
		input.createGoal.Objective = mask(input.createGoal.Objective)
	case ToolGoalBlock:
		input.blockGoal.Code = mask(input.blockGoal.Code)
		input.blockGoal.Message = mask(input.blockGoal.Message)
	case ToolExitPlanMode:
		input.exitPlanMode.Plan = mask(input.exitPlanMode.Plan)
	case ToolScheduleCron:
		input.scheduleCron.Cron = mask(input.scheduleCron.Cron)
		input.scheduleCron.Prompt = mask(input.scheduleCron.Prompt)
	case ToolCancelCron:
		input.cancelCron.JobID = mask(input.cancelCron.JobID)
	case ToolBackgroundRun:
		input.backgroundRun.Command = mask(input.backgroundRun.Command)
		if input.backgroundRun.ApprovalPrefix != nil {
			for i, v := range *input.backgroundRun.ApprovalPrefix {
				(*input.backgroundRun.ApprovalPrefix)[i] = mask(v)
			}
		}
	case ToolCheckBackground:
		if input.checkBackground.ID != nil {
			v := mask(*input.checkBackground.ID)
			input.checkBackground.ID = &v
		}

	case ToolCreateWorktree:
		input.createWorktree.Name = mask(input.createWorktree.Name)
		if input.createWorktree.TaskID != nil {
			v := mask(*input.createWorktree.TaskID)
			input.createWorktree.TaskID = &v
		}
	case ToolRemoveWorktree:
		input.removeWorktree.Name = mask(input.removeWorktree.Name)
	case ToolKeepWorktree, ToolEnterWorktree:
		input.worktreeName.Name = mask(input.worktreeName.Name)
	case ToolBash:
		input.bash.Command = mask(input.bash.Command)
		if input.bash.ApprovalPrefix != nil {
			for i, value := range *input.bash.ApprovalPrefix {
				(*input.bash.ApprovalPrefix)[i] = mask(value)
			}
		}
	case ToolReadFile:
		input.readFile.Path = mask(input.readFile.Path)
	case ToolWriteFile:
		input.writeFile.Path = mask(input.writeFile.Path)
		input.writeFile.Content = mask(input.writeFile.Content)
	case ToolEditFile:
		input.editFile.Path = mask(input.editFile.Path)
		input.editFile.OldText = mask(input.editFile.OldText)
		input.editFile.NewText = mask(input.editFile.NewText)
	case ToolGlob:
		input.glob.Pattern = mask(input.glob.Pattern)
	case ToolTodoWrite:
		for i := range input.todoWrite.Items {
			item := &input.todoWrite.Items[i]
			item.Content = mask(item.Content)
			item.ActiveForm = mask(item.ActiveForm)
			item.Status = TodoStatus(mask(string(item.Status)))
		}
	case ToolTask:
		input.task.Prompt = mask(input.task.Prompt)
		if input.task.AgentType != nil {
			value := AgentType(mask(string(*input.task.AgentType)))
			input.task.AgentType = &value
		}
	case ToolLoadSkill:
		input.loadSkill.Name = mask(input.loadSkill.Name)
		if input.loadSkill.Scope != nil {
			value := SkillScope(mask(string(*input.loadSkill.Scope)))
			input.loadSkill.Scope = &value
		}
	case ToolCreateTask:
		input.createTask.Subject = mask(input.createTask.Subject)
		if input.createTask.Description != nil {
			v := mask(*input.createTask.Description)
			input.createTask.Description = &v
		}
		if input.createTask.Worktree != nil {
			v := mask(*input.createTask.Worktree)
			input.createTask.Worktree = &v
		}
		if input.createTask.BlockedBy != nil {
			for i, value := range *input.createTask.BlockedBy {
				(*input.createTask.BlockedBy)[i] = mask(value)
			}
		}
	case ToolGetTask, ToolClaimTask, ToolCompleteTask:
		input.taskRef.TaskID = mask(input.taskRef.TaskID)
	case ToolAskUser:
		input.askUser.Question = mask(input.askUser.Question)
	}
	return input
}

const MaxProjectionBytes = 16 * 1024 * 1024
const maxProjectionDepth = 256
const maxRequestDiagnosticProjectionDepth = 1000

type projectionKind uint8

const (
	projectionNull projectionKind = iota
	projectionString
	projectionNumber
	projectionBool
	projectionArray
	projectionObject
)

type projectionMember struct {
	key   string
	value projectionValue
}

// This closed JSON tree exists only while encoding a concrete recording value.
// No raw JSON or open-ended interface enters a domain/service object.
type projectionValue struct {
	kind     projectionKind
	text     string
	number   json.Number
	boolean  bool
	elements []projectionValue
	members  []projectionMember
}

func (value projectionValue) MarshalJSON() ([]byte, error) {
	var out bytes.Buffer
	switch value.kind {
	case projectionNull:
		out.WriteString("null")
	case projectionString:
		encoded, _ := json.Marshal(value.text)
		out.Write(encoded)
	case projectionNumber:
		out.WriteString(string(value.number))
	case projectionBool:
		if value.boolean {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case projectionArray:
		out.WriteByte('[')
		for i, element := range value.elements {
			if i > 0 {
				out.WriteByte(',')
			}
			data, err := element.MarshalJSON()
			if err != nil {
				return nil, err
			}
			out.Write(data)
		}
		out.WriteByte(']')
	case projectionObject:
		out.WriteByte('{')
		for i, member := range value.members {
			if i > 0 {
				out.WriteByte(',')
			}
			key, _ := json.Marshal(member.key)
			out.Write(key)
			out.WriteByte(':')
			data, err := member.value.MarshalJSON()
			if err != nil {
				return nil, err
			}
			out.Write(data)
		}
		out.WriteByte('}')
	default:
		return nil, errors.New("invalid recording projection kind")
	}
	return out.Bytes(), nil
}
func readProjection(decoder *json.Decoder, depth, maxDepth int, mask func(string) string) (projectionValue, error) {
	if depth > maxDepth {
		return projectionValue{}, errors.New("recording projection nesting limit exceeded")
	}
	token, err := decoder.Token()
	if err != nil {
		return projectionValue{}, err
	}
	switch value := token.(type) {
	case nil:
		return projectionValue{kind: projectionNull}, nil
	case string:
		return projectionValue{kind: projectionString, text: mask(value)}, nil
	case json.Number:
		return projectionValue{kind: projectionNumber, number: value}, nil
	case bool:
		return projectionValue{kind: projectionBool, boolean: value}, nil
	case json.Delim:
		result := projectionValue{}
		switch value {
		case '[':
			result.kind = projectionArray
			for decoder.More() {
				child, err := readProjection(decoder, depth+1, maxDepth, mask)
				if err != nil {
					return result, err
				}
				result.elements = append(result.elements, child)
			}
		case '{':
			result.kind = projectionObject
			positions := map[string]int{}
			for decoder.More() {
				token, err := decoder.Token()
				if err != nil {
					return result, err
				}
				key, ok := token.(string)
				if !ok {
					return result, errors.New("recording object key is not text")
				}
				key = mask(key)
				child, err := readProjection(decoder, depth+1, maxDepth, mask)
				if err != nil {
					return result, err
				}
				if index, exists := positions[key]; exists {
					result.members[index].value = child
				} else {
					positions[key] = len(result.members)
					result.members = append(result.members, projectionMember{key, child})
				}
			}
		default:
			return result, errors.New("invalid recording delimiter")
		}
		end, err := decoder.Token()
		if err != nil {
			return result, err
		}
		if (value == '[' && end != json.Delim(']')) || (value == '{' && end != json.Delim('}')) {
			return result, errors.New("invalid recording container end")
		}
		return result, nil
	default:
		return projectionValue{}, errors.New("unsupported recording token")
	}
}

// MaskedPythonJSON masks decoded strings AND keys before final JSON escaping.
// Masking never searches an escaped serialized string. Number spelling and
// member order are retained; colliding masked keys preserve Python's last value.
// T is supplied by a concrete boundary caller, not retained as an interface.
func MaskedPythonJSON[T any](value T, mask func(string) string, ascii, compact bool) (string, error) {
	return maskedPythonJSON(value, mask, ascii, compact, maxProjectionDepth)
}

// MaskedRequestDiagnosticJSON is reserved for bounded HTTP validation responses.
// Their pinned source echo depth exceeds the ordinary recording limit. The byte
// bound and decoded string/key masking remain identical; other sinks keep 256.
func MaskedRequestDiagnosticJSON[T any](value T, mask func(string) string) (string, error) {
	return maskedPythonJSON(value, mask, false, true, maxRequestDiagnosticProjectionDepth)
}

func maskedPythonJSON[T any](value T, mask func(string) string, ascii, compact bool, maxDepth int) (string, error) {
	if mask == nil {
		return PythonJSON(value, ascii, compact)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if len(data) > MaxProjectionBytes {
		return "", errors.New("recording projection byte limit exceeded")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	projected, err := readProjection(decoder, 0, maxDepth, mask)
	if err != nil {
		return "", err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return "", errors.New("recording projection has trailing content")
	}
	return PythonJSON(projected, ascii, compact)
}
