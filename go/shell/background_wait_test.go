package shell

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"
)

func TestBackgroundWaitFaultRunsStartObserverThenJoinsProcess(t *testing.T) {
	executor, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	wait := FaultBackgroundWait(BackgroundWaitTextFault)
	var pid ProcessID
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := executor.ExecuteBackground(ctx, BackgroundCommand{Command: "sleep 30", Wait: &wait, Started: func(value ProcessID) { pid = value }})
	if err != nil || result.Error == nil || *result.Error != "Error: '<=' not supported between instances of 'str' and 'int'" || pid == 0 {
		t.Fatal("post-spawn failure stage changed", result, err, pid)
	}
	if result.ExitCode == nil {
		t.Fatal("fault failed to reap its process")
	}
}
func TestBackgroundLongFiniteWaitUsesBoundedTimerSteps(t *testing.T) {
	seconds := float64(1e12)
	step, remaining := backgroundWaitStep(seconds)
	if step != 24*time.Hour || remaining != seconds-step.Seconds() || remaining <= 0 {
		t.Fatal("finite timeout overflowed or lost remaining deadline", step, remaining)
	}
	step, remaining = backgroundWaitStep(0.00000000001)
	if step != time.Nanosecond || remaining != 0 {
		t.Fatal("sub-nanosecond deadline changed")
	}
	if NumericBackgroundWait(math.NaN(), "nan").kind != backgroundWaitImmediate {
		t.Fatal("unguarded Source NaN wait changed")
	}
	if NumericBackgroundWait(math.Inf(1), "inf").kind != backgroundWaitUnbounded {
		t.Fatal("positive infinity wait changed")
	}

}

type backgroundWaitLabelMasker struct{}

func (backgroundWaitLabelMasker) MaskText(text string) string {
	return strings.ReplaceAll(text, "private-label", "[masked]")
}
func TestBackgroundTimeoutMasksCallerLabelBeforeReturning(t *testing.T) {
	executor, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	executor, err = executor.WithMasker(backgroundWaitLabelMasker{})
	if err != nil {
		t.Fatal(err)
	}
	wait := NumericBackgroundWait(-1, "private-label")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := executor.ExecuteBackground(ctx, BackgroundCommand{Command: "sleep 30", Wait: &wait})
	if err != nil || result.Error == nil || *result.Error != "Error: Timeout ([masked]s)" {
		t.Fatal("timeout retained unmasked caller label", result, err)
	}
}
