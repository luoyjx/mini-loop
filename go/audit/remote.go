// Package audit reports configured posture gaps, not an OS enforcement attestation.
package audit

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type Severity string

const (
	Critical Severity = "critical"
	High     Severity = "high"
	Medium   Severity = "medium"
	Info     Severity = "info"
)

type Check string

const (
	PostureUnavailable Check = "posture-unavailable"
	Authentication     Check = "authentication"
	ShellConfinement   Check = "shell-confinement"
	ShellUnavailable   Check = "shell-confinement-unavailable"
	SecretMasking      Check = "secret-masking"
	SecretUnregistered Check = "secret-unregistered"
	DurableState       Check = "durable-state"
	DurableFailing     Check = "durable-state-failing"
	ActionJournal      Check = "action-journal"
)

type Finding struct {
	Severity Severity `json:"severity"`
	Check    Check    `json:"check"`
	Detail   string   `json:"detail"`
	Remedy   string   `json:"remedy"`
}
type Posture struct {
	Sandbox             *string
	SandboxReason       *string
	Secrets             *string
	SecretsUnregistered int
	StateStore          *string
	StateStoreError     *string
	ActionJournal       *string
}

// A nil Posture includes missing, null and empty wire objects. Unknown health
// fields are ignored at decoding, but a nonempty posture remains distinguishable.
type Report struct {
	Authenticated bool
	Posture       *Posture
	Build         *string
	PID           *int
}

func (report *Report) UnmarshalJSON(data []byte) error {
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return err
	}
	if value.Kind() != jsonvalue.Object {
		return errors.New("audit report must be an object")
	}
	result := Report{}
	if build, exists := value.Lookup("build"); exists && build.Kind() != jsonvalue.Null {
		text, ok := build.Text()
		if !ok {
			return errors.New("build must be a string or null")
		}
		result.Build = &text
	}
	if pid, exists := value.Lookup("pid"); exists && pid.Kind() != jsonvalue.Null {
		text, ok := pid.Integer()
		if !ok {
			return errors.New("pid must be an integer or null")
		}
		number, err := strconv.Atoi(text)
		if err != nil {
			return fmt.Errorf("pid: %w", err)
		}
		result.PID = &number
	}
	if auth, exists := value.Lookup("authenticated"); exists && auth.Kind() != jsonvalue.Null {
		var ok bool
		result.Authenticated, ok = auth.Bool()
		if !ok {
			return errors.New("authenticated must be a boolean")
		}
	}
	posture, exists := value.Lookup("posture")
	if !exists || posture.Kind() == jsonvalue.Null {
		*report = result
		return nil
	}
	if posture.Kind() != jsonvalue.Object {
		return errors.New("posture must be an object or null")
	}
	if len(posture.Keys()) == 0 {
		*report = result
		return nil
	}
	p := &Posture{}
	for _, field := range []struct {
		name   string
		target **string
	}{
		{"sandbox", &p.Sandbox}, {"sandbox_reason", &p.SandboxReason},
		{"secrets", &p.Secrets}, {"state_store", &p.StateStore},
		{"state_store_error", &p.StateStoreError}, {"action_journal", &p.ActionJournal},
	} {
		v, exists := posture.Lookup(field.name)
		if !exists || v.Kind() == jsonvalue.Null {
			continue
		}
		text, ok := v.Text()
		if !ok {
			return fmt.Errorf("%s must be a string or null", field.name)
		}
		*field.target = &text
	}
	if count, exists := posture.Lookup("secrets_unregistered"); exists && count.Kind() != jsonvalue.Null {
		text, ok := count.Integer()
		if !ok {
			return errors.New("secrets_unregistered must be an integer")
		}
		p.SecretsUnregistered, err = strconv.Atoi(text)
		if err != nil {
			return fmt.Errorf("secrets_unregistered: %w", err)
		}
	}
	result.Posture = p
	*report = result
	return nil
}

func Remote(report Report, source string) []Finding {
	if report.Posture == nil {
		return []Finding{{Medium, PostureUnavailable, source + " did not report a posture; it is older than this audit or is not a mini-loop server", "upgrade the server, or audit it locally"}}
	}
	p := report.Posture
	findings := make([]Finding, 0, 7)
	add := func(severity Severity, check Check, detail, remedy string) {
		findings = append(findings, Finding{severity, check, detail, remedy})
	}
	if !report.Authenticated {
		add(High, Authentication, source+" accepts unauthenticated callers; every session and transcript on it is readable by anyone who can reach it", "set MINILOOP_API_TOKEN on the server")
	}
	if nonempty(p.SandboxReason) {
		add(High, ShellUnavailable, source+" asked for confinement and could not get it: "+*p.SandboxReason, "run it in a container, or on a host with a backend")
	} else if nullName(p.Sandbox, "NullSandbox", "UnavailableSandbox") {
		add(High, ShellConfinement, source+" runs shell commands unconfined on its host", "start it with a sandbox")
	}
	if nullName(p.Secrets, "NullSecretRegistry") {
		add(High, SecretMasking, source+" does not mask credentials in tool output", "start it with secrets=SecretRegistry.from_environ()")
	} else if p.SecretsUnregistered != 0 {
		add(High, SecretUnregistered, fmt.Sprintf("%s has a secret registry but %d credential-shaped variable(s) in its environment are not registered, so the shell inherits them and their values reach tool output unmasked", source, p.SecretsUnregistered), "register them on the server (SecretRegistry.from_environ() picks up every credential-shaped name)")
	}
	if nonempty(p.StateStoreError) {
		add(High, DurableFailing, source+" has a state store whose writes are failing: "+*p.StateStoreError, "check disk space and permissions on the server's database path")
	} else if named(p.StateStore, "NullStateStore") {
		add(Medium, DurableState, source+" keeps transcripts only in memory", "start it with a SQLite state store")
	}
	if named(p.ActionJournal, "InMemoryActionJournal") {
		add(High, ActionJournal, source+" cannot tell, after a restart, which side effects already happened", "start it with a SQLite state store")
	}
	sort.SliceStable(findings, func(i, j int) bool {
		left, right := priority(findings[i].Severity), priority(findings[j].Severity)
		if left != right {
			return left < right
		}
		return findings[i].Check < findings[j].Check
	})
	return findings
}
func named(value *string, name string) bool { return value != nil && *value == name }
func nonempty(value *string) bool           { return value != nil && *value != "" }
func nullName(value *string, names ...string) bool {
	if value == nil || *value == "None" {
		return true
	}
	for _, name := range names {
		if *value == name {
			return true
		}
	}
	return false
}
func priority(severity Severity) int {
	switch severity {
	case Critical:
		return 0
	case High:
		return 1
	case Medium:
		return 2
	case Info:
		return 3
	}
	return -1
}
func Render(findings []Finding) (string, error) {
	if len(findings) == 0 {
		return "audit: no findings.", nil
	}
	counts := [4]int{}
	lines := make([]string, 0, len(findings)*3+1)
	for _, finding := range findings {
		index := priority(finding.Severity)
		if index < 0 {
			return "", fmt.Errorf("unknown severity: %s", finding.Severity)
		}
		counts[index]++
		lines = append(lines, fmt.Sprintf("[%-8s] %s", strings.ToUpper(string(finding.Severity)), finding.Check), "           "+finding.Detail, "           -> "+finding.Remedy)
	}
	summary := make([]string, 0, 4)
	for index, severity := range []Severity{Critical, High, Medium, Info} {
		if counts[index] != 0 {
			summary = append(summary, fmt.Sprintf("%d %s", counts[index], severity))
		}
	}
	lines = append(lines, fmt.Sprintf("\n%d finding(s): %s", len(findings), strings.Join(summary, ", ")))
	return strings.Join(lines, "\n"), nil
}
