package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type legacyRecoveryEnvironment map[string]string

type legacyRecoveryConstructor struct {
	Name             string
	Environment      legacyRecoveryEnvironment
	AfterEnvironment legacyRecoveryEnvironment `json:"after_environment"`
	ExplicitFallback *string                   `json:"explicit_fallback"`
	Selected         *string
	AfterSelected    *string `json:"after_selected"`
}

type legacyRecoveryAgent struct {
	Name                string
	SettingsEnvironment legacyRecoveryEnvironment `json:"settings_environment"`
	AgentEnvironment    legacyRecoveryEnvironment `json:"agent_environment"`
	AfterEnvironment    legacyRecoveryEnvironment `json:"after_environment"`
	RecoveryKind        string                    `json:"recovery_kind"`
	ExplicitFallback    *string                   `json:"explicit_fallback"`
	SettingsFallback    *string                   `json:"settings_fallback"`
	Selected            *string
	AfterSelected       *string `json:"after_selected"`
	RecoveryClass       string  `json:"recovery_class"`
}

type legacyRecoveryRequest struct {
	Turn  int
	Model string
}

type legacyRecoveryObservation struct {
	Type      SessionEventKind
	Action    RecoveryAction
	Error     *string
	Attempt   *int
	Model     *string
	Agent     string
	Depth     int
	MessageID MessageID `json:"message_id"`
}

type legacyRecoveryError struct {
	Type, Text string
}

type legacyRecoveryRun struct {
	Name             string
	Environment      legacyRecoveryEnvironment
	AfterEnvironment legacyRecoveryEnvironment `json:"after_environment"`
	RecoveryKind     string                    `json:"recovery_kind"`
	TurnCount        int                       `json:"turn_count"`
	SettingsFallback *string                   `json:"settings_fallback"`
	Selected         *string
	Requests         []legacyRecoveryRequest
	Events           []legacyRecoveryObservation
	ErrorEvents      []legacyRecoveryObservation `json:"error_events"`
	Waits            []float64
	Outputs          []*string
	Errors           []*legacyRecoveryError
	ModelAfterTurns  []*string `json:"model_after_turns"`
}

type legacyRecoveryFixture struct {
	Constructors []legacyRecoveryConstructor
	Agents       []legacyRecoveryAgent
	Runs         []legacyRecoveryRun
}

type legacyRecoveryProvider struct {
	turn     int
	requests []legacyRecoveryRequest
}

// The Source fake raises its named Overloaded exception. Preserve that class
// through the existing provider diagnostic seam instead of exposing Go's test
// type name in the Agent's normal error output.
type legacyRecoveryProviderError struct{ *recoveryFixtureError }

func (failure *legacyRecoveryProviderError) ProviderErrorDetail() (string, bool) {
	return failure.detail.Class + ": " + failure.detail.Message, true
}

func (provider *legacyRecoveryProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	provider.requests = append(provider.requests, legacyRecoveryRequest{provider.turn, request.Model})
	if len(provider.requests) <= 3 {
		return protocol.ModelReply{}, &legacyRecoveryProviderError{&recoveryFixtureError{protocol.ModelFailure{
			Kind: protocol.ModelFailureOther, Class: "Overloaded", Message: "fixture overloaded", Status: 529,
		}}}
	}
	return streamedReply("done"), nil
}

func setLegacyRecoveryEnvironment(t *testing.T, environment legacyRecoveryEnvironment) {
	t.Helper()
	for key := range environment {
		if key != "FALLBACK_MODEL_ID" && key != "MINILOOP_FALLBACK_MODEL" {
			t.Fatal("unexpected Source environment key", key)
		}
	}
	for _, key := range []string{"FALLBACK_MODEL_ID", "MINILOOP_FALLBACK_MODEL"} {
		t.Setenv(key, environment[key])
	}
}

func legacyRecoveryText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func legacyRecoverySelected(t *testing.T, policy Recovery) (*string, string) {
	t.Helper()
	switch policy := policy.(type) {
	case *DefaultRecovery:
		if policy == nil {
			t.Fatal("nil default recovery")
		}
		return legacyRecoveryText(policy.fallback), "DefaultRecovery"
	case DirectRecovery:
		return nil, "DirectRecovery"
	default:
		t.Fatalf("unexpected recovery %T", policy)
		return nil, ""
	}
}

func legacyRecoverySession(t *testing.T, environment legacyRecoveryEnvironment, modern *string, kind string, explicit *string, provider Provider) *Session {
	t.Helper()
	setLegacyRecoveryEnvironment(t, environment)
	var policy Recovery
	switch kind {
	case "direct":
		policy = DirectRecovery{}
	case "default", "explicit":
		fallback := modern
		if kind == "explicit" {
			fallback = explicit
		}
		// Keep nil default recovery when Settings has no modern fallback. The
		// real runtime constructor must then select the ambient legacy value.
		if kind == "explicit" || (fallback != nil && *fallback != "") {
			value := ""
			if fallback != nil {
				value = *fallback
			}
			created, err := NewDefaultRecovery(RecoveryConfig{FallbackModel: value})
			if err != nil {
				t.Fatal(err)
			}
			policy = created
		}
	default:
		t.Fatal("unknown Source recovery kind", kind)
	}
	catalog, err := NewToolCatalog()
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewRuntimeSession(RuntimeConfig{
		ID: "legacy-recovery", Owner: "fixture-owner", Provider: provider,
		Recovery: policy, Tools: catalog, Bash: echoExecutor{}, Workspace: t.TempDir(),
		Mode: ModeInteractive, MaxRounds: 4, Model: "primary", MaxTokens: 64,
		SystemBuilder: FixedSystem("Legacy recovery contract."), CachePolicy: NullCachePolicy{}, Label: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func loadLegacyRecoverySettings(t *testing.T, environment legacyRecoveryEnvironment) config.Settings {
	t.Helper()
	if environment == nil {
		t.Fatal("Source settings environment must be an explicit object")
	}
	settings, err := config.Load(environment, config.LoadOptions{Directory: t.TempDir(), HomeDirectory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return settings
}

func TestLegacyRecoveryMatchesActualSourceConstructorsAgentsAndTurns(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-legacy-recovery.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture legacyRecoveryFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Constructors) != 9 || len(fixture.Agents) != 11 || len(fixture.Runs) != 6 {
		t.Fatal("Source inventory changed", len(fixture.Constructors), len(fixture.Agents), len(fixture.Runs))
	}
	for _, row := range fixture.Constructors {
		t.Run("constructor/"+row.Name, func(t *testing.T) {
			setLegacyRecoveryEnvironment(t, row.Environment)
			fallback := ""
			if row.ExplicitFallback != nil {
				fallback = *row.ExplicitFallback
			}
			policy, err := NewDefaultRecovery(RecoveryConfig{FallbackModel: fallback})
			if err != nil {
				t.Fatal(err)
			}
			selected, _ := legacyRecoverySelected(t, policy)
			if !reflect.DeepEqual(selected, row.Selected) {
				t.Fatalf("constructor selected %v, Source %v", selected, row.Selected)
			}
			setLegacyRecoveryEnvironment(t, row.AfterEnvironment)
			after, _ := legacyRecoverySelected(t, policy)
			if !reflect.DeepEqual(after, row.AfterSelected) {
				t.Fatalf("construction snapshot changed: %v, Source %v", after, row.AfterSelected)
			}
		})
	}
	for _, row := range fixture.Agents {
		t.Run("agent/"+row.Name, func(t *testing.T) {
			settings := loadLegacyRecoverySettings(t, row.SettingsEnvironment)
			if !reflect.DeepEqual(settings.FallbackModel, row.SettingsFallback) {
				t.Fatal("legacy environment changed the public Settings fallback", settings.FallbackModel, row.SettingsFallback)
			}
			session := legacyRecoverySession(t, row.AgentEnvironment, settings.FallbackModel, row.RecoveryKind, row.ExplicitFallback, &legacyRecoveryProvider{})
			selected, class := legacyRecoverySelected(t, session.recovery)
			if !reflect.DeepEqual(selected, row.Selected) || class != row.RecoveryClass {
				t.Fatal("Agent constructor differs", selected, class, row.Selected, row.RecoveryClass)
			}
			setLegacyRecoveryEnvironment(t, row.AfterEnvironment)
			after, _ := legacyRecoverySelected(t, session.recovery)
			if !reflect.DeepEqual(after, row.AfterSelected) {
				t.Fatal("Agent recovery reread the environment", after, row.AfterSelected)
			}
		})
	}
	for _, row := range fixture.Runs {
		t.Run("run/"+row.Name, func(t *testing.T) {
			if len(row.Outputs) != row.TurnCount || len(row.Errors) != row.TurnCount || len(row.ModelAfterTurns) != row.TurnCount {
				t.Fatal("Source turn inventory changed")
			}
			settings := loadLegacyRecoverySettings(t, row.Environment)
			if !reflect.DeepEqual(settings.FallbackModel, row.SettingsFallback) {
				t.Fatal("run Settings differ", settings.FallbackModel, row.SettingsFallback)
			}
			provider := &legacyRecoveryProvider{requests: []legacyRecoveryRequest{}}
			session := legacyRecoverySession(t, row.Environment, settings.FallbackModel, row.RecoveryKind, nil, provider)
			selected, _ := legacyRecoverySelected(t, session.recovery)
			if !reflect.DeepEqual(selected, row.Selected) {
				t.Fatal("run selected recovery differs", selected, row.Selected)
			}
			waiter := &recoveryTestWaiter{waits: []float64{}}
			if policy, ok := session.recovery.(*DefaultRecovery); ok {
				policy.wait, policy.jitter = waiter, func() float64 { return 0 }
			}
			setLegacyRecoveryEnvironment(t, row.AfterEnvironment)
			for turn := 1; turn <= row.TurnCount; turn++ {
				provider.turn = turn
				run, err := ExplicitHumanRunContext(HumanRunConfig{})
				if err != nil {
					t.Fatal(err)
				}
				run.messageID = MessageID(fmt.Sprintf("turn-%d", turn))
				output, err := session.RunWithContext(context.Background(), fmt.Sprintf("turn %d", turn), run)
				// Every captured Agent.run returns normally, including the direct
				// control's error output. Do not treat it as an escaping exception.
				if row.Errors[turn-1] != nil || err != nil || row.Outputs[turn-1] == nil || output != *row.Outputs[turn-1] {
					t.Fatalf("turn %d: output=%q error=%v, Source output=%v error=%+v", turn, output, err, row.Outputs[turn-1], row.Errors[turn-1])
				}
				if actual := legacyRecoveryText(session.recoveryModel); !reflect.DeepEqual(actual, row.ModelAfterTurns[turn-1]) {
					t.Fatal("persistent recovery model differs", actual, row.ModelAfterTurns[turn-1])
				}
			}
			observed, errors := []legacyRecoveryObservation{}, []legacyRecoveryObservation{}
			for _, record := range session.Events() {
				if record.Event.Kind() != EventRecovery && record.Event.Kind() != EventError {
					continue
				}
				encoded, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				var event legacyRecoveryObservation
				if err := json.Unmarshal(encoded, &event); err != nil {
					t.Fatal(err)
				}
				if record.Event.Kind() == EventRecovery {
					observed = append(observed, event)
				} else {
					errors = append(errors, event)
				}
			}
			if !reflect.DeepEqual(provider.requests, row.Requests) || !reflect.DeepEqual(waiter.waits, row.Waits) {
				t.Fatal("actual retry requests/waits differ", provider.requests, waiter.waits, row.Requests, row.Waits)
			}
			if !reflect.DeepEqual(observed, row.Events) || !reflect.DeepEqual(errors, row.ErrorEvents) {
				t.Fatal("recovery/error events differ", observed, errors, row.Events, row.ErrorEvents)
			}
		})
	}
}
