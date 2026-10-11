package launcher

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/luoyjx/mini-loop/go/audit"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/secrets"
)

// AuditDeployment constructs the local CLI profile, audits it and stops owned
// services. It never listens, restores sessions or starts model/tool execution.
// Source's local entry point selects full features but does not pass its workflow
// environment setting. Explicit SDK Options.WorkflowTools remains available.
// Injected backends retain caller ownership, as with NewWithOptions.
func AuditDeployment(ctx context.Context, settings config.Settings, environment map[string]string, options Options) (findings []audit.Finding, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	if err := supportedSettings(settings, options).RequireSupported(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(settings.Model) == "" {
		return nil, errors.New("MODEL_ID must not be empty for the Go runtime")
	}
	if environment == nil {
		environment = make(map[string]string)
		for name, value := range secrets.CurrentEnvironment() {
			environment[string(name)] = value
		}
	}
	// Source Settings eagerly creates this root before client construction. Load
	// remains pure elsewhere; this CLI constructor owns the corresponding effect.
	if err := os.MkdirAll(settings.WorkspaceRoot, 0755); err != nil {
		return nil, err
	}
	construction := settings
	construction.EnableWorkflows = false
	app, err := buildApp(ctx, construction, config.ServerSettings{ShutdownTimeout: 10 * time.Second}, nil, selectedFeatureOptions(construction, options))
	if err != nil {
		return nil, err
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if stopErr := app.Stop(stopCtx); stopErr != nil {
			findings = nil
			err = errors.Join(err, stopErr)
		}
	}()
	return audit.Manager(ctx, app.manager, settings, environment)
}
