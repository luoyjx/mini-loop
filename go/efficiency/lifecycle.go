package efficiency

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"time"
)

type LifecyclePhase string

const (
	InitializePhase LifecyclePhase = "initialize"
	HealthPhase     LifecyclePhase = "health"
	ClosePhase      LifecyclePhase = "close"
)

func (p LifecyclePhase) Validate() error {
	switch p {
	case InitializePhase, HealthPhase, ClosePhase:
		return nil
	}
	return enumError(string(p), "LifecyclePhase")
}

type LifecycleStatus string

const (
	LifecycleOK       LifecycleStatus = "ok"
	LifecycleSkipped  LifecycleStatus = "skipped"
	LifecycleDegraded LifecycleStatus = "degraded"
	LifecycleError    LifecycleStatus = "error"
)

func (s LifecycleStatus) Validate() error {
	switch s {
	case LifecycleOK, LifecycleSkipped, LifecycleDegraded, LifecycleError:
		return nil
	}
	return enumError(string(s), "LifecycleStatus")
}

type LifecycleHealthConfig struct {
	Status LifecycleStatus `json:"status"`
	Detail string          `json:"detail"`
}
type LifecycleHealth struct {
	config LifecycleHealthConfig
	valid  bool
}

func NewLifecycleHealth(c LifecycleHealthConfig) (LifecycleHealth, error) {
	if err := c.Status.Validate(); err != nil {
		return LifecycleHealth{}, err
	}
	if c.Detail != "" && !receiptReason.MatchString(c.Detail) {
		return LifecycleHealth{}, errors.New("health detail must be a bounded machine-readable code")
	}
	return LifecycleHealth{c, true}, nil
}
func HealthyComponent() LifecycleHealth {
	return LifecycleHealth{LifecycleHealthConfig{Status: LifecycleOK}, true}
}
func (h LifecycleHealth) Snapshot() (LifecycleHealthConfig, error) {
	if !h.valid {
		return LifecycleHealthConfig{}, errors.New("health hook returned the wrong result type")
	}
	return h.config, nil
}

type LifecycleRecordConfig struct {
	ComponentID      string          `json:"component_id"`
	ComponentVersion string          `json:"component_version"`
	Stage            Stage           `json:"stage"`
	Phase            LifecyclePhase  `json:"phase"`
	Status           LifecycleStatus `json:"status"`
	Detail           string          `json:"detail"`
	ElapsedMS        float64         `json:"elapsed_ms"`
}
type LifecycleRecord struct {
	config LifecycleRecordConfig
	valid  bool
}

func NewLifecycleRecord(c LifecycleRecordConfig) (LifecycleRecord, error) {
	if !componentID.MatchString(c.ComponentID) {
		return LifecycleRecord{}, errors.New("lifecycle component id is invalid")
	}
	if !componentVersion.MatchString(c.ComponentVersion) {
		return LifecycleRecord{}, errors.New("lifecycle component version is invalid")
	}
	if err := c.Stage.Validate(); err != nil {
		return LifecycleRecord{}, err
	}
	if err := c.Phase.Validate(); err != nil {
		return LifecycleRecord{}, err
	}
	if err := c.Status.Validate(); err != nil {
		return LifecycleRecord{}, err
	}
	if c.Detail != "" && !receiptReason.MatchString(c.Detail) {
		return LifecycleRecord{}, errors.New("lifecycle detail must be a bounded machine-readable code")
	}
	if c.ElapsedMS < 0 || math.IsNaN(c.ElapsedMS) || math.IsInf(c.ElapsedMS, 0) {
		return LifecycleRecord{}, errors.New("elapsed_ms must be finite and non-negative")
	}
	return LifecycleRecord{c, true}, nil
}
func (r LifecycleRecord) Snapshot() (LifecycleRecordConfig, error) {
	if !r.valid {
		return LifecycleRecordConfig{}, errors.New("uninitialized lifecycle record")
	}
	return r.config, nil
}

type LifecycleReport struct {
	phase   LifecyclePhase
	status  LifecycleStatus
	records []LifecycleRecord
	valid   bool
}
type LifecycleReportProjection struct {
	Phase      LifecyclePhase          `json:"phase"`
	Status     LifecycleStatus         `json:"status"`
	Healthy    bool                    `json:"healthy"`
	Components []LifecycleRecordConfig `json:"components"`
}

func NewLifecycleReport(phase LifecyclePhase, status LifecycleStatus, records []LifecycleRecord) (LifecycleReport, error) {
	if err := phase.Validate(); err != nil {
		return LifecycleReport{}, err
	}
	if err := status.Validate(); err != nil {
		return LifecycleReport{}, err
	}
	for _, r := range records {
		if _, err := r.Snapshot(); err != nil {
			return LifecycleReport{}, err
		}
	}
	return LifecycleReport{phase, status, append([]LifecycleRecord{}, records...), true}, nil
}
func (r LifecycleReport) Healthy() bool {
	return r.valid && (r.status == LifecycleOK || r.status == LifecycleSkipped)
}
func (r LifecycleReport) Projection() (LifecycleReportProjection, error) {
	if !r.valid {
		return LifecycleReportProjection{}, errors.New("uninitialized lifecycle report")
	}
	out := LifecycleReportProjection{r.phase, r.status, r.Healthy(), make([]LifecycleRecordConfig, 0, len(r.records))}
	for _, record := range r.records {
		out.Components = append(out.Components, record.config)
	}
	return out, nil
}
func (r LifecycleReport) MarshalJSON() ([]byte, error) {
	out, err := r.Projection()
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

// LifecycleServices is an explicit trusted service seam. Raw storage remains
// caller-owned and is never implicitly closed by component teardown.
type LifecycleServices struct{ RawStore *RawStore }
type LifecycleCloseOptions struct{ DeadlineSeconds *float64 }
type ComponentInitializer interface {
	Initialize(context.Context, LifecycleServices) error
}
type ComponentHealthProvider interface {
	Health() (LifecycleHealth, error)
}
type ComponentCloser interface {
	Close(context.Context, LifecycleCloseOptions) error
}

type lifecycleState struct {
	mu                                    sync.Mutex
	serial                                chan struct{}
	drained                               chan struct{}
	active                                int
	initializing, health, closing, closed bool
	initialized, closedReport             *LifecycleReport
}

func newLifecycleState() *lifecycleState {
	drained := make(chan struct{})
	close(drained)
	return &lifecycleState{serial: make(chan struct{}, 1), drained: drained}
}
func (s *lifecycleState) enter(health bool) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing || s.closed {
		return "runtime_closed"
	}
	if s.initializing || s.health || (health && s.active > 0) {
		return "runtime_busy"
	}
	if s.active == 0 {
		s.drained = make(chan struct{})
	}
	s.active++
	s.health = health
	return ""
}
func (s *lifecycleState) leave(health bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active <= 0 || (health && (!s.health || s.active != 1)) {
		panic("unbalanced token-efficiency runtime call gate")
	}
	s.active--
	if health {
		s.health = false
	}
	if s.active == 0 {
		close(s.drained)
	}
}
func acquireLifecycle(ctx context.Context, serial chan struct{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case serial <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func lifecycleReport(phase LifecyclePhase, status LifecycleStatus, records []LifecycleRecord) LifecycleReport {
	return LifecycleReport{phase, status, append([]LifecycleRecord{}, records...), true}
}
func aggregateLifecycle(phase LifecyclePhase, records []LifecycleRecord) LifecycleReport {
	status := LifecycleOK
	allSkipped := len(records) > 0
	for _, r := range records {
		if r.config.Status != LifecycleSkipped {
			allSkipped = false
		}
		if r.config.Status == LifecycleError {
			status = LifecycleError
		} else if r.config.Status == LifecycleDegraded && status != LifecycleError {
			status = LifecycleDegraded
		}
	}
	if allSkipped {
		status = LifecycleSkipped
	}
	return lifecycleReport(phase, status, records)
}
func lifecycleRecord(b *observationBinding, phase LifecyclePhase, status LifecycleStatus, detail string, elapsed time.Duration) LifecycleRecord {
	return LifecycleRecord{LifecycleRecordConfig{b.descriptor.ID, b.descriptor.Version, b.descriptor.Stage, phase, status, detail, float64(elapsed) / float64(time.Millisecond)}, true}
}
func (p *ObservationPipeline) lifecycleBindings() []*observationBinding {
	out := make([]*observationBinding, 0, len(p.bindings))
	for _, b := range p.bindings {
		if b.mode != Off {
			out = append(out, b)
		}
	}
	return out
}

// Trusted hooks are cooperative: no detached worker may continue after teardown.
func callLifecycleHook(ctx context.Context, timeout time.Duration, hook func(context.Context) error) (err error) {
	defer func() {
		if recover() != nil {
			err = &ComponentFault{Class: "RuntimeError", Cause: errors.New("component panic")}
		}
	}()
	child, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err = hook(child)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) && errors.Is(child.Err(), context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return err
}
func cancellation(err error, ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
func (p *ObservationPipeline) Initialize(ctx context.Context, services LifecycleServices) (LifecycleReport, error) {
	s := p.lifecycle
	if err := acquireLifecycle(ctx, s.serial); err != nil {
		return LifecycleReport{}, err
	}
	defer func() { <-s.serial }()
	s.mu.Lock()
	if s.initialized != nil {
		r := *s.initialized
		s.mu.Unlock()
		return r, nil
	}
	if s.closing || s.closed {
		s.mu.Unlock()
		return lifecycleReport(InitializePhase, LifecycleSkipped, nil), nil
	}
	s.initializing = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.initializing = false; s.mu.Unlock() }()
	records := []LifecycleRecord{}
	for _, b := range p.lifecycleBindings() {
		hook, ok := b.reducer.(ComponentInitializer)
		if !ok {
			records = append(records, lifecycleRecord(b, InitializePhase, LifecycleSkipped, "hook_not_implemented", 0))
			continue
		}
		start := time.Now()
		if err := acquireLifecycle(ctx, b.serial); err != nil {
			return LifecycleReport{}, err
		}
		err := callLifecycleHook(ctx, invocationDuration(b.descriptor.TimeoutMS), func(child context.Context) error { return hook.Initialize(child, services) })
		<-b.serial
		if escaped := cancellation(err, ctx); escaped != nil {
			return LifecycleReport{}, escaped
		}
		status, detail := LifecycleOK, "initialized"
		if err != nil {
			status, detail = LifecycleError, componentReason(err)
		}
		records = append(records, lifecycleRecord(b, InitializePhase, status, detail, time.Since(start)))
	}
	report := aggregateLifecycle(InitializePhase, records)
	s.mu.Lock()
	s.initialized = &report
	s.mu.Unlock()
	return report, nil
}
func callHealth(hook ComponentHealthProvider) (health LifecycleHealth, err error) {
	defer func() {
		if recover() != nil {
			err = &ComponentFault{Class: "RuntimeError", Cause: errors.New("component panic")}
		}
	}()
	return hook.Health()
}
func (p *ObservationPipeline) Health() LifecycleReport {
	s := p.lifecycle
	if s.enter(true) != "" {
		return lifecycleReport(HealthPhase, LifecycleSkipped, nil)
	}
	defer s.leave(true)
	records := []LifecycleRecord{}
	for _, b := range p.lifecycleBindings() {
		hook, ok := b.reducer.(ComponentHealthProvider)
		if !ok {
			records = append(records, lifecycleRecord(b, HealthPhase, LifecycleSkipped, "hook_not_implemented", 0))
			continue
		}
		start := time.Now()
		health, err := callHealth(hook)
		status, detail := LifecycleOK, "healthy"
		if err == nil {
			var config LifecycleHealthConfig
			config, err = health.Snapshot()
			if err != nil {
				err = &ComponentFault{Class: "TypeError", Cause: err}
			} else {
				status = config.Status
				if config.Detail != "" {
					detail = config.Detail
				}
			}
		}
		if err != nil {
			status, detail = LifecycleError, componentReason(err)
		}
		records = append(records, lifecycleRecord(b, HealthPhase, status, detail, time.Since(start)))
	}
	return aggregateLifecycle(HealthPhase, records)
}
func (p *ObservationPipeline) closeDeadlineExceeded() LifecycleReport {
	bindings := p.lifecycleBindings()
	records := []LifecycleRecord{}
	for i := len(bindings) - 1; i >= 0; i-- {
		records = append(records, lifecycleRecord(bindings[i], ClosePhase, LifecycleError, "deadline_exceeded", 0))
	}
	return lifecycleReport(ClosePhase, LifecycleError, records)
}
func closeDuration(seconds float64) time.Duration {
	if seconds >= float64(math.MaxInt64)/float64(time.Second) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(seconds * float64(time.Second))
}
func (p *ObservationPipeline) Close(ctx context.Context, options LifecycleCloseOptions) (LifecycleReport, error) {
	// Snapshot the caller's optional deadline before any user code runs.
	var deadline time.Time
	if options.DeadlineSeconds != nil {
		seconds := *options.DeadlineSeconds
		if seconds <= 0 {
			return LifecycleReport{}, errors.New("deadline_seconds must be positive when set")
		}
		if math.IsNaN(seconds) || math.IsInf(seconds, 0) {
			return LifecycleReport{}, errors.New("deadline_seconds must be finite when set")
		}
		deadline = time.Now().Add(closeDuration(seconds))
	}
	s := p.lifecycle
	s.mu.Lock()
	s.closing = true
	drained := s.drained
	s.mu.Unlock()
	wait := ctx
	cancel := func() {}
	if !deadline.IsZero() {
		wait, cancel = context.WithDeadline(ctx, deadline)
	}
	defer cancel()
	select {
	case <-drained:
	case <-wait.Done():
		if ctx.Err() != nil {
			return LifecycleReport{}, ctx.Err()
		}
		return p.closeDeadlineExceeded(), nil
	}
	if err := acquireLifecycle(wait, s.serial); err != nil {
		if ctx.Err() != nil {
			return LifecycleReport{}, ctx.Err()
		}
		return p.closeDeadlineExceeded(), nil
	}
	defer func() { <-s.serial }()
	s.mu.Lock()
	cached := s.closedReport
	s.mu.Unlock()
	if cached != nil {
		return *cached, nil
	}
	if !deadline.IsZero() && time.Until(deadline) <= 0 {
		return p.closeDeadlineExceeded(), nil
	}
	records := []LifecycleRecord{}
	bindings := p.lifecycleBindings()
	for i := len(bindings) - 1; i >= 0; i-- {
		b := bindings[i]
		hook, ok := b.reducer.(ComponentCloser)
		if !ok {
			records = append(records, lifecycleRecord(b, ClosePhase, LifecycleSkipped, "hook_not_implemented", 0))
			continue
		}
		timeout := invocationDuration(b.descriptor.TimeoutMS)
		hookOptions := LifecycleCloseOptions{}
		if !deadline.IsZero() {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				records = append(records, lifecycleRecord(b, ClosePhase, LifecycleError, "deadline_exceeded", 0))
				continue
			}
			if remaining < timeout {
				timeout = remaining
			}
			seconds := remaining.Seconds()
			hookOptions.DeadlineSeconds = &seconds
		}
		start := time.Now()
		err := callLifecycleHook(ctx, timeout, func(child context.Context) error { return hook.Close(child, hookOptions) })
		if escaped := cancellation(err, ctx); escaped != nil {
			return LifecycleReport{}, escaped
		}
		status, detail := LifecycleOK, "closed"
		if err != nil {
			status, detail = LifecycleError, componentReason(err)
		}
		records = append(records, lifecycleRecord(b, ClosePhase, status, detail, time.Since(start)))
	}
	report := aggregateLifecycle(ClosePhase, records)
	s.mu.Lock()
	s.closedReport = &report
	s.closed = true
	s.mu.Unlock()
	return report, nil
}
