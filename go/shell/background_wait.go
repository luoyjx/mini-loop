package shell

import (
	"fmt"
	"math"
	"time"
)

type backgroundWaitKind uint8

const (
	backgroundWaitNumeric backgroundWaitKind = iota
	backgroundWaitUnbounded
	backgroundWaitImmediate
	backgroundWaitFault
)

type BackgroundWaitFault uint8

const (
	BackgroundWaitTextFault BackgroundWaitFault = iota
	BackgroundWaitArrayFault
	BackgroundWaitObjectFault
	BackgroundWaitIntegerOverflow
)

// BackgroundWait contains only an execution policy, never original model data.
// Long finite deadlines are consumed in bounded timer steps without duration overflow.
type BackgroundWait struct {
	kind    backgroundWaitKind
	seconds float64
	label   string
	fault   BackgroundWaitFault
}

func NumericBackgroundWait(seconds float64, label string) BackgroundWait {
	if math.IsNaN(seconds) {
		return ImmediateBackgroundWait(label)
	}
	if math.IsInf(seconds, 1) {
		return UnboundedBackgroundWait()
	}
	return BackgroundWait{kind: backgroundWaitNumeric, seconds: seconds, label: label}
}
func UnboundedBackgroundWait() BackgroundWait { return BackgroundWait{kind: backgroundWaitUnbounded} }
func FaultBackgroundWait(fault BackgroundWaitFault) BackgroundWait {
	return BackgroundWait{kind: backgroundWaitFault, fault: fault}
}
func (wait BackgroundWait) failure() error {
	switch wait.fault {
	case BackgroundWaitTextFault:
		return fmt.Errorf("'<=' not supported between instances of 'str' and 'int'")
	case BackgroundWaitArrayFault:
		return fmt.Errorf("'<=' not supported between instances of 'list' and 'int'")
	case BackgroundWaitObjectFault:
		return fmt.Errorf("'<=' not supported between instances of 'dict' and 'int'")
	case BackgroundWaitIntegerOverflow:
		return fmt.Errorf("int too large to convert to float")
	default:
		return fmt.Errorf("invalid background wait fault")
	}
}
func backgroundWaitStep(seconds float64) (time.Duration, float64) {
	const maximum = 24 * time.Hour
	step := math.Min(seconds, maximum.Seconds())
	duration := time.Duration(step * float64(time.Second))
	if duration <= 0 {
		duration = time.Nanosecond
	}
	return duration, seconds - step
}

func ImmediateBackgroundWait(label string) BackgroundWait {
	return BackgroundWait{kind: backgroundWaitImmediate, label: label}
}
