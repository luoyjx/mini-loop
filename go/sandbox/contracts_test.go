package sandbox

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/shell"
	"github.com/luoyjx/mini-loop/go/workspace"
)

type snapshot struct {
	Kind         string   `json:"kind"`
	Confined     bool     `json:"confined"`
	Describe     string   `json:"describe"`
	Argv         []string `json:"argv"`
	Reason       *string  `json:"reason"`
	Writable     []string `json:"writable"`
	Unreadable   []string `json:"unreadable"`
	Extra        []string `json:"extra"`
	AllowNetwork bool     `json:"allow_network"`
	Preview      string   `json:"preview"`
}
type recipe struct {
	Name         string   `json:"name"`
	Writable     []string `json:"writable"`
	Unreadable   []string `json:"unreadable"`
	Extra        []string `json:"extra"`
	AllowNetwork bool     `json:"allow_network"`
	Target       string   `json:"target"`
	Before       snapshot `json:"before"`
	After        snapshot `json:"after"`
	SameInstance bool     `json:"same_instance"`
}
type contractError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Name    string `json:"name"`
}
type selection struct {
	System             string         `json:"system"`
	BinaryExists       bool           `json:"binary_exists"`
	ProtectCredentials bool           `json:"protect_credentials"`
	Require            bool           `json:"require"`
	Available          bool           `json:"available"`
	UnavailableReason  *string        `json:"unavailable_reason"`
	Result             *snapshot      `json:"result"`
	Error              *contractError `json:"error"`
}
type markerRecipe struct {
	Name     string   `json:"name"`
	Content  *string  `json:"content"`
	Writable []string `json:"writable"`
}
type contracts struct {
	Command               string          `json:"command"`
	Cases                 []recipe        `json:"cases"`
	Selections            []selection     `json:"selections"`
	Markers               []markerRecipe  `json:"markers"`
	CredentialRoots       []string        `json:"credential_roots"`
	FactoryDefaults       snapshot        `json:"factory_defaults"`
	Errors                []contractError `json:"errors"`
	Null                  snapshot        `json:"null"`
	Unavailable           snapshot        `json:"unavailable"`
	NullRebindSame        bool            `json:"null_rebind_same"`
	UnavailableRebindSame bool            `json:"unavailable_rebind_same"`
}

func fixture(t *testing.T) contracts {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-sandbox.json")
	if err != nil {
		t.Fatal(err)
	}
	var result contracts
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}
func testRoot(t *testing.T) string {
	t.Helper()
	root, err := workspace.ResolvePath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}
func replacePaths(values []string, from, to string) []string {
	if values == nil {
		return nil
	}
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = strings.ReplaceAll(value, from, to)
	}
	return result
}
func observe(t *testing.T, sandbox shell.Sandbox, command, root string) snapshot {
	t.Helper()
	argv, err := sandbox.Argv(command)
	if err != nil {
		t.Fatal(err)
	}
	result := snapshot{Argv: replacePaths(argv, root, "<ROOT>")}
	switch sandbox := sandbox.(type) {
	case *SeatbeltSandbox:
		config := sandbox.Configuration()
		result.Kind = "SeatbeltSandbox"
		result.Confined = sandbox.Confined()
		result.Describe = sandbox.Describe()
		result.Writable = replacePaths(config.WritableRoots, root, "<ROOT>")
		result.Unreadable = replacePaths(config.UnreadableRoots, root, "<ROOT>")
		result.Extra = replacePaths(config.ExtraWritable, root, "<ROOT>")
		result.AllowNetwork = config.AllowNetwork
		result.Preview = strings.ReplaceAll(sandbox.Preview(command), root, "<ROOT>")
	case *NullSandbox:
		result.Kind = "NullSandbox"
		result.Describe = sandbox.Describe()
		result.Confined = sandbox.Confined()
	case *UnavailableSandbox:
		result.Kind = "UnavailableSandbox"
		result.Describe = sandbox.Describe()
		result.Confined = sandbox.Confined()
		result.Reason = sandbox.SandboxReason()
	default:
		t.Fatalf("unsupported sandbox %T", sandbox)
	}
	result.Describe = strings.ReplaceAll(result.Describe, root, "<ROOT>")
	return result
}
func equalSnapshot(t *testing.T, actual, expected snapshot) {
	t.Helper()
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("snapshot mismatch\ngot: %#v\nwant: %#v", actual, expected)
	}
}
func TestSourceConstructionAndRebinding(t *testing.T) {
	contract := fixture(t)
	root := testRoot(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	for _, name := range []string{"home", "workspace", "linked", "alternate"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for name, marker := range map[string]string{"linked": "gitdir: ../repo/.git/worktrees/linked\n", "alternate": "gitdir: ../other/.git/worktrees/alternate\n"} {
		if err := os.WriteFile(filepath.Join(root, name, ".git"), []byte(marker), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "workspace"), filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	for _, row := range contract.Cases {
		t.Run(row.Name, func(t *testing.T) {
			sandbox, err := NewSeatbelt(Config{WritableRoots: replacePaths(row.Writable, "<ROOT>", root), UnreadableRoots: replacePaths(row.Unreadable, "<ROOT>", root), ExtraWritable: replacePaths(row.Extra, "<ROOT>", root), AllowNetwork: row.AllowNetwork})
			if err != nil {
				t.Fatal(err)
			}
			equalSnapshot(t, observe(t, sandbox, contract.Command, root), row.Before)
			rebound, err := sandbox.ForWorkspace(strings.ReplaceAll(row.Target, "<ROOT>", root))
			if err != nil {
				t.Fatal(err)
			}
			if (rebound == sandbox) != row.SameInstance {
				t.Fatal("rebind identity differs")
			}
			equalSnapshot(t, observe(t, rebound, contract.Command, root), row.After)
		})
	}
	for _, row := range contract.Markers {
		t.Run("marker-"+row.Name, func(t *testing.T) {
			path := filepath.Join(root, "workspace", ".git")
			if row.Content == nil {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte(strings.ReplaceAll(*row.Content, "<ROOT>", root)), 0600); err != nil {
				t.Fatal(err)
			}
			sandbox, err := NewSeatbelt(Config{WritableRoots: []string{filepath.Join(root, "workspace")}})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(replacePaths(sandbox.Configuration().WritableRoots, root, "<ROOT>"), row.Writable) {
				t.Fatal("git writable roots differ")
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, row := range contract.Errors {
		var err error
		if row.Name == "empty-writable" {
			_, err = NewSeatbelt(Config{})
		} else {
			path := filepath.Join(root, "workspace", ".git")
			if err := os.WriteFile(path, []byte{0xff}, 0600); err != nil {
				t.Fatal(err)
			}
			_, err = NewSeatbelt(Config{WritableRoots: []string{filepath.Join(root, "workspace")}})
		}
		if err == nil || err.Error() != row.Message {
			t.Fatalf("%s error: %v", row.Name, err)
		}
	}
}
func TestSourceDefaultPlatformSelections(t *testing.T) {
	contract := fixture(t)
	root := testRoot(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	for _, row := range contract.Selections {
		host := hostFacts{system: row.System, binaryExists: row.BinaryExists}
		if !reflect.DeepEqual(host.reason(), row.UnavailableReason) || (host.reason() == nil) != row.Available {
			t.Fatal("availability differs")
		}
		value, err := defaultForHost(filepath.Join(root, "workspace"), DefaultOptions{UnreadableRoots: []string{filepath.Join(root, "operator")}, AllowNetwork: true, Require: row.Require, ProtectCredentials: &row.ProtectCredentials}, host)
		if row.Error != nil {
			failure, ok := err.(*SandboxUnavailable)
			if !ok || failure.Error() != row.Error.Message || value != nil {
				t.Fatalf("expected unavailable: %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		equalSnapshot(t, observe(t, value, contract.Command, root), *row.Result)
	}
	value, err := defaultForHost(filepath.Join(root, "workspace"), DefaultOptions{}, hostFacts{system: "Darwin", binaryExists: true})
	if err != nil {
		t.Fatal(err)
	}
	equalSnapshot(t, observe(t, value, contract.Command, root), contract.FactoryDefaults)
	roots, err := DefaultUnreadableRoots()
	if err != nil || !reflect.DeepEqual(replacePaths(roots, root, "<ROOT>"), contract.CredentialRoots) {
		t.Fatal("credential roots differ", err)
	}
	null := &NullSandbox{}
	unavailable := NewUnavailable("fixture unavailable")
	equalSnapshot(t, observe(t, null, contract.Command, root), contract.Null)
	equalSnapshot(t, observe(t, unavailable, contract.Command, root), contract.Unavailable)
	nullBound, _ := null.ForWorkspace(root)
	unavailableBound, _ := unavailable.ForWorkspace(root)
	if (nullBound == null) != contract.NullRebindSame || (unavailableBound == unavailable) != contract.UnavailableRebindSame {
		t.Fatal("inert wrappers changed on rebind")
	}
}
