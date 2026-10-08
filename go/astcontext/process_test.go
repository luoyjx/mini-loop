package astcontext

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A native test executable supplies controlled sidecar behavior without Python
// or a shell. The production adapter still executes its original argv/process path.
func init() {
	if os.Getenv("AST_NATIVE_CHILD") != "1" {
		return
	}
	if marker := os.Getenv("AST_MARKER"); marker != "" {
		_ = os.WriteFile(marker, []byte("started"), 0600)
	}
	switch os.Getenv("AST_MODE") {
	case "operation":
		operationTestChild()
	case "hang":
		time.Sleep(10 * time.Second)
	case "closed-pipes":
		os.Stdout.Close()
		os.Stderr.Close()
		time.Sleep(10 * time.Second)
	case "partial-overflow-hang":
		_, _ = fmt.Fprint(os.Stdout, os.Getenv("AST_STDOUT"))
		time.Sleep(10 * time.Second)
	case "watch":
		time.Sleep(800 * time.Millisecond)
		_ = os.WriteFile(os.Getenv("AST_SURVIVED"), []byte("survived"), 0600)
	case "descendant":
		binary, _ := os.Executable()
		child := exec.Command(binary, "--version")
		child.Env = []string{"AST_NATIVE_CHILD=1", "AST_MODE=watch", "AST_SURVIVED=" + os.Getenv("AST_SURVIVED"), "AST_MARKER=" + os.Getenv("AST_MARKER") + ".child", "GORACE=atexit_sleep_ms=0"}
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(9)
		}
	case "environment":
		cwd, _ := os.Getwd()
		payload := struct {
			Private, Chosen, NoColor, TmpDir, Cwd string
			Arguments                             []string
		}{os.Getenv("AST_PRIVATE_CANARY"), os.Getenv("AST_CHOSEN"), os.Getenv("NO_COLOR"), os.Getenv("TMPDIR"), cwd, os.Args[1:]}
		_ = json.NewEncoder(os.Stdout).Encode(payload)
	case "raw":
		data, _ := hex.DecodeString(os.Getenv("AST_RAW_HEX"))
		_, _ = os.Stdout.Write(data)
	default:
		_, _ = fmt.Fprint(os.Stdout, os.Getenv("AST_STDOUT"))
		_, _ = fmt.Fprint(os.Stderr, os.Getenv("AST_STDERR"))
	}
	code, _ := strconv.Atoi(os.Getenv("AST_EXIT"))
	os.Exit(code)
}

func nativeAdapter(t *testing.T) (Parameters, string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	t.Setenv("AST_NATIVE_CHILD", "1")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	p := DefaultParameters()
	p.Binary = binary
	p.EnvPassthrough = []string{"AST_NATIVE_CHILD", "AST_MODE", "AST_MARKER", "AST_STDOUT", "AST_STDERR", "AST_EXIT", "AST_RAW_HEX", "AST_SURVIVED", "GORACE"}
	return p, workspace
}

func adapter(t *testing.T, p Parameters) *Adapter {
	t.Helper()
	config, err := NewConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewAdapter(config)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestNativeProbesReplayAllSourceCases(t *testing.T) {
	for _, row := range fixture(t).Probes {
		t.Run(row.Name, func(t *testing.T) {
			p, workspace := nativeAdapter(t)
			marker := filepath.Join(t.TempDir(), "calls")
			t.Setenv("AST_MARKER", marker)
			if row.ExitCode != nil {
				t.Setenv("AST_STDOUT", row.Stdout)
				t.Setenv("AST_STDERR", row.Stderr)
				t.Setenv("AST_EXIT", strconv.Itoa(*row.ExitCode))
			} else {
				// The Source executable used by its admission recipes exits 7.
				t.Setenv("AST_STDERR", "  version failure\n")
				t.Setenv("AST_EXIT", "7")
				switch row.Name {
				case "missing":
					p.Binary = filepath.Join(t.TempDir(), "absent")
				case "workspace-binary":
					inside := filepath.Join(workspace, "ast-probe")
					data, err := os.ReadFile(p.Binary)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(inside, data, 0700); err != nil {
						t.Fatal(err)
					}
					p.Binary = inside
				case "digest-mismatch":
					digest := strings.Repeat("0", 64)
					p.ExpectedSHA256 = &digest
				case "digest-match":
					data, err := os.ReadFile(p.Binary)
					if err != nil {
						t.Fatal(err)
					}
					sum := sha256.Sum256(data)
					digest := hex.EncodeToString(sum[:])
					p.ExpectedSHA256 = &digest
				}
			}
			probe, err := adapter(t, p).Probe(context.Background(), workspace)
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(probe)
			if err != nil {
				t.Fatal(err)
			}
			var actual probeWire
			if err := json.Unmarshal(data, &actual); err != nil {
				t.Fatal(err)
			}
			if row.Name == "missing" {
				if actual.Message != "ast-outline executable not found: "+p.Binary {
					t.Fatal("incorrect missing executable text", actual.Message)
				}
				actual.Message = "ast-outline executable not found: <scratch>/absent"
			}
			if actual != row.Output {
				t.Fatal(actual, row.Output)
			}
			_, markerErr := os.Stat(marker)
			executed := markerErr == nil
			wantExecuted := row.Executed
			if executed != wantExecuted {
				t.Fatal("unexpected executable invocation", executed, wantExecuted)
			}
		})
	}
}

func TestIndependentCaptureCapsAndUTF8(t *testing.T) {
	p, workspace := nativeAdapter(t)
	p.MaxOutputBytes = 1000
	for _, stream := range []string{"AST_STDOUT", "AST_STDERR"} {
		t.Run(stream, func(t *testing.T) {
			t.Setenv(stream, strings.Repeat("x", 1001))
			probe, err := adapter(t, p).Probe(context.Background(), workspace)
			if err != nil || probe.Snapshot().Status != Error || probe.Snapshot().Message != "ast-outline output exceeded 1,000 bytes" {
				t.Fatal(probe, err)
			}
		})
	}
	t.Setenv("AST_STDOUT", strings.Repeat("x", 1000))
	t.Setenv("AST_STDERR", strings.Repeat("y", 1000))
	result, err := adapter(t, p).run(context.Background(), workspace, []string{"--version"})
	if err != nil || result.status != Applied || len(result.stdout) != 1000 || len(result.stderr) != 1000 {
		t.Fatal(result, err)
	}
	t.Setenv("AST_MODE", "raw")
	t.Setenv("AST_RAW_HEX", "61e2820d0aff")
	result, err = adapter(t, p).run(context.Background(), workspace, []string{"--version"})
	if err != nil || result.stdout != "a\ufffd\r\n\ufffd" {
		t.Fatal(result, err)
	}
}

func TestPartialOverflowStillSharesBufferedReadDeadline(t *testing.T) {
	p, workspace := nativeAdapter(t)
	p.MaxOutputBytes = 1000
	p.TimeoutSeconds = .3
	t.Setenv("AST_MODE", "partial-overflow-hang")
	t.Setenv("AST_STDOUT", strings.Repeat("x", 1001))
	result, err := adapter(t, p).run(context.Background(), workspace, []string{"--version"})
	if err != nil || result.status != Error || result.message != "ast-outline timed out after 0.3s" || len(result.stdout) != 1000 {
		t.Fatal(result.status, result.message, len(result.stdout), err)
	}
}

func TestSidecarEnvironmentAndArgv(t *testing.T) {
	p, workspace := nativeAdapter(t)
	t.Setenv("AST_MODE", "environment")
	t.Setenv("AST_PRIVATE_CANARY", "secret")
	t.Setenv("AST_CHOSEN", "allowed")
	t.Setenv("NO_COLOR", "wrong")
	t.Setenv("TMPDIR", workspace)
	p.EnvPassthrough = append(p.EnvPassthrough, "AST_CHOSEN")
	argument := "x;touch should-not-exist"
	result, err := adapter(t, p).run(context.Background(), workspace, []string{"grep", argument})
	if err != nil || result.status != Applied {
		t.Fatal(result, err)
	}
	var actual struct {
		Private, Chosen, NoColor, TmpDir, Cwd string
		Arguments                             []string
	}
	if err := json.Unmarshal([]byte(result.stdout), &actual); err != nil {
		t.Fatal(err)
	}
	root, err := resolvedPath(workspace)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := temporaryParent(root)
	if err != nil {
		t.Fatal(err)
	}
	if actual.Private != "" || actual.Chosen != "allowed" || actual.NoColor != "1" || actual.Cwd != root || actual.TmpDir != parent || contained(root, actual.TmpDir) || !reflect.DeepEqual(actual.Arguments, []string{"grep", argument}) {
		t.Fatal(actual)
	}
	if _, err := os.Stat(filepath.Join(workspace, "should-not-exist")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("argv was shell-interpreted")
	}
}

func TestDeadlineIncludesExitedChildDescendantPipes(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("Unix native process-group profile")
	}
	for _, mode := range []string{"hang", "closed-pipes", "descendant"} {
		t.Run(mode, func(t *testing.T) {
			p, workspace := nativeAdapter(t)
			p.TimeoutSeconds = .3
			t.Setenv("AST_MODE", mode)
			survived := filepath.Join(t.TempDir(), "survived")
			t.Setenv("AST_SURVIVED", survived)
			marker := filepath.Join(t.TempDir(), "started")
			t.Setenv("AST_MARKER", marker)
			start := time.Now()
			probe, err := adapter(t, p).Probe(context.Background(), workspace)
			if err != nil || probe.Snapshot().Status != Error || probe.Snapshot().Message != "ast-outline timed out after 0.3s" || time.Since(start) > 2*time.Second {
				t.Fatal(probe, err, time.Since(start))
			}
			if mode == "descendant" {
				if _, err := os.Stat(marker + ".child"); err != nil {
					t.Fatal("descendant did not actually start", err)
				}
				time.Sleep(900 * time.Millisecond)
				if _, err := os.Stat(survived); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("descendant survived original deadline")
				}
			}
		})
	}
}

func TestCancellationJoinsNativeChild(t *testing.T) {
	p, workspace := nativeAdapter(t)
	t.Setenv("AST_MODE", "hang")
	ctx, cancel := context.WithCancel(context.Background())
	marker := filepath.Join(t.TempDir(), "started")
	t.Setenv("AST_MARKER", marker)
	result := make(chan error, 1)
	a := adapter(t, p)
	go func() { _, err := a.Probe(ctx, workspace); result <- err }()
	deadline := time.After(3 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("child did not start")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not join child")
	}
	os.Remove(marker)
	if _, err := adapter(t, p).Probe(ctx, workspace); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("pre-cancelled context executed child")
	}
	if _, err := NewAdapter(Config{}); err == nil {
		t.Fatal("invalid config accepted")
	}
	if _, err := (&Adapter{}).Probe(context.Background(), workspace); err == nil {
		t.Fatal("zero adapter accepted")
	}
}

func TestDigestRecheckedBySameAdapterBeforeEveryProbe(t *testing.T) {
	p, workspace := nativeAdapter(t)
	data, err := os.ReadFile(p.Binary)
	if err != nil {
		t.Fatal(err)
	}
	p.Binary = filepath.Join(t.TempDir(), "operator-binary")
	if err := os.WriteFile(p.Binary, data, 0700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	p.ExpectedSHA256 = &digest
	t.Setenv("AST_STDOUT", "ast-outline 1.9.0\nhomepage: "+Homepage)
	marker := filepath.Join(t.TempDir(), "called")
	t.Setenv("AST_MARKER", marker)
	a := adapter(t, p)
	probe, err := a.Probe(context.Background(), workspace)
	if err != nil || probe.Snapshot().Status != Applied {
		t.Fatal(probe, err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(p.Binary, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("changed")); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	probe, err = a.Probe(context.Background(), workspace)
	if err != nil || probe.Snapshot().Status != Incompatible || probe.Snapshot().Message != "ast-outline executable SHA-256 does not match the pinned digest" {
		t.Fatal(probe, err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("changed binary executed")
	}
}
