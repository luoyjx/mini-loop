package agent

import (
	"context"
	"fmt"
	"sync"
)

const SubscriberQueueMax = 2000

// EventSink runs in event order after publication. Implementations must not
// recursively emit on the same session or call its blocking Messages method.
// They may inspect ManagedSession.Info, event snapshots, or subscriptions.
type EventSink interface {
	OnEvent(context.Context, SessionEventRecord) error
}

type EventSubscription struct {
	events *sessionEvents
	queue  chan SessionEventRecord
	once   sync.Once
}

func (subscription *EventSubscription) Events() <-chan SessionEventRecord { return subscription.queue }
func (subscription *EventSubscription) Close() {
	subscription.once.Do(func() {
		subscription.events.mu.Lock()
		defer subscription.events.mu.Unlock()
		delete(subscription.events.subscribers, subscription)
		close(subscription.queue)
	})
}

// Called under the bus lock. Publication never waits for a consumer.
func (subscription *EventSubscription) offer(record SessionEventRecord) {
	select {
	case subscription.queue <- record:
		return
	default:
	}
	select {
	case <-subscription.queue:
	default:
	}
	select {
	case subscription.queue <- record:
	default:
	}
}
func (record SessionEventRecord) clone() SessionEventRecord {
	record.Event, record.Scope = record.Event.clone(), record.Scope.clone()
	if record.Trajectory != nil {
		v := *record.Trajectory
		v.ID = clonePointer(v.ID)
		v.TraceID = clonePointer(v.TraceID)
		v.GroupID = clonePointer(v.GroupID)
		record.Trajectory = &v
	}
	if record.Terminal != nil {
		v := *record.Terminal
		v.DurationMS = clonePointer(v.DurationMS)
		v.Persisted = clonePointer(v.Persisted)
		v.RecordingError = clonePointer(v.RecordingError)
		if v.TrajectoryDisabledState != nil {
			state := *v.TrajectoryDisabledState
			state.PersistError = clonePointer(state.PersistError)
			v.TrajectoryDisabledState = &state
		}
		record.Terminal = &v
	}
	return record
}
func (events *sessionEvents) subscribe(replay bool) *EventSubscription {
	events.mu.Lock()
	defer events.mu.Unlock()
	subscription := &EventSubscription{events: events, queue: make(chan SessionEventRecord, SubscriberQueueMax)}
	if replay {
		for _, record := range events.records {
			subscription.offer(record.clone())
		}
	}
	if events.subscribers == nil {
		events.subscribers = make(map[*EventSubscription]struct{})
	}
	events.subscribers[subscription] = struct{}{}
	return subscription
}
func (events *sessionEvents) subscriberCount() int {
	events.mu.Lock()
	defer events.mu.Unlock()
	return len(events.subscribers)
}
func (events *sessionEvents) sinkProblem() string {
	events.mu.Lock()
	defer events.mu.Unlock()
	return events.sinkError
}
func notifyEventSink(sink EventSink, record SessionEventRecord) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("event sink panicked: %T", value)
		}
	}()
	return sink.OnEvent(context.Background(), record.clone())
}
func (events *sessionEvents) notifySink(record SessionEventRecord) {
	if events.sink == nil {
		return
	}
	if err := notifyEventSink(events.sink, record); err != nil {
		detail := maskedText(events.secrets, boundedError(err))
		events.mu.Lock()
		events.sinkError = detail
		events.mu.Unlock()
	}
}
func (s *Session) Subscribe(replay bool) *EventSubscription { return s.events.subscribe(replay) }
func (s *Session) SubscriberCount() int                     { return s.events.subscriberCount() }
func (s *Session) SinkError() string                        { return s.events.sinkProblem() }

// EventsAfter returns the available in-memory suffix. ManagedSession.CatchUpEvents
// reads the configured store with a context; sequence gaps remain visible.
func (s *Session) EventsAfter(cursor EventSequence) []SessionEventRecord {
	all := s.Events()
	result := make([]SessionEventRecord, 0, len(all))
	for _, record := range all {
		if record.Sequence > cursor {
			result = append(result, record)
		}
	}
	return result
}
