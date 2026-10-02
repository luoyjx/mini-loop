package secrets

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type secretContracts struct {
	Secret          string
	DefaultPatterns []string `json:"default_patterns"`
	Masks           []struct {
		Values []struct {
			Name  Name
			Value string
		}
		Text, Masked string
		Settings     struct {
			MaskWith  *string `json:"mask_with"`
			MinLength *int    `json:"min_length"`
		}
		Names, Short, Unresolved []Name
	}
	Environments []struct {
		Environment        Environment
		Patterns           []string
		Extra              []Name
		Command            string
		Names, Found       []Name
		Injected, Scrubbed Environment
	}
	Previews []struct {
		Block   protocol.Block
		Preview string
	}
	Payload, Collision struct{ Input, Masked string }
	Rotation           struct {
		Injected      Environment
		Masked, Reset string
		Calls         int
	}
	Failure struct {
		Initial, Cached, Retried string
		Unresolved, Remaining    []Name
		Calls                    int
	}
	Casing []struct{ Text, Lower, Upper string }
}

func contracts(t *testing.T) secretContracts {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-secrets.json")
	if err != nil {
		t.Fatal(err)
	}
	var value secretContracts
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}
func TestRegistryMasksMatchPython(t *testing.T) {
	fixture := contracts(t)
	if !reflect.DeepEqual(DefaultPatterns(), fixture.DefaultPatterns) {
		t.Fatal("pattern drift")
	}
	for _, example := range fixture.Masks {
		registry := New(Config{example.Settings.MaskWith, example.Settings.MinLength})
		for _, entry := range example.Values {
			registry.RegisterValue(entry.Name, entry.Value)
		}
		if actual := registry.MaskText(example.Text); actual != example.Masked {
			t.Fatalf("%q: %q != %q", example.Text, actual, example.Masked)
		}
		if !reflect.DeepEqual(registry.Names(), example.Names) || !reflect.DeepEqual(registry.ShortValues(), example.Short) || !reflect.DeepEqual(registry.Unresolved(), example.Unresolved) {
			t.Fatal("report drift", registry.Names(), registry.ShortValues(), registry.Unresolved())
		}
	}
}
func TestRegistryEnvironmentSelectionMatchesPython(t *testing.T) {
	for _, example := range contracts(t).Environments {
		registry := FromEnvironment(Config{}, example.Environment, example.Patterns, example.Extra)
		if !reflect.DeepEqual(registry.Names(), example.Names) || !reflect.DeepEqual(registry.FindInText(example.Command), example.Found) || !reflect.DeepEqual(registry.EnvForCommand(example.Command), example.Injected) || !reflect.DeepEqual(registry.ScrubEnvironment(example.Environment), example.Scrubbed) {
			t.Fatal("environment drift", example, registry.Names(), registry.FindInText(example.Command), registry.EnvForCommand(example.Command), registry.ScrubEnvironment(example.Environment))
		}
		// Scrubbing must not alter the inherited environment supplied by its caller.
		before := len(example.Environment)
		out := registry.ScrubEnvironment(example.Environment)
		out["NEW"] = "changed"
		if len(example.Environment) != before {
			t.Fatal("environment aliased")
		}
	}
}
func TestCachedRotationAndFailedLookupRetryMatchPython(t *testing.T) {
	fixture := contracts(t)
	registry := New(Config{})
	calls := 0
	registry.RegisterLookup("KEY", func() (string, error) {
		calls++
		if calls == 1 {
			return "old-credential", nil
		}
		return "new-credential", nil
	})
	if !reflect.DeepEqual(registry.EnvForCommand("KEY"), fixture.Rotation.Injected) || registry.MaskText("old-credential new-credential") != fixture.Rotation.Masked || calls != fixture.Rotation.Calls {
		t.Fatal("cached rotation drift", calls)
	}
	registry.RegisterValue("KEY", "new-credential")
	if registry.MaskText("old-credential new-credential") != fixture.Rotation.Reset {
		t.Fatal("re-registration did not reset")
	}
	registry = New(Config{})
	now := time.Unix(0, 0)
	registry.now = func() time.Time { return now }
	calls = 0
	registry.RegisterLookup("KEY", func() (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("sensitive vault error")
		}
		return "retry-credential", nil
	})
	if registry.MaskText("retry-credential") != fixture.Failure.Initial || !reflect.DeepEqual(registry.Unresolved(), fixture.Failure.Unresolved) || registry.MaskText("retry-credential") != fixture.Failure.Cached {
		t.Fatal("failed lookup was not contained")
	}
	now = now.Add(FailedLookupRetry)
	if registry.MaskText("retry-credential") != fixture.Failure.Retried || calls != fixture.Failure.Calls || !reflect.DeepEqual(registry.Unresolved(), fixture.Failure.Remaining) {
		t.Fatal("retry drift", calls)
	}
}
func TestTypedPreviewsAndRecordingJSONMatchPython(t *testing.T) {
	fixture := contracts(t)
	registry := New(Config{})
	registry.RegisterValue("KEY", fixture.Secret)
	for _, example := range fixture.Previews {
		use, _ := example.Block.ToolUse()
		before, _ := protocol.PythonJSON(use.Input, true, false)
		actual, err := registry.ApprovalPreview(use.Input)
		if err != nil || actual != example.Preview {
			t.Fatal("preview drift", use.Name, actual, example.Preview, err)
		}
		copied := registry.MaskApprovalInput(use.Input)
		encoded, err := protocol.PythonJSON(copied, true, false)
		if err != nil || encoded != example.Preview {
			t.Fatal("typed projection drift", use.Name, encoded, example.Preview, err)
		}
		after, _ := protocol.PythonJSON(use.Input, true, false)
		if after != before {
			t.Fatal("mask changed live input")
		}
	}
	for _, example := range []struct{ Input, Masked string }{fixture.Payload, fixture.Collision} {
		actual, err := protocol.MaskedPythonJSON(json.RawMessage(example.Input), registry.MaskText, true, false)
		if err != nil || actual != example.Masked {
			t.Fatal("nested key/escape masking drift", actual, example.Masked, err)
		}
	}
	for _, example := range fixture.Casing {
		if pytext.Lower(example.Text) != example.Lower || pytext.Upper(example.Text) != example.Upper {
			t.Fatal("Unicode case drift", example, pytext.Lower(example.Text), pytext.Upper(example.Text))
		}
	}
}
func TestSharedRegistryResolvesOneValueAndReportsMissingOrPanickingSources(t *testing.T) {
	registry := New(Config{})
	var calls atomic.Int64
	registry.RegisterLookup("KEY", func() (string, error) { calls.Add(1); return "shared-credential", nil })
	var group sync.WaitGroup
	for range 64 {
		group.Add(1)
		go func() {
			defer group.Done()
			if registry.MaskText("shared-credential") != Mask {
				t.Error("unmasked")
			}
		}()
	}
	group.Wait()
	if calls.Load() != 1 {
		t.Fatal("source resolved repeatedly", calls.Load())
	}
	registry.RegisterLookup("PANIC", func() (string, error) { panic("sensitive") })
	registry.RegisterLookup("MISSING", nil)
	registry.RegisterValue("EMPTY", "")
	registry.MaskText("trigger")
	if !reflect.DeepEqual(registry.Unresolved(), []Name{"EMPTY", "MISSING", "PANIC"}) {
		t.Fatal(registry.Unresolved())
	}
}
func TestReregistrationCannotCacheAnOldInflightLookup(t *testing.T) {
	registry := New(Config{})
	entered, release := make(chan struct{}), make(chan struct{})
	registry.RegisterLookup("KEY", func() (string, error) { close(entered); <-release; return "old-credential", nil })
	done := make(chan string, 1)
	go func() { done <- registry.MaskText("old-credential new-credential") }()
	<-entered
	registry.RegisterValue("KEY", "new-credential")
	close(release)
	if <-done != "old-credential "+Mask {
		t.Fatal("stale lookup replaced new registration")
	}
}
func TestNullRegistryLeavesValuesAndEnvironmentUntouched(t *testing.T) {
	null := Null{}
	if null.MaskText("credential-0123") != "credential-0123" || len(null.FindInText("KEY")) != 0 || len(null.EnvForCommand("KEY")) != 0 || len(null.Names()) != 0 {
		t.Fatal("null changed posture")
	}
	environment := Environment{"KEY": "credential-0123"}
	out := null.ScrubEnvironment(environment)
	out["KEY"] = "changed"
	if environment["KEY"] != "credential-0123" {
		t.Fatal("null aliased environment")
	}
}
func TestEmptyReplacementAndRegexMetacharactersStayLiteral(t *testing.T) {
	replacement := `\$1\end`
	minimum := 1
	registry := New(Config{&replacement, &minimum})
	registry.RegisterValue("KEY", `[x]`)
	if registry.MaskText("[x][x]") != strings.Repeat(replacement, 2) {
		t.Fatal("replacement interpreted")
	}
}

func TestNilEnvironmentUsesDetachedProcessSnapshot(t *testing.T) {
	t.Setenv("MINILOOP_GO_TEST_SECRET", "process-credential-0123")
	registry := FromEnvironment(Config{}, nil, nil, nil)
	if registry.EnvForCommand("MINILOOP_GO_TEST_SECRET")["MINILOOP_GO_TEST_SECRET"] != "process-credential-0123" {
		t.Fatal("missing process default")
	}
	if _, exists := registry.ScrubEnvironment(nil)["MINILOOP_GO_TEST_SECRET"]; exists {
		t.Fatal("registered name retained")
	}
	snapshot := (Null{}).ScrubEnvironment(nil)
	snapshot["MINILOOP_GO_TEST_SECRET"] = "changed"
	if os.Getenv("MINILOOP_GO_TEST_SECRET") != "process-credential-0123" {
		t.Fatal("process environment mutated")
	}
}
