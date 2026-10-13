package agent

import (
	"errors"
	"fmt"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

type AgentRole string

const (
	RoleExplore        AgentRole = "Explore"
	RoleWorker         AgentRole = "worker"
	RoleGeneralPurpose AgentRole = "general-purpose"
)

func normalizedRole(role AgentRole) AgentRole {
	return AgentRole(pytext.Lower(pytext.Strip(string(role))))
}

type RoleToolPolicy interface {
	Select(AgentRole, *ToolCatalog) (*ToolCatalog, error)
}

type CapabilityRoleToolPolicy struct {
	profiles map[AgentRole]map[Capability]bool
}
type RoleCapabilityProfile struct {
	Role         AgentRole
	Capabilities []Capability
}

// Profiles describe an ordered mapping. An exact duplicate role replaces its
// value at the first insertion position before normalized role collisions are
// resolved. Capabilities are literal set members, including explicit empty text.
func NewCapabilityRoleToolPolicy(profiles []RoleCapabilityProfile) (*CapabilityRoleToolPolicy, error) {
	result := &CapabilityRoleToolPolicy{profiles: make(map[AgentRole]map[Capability]bool, len(profiles))}
	positions := make(map[AgentRole]int, len(profiles))
	ordered := make([]RoleCapabilityProfile, 0, len(profiles))
	for _, profile := range profiles {
		if position, exists := positions[profile.Role]; exists {
			ordered[position] = profile
		} else {
			positions[profile.Role] = len(ordered)
			ordered = append(ordered, profile)
		}
	}
	for _, profile := range ordered {
		key := normalizedRole(profile.Role)
		allowed := make(map[Capability]bool, len(profile.Capabilities))
		for _, capability := range profile.Capabilities {
			allowed[capability] = true
		}
		result.profiles[key] = allowed
	}
	return result, nil
}
func DefaultRoleToolPolicy() *CapabilityRoleToolPolicy {
	reads := []Capability{CapabilityRepoRead, CapabilityRepoSearch, CapabilityRepoSemanticOutline, CapabilityRepoSymbol, CapabilityRepoReferences}
	worker := append(append([]Capability(nil), reads...), CapabilityWorkspaceWrite, CapabilityProcessExec, CapabilityObservationRecover)
	policy, _ := NewCapabilityRoleToolPolicy([]RoleCapabilityProfile{{RoleExplore, reads}, {RoleWorker, worker}, {RoleGeneralPurpose, worker}})
	return policy
}
func (policy *CapabilityRoleToolPolicy) Select(role AgentRole, parent *ToolCatalog) (*ToolCatalog, error) {
	if parent == nil {
		return nil, errors.New("role selection requires parent catalogue")
	}
	allowed, exists := policy.profiles[normalizedRole(role)]
	if !exists {
		return nil, fmt.Errorf("unknown agent role: %s", pytext.Repr(string(role)))
	}
	var selected []ToolDefinition
	for _, definition := range parent.ordered {
		if len(definition.capabilities) == 0 {
			continue
		}
		fits := true
		for _, capability := range definition.capabilities {
			if !allowed[capability] {
				fits = false
				break
			}
		}
		if fits {
			selected = append(selected, definition)
		}
	}
	return NewToolCatalog(selected...)
}
