// Package launcher owns process-local composition, listening and shutdown.
package launcher

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/httpapi"
	"github.com/luoyjx/mini-loop/go/provider"
	"github.com/luoyjx/mini-loop/go/shell"
	"github.com/luoyjx/mini-loop/go/skills"
	"github.com/luoyjx/mini-loop/go/spill"
	"github.com/luoyjx/mini-loop/go/trajectory"
)

type BuildIdentity struct {
	GoVersion string `json:"go_version"`
	Revision  string `json:"revision"`
	Modified  bool   `json:"modified"`
}

func CurrentBuild() BuildIdentity {
	result := BuildIdentity{Revision: "development"}
	if info, ok := debug.ReadBuildInfo(); ok {
		result.GoVersion = info.GoVersion
		for _, entry := range info.Settings {
			switch entry.Key {
			case "vcs.revision":
				result.Revision = entry.Value
			case "vcs.modified":
				result.Modified = entry.Value == "true"
			}
		}
	}
	return result
}

type ProviderStatus struct {
	Name       string `json:"name"`
	Endpoint   string `json:"endpoint"`
	Credential string `json:"credential"`
}
type Report struct {
	BackgroundTools bool                        `json:"background_tools"`
	Kind            string                      `json:"kind"`
	Settings        config.Snapshot             `json:"settings"`
	Server          config.ServerSettings       `json:"server"`
	Provider        ProviderStatus              `json:"provider"`
	Authenticated   bool                        `json:"authenticated"`
	Build           BuildIdentity               `json:"build"`
	Unsupported     []config.UnsupportedSetting `json:"unsupported"`
	StateStore      string                      `json:"state_store"`
	Sandbox         string                      `json:"sandbox"`
	DotEnvDiscovery bool                        `json:"dotenv_discovery"`
}

func Inspect(settings config.Settings, server config.ServerSettings, auth httpapi.Authenticator) Report {
	return InspectWithOptions(settings, server, auth, Options{})
}

// Options selects individual implemented Go services. The comprehensive Python
// MINILOOP_FEATURES setting remains unsupported until its complete bundle exists.
type Options struct {
	BackgroundTools bool
}

func InspectWithOptions(settings config.Settings, server config.ServerSettings, auth httpapi.Authenticator, options Options) Report {
	endpoint := provider.DefaultEndpoint
	if settings.BaseURL != nil {
		endpoint = *settings.BaseURL
	}
	endpoint = config.RedactEndpoint(endpoint)
	name := "anthropic"
	if settings.BaseURL != nil {
		name = "anthropic-compatible"
	}
	if settings.FakeLLM {
		name, endpoint = "fake", ""
	}
	snapshot := settings.Snapshot()
	return Report{BackgroundTools: options.BackgroundTools, Kind: "settings-and-availability", Settings: snapshot, Server: server, Provider: ProviderStatus{name, endpoint, settings.APIKey.String()}, Authenticated: auth != nil && auth.Configured(), Build: CurrentBuild(), Unsupported: settings.Unsupported(), StateStore: "process-local", Sandbox: "none", DotEnvDiscovery: false}
}

type boundBashFactory struct{ timeout time.Duration }

func (f boundBashFactory) BashFor(ctx context.Context, binding agent.SessionBinding) (agent.BashExecutor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return shell.New(shell.Config{Workspace: binding.Workspace, Timeout: f.timeout})
}

type App struct {
	spill          spill.Store
	manager        *agent.SessionManager
	handler        *httpapi.Server
	transport      *http.Transport
	shutdown       time.Duration
	mu             sync.Mutex
	served         bool
	authConfigured bool
}

func New(ctx context.Context, settings config.Settings, server config.ServerSettings, auth httpapi.Authenticator) (*App, error) {
	return NewWithOptions(ctx, settings, server, auth, Options{})
}

func NewWithOptions(ctx context.Context, settings config.Settings, server config.ServerSettings, auth httpapi.Authenticator, options Options) (*App, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(server.Host) == "" || server.Port < 0 || server.Port > 65535 {
		return nil, errors.New("invalid listener host/port")
	}
	if err := settings.RequireSupported(); err != nil {
		return nil, err
	}
	if err := httpapi.RefuseOpenBind(server.Host, auth); err != nil {
		return nil, err
	}
	if strings.TrimSpace(settings.Model) == "" {
		return nil, errors.New("MODEL_ID must not be empty for the Go runtime")
	}
	if server.ShutdownTimeout <= 0 {
		server.ShutdownTimeout = 10 * time.Second
	}
	var model agent.Provider
	var transport *http.Transport
	if settings.FakeLLM {
		model = agent.NewFakeProvider(agent.FakeProviderConfig{Delay: server.FakeDelay})
	} else {
		transport = &http.Transport{}
		if base, ok := http.DefaultTransport.(*http.Transport); ok {
			transport = base.Clone()
		}
		endpoint := ""
		if settings.BaseURL != nil {
			endpoint = *settings.BaseURL
		}
		client, err := provider.New(provider.Config{APIKey: settings.APIKey.Reveal(), BaseURL: endpoint, HTTPClient: &http.Client{Transport: transport}})
		if err != nil {
			transport.CloseIdleConnections()
			return nil, err
		}
		model = client
	}
	var preservation spill.Store
	if settings.SpillDir != nil {
		// Python manager construction treats an unavailable root as best-effort.
		// Do not keep a typed-nil store in the interface.
		if local, err := spill.NewLocalStore(*settings.SpillDir); err == nil {
			preservation = local
		}
	}
	var catalog *skills.Catalog
	var err error
	if settings.SkillsDir == config.BuiltinSkills {
		catalog, err = skills.NewBuiltinCatalog(ctx)
	} else {
		catalog, err = skills.NewCatalog(ctx, settings.SkillsDir)
	}
	if err != nil {
		if transport != nil {
			transport.CloseIdleConnections()
		}
		return nil, err
	}
	fallback := ""
	if settings.FallbackModel != nil {
		fallback = *settings.FallbackModel
	}
	recovery, err := agent.NewDefaultRecovery(agent.RecoveryConfig{FallbackModel: fallback})
	if err != nil {
		if transport != nil {
			transport.CloseIdleConnections()
		}
		return nil, err
	}
	var trajectories agent.TrajectoryStore
	if settings.TrajectoryEnabled {
		root := filepath.Join(settings.WorkspaceRoot, ".trajectories")
		if settings.TrajectoryRoot != nil {
			root = *settings.TrajectoryRoot
		}
		local, err := trajectory.New(trajectory.Config{Root: root, CaptureContent: settings.TrajectoryCaptureContent})
		if err != nil {
			if transport != nil {
				transport.CloseIdleConnections()
			}
			return nil, err
		}
		trajectories = local
	}
	build := CurrentBuild()
	label := build.Revision
	if build.Modified {
		label += "-modified"
	}
	manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: settings.WorkspaceRoot, BindableRoots: settings.BindableRoots,
		ModelConcurrency: agent.ConcurrencyLimit(settings.MaxConcurrentLLM), ToolConcurrency: agent.ConcurrencyLimit(settings.MaxConcurrentTools), ApprovalTimeout: settings.ApprovalTimeout.Duration(),
		Defaults: agent.SessionDefaults{Model: settings.Model, PermissionMode: agent.ModeInteractive, MaxRounds: settings.MaxTurns, MaxTokens: settings.MaxTokens, TokenThreshold: settings.TokenThreshold, SubagentMaxDepth: settings.SubagentMaxDepth, SubagentMaxRounds: settings.SubagentMaxRounds},
		Services: agent.ManagerServices{BackgroundTools: options.BackgroundTools, Trajectories: trajectories, Build: label, Spill: preservation, Provider: model, Recovery: recovery, Skills: catalog, BashFactory: boundBashFactory{timeout: time.Duration(settings.BashTimeout) * time.Second}}})
	if err != nil {
		if transport != nil {
			transport.CloseIdleConnections()
		}
		return nil, err
	}
	handler, err := httpapi.New(httpapi.Config{Manager: manager, Auth: auth, RateLimitPerMinute: settings.RateLimitPerMinute, FakeLLM: settings.FakeLLM, Build: label})
	if err != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), server.ShutdownTimeout)
		defer cancel()
		manager.Stop(stopCtx)
		if transport != nil {
			transport.CloseIdleConnections()
		}
		return nil, err
	}
	return &App{spill: preservation, manager: manager, handler: handler, transport: transport, shutdown: server.ShutdownTimeout, authConfigured: auth != nil && auth.Configured()}, nil
}
func (a *App) Handler() http.Handler { return a.handler }
func (a *App) Stop(ctx context.Context) error {
	err := a.manager.Stop(ctx)
	if a.transport != nil {
		a.transport.CloseIdleConnections()
	}
	return err
}

// Serve transfers listener ownership. Parent cancellation cancels HTTP request
// contexts and manager-owned background turns, then joins server/manager shutdown.
// No WriteTimeout is set: it would cut off long model calls and SSE streams.
func (a *App) Serve(ctx context.Context, listener net.Listener) error {
	defer listener.Close()
	host, _, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return errors.New("launcher requires a TCP listener")
	}
	if !a.authConfigured {
		if err := httpapi.RefuseOpenBind(host, httpapi.NullAuth{}); err != nil {
			return err
		}
	}
	a.mu.Lock()
	if a.served {
		a.mu.Unlock()
		return errors.New("launcher can serve only once")
	}
	a.served = true
	a.mu.Unlock()
	if err := a.manager.Start(); err != nil {
		return fmt.Errorf("manager startup: %w", err)
	}
	server := &http.Server{Handler: a.handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20, BaseContext: func(net.Listener) context.Context { return ctx }}
	finished := make(chan error, 1)
	go func() { finished <- server.Serve(listener) }()
	var serveErr error
	ended := false
	select {
	case serveErr = <-finished:
		ended = true
	case <-ctx.Done():
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), a.shutdown)
	defer cancel()
	shutdown := make(chan error, 1)
	go func() { shutdown <- server.Shutdown(stopCtx) }()
	managerErr := a.Stop(stopCtx)
	shutdownErr := <-shutdown
	if shutdownErr != nil {
		server.Close()
	}
	if !ended {
		serveErr = <-finished
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	if managerErr != nil {
		return fmt.Errorf("manager shutdown: %w", managerErr)
	}
	if shutdownErr != nil {
		return fmt.Errorf("HTTP shutdown: %w", shutdownErr)
	}
	return serveErr
}
