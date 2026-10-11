package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestLayeredMatchesActualPython(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-layered-skills.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name    string
			Recipes []struct {
				Source, Name, Description, Body string
				Count                           int
			}
			Mutations []struct {
				Source, Name, Text string
				Remove             bool
			}
			Descriptions  string
			Loads         []expectedLoad
			Problems      []expectedProblem
			AgentProblems []expectedProblem `json:"agent_problems"`
			UserProblems  []expectedProblem `json:"user_problems"`
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			base := t.TempDir()
			roots := map[string]string{"agent": filepath.Join(base, "agent"), "user": filepath.Join(base, "user")}
			for _, root := range roots {
				if err := os.Mkdir(root, 0700); err != nil {
					t.Fatal(err)
				}
			}
			for _, recipe := range row.Recipes {
				for i := 0; i < recipe.Count; i++ {
					name := recipe.Name
					if recipe.Count != 1 {
						name = fmt.Sprintf("%s-%03d", name, i)
					}
					path := filepath.Join(roots[recipe.Source], name, "SKILL.md")
					if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
						t.Fatal(err)
					}
					text := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n%s", name, recipe.Description, recipe.Body)
					if err := os.WriteFile(path, []byte(text), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			ctx := context.Background()
			agent, err := NewCatalog(ctx, roots["agent"])
			if err != nil {
				t.Fatal(err)
			}
			user, err := NewCatalog(ctx, roots["user"])
			if err != nil {
				t.Fatal(err)
			}
			layered, err := NewLayeredCatalog(agent, user)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if got := layered.Descriptions(); got != row.Descriptions || utf8.RuneCountInString(got) > MaxCatalogue {
					t.Fatal("catalogue mismatch")
				}
			}
			for _, mutation := range row.Mutations {
				path := filepath.Join(roots[mutation.Source], mutation.Name, "SKILL.md")
				var err error
				if mutation.Remove {
					err = os.Remove(path)
				} else {
					err = os.WriteFile(path, []byte(mutation.Text), 0600)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, load := range row.Loads {
				output, err := layered.Load(ctx, load.Input)
				if err != nil {
					output = "Error: " + err.Error()
				}
				sum := sha256.Sum256([]byte(output))
				if (err != nil) != load.Failed || hex.EncodeToString(sum[:]) != load.SHA256 {
					t.Fatalf("%+v => %q", load.Input, output)
				}
			}
			normalize := func(text string) string {
				return strings.NewReplacer(roots["agent"], "$AGENT", roots["user"], "$USER").Replace(text)
			}
			check := func(actual []Problem, expected []expectedProblem) {
				t.Helper()
				if len(actual) != len(expected) {
					t.Fatalf("problems %v != %v", actual, expected)
				}
				for i, p := range actual {
					if normalize(p.Message) != expected[i].Message || p.Count != expected[i].Count {
						t.Fatalf("problem %v != %v", p, expected[i])
					}
				}
			}
			check(layered.Problems(), row.Problems)
			check(agent.Problems(), row.AgentProblems)
			check(user.Problems(), row.UserProblems)
			if row.Name == "both-flood" {
				var wg sync.WaitGroup
				for i := 0; i < 16; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						if layered.Descriptions() != row.Descriptions {
							t.Error("concurrent catalogue changed")
						}
						if _, err := layered.Load(ctx, protocol.LoadSkillInput{Name: "user:note-079"}); err != nil {
							t.Error(err)
						}
					}()
				}
				wg.Wait()
				if problems := layered.Problems(); len(problems) != 1 || problems[0].Count != 18 {
					t.Fatal("concurrent omissions lost", problems)
				}
			}
		})
	}
}

func TestLayeredBuiltinCancellationAndDetachedProblems(t *testing.T) {
	ctx := context.Background()
	builtin, err := NewBuiltinCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	layered, err := NewLayeredCatalog(builtin, EmptyCatalog())
	if err != nil {
		t.Fatal(err)
	}
	entry := builtin.Entries()[0]
	got, err := layered.Load(ctx, protocol.LoadSkillInput{Name: entry.Name})
	if err != nil || !strings.Contains(got, `source="agent"`) {
		t.Fatal(got, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := layered.Load(cancelled, protocol.LoadSkillInput{Name: entry.Name}); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := NewLayeredCatalog(nil, builtin); err == nil {
		t.Fatal("nil source accepted")
	}
	builtin.report(ProblemName, "construction diagnostic")
	next, err := NewLayeredCatalog(builtin, EmptyCatalog())
	if err != nil {
		t.Fatal(err)
	}
	problems := next.Problems()
	problems[0].Message = "changed"
	if next.Problems()[0].Message != "agent: construction diagnostic" {
		t.Fatal("problem snapshot aliased")
	}
	builtin.report(ProblemChanged, "later diagnostic")
	if len(next.Problems()) != 1 {
		t.Fatal("live source diagnostic copied")
	}
}
