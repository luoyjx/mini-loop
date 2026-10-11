package audit

import (
	"errors"
	"os"
	"strings"
	"syscall"

	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/httpapi"
)

// Settings checks deployment configuration and current workspace permissions.
// It retains Source's check order; the full live audit owns severity sorting.
// Nil environment observes the process; an explicit empty map observes no vars.
// No directories, sessions, providers or credentials are created/resolved.
func Settings(settings config.Settings, environment map[string]string) ([]Finding, error) {
	if environment == nil {
		environment = make(map[string]string)
		for _, entry := range os.Environ() {
			key, value, ok := strings.Cut(entry, "=")
			if ok {
				environment[key] = value
			}
		}
	}
	auth, err := httpapi.AuthFromEnvironment(environment)
	if err != nil {
		return nil, err
	}
	host, exists := environment["HOST"]
	if !exists {
		host = "127.0.0.1"
	}
	findings := make([]Finding, 0, 4)
	if !auth.Configured() {
		if httpapi.RefuseOpenBind(host, auth) != nil {
			findings = append(findings, Finding{Critical, HostBind, "the server binds " + host + " with no authentication -- every session, event stream and transcript is reachable by anyone who can route to the port", "set MINILOOP_API_TOKEN, or bind 127.0.0.1 (`python -m mini_loop` now refuses this combination outright)"})
		} else {
			findings = append(findings, Finding{Medium, Authentication, "no API tokens are configured; every caller on the loopback interface shares one anonymous identity and can read every session and transcript", "set MINILOOP_API_TOKEN (or MINILOOP_API_TOKENS=alice:...,bob:...)"})
		}
	}
	if settings.FakeLLM {
		findings = append(findings, Finding{Info, FakeLLM, "MINILOOP_FAKE_LLM is set; the deterministic offline model is answering, not a provider", "unset it for a real deployment"})
	}
	root := settings.WorkspaceRoot
	if root == "" {
		root = "."
	}
	info, err := os.Stat(root)
	if err != nil && !os.IsNotExist(err) && !errors.Is(err, syscall.ENOTDIR) && !errors.Is(err, syscall.ELOOP) && !errors.Is(err, syscall.EBADF) {
		return nil, err
	}
	if err == nil && info.Mode().Perm()&0002 != 0 {
		findings = append(findings, Finding{High, WorkspacePermissions, root + " is world-writable, so anything on the host can plant files an agent will read and act on", "chmod o-w " + root})
	}
	if settings.TrajectoryEnabled && settings.TrajectoryCaptureContent {
		findings = append(findings, Finding{Medium, TrajectoryContent, "trajectories record full tool inputs and outputs to disk", "set MINILOOP_TRAJECTORY_CAPTURE_CONTENT=0, or make sure secret masking is on so credentials never reach them"})
	}
	return findings, nil
}
