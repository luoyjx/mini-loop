package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/workflows"
)

func (s *WorkflowService) execute(id workflows.RunID) (workflows.WorkflowRun, error) {
	run, err := s.store.GetRun(id)
	if err != nil {
		return workflows.WorkflowRun{}, err
	}
	for run.Status == workflows.RunQueued {
		run, err = s.store.TransitionRun(id, run.Version, workflows.RunRunning, nil)
		if err == nil {
			break
		}
		var conflict *workflows.StoreError
		if !errors.As(err, &conflict) || conflict.Kind != workflows.StoreVersionConflict {
			return workflows.WorkflowRun{}, err
		}
		run, err = s.store.GetRun(id)
		if err != nil {
			return workflows.WorkflowRun{}, err
		}
	}
	definition, err := s.store.GetDefinition(run.DefinitionRevision)
	if err != nil {
		return workflows.WorkflowRun{}, err
	}
	info, err := definitionInfo(definition)
	if err != nil {
		return workflows.WorkflowRun{}, err
	}
	if run.Terminal() {
		if run.Status == workflows.RunCancelled {
			s.emitTerminal(context.Background(), run, WorkflowCancelled, workflowEventPayload{reason: workflowReason(run.CancelReason, "requested before execution")})
			if err := s.enqueueTerminal(run); err != nil {
				return workflows.WorkflowRun{}, err
			}
		}
		return run, nil
	}
	s.emit(context.Background(), run, WorkflowStarted, nil, workflowEventPayload{nodeCount: len(info.Nodes), startedAt: run.StartedAt})
	seconds := min(info.Budget.WallTimeSeconds, s.config.Caps.WallTimeSeconds)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds*float64(time.Second)))
	result, err := s.engine.Execute(ctx, id)
	expired := err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded)
	cancel()
	if expired {
		reason := "wall_time budget exceeded"
		if _, err := s.engine.Cancel(id, &reason); err != nil {
			return workflows.WorkflowRun{}, err
		}
		result, err = s.engine.Execute(context.Background(), id)
		if err != nil {
			return workflows.WorkflowRun{}, err
		}
		s.emitTerminal(context.Background(), result, WorkflowCancelled, workflowEventPayload{reason: workflowReason(result.CancelReason, reason)})
		if err := s.enqueueTerminal(result); err != nil {
			return workflows.WorkflowRun{}, err
		}
		return result, nil
	}
	if err != nil {
		detail := workflowErrorDetail(err)
		current, getErr := s.store.GetRun(id)
		if getErr != nil {
			return workflows.WorkflowRun{}, getErr
		}
		if current.Status == workflows.RunCancelling {
			current, getErr = s.engine.Execute(context.Background(), id)
			if getErr != nil {
				return workflows.WorkflowRun{}, getErr
			}
		}
		if current.Status == workflows.RunRunning {
			current, getErr = s.store.FailRun(id, detail)
			if getErr != nil {
				return workflows.WorkflowRun{}, getErr
			}
		}
		if current.Status == workflows.RunFailed {
			s.emitTerminal(context.Background(), current, WorkflowFailed, workflowEventPayload{error: current.Error})
		}
		if current.Status == workflows.RunCancelled {
			s.emitTerminal(context.Background(), current, WorkflowCancelled, workflowEventPayload{reason: workflowReason(current.CancelReason, "cancelled during workflow failure")})
		}
		if current.Terminal() {
			if err := s.enqueueTerminal(current); err != nil {
				return workflows.WorkflowRun{}, err
			}
		}
		return current, nil
	}
	for _, attempt := range s.store.ListAttempts(id) {
		if attempt.VerificationStatus != workflows.NotApplicable {
			s.emit(context.Background(), result, WorkflowVerdictRecorded, &attempt, workflowEventPayload{verification: attempt.VerificationStatus, artifact: attempt.ResultArtifactID, error: attempt.Error})
		}
	}
	switch result.Status {
	case workflows.RunCompleted:
		if result.FinalArtifactID == nil {
			return workflows.WorkflowRun{}, workflowServiceError(WorkflowRuntimeError, "completed workflow has no final artifact")
		}
		artifact, err := s.store.GetArtifact(*result.FinalArtifactID)
		if err != nil {
			return workflows.WorkflowRun{}, err
		}
		s.emitTerminal(context.Background(), result, WorkflowCompleted, workflowEventPayload{artifact: result.FinalArtifactID, hash: artifact.Snapshot().ContentHash})
	case workflows.RunFailed:
		s.emitTerminal(context.Background(), result, WorkflowFailed, workflowEventPayload{error: result.Error})
	case workflows.RunCancelled:
		s.emitTerminal(context.Background(), result, WorkflowCancelled, workflowEventPayload{reason: workflowReason(result.CancelReason, "requested")})
	default:
		return result, nil
	}
	if err := s.enqueueTerminal(result); err != nil {
		return workflows.WorkflowRun{}, err
	}
	return result, nil
}

func (s *WorkflowService) runAttempt(ctx context.Context, execution workflows.AttemptExecution) (*workflows.ArtifactSubmission, error) {
	attempt := execution.Attempt
	run, err := s.store.GetRun(attempt.RunID)
	if err != nil {
		return nil, err
	}
	parent, present := s.config.ResolveParent(SessionID(run.SessionID))
	if !present {
		return nil, &workflows.RunnerError{Kind: workflows.RunnerRuntimeError, Detail: "parent session was deleted"}
	}
	s.mu.Lock()
	live, found := s.live[run.RunID]
	s.mu.Unlock()
	if !found || live.owner != parent.Owner {
		return nil, &workflows.RunnerError{Kind: workflows.RunnerRuntimeError, Detail: "workflow trusted live launch binding is unavailable"}
	}
	definition, err := s.store.GetDefinition(run.DefinitionRevision)
	if err != nil {
		return nil, err
	}
	info, err := definitionInfo(definition)
	if err != nil {
		return nil, err
	}
	s.emit(context.Background(), run, WorkflowNodeClaimed, &attempt, workflowEventPayload{nodeKind: execution.Node.Kind, spawn: attempt.SpawnIndex})
	s.emit(context.Background(), run, WorkflowAgentStarted, &attempt, workflowEventPayload{nodeKind: execution.Node.Kind})
	config := s.config.Worker
	config.Owner, config.Workspace = parent.Owner, parent.Workspace
	rounds := min(info.Budget.MaxRounds, s.config.Caps.MaxRounds)
	config.MaxRounds = &rounds
	config.ResolveContext = func(_ context.Context, item workflows.NodeAttempt) (RunContext, error) {
		if item.RunID != run.RunID {
			return RunContext{}, workflowServiceError(WorkflowPermissionError, "workflow worker attempted a foreign live context lookup")
		}
		return live.context.clone(), nil
	}
	config.EventSink = workflowProgressSink{service: s, run: run, attempt: attempt}
	runner, err := s.config.WorkerFactory(config)
	if err != nil {
		return nil, err
	}
	if err == nil && runner == nil {
		err = workflowServiceError(WorkflowRuntimeError, "workflow worker factory returned no runner")
	}
	var submission *workflows.ArtifactSubmission
	if err == nil {
		submission, err = runner.Run(ctx, execution)
	}
	if err != nil {
		detail := workflowErrorDetail(err)
		if errors.Is(err, context.DeadlineExceeded) {
			detail = "CancelledError: "
		}
		s.emit(context.Background(), run, WorkflowAgentCompleted, &attempt, workflowEventPayload{success: false, error: &detail})
		return nil, err
	}
	s.emit(context.Background(), run, WorkflowAgentCompleted, &attempt, workflowEventPayload{success: true})
	return submission, nil
}

type workflowProgressSink struct {
	service *WorkflowService
	run     workflows.WorkflowRun
	attempt workflows.NodeAttempt
}

func (sink workflowProgressSink) OnEvent(ctx context.Context, record SessionEventRecord) error {
	// Decode only the five named source progress fields; the full event's tool
	// arguments, output and transcript never survive into workflow telemetry.
	data, err := json.Marshal(record)
	if err != nil {
		sink.service.observe(sink.run.RunID, WorkflowAgentProgress, err)
		return nil
	}
	var progress WorkflowProgress
	if err := json.Unmarshal(data, &progress); err != nil {
		sink.service.observe(sink.run.RunID, WorkflowAgentProgress, err)
		return nil
	}
	sink.service.emit(ctx, sink.run, WorkflowAgentProgress, &sink.attempt, workflowEventPayload{progress: progress})
	return nil
}
func (s *WorkflowService) emit(ctx context.Context, run workflows.WorkflowRun, kind WorkflowEventKind, attempt *workflows.NodeAttempt, payload workflowEventPayload) {
	parent, exists := s.config.ResolveParent(SessionID(run.SessionID))
	sink := parent.Events
	if !exists {
		sink = nil
	}
	if s.resolveEvents != nil {
		sink = s.resolveEvents(SessionID(run.SessionID))
	}
	if sink == nil {
		return
	}
	definition, err := s.store.GetDefinition(run.DefinitionRevision)
	if err != nil {
		s.observe(run.RunID, kind, err)
		return
	}
	info, err := definitionInfo(definition)
	if err != nil {
		s.observe(run.RunID, kind, err)
		return
	}
	id, stamp, err := workflowEventIdentity()
	if err != nil {
		s.observe(run.RunID, kind, err)
		return
	}
	event := WorkflowEvent{Kind: kind, EventID: id, OccurredAt: stamp, SessionID: SessionID(run.SessionID), RunID: run.RunID, Name: info.Name, Revision: definition.Revision(), payload: payload}
	if attempt != nil {
		event.NodeID = &attempt.NodeID
		event.AttemptID = &attempt.AttemptID
		if kind == WorkflowAgentStarted || kind == WorkflowAgentProgress || kind == WorkflowAgentCompleted {
			event.AgentID = &attempt.AgentID
			event.ParentAgentID = attempt.ParentAgentID
		}
	}
	if err := sink.EmitWorkflowEvent(ctx, event.Clone()); err != nil {
		s.observe(run.RunID, kind, err)
	}
}
func (s *WorkflowService) emitTerminal(ctx context.Context, run workflows.WorkflowRun, kind WorkflowEventKind, payload workflowEventPayload) {
	s.mu.Lock()
	seen := s.terminalEvents[run.RunID]
	if !seen {
		s.terminalEvents[run.RunID] = true
	}
	s.mu.Unlock()
	if !seen {
		s.emit(ctx, run, kind, nil, payload)
	}
}
func (s *WorkflowService) observe(id workflows.RunID, kind WorkflowEventKind, err error) {
	detail := workflowErrorDetail(err)
	if utf8.RuneCountInString(detail) > 500 {
		detail = string([]rune(detail)[:500])
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observationErrors = append(s.observationErrors, WorkflowObservationError{id, kind, detail})
	if len(s.observationErrors) > 100 {
		s.observationErrors = append([]WorkflowObservationError(nil), s.observationErrors[len(s.observationErrors)-100:]...)
	}
}
func (s *WorkflowService) ObservabilityErrors() []WorkflowObservationError {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]WorkflowObservationError{}, s.observationErrors...)
}
func (s *WorkflowService) enqueueTerminal(run workflows.WorkflowRun) error {
	if !run.Terminal() {
		return workflowServiceError(WorkflowValueError, "outbox notifications require a terminal workflow run")
	}
	payload, err := json.Marshal(struct {
		RunID    workflows.RunID       `json:"run_id"`
		Revision workflows.Revision    `json:"definition_revision"`
		Status   workflows.RunStatus   `json:"status"`
		Artifact *workflows.ArtifactID `json:"artifact_id"`
		Error    *string               `json:"error"`
		Reason   *string               `json:"cancel_reason"`
	}{run.RunID, run.DefinitionRevision, run.Status, run.FinalArtifactID, run.Error, run.CancelReason})
	if err != nil {
		return err
	}
	value, err := jsonvalue.Decode(string(payload))
	if err != nil {
		return err
	}
	message, err := s.store.EnqueueOutbox(workflows.EnqueueOutboxInput{RunID: run.RunID, Kind: workflows.OutboxKind("workflow_" + strings.ToLower(string(run.Status))), Payload: value})
	if err != nil {
		return err
	}
	s.mu.Lock()
	seen := s.resultEvents[run.RunID]
	if !seen {
		s.resultEvents[run.RunID] = true
	}
	s.mu.Unlock()
	if !seen {
		s.emit(context.Background(), run, WorkflowResultEnqueued, nil, workflowEventPayload{message: message.MessageID, status: run.Status, artifact: run.FinalArtifactID})
	}
	return nil
}
