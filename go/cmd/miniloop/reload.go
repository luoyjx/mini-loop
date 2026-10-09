package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"

	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/reload"
)

// reloadModuleDirectory discovers a Go module from runtime cwd, never from
// embedded compiler paths. An explicit operator directory skips discovery.
func reloadModuleDirectory(directory, current string) (string, error) {
	if directory != "" {
		if !filepath.IsAbs(directory) {
			directory = filepath.Join(current, directory)
		}
		return filepath.Clean(directory), nil
	}
	for candidate := current; ; candidate = filepath.Dir(candidate) {
		info, err := os.Stat(filepath.Join(candidate, "go.mod"))
		if err == nil && info.Mode().IsRegular() {
			return candidate, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if parent := filepath.Dir(candidate); parent == candidate {
			break
		}
	}
	return "", errors.New("MINILOOP_RELOAD requires a Go module in cwd/ancestors or --reload-module")
}

func reloadEnvironment(environment map[string]string) []string {
	if environment == nil {
		return os.Environ()
	}
	names := make([]string, 0, len(environment))
	for name := range environment {
		if name != reload.ListenerEnvironment {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	values := make([]string, 0, len(names))
	for _, name := range names {
		values = append(values, name+"="+environment[name])
	}
	return values
}

func serveReload(ctx context.Context, args []string, environment map[string]string, settings config.ServerSettings, module string, stdout, stderr io.Writer) error {
	current, err := os.Getwd()
	if err != nil {
		return err
	}
	directory, err := reloadModuleDirectory(module, current)
	if err != nil {
		return err
	}
	builder, err := reload.NewBuilder(reload.BuildConfig{ModuleDirectory: directory})
	if err != nil {
		return err
	}
	watcher, err := reload.NewWatcher(reload.WatchConfig{Roots: []string{current}})
	if err != nil {
		return err
	}
	supervisor, err := reload.NewSupervisor(reload.SupervisorConfig{Builder: builder, Watcher: watcher, Directory: current, Arguments: args, Environment: reloadEnvironment(environment), Stdout: stdout, Stderr: stderr, OnEvent: func(event reload.LifecycleEvent) {
		if event.Error != nil {
			fmt.Fprintf(stderr, "mini-loop reload %s: %v\n", event.Kind, event.Error)
			return
		}
		fmt.Fprintf(stderr, "mini-loop reload %s pid=%d forced=%t path=%s\n", event.Kind, event.PID, event.Forced, event.Path)
	}})
	if err != nil {
		return err
	}
	address, err := net.ResolveTCPAddr("tcp", settings.Address())
	if err != nil {
		return err
	}
	listener, err := net.ListenTCP("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close() // also releases the socket if platform admission fails
	fmt.Fprintf(stderr, "mini-loop Go reload listening on %s (watch cwd; process-local sessions reset on replacement)\n", listener.Addr())
	return supervisor.Run(ctx, listener)
}
