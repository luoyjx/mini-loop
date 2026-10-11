package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestBuiltinMatchesActualPythonDeploymentSkill(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-configuration.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Builtin struct {
			Descriptions, Loaded string
			SourceSHA256         string `json:"source_sha256"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(builtinCodeReview))
	if hex.EncodeToString(digest[:]) != fixture.Builtin.SourceSHA256 {
		t.Fatal("embedded source differs from Python skill")
	}
	catalog, err := NewBuiltinCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := catalog.Load(context.Background(), protocol.LoadSkillInput{Name: "code_review"})
	if err != nil || loaded != fixture.Builtin.Loaded || catalog.Descriptions() != fixture.Builtin.Descriptions {
		t.Fatal(err, loaded, catalog.Descriptions())
	}
	if _, err := catalog.Load(context.Background(), protocol.LoadSkillInput{Name: "../code_review"}); err == nil {
		t.Fatal("builtin bypassed name validation")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewBuiltinCatalog(ctx); err == nil {
		t.Fatal("builtin ignores cancellation")
	}
}
