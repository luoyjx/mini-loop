package workflows

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestModelsMatchActualPython(t *testing.T) {
	var fixture struct {
		Definitions []struct {
			Input, Semantic, Output Value
			Canonical               string
		}
		Hashes []struct {
			Input     string `json:"input_json"`
			Canonical string
			Hash      Digest
		}
		Errors []struct {
			Input string `json:"input_json"`
			Error string
		}
		Artifacts    []ArtifactSnapshot
		Kinds        []NodeKind
		Sources      []DefinitionSource
		Verification []VerificationStatus
		Runs         []struct {
			Value    RunStatus
			Terminal bool
		}
		Nodes []struct {
			Value               NodeStatus
			Terminal, Satisfies bool
		}
		Attempts []struct {
			Value    AttemptStatus
			Terminal bool
		}
	}
	data, err := os.ReadFile(filepath.Join("..", "testdata", "python-workflow-models.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for i, row := range fixture.Definitions {
		input, err := row.Input.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		model, err := DecodeDefinition(input)
		if err != nil {
			t.Fatalf("definition %d: %v", i, err)
		}
		canonical, err := CanonicalJSON(model.Semantic())
		if err != nil || string(canonical) != row.Canonical {
			t.Fatalf("definition %d canonical: %s, want %s: %v", i, canonical, row.Canonical, err)
		}
		got, _ := CanonicalJSON(model.Data())
		want, _ := CanonicalJSON(row.Output)
		if string(got) != string(want) {
			t.Fatalf("definition %d: %s want %s", i, got, want)
		}
		// Identity ignores explicit revision/parent and a forged saved hash.
		v, _ := model.Data().Lookup("definition_hash")
		hash, _ := v.Text()
		if string(model.Hash()) != hash {
			t.Fatal("inconsistent identity")
		}
	}
	for _, row := range fixture.Hashes {
		value, err := jsonvalue.Decode(row.Input)
		if err != nil {
			t.Fatal(err)
		}
		data, err := CanonicalJSON(value)
		hash, hashErr := ContentHash(value, "sha256")
		if err != nil || hashErr != nil || string(data) != row.Canonical || hash != row.Hash {
			t.Fatalf("hash input %s: %s %s %v %v", row.Input, data, hash, err, hashErr)
		}
	}
	for _, row := range fixture.Errors {
		value, err := jsonvalue.Decode(row.Input)
		if err != nil {
			t.Fatal(err)
		}
		_, err = ContentHash(value, "sha256")
		if err == nil {
			t.Fatalf("accepted source hash error %s", row.Input)
		}
		if row.Error == "ValueError" && !errors.Is(err, jsonvalue.ErrNonfinite) {
			t.Fatal(err)
		}
		if row.Error == "UnicodeEncodeError" && !errors.Is(err, jsonvalue.ErrSurrogate) {
			t.Fatal(err)
		}
	}
	for _, want := range fixture.Artifacts {
		valid := want.SchemaValid
		artifact, err := NewArtifact(ArtifactInput{want.RunID, want.NodeID, want.AttemptID, want.Value, jsonvalue.ObjectValue([]jsonvalue.Field{{Name: "type", Value: jsonvalue.TextValue("object")}}), want.VerificationStatus, &valid})
		if err != nil {
			t.Fatal(err)
		}
		got := artifact.Snapshot()
		if !strings.HasPrefix(string(got.ArtifactID), "artifact_") || len(got.ArtifactID) != 29 || got.CreatedAt <= 0 {
			t.Fatal("invalid generated metadata")
		}
		got.ArtifactID = want.ArtifactID
		got.CreatedAt = want.CreatedAt
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("artifact %+v want %+v", got, want)
		}
	}
	for _, s := range fixture.Kinds {
		if !s.Valid() {
			t.Fatal(s)
		}
	}
	for _, s := range fixture.Sources {
		if !s.Valid() {
			t.Fatal(s)
		}
	}
	for _, s := range fixture.Verification {
		if !s.Valid() {
			t.Fatal(s)
		}
	}
	for _, s := range fixture.Runs {
		if !s.Value.Valid() || s.Value.Terminal() != s.Terminal {
			t.Fatal(s)
		}
	}
	for _, s := range fixture.Nodes {
		if !s.Value.Valid() || s.Value.Terminal() != s.Terminal || s.Value.SatisfiesDependency() != s.Satisfies {
			t.Fatal(s)
		}
	}
	for _, s := range fixture.Attempts {
		if !s.Value.Valid() || s.Value.Terminal() != s.Terminal {
			t.Fatal(s)
		}
	}
}

func TestDefinitionTypedAdmissionAndDetachedIdentity(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"name":"a","return_from":"x","nodes":[{"id":"x","kind":"bogus"}]}`, `{"name":"a","return_from":"x","source":"foreign"}`, `{"name":"a","return_from":"x","input_schema":null}`, `{"name":"a","return_from":"x","unknown":true}`} {
		if _, err := DecodeDefinition([]byte(raw)); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	d, err := DecodeDefinition([]byte(`{"name":"a","return_from":"x","nodes":[{"id":"x","kind":"agent"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	before := d.Hash()
	v, _ := d.Data().Lookup("nodes")
	nodes, _ := v.Array()
	nodes[0] = jsonvalue.NullValue()
	if d.Hash() != before {
		t.Fatal("mutable content identity")
	}
	v, _ = d.Data().Lookup("nodes")
	nodes, _ = v.Array()
	if nodes[0].Kind() != jsonvalue.Object {
		t.Fatal("leaked mutable definition")
	}
	if NodeKind("bad").Valid() || RunStatus("bad").Valid() || NodeStatus("bad").Valid() || AttemptStatus("bad").Valid() || VerificationStatus("bad").Valid() || DefinitionSource("bad").Valid() {
		t.Fatal("unknown status valid")
	}
}
