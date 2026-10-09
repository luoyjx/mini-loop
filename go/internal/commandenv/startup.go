// Package commandenv owns one CLI startup's process environment preparation.
// Embedding SDKs continue to use explicit, detached config inputs.
package commandenv

import (
	"os"
	"strings"

	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/dotenv"
)

type InitializedEnvironment struct {
	environment      map[string]string
	DiscoveryEnabled bool
}

func (e InitializedEnvironment) Reveal() map[string]string {
	result := make(map[string]string, len(e.environment))
	for name, value := range e.environment {
		result[name] = value
	}
	return result
}
func (e InitializedEnvironment) String() string   { return "<initialized CLI environment>" }
func (e InitializedEnvironment) GoString() string { return e.String() }

// Initialize runs before command interpretation/runtime construction. A compiled
// Go executable uses Source's frozen-program origin: cwd and its ancestors.
// It publishes complete or partial prepared effects to the process so default
// child factories observe them. A loading fault is returned after prior writes,
// before runtime creation; authentication cleanup already follows Source order.
// Call once at the executable entry point, before application goroutines start.
func Initialize() (InitializedEnvironment, error) {
	before := map[string]string{}
	input := dotenv.Environment{}
	for _, entry := range os.Environ() {
		name, value, ok := strings.Cut(entry, "=")
		if ok {
			before[name] = value
			input[dotenv.Name(name)] = value
		}
	}
	prepared := config.PrepareEnvironment(before, dotenv.DiscoveryContext{Frozen: true})
	result := InitializedEnvironment{environment: prepared.Reveal(), DiscoveryEnabled: !dotenv.LoadingDisabled(input)}
	for name, value := range result.environment {
		if previous, exists := before[name]; exists && previous == value {
			continue
		}
		if err := os.Setenv(name, value); err != nil {
			return result, err
		}
	}
	for name := range before {
		if _, exists := result.environment[name]; exists {
			continue
		}
		if err := os.Unsetenv(name); err != nil {
			return result, err
		}
	}
	if prepared.Failure != nil {
		return result, prepared.Failure
	}
	return result, nil
}
