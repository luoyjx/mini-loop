package agent

import (
	"context"
	"errors"

	"github.com/luoyjx/mini-loop/go/cron"
)

type cronMasker struct{ masker TextMasker }

func (m cronMasker) Mask(value string) string {
	if m.masker == nil {
		return value
	}
	return m.masker.MaskText(value)
}

type managerCronResolver struct{ manager *SessionManager }

func (r managerCronResolver) ResolveScheduled(id cron.SessionID) (cron.Runner, error) {
	r.manager.mu.Lock()
	defer r.manager.mu.Unlock()
	if r.manager.state != ManagerActive {
		return nil, nil
	}
	session := r.manager.sessions[SessionID(id)]
	if session == nil {
		return scheduledRestoreRunner{r.manager, SessionID(id)}, nil
	}
	return managedCronRunner{session}, nil
}

// Resolve inside the scheduler-owned context so Stop can cancel pending lookup.
type scheduledRestoreRunner struct {
	manager *SessionManager
	id      SessionID
}

func (r scheduledRestoreRunner) RunScheduled(ctx context.Context, invocation cron.Invocation) error {
	if invocation.Authority() != cron.Untrusted || SessionID(invocation.Session()) != r.id {
		return errors.New("cron invocation does not match its scheduled identity")
	}
	session, err := r.manager.RestoreScheduledSession(ctx, r.id)
	if err != nil {
		return err
	}
	return (managedCronRunner{session}).RunScheduled(ctx, invocation)
}

type managedCronRunner struct{ session *ManagedSession }

func (r managedCronRunner) RunScheduled(ctx context.Context, invocation cron.Invocation) error {
	if invocation.Authority() != cron.Untrusted || SessionID(invocation.Session()) != r.session.ID() {
		return errors.New("cron invocation does not match its admitted session")
	}
	// Run creates a fresh DefaultRunContext. The scheduling caller's context,
	// grants, actor and prior parent message identity are never borrowed.
	_, err := r.session.Run(ctx, invocation.Prompt())
	return err
}

type ScheduleCronRequest struct {
	Cron, Prompt       string
	Recurring, Durable *bool
}
type CronJobView struct {
	ID        cron.ID `json:"id"`
	Cron      string  `json:"cron"`
	Prompt    string  `json:"prompt"`
	Recurring bool    `json:"recurring"`
	Durable   bool    `json:"durable"`
	LastFired string  `json:"last_fired"`
	Armed     bool    `json:"armed"`
}

// CronScheduler is the privileged embedding/operator surface. User-facing
// callers use owner-scoped methods below; direct access has no owner check.
func (manager *SessionManager) CronScheduler() *cron.Scheduler { return manager.cron }
func (manager *SessionManager) cronSession(owner OwnerID, id SessionID, write bool) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	s := manager.sessions[id]
	if owner == "" || s == nil || s.Owner() != owner {
		return ErrSessionNotFound
	}
	if write && manager.state != ManagerActive {
		return ErrManagerStopped
	}
	return nil
}

// Start matches the source lifecycle: restored jobs start a ticker, remaining
// disarmed. New manager construction itself never starts a goroutine.
func (manager *SessionManager) Start() error {
	manager.cronMu.Lock()
	defer manager.cronMu.Unlock()
	manager.mu.Lock()
	active := manager.state == ManagerActive
	manager.mu.Unlock()
	if !active {
		return ErrManagerStopped
	}
	if len(manager.cron.Jobs()) == 0 {
		return nil
	}
	return manager.cron.Start()
}
func (manager *SessionManager) ScheduleCron(owner OwnerID, id SessionID, request ScheduleCronRequest) (cron.Scheduled, error) {
	manager.cronMu.Lock()
	defer manager.cronMu.Unlock()
	if err := manager.cronSession(owner, id, true); err != nil {
		return cron.Scheduled{}, err
	}
	job, err := manager.cron.Schedule(cron.Request{Session: cron.SessionID(id), Cron: request.Cron, Prompt: request.Prompt, Recurring: request.Recurring, Durable: request.Durable})
	// Source starts its ticker before save, even when save fails after in-memory
	// admission. Preserve that retained job/start behavior; validation never starts.
	if job.Job.ID != "" {
		if startErr := manager.cron.Start(); err == nil {
			err = startErr
		}
	}
	return job, err
}
func (manager *SessionManager) CronJobs(owner OwnerID, id SessionID) ([]CronJobView, error) {
	manager.cronMu.Lock()
	defer manager.cronMu.Unlock()
	if err := manager.cronSession(owner, id, false); err != nil {
		return nil, err
	}
	jobs := []CronJobView{}
	for _, j := range manager.cron.Jobs() {
		if SessionID(j.Session) == id {
			jobs = append(jobs, CronJobView{j.ID, j.Cron, j.Prompt, j.Recurring, j.Durable, j.LastFired, manager.cron.Armed(j.ID)})
		}
	}
	return jobs, nil
}
func (manager *SessionManager) CancelCron(owner OwnerID, id SessionID, job cron.ID) (string, error) {
	manager.cronMu.Lock()
	defer manager.cronMu.Unlock()
	if err := manager.cronSession(owner, id, true); err != nil {
		return "", err
	}
	session := cron.SessionID(id)
	return manager.cron.Cancel(job, &session)
}
func (manager *SessionManager) ArmCron(owner OwnerID, id SessionID, job cron.ID) (string, error) {
	manager.cronMu.Lock()
	defer manager.cronMu.Unlock()
	if err := manager.cronSession(owner, id, true); err != nil {
		return "", err
	}
	session := cron.SessionID(id)
	return manager.cron.Arm(job, &session), nil
}
