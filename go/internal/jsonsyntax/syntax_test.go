package jsonsyntax

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestSyntaxDiagnosticsMatchActualASTSourceInvocations(t *testing.T) {
	data, err := os.ReadFile("../../testdata/python-ast-payloads.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Invocations []struct {
			Case struct {
				Name, Stdout string
				ExitCode     int `json:"exit_code"`
			}
			ResultJSON string `json:"result_json"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Invocations) != 65 {
		t.Fatal("Source invocation inventory changed")
	}
	count := 0
	for _, row := range fixture.Invocations {
		if row.Case.ExitCode != 0 {
			continue
		}
		failure := Scan([]byte(row.Case.Stdout), Limits{Containers: 1000, IntegerDigits: 4300})
		if failure == nil {
			continue
		}
		count++
		result, err := jsonvalue.Decode(row.ResultJSON)
		if err != nil {
			t.Fatal(row.Case.Name, err)
		}
		message, _ := result.Lookup("message")
		expected, _ := message.Text()
		prefix := jsonvalue.TextPrefix(row.Case.Stdout, failure.Position())
		line := strings.Count(prefix, "\n") + 1
		column := jsonvalue.RuneCount(prefix[strings.LastIndex(prefix, "\n")+1:]) + 1
		actual := fmt.Sprintf("ast-outline returned invalid JSON: %s: line %d column %d (char %d)", failure.Message(), line, column, failure.Position())
		if actual != expected {
			t.Fatal(row.Case.Name, actual, expected)
		}
	}
	if count != 8 {
		t.Fatal("Source JSON syntax refusals changed", count)
	}
}
