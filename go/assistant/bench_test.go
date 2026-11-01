package assistant

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"testing"
	"time"
)

// streamEvents is the synthetic mix: personal mail, bursts, invites, bulk,
// suspicious and cc'd mail, strangers, bills and the user's chat.
var streamEvents = []struct {
	preset Preset
	chat   string
	weight int
}{
	{PresetManager, "", 24},
	{PresetManagerFollow, "", 12},
	{PresetManagerBurst, "", 12},
	{PresetInviteFree, "", 12},
	{PresetInviteConflict, "", 8},
	{PresetNewsletter, "", 50},
	{PresetPhishing, "", 12},
	{PresetFriend, "", 24},
	{PresetQuestion, "", 12},
	{PresetCc, "", 24},
	{PresetStranger, "", 24},
	{PresetBill, "", 8},
	{"", "今天日程", 10},
	{"", "收件箱", 10},
	{"", "进行中的任务", 10},
}

// StreamReport is the acceptance run's summary.
type StreamReport struct {
	Events    int            `json:"events"`
	Turns     int            `json:"turns"`
	P50       time.Duration  `json:"p50_ns"`
	P95       time.Duration  `json:"p95_ns"`
	Max       time.Duration  `json:"max_ns"`
	Statuses  map[string]int `json:"statuses"`
	Decisions RouterStats    `json:"router"`
	QuietRate float64        `json:"quiet_rate"`
	Wall      time.Duration  `json:"wall_ns"`
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	index := int(float64(len(sorted)-1) * p)
	return sorted[index]
}

// TestSyntheticEventStreamAcceptance runs 300 events through the daemon with
// the offline model and reports latency, router decisions and the quiet rate:
// the share of events that reached the user neither as a message nor as an
// attention item.
func TestSyntheticEventStreamAcceptance(t *testing.T) {
	if testing.Short() {
		t.Skip("acceptance stream skipped in -short")
	}
	const total = 300
	app, _ := launchApp(t, t.TempDir(), nil, appOptions{workers: 4, now: time.Now})
	ctx := context.Background()
	if err := app.Store.SetCandidateMode(ctx, CandidateObserve, "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	sent := 0
	for round := 0; sent < total; round++ {
		for _, event := range streamEvents {
			for i := 0; i < event.weight/4 && sent < total; i++ {
				if event.chat != "" {
					if _, err := app.Store.ChatFromUser(ctx, event.chat, time.Now()); err != nil {
						t.Fatal(err)
					}
					sent++
					continue
				}
				if _, err := app.Connector.DeliverPreset(ctx, event.preset); err != nil {
					t.Fatal(err)
				}
				sent++
				if event.preset == PresetManagerBurst {
					sent++
				}
			}
		}
		if _, err := app.Poller.PollOnce(ctx); err != nil {
			t.Fatal(err)
		}
		app.Router.Wake()
	}
	deadline := time.Now().Add(2 * time.Minute)
	var triggers []Trigger
	for {
		var err error
		triggers, err = app.Store.Triggers(ctx, 2000)
		if err != nil {
			t.Fatal(err)
		}
		busy := 0
		for _, trigger := range triggers {
			if trigger.Status == TriggerPending || trigger.Status == TriggerRunning {
				busy++
			}
		}
		if busy == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d triggers still busy after two minutes", busy)
		}
		time.Sleep(50 * time.Millisecond)
	}
	report := StreamReport{Statuses: map[string]int{}, Decisions: app.Router.Stats(), Wall: time.Since(started)}
	latencies := []time.Duration{}
	quiet := 0
	for _, trigger := range triggers {
		if trigger.Kind == TriggerRoutine || trigger.Kind == TriggerTaskWakeup || trigger.Kind == TriggerJobDone || trigger.Kind == TriggerUpcoming {
			continue
		}
		report.Events++
		report.Statuses[string(trigger.Status)]++
		latencies = append(latencies, trigger.UpdatedAt.Sub(trigger.CreatedAt))
		switch trigger.Status {
		case TriggerQuiet, TriggerIgnored, TriggerObserved:
			quiet++
		case TriggerFailed:
			t.Errorf("trigger #%d failed: %s", trigger.ID, trigger.Decision)
		}
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	report.P50, report.P95, report.Max = percentile(latencies, 0.5), percentile(latencies, 0.95), percentile(latencies, 1)
	report.Turns = report.Decisions.Turns
	report.QuietRate = float64(quiet) / float64(report.Events)
	summary, _ := json.MarshalIndent(report, "", "  ")
	t.Logf("synthetic stream report:\n%s", summary)
	if path := os.Getenv("MINILOOP_ASSISTANT_STREAM_REPORT"); path != "" {
		if err := os.WriteFile(path, append(summary, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if report.Events < total {
		t.Fatalf("only %d of %d events became triggers", report.Events, total)
	}
	if report.Decisions.RulesIgnored == 0 || report.Decisions.Triaged == 0 || report.Decisions.Batched == 0 {
		t.Fatalf("every router path should be exercised: %+v", report.Decisions)
	}
	if report.QuietRate < 0.4 {
		t.Fatalf("quiet rate %.2f: too much reaches the user", report.QuietRate)
	}
	if report.Turns >= report.Events {
		t.Fatalf("filtering and batching should save turns: %d turns for %d events", report.Turns, report.Events)
	}
}
