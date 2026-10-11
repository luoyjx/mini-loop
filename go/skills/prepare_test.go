package skills_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/skills"
	"github.com/luoyjx/mini-loop/go/userresources"
)

func putSource(t *testing.T, path, document string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedCanonicalSourcesMatchPythonFixturesAndRestart(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-user-skills.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Input struct {
				Name, Description, Body string
				DescriptionRepeat       int `json:"description_repeat"`
				BodyRepeat              int `json:"body_repeat"`
			}
			ErrorCode  string `json:"error_code"`
			TextDigest string `json:"text_sha256"`
			BodyDigest string `json:"body_sha256"`
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		if row.ErrorCode != "" {
			continue
		}
		t.Run(row.Input.Name+"/"+row.TextDigest[:8], func(t *testing.T) {
			fields := userresources.SkillFields{Name: row.Input.Name, Description: row.Input.Description, Body: row.Input.Body}
			if row.Input.DescriptionRepeat > 0 {
				fields.Description = strings.Repeat(fields.Description, row.Input.DescriptionRepeat)
			}
			if row.Input.BodyRepeat > 0 {
				fields.Body = strings.Repeat(fields.Body, row.Input.BodyRepeat)
			}
			canonical, err := userresources.NewCanonicalSkill(fields)
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			path := filepath.Join(root, "middle", "SKILL.md")
			original := skills.EmptyCatalog()
			prepared, err := original.WithSourceDocument(context.Background(), path, canonical.Text())
			if err != nil {
				t.Fatal(err)
			}
			if len(original.Entries()) != 0 {
				t.Fatal("original snapshot mutated")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("preparation wrote a file", err)
			}
			entry := prepared.Entries()[0]
			if entry.SourceDigest != row.TextDigest || entry.Digest != row.BodyDigest {
				t.Fatal("Python canonical digests differ", entry)
			}
			if _, err := prepared.Load(context.Background(), protocol.LoadSkillInput{Name: fields.Name}); err == nil {
				t.Fatal("missing committed source was served")
			}
			putSource(t, path, canonical.Text())
			if _, err := original.Load(context.Background(), protocol.LoadSkillInput{Name: fields.Name}); err == nil {
				t.Fatal("original snapshot lookup mutated")
			}
			restarted, err := skills.NewCatalog(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(prepared.Entries(), restarted.Entries()) || prepared.Descriptions() != restarted.Descriptions() {
				t.Fatal("restart snapshot differs")
			}
			before, err := prepared.Load(context.Background(), protocol.LoadSkillInput{Name: fields.Name})
			if err != nil {
				t.Fatal(err)
			}
			after, err := restarted.Load(context.Background(), protocol.LoadSkillInput{Name: fields.Name})
			if err != nil || before != after {
				t.Fatal("restart load differs", err)
			}
			putSource(t, path, canonical.Text()+"changed")
			if _, err := prepared.Load(context.Background(), protocol.LoadSkillInput{Name: fields.Name}); err == nil {
				t.Fatal("changed source was served")
			}
		})
	}
}

func TestPreparedSourceOrderingAndIndependentDiagnostics(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "z"} {
		putSource(t, filepath.Join(root, name, "SKILL.md"), "---\nname: "+name+"\ndescription: description\n---\nbody\n")
	}
	putSource(t, filepath.Join(root, "invalid", "SKILL.md"), "---\nname: invalid/name\n---\nbody")
	original, err := skills.NewCatalog(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "middle", "SKILL.md")
	document := "---\nname: middle\ndescription: description\n---\nbody\n"
	prepared, err := original.WithSourceDocument(context.Background(), path, document)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original.Problems(), prepared.Problems()) || original.ProblemStatistics() != prepared.ProblemStatistics() {
		t.Fatal("diagnostic history lost")
	}
	if got := prepared.Entries(); len(got) != 3 || got[0].Name != "a" || got[1].Name != "middle" || got[2].Name != "z" {
		t.Fatal("source order differs", got)
	}
	putSource(t, path, document)
	restarted, err := skills.NewCatalog(context.Background(), root)
	if err != nil || !reflect.DeepEqual(prepared.Entries(), restarted.Entries()) {
		t.Fatal("ordered restart differs", err)
	}
	putSource(t, filepath.Join(root, "a", "SKILL.md"), "changed")
	var group sync.WaitGroup
	for i := 0; i < 20; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			_, _ = original.Load(context.Background(), protocol.LoadSkillInput{Name: "a"})
			_, _ = original.WithSourceDocument(context.Background(), path, document)
		}()
	}
	group.Wait()
	if len(prepared.Problems()) != 1 || prepared.ProblemStatistics().Total != 1 {
		t.Fatal("old diagnostic mutations aliased clone")
	}
	if _, err := prepared.Load(context.Background(), protocol.LoadSkillInput{Name: "a"}); err == nil {
		t.Fatal("copied entry lost verification")
	}
	if prepared.ProblemStatistics().Total != 2 || original.ProblemStatistics().Total != 21 {
		t.Fatal("diagnostic ownership differs")
	}
}

func TestPreparedSourceRefusesMalformedAndCollidingInputs(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "note", "SKILL.md")
	document := "---\nname: note\ndescription: description\n---\nbody\n"
	for _, test := range []struct{ name, path, document string }{
		{"relative", "note/SKILL.md", document}, {"wrong-file", filepath.Join(filepath.Dir(path), "other.md"), document},
		{"nul-path", path + "\x00", document}, {"missing-name", path, "body"},
		{"name", path, strings.Replace(document, "note", "invalid/name", 1)},
		{"description", path, strings.Replace(document, "description: description", "description: ", 1)},
		{"long-description", path, strings.Replace(document, "description: description", "description: "+strings.Repeat("a", 201), 1)},
		{"empty-body", path, "---\nname: note\ndescription: d\n---\n"},
		{"long-body", path, document + strings.Repeat("x", skills.MaxBody)},
		{"raw-limit", path, strings.Repeat(" ", 4*(skills.MaxBody+skills.MaxDescription+64+512)+1)},
		{"invalid-utf8", path, document + "\xff"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if result, err := skills.EmptyCatalog().WithSourceDocument(ctx, test.path, test.document); err == nil || result != nil {
				t.Fatal("malformed source admitted")
			}
		})
	}
	prepared, err := skills.EmptyCatalog().WithSourceDocument(ctx, path, document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.WithSourceDocument(ctx, path, document); !errors.Is(err, skills.ErrSkillExists) {
		t.Fatal("name collision accepted", err)
	}
	if _, err := prepared.WithSourceDocument(ctx, path, strings.Replace(document, "name: note", "name: other", 1)); !errors.Is(err, skills.ErrSkillPathExists) {
		t.Fatal("path collision accepted", err)
	}
	var absent *skills.Catalog
	if _, err := absent.WithSourceDocument(ctx, path, document); !errors.Is(err, skills.ErrPreparedSource) {
		t.Fatal("nil catalogue accepted", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := prepared.WithSourceDocument(cancelled, path, document); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}
