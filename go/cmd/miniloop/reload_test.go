package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/launcher"
	"github.com/luoyjx/mini-loop/go/reload"
)

func TestReloadDumpDoesNotDiscoverModulesOrBuild(t *testing.T) {
	root := t.TempDir()
	var out, diagnostics bytes.Buffer
	env := map[string]string{"MINILOOP_RELOAD": "false", "MINILOOP_WORKSPACE_ROOT": filepath.Join(root, "workspaces")}
	code := execute(context.Background(), []string{"--dump-config", "--reload-module", filepath.Join(root, "missing")}, env, &out, &diagnostics)
	if code != 0 {
		t.Fatal(code, diagnostics.String())
	}
	var report launcher.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || !report.Server.Reload {
		t.Fatal(err, out.String())
	}
	if _, err := os.Stat(env["MINILOOP_WORKSPACE_ROOT"]); !os.IsNotExist(err) {
		t.Fatal("dump allocated runtime", err)
	}
}

func TestReloadModuleDiscoveryAndEnvironmentProtocolIsolation(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "nested")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	found, err := reloadModuleDirectory("", child)
	if err != nil || found != root {
		t.Fatal(found, err)
	}
	found, err = reloadModuleDirectory("../explicit", child)
	if err != nil || found != filepath.Join(root, "explicit") {
		t.Fatal(found, err)
	}
	env := map[string]string{"Z": "last", "A": "first", reload.ListenerEnvironment: "3"}
	values := reloadEnvironment(env)
	if strings.Join(values, ";") != "A=first;Z=last" {
		t.Fatal(values)
	}
	values[0] = "mutated"
	if env["A"] != "first" {
		t.Fatal("environment alias")
	}
}

func TestInheritedPublicSocketRefusalPrecedesRuntimeAllocation(t *testing.T) {
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4zero})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	root := t.TempDir()
	var out, diagnostics bytes.Buffer
	env := map[string]string{"HOST": "127.0.0.1", "MINILOOP_RELOAD": "1", "MINILOOP_WORKSPACE_ROOT": filepath.Join(root, "workspaces")}
	code := executeWithListener(context.Background(), nil, env, false, &out, &diagnostics, listener)
	if code != 1 || !strings.Contains(diagnostics.String(), "refusing to bind") {
		t.Fatal(code, diagnostics.String())
	}
	if _, err := os.Stat(env["MINILOOP_WORKSPACE_ROOT"]); !os.IsNotExist(err) {
		t.Fatal("public listener created workspace", err)
	}
}
