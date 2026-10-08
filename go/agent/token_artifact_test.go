package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/efficiency"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestTokenArtifactActualTurnsMatchSourceRecovery(t *testing.T) {
	var source struct {
		Readonly     bool
		ParallelSafe bool `json:"parallel_safe"`
		Risk         ToolRisk
		Capabilities []Capability
		Rows         []struct {
			Name, Content, State string
			Repeat               int
			Params               json.RawMessage
			OutputSHA256         string `json:"output_sha256"`
			OutputBytes          int    `json:"output_bytes"`
			Header               string
		}
	}
	data, err := os.ReadFile("../testdata/python-efficiency-recovery.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	if len(source.Rows) != 20 {
		t.Fatal("Source recipe inventory changed")
	}
	for _, row := range source.Rows {
		t.Run(row.Name, func(t *testing.T) {
			root := t.TempDir()
			now := 0.0
			c := efficiency.DefaultRawStoreConfig(root)
			c.TTLSeconds = 10
			c.Clock = func() (float64, error) { return now, nil }
			store, e := efficiency.NewRawStore(c)
			if e != nil {
				t.Fatal(e)
			}
			defer store.Close()
			p, e := store.PutMasked(strings.Repeat(row.Content, row.Repeat))
			if e != nil {
				t.Fatal(e)
			}
			snapshot, e := p.Snapshot()
			if e != nil {
				t.Fatal(e)
			}
			active := store
			ref := snapshot.Ref
			switch row.State {
			case "unavailable":
				active = nil
			case "foreign":
				active, e = efficiency.NewRawStore(c)
				if e != nil {
					t.Fatal(e)
				}
				defer active.Close()
			case "closed":
				store.Close()
			case "expired":
				now = 10
			case "invalid":
				ref = "../invalid"
			}
			params := strings.TrimSpace(string(row.Params))
			refJSON, e := json.Marshal(ref)
			if e != nil {
				t.Fatal(e)
			}
			suffix := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(params, "{"), "}"))
			if suffix != "" {
				suffix = "," + suffix
			}
			input, e := protocol.DecodeToolInput(protocol.ToolReadTokenArtifact, []byte(`{"raw_ref":`+string(refJSON)+suffix+`}`))
			if e != nil {
				t.Fatal(e)
			}
			cfg := runtimeConfig(root, resourceProvider{[]protocol.Block{protocol.NewToolUse("search", input)}})
			cfg.Mode = ModeReadonly
			cfg.TokenArtifactTools = true
			cfg.TokenArtifactStore = active
			session, e := NewRuntimeSession(cfg)
			if e != nil {
				t.Fatal(e)
			}
			definition, ok := session.gate.catalog.Lookup(protocol.ToolReadTokenArtifact)
			if !ok || definition.Readonly() != source.Readonly || definition.ParallelSafe() != source.ParallelSafe || definition.Risk() != source.Risk || len(definition.Capabilities()) != 1 || definition.Capabilities()[0] != source.Capabilities[0] {
				t.Fatal("Source traits drift", definition)
			}
			if _, e = session.Run(context.Background(), "recover"); e != nil {
				t.Fatal(e)
			}
			result := searchResult(t, session)
			sum := sha256.Sum256([]byte(result.Content))
			if result.IsError || hex.EncodeToString(sum[:]) != row.OutputSHA256 || len(result.Content) != row.OutputBytes || strings.SplitN(result.Content, "\n", 2)[0] != row.Header {
				t.Fatalf("recovery differs: bytes %d/%d, header %q, fault %v", len(result.Content), row.OutputBytes, strings.SplitN(result.Content, "\n", 2)[0], result.IsError)
			}
		})
	}
}
func TestTokenArtifactBindingSelectionAndCustomCatalogue(t *testing.T) {
	root := t.TempDir()
	cfg := runtimeConfig(root, resourceProvider{})
	cfg.Mode = ModeReadonly
	plain, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plain.gate.catalog.Lookup(protocol.ToolReadTokenArtifact); ok {
		t.Fatal("default catalogue widened")
	}
	cfg.TokenArtifactTools = true
	cfg.ToolSelection = SelectTools(protocol.ToolReadFile)
	selected, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := selected.gate.catalog.Lookup(protocol.ToolReadTokenArtifact); ok {
		t.Fatal("selection widened")
	}
	c := efficiency.DefaultRawStoreConfig(root)
	store, err := efficiency.NewRawStore(c)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	binding := ToolAuthority{SessionID: "own", OwnerID: "alice", Workspace: root, Mode: ModeReadonly}
	catalogue, err := NewToolCatalog()
	if err != nil {
		t.Fatal(err)
	}
	installed, err := InstallTokenArtifactTool(catalogue, binding, store)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := catalogue.Lookup(protocol.ToolReadTokenArtifact); ok {
		t.Fatal("immutable catalogue changed")
	}
	again, err := InstallTokenArtifactTool(installed, ToolAuthority{}, nil)
	if err != nil || again != installed {
		t.Fatal("same-name catalogue replaced", err)
	}
	definition, _ := installed.Lookup(protocol.ToolReadTokenArtifact)
	for _, foreign := range []ToolAuthority{{SessionID: "foreign", OwnerID: "alice", Workspace: root, Mode: ModeReadonly}, {SessionID: "own", OwnerID: "bob", Workspace: root, Mode: ModeReadonly}, {SessionID: "own", OwnerID: "alice", Workspace: t.TempDir(), Mode: ModeReadonly}} {
		if _, err = definition.handler.ExecuteTool(context.Background(), foreign, protocol.ReadTokenArtifactToolInput(protocol.ReadTokenArtifactInput{RawRef: "bad"})); err == nil {
			t.Fatal("foreign authority accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = definition.handler.ExecuteTool(ctx, binding, protocol.ReadTokenArtifactToolInput(protocol.ReadTokenArtifactInput{RawRef: "bad"})); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	foreignStore, err := efficiency.NewRawStore(efficiency.DefaultRawStoreConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer foreignStore.Close()
	cfg.ToolSelection = ToolSelection{}
	cfg.TokenArtifactStore = foreignStore
	if _, err = NewRuntimeSession(cfg); err == nil {
		t.Fatal("foreign workspace store admitted")
	}
	custom, err := NewToolDefinitionWithSchema(protocol.ReadTokenArtifactSchema(), ToolTraits{Risk: RiskWrite}, echoToolHandler{})
	if err != nil {
		t.Fatal(err)
	}
	customCatalog, err := NewToolCatalog(custom)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := InstallTokenArtifactTool(customCatalog, ToolAuthority{}, nil)
	if err != nil || kept != customCatalog {
		t.Fatal("custom definition not preserved", err)
	}
}

func TestTokenArtifactGuardRunsBeforeStoreRead(t *testing.T) {
	root := t.TempDir()
	reads := 0
	c := efficiency.DefaultRawStoreConfig(root)
	c.Clock = func() (float64, error) { reads++; return 0, nil }
	store, err := efficiency.NewRawStore(c)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p, err := store.PutMasked("masked")
	if err != nil {
		t.Fatal(err)
	}
	meta, err := p.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	reads = 0
	input := protocol.ReadTokenArtifactToolInput(protocol.ReadTokenArtifactInput{RawRef: meta.Ref})
	cfg := runtimeConfig(root, resourceProvider{[]protocol.Block{protocol.NewToolUse("search", input)}})
	cfg.Mode = ModeReadonly
	cfg.TokenArtifactTools = true
	cfg.TokenArtifactStore = store
	cfg.Hooks.Guards = []GuardHook{guardHookFunc(func(context.Context, ToolAuthority, ToolCall) (string, bool, error) {
		return "guard denied recovery", true, nil
	})}
	session, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = session.Run(context.Background(), "recover"); err != nil {
		t.Fatal(err)
	}
	result := searchResult(t, session)
	if !strings.Contains(result.Content, "guard denied recovery") || reads != 0 {
		t.Fatal("guard bypassed", result, reads)
	}
}
