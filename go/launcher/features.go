package launcher

import (
	"context"

	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/diagnostics"
)

func resolveDiagnostics(ctx context.Context, settings config.Settings, options Options) (Options, error) {
	if settings.EnableFeatures && options.DiagnosticsProvider == nil {
		selected, err := diagnostics.NewDefaultPythonSyntaxDiagnostics(ctx)
		if err != nil {
			return options, err
		}
		options.DiagnosticsProvider = selected
	}
	return options, nil
}

// Inspection is pure. Startup checks interpreter availability; an explicitly
// supplied provider replaces the default without probing that interpreter.
func supportedSettings(settings config.Settings, _ Options) config.Settings {
	return settings
}

func selectedFeatureOptions(settings config.Settings, options Options) Options {
	if settings.EnableFeatures {
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
