//go:build darwin || linux

package reload

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// This child imports the public descriptor protocol, rather than duplicating it.
const serverFixture = `package main
import (
 "context"
 "fmt"
 "net/http"
 "os"
 "os/signal"
 "syscall"
 "time"
 "github.com/luoyjx/mini-loop/go/reload"
)
func main() {
 listener, err := reload.InheritedListener(); if err != nil { panic(err) }
 server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){fmt.Fprintf(w,"%s:%%d:%%s",os.Getpid(),os.Getenv("CHILD_VALUE"))})}
 signals := make(chan os.Signal,1); signal.Notify(signals,syscall.SIGTERM)
 go func(){ <-signals; ctx,cancel:=context.WithTimeout(context.Background(),time.Second); defer cancel(); server.Shutdown(ctx) }()
 server.Serve(listener)
}
`

func TestSupervisorRetainsSocketAcrossRecoveryAndJoinsChildren(t *testing.T) {
	root := buildFixture(t)
	module, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "go.mod"), []byte(fmt.Sprintf("module reload.fixture\n\ngo 1.23\n\nrequire github.com/luoyjx/mini-loop/go v0.0.0\nreplace github.com/luoyjx/mini-loop/go => %q\n", module)), 0600); err != nil {
		t.Fatal(err)
	}
	generation := 0
	write := func(source string) {
		t.Helper()
		generation++
		// Publish content and its synthetic timestamp together. An intermediate
		// wall-clock mtime could become StatReload's baseline before Chtimes.
		file, err := os.CreateTemp(root, ".reload-source-")
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(file.Name())
		defer file.Close()
		if _, err = file.WriteString(source); err != nil {
			t.Fatal(err)
		}
		if err = file.Close(); err != nil {
			t.Fatal(err)
		}
		stamp := time.Unix(int64(100+generation), 0)
		if err = os.Chtimes(file.Name(), stamp, stamp); err != nil {
			t.Fatal(err)
		}
		if err = os.Rename(file.Name(), filepath.Join(root, "main.go")); err != nil {
			t.Fatal(err)
		}
	}
	write(fmt.Sprintf(serverFixture, "first"))
	outputs := t.TempDir()
	builder, err := NewBuilder(BuildConfig{ModuleDirectory: root, Package: ".", TemporaryParent: outputs})
	if err != nil {
		t.Fatal(err)
	}
	delay := 20 * time.Millisecond
	watcher, err := NewWatcher(WatchConfig{Roots: []string{root}, Delay: &delay})
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan LifecycleEvent, 64)
	environment := append(os.Environ(), "CHILD_VALUE=captured")
	supervisor, err := NewSupervisor(SupervisorConfig{Builder: builder, Watcher: watcher, Directory: root, Environment: environment, StopGrace: time.Second, OnEvent: func(event LifecycleEvent) { events <- event }})
	if err != nil {
		t.Fatal(err)
	}
	environment[len(environment)-1] = "CHILD_VALUE=mutated"
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- supervisor.Run(ctx, listener) }()
	joined := false
	t.Cleanup(func() {
		cancel()
		if !joined {
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(15 * time.Second):
				t.Error("supervisor did not join")
			}
		}
	})
	await := func(kind LifecycleKind) LifecycleEvent {
		t.Helper()
		select {
		case event := <-events:
			if event.Kind != kind {
				t.Fatalf("event=%+v want %s", event, kind)
			}
			return event
		case err := <-done:
			joined = true
			t.Fatalf("early supervisor exit: %v", err)
		case <-time.After(30 * time.Second):
			t.Fatalf("timeout waiting for %s", kind)
		}
		return LifecycleEvent{}
	}
	client := &http.Client{Timeout: 200 * time.Millisecond}
	t.Cleanup(client.CloseIdleConnections)
	response := func(want string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			resp, err := client.Get("http://" + address)
			if err == nil {
				body, readErr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if readErr == nil && string(body) == want {
					return
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("HTTP response did not become %q", want)
	}
	first := await(ChildStarted)
	response(fmt.Sprintf("first:%d:captured", first.PID))
	write(fmt.Sprintf(serverFixture, "second"))
	await(SourceChanged)
	stopped := await(ChildStopped)
	if stopped.PID != first.PID || stopped.Forced {
		t.Fatalf("stop=%+v", stopped)
	}
	second := await(ChildStarted)
	if second.PID == first.PID {
		t.Fatal("child was not replaced")
	}
	response(fmt.Sprintf("second:%d:captured", second.PID))
	write("package main\nfunc main( {\n")
	await(SourceChanged)
	await(ChildStopped)
	failed := await(BuildFailed)
	if failed.Error == nil {
		t.Fatal("missing compilation error")
	}
	if duplicate, err := net.Listen("tcp", address); err == nil {
		duplicate.Close()
		t.Fatal("parent released socket on failed build")
	}
	write(fmt.Sprintf(serverFixture, "recovered"))
	await(SourceChanged)
	third := await(ChildStarted)
	response(fmt.Sprintf("recovered:%d:captured", third.PID))
	cancel()
	select {
	case err := <-done:
		joined = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("shutdown timeout")
	}
	await(ChildStopped)
	assertNoBuildOutputs(t, outputs)
	rebound, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("parent socket leaked: %v", err)
	}
	rebound.Close()
	for _, pid := range []int{first.PID, second.PID, third.PID} {
		if syscall.Kill(pid, 0) == nil {
			t.Fatalf("child %d still alive", pid)
		}
	}
	other, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := supervisor.Run(context.Background(), other); err == nil {
		t.Fatal("reused supervisor admitted")
	}
	file, err := other.File()
	if err != nil {
		t.Fatalf("rejected listener was closed: %v", err)
	}
	file.Close()
}

func TestSupervisorAdmissionAndOpaqueConfiguration(t *testing.T) {
	if _, err := NewSupervisor(SupervisorConfig{}); err == nil {
		t.Fatal("missing collaborators admitted")
	}
	if strings.Contains(fmt.Sprintf("%#v", &Supervisor{environment: []string{"secret"}}), "secret") {
		t.Fatal("configuration leaked")
	}
	t.Setenv(ListenerEnvironment, "not-a-descriptor")
	if _, err := InheritedListener(); err == nil {
		t.Fatal("invalid descriptor marker admitted")
	}
}

func TestSupervisorForcedStopAndCallbackFailureCleanup(t *testing.T) {
	for _, panicCallback := range []bool{false, true} {
		t.Run(fmt.Sprintf("callback-panic-%t", panicCallback), func(t *testing.T) {
			root := buildFixture(t)
			source := `package main
import("net";"net/http";"os";"os/signal";"syscall")
func main(){signal.Ignore(syscall.SIGTERM);file:=os.NewFile(3,"listener");listener,err:=net.FileListener(file);file.Close();if err!=nil{panic(err)};http.Serve(listener,http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.Write([]byte("ready"))}))}
`
			if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			outputs := t.TempDir()
			builder, err := NewBuilder(BuildConfig{ModuleDirectory: root, Package: ".", TemporaryParent: outputs})
			if err != nil {
				t.Fatal(err)
			}
			watcher, err := NewWatcher(WatchConfig{Roots: []string{root}})
			if err != nil {
				t.Fatal(err)
			}
			events := make(chan LifecycleEvent, 4)
			supervisor, err := NewSupervisor(SupervisorConfig{Builder: builder, Watcher: watcher, StopGrace: 30 * time.Millisecond, OnEvent: func(event LifecycleEvent) {
				events <- event
				if panicCallback && event.Kind == ChildStarted {
					panic("callback failure")
				}
			}})
			if err != nil {
				t.Fatal(err)
			}
			listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
			if err != nil {
				t.Fatal(err)
			}
			address := listener.Addr().String()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- supervisor.Run(ctx, listener) }()
			joined := false
			t.Cleanup(func() {
				cancel()
				if !joined {
					select {
					case <-done:
					case <-time.After(10 * time.Second):
						t.Error("cleanup join timeout")
					}
				}
			})
			var started LifecycleEvent
			select {
			case started = <-events:
				if started.Kind != ChildStarted {
					t.Fatalf("start=%+v", started)
				}
			case <-time.After(30 * time.Second):
				t.Fatal("start timeout")
			}
			if !panicCallback {
				client := &http.Client{Timeout: 200 * time.Millisecond}
				defer client.CloseIdleConnections()
				ready := false
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					resp, err := client.Get("http://" + address)
					if err == nil {
						resp.Body.Close()
						ready = true
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if !ready {
					t.Fatal("child not ready")
				}
				cancel()
			}
			select {
			case err := <-done:
				joined = true
				if panicCallback {
					if err == nil || !strings.Contains(err.Error(), "callback panicked") {
						t.Fatalf("callback result=%v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("stop timeout")
			}
			stopped := <-events
			if stopped.Kind != ChildStopped || stopped.PID != started.PID || (!panicCallback && !stopped.Forced) {
				t.Fatalf("stop=%+v", stopped)
			}
			assertNoBuildOutputs(t, outputs)
			if syscall.Kill(started.PID, 0) == nil {
				t.Fatal("child survived shutdown")
			}
			rebound, err := net.Listen("tcp", address)
			if err != nil {
				t.Fatal(err)
			}
			rebound.Close()
		})
	}
}
