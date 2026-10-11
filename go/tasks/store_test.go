package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/luoyjx/mini-loop/go/secrets"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestActualPythonTaskTransitionsPrivacyAndViews(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-tasks.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name   string
			Secret *string
			Steps  []struct {
				Input struct {
					Op, ID, Subject, Description, Owner, Name, Text string
					Dependencies                                    []ID
					Worktree                                        *string
					Count                                           int
				}
				Value          json.RawMessage
				Error          *string
				Problems       []string
				Total, Dropped uint64
			}
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range fixture.Cases {
		t.Run(scenario.Name, func(t *testing.T) {
			config := Config{Workspace: t.TempDir()}
			if scenario.Secret != nil {
				registry := secrets.New(secrets.Config{})
				registry.RegisterValue("KEY", *scenario.Secret)
				config.Secrets = registry
			}
			store, err := New(config)
			if err != nil {
				t.Fatal(err)
			}
			counter := 0
			store.nextID = func() (ID, error) { counter++; return ID(fmt.Sprintf("task_%012d", counter)), nil }
			for index, step := range scenario.Steps {
				input := step.Input
				var actual []byte
				var err error
				encode := func(value interface{}) { actual, err = json.Marshal(value) }
				// Dynamic test expectations remain at the test serialization boundary.
				switch input.Op {
				case "create":
					var task Task
					task, err = store.Create(input.Subject, input.Description, input.Dependencies, input.Worktree)
					if err == nil {
						encode(task)
					}
				case "claim":
					var text string
					text, err = store.Claim(ID(input.ID), Owner(input.Owner))
					if err == nil {
						encode(text)
					}
				case "complete":
					var text string
					var owner *Owner
					if input.Owner != "" {
						value := Owner(input.Owner)
						owner = &value
					}
					text, err = store.Complete(ID(input.ID), owner)
					if err == nil {
						encode(text)
					}
				case "bind":
					var text string
					text, err = store.BindWorktree(ID(input.ID), input.Name)
					if err == nil {
						encode(text)
					}
				case "load":
					var task *Task
					task, err = store.Load(ID(input.ID))
					if err == nil {
						encode(task)
					}
				case "list", "runnable":
					var list []Task
					if input.Op == "list" {
						list, err = store.List()
					} else {
						list, err = store.Runnable()
					}
					if err == nil {
						encode(list)
					}
				case "can_start":
					var ready bool
					ready, err = store.CanStart(ID(input.ID))
					if err == nil {
						encode(ready)
					}
				case "render":
					var text string
					text, err = store.Render()
					if err == nil {
						encode(text)
					}
				case "file":
					err = os.WriteFile(filepath.Join(store.Root(), input.Name), []byte(input.Text), 0600)
					actual = []byte("null")
				case "marker":
					err = os.WriteFile(filepath.Join(store.Root(), input.ID+".owner"), []byte(input.Text), 0600)
					actual = []byte("null")
				case "batch":
					for count := 0; count < input.Count; count++ {
						_, err = store.Create(input.Subject, "", nil, nil)
						if err != nil {
							break
						}
					}
					actual = []byte("null")
				default:
					t.Fatalf("unknown fixture op %s", input.Op)
				}
				if step.Error != nil {
					if err == nil || err.Error() != *step.Error {
						t.Fatalf("%d error: %v, expected %s", index, err, *step.Error)
					}
				} else {
					if err != nil {
						t.Fatalf("%d: %v", index, err)
					}
					var got, want interface{}
					json.Unmarshal(actual, &got)
					json.Unmarshal(step.Value, &want)
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("%d %s mismatch: %.400s / %.400s", index, input.Op, actual, step.Value)
					}
				}
				diagnostics := store.Diagnostics()
				messages := []string{}
				for _, p := range diagnostics.Problems {
					text := p.Message
					if p.Count > 1 {
						text += fmt.Sprintf(" (x%d)", p.Count)
					}
					messages = append(messages, text)
				}
				if !reflect.DeepEqual(messages, step.Problems) || diagnostics.Total != step.Total || diagnostics.Dropped != step.Dropped {
					t.Fatalf("%d diagnostics: %+v / %v", index, diagnostics, step.Problems)
				}
			}
		})
	}
}
func TestClaimSubprocess(t *testing.T) {
	root := os.Getenv("GO_TASK_TEST_ROOT")
	if root == "" {
		return
	}
	owner := Owner(os.Getenv("GO_TASK_TEST_OWNER"))
	store, err := New(Config{Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, string(owner)+".ready"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err = os.Stat(filepath.Join(root, "start")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("claim barrier timeout")
		}
		time.Sleep(5 * time.Millisecond)
	}
	result, err := store.Claim(ID(os.Getenv("GO_TASK_TEST_ID")), owner)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(result)
}
func TestClaimsAcrossProcessesAndSpentMarker(t *testing.T) {
	root := t.TempDir()
	store, err := New(Config{Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Create("shared", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	type child struct {
		cmd    *exec.Cmd
		output strings.Builder
	}
	children := make([]child, 2)
	for index, owner := range []string{"alice", "bob"} {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestClaimSubprocess$", "-test.v")
		cmd.Env = append(os.Environ(), "GO_TASK_TEST_ROOT="+root, "GO_TASK_TEST_OWNER="+owner, "GO_TASK_TEST_ID="+string(task.ID))
		children[index].cmd = cmd
		cmd.Stdout = &children[index].output
		cmd.Stderr = &children[index].output
		if err = cmd.Start(); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, a := os.Stat(filepath.Join(root, "alice.ready"))
		_, b := os.Stat(filepath.Join(root, "bob.ready"))
		if a == nil && b == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("children did not become ready")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err = os.WriteFile(filepath.Join(root, "start"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	winners := 0
	for index := range children {
		child := &children[index]
		if err = child.cmd.Wait(); err != nil {
			t.Fatalf("claimer: %v %s", err, child.output.String())
		}
		if strings.Contains(child.output.String(), "Claimed "+string(task.ID)) {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("%d successful cross-process claimers", winners)
	}
	fresh, err := New(Config{Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	record, err := fresh.Load(task.ID)
	if err != nil || record == nil || record.Owner == nil || record.Status != InProgress {
		t.Fatalf("record lost: %+v %v", record, err)
	}
	if _, err = fresh.Complete(task.ID, record.Owner); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(store.Root(), string(task.ID)+".owner")); !os.IsNotExist(err) {
		t.Fatal("spent marker retained")
	}
	verdict, err := fresh.Claim(task.ID, "third")
	if err != nil || !strings.Contains(verdict, "completed, not claimable") {
		t.Fatal("completion reopened claim", verdict, err)
	}
}
