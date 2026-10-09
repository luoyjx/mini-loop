package shell

import "reflect"

// SandboxPosture reports configuration only, never proof of OS confinement.
type SandboxPosture struct {
	Name   string
	Reason *string
}

// SandboxPostureProvider is a read-only optional seam for executors/factories.
// Implementations must be concurrency safe and must not execute a command.
type SandboxPostureProvider interface{ SandboxPosture() SandboxPosture }

// SandboxReasonProvider exposes an explicit unavailable-backend reason, when one
// exists. A configured argv wrapper alone cannot attest to confinement.
type SandboxReasonProvider interface{ SandboxReason() *string }

func (executor *Executor) SandboxPosture() SandboxPosture {
	return InspectSandbox(executor.sandbox)
}

// InspectSandbox reads configuration metadata without binding or executing it.
// Optional reason callbacks must be concurrency safe, like the sandbox itself.
func InspectSandbox(sandbox Sandbox) SandboxPosture {
	if sandbox == nil {
		return SandboxPosture{Name: "None"}
	}
	t := reflect.TypeOf(sandbox)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	name := t.Name()
	if name == "" {
		name = t.Kind().String()
	}
	result := SandboxPosture{Name: name}
	if source, ok := sandbox.(SandboxReasonProvider); ok {
		if reason := source.SandboxReason(); reason != nil {
			value := *reason
			result.Reason = &value
		}
	}
	return result
}
