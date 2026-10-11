package mcp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/secrets"
)

const DefaultTimeout = 60 * time.Second

// SecretEnvironment is the credential selection seam, shared with Registry/Null.
type SecretEnvironment interface {
	Names() []secrets.Name
	ScrubEnvironment(secrets.Environment) secrets.Environment
}

type StdioConfig struct {
	Name                   string
	Command                []string
	Secrets                SecretEnvironment
	EnvironmentPassthrough []secrets.Name
	Timeout                time.Duration
}

type frame struct {
	line string
	err  error
}
type process struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   *os.File
	frames   chan frame
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

// StdioClient serializes RPCs, reuses a live child, and restarts a dead child only
// on the next operation. A failed in-flight tools/call is never replayed.
// Close is reusable: the next operation can start a new child, as in Python.
type StdioClient struct {
	config    StdioConfig
	startGate chan struct{}
	rpcGate   chan struct{}
	mu        sync.Mutex
	child     *process
	withheld  []secrets.Name
	nextID    int64
}

func NewStdio(config StdioConfig) (*StdioClient, error) {
	if len(config.Command) == 0 || config.Command[0] == "" {
		return nil, errors.New("MCP command is required")
	}
	if config.Timeout == 0 {
		config.Timeout = DefaultTimeout
	}
	if config.Timeout < 0 {
		return nil, errors.New("MCP timeout must be positive")
	}
	if config.Secrets == nil {
		config.Secrets = secrets.Null{}
	}
	config.Command = append([]string(nil), config.Command...)
	config.EnvironmentPassthrough = append([]secrets.Name(nil), config.EnvironmentPassthrough...)
	return &StdioClient{config: config, startGate: make(chan struct{}, 1), rpcGate: make(chan struct{}, 1)}, nil
}

func (c *StdioClient) Name() string { return c.config.Name }
func (c *StdioClient) Withheld() []secrets.Name {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]secrets.Name(nil), c.withheld...)
}
func acquire(ctx context.Context, gate chan struct{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	select {
	case gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func alive(p *process) bool {
	if p == nil {
		return false
	}
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (c *StdioClient) environment() []string {
	current := secrets.CurrentEnvironment()
	environment := c.config.Secrets.ScrubEnvironment(current)
	passed := make(map[secrets.Name]bool)
	for _, name := range c.config.EnvironmentPassthrough {
		passed[name] = true
		if value, ok := current[name]; ok {
			environment[name] = value
		}
	}
	var withheld []secrets.Name
	for _, name := range c.config.Secrets.Names() {
		if !passed[name] {
			withheld = append(withheld, name)
		}
	}
	sort.Slice(withheld, func(i, j int) bool { return withheld[i] < withheld[j] })
	c.mu.Lock()
	c.withheld = withheld
	c.mu.Unlock()
	names := make([]string, 0, len(environment))
	for name, value := range environment {
		names = append(names, string(name)+"="+value)
	}
	sort.Strings(names)
	return names
}

func spawn(command []string, environment []string) (*process, error) {
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Env, cmd.Stderr = environment, os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	// Own stdout independently: Cmd.Wait must not discard an unread final frame.
	reader, writer, err := os.Pipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	cmd.Stdout = writer
	if err = cmd.Start(); err != nil {
		stdin.Close()
		reader.Close()
		writer.Close()
		return nil, err
	}
	writer.Close()
	p := &process{cmd: cmd, stdin: stdin, stdout: reader, frames: make(chan frame, 1), stop: make(chan struct{}), done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(p.done) }()
	go func() {
		defer reader.Close()
		defer close(p.frames)
		input := bufio.NewReaderSize(reader, 64*1024)
		for {
			line, err := readFrame(input)
			if err == io.EOF {
				return
			}
			select {
			case p.frames <- frame{line: line, err: err}:
			case <-p.stop:
				return
			}
			if err != nil && !errors.Is(err, errLineLimit) && !errors.Is(err, errFrameUTF8) {
				return
			}
		}
	}()
	return p, nil
}

var errFrameUTF8 = errors.New("MCP stdout is not valid UTF-8")

var errLineLimit = fmt.Errorf("MCP message larger than %s bytes", comma(MaxRPCLine))

// Drain an oversized line without retaining it, then resume at the next frame.
// Python readline also discards its offending line after a limit exception.
func readFrame(reader *bufio.Reader) (string, error) {
	var line []byte
	oversized := false
	for {
		chunk, err := reader.ReadSlice('\n')
		length := len(chunk)
		if length > 0 && chunk[length-1] == '\n' {
			length--
		}
		if !oversized {
			if len(line)+length > MaxRPCLine {
				oversized = true
				line = nil
			} else {
				line = append(line, chunk[:length]...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if oversized {
			return "", errLineLimit
		}
		if err != nil && err != io.EOF {
			return "", err
		}
		if err == io.EOF && len(line) == 0 {
			return "", io.EOF
		}
		if !utf8.Valid(line) {
			return "", errFrameUTF8
		}
		return string(line), nil
	}
}

func stopProcess(p *process, immediate bool) {
	if p == nil {
		return
	}
	p.stopOnce.Do(func() { close(p.stop); p.stdin.Close(); p.stdout.Close() })
	if !alive(p) {
		return
	}
	if immediate {
		_ = p.cmd.Process.Kill()
	} else {
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
	}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-p.done:
	case <-timer.C:
		_ = p.cmd.Process.Kill()
		<-p.done
	}
}

func (c *StdioClient) start(ctx context.Context) (*process, error) {
	if err := acquire(ctx, c.startGate); err != nil {
		return nil, err
	}
	defer func() { <-c.startGate }()
	c.mu.Lock()
	p := c.child
	c.mu.Unlock()
	if alive(p) {
		return p, nil
	}
	stopProcess(p, true)
	p, err := spawn(c.config.Command, c.environment())
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.child = p
	c.mu.Unlock()
	handshakeCtx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	params := object(field("protocolVersion", jsonvalue.TextValue("2024-11-05")), field("capabilities", object()), field("clientInfo", object(field("name", jsonvalue.TextValue("mini-loop")), field("version", jsonvalue.TextValue("0.1.0")))))
	_, err = c.rpc(handshakeCtx, p, "initialize", params)
	if err == nil {
		err = c.write(handshakeCtx, p, object(field("jsonrpc", jsonvalue.TextValue("2.0")), field("method", jsonvalue.TextValue("notifications/initialized")), field("params", object())))
	}
	if err != nil {
		c.mu.Lock()
		if c.child == p {
			c.child = nil
		}
		c.mu.Unlock()
		stopProcess(p, true)
		return nil, err
	}
	return p, nil
}

func (c *StdioClient) write(ctx context.Context, p *process, message jsonvalue.Value) error {
	data, err := jsonvalue.AppendLegacyDefault(message)
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { _, err := p.stdin.Write(append(data, '\n')); done <- err }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		// A blocked pipe write cannot safely be abandoned with a partial request.
		stopProcess(p, true)
		<-done
		return ctx.Err()
	}
}

func (c *StdioClient) rpc(ctx context.Context, p *process, method string, params jsonvalue.Value) (jsonvalue.Value, error) {
	if err := acquire(ctx, c.rpcGate); err != nil {
		return jsonvalue.Value{}, err
	}
	defer func() { <-c.rpcGate }()
	c.nextID++
	id := c.nextID
	err := c.write(ctx, p, object(field("jsonrpc", jsonvalue.TextValue("2.0")), field("id", jsonvalue.IntegerValue(id)), field("method", jsonvalue.TextValue(method)), field("params", params)))
	if err != nil {
		return jsonvalue.Value{}, err
	}
	for {
		select {
		case <-ctx.Done():
			return jsonvalue.Value{}, ctx.Err()
		case frame, ok := <-p.frames:
			if !ok {
				return jsonvalue.Value{}, fmt.Errorf("MCP server '%s' closed stdout", c.Name())
			}
			if frame.err != nil {
				return jsonvalue.Value{}, frame.err
			}
			response, err := jsonvalue.Decode(frame.line)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			if response.Kind() != jsonvalue.Object {
				return jsonvalue.Value{}, errors.New("MCP response must be an object")
			}
			responseID, _ := response.Lookup("id")
			// Python compares numeric ids, including booleans and integral floats.
			matching := false
			switch responseID.Kind() {
			case jsonvalue.Integer:
				text, _ := responseID.Integer()
				matching = text == fmt.Sprint(id)
			case jsonvalue.Float:
				value, _ := responseID.Float()
				matching = value == float64(id)
			case jsonvalue.Boolean:
				value, _ := responseID.Bool()
				matching = value && id == 1
			}
			if !matching {
				continue
			}
			if fault, present := response.Lookup("error"); present {
				return jsonvalue.Value{}, fmt.Errorf("MCP error: %s", fault.PythonString())
			}
			if result, present := response.Lookup("result"); present {
				return result, nil
			}
			return object(), nil
		}
	}
}

func (c *StdioClient) ListTools(ctx context.Context) ([]ToolDescription, error) {
	p, err := c.start(ctx)
	if err != nil {
		return nil, err
	}
	bounded, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	result, err := c.rpc(bounded, p, "tools/list", object())
	if err != nil {
		return nil, err
	}
	return decodeTools(result)
}

// CallTool accepts only closed object arguments. Its context carries the caller's
// execution deadline; source applies the tool timeout in the registration layer.
func (c *StdioClient) CallTool(ctx context.Context, name string, arguments jsonvalue.Value) (string, error) {
	if arguments.Kind() != jsonvalue.Object {
		return "", errors.New("MCP arguments must be an object")
	}
	p, err := c.start(ctx)
	if err != nil {
		return "", err
	}
	result, err := c.rpc(ctx, p, "tools/call", object(field("name", jsonvalue.TextValue(name)), field("arguments", arguments)))
	if err != nil {
		return "", err
	}
	return renderResult(result)
}
func (c *StdioClient) Close() error {
	if err := acquire(context.Background(), c.startGate); err != nil {
		return err
	}
	defer func() { <-c.startGate }()
	c.mu.Lock()
	p := c.child
	c.child = nil
	c.mu.Unlock()
	stopProcess(p, false)
	return nil
}
