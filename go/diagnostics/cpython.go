package diagnostics

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const CPythonVersion = "3.11.3"
const MaxParserSourceBytes = 3 * MaxFileBytes
const parserOutputLimit = 64 * 1024
const parserDeadline = 5 * time.Second

// All instances share the process budget. Waiting honors the same deadline.
var cpythonProcesses = make(chan struct{}, 2)

// CPythonParser owns an immutable executable selection, not a persistent child.
// Construction and every request verify the exact approved ast.parse profile.
type CPythonParser struct{ executable string }

// ParserFault preserves an actual non-SyntaxError. It cannot become a clean
// SyntaxResult or a synthesized filesystem warning.
type ParserFault struct{ class, message string }

func (fault *ParserFault) Class() string { return fault.class }
func (fault *ParserFault) Error() string { return fault.message }

type cpythonReply struct {
	Implementation string `json:"implementation"`
	Version        string `json:"version"`
	Kind           string `json:"kind"`
	Class          string `json:"class"`
	Line           int    `json:"line"`
	Message        string `json:"message"`
}

// NewCPythonParser selects python3.11 when executable is empty. Missing or
// different runtimes fail before Manager/Launcher resource construction.
// An explicit executable belongs to the trusted embedding caller and must honor
// the fixed isolated helper without launching independent background work.
func NewCPythonParser(ctx context.Context, executable string) (*CPythonParser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if executable == "" {
		executable = "python3.11"
	}
	path, err := exec.LookPath(executable)
	if err != nil {
		return nil, errors.New("CPython 3.11.3 diagnostics interpreter is unavailable")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, errors.New("CPython diagnostics interpreter path is invalid")
	}
	parser := &CPythonParser{executable: path}
	reply, err := parser.run(ctx, "probe", "")
	if err != nil {
		return nil, err
	}
	if reply.Kind != "ready" || reply.Class != "" || reply.Line != 0 || reply.Message != "" {
		return nil, errors.New("CPython diagnostics interpreter returned an invalid probe")
	}
	return parser, nil
}

func NewDefaultPythonSyntaxDiagnostics(ctx context.Context) (*PythonSyntaxDiagnostics, error) {
	parser, err := NewCPythonParser(ctx, "")
	if err != nil {
		return nil, err
	}
	return NewPythonSyntaxDiagnostics(parser)
}

func (parser *CPythonParser) Parse(ctx context.Context, source string) (SyntaxResult, error) {
	if err := ctx.Err(); err != nil {
		return SyntaxResult{}, err
	}
	if len(source) > MaxParserSourceBytes {
		return SyntaxResult{}, errors.New("CPython parser source exceeds its byte budget")
	}
	// Base64 retains surrogatepass text. encoding/json would replace lone
	// surrogate bytes before ast.parse could observe the approved fault.
	reply, err := parser.run(ctx, "parse", source)
	if err != nil {
		return SyntaxResult{}, err
	}
	switch reply.Kind {
	case "clean":
		if reply.Class == "" && reply.Line == 0 && reply.Message == "" {
			return CleanSyntax(), nil
		}
	case "syntax":
		if (reply.Class == "SyntaxError" || reply.Class == "IndentationError" || reply.Class == "TabError") && reply.Line >= 0 && reply.Message != "" {
			return NewSyntaxProblem(reply.Line, reply.Message), nil
		}
	case "fault":
		if reply.Class != "" && reply.Line == 0 {
			return SyntaxResult{}, &ParserFault{class: reply.Class, message: reply.Message}
		}
	}
	return SyntaxResult{}, errors.New("CPython parser returned an invalid observation")
}

type parserCapture struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
	cancel   context.CancelFunc
}

func (capture *parserCapture) Write(data []byte) (int, error) {
	remaining := capture.limit - capture.buffer.Len()
	if len(data) > remaining {
		_, _ = capture.buffer.Write(data[:remaining])
		capture.overflow = true
		capture.cancel()
		return remaining, errors.New("CPython parser output exceeds its byte budget")
	}
	return capture.buffer.Write(data)
}

func (parser *CPythonParser) run(ctx context.Context, operation, input string) (cpythonReply, error) {
	if parser == nil || parser.executable == "" {
		return cpythonReply{}, errors.New("CPython diagnostics parser is not bound")
	}
	bounded, cancel := context.WithTimeout(ctx, parserDeadline)
	defer cancel()
	select {
	case cpythonProcesses <- struct{}{}:
		defer func() { <-cpythonProcesses }()
	case <-bounded.Done():
		return cpythonReply{}, bounded.Err()
	}
	// Do not allocate an encoded copy for callers waiting in the shared queue.
	// At most two admitted operations own the additional base64 input buffer.
	if operation == "parse" {
		input = base64.StdEncoding.EncodeToString([]byte(input))
	}
	command := exec.CommandContext(bounded, parser.executable, "-I", "-S", "-B", "-c", cpythonHelper, operation)
	command.Env = []string{}
	command.Stdin = strings.NewReader(input)
	configureParserProcess(command)
	command.Cancel = func() error { killParserProcess(command.Process); return nil }
	command.WaitDelay = time.Second
	output := &parserCapture{limit: parserOutputLimit, cancel: cancel}
	stderr := &parserCapture{limit: 4096, cancel: cancel}
	command.Stdout, command.Stderr = output, stderr
	err := command.Run()
	// Run joined the interpreter and pipe copies before this slot can be reused.
	// The fixed helper never forks. This is not arbitrary wrapper-tree supervision.
	if ctx.Err() != nil {
		return cpythonReply{}, ctx.Err()
	}
	if output.overflow || stderr.overflow {
		return cpythonReply{}, errors.New("CPython parser output exceeds its byte budget")
	}
	if bounded.Err() != nil {
		return cpythonReply{}, bounded.Err()
	}
	if err != nil {
		return cpythonReply{}, errors.New("CPython diagnostics process failed")
	}
	var reply cpythonReply
	decoder := json.NewDecoder(bytes.NewReader(output.buffer.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&reply); err != nil {
		return cpythonReply{}, errors.New("CPython parser returned an invalid response")
	}
	if err := decoder.Decode(new(cpythonReply)); !errors.Is(err, io.EOF) {
		return cpythonReply{}, errors.New("CPython parser returned trailing output")
	}
	if reply.Implementation != "cpython" || reply.Version != CPythonVersion {
		return cpythonReply{}, fmt.Errorf("CPython diagnostics requires cpython %s", CPythonVersion)
	}
	return reply, nil
}

// -I excludes cwd/PYTHONPATH and user site configuration, -S skips site startup,
// -B avoids bytecode writes. The empty environment carries no application secrets.
// Source is data for ast.parse; it is never imported, compiled or executed.
const cpythonHelper = `import ast, base64, json, sys
if sys.platform in ("darwin", "linux"):
    import resource
    def bound(which, soft, hard):
        inherited_soft, inherited_hard = resource.getrlimit(which)
        if inherited_soft != resource.RLIM_INFINITY:
            soft = min(soft, inherited_soft)
        if inherited_hard != resource.RLIM_INFINITY:
            hard = min(hard, inherited_hard)
        resource.setrlimit(which, (min(soft, hard), hard))
    bound(resource.RLIMIT_CPU, 4, 5)
    if sys.platform == "linux":
        bound(resource.RLIMIT_AS, 512 * 1024 * 1024, 512 * 1024 * 1024)
reply = dict(implementation=sys.implementation.name,
             version=".".join(str(n) for n in sys.version_info[:3]),
             kind="ready", **{"class": ""}, line=0, message="")
if reply["implementation"] == "cpython" and reply["version"] == "3.11.3" and sys.argv[1] == "parse":
    data = sys.stdin.buffer.read(4000001)
    if len(data) > 4000000:
        raise ValueError("parser input budget exceeded")
    source = base64.b64decode(data, validate=True).decode("utf-8", "surrogatepass")
    try:
        ast.parse(source)
        reply["kind"] = "clean"
    except SyntaxError as error:
        reply.update(kind="syntax", line=int(error.lineno or 0), message=error.msg or "syntax error")
        reply["class"] = type(error).__name__
    except Exception as error:
        reply.update(kind="fault", message=str(error))
        reply["class"] = type(error).__name__
sys.stdout.write(json.dumps(reply, ensure_ascii=True, separators=(",", ":")))
`
