package astcontext

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

// Adapter owns no persistent subprocess or mutable request state. Each probe
// launches an isolated process after rechecking the operator's executable.
type Adapter struct{ config Config }

func NewAdapter(config Config) (*Adapter, error) {
	if !config.valid {
		return nil, errors.New("AST context configuration is not initialized")
	}
	return &Adapter{config: config}, nil
}

func (a *Adapter) Probe(ctx context.Context, workspace string) (Probe, error) {
	result, err := a.run(ctx, workspace, []string{"--version"})
	if err != nil {
		return Probe{}, err
	}
	if result.status != Applied {
		return NewProbe(result.status, "", "", result.message)
	}
	return ParseVersion(a.config, VersionOutput{result.stdout, result.stderr, result.exitCode})
}

type processResult struct {
	status                  Status
	stdout, stderr, message string
	exitCode                int
}

func contained(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func resolvedPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(absolute)
}

func (a *Adapter) binary() (string, error) {
	configured := a.config.parameters.Binary
	if filepath.IsAbs(configured) || strings.ContainsRune(configured, filepath.Separator) {
		if strings.HasPrefix(configured, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			configured = filepath.Join(home, configured[2:])
		}
	} else {
		found, err := exec.LookPath(configured)
		if err != nil && !errors.Is(err, exec.ErrDot) {
			return "", err
		}
		configured = found
	}
	path, err := resolvedPath(configured)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", os.ErrNotExist
	}
	return path, nil
}

func temporaryParent(root string) (string, error) {
	for _, path := range []string{"/tmp", "/var/tmp"} {
		candidate, err := resolvedPath(path)
		if err != nil {
			continue
		}
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() && !contained(root, candidate) {
			return candidate, nil
		}
	}
	return "", errors.New("no trusted temporary directory is available outside workspace")
}

func (a *Adapter) environment(root string) ([]string, error) {
	names := append([]string{"LANG", "LC_ALL", "SYSTEMROOT"}, a.config.parameters.EnvPassthrough...)
	values := make(map[string]string)
	for _, name := range names {
		if value, ok := os.LookupEnv(name); ok {
			values[name] = value
		}
	}
	values["NO_COLOR"] = "1"
	if root != "" {
		parent, err := temporaryParent(root)
		if err != nil {
			return nil, err
		}
		values["TMPDIR"] = parent
	}
	environment := make([]string, 0, len(values))
	for name, value := range values {
		environment = append(environment, name+"="+value)
	}
	return environment, nil
}

func (a *Adapter) integrity(ctx context.Context, binary, root string) (string, error) {
	if root != "" && contained(root, binary) {
		return "ast-outline executable must live outside the model-visible workspace", nil
	}
	expected := a.config.parameters.ExpectedSHA256
	if expected == nil {
		return "", ctx.Err()
	}
	file, err := os.Open(binary)
	if err != nil {
		return "could not verify ast-outline executable: " + filesystemErrorClass(err), nil
	}
	defer file.Close()
	digest := sha256.New()
	buffer := make([]byte, 128*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		count, err := file.Read(buffer)
		if count > 0 {
			_, _ = digest.Write(buffer[:count])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "could not verify ast-outline executable: " + filesystemErrorClass(err), nil
		}
	}
	actual := hex.EncodeToString(digest.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(actual), []byte(*expected)) != 1 {
		return "ast-outline executable SHA-256 does not match the pinned digest", nil
	}
	return "", ctx.Err()
}

func filesystemErrorClass(err error) string {
	if errors.Is(err, os.ErrNotExist) {
		return "FileNotFoundError"
	}
	if errors.Is(err, os.ErrPermission) {
		return "PermissionError"
	}
	return "OSError"
}

type drainResult struct {
	stream   int
	data     []byte
	overflow bool
	err      error
}

func capture(reader *os.File, stream, limit int, overflow chan<- struct{}, results chan<- drainResult) {
	defer reader.Close()
	result := drainResult{stream: stream}
	buffer := make([]byte, 65_536)
	for {
		// Popen's buffered stream.read(65_536) fills the requested block or
		// waits for EOF. Preserve that ordering when a small over-limit child
		// keeps its pipe open: the common deadline can win before overflow.
		count, err := io.ReadFull(reader, buffer)
		remaining := limit - len(result.data)
		if count > remaining {
			result.data = append(result.data, buffer[:remaining]...)
			result.overflow = true
			select {
			case overflow <- struct{}{}:
			default:
			}
			break
		}
		result.data = append(result.data, buffer[:count]...)
		if err != nil {
			if err != io.EOF && err != io.ErrUnexpectedEOF {
				result.err = err
			}
			break
		}
	}
	results <- result
}

func decodeCapture(data []byte) string {
	var result strings.Builder
	for len(data) > 0 {
		r, size, _ := pytext.DecodeRune(data)
		result.WriteRune(r)
		data = data[size:]
	}
	return result.String()
}

func comma(value int) string {
	text := strconv.Itoa(value)
	for i := len(text) - 3; i > 0; i -= 3 {
		text = text[:i] + "," + text[i:]
	}
	return text
}

func (a *Adapter) run(ctx context.Context, workspace string, args []string) (processResult, error) {
	if a == nil || !a.config.valid {
		return processResult{}, errors.New("AST context configuration is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return processResult{}, err
	}
	binary, err := a.binary()
	if err != nil {
		return processResult{status: Missing, message: "ast-outline executable not found: " + a.config.parameters.Binary}, nil
	}
	root := ""
	if workspace != "" {
		root, err = resolvedPath(workspace)
		if err != nil {
			return processResult{status: Error, message: err.Error()}, nil
		}
	}
	message, err := a.integrity(ctx, binary, root)
	if err != nil {
		return processResult{}, err
	}
	if message != "" {
		return processResult{status: Incompatible, message: message}, nil
	}
	environment, err := a.environment(root)
	if err != nil {
		return processResult{status: Error, message: err.Error()}, nil
	}
	if err := ctx.Err(); err != nil {
		return processResult{}, err
	}
	cmd := exec.Command(binary, args...)
	cmd.Env, cmd.Dir = environment, root
	configureGroup(cmd)
	outReader, outWriter, err := os.Pipe()
	if err != nil {
		return processResult{}, err
	}
	defer outReader.Close()
	defer outWriter.Close()
	errReader, errWriter, err := os.Pipe()
	if err != nil {
		return processResult{}, err
	}
	defer errReader.Close()
	defer errWriter.Close()
	cmd.Stdout, cmd.Stderr = outWriter, errWriter
	if err := cmd.Start(); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return processResult{status: Missing, message: "ast-outline executable not found: " + binary}, nil
		}
		return processResult{status: Error, message: "could not start ast-outline: " + err.Error()}, nil
	}
	outWriter.Close()
	errWriter.Close()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	drains := make(chan drainResult, 2)
	overflow := make(chan struct{}, 1)
	go capture(outReader, 0, a.config.parameters.MaxOutputBytes, overflow, drains)
	go capture(errReader, 1, a.config.parameters.MaxOutputBytes, overflow, drains)
	timer := time.NewTimer(a.config.timeout)
	defer timer.Stop()
	var results [2]drainResult
	drainCount := 0
	processDone := false
	timedOut := false
	var cancelled error
	var cleanup *time.Timer
	var cleanupDeadline <-chan time.Time
	stop := func() {
		killGroup(cmd.Process)
		if cleanup == nil {
			cleanup = time.NewTimer(2 * time.Second)
			cleanupDeadline = cleanup.C
		}
	}
	defer func() {
		if cleanup != nil {
			cleanup.Stop()
		}
	}()
	deadline := timer.C
	caller := ctx.Done()
	for !processDone || drainCount < 2 {
		select {
		case <-waited:
			processDone = true
			waited = nil
		case result := <-drains:
			results[result.stream] = result
			drainCount++
			if result.overflow {
				stop()
			}
		case <-overflow:
			stop()
		case <-deadline:
			timedOut = true
			deadline = nil
			stop()
		case <-caller:
			cancelled = ctx.Err()
			caller = nil
			stop()
		case <-cleanupDeadline:
			// Escaped descendants may keep descriptors open; own and close both
			// reads, then join their finite drains without waiting for foreign IO.
			outReader.Close()
			errReader.Close()
			cleanupDeadline = nil
		}
	}
	if cancelled != nil {
		return processResult{}, cancelled
	}
	if err := ctx.Err(); err != nil {
		return processResult{}, err
	}
	if cmd.ProcessState == nil {
		return processResult{status: Error, message: "could not wait for ast-outline process"}, nil
	}
	result := processResult{status: Applied, stdout: decodeCapture(results[0].data), stderr: decodeCapture(results[1].data), exitCode: processExitCode(cmd.ProcessState)}
	if timedOut {
		result.status = Error
		result.message = "ast-outline timed out after " + strconv.FormatFloat(a.config.parameters.TimeoutSeconds, 'g', 6, 64) + "s"
	} else if results[0].overflow || results[1].overflow {
		result.status = Error
		result.message = "ast-outline output exceeded " + comma(a.config.parameters.MaxOutputBytes) + " bytes"
	} else if results[0].err != nil || results[1].err != nil {
		result.status = Error
		result.message = "could not read ast-outline output"
	}
	return result, nil
}
