package assistant

import (
	"sort"
	"sync"
	"time"
)

// healthThreshold is how many failures in a row turn a component into a
// problem worth the user's attention; one network blip is not.
const healthThreshold = 3

// HealthComponent names one background loop whose failures the page reports.
type HealthComponent string

const (
	HealthMailPoll HealthComponent = "mail_poll"
	HealthCalendar HealthComponent = "calendar_watch"
	HealthOutbox   HealthComponent = "outbox"
	HealthWorker   HealthComponent = "worker"
	HealthEmbed    HealthComponent = "embedding"
)

var healthLabels = map[HealthComponent]string{
	HealthMailPoll:                      "收信轮询（MCP mail_poll）",
	HealthCalendar:                      "日程查看（MCP calendar_list）",
	HealthOutbox:                        "发件箱（MCP 发送）",
	HealthWorker:                        "worker 模型调用",
	HealthEmbed:                         "语义检索（本地嵌入模型）",
	remoteHealth(RemoteTelegram, false): "Telegram 收消息",
	remoteHealth(RemoteTelegram, true):  "Telegram 发消息",
	remoteHealth(RemoteDiscord, false):  "Discord 收消息",
	remoteHealth(RemoteDiscord, true):   "Discord 发消息",
	remoteHealth(RemoteEmail, false):    "邮件信道收信（IMAP）",
	remoteHealth(RemoteEmail, true):     "邮件信道发信（SMTP）",
}

// remoteHealth names a remote channel's receive or send loop.
func remoteHealth(name string, send bool) HealthComponent {
	if send {
		return HealthComponent(name + "_send")
	}
	return HealthComponent(name + "_receive")
}

// HealthProblem is a component that failed at least healthThreshold times in
// a row and has not succeeded since.
type HealthProblem struct {
	Component HealthComponent
	Failures  int
	Since     time.Time
	LastError string
}

// Health counts consecutive failures per component. Any success clears the
// component. It lives in memory: after a restart a component must fail three
// times again before it is reported. A nil *Health records nothing.
type Health struct {
	mu      sync.Mutex
	entries map[HealthComponent]*healthEntry
}

type healthEntry struct {
	failures int
	since    time.Time
	last     string
}

func NewHealth() *Health { return &Health{entries: map[HealthComponent]*healthEntry{}} }

// Record notes one outcome: nil clears the component, an error extends its
// run of failures.
func (h *Health) Record(component HealthComponent, err error, now time.Time) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err == nil {
		delete(h.entries, component)
		return
	}
	entry, ok := h.entries[component]
	if !ok {
		entry = &healthEntry{since: now}
		h.entries[component] = entry
	}
	entry.failures++
	entry.last = truncateRunes(err.Error(), 200)
}

// Problems lists the components at or over the threshold, in a stable order.
func (h *Health) Problems() []HealthProblem {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	problems := []HealthProblem{}
	for component, entry := range h.entries {
		if entry.failures >= healthThreshold {
			problems = append(problems, HealthProblem{Component: component, Failures: entry.failures, Since: entry.since, LastError: entry.last})
		}
	}
	sort.Slice(problems, func(i, j int) bool { return problems[i].Component < problems[j].Component })
	return problems
}
