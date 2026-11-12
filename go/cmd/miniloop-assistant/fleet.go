package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/luoyjx/mini-loop/go/assistant"
)

const (
	defaultFleetAddr = "127.0.0.1:8791"
	// envFleetAdminToken holds the operator's token for /admin/. It is
	// required: a fleet without one could not be managed, and one with an
	// empty token would be open to anyone.
	envFleetAdminToken = "MINILOOP_FLEET_ADMIN_TOKEN"
	minAdminToken      = 24
)

// runFleet hosts many owners' assistants in one process. Each owner reaches
// /t/{id}/ with the token the admin API issued; an assistant runs only while
// its owner uses it or it has something due, and sleeps otherwise.
func runFleet(ctx context.Context, args []string, env environment, std streams) int {
	flags := flag.NewFlagSet("miniloop-assistant fleet", flag.ContinueOnError)
	flags.SetOutput(std.err)
	addr := flags.String("addr", defaultFleetAddr, "listen address; a non-loopback address needs -tls-cert/-tls-key or -behind-tls-proxy")
	data := flags.String("data", defaultFleetDir(env), "fleet data directory (fleet.db and tenants/)")
	model := flags.String("model", "scripted", "scripted (offline) or anthropic (reads ANTHROPIC_API_KEY / ANTHROPIC_AUTH_TOKEN / ANTHROPIC_BASE_URL); shared by every tenant")
	modelID := flags.String("model-id", "", "model ID for -model anthropic (default MODEL_ID or the agent default)")
	maxActive := flags.Int("max-active", 256, "assistants open at once; the rest sleep with their next due time in the registry")
	idle := flags.Duration("idle", 2*time.Minute, "how long an assistant stays open after its owner's last request")
	turns := flags.Int("turns", 8, "model turns running at once across all tenants")
	chatTurns := flags.Int("chat-turns", 0, "of -turns, slots kept for owners' chat (default a quarter)")
	tenantWorkers := flags.Int("tenant-workers", 2, "concurrent turns of one tenant")
	dailyCalls := flags.Int("daily-calls", 200, "model calls per tenant per day; 0 is unlimited")
	dormant := flags.Duration("dormant-after", 7*24*time.Hour, "stop waking a tenant whose owner has been away this long; 0 keeps waking")
	archive := flags.Duration("archive-after", 30*24*time.Hour, "compress the data of a tenant whose owner has been away this long; 0 never")
	sweep := flags.Duration("sweep", 0, "wake each sleeping tenant at least this often (for mail without push); 0 relies on its timers")
	tlsCert := flags.String("tls-cert", "", "TLS certificate file")
	tlsKey := flags.String("tls-key", "", "TLS private key file")
	behindProxy := flags.Bool("behind-tls-proxy", false, "serve plain HTTP on a non-loopback address because a proxy in front terminates TLS")
	if code, ok := parse(flags, args); !ok {
		return code
	}
	fail := failer(std.err)
	admin := env.getenv(envFleetAdminToken)
	if len(admin) < minAdminToken {
		return fail(fmt.Errorf("fleet needs %s of at least %d characters for its admin API", envFleetAdminToken, minAdminToken))
	}
	if (*tlsCert == "") != (*tlsKey == "") {
		return fail(errors.New("-tls-cert and -tls-key go together"))
	}
	secure := *tlsCert != ""
	if requireLoopback(*addr) != nil && !secure && !*behindProxy {
		return fail(fmt.Errorf("refusing plain HTTP on non-loopback %q: tenant tokens would cross the network in clear; give -tls-cert/-tls-key or -behind-tls-proxy", *addr))
	}
	root, err := filepath.Abs(*data)
	if err != nil {
		return fail(err)
	}
	config := assistant.FleetConfig{
		DataDir: root, Log: std.err, MaxActive: *maxActive, IdleTTL: *idle, Turns: *turns, ChatTurns: *chatTurns,
		TenantWorkers: *tenantWorkers, DailyCalls: *dailyCalls, DormantAfter: *dormant, ArchiveAfter: *archive, Sweep: *sweep,
	}
	if config.Provider, config.Model, config.ModelLabel, err = chooseModel(*model, *modelID, env); err != nil {
		return fail(err)
	}
	fleet, err := assistant.OpenFleet(ctx, config)
	if err != nil {
		return fail(err)
	}
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return fail(errors.Join(err, fleet.Close()))
	}
	fleet.Start()
	server := &http.Server{
		Handler: fleet.Handler(admin), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: time.Minute, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 64 << 10,
	}
	served := make(chan error, 1)
	scheme := "http"
	if secure {
		scheme = "https"
		go func() { served <- server.ServeTLS(listener, *tlsCert, *tlsKey) }()
	} else {
		go func() { served <- server.Serve(listener) }()
	}
	label := config.ModelLabel
	if label == "" {
		label = config.Model
	}
	fmt.Fprintf(std.err, "mini-loop assistant fleet on %s://%s (model=%s, data=%s, max-active=%d, turns=%d, daily-calls=%d)\n", scheme, listener.Addr(), label, root, *maxActive, *turns, *dailyCalls)
	select {
	case <-ctx.Done():
	case err = <-served:
	}
	shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()
	err = errors.Join(ignoreClosed(err), server.Shutdown(shutdown), fleet.Close())
	if err != nil {
		return fail(err)
	}
	return 0
}

func defaultFleetDir(env environment) string {
	if dir, err := env.configDir(); err == nil && dir != "" {
		return filepath.Join(dir, "mini-loop", "fleet")
	}
	return filepath.Join("var", "fleet")
}
