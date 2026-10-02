package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type PermissionMode string

const (
	ModeReadonly    PermissionMode = "readonly"
	ModeInteractive PermissionMode = "interactive"
	ModeAuto        PermissionMode = "auto"
)

func (mode PermissionMode) Valid() bool {
	return mode == ModeReadonly || mode == ModeInteractive || mode == ModeAuto
}

type RuleAction string

const (
	RuleDeny RuleAction = "deny"
	RuleAsk  RuleAction = "ask"
)

type PermissionEventDecision string

const (
	PermissionAllow PermissionEventDecision = "allow"
	PermissionDeny  PermissionEventDecision = "deny"
)

type PermissionEvent struct {
	Decision PermissionEventDecision
	Rule     string
	Tool     protocol.ToolName
	Reason   string
}

type ApprovalRequest struct {
	Authority ToolAuthority
	Call      ToolCall
	Rule      string
	Message   string
}

type Approver interface {
	Approve(context.Context, ApprovalRequest) (bool, error)
}

type RuleMatcher func(ToolAuthority, ToolCall, ToolRisk, bool) bool

type PermissionRule struct {
	name    string
	message string
	action  RuleAction
	matches RuleMatcher
}

func NewPermissionRule(name, message string, action RuleAction, matches RuleMatcher) (PermissionRule, error) {
	if name == "" || message == "" || (action != RuleDeny && action != RuleAsk) || matches == nil {
		return PermissionRule{}, errors.New("permission rule requires name, message, action, and matcher")
	}
	return PermissionRule{name: name, message: message, action: action, matches: matches}, nil
}

type PermissionOutcome struct {
	denied  bool
	message string
	events  []PermissionEvent
}

func (outcome PermissionOutcome) Denied() bool    { return outcome.denied }
func (outcome PermissionOutcome) Message() string { return outcome.message }
func (outcome PermissionOutcome) Events() []PermissionEvent {
	return append([]PermissionEvent(nil), outcome.events...)
}

type PermissionPolicy struct {
	rules        []PermissionRule
	approver     Approver
	denyCommands []string
}

func NewPermissionPolicy(rules []PermissionRule, approver Approver, denyCommands []string) (*PermissionPolicy, error) {
	for i, rule := range rules {
		if rule.name == "" || rule.message == "" || rule.matches == nil || (rule.action != RuleAsk && rule.action != RuleDeny) {
			return nil, fmt.Errorf("permission rule %d is invalid", i)
		}
	}
	return &PermissionPolicy{
		rules: append([]PermissionRule(nil), rules...), approver: approver,
		denyCommands: append([]string(nil), denyCommands...),
	}, nil
}

var destructiveShell = regexp.MustCompile(`(?i)(^|[;&|\n]\s*)(rm\s|git\s+(?:reset\s+--hard|clean\s+-)|chmod\s+(?:-R\s+|777\s)|chown\s+-R)|>\s*/etc/`)

func canonicalPath(path string) (string, error) {
	path = filepath.Clean(path)
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		suffix = append(suffix, filepath.Base(path))
		path = parent
	}
}

func pathEscapesWorkspace(authority ToolAuthority, call ToolCall) bool {
	var path string
	switch call.Name() {
	case protocol.ToolWriteFile:
		input, _ := call.Input.WriteFile()
		path = input.Path
	case protocol.ToolEditFile:
		input, _ := call.Input.EditFile()
		path = input.Path
	default:
		return false
	}
	if authority.Workspace == "" || path == "" {
		return true
	}
	root, err := filepath.Abs(authority.Workspace)
	if err != nil {
		return true
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return true
	}
	target := path
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	target, err = canonicalPath(target)
	if err != nil {
		return true
	}
	relative, err := filepath.Rel(root, target)
	return err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func DefaultPermissionPolicy(approver Approver) *PermissionPolicy {
	rules := []PermissionRule{
		{name: "workspace-boundary", message: "Path escapes the workspace", action: RuleDeny,
			matches: func(authority ToolAuthority, call ToolCall, _ ToolRisk, _ bool) bool {
				return pathEscapesWorkspace(authority, call)
			}},
		{name: "destructive-shell", message: "Potentially destructive shell command", action: RuleAsk,
			matches: func(_ ToolAuthority, call ToolCall, _ ToolRisk, _ bool) bool {
				if call.Name() != protocol.ToolBash {
					return false
				}
				input, _ := call.Input.Bash()
				return destructiveShell.MatchString(input.Command)
			}},
		{name: "external-action", message: "Tool acts outside this machine", action: RuleAsk,
			matches: func(_ ToolAuthority, _ ToolCall, risk ToolRisk, exists bool) bool {
				return exists && risk == RiskExternal
			}},
		{name: "unclassified-tool", message: "Tool declares no risk level; treated as external until classified", action: RuleAsk,
			matches: func(_ ToolAuthority, _ ToolCall, risk ToolRisk, exists bool) bool {
				return exists && risk == RiskUnclassified
			}},
	}
	policy, _ := NewPermissionPolicy(rules, approver, []string{
		"rm -rf /", "sudo", "shutdown", "reboot", "> /dev/", ":(){", "mkfs", "dd if=",
	})
	return policy
}

func (policy *PermissionPolicy) Evaluate(ctx context.Context, authority ToolAuthority, call ToolCall, risk ToolRisk, exists bool) (PermissionOutcome, error) {
	if err := authority.Validate(); err != nil {
		return PermissionOutcome{}, err
	}
	var outcome PermissionOutcome
	if call.Name() == protocol.ToolBash {
		input, _ := call.Input.Bash()
		for _, pattern := range policy.denyCommands {
			if strings.Contains(input.Command, pattern) {
				outcome.denied = true
				outcome.message = fmt.Sprintf("Permission denied: '%s' is blocked", pattern)
				outcome.events = append(outcome.events, PermissionEvent{PermissionDeny, "immutable-deny-list", call.Name(), pattern})
				return outcome, nil
			}
		}
	}
	if authority.Mode == ModeReadonly && exists && risk != RiskRead {
		outcome.denied = true
		outcome.message = fmt.Sprintf("Permission denied: this session is read-only (tool risk: %s)", risk)
		outcome.events = append(outcome.events, PermissionEvent{PermissionDeny, "readonly-mode", call.Name(), string(risk)})
		return outcome, nil
	}
	for _, rule := range policy.rules {
		if !rule.matches(authority, call, risk, exists) {
			continue
		}
		if rule.action == RuleAsk && authority.Mode == ModeAuto {
			outcome.events = append(outcome.events, PermissionEvent{PermissionAllow, rule.name, call.Name(), "auto mode"})
			continue
		}
		allowed := false
		if rule.action == RuleAsk && policy.approver != nil {
			var err error
			allowed, err = policy.approver.Approve(ctx, ApprovalRequest{authority, call, rule.name, rule.message})
			if err != nil {
				return PermissionOutcome{}, err
			}
		}
		if allowed {
			outcome.events = append(outcome.events, PermissionEvent{PermissionAllow, rule.name, call.Name(), rule.message})
			continue
		}
		outcome.denied = true
		outcome.message = "Permission denied: " + rule.message
		if rule.action == RuleAsk && policy.approver == nil {
			outcome.message += " (approval required)"
		}
		outcome.events = append(outcome.events, PermissionEvent{PermissionDeny, rule.name, call.Name(), rule.message})
		return outcome, nil
	}
	return outcome, nil
}
