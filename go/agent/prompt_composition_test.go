package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type compositionSection struct {
	Kind, Text, Label string
	Error             bool
}

type compositionFailure struct {
	Kind, Message string
}

func (failure compositionFailure) Error() string { return failure.Message }

type compositionResult struct {
	Output string
	Error  *compositionFailure
}

type compositionSeed struct {
	Name, Description, Body string
	Type                    memory.Type
	Owner                   memory.OwnerID
}

type compositionSectionsRecipe struct {
	Name     string
	Sections []compositionSection
	Builds   int
}

type compositionSectionsRow struct {
	Recipe            compositionSectionsRecipe
	Results           []compositionResult
	Trace             []string
	MessagesUnchanged bool `json:"messages_unchanged"`
}

type compositionFactsRecipe struct {
	Name, Owner     string
	Todos           []protocol.TodoItem
	Used, Threshold int
	Memory, Recall  bool
	Seeds           []compositionSeed
}

type compositionFactsRow struct {
	Recipe                 compositionFactsRecipe
	UsedTokens             int `json:"used_tokens"`
	Output                 string
	Todos                  []protocol.TodoItem
	IndexExists            bool `json:"index_exists"`
	RecordsUnchanged       bool `json:"records_unchanged"`
	MessagesUnchanged      bool `json:"messages_unchanged"`
	MeterUnchanged         bool `json:"meter_unchanged"`
	DefaultSystemUnchanged bool `json:"default_system_unchanged"`
}

type compositionMemoryRecipe struct {
	Name, Owner string
	Sections    []compositionSection
	Seeds       []compositionSeed
	SecondWrite *compositionSeed `json:"second_write"`
}

type compositionMemoryRow struct {
	Recipe                compositionMemoryRecipe
	Results               []compositionResult
	Trace                 []string
	FirstIndexExists      bool `json:"first_index_exists"`
	FirstRecordsUnchanged bool `json:"first_records_unchanged"`
	MessagesUnchanged     bool `json:"messages_unchanged"`
}

type compositionFixture struct {
	Sections       []compositionSectionsRow
	RuntimeFacts   []compositionFactsRow  `json:"runtime_facts"`
	MemorySystem   []compositionMemoryRow `json:"memory_system"`
	PythonVersion  string                 `json:"python_version"`
	Scope          string
	SourceSHA256   map[string]string `json:"source_sha256"`
	ProducerSHA256 string            `json:"producer_sha256"`
}

func readPromptComposition(t *testing.T) compositionFixture {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-prompt-composition.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture compositionFixture
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Sections) != 11 || len(fixture.RuntimeFacts) != 13 || len(fixture.MemorySystem) != 7 {
		t.Fatal("Source prompt composition inventory changed")
	}
	return fixture
}

func compositionBuilder(t *testing.T, specs []compositionSection, trace *[]string) agent.SystemBuilder {
	t.Helper()
	sections := make([]agent.SystemBuilder, 0, len(specs))
	for _, spec := range specs {
		if spec.Kind == "static" {
			sections = append(sections, agent.StaticSystemSection(spec.Text))
			continue
		}
		spec := spec
		sections = append(sections, agent.SystemBuilderFunc(func(value agent.SystemContext) (string, error) {
			*trace = append(*trace, spec.Label)
			if spec.Error {
				return "", compositionFailure{Kind: "ValueError", Message: "section fault"}
			}
			switch spec.Kind {
			case "none":
				// Source's falsey None callback result has an empty-string adapter.
				return "", nil
			case "workspace":
				return "Workspace: " + value.Workspace, nil
			case "tools":
				names := []string{}
				for _, name := range value.Catalog.SentNames() {
					names = append(names, string(name))
				}
				return "Tools: " + strings.Join(names, ", "), nil
			case "callback":
				return spec.Text, nil
			default:
				t.Fatalf("unknown Source callback recipe %q", spec.Kind)
				return "", nil
			}
		}))
	}
	return agent.SectionsBuilder(sections...)
}

func compositionInvoke(t *testing.T, build agent.SystemBuilder) compositionResult {
	t.Helper()
	output, err := build.BuildSystem(agent.SystemContext{Workspace: "<workspace>"})
	result := compositionResult{Output: output}
	if err != nil {
		var failure compositionFailure
		if !errors.As(err, &failure) {
			t.Fatalf("unexpected native composition failure: %v", err)
		}
		result.Error = &failure
	}
	return result
}

func compositionStore(t *testing.T, seeds []compositionSeed, owner string) (*memory.Store, *memory.ScopedStore, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "memory")
	store, err := memory.NewStore(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range seeds {
		compositionWrite(t, store, seed)
	}
	bound, err := memory.Bind(store, memory.OwnerID(owner))
	if err != nil {
		t.Fatal(err)
	}
	return store, bound, root
}

func compositionWrite(t *testing.T, store *memory.Store, seed compositionSeed) {
	t.Helper()
	_, err := store.Write(context.Background(), seed.Owner, memory.Input{
		Name: seed.Name, Type: seed.Type, Description: seed.Description,
		Body: seed.Body, Origin: memory.Explicit,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func compositionRecords(t *testing.T, store *memory.Store) []memory.Record {
	t.Helper()
	if store == nil {
		return nil
	}
	records, err := store.List(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return records
}

func compositionIndexExists(root string) bool {
	if root == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(root, "MEMORY.md"))
	return err == nil
}

func TestPublicPromptCompositionMatchesActualSource(t *testing.T) {
	fixture := readPromptComposition(t)
	for _, row := range fixture.Sections {
		t.Run("sections/"+row.Recipe.Name, func(t *testing.T) {
			trace := []string{}
			build := compositionBuilder(t, row.Recipe.Sections, &trace)
			count := row.Recipe.Builds
			if count == 0 {
				count = 1
			}
			results := []compositionResult{}
			for i := 0; i < count; i++ {
				results = append(results, compositionInvoke(t, build))
			}
			if !reflect.DeepEqual(results, row.Results) || !reflect.DeepEqual(trace, row.Trace) || !row.MessagesUnchanged {
				t.Fatalf("results=%+v trace=%v; Source=%+v trace=%v", results, trace, row.Results, row.Trace)
			}
		})
	}
	for _, row := range fixture.RuntimeFacts {
		t.Run("facts/"+row.Recipe.Name, func(t *testing.T) {
			var store *memory.Store
			var bound *memory.ScopedStore
			var root string
			if row.Recipe.Memory {
				store, bound, root = compositionStore(t, row.Recipe.Seeds, row.Recipe.Owner)
			}
			var todos agent.TodoManager
			if _, err := todos.Update(row.Recipe.Todos); err != nil {
				t.Fatal(err)
			}
			before := compositionRecords(t, store)
			snapshot := todos.Snapshot()
			value := agent.RuntimeFactsContext{Todos: snapshot, UsedTokens: row.UsedTokens,
				TokenThreshold: row.Recipe.Threshold, Memory: bound, RecallAvailable: row.Recipe.Recall}
			output, err := agent.RuntimeFacts(context.Background(), value)
			if err != nil || output != row.Output {
				t.Fatalf("output=%q err=%v; Source=%q", output, err, row.Output)
			}
			if len(snapshot) != len(row.Todos) || len(snapshot) > 0 && !reflect.DeepEqual(snapshot, row.Todos) {
				t.Fatal("Todo observation differs from Source")
			}
			if !reflect.DeepEqual(before, compositionRecords(t, store)) || !reflect.DeepEqual(snapshot, todos.Snapshot()) {
				t.Fatal("public facts mutated caller records or todos")
			}
			if compositionIndexExists(root) != row.IndexExists || !row.RecordsUnchanged || !row.MessagesUnchanged || !row.MeterUnchanged || !row.DefaultSystemUnchanged {
				t.Fatal("Source readonly/default-system/index effects differ")
			}
		})
	}
	for _, row := range fixture.MemorySystem {
		t.Run("memory/"+row.Recipe.Name, func(t *testing.T) {
			store, bound, root := compositionStore(t, row.Recipe.Seeds, row.Recipe.Owner)
			before := compositionRecords(t, store)
			trace := []string{}
			build := agent.MemorySystemBuilder(context.Background(), compositionBuilder(t, row.Recipe.Sections, &trace), bound)
			results := []compositionResult{compositionInvoke(t, build)}
			if compositionIndexExists(root) != row.FirstIndexExists || !reflect.DeepEqual(before, compositionRecords(t, store)) || !row.FirstRecordsUnchanged {
				t.Fatal("base/index evaluation order or readonly records differ")
			}
			if row.Recipe.SecondWrite != nil {
				compositionWrite(t, store, *row.Recipe.SecondWrite)
				results = append(results, compositionInvoke(t, build))
			}
			if !reflect.DeepEqual(results, row.Results) || !reflect.DeepEqual(trace, row.Trace) || !row.MessagesUnchanged {
				t.Fatalf("results=%+v trace=%v; Source=%+v trace=%v", results, trace, row.Results, row.Trace)
			}
		})
	}
}

func TestPromptCompositionOwnsSectionsAndKeepsNativeFailures(t *testing.T) {
	sections := []agent.SystemBuilder{agent.StaticSystemSection("first"), agent.StaticSystemSection("second")}
	build := agent.SectionsBuilder(sections...)
	sections[0] = agent.StaticSystemSection("mutated")
	if output, err := build.BuildSystem(agent.SystemContext{}); err != nil || output != "first\n\nsecond" {
		t.Fatal("builder retained caller section slice", output, err)
	}
	want := errors.New("callback failure")
	called := 0
	broken := agent.SystemBuilderFunc(func(agent.SystemContext) (string, error) { called++; return "", want })
	later := agent.SystemBuilderFunc(func(agent.SystemContext) (string, error) { t.Fatal("later callback ran"); return "", nil })
	if _, err := agent.SectionsBuilder(broken, later).BuildSystem(agent.SystemContext{}); !errors.Is(err, want) || called != 1 {
		t.Fatal("callback identity/order lost", err, called)
	}
	if _, err := agent.MemorySystemBuilder(nil, broken, nil).BuildSystem(agent.SystemContext{}); !errors.Is(err, want) || called != 2 {
		t.Fatal("base error must precede index dependency validation", err, called)
	}
	var missing agent.SystemBuilderFunc
	if _, err := agent.SectionsBuilder(missing).BuildSystem(agent.SystemContext{}); err == nil {
		t.Fatal("nil callback was invoked")
	}
}

func TestRuntimeFactsListsBoundStoreBeforeRecallSelection(t *testing.T) {
	_, bound, root := compositionStore(t, nil, "alice")
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.RuntimeFacts(context.Background(), agent.RuntimeFactsContext{Memory: bound}); err == nil {
		t.Fatal("disabled recall bypassed the public helper's initial list")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := agent.RuntimeFacts(ctx, agent.RuntimeFactsContext{}); !errors.Is(err, context.Canceled) {
		t.Fatal("caller cancellation lost", err)
	}
}
