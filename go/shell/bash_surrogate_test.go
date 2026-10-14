package shell_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/sandbox"
	"github.com/luoyjx/mini-loop/go/shell"
)

type sourceArgvException struct {
	Class      string `json:"exception_class"`
	Message    string `json:"message"`
	Encoding   string `json:"encoding"`
	Reason     string `json:"reason"`
	Start      int    `json:"start"`
	End        int    `json:"end"`
	Argument   int    `json:"argument_index"`
	ObjectJSON string `json:"object_json_ascii"`
}

type sourceArgvResult struct {
	Stdout       string  `json:"stdout"`
	Stderr       string  `json:"stderr"`
	ExitCode     *int    `json:"exit_code"`
	TimedOut     bool    `json:"timed_out"`
	Overflowed   bool    `json:"overflowed"`
	Error        *string `json:"error"`
	Projection   *string `json:"projection"`
	CaptureLimit int     `json:"capture_limit"`
	Rendered     string  `json:"rendered"`
}

type sourceArgvCase struct {
	Name                    string               `json:"name"`
	Mode                    string               `json:"sandbox_mode"`
	CommandJSON             string               `json:"command_json_ascii"`
	SelectedJSON            string               `json:"selected_argv_json_ascii"`
	SandboxCalls            int                  `json:"sandbox_calls"`
	SandboxReceivedOriginal bool                 `json:"sandbox_received_original"`
	Result                  *sourceArgvResult    `json:"result"`
	Exception               *sourceArgvException `json:"exception"`
	MarkerHex               *string              `json:"marker_hex"`
	ArgumentHex             *string              `json:"argument_hex"`
}

type sourceArgvFixture struct {
	Encoding       string            `json:"filesystem_encoding"`
	Errors         string            `json:"filesystem_errors"`
	Cases          []sourceArgvCase  `json:"cases"`
	SourceSHA256   map[string]string `json:"source_sha256"`
	ProducerSHA256 string            `json:"producer_sha256"`
	Scope          []string          `json:"scope"`
}

type observedArgvSandbox struct {
	mode     string
	selected []string
	calls    int
	received string
}

func (s *observedArgvSandbox) ForWorkspace(string) (shell.Sandbox, error) { return s, nil }
func (s *observedArgvSandbox) Argv(command string) ([]string, error) {
	s.calls++
	s.received = command
	if s.mode == "null" {
		return (&sandbox.NullSandbox{}).Argv(command)
	}
	return append([]string(nil), s.selected...), nil
}

type argvEncodingFault interface {
	error
	ExceptionClass() string
	ArgumentIndex() int
	EncodingRange() (int, int)
}

func sourceArgvText(t *testing.T, encoded string) string {
	t.Helper()
	value, err := jsonvalue.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	text, ok := value.Text()
	if !ok {
		t.Fatal("fixture is not a text scalar")
	}
	return text
}

func sourceArgvList(t *testing.T, encoded string) []string {
	t.Helper()
	value, err := jsonvalue.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	items, ok := value.Array()
	if !ok {
		t.Fatal("fixture is not an argv array")
	}
	result := make([]string, len(items))
	for i, item := range items {
		var valid bool
		result[i], valid = item.Text()
		if !valid {
			t.Fatal("fixture argv element is not text")
		}
	}
	return result
}

func observedFileHex(t *testing.T, root, name string) *string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	value := hex.EncodeToString(data)
	return &value
}

func TestActualPythonSelectedArgvFilesystemEncoding(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-bash-surrogates.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture sourceArgvFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Encoding != "utf-8" || fixture.Errors != "surrogateescape" || len(fixture.Cases) != 12 {
		t.Fatal("Source filesystem profile changed")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			root := t.TempDir()
			command := sourceArgvText(t, row.CommandJSON)
			backend := &observedArgvSandbox{mode: row.Mode, selected: sourceArgvList(t, row.SelectedJSON)}
			executor, err := shell.New(shell.Config{Workspace: root, Timeout: 3 * time.Second, Sandbox: backend})
			if err != nil {
				t.Fatal(err)
			}
			result, err := executor.ExecuteBashResult(context.Background(), protocol.BashInput{Command: command})
			marker, argument := observedFileHex(t, root, "effect.marker"), observedFileHex(t, root, "argument.bytes")
			if !reflect.DeepEqual(marker, row.MarkerHex) || !reflect.DeepEqual(argument, row.ArgumentHex) {
				show := func(value *string) string {
					if value == nil {
						return "absent"
					}
					return *value
				}
				t.Errorf("actual subprocess effects marker=%s argument=%s; Source marker=%s argument=%s", show(marker), show(argument), show(row.MarkerHex), show(row.ArgumentHex))
			}
			if backend.calls != row.SandboxCalls || !row.SandboxReceivedOriginal || backend.received != command {
				t.Fatal("input was rejected or changed before sandbox selection")
			}
			if row.Exception != nil {
				if err == nil {
					t.Fatalf("Source failed before a child could run; native returned %+v", result)
				}
				var fault argvEncodingFault
				if !errors.As(err, &fault) || fault.ExceptionClass() != row.Exception.Class || fault.ArgumentIndex() != row.Exception.Argument || err.Error() != row.Exception.Message {
					t.Fatalf("filesystem fault differs: %T %v; Source %+v", err, err, row.Exception)
				}
				start, end := fault.EncodingRange()
				if start != row.Exception.Start || end != row.Exception.End || result.ExitCode != nil || result.Error != nil {
					t.Fatal("pre-child fault location/result differs from actual Source")
				}
			} else {
				if err != nil || row.Result == nil {
					t.Fatal("Source-successful argv was refused", err)
				}
				actual := sourceArgvResult{Stdout: result.Stdout, Stderr: result.Stderr, ExitCode: result.ExitCode, TimedOut: result.TimedOut, Overflowed: result.Overflowed, Error: result.Error, Projection: result.Projection, CaptureLimit: result.CaptureLimit, Rendered: result.Render()}
				if !reflect.DeepEqual(actual, *row.Result) {
					t.Fatalf("actual structured result %+v; Source %+v", actual, *row.Result)
				}
			}
		})
	}
}

func TestRawNativeArgumentBytesKeepExistingEmbeddingSemantics(t *testing.T) {
	root := t.TempDir()
	executor, err := shell.New(shell.Config{Workspace: root, Timeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	// These raw OS bytes are a native embedding input, not a Source Unicode
	// claim. A blanket utf8.ValidString gate would change this existing path.
	command := "printf '%s' '" + string([]byte{0x80, 0xff}) + "' > argument.bytes"
	result, err := executor.ExecuteBashResult(context.Background(), protocol.BashInput{Command: command})
	want := "80ff"
	if err != nil || result.Failed() || !reflect.DeepEqual(observedFileHex(t, root, "argument.bytes"), &want) {
		t.Fatal("unrelated native byte arguments were changed", result, err)
	}
}
