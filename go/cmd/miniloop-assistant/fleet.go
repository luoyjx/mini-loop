package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/luoyjx/mini-loop/go/assistant"
)

const (
	defaultFleetAddr = "127.0.0.1:8791"
	// envFleetAdminToken holds the operator's token for /admin/. It is
	// required: a fleet without one could not be managed, and one with an
	// empty token would be open to anyone.
	envFleetAdminToken = "MINILOOP_FLEET_ADMIN_TOKEN"
	// envFleetPushToken, when set, opens POST /push/{id} for the mail and
	// calendar provider's push bridge. It can only wake tenants.
	envFleetPushToken = "MINILOOP_FLEET_PUSH_TOKEN"
	// envFleetDatabase, when set, is the URL of the PostgreSQL database that
	// holds the registry and every tenant's data, such as
	// postgres://user:password@host:5432/name. It carries a password, so it
	// is read from the environment and never printed.
	envFleetDatabase = "MINILOOP_FLEET_DATABASE_URL"
	// envFleetShards, when set, names more PostgreSQL databases for the
	// tenants' data, as name=URL pairs separated by white space:
	// "s1=postgres://... s2=postgres://...". The URLs carry passwords.
	envFleetShards = "MINILOOP_FLEET_SHARD_URLS"
	// The keys that sign requests to an S3 archive store (-archive-store),
	// and the session token of temporary ones.
	envFleetArchiveKeyID   = "MINILOOP_FLEET_ARCHIVE_ACCESS_KEY_ID"
	envFleetArchiveSecret  = "MINILOOP_FLEET_ARCHIVE_SECRET_ACCESS_KEY"
	envFleetArchiveSession = "MINILOOP_FLEET_ARCHIVE_SESSION_TOKEN"
	minAdminToken          = 24
)

// parseShards reads envFleetShards. It never echoes a URL.
func parseShards(value string) (map[string]string, error) {
	shards := map[string]string{}
	for _, pair := range strings.Fields(value) {
		name, address, ok := strings.Cut(pair, "=")
		if !ok || name == "" || address == "" {
			return nil, fmt.Errorf("%s must hold name=URL pairs separated by white space", envFleetShards)
		}
		if _, twice := shards[name]; twice {
			return nil, fmt.Errorf("%s names shard %q twice", envFleetShards, name)
		}
		shards[name] = address
	}
	return shards, nil
}

// runFleet hosts many owners' assistants in one process. Each owner reaches
// /t/{id}/ with the token the admin API issued; an assistant runs only while
// its owner uses it or it has something due, and sleeps otherwise.
func runFleet(ctx context.Context, args []string, env environment, std streams) int {
	flags := flag.NewFlagSet("miniloop-assistant fleet", flag.ContinueOnError)
	flags.SetOutput(std.err)
	addr := flags.String("addr", defaultFleetAddr, "listen address; a non-loopback address needs -tls-cert/-tls-key or -behind-tls-proxy")
	data := flags.String("data", defaultFleetDir(env), "fleet data directory (fleet.db and tenants/); with "+envFleetDatabase+" only this node's cache")
	model := flags.String("model", "scripted", "scripted (offline) or anthropic (reads ANTHROPIC_API_KEY / ANTHROPIC_AUTH_TOKEN / ANTHROPIC_BASE_URL); shared by every tenant")
	modelID := flags.String("model-id", "", "model ID for -model anthropic (default MODEL_ID or the agent default)")
	maxActive := flags.Int("max-active", 256, "assistants open at once; the rest sleep with their next due time in the registry")
	idle := flags.Duration("idle", 2*time.Minute, "how long an assistant stays open after its owner's last request")
	turns := flags.Int("turns", 8, "model turns running at once across all tenants")
	chatTurns := flags.Int("chat-turns", 0, "of -turns, slots kept for owners' chat (default a quarter)")
	tenantWorkers := flags.Int("tenant-workers", 2, "concurrent turns of one tenant")
	dailyCalls := flags.Int("daily-calls", 200, "model calls per tenant per day; 0 is unlimited")
	dormant := flags.Duration("dormant-after", 7*24*time.Hour, "stop waking a tenant whose owner has been away this long; 0 keeps waking")
	archive := flags.Duration("archive-after", 30*24*time.Hour, "move the data of a tenant whose owner has been away this long to the cheapest place: a compressed file, or with "+envFleetDatabase+" the -archive-store; 0 never")
	archiveStore := flags.String("archive-store", "", "with "+envFleetDatabase+", where archived tenants go: file:///directory, https://host/bucket/prefix of an S3-compatible store, or s3://bucket/prefix (keys in "+envFleetArchiveKeyID+" and "+envFleetArchiveSecret+")")
	archiveRegion := flags.String("archive-region", "us-east-1", "the region an S3 -archive-store signs for")
	largeKB := flags.Int64("large-file-kb", 1024, "with -archive-store, keep the bulk of a tenant's file of this many KiB or more as one object in the store and only its changed pages in the database; 0 keeps every file in the database")
	cacheAfter := flags.Duration("cache-after", 0, "with "+envFleetDatabase+", drop this node's copy of a tenant it has not opened for this long (default -archive-after)")
	cacheMB := flags.Int64("cache-max-mb", 0, "with "+envFleetDatabase+", drop the copies opened longest ago once they take more than this many MiB; 0 is no bound")
	usageRetention := flags.Duration("usage-retention", 0, "drop per-day usage rows older than this; 0 keeps them")
	databaseConns := flags.Int("database-conns", 8, "connections this node keeps to each PostgreSQL database; all nodes together must fit its max_connections")
	registryGrace := flags.Duration("registry-grace", 0, "with "+envFleetDatabase+", how long a node that cannot reach the registry keeps its tenants open (default 12 times -node-ttl)")
	janitorTick := flags.Duration("janitor-tick", 10*time.Minute, "how often dormant tenants are retired, absent ones archived, leftover copies removed and this node's cache trimmed")
	leaveWait := flags.Duration("leave-wait", 0, "on shutdown, answer /readyz with 503 for this long before closing, so a load balancer stops sending requests first")
	sweep := flags.Duration("sweep", 0, "wake each sleeping tenant at least this often (for mail without push); 0 relies on its timers")
	advertise := flags.String("advertise", "", "this node's base URL as the other nodes reach it, such as http://10.0.0.3:8791; setting it lets several fleet processes share -data and pass requests to each other")
	nodeTTL := flags.Duration("node-ttl", 10*time.Second, "with -advertise, how long a node may miss its heartbeat before the others take its tenants")
	checkpoint := flags.Duration("checkpoint", 2*time.Second, "with "+envFleetDatabase+", how often an open tenant's changes are copied to the database; a node that is lost loses at most this much of a running turn")
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
	push := env.getenv(envFleetPushToken)
	switch {
	case push != "" && len(push) < minAdminToken:
		return fail(fmt.Errorf("%s must have at least %d characters", envFleetPushToken, minAdminToken))
	case push != "" && push == admin:
		return fail(fmt.Errorf("%s must differ from %s", envFleetPushToken, envFleetAdminToken))
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
		Advertise: strings.TrimSuffix(*advertise, "/"), NodeTTL: *nodeTTL,
		Database: env.getenv(envFleetDatabase), Checkpoint: *checkpoint,
		JanitorTick: *janitorTick, CacheAfter: *cacheAfter, CacheBytes: *cacheMB << 20, UsageRetention: *usageRetention, DatabaseConns: *databaseConns, RegistryGrace: *registryGrace,
		LargeFile: *largeKB << 10,
		Archive: assistant.ArchiveStore{
			URL: *archiveStore, Region: *archiveRegion, AccessKeyID: env.getenv(envFleetArchiveKeyID),
			SecretAccessKey: env.getenv(envFleetArchiveSecret), SessionToken: env.getenv(envFleetArchiveSession),
		},
	}
	if config.Shards, err = parseShards(env.getenv(envFleetShards)); err != nil {
		return fail(err)
	}
	if config.Database == "" && (len(config.Shards) > 0 || config.Archive.URL != "") {
		return fail(fmt.Errorf("%s and -archive-store need %s", envFleetShards, envFleetDatabase))
	}
	if *largeKB < 0 || *largeKB > 1<<30 {
		return fail(errors.New("-large-file-kb is 0 (off) or a size in KiB up to 1 TiB"))
	}
	store := "data directory"
	if config.Database != "" {
		large := "off"
		if config.Archive.URL != "" && config.LargeFile > 0 {
			large = fmt.Sprintf("%d KiB", *largeKB)
		}
		store = fmt.Sprintf("postgres (%d shards, archive store=%t, large files in the store from %s)", len(config.Shards), config.Archive.URL != "", large)
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
		Handler: fleet.Handler(admin, push), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
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
	fmt.Fprintf(std.err, "mini-loop assistant fleet on %s://%s (model=%s, data=%s, tenants in=%s, max-active=%d, turns=%d, daily-calls=%d, push=%t, advertise=%q)\n", scheme, listener.Addr(), label, root, store, *maxActive, *turns, *dailyCalls, push != "", config.Advertise)
	select {
	case <-ctx.Done():
		// Tell the load balancer first, and close once it has stopped
		// sending requests here.
		fleet.Leave()
		select {
		case <-time.After(*leaveWait):
		case err = <-served:
		}
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
