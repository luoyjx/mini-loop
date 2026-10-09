package audit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/fnmatch"
	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/selfaudit"
)

const (
	ConcurrentSideEffects Check = "concurrent-side-effects"
	ConcurrentWriters     Check = "concurrent-writers"
	ResourceLimits        Check = "resource-limits"
	SecretUnresolved      Check = "secret-unresolved"
	SecretTooShort        Check = "secret-too-short"
	MCPProblems           Check = "mcp-problems"
	CronProblems          Check = "cron-problems"
	SkillsRejected        Check = "skills-rejected"
	ExperimentalWorkflows Check = "workflows"
)

// Aliases keep audit composition and manager observation on one concrete schema.
type ToolObservation = agent.DeploymentToolObservation
type CatalogObservation = agent.DeploymentCatalogObservation
type SubsystemName = agent.DeploymentSubsystemName
type ProblemChannel = agent.DeploymentProblemChannel
type LiveObservation = agent.DeploymentAuditObservation

// ComposeLive combines configuration findings with full deployment observations.
// It performs no runtime callbacks, filesystem IO, secret lookup or tool effects.
// Manager observation and local CLI binding are separate from this pure library.
func ComposeLive(configuration []Finding, observation LiveObservation, environment secrets.Environment) ([]Finding, error) {
	findings := append(make([]Finding, 0, len(configuration)+20), configuration...)
	for _, f := range findings {
		if priority(f.Severity) < 0 {
			return nil, fmt.Errorf("unknown severity: %s", f.Severity)
		}
	}
	add := func(s Severity, c Check, d, r string) { findings = append(findings, Finding{s, c, d, r}) }
	hasShell := observation.Catalog == nil
	var sideEffects, writers []string
	if observation.Catalog != nil {
		for _, tool := range observation.Catalog.Tools {
			if tool.Name == "bash" {
				hasShell = true
			}
			switch tool.Risk {
			case agent.RiskRead, agent.RiskWrite, agent.RiskExec, agent.RiskExternal, agent.RiskUnclassified:
			default:
				return nil, fmt.Errorf("unknown tool risk: %s", tool.Risk)
			}
			if !tool.ParallelSafe {
				continue
			}
			switch tool.Risk {
			case agent.RiskWrite:
				writers = append(writers, string(tool.Name))
			case agent.RiskExec, agent.RiskExternal, agent.RiskUnclassified:
				sideEffects = append(sideEffects, string(tool.Name))
			}
		}
	}
	sort.Strings(sideEffects)
	sort.Strings(writers)
	if len(sideEffects) > 0 {
		add(High, ConcurrentSideEffects, strings.Join(sideEffects, ", ")+" declare themselves safe to run concurrently while running code or acting outside this machine (or declaring no risk at all); two in one model-emitted batch can fire overlapping side effects the harness cannot serialize or verify", "drop parallel_safe on tools that exec or act externally, or confirm the downstream has its own concurrency control")
	}
	if len(writers) > 0 {
		add(Medium, ConcurrentWriters, strings.Join(writers, ", ")+" declare themselves safe to run concurrently while also mutating local state; two of them in one model-emitted batch can lose an update, and the harness cannot check the claim", "drop parallel_safe on tools that write, or confirm their writes go somewhere with its own concurrency control")
	}
	shellSeverity := Medium
	if hasShell {
		shellSeverity = High
	}
	if observation.SandboxReason != nil {
		add(shellSeverity, ShellUnavailable, "confinement was requested but is unavailable: "+*observation.SandboxReason+". Shell commands are running on the host with the agent's full rights.", "run this deployment in a container, or on a host where a backend exists; passing a different sandbox= will not help")
	} else if observation.SandboxNull {
		add(shellSeverity, ShellConfinement, "shell commands run on the host with the agent's full user rights; `cwd` is the workspace but nothing stops `cd /`. The DANGEROUS command list is a typo guard, not confinement -- it is substring matching, and `$(echo rm) -rf /` walks past it", "pass sandbox=SeatbeltSandbox(...) (or default_sandbox(workspace))")
	}
	if hasShell {
		add(Medium, ResourceLimits, "nothing bounds how much a shell command consumes: memory, disk and CPU are unlimited, and a single command allocated 700 MB unopposed in testing. `bash_timeout` bounds wall time only. A sandbox does not help -- it confines where a command writes, not how much", "run this deployment in a container with memory, disk and CPU limits; shell `ulimit` was measured and does not enforce these on macOS")
	}
	exposed := credentialNames(environment)
	if observation.SecretsNull {
		severity := Medium
		detail := "no secret registry: the shell inherits the whole process environment and tool output is not masked"
		if len(exposed) > 0 {
			severity = High
			detail += fmt.Sprintf("; %d credential-shaped variables are set (%s)", len(exposed), previewNames(exposed))
		}
		add(severity, SecretMasking, detail, "pass secrets=SecretRegistry.from_environ()")
	} else {
		registered := make(map[secrets.Name]bool, len(observation.SecretNames))
		for _, name := range observation.SecretNames {
			registered[name] = true
		}
		var missing []secrets.Name
		for _, name := range exposed {
			if !registered[name] {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			add(High, SecretUnregistered, fmt.Sprintf("%d credential-shaped variable(s) are set but not registered for masking (%s); the shell inherits them and their values reach tool output unmasked", len(missing), previewNames(missing)), "register them (SecretRegistry.from_environ() picks up every credential-shaped name), or unset them in this deployment")
		}
	}
	if nonempty(observation.PersistenceError) {
		add(High, DurableFailing, "a state store is configured but its writes are failing ("+*observation.PersistenceError+"); the agent keeps running and every session on this process is unrecoverable after a restart", "check disk space and permissions on the database path; this is a live fault, not a configuration choice")
	}
	if len(observation.Unresolved) > 0 {
		add(High, SecretUnresolved, joinNames(observation.Unresolved)+" are registered as secrets but their values could not be read, so nothing is being masked for them while the deployment reports masking as on", "check the source of those values; a registered-but-unreadable secret is worse than an unregistered one, because it looks safe")
	}
	if len(observation.ShortValues) > 0 {
		add(Medium, SecretTooShort, joinNames(observation.ShortValues)+" hold values below the masking floor, so they are left in output rather than shredding unrelated text", "use longer values, or stop registering these names")
	}
	for _, row := range []struct {
		ledger          selfaudit.Ledger
		check           Check
		subject, remedy string
	}{
		{observation.MCP, MCPProblems, "MCP tool(s) were refused or truncated", "a tool that quietly failed to appear looks the same as a server that was never connected; rename the servers or drop one"},
		{observation.Cron, CronProblems, "scheduled job(s) were refused, dropped or fired into nothing", "a schedule that stops running looks exactly like one that was never created; fix or cancel the jobs"},
		{observation.Skills, SkillsRejected, "skill file(s) were refused, shadowed or truncated", "a skill is an instruction the model follows; fix or remove the files rather than leaving the set ambiguous"},
	} {
		detail, err := ledgerDetail(row.ledger, row.subject)
		if err != nil {
			return nil, err
		}
		if detail != "" {
			add(Medium, row.check, detail, row.remedy)
		}
	}
	channels := append([]ProblemChannel(nil), observation.Channels...)
	sort.SliceStable(channels, func(i, j int) bool { return channels[i].Name < channels[j].Name })
	seen := make(map[SubsystemName]bool, len(channels))
	for _, channel := range channels {
		name := string(channel.Name)
		if name == "" {
			return nil, fmt.Errorf("empty subsystem name")
		}
		if seen[channel.Name] {
			return nil, fmt.Errorf("duplicate subsystem: %s", name)
		}
		seen[channel.Name] = true
		if strings.HasPrefix(name, "_") || name == "cron" || name == "skills" {
			continue
		}
		detail, err := ledgerDetail(channel.Ledger, "problem(s) reported by "+name)
		if err != nil {
			return nil, err
		}
		if detail != "" {
			add(Medium, Check(name+"-problems"), detail, "inspect manager."+name+".problems; a subsystem that degrades quietly stays degraded")
		}
	}
	if observation.StateNull {
		add(Medium, DurableState, "no state store: transcripts and event cursors exist only in this process and are lost on restart", "pass state_store=SQLiteStateStore(path)")
	}
	if observation.JournalMemory {
		add(shellSeverity, ActionJournal, "the action journal is in-memory, so a restarted process cannot tell which side effects already happened and may run them again", "pass a SQLite state_store; the journal becomes durable with it")
	}
	if observation.Workflows {
		add(Info, ExperimentalWorkflows, "the experimental workflow surface is enabled; it is process-local and not restart-safe", "confirm this manager is not the one serving untrusted input")
	}
	sort.SliceStable(findings, func(i, j int) bool {
		a, b := priority(findings[i].Severity), priority(findings[j].Severity)
		if a != b {
			return a < b
		}
		return findings[i].Check < findings[j].Check
	})
	return findings, nil
}

func credentialNames(environment secrets.Environment) []secrets.Name {
	if environment == nil {
		environment = secrets.CurrentEnvironment()
	}
	patterns := secrets.DefaultPatterns()
	compiled := make([]fnmatch.Pattern, len(patterns))
	for i, p := range patterns {
		compiled[i] = fnmatch.Compile(p)
	}
	names := make([]secrets.Name, 0)
	for name, value := range environment {
		if value == "" {
			continue
		}
		for _, p := range compiled {
			if p.Matches(pytext.Upper(string(name))) {
				names = append(names, name)
				break
			}
		}
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	return names
}
func joinNames(names []secrets.Name) string {
	values := make([]string, len(names))
	for i, n := range names {
		values[i] = string(n)
	}
	return strings.Join(values, ", ")
}
func previewNames(names []secrets.Name) string {
	if len(names) > 3 {
		return joinNames(names[:3]) + "..."
	}
	return joinNames(names)
}
func ledgerDetail(ledger selfaudit.Ledger, subject string) (string, error) {
	if ledger.Failure != nil {
		return "", fmt.Errorf("problem observation failed: %s", ledger.Failure.Class)
	}
	if len(ledger.Entries) == 0 {
		return "", nil
	}
	text := ledger.Entries[0]
	if ledger.Summary != nil {
		if len(*ledger.Summary) == 0 {
			return "", fmt.Errorf("nonempty ledger has an empty summary")
		}
		text = (*ledger.Summary)[0]
		if ledger.Churning {
			if ledger.Total == nil {
				return "", fmt.Errorf("churning ledger has no total")
			}
			text += " [log churning: " + groupDecimal(ledger.Total.String()) + " occurrences seen, counts are lower bounds]"
		}
	}
	detail := fmt.Sprintf("%d %s: %s", len(ledger.Entries), subject, text)
	if len(ledger.Entries) > 1 {
		detail += fmt.Sprintf(" (+%d more)", len(ledger.Entries)-1)
	}
	return detail, nil
}
func groupDecimal(text string) string {
	first := len(text) % 3
	if first == 0 {
		first = 3
	}
	out := text[:first]
	for i := first; i < len(text); i += 3 {
		out += "," + text[i:i+3]
	}
	return out
}
