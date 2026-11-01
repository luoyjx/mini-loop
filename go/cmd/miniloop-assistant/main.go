// Command miniloop-assistant runs the opt-in proactive personal-assistant
// demo on the local machine: a loopback daemon with durable SQLite state, a
// simulated mail/calendar MCP server in a child process, routines, a proposal
// outbox and a simulated channel web page.
//
//	miniloop-assistant [flags]                run the daemon (spawns mock-mcp)
//	miniloop-assistant mock-mcp -data DIR     the simulated MCP server on stdio
//	miniloop-assistant launchd-plist [flags]  print a macOS LaunchAgent plist
package main

import (
	"context"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/assistant"
	"github.com/luoyjx/mini-loop/go/provider"
)

const (
	defaultAddr  = "127.0.0.1:8790"
	defaultLabel = "dev.mini-loop.assistant"
	worldDirName = "mock-mcp"
)

// streams are the process's standard files; tests substitute them.
type streams struct {
	in  io.Reader
	out io.Writer
	err io.Writer
}

// environment is what run needs from the process beyond its arguments.
type environment struct {
	getenv     func(string) string
	executable func() (string, error)
	configDir  func() (string, error)
	homeDir    func() (string, error)
}

func osEnvironment() environment {
	return environment{getenv: os.Getenv, executable: os.Executable, configDir: os.UserConfigDir, homeDir: os.UserHomeDir}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], osEnvironment(), streams{os.Stdin, os.Stdout, os.Stderr}))
}

func run(ctx context.Context, args []string, env environment, std streams) int {
	if len(args) > 0 {
		switch args[0] {
		case "mock-mcp":
			return runMockMCP(ctx, args[1:], std)
		case "launchd-plist":
			return runPlist(args[1:], env, std)
		}
	}
	return runDaemon(ctx, args, env, std)
}

func failer(stderr io.Writer) func(error) int {
	return func(err error) int { fmt.Fprintln(stderr, "miniloop-assistant:", err); return 1 }
}

// defaultDataDir keeps state in the per-user application-support directory
// (~/Library/Application Support/mini-loop/assistant on macOS), so the daemon
// works from any working directory, including launchd's "/".
func defaultDataDir(env environment) string {
	if dir, err := env.configDir(); err == nil && dir != "" {
		return filepath.Join(dir, "mini-loop", "assistant")
	}
	return filepath.Join("var", "assistant")
}

type daemonFlags struct {
	addr, data, model, modelID *string
	workers                    *int
}

func bindDaemonFlags(flags *flag.FlagSet, env environment) daemonFlags {
	return daemonFlags{
		addr:    flags.String("addr", defaultAddr, "loopback listen address for the simulated channel"),
		data:    flags.String("data", defaultDataDir(env), "durable data directory (assistant.db, agent.db, memory/, mock-mcp/world.db)"),
		model:   flags.String("model", "scripted", "scripted (offline, no credentials) or anthropic (reads ANTHROPIC_API_KEY / ANTHROPIC_AUTH_TOKEN / ANTHROPIC_BASE_URL)"),
		modelID: flags.String("model-id", "", "model ID for -model anthropic (default MODEL_ID or "+agent.DefaultModel+")"),
		workers: flags.Int("workers", 4, "maximum concurrent worker turns"),
	}
}

func parse(flags *flag.FlagSet, args []string) (int, bool) {
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, false
		}
		return 2, false
	}
	return 0, true
}

func runDaemon(ctx context.Context, args []string, env environment, std streams) int {
	flags := flag.NewFlagSet("miniloop-assistant", flag.ContinueOnError)
	flags.SetOutput(std.err)
	options := bindDaemonFlags(flags, env)
	if code, ok := parse(flags, args); !ok {
		return code
	}
	fail := failer(std.err)
	if err := requireLoopback(*options.addr); err != nil {
		return fail(err)
	}
	data, err := filepath.Abs(*options.data)
	if err != nil {
		return fail(err)
	}
	config := assistant.Config{DataDir: data, Log: std.err, Workers: *options.workers}
	switch *options.model {
	case "scripted":
		config.Provider, config.Model, config.ModelLabel = &assistant.ScriptedModel{}, assistant.ScriptedModelID, "scripted（离线脚本）"
	case "anthropic":
		key, token := env.getenv("ANTHROPIC_API_KEY"), env.getenv("ANTHROPIC_AUTH_TOKEN")
		if key == "" && token == "" {
			return fail(errors.New("-model anthropic needs ANTHROPIC_API_KEY or ANTHROPIC_AUTH_TOKEN"))
		}
		client, err := provider.New(provider.Config{APIKey: key, AuthToken: token, BaseURL: env.getenv("ANTHROPIC_BASE_URL")})
		if err != nil {
			return fail(err)
		}
		id := *options.modelID
		if id == "" {
			id = env.getenv("MODEL_ID")
		}
		if id == "" {
			id = agent.DefaultModel
		}
		config.Provider, config.Model = client, id
	default:
		return fail(fmt.Errorf("unknown -model %q", *options.model))
	}
	// The mock MCP server is this same binary in a child process with its own
	// database; the daemon talks to it only over stdio JSON-RPC.
	self, err := env.executable()
	if err != nil {
		return fail(err)
	}
	connector, err := assistant.NewStdioConnector([]string{self, "mock-mcp", "-data", filepath.Join(data, worldDirName)})
	if err != nil {
		return fail(err)
	}
	tools, err := connector.Verify(ctx)
	if err != nil {
		return fail(errors.Join(err, connector.Close()))
	}
	config.Connector = connector
	app, err := assistant.NewApp(ctx, config)
	if err != nil {
		return fail(err)
	}
	listener, err := net.Listen("tcp", *options.addr)
	if err != nil {
		return fail(errors.Join(err, app.Close(context.Background())))
	}
	runCtx, cancel := context.WithCancel(ctx)
	app.Start(runCtx)
	server := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	fmt.Fprintf(std.err, "mini-loop assistant demo on http://%s (model=%s, data=%s, mcp=%s over %s, %d tools)\n", listener.Addr(), app.ModelLabel, data, connector.Server(), connector.Transport(), tools)
	select {
	case <-ctx.Done():
	case err = <-served:
	}
	cancel()
	shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()
	err = errors.Join(ignoreClosed(err), server.Shutdown(shutdown), app.Close(shutdown))
	if err != nil {
		return fail(err)
	}
	return 0
}

// runMockMCP serves the simulated mail/calendar World on stdin/stdout until
// the daemon closes stdin or the process is signalled.
func runMockMCP(ctx context.Context, args []string, std streams) int {
	flags := flag.NewFlagSet("miniloop-assistant mock-mcp", flag.ContinueOnError)
	flags.SetOutput(std.err)
	data := flags.String("data", "", "directory for world.db (required)")
	if code, ok := parse(flags, args); !ok {
		return code
	}
	fail := failer(std.err)
	if *data == "" {
		return fail(errors.New("mock-mcp needs -data"))
	}
	world, err := assistant.OpenWorld(ctx, filepath.Join(*data, "world.db"), time.Now)
	if err != nil {
		return fail(err)
	}
	served := make(chan error, 1)
	go func() { served <- assistant.ServeMCP(ctx, world, std.in, std.out) }()
	select {
	case err = <-served:
	case <-ctx.Done():
	}
	if err = errors.Join(err, world.Close()); err != nil && !errors.Is(err, context.Canceled) {
		return fail(err)
	}
	return 0
}

// runPlist prints a per-user LaunchAgent that keeps the daemon running and
// restarts it after a crash. It writes nothing itself; the README shows how to
// install and remove it with launchctl.
func runPlist(args []string, env environment, std streams) int {
	flags := flag.NewFlagSet("miniloop-assistant launchd-plist", flag.ContinueOnError)
	flags.SetOutput(std.err)
	options := bindDaemonFlags(flags, env)
	label := flags.String("label", defaultLabel, "launchd label")
	binary := flags.String("binary", "", "installed miniloop-assistant path (default: this executable)")
	logPath := flags.String("log", "", "log file (default ~/Library/Logs/mini-loop-assistant.log)")
	if code, ok := parse(flags, args); !ok {
		return code
	}
	fail := failer(std.err)
	if err := requireLoopback(*options.addr); err != nil {
		return fail(err)
	}
	if *options.model != "scripted" {
		// launchd does not read shell profiles, and a plist is no place for an
		// API key; a keychain-backed secret source is future work.
		return fail(errors.New("launchd-plist supports -model scripted only; run -model anthropic from a shell"))
	}
	program := *binary
	if program == "" {
		self, err := env.executable()
		if err != nil {
			return fail(err)
		}
		program = self
	}
	if strings.Contains(program, string(filepath.Separator)+"go-build") {
		return fail(fmt.Errorf("%s is a temporary go run binary; build one first: go build -o ~/.local/bin/miniloop-assistant ./cmd/miniloop-assistant", program))
	}
	data, err := filepath.Abs(*options.data)
	if err != nil {
		return fail(err)
	}
	program, err = filepath.Abs(program)
	if err != nil {
		return fail(err)
	}
	logFile := *logPath
	if logFile == "" {
		home, err := env.homeDir()
		if err != nil {
			return fail(err)
		}
		logFile = filepath.Join(home, "Library", "Logs", "mini-loop-assistant.log")
	}
	arguments := []string{program, "-addr", *options.addr, "-data", data, "-workers", fmt.Sprint(*options.workers)}
	if _, err := std.out.Write(launchAgentPlist(*label, arguments, logFile)); err != nil {
		return fail(err)
	}
	return 0
}

func xmlText(text string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(text))
	return b.String()
}

func launchAgentPlist(label string, arguments []string, logFile string) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + xmlText(label) + `</string>
	<key>ProgramArguments</key>
	<array>
`)
	for _, argument := range arguments {
		b.WriteString("\t\t<string>" + xmlText(argument) + "</string>\n")
	}
	b.WriteString(`	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>ThrottleInterval</key>
	<integer>10</integer>
	<key>StandardOutPath</key>
	<string>` + xmlText(logFile) + `</string>
	<key>StandardErrorPath</key>
	<string>` + xmlText(logFile) + `</string>
</dict>
</plist>
`)
	return []byte(b.String())
}

func ignoreClosed(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("refusing non-loopback address %q: the demo channel has no authentication", addr)
}
