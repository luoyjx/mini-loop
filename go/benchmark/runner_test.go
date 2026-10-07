package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/shell"
)

type armContract struct {
	Cases []struct {
		Name     string
		Rows     []TaskResult
		Error    *string
		Sessions []struct {
			Owner    string
			Mode     agent.PermissionMode
			Messages []protocol.Message
			Files    []string
			Tools    []protocol.ToolName
		}
	}
}

func armFixture(t *testing.T) armContract {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-benchmark-arms.json")
	if err != nil {
		t.Fatal(err)
	}
	var result armContract
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Cases) != 8 {
		t.Fatal("arm contract inventory drift")
	}
	return result
}

type armProvider struct {
	inner    agent.Provider
	requests []protocol.ModelRequest
}

func (p *armProvider) Complete(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	p.requests = append(p.requests, request.Clone())
	return p.inner.Complete(ctx, request)
}
func armManagerConfig(root string, provider agent.Provider) agent.ManagerConfig {
	return agent.ManagerConfig{WorkspaceRoot: root, Services: agent.ManagerServices{Provider: provider}}
}

type armBashFactory struct{ bindings *[]agent.SessionBinding }

func (f armBashFactory) BashFor(_ context.Context, b agent.SessionBinding) (agent.BashExecutor, error) {
	*f.bindings = append(*f.bindings, b)
	return shell.New(shell.Config{Workspace: b.Workspace})
}
func TestRunArmMatchesActualPythonDefaultFakeArms(t *testing.T) {
	for _, fixture := range armFixture(t).Cases[:3] {
		t.Run(fixture.Name, func(t *testing.T) {
			var tasks []Task
			switch fixture.Name {
			case "visible":
				tasks = DefaultTasks()
			case "heldout":
				tasks = HeldoutTasks()
			case "empty":
				tasks = []Task{}
			}
			provider := &armProvider{inner: (&agent.FakeProvider{}).ObjectView()}
			roots := make(map[string]bool)
			for i := range tasks {
				original := tasks[i].expect
				observation := fixture.Sessions[i]
				tasks[i].expect = func(ctx context.Context, root, final string) (bool, error) {
					if roots[root] {
						t.Error("tasks shared a workspace")
					}
					roots[root] = true
					var files []string
					err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
						if err != nil {
							return err
						}
						if entry.Type().IsRegular() {
							name, _ := filepath.Rel(root, path)
							files = append(files, filepath.ToSlash(name))
						}
						return nil
					})
					if err != nil {
						t.Fatal(err)
					}
					if !slices.Equal(files, observation.Files) {
						t.Errorf("effects: %v want %v", files, observation.Files)
					}
					last := observation.Messages[len(observation.Messages)-1]
					blocks, _ := last.Content.Blocks()
					var source strings.Builder
					for _, block := range blocks {
						if text, ok := block.Text(); ok {
							source.WriteString(text.Text)
						}
					}
					if final != source.String() {
						t.Errorf("final drift: %q want %q", final, source.String())
					}
					return original(ctx, root, final)
				}
			}
			config := armManagerConfig(t.TempDir(), provider)
			config.Defaults.PermissionMode = agent.ModeAuto
			var bindings []agent.SessionBinding
			config.Services.BashFactory = armBashFactory{&bindings}
			rows, err := RunArm(context.Background(), "test", config, tasks)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != len(fixture.Rows) {
				t.Fatal("row count", rows)
			}
			for i, row := range rows {
				if row.DurationMilliseconds == nil || row.DurationMilliseconds.Float64() < 0 {
					t.Fatal("duration missing")
				}
				row.DurationMilliseconds = nil
				if !reflect.DeepEqual(row, fixture.Rows[i]) {
					t.Errorf("row %d: %+v want %+v", i, row, fixture.Rows[i])
					got, _ := json.Marshal(row)
					want, _ := json.Marshal(fixture.Rows[i])
					t.Log(string(got), string(want))
				}
			}
			if len(provider.requests) != 2*len(tasks) {
				t.Fatal("default fake call count", len(provider.requests))
			}
			for i, observation := range fixture.Sessions {
				if string(bindings[i].Owner) != observation.Owner || bindings[i].Mode != observation.Mode {
					t.Fatal("arm ownership/mode drift", bindings[i])
				}
				first := provider.requests[2*i]
				if len(first.Messages) != 1 {
					t.Fatal("task reused history")
				}
				var names []protocol.ToolName
				for _, schema := range first.Tools {
					names = append(names, schema.Name)
				}
				if !slices.Equal(names, observation.Tools) {
					t.Errorf("catalogue drift: %v want %v", names, observation.Tools)
				}
			}
		})
	}
}

type runnerProviderFunc func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error)

type runnerPromptHook struct{ fault error }

func (h runnerPromptHook) RewriteUserPrompt(context.Context, agent.TurnContext, string) (*string, error) {
	return nil, h.fault
}

func (f runnerProviderFunc) Complete(ctx context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
	return f(ctx, r)
}

func TestRunArmFaultBoundariesAndCapturedTasks(t *testing.T) {
	fault := errors.New("run fault")
	judgeCalled := false
	task, _ := NewTask(TaskConfig{Name: "bad-run", Prompt: "run", Expect: func(context.Context, string, string) (bool, error) { judgeCalled = true; return true, nil }})
	config := armManagerConfig(t.TempDir(), runnerProviderFunc(func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
		return protocol.ModelReply{}, fault
	}))
	config.Services.Recovery = agent.DirectRecovery{}
	config.Services.UserPromptHooks = []agent.UserPromptHook{runnerPromptHook{fault}}
	rows, err := RunArm(context.Background(), "test", config, []Task{task})
	if err != nil || len(rows) != 1 || rows[0].Passed || rows[0].Error == nil || !strings.HasPrefix(*rows[0].Error, "run:") || judgeCalled {
		t.Fatal("run fault boundary", rows, err, judgeCalled)
	}
	setupFault := errors.New("setup fault")
	setup, _ := NewTask(TaskConfig{Expect: textJudge(""), Setup: func(context.Context, string) error { return setupFault }})
	rows, err = RunArm(context.Background(), "test", config, []Task{setup})
	var armError *ArmError
	if rows != nil || !errors.Is(err, setupFault) || !errors.As(err, &armError) || armError.Stage != ArmSetup {
		t.Fatal("setup fault scored row", rows, err)
	}
	config.Services.Provider = &agent.FakeProvider{}
	config.Services.Recovery = nil
	config.Services.UserPromptHooks = nil
	judge, _ := NewTask(TaskConfig{Prompt: "judge", Expect: func(context.Context, string, string) (bool, error) { return true, errors.New("judge fault") }})
	rows, err = RunArm(context.Background(), "test", config, []Task{judge})
	if err != nil || rows[0].Passed || rows[0].Error == nil || !strings.HasPrefix(*rows[0].Error, "expect raised:") {
		t.Fatal("judge fault boundary", rows, err)
	}
	panicked, _ := NewTask(TaskConfig{Expect: func(context.Context, string, string) (bool, error) { panic("private value") }})
	rows, err = RunArm(context.Background(), "test", config, []Task{panicked})
	if err != nil || rows[0].Passed || strings.Contains(*rows[0].Error, "private value") {
		t.Fatal("judge panic escaped", rows, err)
	}
	panicked.setup = func(context.Context, string) error { panic("private value") }
	if rows, err = RunArm(context.Background(), "test", config, []Task{panicked}); rows != nil || err == nil || strings.Contains(err.Error(), "private value") {
		t.Fatal("setup panic escaped", rows, err)
	}
	if _, err = RunArm(context.Background(), "test", config, []Task{{}}); !errors.Is(err, ErrInvalidTask) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := filepath.Join(t.TempDir(), "unused")
	if _, err = RunArm(ctx, "test", armManagerConfig(root, &agent.FakeProvider{}), []Task{task}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("cancelled arm allocated root", err)
	}
}

type runnerResponder func(context.Context, protocol.ModelRequest) (agent.FakeGeneration, error)

func (f runnerResponder) Respond(ctx context.Context, r protocol.ModelRequest) (agent.FakeGeneration, error) {
	return f(ctx, r)
}

func TestRunArmMatchesSourceFaultAndRecoveryRecipes(t *testing.T) {
	for _, fixture := range armFixture(t).Cases[3:] {
		t.Run(fixture.Name, func(t *testing.T) {
			var task Task
			config := armManagerConfig(t.TempDir(), agent.NewFakeProvider(agent.FakeProviderConfig{Responder: runnerResponder(func(context.Context, protocol.ModelRequest) (agent.FakeGeneration, error) {
				return agent.FakeGeneration{Content: []protocol.Block{protocol.NewTextBlock("done")}, StopReason: protocol.StopEndTurn}, nil
			})}).ObjectView())
			switch fixture.Name {
			case "judge-fault":
				task, _ = NewTask(TaskConfig{Name: "bad-judge", Prompt: "judge", Expect: func(context.Context, string, string) (bool, error) { return false, errors.New("judge fault") }})
			case "setup-fault":
				task, _ = NewTask(TaskConfig{Name: "bad-setup", Prompt: "setup", Expect: textJudge(""), Setup: func(context.Context, string) error { return errors.New("setup fault") }})
			case "provider-fault-recovered":
				task, _ = NewTask(TaskConfig{Name: "bad-run", Prompt: "run", Expect: textJudge("")})
				// The fixture preserves Python's named exception diagnostic. Native
				// providers supply their own error spelling; no class is inferred.
				config.Services.Provider = runnerProviderFunc(func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
					return protocol.ModelReply{}, errors.New("ValueError: run fault")
				})
			case "run-fault":
				task, _ = NewTask(TaskConfig{Name: "bad-run", Prompt: "run", Expect: textJudge("")})
				config.Services.UserPromptHooks = []agent.UserPromptHook{runnerPromptHook{errors.New("run fault")}}
			case "cancelled-provider":
				task, _ = NewTask(TaskConfig{Name: "cancelled", Prompt: "cancel", Expect: textJudge("")})
				config.Services.Provider = runnerProviderFunc(func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
					return protocol.ModelReply{}, context.Canceled
				})
			}
			rows, err := RunArm(context.Background(), "test", config, []Task{task})
			if (err != nil) != (fixture.Error != nil) {
				t.Fatal("arm abort drift", rows, err, fixture.Error)
			}
			if fixture.Name == "cancelled-provider" && !errors.Is(err, context.Canceled) {
				t.Fatal("provider cancellation scored a row", rows, err)
			}
			if err != nil {
				return
			}
			want := fixture.Rows[0]
			got := rows[0]
			if (got.Error != nil) != (want.Error != nil) {
				t.Fatal("row error drift", got, want)
			}
			// Native fault notes name the stage; Python additionally names its
			// exception class. All observable effect/motion/token fields remain.
			got.Error, want.Error = nil, nil
			got.DurationMilliseconds = nil
			if fixture.Name == "provider-fault-recovered" {
				// Native bounded diagnostics include *errors.errorString before
				// the scripted ValueError text, increasing this transcript's
				// measured cost from Python's 32 to 38. Retain the real cost.
				nativeCost := Integer(38)
				want.ContextTokensEstimate = &nativeCost
			}
			if !reflect.DeepEqual(got, want) {
				a, _ := json.Marshal(got)
				b, _ := json.Marshal(want)
				t.Fatalf("row drift %s / %s", a, b)
			}
		})
	}
}

func TestRunArmCancellationJoinsCallAndSnapshotsWorkload(t *testing.T) {
	entered, exited := make(chan struct{}), make(chan struct{})
	provider := runnerProviderFunc(func(ctx context.Context, _ protocol.ModelRequest) (protocol.ModelReply, error) {
		defer close(exited)
		close(entered)
		<-ctx.Done()
		return protocol.ModelReply{}, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	task, _ := NewTask(TaskConfig{Prompt: "wait", Expect: textJudge("")})
	result := make(chan error, 1)
	config := armManagerConfig(t.TempDir(), provider)
	go func() { _, err := RunArm(ctx, "test", config, []Task{task}); result <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("provider not entered")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled arm not joined")
	}
	select {
	case <-exited:
	default:
		t.Fatal("arm returned with a live call")
	}

	tasks := []Task{{name: "first", prompt: "first", expect: textJudge("Done")}, {name: "second", prompt: "second", expect: textJudge("Done")}}
	var setupTime, judgeTime time.Duration
	tasks[0].setup = func(context.Context, string) error {
		start := time.Now()
		defer func() { setupTime = time.Since(start) }()
		tasks[1] = Task{}
		time.Sleep(20 * time.Millisecond)
		return nil
	}
	original := tasks[0].expect
	tasks[0].expect = func(ctx context.Context, root, final string) (bool, error) {
		start := time.Now()
		defer func() { judgeTime = time.Since(start) }()
		time.Sleep(20 * time.Millisecond)
		return original(ctx, root, final)
	}
	start := time.Now()
	rows, err := RunArm(context.Background(), "test", armManagerConfig(t.TempDir(), (&agent.FakeProvider{}).ObjectView()), tasks)
	elapsed := time.Since(start)
	if err != nil || len(rows) != 2 || rows[1].Task != "second" || !rows[1].Passed {
		t.Fatal("callback mutated captured workload", rows, err)
	}
	measured := time.Duration(rows[0].DurationMilliseconds.Float64() * float64(time.Millisecond))
	if measured+setupTime+judgeTime > elapsed+time.Millisecond {
		t.Fatal("duration includes setup/judge", measured, setupTime, judgeTime, elapsed)
	}
}
