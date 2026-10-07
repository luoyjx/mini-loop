package benchmark

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
)

type ArmStage string

const (
	ArmCreate ArmStage = "create"
	ArmSetup  ArmStage = "setup"
	ArmRun    ArmStage = "run"
	ArmJudge  ArmStage = "judge"
)

// ArmError aborts the instrument (construction, setup or cancellation). Ordinary
// run/judge faults score failed rows instead. Cause supports errors.Is/As.
type ArmError struct {
	Stage ArmStage
	Task  string
	Cause error
}

func (err *ArmError) Error() string {
	return fmt.Sprintf("benchmark %s for %q: %v", err.Stage, err.Task, err.Cause)
}
func (err *ArmError) Unwrap() error { return err.Cause }

// RunArm owns and joins one manager, creating a fresh anonymous interactive
// session per admitted task, as Python run_arm does. Configured shared services
// remain caller-owned; a custom workspace factory must supply isolated paths.
// Workspaces are retained for inspection. Tasks are captured before callbacks.
// Nil/empty tasks run no tasks; use DefaultTasks explicitly for the visible set.
func RunArm(ctx context.Context, label string, config agent.ManagerConfig, tasks []Task) (rows []TaskResult, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tasks = append([]Task(nil), tasks...)
	for _, task := range tasks {
		if task.expect == nil {
			return nil, &ArmError{ArmSetup, task.Name(), ErrInvalidTask}
		}
	}
	manager, err := agent.NewSessionManager(config)
	if err != nil {
		return nil, &ArmError{Stage: ArmCreate, Cause: err}
	}
	// Cancellation must not strand manager-owned goroutines or active holders.
	// Python's source runner has no explicit manager stop; joining is a deliberate
	// native lifecycle guarantee, not a timeout that pretends teardown finished.
	defer func() { err = errors.Join(err, manager.Stop(context.Background())) }()
	rows = make([]TaskResult, 0, len(tasks))
	for _, task := range tasks {
		request := agent.CreateSessionRequest{Owner: "anonymous", PermissionMode: agent.ModeInteractive}
		if names, selected := task.ToolNames(); selected {
			request.ToolSelection = agent.SelectTools(names...)
		}
		session, err := manager.Create(ctx, request)
		if err != nil {
			return nil, &ArmError{ArmCreate, task.Name(), err}
		}
		workspace := session.Info().Workspace
		if err := prepareTask(ctx, task, workspace); err != nil {
			return nil, &ArmError{ArmSetup, task.Name(), err}
		}
		started := time.Now()
		final, runErr := session.Run(ctx, task.Prompt())
		duration := Number{kind: floatingNumber, decimal: roundDecimal(float64(time.Since(started))/float64(time.Millisecond), 1)}
		if ctx.Err() != nil {
			return nil, &ArmError{ArmRun, task.Name(), ctx.Err()}
		}
		if errors.Is(runErr, context.Canceled) {
			return nil, &ArmError{ArmRun, task.Name(), runErr}
		}
		row := TaskResult{Arm: label, Task: task.Name()}
		if runErr != nil {
			note := "run: " + runErr.Error()
			row.Error = &note
		} else {
			passed, judgeErr := judgeTask(ctx, task, workspace, final)
			if ctx.Err() != nil {
				return nil, &ArmError{ArmJudge, task.Name(), ctx.Err()}
			}
			if errors.Is(judgeErr, context.Canceled) {
				return nil, &ArmError{ArmJudge, task.Name(), judgeErr}
			}
			row.Passed = passed
			if judgeErr != nil {
				note := "expect raised: " + judgeErr.Error()
				row.Error = &note
			}
		}
		messages := session.Messages()
		behavior := BehavioralMetrics(messages)
		tokens, rounds := Integer(int64(agent.EstimateTokens(messages))), Integer(behavior.Rounds)
		calls, failures, repeats := Integer(behavior.ToolCalls), Integer(behavior.ToolErrors), Integer(behavior.RepeatedReads)
		row.Measurements = Measurements{&duration, &tokens, &rounds, &calls, &failures, &repeats}
		rows = append(rows, row)
	}
	return rows, nil
}

func prepareTask(ctx context.Context, task Task, workspace string) (err error) {
	defer func() {
		if fault := recover(); fault != nil {
			err = fmt.Errorf("setup panicked (%T)", fault)
		}
	}()
	return task.Prepare(ctx, workspace)
}
func judgeTask(ctx context.Context, task Task, workspace, final string) (passed bool, err error) {
	defer func() {
		if fault := recover(); fault != nil {
			passed = false
			err = fmt.Errorf("judge panicked (%T)", fault)
		}
	}()
	return task.Judge(ctx, workspace, final)
}
