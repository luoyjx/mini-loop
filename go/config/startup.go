package config

import "github.com/luoyjx/mini-loop/go/dotenv"

// PreparedEnvironment retains import-stage effects, including writes preceding
// a dotenv fault. Secrets are exposed only through Reveal; this value's ordinary
// diagnostic rendering contains no environment values.
type PreparedEnvironment struct {
	environment map[string]string
	Failure     *dotenv.Failure
}

func (p PreparedEnvironment) String() string   { return "<prepared startup environment>" }
func (p PreparedEnvironment) GoString() string { return p.String() }

// Reveal returns a detached snapshot for settings, auth and child-process input.
func (p PreparedEnvironment) Reveal() map[string]string {
	result := make(map[string]string, len(p.environment))
	for name, value := range p.environment {
		result[name] = value
	}
	return result
}

// PrepareEnvironment implements the config import phase: dotenv override=true,
// then Source's custom-base-URL authentication cleanup. Caller-origin evidence
// is explicit. Nil environment means empty, not ambient process lookup.
// It does not load Settings, create workspace roots, mutate process state or
// install discovery in the pure Load API. Prepare once, then use Load as needed.
func PrepareEnvironment(environment map[string]string, origin dotenv.DiscoveryContext) PreparedEnvironment {
	input := make(dotenv.Environment, len(environment))
	for name, value := range environment {
		input[dotenv.Name(name)] = value
	}
	application := dotenv.LoadFile(dotenv.FileOptions{Discovery: dotenv.DiscoveryOptions{Context: origin}}, input, true)
	result := PreparedEnvironment{environment: make(map[string]string, len(application.Environment)), Failure: application.Failure}
	for name, value := range application.Environment {
		result.environment[string(name)] = value
	}
	if result.Failure == nil && result.environment["ANTHROPIC_BASE_URL"] != "" {
		delete(result.environment, "ANTHROPIC_AUTH_TOKEN")
	}
	return result
}
