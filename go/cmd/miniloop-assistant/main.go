// Command miniloop-assistant runs the opt-in proactive personal-assistant
// demo: a loopback daemon with durable SQLite state, mock mail/calendar
// connectors, routines, a proposal outbox and a simulated channel web page.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/assistant"
	"github.com/luoyjx/mini-loop/go/provider"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stderr))
}

func run(ctx context.Context, args []string, getenv func(string) string, stderr io.Writer) int {
	flags := flag.NewFlagSet("miniloop-assistant", flag.ContinueOnError)
	flags.SetOutput(stderr)
	addr := flags.String("addr", "127.0.0.1:8790", "loopback listen address for the simulated channel")
	data := flags.String("data", "var/assistant", "durable data directory (assistant.db, agent.db, memory/)")
	model := flags.String("model", "scripted", "scripted (offline, no credentials) or anthropic (reads ANTHROPIC_API_KEY / ANTHROPIC_AUTH_TOKEN / ANTHROPIC_BASE_URL)")
	modelID := flags.String("model-id", "", "model ID for -model anthropic (default MODEL_ID or "+agent.DefaultModel+")")
	workers := flags.Int("workers", 4, "maximum concurrent worker turns")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(stderr, "miniloop-assistant:", err); return 1 }
	if err := requireLoopback(*addr); err != nil {
		return fail(err)
	}
	config := assistant.Config{DataDir: *data, Log: stderr, Workers: *workers}
	switch *model {
	case "scripted":
		config.Provider, config.Model, config.ModelLabel = &assistant.ScriptedModel{}, assistant.ScriptedModelID, "scripted（离线脚本）"
	case "anthropic":
		key, token := getenv("ANTHROPIC_API_KEY"), getenv("ANTHROPIC_AUTH_TOKEN")
		if key == "" && token == "" {
			return fail(errors.New("-model anthropic needs ANTHROPIC_API_KEY or ANTHROPIC_AUTH_TOKEN"))
		}
		client, err := provider.New(provider.Config{APIKey: key, AuthToken: token, BaseURL: getenv("ANTHROPIC_BASE_URL")})
		if err != nil {
			return fail(err)
		}
		id := *modelID
		if id == "" {
			id = getenv("MODEL_ID")
		}
		if id == "" {
			id = agent.DefaultModel
		}
		config.Provider, config.Model = client, id
	default:
		return fail(fmt.Errorf("unknown -model %q", *model))
	}
	app, err := assistant.NewApp(ctx, config)
	if err != nil {
		return fail(err)
	}
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return fail(errors.Join(err, app.Close(context.Background())))
	}
	runCtx, cancel := context.WithCancel(ctx)
	app.Start(runCtx)
	server := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	fmt.Fprintf(stderr, "mini-loop assistant demo on http://%s (model=%s, data=%s)\n", listener.Addr(), app.ModelLabel, *data)
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
