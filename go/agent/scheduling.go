package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type toolAttempt struct {
	use       protocol.ToolUseBlock
	outcome   ToolOutcome
	err       error
	completed bool
}

// Only the turn goroutine writes history and the stuck ledger. Workers publish
// through the synchronized bus and execute through the same effect gate.
func (s *Session) dispatchBatch(ctx context.Context, run RunContext, blocks []protocol.Block) (results []protocol.Block, err error) {
	attempts := make([]toolAttempt, 0)
	for _, block := range blocks {
		if use, ok := block.ToolUse(); ok {
			attempts = append(attempts, toolAttempt{use: use})
		}
	}
	if len(attempts) == 0 {
		return nil, nil
	}
	defer func() {
		if fault := recover(); fault != nil {
			s.repairAttempts(attempts)
			panic(fault)
		}
	}()
	groupStart := 0
	flush := func(end int) error {
		if groupStart == end {
			return nil
		}
		return s.parallelGroup(ctx, run, attempts[groupStart:end])
	}
	for i := range attempts {
		definition, exists := s.gate.catalog.Lookup(attempts[i].use.Input.Name())
		if exists && definition.ExecutionMode(ToolCall{ID: attempts[i].use.ID, Input: attempts[i].use.Input}) == ExecutionParallel {
			continue
		}
		if err = flush(i); err != nil {
			break
		}
		if err = ctx.Err(); err != nil {
			break
		}
		attempts[i].outcome, attempts[i].err = s.dispatchTool(ctx, run, attempts[i].use)
		attempts[i].completed = attempts[i].err == nil
		if err = attempts[i].err; err != nil {
			break
		}
		groupStart = i + 1
	}
	if err == nil {
		err = flush(len(attempts))
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		s.repairAttempts(attempts)
		return nil, err
	}
	results = make([]protocol.Block, 0, len(attempts))
	for _, attempt := range attempts {
		results = append(results, protocol.NewToolResult(attempt.use.ID, attempt.outcome.Output, false))
		if err = s.recordToolStep(attempt.use.Input.Name(), attempt.outcome); err != nil {
			s.repairAttempts(attempts)
			return nil, err
		}
	}
	return results, nil
}

func (s *Session) parallelGroup(ctx context.Context, run RunContext, attempts []toolAttempt) error {
	group, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var workers sync.WaitGroup
	// Joining is mandatory, including cancellation while waiting for capacity.
	defer workers.Wait()
	var admissionErr error
	for i := range attempts {
		lease, err := s.toolLimiter.Acquire(group)
		if err != nil {
			admissionErr = err
			break
		}
		announced := make(chan struct{})
		workers.Add(1)
		go func(attempt *toolAttempt) {
			defer workers.Done()
			defer lease.Release()
			var once sync.Once
			announce := func() { once.Do(func() { close(announced) }) }
			defer announce()
			defer func() {
				if fault := recover(); fault != nil {
					// Do not retain or expose a caller panic payload in runtime state.
					attempt.err = fmt.Errorf("parallel tool handler panicked (%T)", fault)
					cancel(attempt.err)
				}
			}()
			attempt.outcome, attempt.err = s.dispatchToolAnnounced(group, run, attempt.use, announce)
			attempt.completed = attempt.err == nil
			if attempt.err != nil {
				cancel(attempt.err)
			}
		}(&attempts[i])
		// Tool-use telemetry starts in model order, regardless of completion order.
		<-announced
	}
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	if cause := context.Cause(group); cause != nil {
		return cause
	}
	for _, attempt := range attempts {
		if attempt.err != nil {
			return attempt.err
		}
	}
	return admissionErr
}

func (s *Session) repairAttempts(attempts []toolAttempt) {
	results := make([]protocol.Block, 0, len(attempts))
	for _, attempt := range attempts {
		output := unknownToolResult
		if attempt.completed {
			output = attempt.outcome.Output
		} else {
			s.repairedToolUses = append(s.repairedToolUses, attempt.use.ID)
		}
		results = append(results, protocol.NewToolResult(attempt.use.ID, output, false))
	}
	s.appendMessages(protocol.Message{Role: protocol.RoleUser, Content: protocol.BlockContent(results...)})
}
