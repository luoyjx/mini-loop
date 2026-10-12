package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInventorySeparatesCommentsShadowedCallsAndUnexecutedTests(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "testdata"), 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"testdata/python-comment.json": "{}",
		"testdata/python-linked.json":  "{}", "testdata/python-skipped.json": "{}", "testdata/python-shadowed.json": "{}", "testdata/python-missing.json": "{}",
		"helpers.go": `package sample
func fixture(){use("../testdata/python-linked.json")}
func shadowed(){use("../testdata/python-shadowed.json")}
func use(string){}`,
		"active_test.go": `package sample
import alias "testing"
// ../testdata/python-comment.json is a comment, not a consumer.
func TestLinked(t *alias.T){fixture()}
func TestShadowed(t *alias.T){shadowed:=func(){};shadowed()}
func TestSkipped(t *alias.T){use("../testdata/python-skipped.json")}
func Testlower(t *alias.T){use("../testdata/python-missing.json")}`,
		"inactive_test.go": `package sample
func TestIgnored(){use("../testdata/python-missing.json")}`,
		"results.jsonl": "{\"Action\":\"pass\",\"Package\":\"sample.test\",\"Test\":\"TestLinked\"}\n{\"Action\":\"skip\",\"Package\":\"sample.test\",\"Test\":\"TestSkipped\"}\n{\"Action\":\"pass\",\"Package\":\"sample.test\",\"Test\":\"TestSkipped/subtest\"}\n",
	}
	for path, data := range files {
		if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	packages := []packageMetadata{{ImportPath: "sample.test", Dir: root, GoFiles: []string{"helpers.go"}, TestGoFiles: []string{"active_test.go"}}}
	report, err := buildInventory(root, packages, filepath.Join(root, "results.jsonl"), "")
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]string{}
	for _, fixture := range report.Fixtures {
		statuses[fixture.Name] = fixture.Status
	}
	expected := map[string]string{"python-linked.json": "test_pass_observed", "python-skipped.json": "test_not_observed", "python-shadowed.json": "unlinked", "python-missing.json": "unlinked", "python-comment.json": "unreferenced"}
	// The lowercase Testlower function is inventoried, but is not a Go test root.
	for name, want := range expected {
		if statuses[name] != want {
			t.Fatalf("%s: %s want %s", name, statuses[name], want)
		}
	}
	if err := requireResults(report); err == nil {
		t.Fatal("missing candidate results accepted")
	}
	if _, exists := report.SourceHashes["inactive_test.go"]; exists {
		t.Fatal("inactive file entered active profile")
	}
	report, err = buildInventory(root, packages, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = requireResults(report); err == nil {
		t.Fatal("inventory-only report certified execution")
	}
}

func TestInventoryRejectsMalformedResultsAndMethodGuessing(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "testdata"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"testdata/python-method.json": "{}",
		"method_test.go": `package sample
import "testing"
type Reader struct{}
func (Reader) Load(){if true {use("../testdata/python-method.json")}}
func TestMethod(t *testing.T){Reader{}.Load()}
func use(string){}`,
		"results.jsonl": "not-json",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	packages := []packageMetadata{{ImportPath: "sample.test", Dir: root, TestGoFiles: []string{"method_test.go"}}}
	if _, err := buildInventory(root, packages, filepath.Join(root, "results.jsonl"), ""); err == nil {
		t.Fatal("malformed result stream accepted")
	}
	report, err := buildInventory(root, packages, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Fixtures[0].Status != "unlinked" {
		t.Fatal("method edge was guessed", report.Fixtures[0])
	}
	guards := report.Fixtures[0].References[0].EnclosingIf
	if len(guards) != 1 || guards[0] != "then: true" {
		t.Fatal("enclosing branch lost", guards)
	}
	if err := os.WriteFile(filepath.Join(root, "results.jsonl"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := testResults(filepath.Join(root, "results.jsonl")); err == nil {
		t.Fatal("empty result stream accepted")
	}
}

func TestManifestRejectsDriftAndKeepsSeparateSourceArtifactsVisible(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "testdata"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"python-current.json", "python-separate.json"} {
		if err := os.WriteFile(filepath.Join(root, "testdata", name), []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "manifest.json")
	manifest := sourceManifest{ProducerSHA256: digest([]byte("producer")), Files: []manifestFixture{{Name: "python-current.json", SHA256: digest([]byte("{}"))}}}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	report, err := buildInventory(root, nil, "", path)
	if err != nil {
		t.Fatal(err)
	}
	if report.Fixtures[0].ProducerScope != "current_export" || report.Fixtures[1].ProducerScope != "outside_current_export" {
		t.Fatal(report.Fixtures)
	}
	if err = os.WriteFile(filepath.Join(root, "testdata/python-current.json"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = buildInventory(root, nil, "", path); err == nil {
		t.Fatal("Source hash drift accepted")
	}
	if err = os.WriteFile(filepath.Join(root, "testdata/python-current.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	manifest.Files = append(manifest.Files, manifest.Files[0])
	data, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = buildInventory(root, nil, "", path); err == nil {
		t.Fatal("duplicate Source manifest entry accepted")
	}
}
