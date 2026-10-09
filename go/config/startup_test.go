package config

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/dotenv"
)

type bootSettings struct {
	Model         string
	BaseURL       *string
	APIKey        *string
	MaxTokens     int
	FakeLLM       bool
	WorkspaceRoot string
}

func TestActualSourceConfigImportPreparation(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-dotenv-boot.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Version string
		Cases   []struct {
			Name, CWD, Caller, Stage                                  string
			Files                                                     []struct{ Path, Content string }
			Environment                                               map[string]string
			Interactive, Debugger, Frozen, Imported, WorkspaceCreated bool
			PostEnvironment                                           map[string]string
			PostFile                                                  *string
			Settings                                                  *bootSettings
			Fault                                                     *dotenv.Failure
			Applied                                                   map[string]string
		}
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.Version != dotenv.SourceVersion || len(corpus.Cases) != 18 {
		t.Fatal("Source boot corpus changed")
	}
	processBefore := os.Environ()
	for _, row := range corpus.Cases {
		t.Run(row.Name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			cwd, caller := filepath.Join(root, row.CWD), filepath.Join(root, row.Caller)
			if err := os.MkdirAll(cwd, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(caller), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(caller, nil, 0600); err != nil {
				t.Fatal(err)
			}
			for _, file := range row.Files {
				path := filepath.Join(root, file.Path)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				content, err := hex.DecodeString(file.Content)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, content, 0600); err != nil {
					t.Fatal(err)
				}
			}
			normalize := func(input map[string]string) map[string]string {
				result := make(map[string]string, len(input))
				for key, value := range input {
					result[key] = strings.ReplaceAll(value, "$ROOT", root)
				}
				return result
			}
			environment := normalize(row.Environment)
			before, _ := json.Marshal(environment)
			prepared := PrepareEnvironment(environment, dotenv.DiscoveryContext{WorkingDirectory: cwd, CallerFiles: []string{caller}, Interactive: row.Interactive, Debugger: row.Debugger, Frozen: row.Frozen})
			applied := prepared.Reveal()
			if row.Stage == "import" {
				if !reflect.DeepEqual(prepared.Failure, row.Fault) {
					t.Fatalf("import fault: %#v; want %#v", prepared.Failure, row.Fault)
				}
			} else {
				if prepared.Failure != nil {
					t.Fatal(prepared.Failure)
				}
				for name, value := range row.PostEnvironment {
					applied[name] = value
				}
				if row.PostFile != nil {
					if err := os.WriteFile(filepath.Join(root, "source/.env"), []byte(*row.PostFile), 0600); err != nil {
						t.Fatal(err)
					}
				}
				settings, err := Load(applied, LoadOptions{Directory: cwd})
				if row.Stage == "settings" {
					// Existing Load diagnostics differ in wording; import-stage faults
					// above compare the exact Source class/message.
					if err == nil {
						t.Fatal("Source settings refusal accepted")
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					key := settings.APIKey.Reveal()
					var keyPointer *string
					if key != "" {
						keyPointer = &key
					}
					observed := bootSettings{Model: settings.Model, BaseURL: settings.BaseURL, APIKey: keyPointer, MaxTokens: settings.MaxTokens, FakeLLM: settings.FakeLLM, WorkspaceRoot: strings.ReplaceAll(settings.WorkspaceRoot, root, "$ROOT")}
					if !reflect.DeepEqual(&observed, row.Settings) {
						t.Fatalf("settings: %#v; want %#v", observed, row.Settings)
					}
				}
			}
			if !reflect.DeepEqual(applied, normalize(row.Applied)) {
				t.Fatalf("environment differs: %#v; want %#v", applied, normalize(row.Applied))
			}
			after, _ := json.Marshal(environment)
			if string(before) != string(after) {
				t.Fatal("input environment changed")
			}
			applied["MODEL_ID"] = "changed"
			if prepared.Reveal()["MODEL_ID"] == "changed" {
				t.Fatal("revealed environment aliases preparation")
			}
			if strings.Contains(fmt.Sprintf("%v %#v", prepared, prepared), "incoming-key") {
				t.Fatal("diagnostic exposes a credential")
			}
			if _, err := os.Stat(filepath.Join(root, "workspaces")); !os.IsNotExist(err) {
				t.Fatal("pure preparation/Load created workspace", err)
			}
		})
	}
	if !reflect.DeepEqual(processBefore, os.Environ()) {
		t.Fatal("preparation changed process environment")
	}
}
