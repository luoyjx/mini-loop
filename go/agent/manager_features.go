package agent

import "errors"

// Full features select Source's twelve registry groups. Independent workflow,
// AST and decision options keep their own selection. A caller catalogue replaces
// the full registry; lifecycle defaults are bound separately for that case.
func normalizeManagerFeatures(config *ManagerConfig) error {
	if !config.EnableFeatures || config.Services.Tools != nil {
		return nil
	}
	services := &config.Services
	if services.DiagnosticsProvider == nil {
		return errors.New("full features require an explicit diagnostics provider; the default Python syntax parser is not implemented")
	}
	services.TaskTools = true
	services.BackgroundTools = true
	services.MemoryTools = true
	services.CronTools = true
	services.TeamTools = true
	services.WorktreeTools = true
	services.MCPTools = true
	services.PlanModeTools = true
	services.GoalTools = true
	services.SessionQueryTools = true
	services.SelfAuditTools = true
	return nil
}
