package cron

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const MaxPrompt = 8000
const MaxJobs = 200

type ID string
type SessionID string
type Authority string

const Untrusted Authority = "untrusted"

// Invocation is constructed only by the scheduler. The adapter must turn its
// untrusted authority into a fresh runtime context; restoration never supplies
// human authority. No model-facing arm operation is exposed by this package.
type Invocation struct {
	session SessionID
	prompt  string
}

func (i Invocation) Session() SessionID   { return i.session }
func (i Invocation) Prompt() string       { return i.prompt }
func (i Invocation) Authority() Authority { return Untrusted }

type Runner interface {
	RunScheduled(context.Context, Invocation) error
}
type Resolver interface {
	ResolveScheduled(SessionID) (Runner, error)
}
type RunnerFunc func(context.Context, Invocation) error

func (f RunnerFunc) RunScheduled(ctx context.Context, i Invocation) error { return f(ctx, i) }

type ResolverFunc func(SessionID) (Runner, error)

func (f ResolverFunc) ResolveScheduled(id SessionID) (Runner, error) { return f(id) }

type Masker interface{ Mask(string) string }
type Config struct {
	Resolver    Resolver
	DurablePath string
	Secrets     Masker
}
type Job struct {
	ID        ID        `json:"id"`
	Cron      string    `json:"cron"`
	Prompt    string    `json:"prompt"`
	Session   SessionID `json:"session_id"`
	Recurring bool      `json:"recurring"`
	Durable   bool      `json:"durable"`
	LastFired string    `json:"last_fired"`
}
type Request struct {
	Session            SessionID
	Cron, Prompt       string
	Recurring, Durable *bool
}
type Scheduled struct{ Job Job }

func (s Scheduled) Render() string {
	return fmt.Sprintf("Scheduled cron %s: '%s' -> %s", s.Job.ID, s.Job.Cron, head(s.Job.Prompt, 60))
}

type execution struct {
	cancel context.CancelFunc
	done   chan struct{}
}
type Problem struct {
	Text  string
	Count uint64
}
type ProblemSnapshot struct {
	Entries        []Problem
	Total, Dropped uint64
}

func (s ProblemSnapshot) Summary() []string {
	result := make([]string, 0, len(s.Entries))
	for _, p := range s.Entries {
		value := p.Text
		if p.Count > 1 {
			value += fmt.Sprintf(" (x%d)", p.Count)
		}
		result = append(result, value)
	}
	return result
}

type Scheduler struct {
	mu           sync.Mutex
	resolver     Resolver
	secrets      Masker
	path, claims string
	jobs         []Job
	armed        map[ID]bool
	problems     ProblemSnapshot
	loop         *execution
	running      map[*execution]struct{}
	generation   uint64
	stopping     bool
}

func New(config Config) (*Scheduler, error) {
	if config.Resolver == nil {
		return nil, errors.New("cron requires an explicit session resolver")
	}
	s := &Scheduler{resolver: config.Resolver, secrets: config.Secrets, path: config.DurablePath, armed: make(map[ID]bool), running: make(map[*execution]struct{})}
	if s.path != "" {
		s.claims = claimsPath(s.path)
		s.load()
	}
	return s, nil
}
func head(value string, n int) string {
	r := []rune(value)
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}
func (s *Scheduler) problem(value string) {
	s.problems.Total++
	for n, p := range s.problems.Entries {
		if p.Text == value {
			s.problems.Entries[n].Count++
			return
		}
	}
	if len(s.problems.Entries) == 50 {
		s.problems.Entries = append(s.problems.Entries[:0], s.problems.Entries[1:]...)
		s.problems.Dropped++
	}
	s.problems.Entries = append(s.problems.Entries, Problem{value, 1})
}
func (s *Scheduler) Problems() ProblemSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.problems
	v.Entries = append([]Problem{}, v.Entries...)
	return v
}
func (s *Scheduler) Jobs() []Job      { s.mu.Lock(); defer s.mu.Unlock(); return append([]Job{}, s.jobs...) }
func (s *Scheduler) Armed(id ID) bool { s.mu.Lock(); defer s.mu.Unlock(); return s.armed[id] }
func (s *Scheduler) index(id ID, session *SessionID) int {
	for n, j := range s.jobs {
		if j.ID == id && (session == nil || j.Session == *session) {
			return n
		}
	}
	return -1
}
func (s *Scheduler) Arm(id ID, session *SessionID) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.index(id, session) < 0 {
		return fmt.Sprintf("Error: no such job %s", id)
	}
	s.armed[id] = true
	return fmt.Sprintf("Armed %s", id)
}
func (s *Scheduler) ArmAll() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, j := range s.jobs {
		if !s.armed[j.ID] {
			s.armed[j.ID] = true
			count++
		}
	}
	return count
}
func (s *Scheduler) Schedule(r Request) (Scheduled, error) {
	if _, err := Parse(r.Cron); err != nil {
		return Scheduled{}, fmt.Errorf("Error: %w", err)
	}
	if !utf8.ValidString(r.Prompt) {
		return Scheduled{}, errors.New("Error: prompt must be valid UTF-8")
	}
	length := utf8.RuneCountInString(r.Prompt)
	if length > MaxPrompt {
		return Scheduled{}, fmt.Errorf("Error: prompt is %s characters; the limit is 8,000", comma(length))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.jobs) >= MaxJobs {
		return Scheduled{}, errors.New("Error: 200 scheduled jobs already exist")
	}
	var id ID
	for {
		var b [4]byte
		if _, err := rand.Read(b[:]); err != nil {
			return Scheduled{}, err
		}
		id = ID(hex.EncodeToString(b[:]))
		if s.index(id, nil) < 0 {
			break
		}
	}
	j := Job{ID: id, Cron: r.Cron, Prompt: r.Prompt, Session: r.Session, Recurring: true, Durable: true}
	if r.Recurring != nil {
		j.Recurring = *r.Recurring
	}
	if r.Durable != nil {
		j.Durable = *r.Durable
	}
	s.jobs = append(s.jobs, j)
	s.armed[id] = true
	if j.Durable {
		if err := s.save(); err != nil {
			return Scheduled{j}, err
		}
	}
	// Go has no ambient asyncio loop. Explicit Start owns automatic ticking.
	return Scheduled{j}, nil
}
func comma(n int) string {
	v := fmt.Sprint(n)
	for i := len(v) - 3; i > 0; i -= 3 {
		v = v[:i] + "," + v[i:]
	}
	return v
}
func (s *Scheduler) Cancel(id ID, session *SessionID) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.index(id, session)
	if n < 0 {
		return fmt.Sprintf("No cron %s", id), nil
	}
	j := s.jobs[n]
	s.jobs = append(s.jobs[:n], s.jobs[n+1:]...)
	delete(s.armed, id)
	s.unclaim(id, j.LastFired)
	return fmt.Sprintf("Cancelled cron %s", id), s.save()
}
func (s *Scheduler) CancelForSession(session SessionID) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	persist := false
	kept := make([]Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		if j.Session != session {
			kept = append(kept, j)
			continue
		}
		count++
		persist = persist || j.Durable
		delete(s.armed, j.ID)
		s.unclaim(j.ID, j.LastFired)
	}
	s.jobs = kept
	if persist {
		return count, s.save()
	}
	return count, nil
}
func (s *Scheduler) ListFor(session SessionID) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	lines := []string{}
	for _, j := range s.jobs {
		if j.Session != session {
			continue
		}
		kind := "one-shot"
		if j.Recurring {
			kind = "recurring"
		}
		if j.Durable {
			kind += ", durable"
		}
		if !s.armed[j.ID] {
			kind += ", DISARMED (restored; needs operator arm)"
		}
		lines = append(lines, fmt.Sprintf("%s: '%s' [%s] -> %s", j.ID, j.Cron, kind, head(j.Prompt, 50)))
	}
	if len(lines) == 0 {
		return "No scheduled jobs."
	}
	return strings.Join(lines, "\n")
}

// Tick uses the supplied local civil minute. It never inherits caller authority.
// State is marked and persisted before resolving a target. Resolver callbacks
// run outside the state lock and may inspect the scheduler.
func (s *Scheduler) Tick(now time.Time) {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return
	}
	generation := s.generation
	jobs := append([]Job{}, s.jobs...)
	s.mu.Unlock()
	for _, snapshot := range jobs {
		if j, ok := s.admit(snapshot.ID, now, generation); ok {
			s.fire(j, generation)
		}
	}
}
func (s *Scheduler) admit(id ID, now time.Time, generation uint64) (Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping || s.generation != generation {
		return Job{}, false
	}
	n := s.index(id, nil)
	if n < 0 {
		return Job{}, false
	}
	j := s.jobs[n]
	marker := now.Format("2006-01-02 15:04")
	expr, err := Parse(j.Cron)
	if err != nil || !expr.Matches(now) || j.LastFired == marker {
		return Job{}, false
	}
	if !s.armed[j.ID] {
		s.problem(fmt.Sprintf("%s: restored from disk and not re-armed; skipping until arm() records a new authorization", j.ID))
		return Job{}, false
	}
	if j.Durable {
		won, err := s.claim(j.ID, marker)
		if err != nil {
			s.problem(fmt.Sprintf("%s: firing failed (%v); the occurrence was lost", j.ID, err))
			return Job{}, false
		}
		if !won {
			s.jobs[n].LastFired = marker
			return Job{}, false
		}
	}
	previous := j.LastFired
	j.LastFired = marker
	s.jobs[n] = j
	if !j.Recurring {
		s.jobs = append(s.jobs[:n], s.jobs[n+1:]...)
	}
	if j.Durable {
		if err := s.save(); err != nil {
			s.problem(fmt.Sprintf("%s: firing failed (%v); the occurrence was lost", j.ID, err))
			return Job{}, false
		}
		s.unclaim(j.ID, previous)
	}
	return j, true
}
func resolve(resolver Resolver, id SessionID) (runner Runner, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("resolver panicked (%v)", p)
		}
	}()
	return resolver.ResolveScheduled(id)
}
func (s *Scheduler) fire(j Job, generation uint64) {
	runner, err := resolve(s.resolver, j.Session)
	s.mu.Lock()
	defer s.mu.Unlock()
	if generation != s.generation || s.stopping {
		s.problem(fmt.Sprintf("%s: scheduler stopped before dispatch; the occurrence was lost", j.ID))
		return
	}
	if err != nil {
		s.problem(fmt.Sprintf("%s: firing failed (%v); the occurrence was lost", j.ID, err))
		return
	}
	if runner == nil {
		s.problem(fmt.Sprintf("%s: fired at its scheduled time but session '%s' does not exist; the occurrence was lost", j.ID, j.Session))
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	e := &execution{cancel: cancel, done: make(chan struct{})}
	s.running[e] = struct{}{}
	go func() {
		defer func() { cancel(); s.mu.Lock(); delete(s.running, e); close(e.done); s.mu.Unlock() }()
		defer func() {
			if value := recover(); value != nil {
				s.mu.Lock()
				s.problem(fmt.Sprintf("%s: scheduled turn panicked (%v)", j.ID, value))
				s.mu.Unlock()
			}
		}()
		if err := runner.RunScheduled(ctx, Invocation{j.Session, fmt.Sprintf("[Scheduled cron %s] %s", j.ID, j.Prompt)}); err != nil && !errors.Is(err, context.Canceled) {
			s.mu.Lock()
			s.problem(fmt.Sprintf("%s: scheduled turn failed (%v)", j.ID, err))
			s.mu.Unlock()
		}
	}()
}
func (s *Scheduler) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return errors.New("cron stop is draining")
	}
	if s.loop != nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	e := &execution{cancel: cancel, done: make(chan struct{})}
	s.loop = e
	go func() {
		defer close(e.done)
		s.Tick(time.Now())
		timer := time.NewTicker(20 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-timer.C:
				s.Tick(now)
			}
		}
	}()
	return nil
}
func wait(ctx context.Context, executions []*execution) error {
	for _, e := range executions {
		select {
		case <-e.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Wait observes admitted runs; callers quiesce admission before relying on it.
func (s *Scheduler) Wait(ctx context.Context) error {
	s.mu.Lock()
	runs := make([]*execution, 0, len(s.running))
	for e := range s.running {
		runs = append(runs, e)
	}
	s.mu.Unlock()
	return wait(ctx, runs)
}

// Stop cancels and joins the ticker and current runs. Timed-out observers may
// resume joining; jobs remain armed in memory, permitting explicit later Start.
func (s *Scheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.stopping {
		s.generation++
		s.stopping = true
	}
	generation := s.generation
	loop := s.loop
	runs := make([]*execution, 0, len(s.running)+1)
	if s.loop != nil {
		s.loop.cancel()
		runs = append(runs, s.loop)
	}
	for e := range s.running {
		e.cancel()
		runs = append(runs, e)
	}
	s.mu.Unlock()
	if err := wait(ctx, runs); err != nil {
		return err
	}
	s.mu.Lock()
	if s.generation == generation && s.loop == loop {
		s.loop = nil
		s.stopping = false
	}
	s.mu.Unlock()
	return nil
}
