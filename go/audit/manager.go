package audit

import (
	"context"
	"errors"
	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/secrets"
)

// Manager performs the local fleet audit over an already constructed manager.
// The caller owns it and supplies its selected settings/environment. This API
// does not establish owner authority or alter the manager's lifecycle.
func Manager(ctx context.Context, manager *agent.SessionManager, settings config.Settings, environment map[string]string) ([]Finding, error) {
	if manager == nil {
		return nil, errors.New("deployment audit requires a manager")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if environment == nil {
		environment = make(map[string]string)
		for name, value := range secrets.CurrentEnvironment() {
			environment[string(name)] = value
		}
	}
	configuration, err := Settings(settings, environment)
	if err != nil {
		return nil, err
	}
	observation, err := manager.ObserveDeploymentAudit(ctx)
	if err != nil {
		return nil, err
	}
	credentialEnvironment := make(secrets.Environment, len(environment))
	for name, value := range environment {
		credentialEnvironment[secrets.Name(name)] = value
	}
	return ComposeLive(configuration, observation, credentialEnvironment)
}
