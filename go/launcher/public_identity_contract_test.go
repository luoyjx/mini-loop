package launcher

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/httpapi"
	"github.com/luoyjx/mini-loop/go/identity"
)

func TestPublicRuntimeIdentityReachesLauncherAndSharedHTTPProcess(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-public-identity-audit.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Runtime []struct {
			Name                string
			BuildMatchesPublic  bool `json:"build_matches_public"`
			PIDMatchesProcess   bool `json:"pid_matches_process"`
			StartedAtStable     bool `json:"started_at_stable"`
			UptimeNonnegative   bool `json:"uptime_nonnegative"`
			UptimeNondecreasing bool `json:"uptime_nondecreasing"`
			Posture             *agent.RuntimePosture
		}
		ManagerSideEffects struct {
			Sessions   int
			ModelCalls int `json:"model_calls"`
		} `json:"manager_side_effects"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Runtime) != 3 || fixture.ManagerSideEffects.Sessions != 0 || fixture.ManagerSideEffects.ModelCalls != 0 {
		t.Fatal("incomplete actual Source identity manager corpus")
	}
	for _, row := range fixture.Runtime[1:] {
		t.Run(row.Name, func(t *testing.T) {
			if row.Posture == nil || !row.BuildMatchesPublic || !row.PIDMatchesProcess || !row.StartedAtStable || !row.UptimeNonnegative || !row.UptimeNondecreasing {
				t.Fatal("Source runtime identity relationship changed", row)
			}
			settings := settingsFor(t, nil)
			var auth httpapi.Authenticator
			if row.Name == "authenticated-manager" {
				auth, err = httpapi.NewTokenAuth([]httpapi.TokenBinding{{Token: "fixture-token", Principal: "fixture-owner"}})
				if err != nil {
					t.Fatal(err)
				}
			}
			app, err := New(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, auth)
			if err != nil {
				t.Fatal(err)
			}
			defer stopApp(t, app)
			read := func(handler http.Handler) httpapi.HealthResponse {
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
				if recorder.Code != http.StatusOK {
					t.Fatal(recorder.Code, recorder.Body.String())
				}
				var health httpapi.HealthResponse
				if err := json.Unmarshal(recorder.Body.Bytes(), &health); err != nil {
					t.Fatal(err)
				}
				return health
			}
			first := read(app.Handler())
			var clockCalls atomic.Int64
			second, err := httpapi.New(httpapi.Config{Manager: app.manager, Auth: auth, Now: func() time.Time {
				clockCalls.Add(1)
				return time.Unix(5, 0)
			}})
			if err != nil {
				t.Fatal(err)
			}
			other := read(second)
			again := read(app.Handler())
			process := identity.Process()
			build := CurrentBuild()
			inspected := Inspect(settings, config.ServerSettings{Host: "127.0.0.1"}, auth)
			if build.Fingerprint == "" || build.FingerprintError != nil || !reflect.DeepEqual(build, inspected.Build) || first.Build != string(build.Fingerprint) || other.Build != first.Build || again.Build != first.Build {
				t.Fatal("launcher inspection and runtime disagree about native content fingerprint", build, first.Build, other.Build, again.Build)
			}
			if first.PID != os.Getpid() || other.PID != first.PID || again.PID != first.PID || first.Started != process.StartedAt || other.Started != first.Started || again.Started != first.Started || first.Uptime < 0 || other.Uptime < first.Uptime || again.Uptime < other.Uptime || clockCalls.Load() != 0 {
				t.Fatal("server-local or injected request time leaked into process identity", first, other, again, process, clockCalls.Load())
			}
			wantPosture := *row.Posture
			// Existing native component-label adaptations remain visible. The
			// process identity change does not rename or activate these services.
			if wantPosture.Sandbox == "NullSandbox" {
				wantPosture.Sandbox = "None"
			}
			if wantPosture.Secrets == "NullSecretRegistry" {
				wantPosture.Secrets = "None"
			}
			if !reflect.DeepEqual(first.Posture, wantPosture) || !reflect.DeepEqual(other.Posture, wantPosture) {
				t.Fatal("identity did not bind the actual selected manager posture", first.Posture, wantPosture)
			}
			if app.manager.Summary().Sessions != fixture.ManagerSideEffects.Sessions {
				t.Fatal("runtime inspection created a session")
			}
			// A caller-supplied embedding label remains explicit. It does not
			// change or reset the shared process identity.
			override, err := httpapi.New(httpapi.Config{Manager: app.manager, Auth: auth, Build: "explicit-embedding-build"})
			if err != nil {
				t.Fatal(err)
			}
			custom := read(override)
			if custom.Build != "explicit-embedding-build" || custom.PID != first.PID || custom.Started != first.Started {
				t.Fatal("explicit Build override changed unrelated process identity", custom)
			}
		})
	}
}

func TestPublicIdentityUnavailableExecutableFailsBeforeComposition(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	copyPath := filepath.Join(t.TempDir(), "identity-failure-probe")
	output, err := os.OpenFile(copyPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := io.CopyBuffer(output, input, make([]byte, 32*1024))
	closeErr := output.Close()
	if readErr != nil || closeErr != nil {
		t.Fatal(readErr, closeErr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, copyPath, "-test.run=^TestPublicIdentityUnavailableExecutableHelper$", "-test.count=1", "-test.v")
	command.Env = append(os.Environ(), "MINILOOP_IDENTITY_UNLINK_HELPER=1", "MINILOOP_IDENTITY_PROBE_ROOT="+filepath.Join(t.TempDir(), "uncreated-workspace"))
	data, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(data), "identity-unavailable-contract-passed") {
		t.Fatal("fresh-process fingerprint failure contract", string(data), err)
	}
}

func TestPublicIdentityUnavailableExecutableHelper(t *testing.T) {
	if os.Getenv("MINILOOP_IDENTITY_UNLINK_HELPER") != "1" {
		return
	}
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Only a private copied test executable is removed, before its first build
	// identity read. This exercises the real public cache and startup path.
	if filepath.Base(path) != "identity-failure-probe" {
		t.Fatal("refusing to unlink any other executable", path)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	build, err := identity.BuildID()
	if build != "" || err == nil || err.Error() != "native executable fingerprint unavailable" {
		t.Fatal("unreadable executable produced a build identity", build, err)
	}
	metadata := CurrentBuild()
	if metadata.Fingerprint != "" || metadata.FingerprintError == nil || *metadata.FingerprintError != err.Error() {
		t.Fatal("inspection hid a failed fingerprint", metadata)
	}
	if snapshot, err := identity.Runtime(); err == nil || snapshot != (identity.Snapshot{}) {
		t.Fatal("runtime identity hid a failed fingerprint", snapshot, err)
	}
	root := os.Getenv("MINILOOP_IDENTITY_PROBE_ROOT")
	settings := settingsFor(t, map[string]string{"MINILOOP_WORKSPACE_ROOT": root})
	app, err := New(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil)
	if app != nil || err == nil || err.Error() != "native executable fingerprint unavailable" {
		t.Fatal("composition accepted unavailable fingerprint", app, err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("failed identity created workspace before startup refusal", err)
	}
	provider := agent.NewFakeProvider(agent.FakeProviderConfig{}).ObjectView()
	manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: t.TempDir(), Services: agent.ManagerServices{Provider: provider}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := manager.Stop(ctx); err != nil {
			t.Error(err)
		}
	}()
	if server, err := httpapi.New(httpapi.Config{Manager: manager}); server != nil || err == nil || err.Error() != "native executable fingerprint unavailable" {
		t.Fatal("default HTTP construction hid failed fingerprint", server, err)
	}
	server, err := httpapi.New(httpapi.Config{Manager: manager, Build: "explicit-embedding-build"})
	if err != nil {
		t.Fatal("explicit Build unexpectedly required native artifact I/O", err)
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"build":"explicit-embedding-build"`) {
		t.Fatal("health performed an implicit fingerprint read despite override", recorder.Code, recorder.Body.String())
	}
	t.Log("identity-unavailable-contract-passed")
}
