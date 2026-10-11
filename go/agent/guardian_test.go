package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type guardianSourceProvider struct {
	answer   string
	hostile  bool
	requests []protocol.ModelRequest
}

func TestGuardianConstructionAndCancellationRequireBoundEnvironment(t *testing.T) {
	if _, err := NewAgentGuardian(SubagentParent{}); err == nil {
		t.Fatal("zero parent constructed guardian")
	}
	provider := &guardianSourceProvider{answer: "ALLOW safe"}
	parent, err := NewRuntimeSession(runtimeConfig(t.TempDir(), provider))
	if err != nil {
		t.Fatal(err)
	}
	guardian, err := parent.Guardian()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := guardian.Review(ctx, GuardianRequest{}); !errors.Is(err, context.Canceled) || len(provider.requests) != 0 {
		t.Fatal("cancellation reached model", err)
	}
	var absent *AgentGuardian
	if _, err := absent.Review(context.Background(), GuardianRequest{}); err == nil {
		t.Fatal("nil guardian reviewed")
	}
}

func TestGuardianReviewerBridgeMatchesSourceMaskAndPreview(t *testing.T) {
	var fixture struct {
		Bridges []struct {
			Name, Command, Secret, Answer, Prompt string
			Verdict                               *bool
		}
	}
	data, err := os.ReadFile("../testdata/python-guardian.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Bridges) != 4 {
		t.Fatal("Source bridge inventory changed")
	}
	for _, row := range fixture.Bridges {
		t.Run(row.Name, func(t *testing.T) {
			provider := &guardianSourceProvider{answer: row.Answer}
			parent, err := NewRuntimeSession(runtimeConfig(t.TempDir(), provider))
			if err != nil {
				t.Fatal(err)
			}
			guardian, err := parent.Guardian()
			if err != nil {
				t.Fatal(err)
			}
			reviewer := GuardianReviewer{Guardian: guardian}
			if row.Secret != "" {
				reviewer.Secrets = literalApprovalRedactor{row.Secret}
			}
			input := protocol.BashToolInput(protocol.BashInput{Command: row.Command})
			request := ApprovalRequest{Authority: guardian.parent.authority, Call: ToolCall{ID: "tool", Input: input}, Rule: "danger", Message: "approval required"}
			verdict, err := reviewer.ReviewApproval(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			want := ReviewAbstain
			if row.Verdict != nil {
				want = ReviewDeny
				if *row.Verdict {
					want = ReviewAllow
				}
			}
			if verdict != want {
				t.Fatal(verdict, want)
			}
			prompt, _ := provider.requests[0].Messages[0].Content.Plain()
			if prompt != row.Prompt {
				t.Fatal("masked/bounded review prompt differs", prompt, row.Prompt)
			}
			if row.Verdict != nil {
				broker, err := NewApprovalBroker(ApprovalBrokerConfig{Reviewer: reviewer, Timeout: time.Second})
				if err != nil {
					t.Fatal(err)
				}
				surface, err := broker.ForSession(request.Authority, nil)
				if err != nil {
					t.Fatal(err)
				}
				allowed, err := surface.Approve(context.Background(), request)
				if err != nil || allowed != *row.Verdict || len(broker.List(request.Authority.SessionID)) != 0 {
					t.Fatal("explicit broker review failed", allowed, err)
				}
			}
		})
	}
}

type guardianWidePolicy struct{}

func (guardianWidePolicy) Select(_ AgentRole, parent *ToolCatalog) (*ToolCatalog, error) {
	return parent, nil
}

type guardianRoleProbe struct {
	t        *testing.T
	parent   string
	depth    int
	observed bool
}

func (probe *guardianRoleProbe) Stop(_ context.Context, view StopContext) (*string, error) {
	probe.observed = true
	child := view.session
	if child.label != probe.parent+">auditor" || child.depth != probe.depth+1 || child.lineage.Role != AgentRole("auditor") || view.Authority.Mode != ModeReadonly {
		probe.t.Fatal("readonly auditor lineage lost")
	}
	if view.Authority.RunContext.Authority() != AuthorityPeerAgent {
		probe.t.Fatal("reviewer inherited human authority")
	}
	return nil, nil
}

func (p *guardianSourceProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	p.requests = append(p.requests, request)
	if p.hostile && len(p.requests) == 1 {
		input, err := protocol.DecodeToolInput(protocol.ToolWriteFile, []byte(`{"path":"planted.txt","content":"x"}`))
		if err != nil {
			return protocol.ModelReply{}, err
		}
		return fakeReply([]protocol.Block{protocol.NewToolUse("write", input)}, protocol.StopToolUse), nil
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock(p.answer)}, protocol.StopEndTurn), nil
}

func TestAgentGuardianMatchesSourceReadonlyReview(t *testing.T) {
	var fixture struct {
		Reviews []struct {
			Name, Answer, Prompt string
			Hostile              bool
			Wide                 bool
			Verdict              *struct {
				Allowed bool
				Reason  string
			}
			Names          [][]protocol.ToolName
			System         json.RawMessage
			Results        []string
			ParentMessages int  `json:"parent_messages"`
			FileExists     bool `json:"file_exists"`
		}
	}
	data, err := os.ReadFile("../testdata/python-guardian.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Reviews) != 18 {
		t.Fatal("Source guardian inventory changed")
	}
	for _, row := range fixture.Reviews {
		t.Run(row.Name, func(t *testing.T) {
			root := t.TempDir()
			provider := &guardianSourceProvider{answer: row.Answer, hostile: row.Hostile}
			config := runtimeConfig(root, provider)
			probe := &guardianRoleProbe{t: t, parent: string(config.ID)}
			config.StopHooks = []StopHook{probe}
			if row.Wide {
				config.RoleToolPolicy = guardianWidePolicy{}
			}
			parent, err := NewRuntimeSession(config)
			if err != nil {
				t.Fatal(err)
			}
			guardian, err := parent.Guardian()
			if err != nil {
				t.Fatal(err)
			}
			decision, err := guardian.Review(context.Background(), GuardianRequest{Tool: protocol.ToolBash, Rule: "danger", Message: "approval required", InputPreview: `{"command": "rm fixture"}`, SessionID: "observed"})
			if err != nil {
				t.Fatal(err)
			}
			if row.Verdict == nil {
				if decision.Verdict != ReviewAbstain || decision.Reason != "" {
					t.Fatal(decision)
				}
			} else {
				want := ReviewDeny
				if row.Verdict.Allowed {
					want = ReviewAllow
				}
				if decision.Verdict != want || decision.Reason != row.Verdict.Reason {
					t.Fatal(decision, *row.Verdict)
				}
			}
			if len(parent.Messages()) != row.ParentMessages {
				t.Fatal("guardian modified parent history")
			}
			if !probe.observed {
				t.Fatal("reviewer did not inherit stop hooks")
			}
			_, err = os.Stat(filepath.Join(root, "planted.txt"))
			if row.FileExists || !os.IsNotExist(err) {
				t.Fatal("guardian wrote workspace", err)
			}
			names, results := [][]protocol.ToolName{}, []string{}
			for _, request := range provider.requests {
				catalog := []protocol.ToolName{}
				for _, schema := range request.Tools {
					catalog = append(catalog, schema.Name)
				}
				names = append(names, catalog)
				for _, message := range request.Messages {
					blocks, _ := message.Content.Blocks()
					for _, block := range blocks {
						if result, ok := block.ToolResult(); ok {
							results = append(results, result.Content)
						}
					}
				}
			}
			if !reflect.DeepEqual(names, row.Names) || !reflect.DeepEqual(results, row.Results) {
				t.Fatal(names, results, "want", row.Names, row.Results)
			}
			first := provider.requests[0]
			prompt, _ := first.Messages[0].Content.Plain()
			if prompt != row.Prompt {
				t.Fatal("review prompt differs", prompt)
			}
			wire, err := first.Wire()
			if err != nil {
				t.Fatal(err)
			}
			actual, err := json.Marshal(wire.System)
			if err != nil {
				t.Fatal(err)
			}
			got, err := jsonvalue.Decode(string(actual))
			if err != nil {
				t.Fatal(err)
			}
			want, err := jsonvalue.Decode(string(row.System))
			if err != nil {
				t.Fatal(err)
			}
			if !got.PythonEqual(want) {
				t.Fatal("guardian system differs", string(actual), string(row.System))
			}
		})
	}
}
