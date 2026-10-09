package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/httpapi"
	"github.com/luoyjx/mini-loop/go/internal/commandenv"
	"github.com/luoyjx/mini-loop/go/launcher"
	"github.com/luoyjx/mini-loop/go/reload"
)

func execute(ctx context.Context, args []string, env map[string]string, stdout, stderr io.Writer) int {
	return executePrepared(ctx, args, env, false, stdout, stderr)
}

func executePrepared(ctx context.Context, args []string, env map[string]string, discovery bool, stdout, stderr io.Writer) int {
	return executeWithListener(ctx, args, env, discovery, stdout, stderr, nil)
}

func executeWithListener(ctx context.Context, args []string, env map[string]string, discovery bool, stdout, stderr io.Writer, inherited *net.TCPListener) int {
	flags := flag.NewFlagSet("miniloop", flag.ContinueOnError)
	flags.SetOutput(stderr)
	sessionQueryTools := flags.Bool("session-query-tools", false, "enable the implemented Go read-only transcript search tool")
	mcpTools := flags.Bool("mcp-tools", false, "enable MCP connection tools (operator servers are configured through launcher.Options)")
	workflowTools := flags.Bool("workflow-tools", false, "enable the workflow service, model tools and owned HTTP workflow routes")
	teamTools := flags.Bool("team-tools", false, "enable team tools, round inbox delivery and owned teammate idle turns")
	memoryTools := flags.Bool("memory-tools", false, "enable the implemented Go remember and recall tools")
	memoryAuto := flags.Bool("memory-auto", true, "enable automatic memory selection/capture when both memory tools are installed")
	reloadModule := flags.String("reload-module", "", "explicit Go module directory for MINILOOP_RELOAD (default: cwd/ancestors)")
	dump := flags.Bool("dump-config", false, "print redacted settings and availability without starting a listener")
	selfAuditTools := flags.Bool("self-audit-tools", false, "enable the implemented Go read-only self-audit tool")
	goalTools := flags.Bool("goal-tools", false, "enable the implemented Go goal tools (arming requires explicit human authority)")
	planModeTools := flags.Bool("plan-mode-tools", false, "enable the implemented Go plan tools (headless approval)")
	cronTools := flags.Bool("cron-tools", false, "enable the implemented Go session cron tools")
	background := flags.Bool("background-tools", false, "enable the implemented Go background shell tools and session cleanup")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "miniloop accepts --dump-config, --session-query-tools, --mcp-tools, --workflow-tools, --team-tools, --memory-tools, --memory-auto, --background-tools, --cron-tools, --plan-mode-tools, --goal-tools, --self-audit-tools and --reload-module flags")
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	settings, err := config.Load(env, config.LoadOptions{})
	if err != nil {
		return fail(err)
	}
	server, err := config.LoadServer(env)
	if err != nil {
		return fail(err)
	}
	auth, err := httpapi.AuthFromEnvironment(env)
	if err != nil {
		return fail(err)
	}
	if inherited != nil {
		address := inherited.Addr().(*net.TCPAddr)
		server.Host, server.Port = address.IP.String(), address.Port
	}
	options := launcher.Options{SessionQueryTools: *sessionQueryTools, MCPTools: *mcpTools, WorkflowTools: *workflowTools, TeamTools: *teamTools, SelfAuditTools: *selfAuditTools, MemoryTools: *memoryTools, MemoryAuto: memoryAuto, GoalTools: *goalTools, PlanModeTools: *planModeTools, CronTools: *cronTools, BackgroundTools: *background}
	if *dump {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		report := launcher.InspectWithOptions(settings, server, auth, options)
		report.DotEnvDiscovery = discovery
		if err := encoder.Encode(report); err != nil {
			return fail(err)
		}
		return 0
	}
	if err := httpapi.RefuseOpenBind(server.Host, auth); err != nil {
		return fail(err)
	}
	if server.Reload && inherited == nil {
		if err := settings.RequireSupported(); err != nil {
			return fail(err)
		}
		if strings.TrimSpace(settings.Model) == "" {
			return fail(fmt.Errorf("MODEL_ID must not be empty for the Go runtime"))
		}
		if err := serveReload(ctx, args, env, server, *reloadModule, stdout, stderr); err != nil {
			return fail(err)
		}
		return 0
	}
	app, err := launcher.NewWithOptions(ctx, settings, server, auth, options)
	if err != nil {
		return fail(err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		app.Stop(stopCtx)
	}()
	var listener net.Listener = inherited
	if inherited == nil {
		listener, err = net.Listen("tcp", server.Address())
	}
	if err != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		app.Stop(stopCtx)
		return fail(err)
	}
	fmt.Fprintf(stderr, "mini-loop Go listening on %s (process-local session state; host shell)\n", listener.Addr())
	if err := app.Serve(ctx, listener); err != nil {
		return fail(err)
	}
	return 0
}

// runMain owns inherited descriptors even when startup or dispatch fails.
func runMain() int {
	var inherited *net.TCPListener
	if os.Getenv(reload.ListenerEnvironment) != "" {
		var err error
		inherited, err = reload.InheritedListener()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer inherited.Close()
	}
	// Capture process role before dotenv can overwrite it, and keep this private
	// descriptor marker out of the environment inherited by model/tool children.
	if err := os.Unsetenv(reload.ListenerEnvironment); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	env, err := commandenv.Initialize()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.Unsetenv(reload.ListenerEnvironment); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	values := env.Reveal()
	delete(values, reload.ListenerEnvironment)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return executeWithListener(ctx, os.Args[1:], values, env.DiscoveryEnabled, os.Stdout, os.Stderr, inherited)
}
func main() { os.Exit(runMain()) }
