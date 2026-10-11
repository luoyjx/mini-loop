package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/decisions"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestUsageCountsMatchActualSourceMeterObservations(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-usage-consumer.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Mode string
			Usage      protocol.TokenUsage `json:"event_usage"`
			Consumer   struct {
				Anchor             *int
				Observations, Used int
			} `json:"consumer"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 34 {
		t.Fatal("Source inventory changed")
	}
	messages := []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("hello")}}
	for _, row := range fixture.Cases {
		t.Run(row.Name+"/"+row.Mode, func(t *testing.T) {
			meter := TokenMeter{}
			meter.Observe(row.Usage, messages, "fixture")
			actual := meter.Snapshot()
			if actual.Observations != row.Consumer.Observations || (actual.AnchorTokens == nil) != (row.Consumer.Anchor == nil) {
				t.Fatal("Source meter observation changed", actual, row.Consumer)
			}
			if actual.AnchorTokens != nil && *actual.AnchorTokens != *row.Consumer.Anchor {
				t.Fatal("Source meter anchor changed")
			}
			if meter.UsedFor(messages, "fixture") != row.Consumer.Used {
				t.Fatal("Source meter used changed")
			}
		})
	}
}

func TestDecisionUsageCountsRemainStrictAfterObservationDecode(t *testing.T) {
	fixture := readDecisionRuntimeFixture(t)
	row := fixture.LLM[0]
	for _, raw := range []string{`"13"`, `13.0`, `true`, `null`, `[]`, `{}`, `-1`} {
		t.Run(raw, func(t *testing.T) {
			reply := row.Reply.Clone()
			if err := json.Unmarshal([]byte(`{"input_tokens":`+raw+`,"output_tokens":3}`), &reply.Usage); err != nil {
				t.Fatal(err)
			}
			cfg := runtimeConfig(t.TempDir(), stateProviderFunc(func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) { return reply, nil }))
			session, err := NewRuntimeSession(cfg)
			if err != nil {
				t.Fatal(err)
			}
			provider, err := NewLLMDecisionProvider(session, RunContext{}, DecisionLLMConfig{})
			if err != nil {
				t.Fatal(err)
			}
			_, err = provider.Evaluate(context.Background(), row.Input)
			var failure *decisions.Error
			if !errors.As(err, &failure) || failure.Kind() != decisions.ValidationError {
				t.Fatal("noninteger observation gained strict decision count", err)
			}
		})
	}
}

func TestStoredCompactionRetainsObservedCountKinds(t *testing.T) {
	for _, raw := range []string{`"tokens"`, `13.75`, `true`, `null`, `[13]`, `{"count":13}`, `-3`} {
		assertStoredEventRoundTrip(t, []byte(`{"seq":1,"ts":10,"session":"s","transcript_epoch":2,"type":"compact","kind":"auto","transcript":"summary","replaced_messages":2,"replaced_tokens_estimate":10,"summary_input_tokens":`+raw+`,"summary_output_tokens":3,"summary_model":"model"}`))
	}
}

func TestObservedUsageCountSecretsAreMaskedWithoutChangingOriginal(t *testing.T) {
	safe, _ := json.Marshal(runtimeCanary)
	for _, raw := range []string{string(safe), `[` + string(safe) + `]`, `{` + string(safe) + `:` + string(safe) + `}`} {
		var usage protocol.TokenUsage
		if err := json.Unmarshal([]byte(`{"input_tokens":`+raw+`,"output_tokens":3}`), &usage); err != nil {
			t.Fatal(err)
		}
		before, err := json.Marshal(usage)
		if err != nil {
			t.Fatal(err)
		}
		events := &sessionEvents{secrets: runtimeSecrets(), sessionID: "counts", epoch: 1}
		events.append(SessionEvent{kind: EventModelEnd, modelEnd: ModelEndEvent{Status: ModelCompleted, Usage: &usage}})
		events.append(SessionEvent{kind: EventCompact, compact: CompactionEvent{kind: CompactAuto, summary: SummaryReceipt{Transcript: "summary", InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens}}})
		for _, record := range events.snapshot() {
			encoded, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			value, err := jsonvalue.Decode(string(encoded))
			if err != nil {
				t.Fatal(err)
			}
			value.MapStrings(func(text string) string {
				if strings.Contains(text, runtimeCanary) {
					t.Fatal("observed count leaked registered secret")
				}
				return text
			})
			assertStoredEventRoundTrip(t, encoded)
		}
		original, err := json.Marshal(usage)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(original, before) {
			t.Fatal("projection modified live observation")
		}
	}
}
