package benchmark

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type taskContract struct {
	Specs []struct {
		Name, Prompt string
		Setup        bool
		ToolNames    *[]protocol.ToolName `json:"tool_names"`
	}
	Cases []struct {
		Task, Kind, Path, Final string
		HexBytes                string `json:"hex_bytes"`
		Passed                  bool
		Error                   *string
	}
	Seed struct {
		Size, Lines               int
		SHA256, First, Deep, Last string
	}
}

func readTaskContract(t *testing.T) taskContract {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-benchmark-tasks.json")
	if err != nil {
		t.Fatal(err)
	}
	var contract taskContract
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	if len(contract.Specs) != 8 || len(contract.Cases) != 50 {
		t.Fatal("source task inventory drift")
	}
	return contract
}
func TestAdmittedTasksMatchActualPythonSpecsAndJudges(t *testing.T) {
	contract := readTaskContract(t)
	tasks := append(DefaultTasks(), HeldoutTasks()...)
	byName := make(map[string]Task, len(tasks))
	for i, task := range tasks {
		spec := contract.Specs[i]
		names, selected := task.ToolNames()
		if task.Name() != spec.Name || task.Prompt() != spec.Prompt || (task.setup != nil) != spec.Setup || selected != (spec.ToolNames != nil) {
			t.Fatalf("spec drift: %s", task.Name())
		}
		if selected && !slices.Equal(names, *spec.ToolNames) {
			t.Fatal("whitelist drift", names)
		}
		byName[task.Name()] = task
	}
	for i, recipe := range contract.Cases {
		t.Run(fmt.Sprintf("%02d-%s-%s", i, recipe.Task, recipe.Kind), func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, recipe.Path)
			data, err := hex.DecodeString(recipe.HexBytes)
			if err != nil {
				t.Fatal(err)
			}
			if recipe.Path != "" {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
			}
			switch recipe.Kind {
			case "file":
				err = os.WriteFile(path, data, 0600)
			case "directory":
				err = os.Mkdir(path, 0700)
			case "broken":
				err = os.Symlink("absent", path)
			case "loop":
				err = os.Symlink(filepath.Base(path), path)
			case "symlink":
				err = os.WriteFile(filepath.Join(filepath.Dir(path), "actual"), data, 0600)
				if err == nil {
					err = os.Symlink("actual", path)
				}
			case "missing":
			default:
				t.Fatal("unknown recipe", recipe.Kind)
			}
			if err != nil {
				t.Fatal(err)
			}
			passed, err := byName[recipe.Task].Judge(context.Background(), root, recipe.Final)
			if passed != recipe.Passed || (err != nil) != (recipe.Error != nil) {
				t.Fatalf("judge drift: %v %v, want %v %v", passed, err, recipe.Passed, recipe.Error)
			}
			if recipe.Error != nil {
				switch *recipe.Error {
				case "UnicodeDecodeError":
					if !errors.Is(err, ErrInvalidText) {
						t.Fatal(err)
					}
				case "IsADirectoryError":
					if !errors.Is(err, syscall.EISDIR) {
						t.Fatal(err)
					}
				default:
					t.Fatal("unclassified source fault", *recipe.Error)
				}
			}
		})
	}
}
func TestLongLogSetupMatchesActualPythonBytes(t *testing.T) {
	seed := readTaskContract(t).Seed
	for _, task := range DefaultTasks()[3:] {
		root := t.TempDir()
		path := filepath.Join(root, "data.log")
		if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := task.Prepare(context.Background(), root); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
		if len(data) != seed.Size || fmt.Sprintf("%x", sha256.Sum256(data)) != seed.SHA256 || len(lines) != seed.Lines || lines[0] != seed.First || lines[4320] != seed.Deep || lines[len(lines)-1] != seed.Last {
			t.Fatal("seed bytes drift", task.Name())
		}
	}
}
func TestTaskCallbacksCancellationAndDetachedState(t *testing.T) {
	names := []protocol.ToolName{protocol.ToolReadFile}
	fault := errors.New("judge/setup fault")
	called := 0
	task, err := NewTask(TaskConfig{Expect: func(context.Context, string, string) (bool, error) { called++; return true, fault }, Setup: func(context.Context, string) error { called++; return fault }, ToolNames: &names})
	if err != nil {
		t.Fatal(err)
	}
	names[0] = protocol.ToolBash
	got, selected := task.ToolNames()
	got[0] = protocol.ToolWriteFile
	again, _ := task.ToolNames()
	if !selected || again[0] != protocol.ToolReadFile {
		t.Fatal("caller mutated whitelist")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := task.Prepare(ctx, ""); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := task.Judge(ctx, "", ""); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if called != 0 {
		t.Fatal("cancelled task entered callbacks")
	}
	if err := task.Prepare(context.Background(), ""); !errors.Is(err, fault) {
		t.Fatal(err)
	}
	if passed, err := task.Judge(context.Background(), "", ""); passed || !errors.Is(err, fault) {
		t.Fatal("fault scored success", passed, err)
	}
	if _, err := NewTask(TaskConfig{}); !errors.Is(err, ErrInvalidTask) {
		t.Fatal(err)
	}
	if err := (Task{}).Prepare(context.Background(), ""); !errors.Is(err, ErrInvalidTask) {
		t.Fatal(err)
	}
	if _, err := (Task{}).Judge(context.Background(), "", ""); !errors.Is(err, ErrInvalidTask) {
		t.Fatal(err)
	}
	empty := []protocol.ToolName{}
	valid, _ := NewTask(TaskConfig{Expect: textJudge(""), ToolNames: &empty})
	if names, selected := valid.ToolNames(); !selected || len(names) != 0 {
		t.Fatal("empty selection became absent")
	}
	if err := valid.Prepare(context.Background(), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	list := DefaultTasks()
	list[0] = Task{}
	if DefaultTasks()[0].Name() != "write-file" {
		t.Fatal("caller replaced admitted task")
	}
}
