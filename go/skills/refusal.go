package skills

import (
	"context"
	"errors"
)

// RefusalError is a completed skill-domain refusal. Runtime adapters render its
// source-compatible text; cancellation and custom backend faults remain errors.
type RefusalError struct{ message string }

func (e *RefusalError) Error() string { return e.message }
func loadRefusal(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return &RefusalError{err.Error()}
}
