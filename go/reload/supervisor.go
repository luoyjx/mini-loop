package reload

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const ListenerEnvironment = "MINILOOP_RELOAD_LISTENER_FD"

type LifecycleKind string

const (
	SourceChanged    LifecycleKind = "source_changed"
	BuildFailed      LifecycleKind = "build_failed"
	ChildStarted     LifecycleKind = "child_started"
	ChildStartFailed LifecycleKind = "child_start_failed"
	ChildExited      LifecycleKind = "child_exited"
	ChildStopped     LifecycleKind = "child_stopped"
)

type LifecycleEvent struct {
	Kind   LifecycleKind
	Path   string
	PID    int
	Forced bool
	Error  error
}

type SupervisorConfig struct {
	Builder   *Builder
	Watcher   *Watcher
	Directory string
	Arguments []string
	// Nil captures the process environment once; empty supplies an empty base.
	Environment    []string
	Stdout, Stderr io.Writer
	StopGrace      time.Duration
	// The caller must return promptly. A panic becomes an error after cleanup.
	OnEvent func(LifecycleEvent)
}

type Supervisor struct {
	mu                     sync.Mutex
	used                   bool
	builder                *Builder
	watcher                *Watcher
	directory              string
	arguments, environment []string
	stdout, stderr         io.Writer
	grace                  time.Duration
	onEvent                func(LifecycleEvent)
}

func (*Supervisor) String() string              { return "<native reload supervisor>" }
func (supervisor *Supervisor) GoString() string { return supervisor.String() }

func NewSupervisor(config SupervisorConfig) (*Supervisor, error) {
	if config.Builder == nil || config.Watcher == nil || config.StopGrace < 0 {
		return nil, errors.New("reload supervisor requires builder, watcher and nonnegative stop grace")
	}
	directory := config.Directory
	if directory == "" {
		var err error
		directory, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	directory, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("reload child directory must be a directory")
	}
	env := config.Environment
	if env == nil {
		env = os.Environ()
	}
	grace := config.StopGrace
	if grace == 0 {
		grace = 10 * time.Second
	}
	stdout, stderr := config.Stdout, config.Stderr
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	return &Supervisor{builder: config.Builder, watcher: config.Watcher, directory: directory,
		arguments: append([]string{}, config.Arguments...), environment: append([]string{}, env...),
		stdout: stdout, stderr: stderr, grace: grace, onEvent: config.OnEvent}, nil
}

func (supervisor *Supervisor) emit(event LifecycleEvent) (err error) {
	if supervisor.onEvent == nil {
		return nil
	}
	defer func() {
		if recover() != nil {
			err = errors.New("reload lifecycle callback panicked")
		}
	}()
	supervisor.onEvent(event)
	return nil
}

type supervisedChild struct {
	command  *exec.Cmd
	artifact *Artifact
	done     chan struct{}
	err      error // Published before done closes; readers wait for done.
}

func (supervisor *Supervisor) launch(ctx context.Context, file *os.File, artifact *Artifact) (child *supervisedChild, err error) {
	defer func() {
		if child == nil {
			err = errors.Join(err, artifact.Close())
		}
	}()
	path, err := artifact.Executable(ctx)
	if err != nil {
		return nil, err
	}
	command := exec.Command(path, supervisor.arguments...)
	command.Dir = supervisor.directory
	command.Env = append(append([]string{}, supervisor.environment...), ListenerEnvironment+"=3")
	command.ExtraFiles = []*os.File{file}
	command.Stdout, command.Stderr = supervisor.stdout, supervisor.stderr
	command.WaitDelay = 2 * time.Second
	configureBuildGroup(command)
	if err := command.Start(); err != nil {
		return nil, err
	}
	child = &supervisedChild{command: command, artifact: artifact, done: make(chan struct{})}
	go func(child *supervisedChild) {
		child.err = child.command.Wait()
		killBuildGroup(child.command.Process)
		close(child.done)
	}(child)
	return child, nil
}

func (supervisor *Supervisor) stop(child *supervisedChild) (forced bool, err error) {
	select {
	case <-child.done:
	default:
		signalChildStop(child.command.Process)
		timer := time.NewTimer(supervisor.grace)
		select {
		case <-child.done:
		case <-timer.C:
			forced = true
			killBuildGroup(child.command.Process)
			<-child.done
		}
		timer.Stop()
	}
	return forced, child.artifact.Close()
}

// Run transfers ownership of the supplied TCP listener after single-use/platform
// admission. It retains the parent socket across every replacement and closes it
// only after the final child/compiler is joined. Cancellation is normal shutdown.
// Build/start failures leave the observer running for a future source correction.
func (supervisor *Supervisor) Run(ctx context.Context, listener *net.TCPListener) (err error) {
	if listener == nil || !inheritedListenersSupported {
		return errors.New("reload supervisor requires an inherited TCP listener on macOS/Linux")
	}
	supervisor.mu.Lock()
	if supervisor.used {
		supervisor.mu.Unlock()
		return errors.New("reload supervisor is single-use")
	}
	supervisor.used = true
	supervisor.mu.Unlock()
	defer listener.Close()
	file, err := listener.File()
	if err != nil {
		return err
	}
	defer file.Close()
	var child *supervisedChild
	defer func() {
		if child != nil {
			forced, stopErr := supervisor.stop(child)
			err = errors.Join(err, stopErr, supervisor.emit(LifecycleEvent{Kind: ChildStopped, PID: child.command.Process.Pid, Forced: forced}))
		}
	}()
	interval := supervisor.watcher.delay
	if interval == 0 {
		interval = time.Nanosecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	// Prime before building so edits during a slow build are observed afterward.
	prime := func() error { supervisor.watcher.Reset(); _, err := supervisor.watcher.Scan(ctx); return err }
	if err := prime(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	rebuild := true
	for {
		if ctx.Err() != nil {
			return nil
		}
		if rebuild {
			if child != nil {
				forced, stopErr := supervisor.stop(child)
				pid := child.command.Process.Pid
				child = nil
				if err := errors.Join(stopErr, supervisor.emit(LifecycleEvent{Kind: ChildStopped, PID: pid, Forced: forced})); err != nil {
					return err
				}
			}
			artifact, buildErr := supervisor.builder.Build(ctx, supervisor.stderr)
			if ctx.Err() != nil {
				if artifact != nil {
					return artifact.Close()
				}
				return nil
			}
			if buildErr != nil {
				if err := supervisor.emit(LifecycleEvent{Kind: BuildFailed, Error: buildErr}); err != nil {
					return err
				}
			} else {
				child, err = supervisor.launch(ctx, file, artifact)
				if err != nil {
					if err := supervisor.emit(LifecycleEvent{Kind: ChildStartFailed, Error: err}); err != nil {
						return err
					}
				} else if err := supervisor.emit(LifecycleEvent{Kind: ChildStarted, PID: child.command.Process.Pid}); err != nil {
					return err
				}
			}
			rebuild = false
		}
		var childDone <-chan struct{}
		if child != nil {
			childDone = child.done
		}
		select {
		case <-ctx.Done():
			return nil
		case <-childDone:
			pid, exitErr := child.command.Process.Pid, child.err
			if err := child.artifact.Close(); err != nil {
				return err
			}
			child = nil
			if err := supervisor.emit(LifecycleEvent{Kind: ChildExited, PID: pid, Error: exitErr}); err != nil {
				return err
			}
		case <-ticker.C:
			change, scanErr := supervisor.watcher.Scan(ctx)
			if ctx.Err() != nil {
				return nil
			}
			if scanErr != nil {
				return scanErr
			}
			if change.Path != "" {
				if err := supervisor.emit(LifecycleEvent{Kind: SourceChanged, Path: change.Path}); err != nil {
					return err
				}
				if err := prime(); err != nil {
					if ctx.Err() != nil {
						return nil
					}
					return err
				}
				rebuild = true
			}
		}
	}
}

// InheritedListener is the child-side protocol. CLI admission/authentication is
// still the caller's responsibility, including the actual inherited bind address.
func InheritedListener() (*net.TCPListener, error) {
	if !inheritedListenersSupported || os.Getenv(ListenerEnvironment) != "3" {
		return nil, errors.New("reload child requires TCP listener descriptor 3")
	}
	file := os.NewFile(3, "miniloop-reload-listener")
	defer file.Close()
	listener, err := net.FileListener(file)
	if err != nil {
		return nil, err
	}
	tcp, ok := listener.(*net.TCPListener)
	if !ok {
		listener.Close()
		return nil, errors.New("reload inherited listener is not TCP")
	}
	return tcp, nil
}
