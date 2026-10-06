package userresources

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/skills"
)

type resourceFile struct{ Source, Owner, Name, Text string }
type resourceStep struct {
	Op, Owner, Scope, Key, Descriptions, Output, Name, Body string
	Cached, Failed                                          bool
	File                                                    resourceFile
	Problems                                                []string
	LocalProblems                                           []string `json:"local_problems"`
}

func TestResourcesMatchActualPython(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-owner-resources.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Secret string
			Files        []resourceFile
			Steps        []resourceStep
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			ctx := context.Background()
			base := t.TempDir()
			agentDir := filepath.Join(base, "agent")
			root := filepath.Join(base, "users")
			if err := os.Mkdir(agentDir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			write := func(file resourceFile) {
				directory := agentDir
				if file.Source == "user" {
					key, err := OwnerDirectoryKey(file.Owner)
					if err != nil {
						t.Fatal(err)
					}
					directory = filepath.Join(root, string(key), "skills")
				}
				path := filepath.Join(directory, file.Name, "SKILL.md")
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(file.Text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for _, file := range row.Files {
				write(file)
			}
			agent, err := skills.NewCatalog(ctx, agentDir)
			if err != nil {
				t.Fatal(err)
			}
			var masker memory.Masker
			if row.Secret != "" {
				registry := secrets.New(secrets.Config{})
				registry.RegisterValue("TEST_SECRET", row.Secret)
				masker = registry
			}
			resolver, err := NewResolver(ctx, root, agent, masker)
			if err != nil {
				t.Fatal(err)
			}
			pinned := make(map[OwnerID]Resources)
			normalize := func(text string) string { return strings.NewReplacer(root, "$USERS", agentDir, "$AGENT").Replace(text) }
			for _, step := range row.Steps {
				owner := OwnerID(step.Owner)
				if step.Op == "file" {
					write(step.File)
					continue
				}
				if step.Op == "new-resolver" {
					resolver, err = NewResolver(ctx, root, agent, masker)
					if err != nil {
						t.Fatal(err)
					}
					continue
				}
				// Operator Problems is evaluated before ForOwner, matching the source fixture.
				var problems []string
				if step.Op == "problems" {
					log, err := resolver.Problems(ctx)
					if err != nil {
						t.Fatal(err)
					}
					problems = []string{}
					for _, p := range log {
						problems = append(problems, normalize(p.Message))
					}
				}
				resource, err := resolver.ForOwner(ctx, owner)
				if err != nil {
					t.Fatal(err)
				}
				switch step.Op {
				case "resolve":
					prior, ok := pinned[owner]
					cached := ok && prior == resource
					if !ok {
						pinned[owner] = resource
					}
					if cached != step.Cached || string(resource.Scope()) != step.Scope || string(resource.Directories().Key()) != step.Key || resource.Skills().Descriptions() != step.Descriptions {
						t.Fatal("snapshot mismatch", step.Owner)
					}
					if resource.Owner() != owner || resource.Memory().Owner() != memory.OwnerID(owner) {
						t.Fatal("owner binding drift")
					}
				case "load":
					output, err := resource.Skills().Load(ctx, protocol.LoadSkillInput{Name: step.Name})
					if err != nil {
						output = "Error: " + err.Error()
					}
					if output != step.Output || (err != nil) != step.Failed {
						t.Fatal(output, step.Output)
					}
				case "remember":
					output, err := resource.Memory().Write(ctx, memory.Input{Name: step.Name, Body: step.Body})
					if err != nil || output != step.Output {
						t.Fatal(output, err)
					}
				case "index":
					output, err := resource.Memory().Index(ctx)
					if err != nil || output != step.Output {
						t.Fatal(output, err)
					}
				case "problems":
					local := []string{}
					for _, p := range resource.Skills().Problems() {
						local = append(local, normalize(p.Message))
					}
					if !reflect.DeepEqual(problems, step.Problems) || !reflect.DeepEqual(local, step.LocalProblems) {
						t.Fatal(problems, step.Problems, local, step.LocalProblems)
					}
				default:
					t.Fatal(step.Op)
				}
			}
		})
	}
}

func TestResolverConcurrentReuseFailureRetryAndCancellation(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "users")
	resolver, err := NewResolver(ctx, root, skills.EmptyCatalog(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	values := make(chan Resources, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := resolver.ForOwner(ctx, "alice")
			if err != nil {
				t.Error(err)
			}
			values <- r
		}()
	}
	wg.Wait()
	close(values)
	var first Resources
	for r := range values {
		if first.Skills() == nil {
			first = r
		}
		if r != first {
			t.Fatal("different cached resource")
		}
	}
	key, _ := OwnerDirectoryKey("retry")
	ownerRoot := filepath.Join(root, string(key))
	skillRoot := filepath.Join(ownerRoot, "skills")
	memoryRoot := filepath.Join(ownerRoot, "memory")
	outside := t.TempDir()
	if err := os.MkdirAll(skillRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, memoryRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.ForOwner(ctx, "retry"); err == nil {
		t.Fatal("memory link accepted")
	}
	if err := os.Remove(memoryRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(skillRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, skillRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.ForOwner(ctx, "retry"); err == nil {
		t.Fatal("retry reused partial binding")
	}
	if err := os.Remove(skillRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.ForOwner(ctx, "retry"); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := resolver.ForOwner(cancelled, "never"); err != context.Canceled {
		t.Fatal(err)
	}
	key, _ = OwnerDirectoryKey("never")
	if _, err := os.Stat(filepath.Join(root, string(key))); !os.IsNotExist(err) {
		t.Fatal("cancelled construction left directories")
	}
	if _, err := resolver.ForOwner(ctx, ""); err == nil {
		t.Fatal("empty owner accepted")
	}
	if _, err := NewResolver(ctx, filepath.Join(root, "nil-agent"), nil, nil); err == nil {
		t.Fatal("nil catalogue accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "nil-agent")); !os.IsNotExist(err) {
		t.Fatal("invalid construction created root")
	}
}
