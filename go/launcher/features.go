package launcher

import "github.com/luoyjx/mini-loop/go/config"

// Config alone has no diagnostics backend. An embedding caller can supply one;
// remove only that resolved startup limitation from a detached validation copy.
// The original settings still select the full Manager profile.
func supportedSettings(settings config.Settings, options Options) config.Settings {
	if options.DiagnosticsProvider != nil {
		settings.EnableFeatures = false
	}
	return settings
}

func selectedFeatureOptions(settings config.Settings, options Options) Options {
	if settings.EnableFeatures && options.DiagnosticsProvider != nil {
		options.SessionQueryTools = true
		options.MCPTools = true
		options.TeamTools = true
		options.MemoryTools = true
		options.SelfAuditTools = true
		options.GoalTools = true
		options.PlanModeTools = true
		options.CronTools = true
		options.BackgroundTools = true
	}
	return options
}
