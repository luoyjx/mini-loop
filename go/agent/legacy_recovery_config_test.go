package agent

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestLegacyRecoveryConstructorSelection(t *testing.T) {
	empty, legacy, spaces, long := "", "explicit-legacy", " \t ", strings.Repeat("x", 513)
	for _, row := range []struct {
		name, modern, ambient, want string
		legacy                      *string
	}{
		{name: "ambient", ambient: "ambient-legacy", want: "ambient-legacy"},
		{name: "empty-ambient"},
		{name: "explicit-empty", ambient: "ambient-legacy", legacy: &empty},
		{name: "explicit-legacy", ambient: "ambient-legacy", legacy: &legacy, want: legacy},
		{name: "modern-before-ambient", modern: "modern", ambient: "ambient-legacy", want: "modern"},
		{name: "modern-before-explicit", modern: "modern", legacy: &legacy, want: "modern"},
		{name: "modern-space-is-nonempty", modern: spaces, ambient: "ambient-legacy", want: spaces},
		{name: "legacy-space-is-nonempty", ambient: "ambient-legacy", legacy: &spaces, want: spaces},
		{name: "legacy-513", ambient: "ambient-legacy", legacy: &long, want: long},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Setenv("FALLBACK_MODEL_ID", row.ambient)
			policy, err := NewDefaultRecovery(RecoveryConfig{FallbackModel: row.modern, LegacyFallbackModel: row.legacy})
			if err != nil {
				t.Fatal(err)
			}
			if policy.fallback != row.want {
				t.Fatalf("selected %q, want %q", policy.fallback, row.want)
			}
		})
	}
}

func TestLegacyRecoveryRetainsConstructionSnapshotDuringActualRetry(t *testing.T) {
	for _, row := range []struct {
		name, selected string
		explicit       bool
	}{
		{name: "ambient", selected: "construction-model"},
		{name: "explicit", selected: "construction-model", explicit: true},
		{name: "legacy-513", selected: strings.Repeat("x", 513)},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Setenv("FALLBACK_MODEL_ID", row.selected)
			legacy := row.selected
			config := RecoveryConfig{Waiter: &recoveryTestWaiter{}, Jitter: func() float64 { return 0 }}
			if row.explicit {
				config.LegacyFallbackModel = &legacy
			}
			policy, err := NewDefaultRecovery(config)
			if err != nil {
				t.Fatal(err)
			}
			legacy = "caller-mutation"
			t.Setenv("FALLBACK_MODEL_ID", "later-environment")
			provider := &fallbackProvider{}
			var selected string
			var fallbackEvents []string
			_, err = policy.Recover(context.Background(), RecoveryInput{Request: protocol.ModelRequest{
				Model: "primary", MaxTokens: 64,
				Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("safe probe")}},
			}}, RecoveryServices{Call: provider.Complete, SetModel: func(model string) { selected = model }, Emit: func(event RecoveryEvent) {
				if event.Action == RecoveryFallback && event.Model != nil {
					fallbackEvents = append(fallbackEvents, *event.Model)
				}
			}})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(provider.models, []string{"primary", "primary", "primary", row.selected}) || selected != row.selected || !reflect.DeepEqual(fallbackEvents, []string{row.selected}) {
				t.Fatal("retry did not use the construction snapshot", provider.models, selected, fallbackEvents)
			}
		})
	}
}
