package audit

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestPublicFindingAndRendererMatchActualPython(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-public-identity-audit.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		PublicAliases map[string]bool `json:"public_aliases"`
		Renderer      []struct {
			Recipe struct {
				Name     string
				Findings []Finding
			}
			Findings             []Finding
			Rendered             string
			UnchangedAfterRender bool `json:"unchanged_after_render"`
		}
		InvalidFindings []struct {
			Severity       Severity
			ExceptionClass string `json:"exception_class"`
			Message        string
		} `json:"invalid_findings"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Renderer) != 6 || len(fixture.InvalidFindings) != 5 || !fixture.PublicAliases["audit"] || !fixture.PublicAliases["Finding"] || !fixture.PublicAliases["render_audit"] {
		t.Fatal("incomplete actual public audit corpus")
	}
	for _, row := range fixture.Renderer {
		t.Run(row.Recipe.Name, func(t *testing.T) {
			// Go's detached typed value replaces Python's frozen dataclass. Read
			// recipe inputs independently of the observed Finding projection.
			findings := append([]Finding{}, row.Recipe.Findings...)
			if !reflect.DeepEqual(findings, row.Findings) {
				t.Fatal("Finding fields differ from actual Source construction", findings, row.Findings)
			}
			before, err := json.Marshal(findings)
			if err != nil {
				t.Fatal(err)
			}
			rendered, err := Render(findings)
			if err != nil || rendered != row.Rendered {
				t.Fatalf("native %q; Source %q; error %v", rendered, row.Rendered, err)
			}
			after, err := json.Marshal(findings)
			if err != nil || string(before) != string(after) || !row.UnchangedAfterRender {
				t.Fatal("renderer mutated findings", err)
			}
		})
	}
	for _, row := range fixture.InvalidFindings {
		t.Run("invalid-severity-"+string(row.Severity), func(t *testing.T) {
			// A Go struct literal cannot reject construction. Render validates the
			// enum before returning text; it never returns a partial report.
			text, err := Render([]Finding{{Severity: row.Severity, Check: "invalid", Detail: "fixture detail", Remedy: "fixture remedy"}})
			if row.ExceptionClass != "ValueError" || err == nil || err.Error() != row.Message || text != "" {
				t.Fatal(text, err, row)
			}
		})
	}
}
