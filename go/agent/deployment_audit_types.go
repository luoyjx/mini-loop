package agent

import (
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/selfaudit"
)

type DeploymentToolObservation struct {
	Name         protocol.ToolName `json:"name"`
	Risk         ToolRisk          `json:"risk"`
	ParallelSafe bool              `json:"parallel_safe"`
}

type DeploymentCatalogObservation struct {
	Tools []DeploymentToolObservation `json:"tools"`
}
type DeploymentSubsystemName string

type DeploymentProblemChannel struct {
	Name   DeploymentSubsystemName `json:"name"`
	Ledger selfaudit.Ledger        `json:"ledger"`
}

// DeploymentAuditObservation is inert metadata from a trusted local observer. It grants no
// authority and does not itself prove that a backend enforces its policy.
// Nil Catalog means Source's default shell catalogue, distinct from an empty one.
type DeploymentAuditObservation struct {
	Catalog          *DeploymentCatalogObservation `json:"catalog"`
	SandboxNull      bool                          `json:"sandbox_null"`
	SandboxReason    *string                       `json:"sandbox_reason"`
	SecretsNull      bool                          `json:"secrets_null"`
	SecretNames      []secrets.Name                `json:"secret_names"`
	Unresolved       []secrets.Name                `json:"unresolved"`
	ShortValues      []secrets.Name                `json:"short_values"`
	PersistenceError *string                       `json:"persistence_error"`
	MCP              selfaudit.Ledger              `json:"mcp"`
	Cron             selfaudit.Ledger              `json:"cron"`
	Skills           selfaudit.Ledger              `json:"skills"`
	Channels         []DeploymentProblemChannel    `json:"channels"`
	StateNull        bool                          `json:"state_null"`
	JournalMemory    bool                          `json:"journal_memory"`
	Workflows        bool                          `json:"workflows"`
}
