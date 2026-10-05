package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/luoyjx/mini-loop/go/protocol"
)

const MaxSteerChars = 16000
const MaxSteerQueue = 100
const EventSteeringDelivered SessionEventKind = "steering_delivered"
const EventPostureUpdate SessionEventKind = "posture_update"

type SteeringDeliveredEvent struct {
	Count int
	Text  string
}
type PostureUpdateEvent struct {
	Count int
	Text  string
}

func (e SessionEvent) SteeringDelivered() (SteeringDeliveredEvent, bool) {
	return e.steeringDelivered, e.kind == EventSteeringDelivered
}
func (e SessionEvent) PostureUpdate() (PostureUpdateEvent, bool) {
	return e.postureUpdate, e.kind == EventPostureUpdate
}

type SteeringDelivery string

const DeliverySteering SteeringDelivery = "steering"
const DeliveryNewTurn SteeringDelivery = "new_turn"

type SteeringReceipt struct {
	Session   SessionID        `json:"session"`
	Queued    int              `json:"queued"`
	Busy      bool             `json:"busy"`
	Delivered SteeringDelivery `json:"delivered"`
}

type permissionModeSource interface{ CurrentMode() PermissionMode }

// This lock is independent of the live transcript/model lock. Only a managed
// session owns these controls; fresh children never drain their parent's queue.
type sessionControl struct {
	mu       sync.Mutex
	mode     PermissionMode
	steering []string
	posture  []string
}

func (c *sessionControl) CurrentMode() PermissionMode {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mode
}
func (c *sessionControl) snapshot() (PermissionMode, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mode, len(c.steering)
}
func charPrefix(text string, cap int) (string, bool) {
	n := 0
	for offset := range text {
		if n == cap {
			return text[:offset], true
		}
		n++
	}
	return text, false
}
func (c *sessionControl) steer(text string) int {
	if prefix, cut := charPrefix(text, MaxSteerChars); cut {
		text = prefix + "\n[steer truncated]"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.steering) == MaxSteerQueue {
		copy(c.steering, c.steering[1:])
		c.steering = c.steering[:MaxSteerQueue-1]
	}
	c.steering = append(c.steering, text)
	return len(c.steering)
}
func (c *sessionControl) change(mode PermissionMode, hasRun bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	old := c.mode
	c.mode = mode
	if old == mode || !hasRun {
		return
	}
	var meaning string
	switch mode {
	case ModeReadonly:
		meaning = "mutating tools are now refused outright"
	case ModeInteractive:
		meaning = "risky actions now ask for approval"
	case ModeAuto:
		meaning = "actions now run without asking; the deny-list still applies"
	}
	c.posture = append(c.posture, fmt.Sprintf("Permission posture changed by the operator: %s -> %s. %s", old, mode, meaning))
}
func (s *Session) permissionMode() PermissionMode {
	if s.control != nil {
		return s.control.CurrentMode()
	}
	return s.mode
}

// Called after user injectors, before context facts/compaction and model input.
// Events are recording projections; injected history keeps the live raw text.
func (s *Session) injectControls() {
	if s.control == nil {
		return
	}
	c := s.control
	c.mu.Lock()
	steers := c.steering
	c.steering = nil
	c.mu.Unlock()
	if len(steers) > 0 {
		body := strings.Join(steers, "\n\n")
		display, _ := charPrefix(body, 2000)
		s.events.append(SessionEvent{kind: EventSteeringDelivered, steeringDelivered: SteeringDeliveredEvent{len(steers), display}})
		s.appendMessages(protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent("<user_interjection>\n" + body + "\n</user_interjection>")})
	}
	c.mu.Lock()
	notes := c.posture
	c.posture = nil
	c.mu.Unlock()
	if len(notes) > 0 {
		body := strings.Join(notes, "\n")
		s.events.append(SessionEvent{kind: EventPostureUpdate, postureUpdate: PostureUpdateEvent{len(notes), body}})
		s.appendMessages(protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent("<posture_update>\n" + body + "\n</posture_update>")})
	}
}

// Steer parks input for the next model round/turn, even when currently idle.
func (s *ManagedSession) Steer(text string) (int, error) {
	s.mu.Lock()
	if err := s.admissionError(); err != nil {
		s.mu.Unlock()
		return 0, err
	}
	queued := s.core.control.steer(text)
	s.mu.Unlock()
	s.core.persistence.refreshRecord()
	return queued, nil
}
func (s *ManagedSession) ChangePermissionMode(mode PermissionMode) (PermissionMode, error) {
	if !mode.Valid() {
		return "", errors.New("unknown permission mode")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.admissionError(); err != nil {
		return "", err
	}
	s.core.control.change(mode, s.runCount > 0)
	return mode, nil
}

// SubmitSteering implements HTTP wakeup: publish an owned active turn before
// returning, or append to its next-round queue when admission is occupied.
// The manager's existing drain/cancel lifecycle owns this background holder.
func (s *ManagedSession) SubmitSteering(text string) (SteeringReceipt, error) {
	s.mu.Lock()
	if err := s.admissionError(); err != nil {
		s.mu.Unlock()
		return SteeringReceipt{}, err
	}
	select {
	case <-s.admission:
		s.mu.Unlock()
		run, err := DefaultRunContext()
		if err == nil {
			err = s.core.persistence.requireLease(context.Background())
		}
		if err != nil {
			s.admission <- struct{}{}
			return SteeringReceipt{}, err
		}
		s.mu.Lock()
		if err := s.admissionError(); err != nil {
			s.mu.Unlock()
			s.admission <- struct{}{}
			return SteeringReceipt{}, err
		}
		ctx, active := s.beginTurnLocked(context.Background())
		s.mu.Unlock()
		go func() { defer func() { s.admission <- struct{}{} }(); s.runActive(ctx, text, run, active) }()
		return SteeringReceipt{s.ID(), 0, false, DeliveryNewTurn}, nil
	default:
		queued := s.core.control.steer(text)
		s.mu.Unlock()
		s.core.persistence.refreshRecord()
		return SteeringReceipt{s.ID(), queued, true, DeliverySteering}, nil
	}
}
func (m *SessionManager) Steer(owner OwnerID, id SessionID, text string) (SteeringReceipt, error) {
	session, err := m.Get(owner, id)
	if err != nil {
		return SteeringReceipt{}, err
	}
	return session.SubmitSteering(text)
}
