package commandenv

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/dotenv"
)

type probeRequest struct {
	PostEnvironment map[string]string
	PostFile        *string
	FilePath        string
}
type probeResponse struct {
	Environment      map[string]string
	Child            map[string]string
	Failure          *dotenv.Failure
	DiscoveryEnabled bool
	Diagnostic       string
}

func TestProcessBootstrapProbe(t *testing.T) {
	if os.Getenv("MINILOOP_BOOTSTRAP_PROBE") != "1" {
		return
	}
	var request probeRequest
	if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
		t.Fatal(err)
	}
	initialized, err := Initialize()
	response := probeResponse{Environment: initialized.Reveal(), DiscoveryEnabled: initialized.DiscoveryEnabled, Diagnostic: fmt.Sprintf("%v %#v", initialized, initialized)}
	if err != nil && !errors.As(err, &response.Failure) {
		t.Fatal(err)
	}
	if err == nil {
		for name, value := range request.PostEnvironment {
			if err := os.Setenv(name, value); err != nil {
				t.Fatal(err)
			}
			response.Environment[name] = value
		}
		if request.PostFile != nil {
			if err := os.WriteFile(request.FilePath, []byte(*request.PostFile), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	child, childErr := exec.Command("/usr/bin/env").Output()
	if childErr != nil {
		t.Fatal(childErr)
	}
	response.Child = map[string]string{}
	for _, line := range strings.Split(string(child), "\n") {
		if name, value, ok := strings.Cut(line, "="); ok {
			response.Child[name] = value
		}
	}
	// Remove only test/runtime harness inputs, not prepared Source fields.
	for _, name := range []string{"MINILOOP_BOOTSTRAP_PROBE", "PATH", "GOMAXPROCS", "PWD", "SHLVL", "_"} {
		delete(response.Environment, name)
		delete(response.Child, name)
	}
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func TestActualSourceEffectsPublishedToChildProcesses(t *testing.T) {
	data, err := os.ReadFile("../../testdata/python-dotenv-boot.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			Name, CWD, Caller, Stage              string
			Interactive, Debugger, Frozen         bool
			Files                                 []struct{ Path, Content string }
			Environment, Applied, PostEnvironment map[string]string
			PostFile                              *string
			Fault                                 *dotenv.Failure
		}
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	for _, row := range corpus.Cases {
		t.Run(row.Name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range row.Files {
				path := filepath.Join(root, file.Path)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				data, err := hex.DecodeString(file.Content)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			// Compiled commands follow Source frozen origin. For Source ordinary
			// caller profiles, put cwd at the equivalent package-origin directory.
			cwd := filepath.Dir(filepath.Join(root, row.Caller))
			if row.Frozen || row.Interactive || row.Debugger {
				cwd = filepath.Join(root, row.CWD)
			}
			if err := os.MkdirAll(cwd, 0700); err != nil {
				t.Fatal(err)
			}
			input, _ := json.Marshal(probeRequest{PostEnvironment: row.PostEnvironment, PostFile: row.PostFile, FilePath: filepath.Join(root, "source/.env")})
			command := exec.Command(os.Args[0], "-test.run=^TestProcessBootstrapProbe$")
			command.Dir = cwd
			command.Stdin = strings.NewReader(string(input))
			command.Env = []string{"MINILOOP_BOOTSTRAP_PROBE=1", "PATH=" + os.Getenv("PATH"), "GOMAXPROCS=2"}
			for name, value := range row.Environment {
				command.Env = append(command.Env, name+"="+strings.ReplaceAll(value, "$ROOT", root))
			}
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("%v: %s", err, output)
			}
			var response probeResponse
			if err := json.Unmarshal(output, &response); err != nil {
				t.Fatalf("%v: %s", err, output)
			}
			expected := map[string]string{}
			for name, value := range row.Applied {
				expected[name] = strings.ReplaceAll(value, "$ROOT", root)
			}
			if !reflect.DeepEqual(response.Environment, expected) || !reflect.DeepEqual(response.Child, expected) {
				t.Fatalf("prepared/child effects differ: %#v; want %#v", response, expected)
			}
			var failure *dotenv.Failure
			if row.Stage == "import" {
				failure = row.Fault
			}
			if !reflect.DeepEqual(response.Failure, failure) {
				t.Fatal(response.Failure, failure)
			}
			disabled := dotenv.Environment{}
			for name, value := range row.Environment {
				disabled[dotenv.Name(name)] = value
			}
			if response.DiscoveryEnabled == dotenv.LoadingDisabled(disabled) {
				t.Fatal("incorrect discovery status")
			}
			if strings.Contains(response.Diagnostic, "incoming-key") {
				t.Fatal("diagnostic leaked credential")
			}
		})
	}
}
