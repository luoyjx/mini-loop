package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMain lets the daemon under test spawn this test binary as its mock MCP
// child: "<test binary> mock-mcp -data DIR" serves stdio instead of testing.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "mock-mcp" {
		os.Exit(run(context.Background(), os.Args[1:], osEnvironment(), streams{os.Stdin, os.Stdout, os.Stderr}))
	}
	os.Exit(m.Run())
}

func testEnvironment(t *testing.T) environment {
	config, home := t.TempDir(), t.TempDir()
	return environment{
		getenv:     func(string) string { return "" },
		executable: os.Executable,
		configDir:  func() (string, error) { return config, nil },
		homeDir:    func() (string, error) { return home, nil },
	}
}

func TestRequireLoopback(t *testing.T) {
	for addr, ok := range map[string]bool{"127.0.0.1:8790": true, "localhost:0": true, "[::1]:8790": true, "0.0.0.0:8790": false, "192.168.1.2:8790": false, ":8790": false} {
		if err := requireLoopback(addr); (err == nil) != ok {
			t.Errorf("%s: err=%v, want ok=%v", addr, err, ok)
		}
	}
}

func TestDefaultDataDirUsesUserConfigDir(t *testing.T) {
	env := testEnvironment(t)
	config, _ := env.configDir()
	if got := defaultDataDir(env); got != filepath.Join(config, "mini-loop", "assistant") {
		t.Fatalf("default data dir %q", got)
	}
	env.configDir = func() (string, error) { return "", errors.New("no config dir") }
	if got := defaultDataDir(env); got != filepath.Join("var", "assistant") {
		t.Fatalf("fallback data dir %q", got)
	}
}

func TestRunRefusesUnsafeStartup(t *testing.T) {
	cases := map[string][]string{
		"non-loopback bind":        {"-addr", "0.0.0.0:8790"},
		"anthropic without secret": {"-addr", "127.0.0.1:0", "-model", "anthropic", "-data", t.TempDir()},
		"unknown model":            {"-addr", "127.0.0.1:0", "-model", "gpt", "-data", t.TempDir()},
		"mock-mcp without data":    {"mock-mcp"},
		"telegram without token":   {"-addr", "127.0.0.1:0", "-data", t.TempDir(), "-telegram-chat", "42"},
		"discord without token":    {"-addr", "127.0.0.1:0", "-data", t.TempDir(), "-discord-user", "1001"},
		"email without password": {"-addr", "127.0.0.1:0", "-data", t.TempDir(), "-email-owner", "me@owner.example", "-email-user", "bot@bot.example",
			"-email-imap", "imap.bot.example:993", "-email-smtp", "smtp.bot.example:587", "-email-authserv", "mx.bot.example"},
	}
	for name, args := range cases {
		var stderr bytes.Buffer
		if code := run(context.Background(), args, testEnvironment(t), streams{strings.NewReader(""), io.Discard, &stderr}); code != 1 || !strings.Contains(stderr.String(), "miniloop-assistant:") {
			t.Errorf("%s: code=%d stderr=%q", name, code, stderr.String())
		}
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// The daemon spawns the mock MCP server as a real child process, verifies its
// tool contract, serves, and shuts both down on cancellation.
func TestRunServesUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stderr := &lockedBuffer{}
	data := t.TempDir()
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"-addr", "127.0.0.1:0", "-data", data}, testEnvironment(t), streams{strings.NewReader(""), io.Discard, stderr})
	}()
	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(stderr.String(), "demo on http://127.0.0.1:") {
		if time.Now().After(deadline) {
			t.Fatalf("daemon did not start: %q", stderr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if out := stderr.String(); !strings.Contains(out, "scripted") || !strings.Contains(out, "mcp=mock-mailcal over stdio, 10 tools") {
		t.Fatalf("startup line should name the offline model and the MCP child: %q", out)
	}
	if _, err := os.Stat(filepath.Join(data, worldDirName, "world.db")); err != nil {
		t.Fatalf("the mock server keeps its own database: %v", err)
	}
}

func TestLaunchdPlistIsWellFormed(t *testing.T) {
	env := testEnvironment(t)
	data := "/Users/me/Library/Application Support/mini-loop/assistant"
	var out, stderr bytes.Buffer
	code := run(context.Background(), []string{"launchd-plist", "-binary", "/Users/me/.local/bin/miniloop-assistant", "-data", data, "-log", "/tmp/a&b.log"}, env, streams{strings.NewReader(""), &out, &stderr})
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	plist := out.String()
	for _, want := range []string{"<string>dev.mini-loop.assistant</string>", "<string>/Users/me/.local/bin/miniloop-assistant</string>", "<string>" + data + "</string>", "<string>127.0.0.1:8790</string>", "<string>/tmp/a&amp;b.log</string>", "<key>SuccessfulExit</key>"} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist lacks %s:\n%s", want, plist)
		}
	}
	decoder := xml.NewDecoder(strings.NewReader(plist))
	decoder.Strict = true
	for {
		if _, err := decoder.Token(); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("plist is not well-formed XML: %v", err)
		}
	}
	if lint, err := exec.LookPath("plutil"); err == nil {
		path := filepath.Join(t.TempDir(), "agent.plist")
		if err := os.WriteFile(path, out.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		if result, err := exec.Command(lint, "-lint", path).CombinedOutput(); err != nil {
			t.Fatalf("plutil -lint: %v %s", err, result)
		}
	}
}

func TestLaunchdPlistRefusesUnsafeInstalls(t *testing.T) {
	cases := map[string][]string{
		"go run binary":    {"launchd-plist", "-binary", "/var/folders/x/go-build123/b001/exe/miniloop-assistant"},
		"model credential": {"launchd-plist", "-binary", "/usr/local/bin/miniloop-assistant", "-model", "anthropic"},
		"non-loopback":     {"launchd-plist", "-binary", "/usr/local/bin/miniloop-assistant", "-addr", "0.0.0.0:8790"},
		"unknown embed":    {"launchd-plist", "-binary", "/usr/local/bin/miniloop-assistant", "-embed", "cloud"},
		"bad embed url":    {"launchd-plist", "-binary", "/usr/local/bin/miniloop-assistant", "-embed", "ollama", "-embed-url", "localhost:11434"},
		"telegram":         {"launchd-plist", "-binary", "/usr/local/bin/miniloop-assistant", "-telegram-chat", "42"},
		"email":            {"launchd-plist", "-binary", "/usr/local/bin/miniloop-assistant", "-email-owner", "me@owner.example"},
	}
	for name, args := range cases {
		var out, stderr bytes.Buffer
		if code := run(context.Background(), args, testEnvironment(t), streams{strings.NewReader(""), &out, &stderr}); code != 1 || out.Len() != 0 {
			t.Errorf("%s: code=%d out=%q stderr=%q", name, code, out.String(), stderr.String())
		}
	}
}

// The plist passes the embedding flags through, and leaves them out when
// embeddings are off.
func TestLaunchdPlistCarriesTheEmbeddingFlags(t *testing.T) {
	plist := func(extra ...string) string {
		t.Helper()
		var out, stderr bytes.Buffer
		args := append([]string{"launchd-plist", "-binary", "/usr/local/bin/miniloop-assistant"}, extra...)
		if code := run(context.Background(), args, testEnvironment(t), streams{strings.NewReader(""), &out, &stderr}); code != 0 {
			t.Fatalf("code=%d stderr=%q", code, stderr.String())
		}
		return out.String()
	}
	if off := plist(); strings.Contains(off, "-embed") {
		t.Fatalf("embeddings are off by default:\n%s", off)
	}
	on := plist("-embed", "ollama", "-embed-model", "bge-m3")
	for _, want := range []string{"<string>-embed</string>\n\t\t<string>ollama</string>", "<string>http://127.0.0.1:11434</string>", "<string>bge-m3</string>"} {
		if !strings.Contains(on, want) {
			t.Errorf("plist lacks %q:\n%s", want, on)
		}
	}
}

// Remote channels take their accounts from flags and their secrets only from
// the environment.
func TestRemoteFlagsBuildTheChannels(t *testing.T) {
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	remote := bindRemoteFlags(flags)
	if err := flags.Parse(nil); err != nil {
		t.Fatal(err)
	}
	env := testEnvironment(t)
	if channels, err := remote.channels(env); err != nil || len(channels) != 0 || remote.any() {
		t.Fatalf("no channel by default: %v %v", channels, err)
	}
	if err := flags.Parse([]string{"-telegram-chat", "42", "-discord-user", "1001", "-email-owner", "me@owner.example", "-email-user", "bot@bot.example",
		"-email-imap", "imap.bot.example:993", "-email-smtp", "smtp.bot.example:587", "-email-authserv", "mx.bot.example"}); err != nil {
		t.Fatal(err)
	}
	secrets := map[string]string{envTelegramToken: "1:t", envDiscordToken: "d", envEmailPassword: "p"}
	env.getenv = func(name string) string { return secrets[name] }
	channels, err := remote.channels(env)
	if err != nil || len(channels) != 3 || !remote.any() {
		t.Fatalf("channels %v %v", channels, err)
	}
	names := []string{}
	for _, channel := range channels {
		names = append(names, channel.Name())
	}
	if strings.Join(names, ",") != "telegram,discord,email" {
		t.Fatalf("names %v", names)
	}
}

const (
	testAdminToken = "fleet-admin-token-for-tests-0123"
	testPushToken  = "fleet-push-token-for-tests-45678"
)

func fleetEnvironment(t *testing.T, admin string) environment {
	return fleetEnvironmentWith(t, admin, "")
}

func fleetEnvironmentWith(t *testing.T, admin, push string) environment {
	env := testEnvironment(t)
	env.getenv = func(name string) string {
		switch name {
		case envFleetAdminToken:
			return admin
		case envFleetPushToken:
			return push
		}
		return ""
	}
	return env
}

// A database URL the fleet cannot use is refused without repeating it: it
// holds a password.
func TestFleetRefusesADatabaseItCannotUseWithoutPrintingIt(t *testing.T) {
	for _, address := range []string{"mysql://fleet:hunter2secret@db.internal/fleet", "postgres://fleet:hunter2secret@", "hunter2secret"} {
		env := fleetEnvironmentWith(t, testAdminToken, "")
		inner := env.getenv
		env.getenv = func(name string) string {
			if name == envFleetDatabase {
				return address
			}
			return inner(name)
		}
		var stderr bytes.Buffer
		code := run(context.Background(), []string{"fleet", "-addr", "127.0.0.1:0", "-data", t.TempDir()}, env, streams{strings.NewReader(""), io.Discard, &stderr})
		if code != 1 || !strings.Contains(stderr.String(), "database URL") || strings.Contains(stderr.String(), "hunter2secret") {
			t.Errorf("%q: code=%d stderr=%q", address, code, stderr.String())
		}
	}
}

// The shards' URLs and the archive store's keys come from the environment
// only, and nothing the fleet prints when it cannot use them repeats them.
func TestFleetReadsShardsAndArchiveKeysFromTheEnvironmentWithoutPrintingThem(t *testing.T) {
	shards, err := parseShards(" s1=postgres://fleet@pg1/fleet\n\ts2=postgres://fleet@pg2/fleet?sslmode=verify-full ")
	if err != nil || len(shards) != 2 || shards["s2"] != "postgres://fleet@pg2/fleet?sslmode=verify-full" {
		t.Fatalf("pairs separated by white space: %v %v", shards, err)
	}
	if shards, err := parseShards(""); err != nil || len(shards) != 0 {
		t.Fatalf("no shards: %v %v", shards, err)
	}
	const secret = "hunter2secret"
	unreachable := "postgres://fleet:" + secret + "@127.0.0.1:1/fleet?connect_timeout=1"
	cases := map[string]struct {
		set  map[string]string
		args []string
		want string
	}{
		"a pair without a name":     {set: map[string]string{envFleetShards: "=" + unreachable}, want: envFleetShards},
		"a pair without a URL":      {set: map[string]string{envFleetShards: "s1"}, want: envFleetShards},
		"a name twice":              {set: map[string]string{envFleetShards: "s1=" + unreachable + " s1=" + unreachable}, want: "twice"},
		"shards without a database": {set: map[string]string{envFleetShards: "s1=" + unreachable}, want: envFleetDatabase},
		"an archive without a database": {args: []string{"-archive-store", "s3://bucket/fleet"},
			set: map[string]string{envFleetArchiveKeyID: "AKIDEXAMPLE", envFleetArchiveSecret: secret}, want: envFleetDatabase},
		"a database that is away": {set: map[string]string{envFleetDatabase: unreachable, envFleetShards: "s1=" + unreachable}, want: "127.0.0.1:1"},
	}
	for name, c := range cases {
		env := fleetEnvironmentWith(t, testAdminToken, "")
		inner := env.getenv
		env.getenv = func(name string) string {
			if value, ok := c.set[name]; ok {
				return value
			}
			return inner(name)
		}
		var stderr bytes.Buffer
		args := append([]string{"fleet", "-addr", "127.0.0.1:0", "-data", t.TempDir()}, c.args...)
		code := run(context.Background(), args, env, streams{strings.NewReader(""), io.Discard, &stderr})
		if code != 1 || !strings.Contains(stderr.String(), c.want) || strings.Contains(stderr.String(), secret) {
			t.Errorf("%s: code=%d stderr=%q", name, code, stderr.String())
		}
	}
}

func TestFleetRefusesUnsafeStartup(t *testing.T) {
	cases := map[string]struct {
		admin string
		args  []string
		push  string
	}{
		"short push token":          {admin: testAdminToken, args: []string{"fleet", "-addr", "127.0.0.1:0", "-data", t.TempDir()}, push: "short"},
		"push token is admin token": {admin: testAdminToken, args: []string{"fleet", "-addr", "127.0.0.1:0", "-data", t.TempDir()}, push: testAdminToken},
		"advertise is not a URL":    {admin: testAdminToken, args: []string{"fleet", "-addr", "127.0.0.1:0", "-advertise", "10.0.0.3:8791", "-data", t.TempDir()}},
		"no admin token":            {admin: "", args: []string{"fleet", "-addr", "127.0.0.1:0", "-data", t.TempDir()}},
		"short admin token":         {admin: "short", args: []string{"fleet", "-addr", "127.0.0.1:0", "-data", t.TempDir()}},
		"plain http on network":     {admin: testAdminToken, args: []string{"fleet", "-addr", "0.0.0.0:0", "-data", t.TempDir()}},
		"cert without key":          {admin: testAdminToken, args: []string{"fleet", "-addr", "0.0.0.0:0", "-tls-cert", "cert.pem", "-data", t.TempDir()}},
		"anthropic without secret":  {admin: testAdminToken, args: []string{"fleet", "-addr", "127.0.0.1:0", "-model", "anthropic", "-data", t.TempDir()}},
	}
	for name, c := range cases {
		var stderr bytes.Buffer
		if code := run(context.Background(), c.args, fleetEnvironmentWith(t, c.admin, c.push), streams{strings.NewReader(""), io.Discard, &stderr}); code != 1 || !strings.Contains(stderr.String(), "miniloop-assistant:") {
			t.Errorf("%s: code=%d stderr=%q", name, code, stderr.String())
		}
	}
}

// The fleet serves the admin API and a tenant's page and API behind their
// tokens, and stops cleanly.
func TestFleetServesTenantsUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stderr := &lockedBuffer{}
	data := t.TempDir()
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"fleet", "-addr", "127.0.0.1:0", "-data", data}, fleetEnvironmentWith(t, testAdminToken, testPushToken), streams{strings.NewReader(""), io.Discard, stderr})
	}()
	deadline := time.Now().Add(20 * time.Second)
	var base string
	for base == "" {
		if time.Now().After(deadline) {
			t.Fatalf("fleet did not start: %q", stderr.String())
		}
		if _, rest, ok := strings.Cut(stderr.String(), "fleet on "); ok {
			base, _, _ = strings.Cut(rest, " ")
		}
		time.Sleep(10 * time.Millisecond)
	}
	request := func(method, path, token, body string) (int, string) {
		t.Helper()
		req, err := http.NewRequest(method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		payload, _ := io.ReadAll(response.Body)
		return response.StatusCode, string(payload)
	}
	code, body := request(http.MethodPost, "/admin/tenants", testAdminToken, `{"id":"alice"}`)
	var created struct{ Token string }
	if err := json.Unmarshal([]byte(body), &created); code != http.StatusCreated || err != nil || created.Token == "" {
		t.Fatalf("create tenant: %d %s", code, body)
	}
	if code, _ := request(http.MethodGet, "/t/alice/api/state", "", ""); code != http.StatusUnauthorized {
		t.Fatalf("state without token: %d", code)
	}
	if code, body := request(http.MethodGet, "/t/alice/api/state", created.Token, ""); code != http.StatusOK || !strings.Contains(body, `"channel"`) {
		t.Fatalf("state with token: %d", code)
	}
	if code, _ := request(http.MethodPost, "/push/alice", created.Token, `{}`); code != http.StatusUnauthorized {
		t.Fatalf("an owner's token cannot push: %d", code)
	}
	if code, body := request(http.MethodPost, "/push/alice", testPushToken, `{}`); code != http.StatusAccepted || !strings.Contains(body, `"polled"`) {
		t.Fatalf("push to the open tenant: %d %s", code, body)
	}
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "model=scripted") {
		t.Fatalf("startup line names the shared model: %q", stderr.String())
	}
}
