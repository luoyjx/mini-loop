package astcontext

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestASTDecimalConversionsMatchSourceInvocationAndPublicOperation(t *testing.T) {
	type observation struct {
		ResultJSON string `json:"result_json"`
		ErrorClass string `json:"error_class"`
		Error      string
		Calls      []operationAudit
	}
	var fixture struct {
		Invocations []struct {
			Name, Stdout   string
			Invoke, Public observation
		}
	}
	raw, err := os.ReadFile("../testdata/python-ast-integers.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Invocations) != 12 {
		t.Fatal("Source integer inventory changed")
	}
	p, workspace := nativeAdapter(t)
	root, err := astWorkspace(context.Background(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "code.py"), []byte("class One: pass\n"), 0600); err != nil {
		t.Fatal(err)
	}
	calls, payload := filepath.Join(t.TempDir(), "calls"), filepath.Join(t.TempDir(), "stdout")
	p.EnvPassthrough = append(p.EnvPassthrough, "AST_CALLS", "AST_PAYLOAD_FILE")
	t.Setenv("AST_MODE", "operation")
	t.Setenv("AST_CALLS", calls)
	t.Setenv("AST_PAYLOAD_FILE", payload)
	a := adapter(t, p)
	probe, _ := NewProbe(Applied, "1.9.0", Homepage, "")
	compare := func(t *testing.T, result Result, err error, want observation) {
		t.Helper()
		if want.ErrorClass != "" {
			var conversion PayloadIntegerError
			if want.ErrorClass != "ValueError" || !errors.As(err, &conversion) || !errors.Is(err, jsonvalue.ErrInteger) || err.Error() != want.Error {
				t.Fatal("conversion fault", err, want.Error)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		value, err := result.Value()
		if err != nil {
			t.Fatal(err)
		}
		text, err := jsonvalue.AppendLegacy(nil, value)
		if err != nil {
			t.Fatal(err)
		}
		if string(text) != want.ResultJSON {
			t.Fatalf("got %s\nwant %s", text, want.ResultJSON)
		}
	}
	for _, row := range fixture.Invocations {
		t.Run(row.Name, func(t *testing.T) {
			result, err := classifyPayload(FileOutline, probe, processResult{status: Applied, stdout: row.Stdout}, nil, nil, SchemaVersion)
			compare(t, result, err, row.Invoke)
			if err = os.WriteFile(payload, []byte(row.Stdout), 0600); err != nil {
				t.Fatal(err)
			}
			if err = os.Remove(calls); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			result, err = a.FileOutline(context.Background(), root, FileOutlineRequest{Paths: []string{"code.py"}})
			compare(t, result, err, row.Public)
			data, err := os.ReadFile(calls)
			if err != nil {
				t.Fatal(err)
			}
			observed := []operationAudit{}
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				var call operationAudit
				if err = json.Unmarshal([]byte(line), &call); err != nil {
					t.Fatal(err)
				}
				if call.Cwd != root {
					t.Fatal("wrong working directory", call.Cwd, root)
				}
				call.Cwd = "<workspace>"
				for i, arg := range call.Args {
					if strings.Contains(arg, "/mini-loop-ast-") {
						if _, err = os.Stat(arg); !os.IsNotExist(err) {
							t.Fatal("conversion failure retained snapshot", arg, err)
						}
						_, suffix, ok := strings.Cut(arg, "/workspace")
						if !ok {
							t.Fatal(arg)
						}
						call.Args[i] = "<snapshot>" + suffix
					}
				}
				observed = append(observed, call)
			}
			if !reflect.DeepEqual(observed, row.Public.Calls) {
				t.Fatal(observed, row.Public.Calls)
			}
		})
	}
}
