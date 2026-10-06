package skills

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// LayeredCatalog preserves two independent construction snapshots. It neither
// assigns an owner nor grants tool authority; trusted callers choose the sources.
type LayeredCatalog struct {
	agent, user *Catalog
	diagnostics *Catalog
}

func NewLayeredCatalog(agent, user *Catalog) (*LayeredCatalog, error) {
	if agent == nil || user == nil {
		return nil, errors.New("layered skills require both source catalogues")
	}
	layered := &LayeredCatalog{agent: agent, user: user, diagnostics: EmptyCatalog()}
	for _, source := range []protocol.SkillScope{protocol.ScopeAgent, protocol.ScopeUser} {
		for _, problem := range layered.loader(source).Problems() {
			for i := 0; i < problem.Count; i++ {
				layered.diagnostics.report(problem.Kind, string(source)+": "+problem.Message)
			}
		}
	}
	return layered, nil
}

func (catalog *LayeredCatalog) loader(source protocol.SkillScope) *Catalog {
	if source == protocol.ScopeAgent {
		return catalog.agent
	}
	return catalog.user
}
func (catalog *LayeredCatalog) Problems() []Problem { return catalog.diagnostics.Problems() }
func (catalog *LayeredCatalog) ProblemStatistics() ProblemStatistics {
	return catalog.diagnostics.ProblemStatistics()
}

func layeredLine(source protocol.SkillScope, entry Entry) string {
	description := entry.Description
	if description == "" {
		description = "-"
	}
	return fmt.Sprintf("  - %s:%s [digest=%.16s]: %s", source, entry.Name, entry.Digest, description)
}
func omissionNotice(count int) string {
	return fmt.Sprintf("  [%d more skill(s) omitted; catalogue is full]", count)
}
func (catalog *LayeredCatalog) render(agent, user []string, notice string) string {
	lines := []string{"Agent-provided skills:"}
	if len(catalog.agent.ordered) == 0 {
		lines = append(lines, "  (none)")
	} else {
		lines = append(lines, agent...)
	}
	lines = append(lines, "User-scoped skills:")
	if len(catalog.user.ordered) == 0 {
		lines = append(lines, "  (none)")
	} else {
		lines = append(lines, user...)
	}
	if notice != "" {
		lines = append(lines, notice)
	}
	return strings.Join(lines, "\n")
}

// Descriptions reserves both provenance headings and the largest omission notice
// before accepting agent lines, then user lines, under one character budget.
func (catalog *LayeredCatalog) Descriptions() string {
	var agent, user []string
	for _, entry := range catalog.agent.ordered {
		agent = append(agent, layeredLine(protocol.ScopeAgent, entry))
	}
	for _, entry := range catalog.user.ordered {
		user = append(user, layeredLine(protocol.ScopeUser, entry))
	}
	full := catalog.render(agent, user, "")
	if utf8.RuneCountInString(full) <= MaxCatalogue {
		return full
	}
	total := len(agent) + len(user)
	reserve := omissionNotice(total)
	var keptAgent, keptUser []string
	used := utf8.RuneCountInString(catalog.render(nil, nil, reserve))
	for _, line := range agent {
		added := utf8.RuneCountInString(line) + 1
		if used+added <= MaxCatalogue {
			keptAgent = append(keptAgent, line)
			used += added
		}
	}
	for _, line := range user {
		added := utf8.RuneCountInString(line) + 1
		if used+added <= MaxCatalogue {
			keptUser = append(keptUser, line)
			used += added
		}
	}
	dropped := total - len(keptAgent) - len(keptUser)
	catalog.diagnostics.report(ProblemCatalogue, fmt.Sprintf("%d skill(s) omitted from the catalogue at the combined 8,000-character limit", dropped))
	return catalog.render(keptAgent, keptUser, omissionNotice(dropped))
}

func (catalog *LayeredCatalog) available(source protocol.SkillScope) string {
	var names []string
	for _, candidate := range []protocol.SkillScope{protocol.ScopeAgent, protocol.ScopeUser} {
		if source != "" && source != candidate {
			continue
		}
		for _, entry := range catalog.loader(candidate).ordered {
			names = append(names, string(candidate)+":"+entry.Name)
		}
	}
	if len(names) == 0 {
		return "(none)"
	}
	var kept []string
	used := 0
	for _, name := range names {
		added := utf8.RuneCountInString(name)
		if len(kept) > 0 {
			added += 2
		}
		if used+added > MaxCatalogue-256 {
			break
		}
		kept = append(kept, name)
		used += added
	}
	result := strings.Join(kept, ", ")
	if dropped := len(names) - len(kept); dropped > 0 {
		result += fmt.Sprintf(", ... (%d more omitted)", dropped)
	}
	return result
}

func (catalog *LayeredCatalog) Load(ctx context.Context, input protocol.LoadSkillInput) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if input.Name == "" {
		return "", errors.New("Skill name must be a non-empty valid identifier")
	}
	name := input.Name
	prefix, remainder, qualified := strings.Cut(name, ":")
	qualified = qualified && (prefix == "agent" || prefix == "user")
	var scope protocol.SkillScope
	if input.Scope != nil {
		scope = protocol.SkillScope(strings.ToLower(pytext.Strip(string(*input.Scope))))
		if scope != protocol.ScopeAgent && scope != protocol.ScopeUser {
			return "", fmt.Errorf("Unknown skill scope %s. Expected one of: agent, user", pytext.Repr(string(*input.Scope)))
		}
	}
	if qualified {
		if scope != "" && string(scope) != prefix {
			return "", fmt.Errorf("Skill %s selects source %s, which conflicts with scope %s", pytext.Repr(name), pytext.Repr(prefix), pytext.Repr(string(scope)))
		}
		scope, name = protocol.SkillScope(prefix), remainder
	}
	if !validName(name) {
		return "", errors.New("Skill name must match [A-Za-z0-9][A-Za-z0-9._-]{0,63}")
	}
	if scope == "" {
		_, agent := catalog.agent.entries[name]
		_, user := catalog.user.entries[name]
		if agent && user {
			return "", fmt.Errorf("Ambiguous skill %s; choose agent:%s or user:%s or pass scope='agent'/'user'", pytext.Repr(name), name, name)
		}
		if !agent && !user {
			return "", fmt.Errorf("Unknown skill %s. Available: %s", pytext.Repr(name), catalog.available(""))
		}
		scope = protocol.ScopeUser
		if agent {
			scope = protocol.ScopeAgent
		}
	}
	loader := catalog.loader(scope)
	entry, exists := loader.entries[name]
	if !exists {
		return "", fmt.Errorf("Unknown %s skill %s. Available: %s", scope, pytext.Repr(name), catalog.available(scope))
	}
	if err := loader.verifySnapshot(ctx, entry); err != nil {
		return "", err
	}
	return fmt.Sprintf("<skill name=\"%s\" source=\"%s\" digest=\"%s\">\n%s\n</skill>", name, scope, entry.Digest, entry.Body), nil
}
