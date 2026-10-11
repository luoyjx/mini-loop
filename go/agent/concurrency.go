package agent

import (
	"context"
	"errors"
	"sync"
)

type ConcurrencyLimit int

const DefaultToolConcurrency ConcurrencyLimit = 8

// ConcurrencyLimiter is explicitly shareable across sessions and children.
// A nil model limiter is unbounded, matching a bare Python Agent.
type ConcurrencyLimiter struct{ slots chan struct{} }

type ConcurrencyLease struct {
	limiter *ConcurrencyLimiter
	once    sync.Once
}

func NewConcurrencyLimiter(limit ConcurrencyLimit) (*ConcurrencyLimiter, error) {
	if limit < 1 {
		return nil, errors.New("concurrency limit must be positive")
	}
	return &ConcurrencyLimiter{slots: make(chan struct{}, int(limit))}, nil
}

func (limiter *ConcurrencyLimiter) Acquire(ctx context.Context) (*ConcurrencyLease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limiter != nil {
		if limiter.slots == nil {
			return nil, errors.New("uninitialized concurrency limiter")
		}
		select {
		case limiter.slots <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if err := ctx.Err(); err != nil {
			<-limiter.slots
			return nil, err
		}
	}
	return &ConcurrencyLease{limiter: limiter}, nil
}

func (lease *ConcurrencyLease) Release() {
	if lease == nil {
		return
	}
	lease.once.Do(func() {
		if lease.limiter != nil {
			<-lease.limiter.slots
		}
	})
}
