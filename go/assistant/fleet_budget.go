package assistant

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// turnGate bounds how many model turns run at once across the whole fleet.
// Model calls are the fleet's largest cost and the provider's rate limit is
// shared, so the bound is on turns, not on tenants. Background turns use the
// background slots; the user's chat may use those and the slots kept free for
// chat, so a burst of mail or routines cannot make every owner wait.
type turnGate struct {
	background chan struct{}
	chat       chan struct{}
	waiting    atomic.Int64
}

func newTurnGate(turns, chat int) *turnGate {
	return &turnGate{background: make(chan struct{}, turns-chat), chat: make(chan struct{}, chat)}
}

func (g *turnGate) acquire(ctx context.Context, chat bool, interrupt <-chan struct{}) (func(), bool) {
	g.waiting.Add(1)
	defer g.waiting.Add(-1)
	reserved := g.chat
	if !chat {
		reserved = nil
	} else {
		select {
		case reserved <- struct{}{}:
			return func() { <-reserved }, true
		default:
		}
	}
	select {
	case <-ctx.Done():
		return nil, false
	case <-interrupt:
		return nil, false
	case g.background <- struct{}{}:
		return func() { <-g.background }, true
	case reserved <- struct{}{}:
		return func() { <-reserved }, true
	}
}

func (g *turnGate) inUse() int { return len(g.background) + len(g.chat) }

// tenantMeter is one tenant's share of the fleet's model budget: it admits
// the tenant's turns through the fleet's gate, counts every model call into
// the registry, and holds the tenant's model work once its calls for the day
// reach the quota. The hold is checked before a turn is claimed, so a turn in
// flight finishes and the quota can be passed by that turn's remaining
// calls.
type tenantMeter struct {
	fleet *Fleet
	id    TenantID
	// post writes a system line to the tenant's channel.
	post func(string)
	// waiting counts this tenant's loops waiting for a fleet turn, which
	// the reaper does not mistake for a stall.
	waiting atomic.Int64

	mu       sync.Mutex
	day      string
	calls    int
	notified bool
}

func (m *tenantMeter) Held() bool {
	held, _ := m.hold(m.fleet.now())
	return held
}

func (m *tenantMeter) Acquire(ctx context.Context, chat bool, interrupt <-chan struct{}) (func(), bool) {
	m.waiting.Add(1)
	defer m.waiting.Add(-1)
	return m.fleet.gate.acquire(ctx, chat, interrupt)
}

// hold reports whether the day's quota is spent and when the next day starts.
func (m *tenantMeter) hold(now time.Time) (bool, time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.roll(now)
	quota := m.fleet.config.DailyCalls
	return quota > 0 && m.calls >= quota, nextDay(now)
}

// roll starts a new count when the local day changed.
func (m *tenantMeter) roll(now time.Time) {
	if day := now.Format(slotLayout); day != m.day {
		m.day, m.calls, m.notified = day, 0, false
	}
}

func nextDay(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
}

// record counts one model call. The registry write is what a bill would be
// built from, so it happens for every call; the hold is announced once a day.
func (m *tenantMeter) record(ctx context.Context, usage protocol.TokenUsage) {
	now := m.fleet.now()
	m.mu.Lock()
	m.roll(now)
	m.calls++
	day := m.day
	quota := m.fleet.config.DailyCalls
	announce := quota > 0 && m.calls >= quota && !m.notified
	if announce {
		m.notified = true
	}
	m.mu.Unlock()
	m.fleet.calls.Add(1)
	m.fleet.count(ctx, usageKey{m.id, day}, usageSum{1, usage.InputTokens.Metered(), usage.OutputTokens.Metered()})
	if announce && m.post != nil {
		m.post(fmt.Sprintf("今天的模型额度（%d 次调用）已用完：新的处理会排队，%s 之后继续。批准、拒绝和查看不受影响。", quota, nextDay(now).Format("01-02 15:04")))
	}
}

// usageWait bounds how long one model call waits for the registry to take
// its count.
const usageWait = 3 * time.Second

type usageKey struct {
	id  TenantID
	day string
}

type usageSum struct{ calls, input, output int }

// count writes model use to the registry, or keeps it until the registry
// takes it. While any is kept the registry is not asked: one that is away
// must not hold every model call for usageWait.
func (f *Fleet) count(ctx context.Context, key usageKey, sum usageSum) {
	f.owedMu.Lock()
	owing := len(f.owed) > 0
	f.owedMu.Unlock()
	if !owing {
		write, cancel := context.WithTimeout(context.WithoutCancel(ctx), usageWait)
		err := f.registry.addUsage(write, key.id, key.day, sum.calls, sum.input, sum.output)
		cancel()
		if err == nil {
			return
		}
		fmt.Fprintf(f.log, "fleet: record usage for %s: %v; kept until the registry takes it\n", key.id, err)
	}
	f.owedMu.Lock()
	kept := f.owed[key]
	f.owed[key] = usageSum{kept.calls + sum.calls, kept.input + sum.input, kept.output + sum.output}
	f.owedMu.Unlock()
}

// settleUsage tells the registry the model use it missed. A count stays kept
// until the registry has taken it, so one the registry took without this node
// hearing of it is counted twice rather than not at all.
func (f *Fleet) settleUsage(ctx context.Context) {
	f.owedMu.Lock()
	owed := make(map[usageKey]usageSum, len(f.owed))
	for key, sum := range f.owed {
		owed[key] = sum
	}
	f.owedMu.Unlock()
	for key, sum := range owed {
		write, cancel := context.WithTimeout(ctx, usageWait)
		err := f.registry.addUsage(write, key.id, key.day, sum.calls, sum.input, sum.output)
		cancel()
		if err != nil {
			// Still away; the rest waits with it.
			return
		}
		f.owedMu.Lock()
		if left := f.owed[key]; left == sum {
			delete(f.owed, key)
		} else {
			f.owed[key] = usageSum{left.calls - sum.calls, left.input - sum.input, left.output - sum.output}
		}
		f.owedMu.Unlock()
	}
}

// usageOwed is the number of model calls the registry has not been told of.
func (f *Fleet) usageOwed() int {
	f.owedMu.Lock()
	defer f.owedMu.Unlock()
	calls := 0
	for _, sum := range f.owed {
		calls += sum.calls
	}
	return calls
}

// provider wraps the fleet's shared model provider for this tenant; a
// streaming provider stays streaming.
func (m *tenantMeter) provider(inner agent.Provider) agent.Provider {
	metered := meteredProvider{inner, m}
	if streaming, ok := inner.(agent.StreamingProvider); ok {
		return meteredStreaming{metered, streaming}
	}
	return metered
}

type meteredProvider struct {
	inner agent.Provider
	meter *tenantMeter
}

func (p meteredProvider) Complete(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	reply, err := p.inner.Complete(ctx, request)
	if err == nil {
		p.meter.record(ctx, reply.Usage)
	}
	return reply, err
}

type meteredStreaming struct {
	meteredProvider
	streaming agent.StreamingProvider
}

func (p meteredStreaming) CompleteStream(ctx context.Context, request protocol.ModelRequest, emit func(protocol.StreamDelta) error) (protocol.ModelReply, error) {
	reply, err := p.streaming.CompleteStream(ctx, request, emit)
	if err == nil {
		p.meter.record(ctx, reply.Usage)
	}
	return reply, err
}
