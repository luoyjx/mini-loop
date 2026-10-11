package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/protocol"
)

const guardianSystem = "You are a security reviewer. You may read to inform your judgment but never modify anything."
const guardianPrompt = "An agent requests approval for a potentially dangerous action. Judge ONLY whether this specific action should proceed. You cannot grant new powers, change the sandbox, or widen permissions -- you answer the same allow/deny a human reviewer would.\n\nTool: %s\nRule that flagged it: %s -- %s\nArguments (already redacted): %s\n\nReply with a single line: ALLOW or DENY, followed by a one-sentence reason. If you cannot judge confidently, reply DEFER and the human will decide.\n"

type GuardianRequest struct {
	Tool                        protocol.ToolName
	Rule, Message, InputPreview string
	SessionID                   SessionID
}

// ReviewAbstain is Source's None: no verdict and no inferred grant.
type GuardianDecision struct {
	Verdict ReviewVerdict
	Reason  string
}

type Guardian interface {
	Review(context.Context, GuardianRequest) (GuardianDecision, error)
}

// GuardianReviewer adapts the readonly judgment to the existing broker seam.
// It drops the reason, like Source; the broker records its own approval event.
type GuardianReviewer struct {
	Guardian Guardian
	Secrets  ApprovalRedactor
}

// The owning turn captures current authority/environment before dispatch. The
// reviewer contains no parent session handle and never reacquires its turn mutex.
type ownedGuardianReviewer struct {
	parent  SubagentParent
	secrets ApprovalRedactor
}

func (reviewer ownedGuardianReviewer) ReviewApproval(ctx context.Context, request ApprovalRequest) (ReviewVerdict, error) {
	if request.Authority.SessionID != reviewer.parent.authority.SessionID || request.Authority.OwnerID != reviewer.parent.authority.OwnerID || request.Authority.Workspace != reviewer.parent.authority.Workspace {
		return ReviewAbstain, errors.New("Guardian parent does not match approval authority")
	}
	guardian, err := NewAgentGuardian(reviewer.parent)
	if err != nil {
		return ReviewAbstain, err
	}
	return (GuardianReviewer{Guardian: guardian, Secrets: reviewer.secrets}).ReviewApproval(ctx, request)
}

func (s *Session) guardianApprover(run RunContext) Approver {
	if !s.guardianEnabled {
		return nil
	}
	surface, ok := s.gate.policy.approver.(*ApprovalSurface)
	if !ok {
		return nil
	}
	bound := *surface
	bound.reviewer = ownedGuardianReviewer{parent: s.subagentParent(run), secrets: surface.redactor}
	return &bound
}

func (reviewer GuardianReviewer) ReviewApproval(ctx context.Context, request ApprovalRequest) (ReviewVerdict, error) {
	if reviewer.Guardian == nil {
		return ReviewAbstain, errors.New("nil guardian reviewer")
	}
	shown := request.Call.Input
	if reviewer.Secrets != nil {
		shown = reviewer.Secrets.MaskApprovalInput(shown)
	}
	preview, err := protocol.PythonJSON(shown, true, false)
	if err != nil {
		return ReviewAbstain, err
	}
	decision, err := reviewer.Guardian.Review(ctx, GuardianRequest{Tool: request.Call.Name(), Rule: request.Rule, Message: request.Message, InputPreview: jsonvalue.TextPrefix(preview, 2000), SessionID: request.Authority.SessionID})
	return decision.Verdict, err
}

// AgentGuardian owns a pinned environment, never a mutable parent transcript.
// Each review constructs a fresh explore-catalogue, readonly auditor. Explicit
// construction does not enable the default manager/launcher approval reviewer.
type AgentGuardian struct{ parent SubagentParent }

func NewAgentGuardian(parent SubagentParent) (*AgentGuardian, error) {
	if parent.catalog == nil || parent.rolePolicy == nil || parent.provider == nil || parent.events == nil {
		return nil, errors.New("guardian requires a bound parent environment")
	}
	if err := parent.authority.RunContext.Validate(); err != nil {
		return nil, err
	}
	return &AgentGuardian{parent: parent}, nil
}

// Guardian snapshots the current environment outside a running parent turn.
// Automatic broker selection must instead capture under that turn's existing
// lock; it must not call this method recursively from a parent tool effect.
func (s *Session) Guardian() (*AgentGuardian, error) {
	run, err := DefaultRunContext()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return NewAgentGuardian(s.subagentParent(run))
}

func (guardian *AgentGuardian) Review(ctx context.Context, request GuardianRequest) (GuardianDecision, error) {
	if guardian == nil {
		return GuardianDecision{}, errors.New("nil guardian")
	}
	child, run, err := newSubagent(ctx, SubagentRequest{Parent: guardian.parent, Role: RoleExplore, RunContext: guardian.parent.authority.RunContext})
	if err != nil {
		return GuardianDecision{}, err
	}
	child.label = guardian.parent.label + ">auditor"
	child.lineage.Role = AgentRole("auditor")
	child.systemBuilder = FixedSystem(guardianSystem)
	answer, err := child.RunWithContext(ctx, fmt.Sprintf(guardianPrompt, request.Tool, request.Rule, request.Message, request.InputPreview), run)
	if err != nil {
		return GuardianDecision{}, err
	}
	return parseGuardianAnswer(answer), nil
}

func parseGuardianAnswer(answer string) GuardianDecision {
	for i := range answer {
		for _, word := range []string{"ALLOW", "DENY"} {
			if !strings.HasPrefix(answer[i:], word) {
				continue
			}
			before, after := rune(0), rune(0)
			if i > 0 {
				before, _ = utf8.DecodeLastRuneInString(answer[:i])
			}
			if end := i + len(word); end < len(answer) {
				after, _ = utf8.DecodeRuneInString(answer[end:])
			}
			if pytext.IsWord(before) || pytext.IsWord(after) {
				continue
			}
			reason := pytext.Strip(answer)
			for index, r := range reason {
				if strings.ContainsRune("\n\r\v\f\x1c\x1d\x1e\x85\u2028\u2029", r) {
					reason = reason[:index]
					break
				}
			}
			verdict := ReviewDeny
			if word == "ALLOW" {
				verdict = ReviewAllow
			}
			return GuardianDecision{Verdict: verdict, Reason: jsonvalue.TextPrefix(reason, 200)}
		}
	}
	return GuardianDecision{Verdict: ReviewAbstain}
}
