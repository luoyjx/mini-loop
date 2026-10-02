package agent

import (
	"errors"
	"fmt"
	"strings"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

type AgentRole string

const (
	RoleExplore        AgentRole = "Explore"
	RoleWorker         AgentRole = "worker"
	RoleGeneralPurpose AgentRole = "general-purpose"
)

func normalizedRole(role AgentRole) AgentRole {
	return AgentRole(strings.ToLower(pytext.Strip(string(role))))
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

func NewCapabilityRoleToolPolicy(profiles []RoleCapabilityProfile) (*CapabilityRoleToolPolicy, error) {
	result := &CapabilityRoleToolPolicy{profiles: make(map[AgentRole]map[Capability]bool, len(profiles))}
	for _, profile := range profiles {
		key := normalizedRole(profile.Role)
		if key == "" {
			return nil, errors.New("role requires a name")
		}
		allowed := make(map[Capability]bool, len(profile.Capabilities))
		for _, capability := range profile.Capabilities {
			if capability == "" {
				return nil, errors.New("role capability requires a name")
			}
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
		return nil, fmt.Errorf("unknown agent role: %q", role)
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
