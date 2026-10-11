package shell

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/spill"
)

type preservedFile struct {
	SHA256 string
	Bytes  int
}
type projectionContract struct {
	Name, Command           string
	Enabled, Broken, Secret bool
	RenderSHA256            string `json:"render_sha256"`
	RenderChars             int    `json:"render_chars"`
	Preserved               []preservedFile
	StdoutPrefix            string `json:"stdout_prefix"`
	StdoutPattern           string `json:"stdout_pattern"`
	StdoutRepeat            int    `json:"stdout_repeat"`
	StdoutSuffix            string `json:"stdout_suffix"`
	Stderr                  string
	Error                   *string
	Projection              bool
}

func spillFixtures(t *testing.T) ([]projectionContract, []projectionContract) {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-spill.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Bash, Projections []projectionContract }
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture.Bash, fixture.Projections
}
func checkPreservation(t *testing.T, root, rendered string, row projectionContract) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(root, "session-*", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != len(row.Preserved) {
		t.Fatalf("artifacts %d, expected %d", len(paths), len(row.Preserved))
	}
	for i, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(raw)
		if hex.EncodeToString(digest[:]) != row.Preserved[i].SHA256 || len(raw) != row.Preserved[i].Bytes || strings.Contains(string(raw), "fixture-secret") {
			t.Fatal("saved bytes differ from source")
		}
		rendered = strings.ReplaceAll(rendered, path, "<artifact>")
	}
	digest := sha256.Sum256([]byte(rendered))
	if hex.EncodeToString(digest[:]) != row.RenderSHA256 || utf8.RuneCountInString(rendered) != row.RenderChars {
		t.Fatalf("projection differs: %s, chars %d, expected %s / %d", hex.EncodeToString(digest[:]), utf8.RuneCountInString(rendered), row.RenderSHA256, row.RenderChars)
	}
}

type brokenSpill struct{ panic bool }

func (s brokenSpill) SaveText(context.Context, spill.Request) (spill.Ref, error) {
	if s.panic {
		panic("private plugin data")
	}
	return spill.Ref{}, errors.New("private backend data")
}

func TestActualPythonStringBashPreservation(t *testing.T) {
	bash, _ := spillFixtures(t)
	if len(bash) != 8 {
		t.Fatal("source inventory changed")
	}
	for _, row := range bash {
		t.Run(row.Name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "store")
			var store spill.Store
			if row.Enabled && !row.Broken {
				local, err := spill.NewLocalStore(root)
				if err != nil {
					t.Fatal(err)
				}
				root = local.Root()
				store = local
			}
			if row.Broken {
				store = brokenSpill{}
			}
			registry := secrets.New(secrets.Config{})
			if row.Secret {
				registry.RegisterValue("DEMO", "fixture-secret")
			}
			executor := makeExecutor(t, Config{Spill: store, Secrets: registry})
			rendered, err := executor.ExecuteBash(context.Background(), protocol.BashInput{Command: row.Command})
			if err != nil {
				t.Fatal(err)
			}
			checkPreservation(t, root, rendered, row)
		})
	}
}
func TestActualPythonProjectionAuthorityAndErrorPolicy(t *testing.T) {
	_, rows := spillFixtures(t)
	for _, row := range rows {
		t.Run(row.Name, func(t *testing.T) {
			store, err := spill.NewLocalStore(filepath.Join(t.TempDir(), "store"))
			if err != nil {
				t.Fatal(err)
			}
			executor := makeExecutor(t, Config{Spill: store})
			zero := 0
			result := Result{Stdout: row.StdoutPrefix + strings.Repeat(row.StdoutPattern, row.StdoutRepeat) + row.StdoutSuffix, Stderr: row.Stderr, Error: row.Error, TimedOut: row.Error != nil, ExitCode: &zero, CaptureLimit: CaptureLimit}
			if row.Projection {
				combined := result.Stdout + result.Stderr
				result.Projection = &combined
			}
			rendered := executor.projectBash(context.Background(), result)
			checkPreservation(t, store.Root(), rendered, row)
		})
	}
}
func TestStorePanicKeepsIdenticalPreviewAndSecretRebindingKeepsStore(t *testing.T) {
	plain := makeExecutor(t, Config{})
	input := protocol.BashInput{Command: "awk 'BEGIN {for(i=0;i<60000;i++)printf \"A\"}'"}
	expected, err := plain.ExecuteBash(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	crashing := makeExecutor(t, Config{Spill: brokenSpill{panic: true}})
	actual, err := crashing.ExecuteBash(context.Background(), input)
	if err != nil || actual != expected {
		t.Fatal("panic changed preview", err)
	}
	store, err := spill.NewLocalStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	executor := makeExecutor(t, Config{Spill: store})
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("DEMO", "fixture-secret")
	bound, err := executor.WithSecrets(registry)
	if err != nil {
		t.Fatal(err)
	}
	masked, err := bound.ExecuteBash(context.Background(), protocol.BashInput{Command: input.Command + "; printf 'fixture-secret'"})
	if err != nil || strings.Contains(masked, "fixture-secret") || !strings.Contains(masked, "full output preserved:") {
		t.Fatal("rebinding lost mask/store", err)
	}
	artifacts, _ := filepath.Glob(filepath.Join(store.Root(), "session-*", "*.txt"))
	if len(artifacts) != 1 {
		t.Fatal("rebinding lost store")
	}
	raw, err := os.ReadFile(artifacts[0])
	if err != nil || strings.Contains(string(raw), "fixture-secret") || !strings.Contains(string(raw), secrets.Mask) {
		t.Fatal("raw credential reached disk", err)
	}
}
