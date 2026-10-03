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
	"syscall"
	"time"

	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/httpapi"
	"github.com/luoyjx/mini-loop/go/launcher"
)

func execute(ctx context.Context, args []string, env map[string]string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("miniloop", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dump := flags.Bool("dump-config", false, "print redacted settings and availability without starting a listener")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "miniloop accepts --dump-config or no arguments")
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
	if *dump {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(launcher.Inspect(settings, server, auth)); err != nil {
			return fail(err)
		}
		return 0
	}
	if err := httpapi.RefuseOpenBind(server.Host, auth); err != nil {
		return fail(err)
	}
	app, err := launcher.New(ctx, settings, server, auth)
	if err != nil {
		return fail(err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		app.Stop(stopCtx)
	}()
	listener, err := net.Listen("tcp", server.Address())
	if err != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		app.Stop(stopCtx)
		return fail(err)
	}
	fmt.Fprintf(stderr, "mini-loop Go listening on %s (process-local state; host shell; no trajectories)\n", listener.Addr())
	if err := app.Serve(ctx, listener); err != nil {
		return fail(err)
	}
	return 0
}
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	// Snapshot the environment once; settings/auth see the same input. No .env
	// discovery or mutation of process credentials occurs here.
	env := map[string]string{}
	for _, entry := range os.Environ() {
		for i := 0; i < len(entry); i++ {
			if entry[i] == '=' {
				env[entry[:i]] = entry[i+1:]
				break
			}
		}
	}
	os.Exit(execute(ctx, os.Args[1:], env, os.Stdout, os.Stderr))
}
