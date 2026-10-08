package astcontext

import (
	"encoding/json"
	"errors"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"os"
	"strings"
	"testing"
)

func TestCapturedPayloadsMatchActualSourceInvocations(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-ast-payloads.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Invocations []struct {
			Case struct {
				Name, Command, Stdout, Stderr string
				ExitCode                      int   `json:"exit_code"`
				IncludeImports                *bool `json:"include_imports"`
			}
			Operation  string
			ResultJSON string `json:"result_json"`
			UTF8JSON   string `json:"utf8_json"`
			ErrorClass string `json:"error_class"`
			Error      string
		}
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Invocations) != 65 {
		t.Fatal("Source inventory changed")
	}
	probe, _ := NewProbe(Applied, "1.9.0", Homepage, "")
	success, faults := 0, 0
	for _, row := range fixture.Invocations {
		t.Run(row.Case.Name, func(t *testing.T) {
			op, err := ParseOperation(row.Operation)
			if err != nil {
				t.Fatal(err)
			}
			if op.command() != row.Case.Command {
				t.Fatal("operation command mismatch")
			}
			result, err := classifyPayload(op, probe, processResult{status: Applied, stdout: row.Case.Stdout, stderr: row.Case.Stderr, exitCode: row.Case.ExitCode}, row.Case.IncludeImports, []snapshotRewrite{{snapshot: "/private/frozen/workspace", workspace: "<workspace>"}}, SchemaVersion)
			if row.ErrorClass != "" {
				faults++
				var iteration PayloadIterationError
				if row.ErrorClass != "TypeError" || !errors.As(err, &iteration) || err.Error() != row.Error {
					t.Fatal(err, row.Error)
				}
				return
			}
			success++
			if err != nil {
				t.Fatal(err)
			}
			value, err := result.Value()
			if err != nil {
				t.Fatal(err)
			}
			ascii, err := jsonvalue.AppendLegacy(nil, value)
			if err != nil {
				t.Fatal(err)
			}
			if string(ascii) != row.ResultJSON {
				t.Fatalf("ASCII projection\ngot %s\nwant %s", ascii, row.ResultJSON)
			}
			utf8, err := result.ToJSON()
			if err != nil {
				t.Fatal(err)
			}
			if utf8 != row.UTF8JSON {
				t.Fatalf("UTF8 projection\ngot %s\nwant %s", utf8, row.UTF8JSON)
			}
		})
	}
	if success != 61 || faults != 4 {
		t.Fatal(success, faults)
	}
}

func TestResultOwnershipAndPublication(t *testing.T) {
	body, err := jsonvalue.Decode(`{"files":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := NewPayload(body)
	probe, _ := NewProbe(Applied, "1.9.0", Homepage, "")
	warnings := []string{"original"}
	result, err := NewResult(ResultParameters{Status: Partial, Operation: FileOutline, Data: &payload, Probe: &probe, Warnings: warnings})
	if err != nil {
		t.Fatal(err)
	}
	warnings[0] = "changed"
	payload = Payload{}
	probe = Probe{}
	p := result.Parameters()
	p.Warnings[0] = "changed again"
	*p.Data = Payload{}
	*p.Probe = Probe{}
	current := result.Parameters()
	if current.Warnings[0] != "original" || current.Data.Value().Kind() != jsonvalue.Object || current.Probe.status != Applied {
		t.Fatal("result aliases caller records")
	}
	if _, err := json.Marshal(Result{}); err == nil {
		t.Fatal("zero result published")
	}
	if _, err := NewResult(ResultParameters{Status: Applied, Operation: InvalidOperation}); err == nil {
		t.Fatal("invalid operation admitted")
	}
	legacy, _ := jsonvalue.Decode(`{"value":NaN}`)
	data, _ := NewPayload(legacy)
	historical, _ := NewResult(ResultParameters{Status: Applied, Operation: RepoMap, Data: &data})
	if _, err := historical.ToJSON(); err != nil {
		t.Fatal(err)
	}
	if _, err := json.Marshal(historical); !errors.Is(err, jsonvalue.ErrNonfinite) {
		t.Fatal("standard JSON accepted nonfinite data", err)
	}
}

func TestClassifierProbeProcessAndDecoderFailures(t *testing.T) {
	missing, _ := NewProbe(Missing, "", Homepage, "missing binary")
	result, err := classifyPayload(RepoMap, missing, processResult{stdout: "ignored"}, nil, nil, SchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	p := result.Parameters()
	if p.Status != Missing || p.OutputBytes != 0 || p.Message != "missing binary" {
		t.Fatal(p)
	}
	probe, _ := NewProbe(Applied, "1.9.0", Homepage, "")
	result, err = classifyPayload(RepoMap, probe, processResult{status: Error, stdout: "é", message: "private timeout"}, nil, []snapshotRewrite{{snapshot: "private", workspace: "public"}}, SchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	p = result.Parameters()
	if p.OutputBytes != 2 || p.Message != "public timeout" {
		t.Fatal(p)
	}
	for _, test := range []struct {
		raw  string
		want error
	}{
		{strings.Repeat("1", 4301), jsonvalue.ErrInteger},
		{strings.Repeat("[", 1001) + "0" + strings.Repeat("]", 1001), jsonvalue.ErrDepth},
	} {
		_, err := classifyPayload(RepoMap, probe, processResult{status: Applied, stdout: test.raw}, nil, nil, SchemaVersion)
		if !errors.Is(err, test.want) {
			t.Fatal("decoder fault became a semantic result", err)
		}
	}
}
