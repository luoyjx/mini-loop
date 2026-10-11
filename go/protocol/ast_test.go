package protocol

import (
	"encoding/json"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"os"
	"strings"
	"testing"
)

func TestASTSchemasMatchInstalledSourceDeclarations(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-ast-tools.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Tools []struct{ Schema ToolSchema } }
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	schemas := ASTToolSchemas()
	if len(fixture.Tools) != 4 {
		t.Fatal("Source tool inventory changed")
	}
	for i, source := range fixture.Tools {
		if err = schemas[i].Validate(); err != nil {
			t.Fatal(err)
		}
		got, _ := json.Marshal(schemas[i])
		want, _ := json.Marshal(source.Schema)
		g, err := jsonvalue.Decode(string(got))
		if err != nil {
			t.Fatal(err)
		}
		w, err := jsonvalue.Decode(string(want))
		if err != nil {
			t.Fatal(err)
		}
		if !g.PythonEqual(w) {
			t.Fatalf("schema %s got %s want %s", schemas[i].Name, got, want)
		}
	}
	cloned := schemas[0].Clone()
	paths := (*cloned.InputSchema.Properties)["paths"]
	*paths.Items.MaxLength = 1
	density := (*cloned.InputSchema.Properties)["density"]
	*density.Default = jsonvalue.TextValue("changed")
	if *(*schemas[0].InputSchema.Properties)["paths"].Items.MaxLength != 2048 {
		t.Fatal("schema bound aliases clone")
	}
	text, _ := (*schemas[0].InputSchema.Properties)["density"].Default.Text()
	if text != "default" {
		t.Fatal("schema default aliases clone")
	}
}
func TestASTInputVariantsReplayIdentityAndDetach(t *testing.T) {
	for _, test := range []struct {
		name           ToolName
		raw, canonical string
	}{
		{ToolRepoMap, `{"paths":["α.py"]}`, `{"paths":["α.py"]}`},
		{ToolRepoMap, `{"paths":["α.py"],"density":"wide","include_imports":false}`, `{"density":"wide","include_imports":false,"paths":["α.py"]}`},
		{ToolFileOutline, `{"paths":["code.py"],"view":"minimal","include_imports":true}`, `{"include_imports":true,"paths":["code.py"],"view":"minimal"}`},
		{ToolShowSymbol, `{"symbols":["-Option"],"path_or_glob":"*.py","signature_only":false}`, `{"path_or_glob":"*.py","signature_only":false,"symbols":["-Option"]}`},
		{ToolSymbolReferences, `{"paths":["code.py"],"patterns":["-Option"],"kinds":[],"max_per_file":7}`, `{"kinds":[],"max_per_file":7,"paths":["code.py"],"patterns":["-Option"]}`},
	} {
		input, err := DecodeToolInput(test.name, []byte(test.raw))
		if err != nil {
			t.Fatal(err)
		}
		got, err := input.CanonicalJSON()
		if err != nil || got != test.canonical {
			t.Fatal(got, err, test.canonical)
		}
		masked := MapToolInputStrings(input, func(s string) string { return strings.ReplaceAll(s, "code.py", "masked.py") })
		original, _ := input.CanonicalJSON()
		if original != test.canonical {
			t.Fatal("masking changed original")
		}
		if strings.Contains(test.raw, "code.py") {
			raw, _ := json.Marshal(masked)
			if !strings.Contains(string(raw), "masked.py") {
				t.Fatal("masking skipped AST field", string(raw))
			}
		}
	}
	kinds := []ASTReferenceKind{ASTDefinition}
	paths := []string{"code.py"}
	cap := 7
	input := SymbolReferencesToolInput(SymbolReferencesInput{Patterns: []string{"One"}, Paths: paths, Kinds: &kinds, MaxPerFile: &cap})
	paths[0] = "changed"
	kinds[0] = ASTCall
	cap = 9
	value, _ := input.SymbolReferences()
	value.Paths[0] = "changed again"
	(*value.Kinds)[0] = ASTString
	*value.MaxPerFile = 12
	value, _ = input.SymbolReferences()
	if value.Paths[0] != "code.py" || (*value.Kinds)[0] != ASTDefinition || *value.MaxPerFile != 7 {
		t.Fatal("AST variant aliases caller data")
	}
}
func TestASTTypedDecoderRefusesAmbiguousShapes(t *testing.T) {
	for _, test := range []struct {
		name ToolName
		raw  string
	}{
		{ToolRepoMap, `{}`}, {ToolRepoMap, `{"paths":null}`}, {ToolRepoMap, `{"paths":[1]}`}, {ToolRepoMap, `{"paths":"code.py"}`}, {ToolRepoMap, `{"paths":[],"density":"other"}`}, {ToolRepoMap, `{"paths":[],"include_imports":null}`}, {ToolRepoMap, `{"paths":[],"workspace":"foreign"}`},
		{ToolFileOutline, `{"paths":[],"view":"other"}`}, {ToolShowSymbol, `{"symbols":[]}`}, {ToolSymbolReferences, `{"patterns":[],"paths":[],"kinds":["unknown"]}`}, {ToolSymbolReferences, `{"patterns":[],"paths":[],"max_per_file":true}`},
	} {
		if _, err := DecodeToolInput(test.name, []byte(test.raw)); err == nil {
			t.Fatal("ambiguous AST input admitted", test.raw)
		}
	}
}

func TestNamedSchemaAnnotationsRefuseInvalidBounds(t *testing.T) {
	negative, low, high := -1, 10, 1
	for _, schema := range []InputSchema{
		{Type: SchemaString, MaxLength: &negative},
		{Type: SchemaBoolean, Minimum: &low},
		{Type: SchemaInteger, Minimum: &low, Maximum: &high},
	} {
		if err := schema.Validate(); err == nil {
			t.Fatal("invalid named schema bounds admitted")
		}
	}
}
