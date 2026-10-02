package agent

import (
	"context"
	"errors"
	"sync"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/skills"
	"github.com/luoyjx/mini-loop/go/workspace"
)

type SkillSource interface {
	Descriptions() string
	Load(context.Context, protocol.LoadSkillInput) (string, error)
}

type QuestionRequest struct {
	Authority ToolAuthority
	Question  string
}

type QuestionAnswerKind string

const (
	QuestionAnswered   QuestionAnswerKind = "answered"
	QuestionUnanswered QuestionAnswerKind = "unanswered"
)

// Text is present for an answered question, including an empty answer. A
// decline/timeout is its own variant and cannot become an approval boolean.
type QuestionAnswer struct {
	kind QuestionAnswerKind
	text string
}

func AnswerQuestion(text string) QuestionAnswer        { return QuestionAnswer{QuestionAnswered, text} }
func NoQuestionAnswer() QuestionAnswer                 { return QuestionAnswer{kind: QuestionUnanswered} }
func (answer QuestionAnswer) Kind() QuestionAnswerKind { return answer.kind }
func (answer QuestionAnswer) Text() (string, bool) {
	return answer.text, answer.kind == QuestionAnswered
}

type Questioner interface {
	AskQuestion(context.Context, QuestionRequest) (QuestionAnswer, error)
}

// RuntimeConfig describes explicit dependencies. A nil Skills source is an
// empty catalogue; a nil Questions surface reports the Python bare-Agent
// unavailability notice. This callback is not a durable approval broker.
type RuntimeConfig struct {
	ID        SessionID
	Owner     OwnerID
	Provider  Provider
	Bash      BashExecutor
	Workspace string
	Mode      PermissionMode
	MaxRounds int
	Skills    SkillSource
	Questions Questioner
	Approver  Approver
	Hooks     GateHooks
}

type runtimeHandler struct {
	mu        sync.Mutex
	binding   ToolAuthority
	todos     *TodoManager
	events    *sessionEvents
	skills    SkillSource
	questions Questioner
}

func (handler *runtimeHandler) ExecuteTool(ctx context.Context, authority ToolAuthority, input protocol.ToolInput) (string, error) {
	if authority.SessionID != handler.binding.SessionID || authority.OwnerID != handler.binding.OwnerID {
		return "", errors.New("runtime handler identity does not match bound session")
	}
	root, err := workspace.ResolvePath(authority.Workspace)
	if err != nil || root != handler.binding.Workspace {
		return "", errors.New("runtime handler workspace does not match bound session")
	}
	handler.mu.Lock()
	defer handler.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	switch input.Name() {
	case protocol.ToolTodoWrite:
		value, _ := input.TodoWrite()
		output, err := handler.todos.Update(value.Items)
		if err == nil {
			handler.events.append(SessionEvent{kind: EventTodo, todos: handler.todos.Snapshot()})
		}
		return output, err
	case protocol.ToolLoadSkill:
		value, _ := input.LoadSkill()
		return handler.skills.Load(ctx, value)
	case protocol.ToolAskUser:
		if handler.questions == nil {
			return "[ask_user unavailable on this surface: no approval broker]", nil
		}
		value, _ := input.AskUser()
		answer, err := handler.questions.AskQuestion(ctx, QuestionRequest{authority, value.Question})
		if err != nil {
			return "", err
		}
		switch answer.kind {
		case QuestionAnswered:
			return "The user answered: " + answer.text, nil
		case QuestionUnanswered:
			return "[no answer] The user declined or did not respond in time. Proceed on your best judgment and say what you assumed.", nil
		default:
			return "", errors.New("question surface returned an invalid answer variant")
		}
	default:
		return "", errors.New("unsupported runtime handler")
	}
}

// NewRuntimeSession adds per-session todos, on-demand skills and textual human
// questions to the implemented workspace tools. Task and compress await their
// subagent and context pipelines; neither is advertised as executable yet.
func NewRuntimeSession(config RuntimeConfig) (*Session, error) {
	if config.ID == "" || config.Owner == "" || config.Provider == nil || config.Bash == nil || !config.Mode.Valid() || config.MaxRounds < 1 {
		return nil, errors.New("runtime session requires valid identity, provider, executor, mode and round limit")
	}
	files, err := workspace.NewFiles(config.Workspace)
	if err != nil {
		return nil, err
	}
	base, err := NewWorkspaceToolCatalog(config.Bash, files)
	if err != nil {
		return nil, err
	}
	source := config.Skills
	if source == nil {
		source = skills.EmptyCatalog()
	}
	handler := &runtimeHandler{
		binding: ToolAuthority{config.ID, config.Owner, files.Root(), config.Mode},
		todos:   &TodoManager{}, events: &sessionEvents{}, skills: source, questions: config.Questions,
	}
	definitions := append([]ToolDefinition(nil), base.ordered...)
	for _, name := range []protocol.ToolName{protocol.ToolTodoWrite, protocol.ToolLoadSkill, protocol.ToolAskUser} {
		traits := ToolTraits{Risk: RiskRead, Readonly: true}
		if name == protocol.ToolTodoWrite {
			traits = ToolTraits{Risk: RiskWrite}
		}
		definition, err := NewToolDefinition(name, traits, handler)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, definition)
	}
	catalog, err := NewToolCatalog(definitions...)
	if err != nil {
		return nil, err
	}
	gate, err := NewToolGate(catalog, DefaultPermissionPolicy(config.Approver), config.Hooks)
	if err != nil {
		return nil, err
	}
	session, err := NewSessionWithGate(config.ID, config.Owner, config.Provider, gate, config.Mode, files.Root(), config.MaxRounds)
	if err != nil {
		return nil, err
	}
	session.todos, session.events = handler.todos, handler.events
	return session, nil
}
