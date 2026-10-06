package userresources

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/durable"
	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/skills"
)

type publicationFile struct {
	Owner        OwnerID
	Path, SHA256 string
	Mode         uint32
}
type publicationStep struct {
	Owner              OwnerID
	Fields             SkillFields
	Receipt            *PublicationReceipt
	Error              *PublicationFailure
	FutureDescriptions string `json:"future_descriptions"`
	OldDescriptions    string `json:"old_descriptions"`
	MemoryReused       bool   `json:"memory_reused"`
	LoadSHA256         string `json:"load_sha256"`
}
type publicationCase struct {
	Name, Seed      string
	SecretMode      string `json:"secret_mode"`
	Collision       bool
	Steps           []publicationStep
	Files           []publicationFile
	VictimUnchanged bool `json:"victim_unchanged"`
}

func publicationFields() SkillFields { return SkillFields{"review", "Review safely", "Read first."} }
func publicationResolver(t *testing.T, root string, masker memory.Masker) *Resolver {
	t.Helper()
	r, err := NewResolver(context.Background(), root, skills.EmptyCatalog(), masker)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func publicationWrite(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPublicationMatchesActualPython(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-user-publication.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Cases []publicationCase }
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			ctx := context.Background()
			base := t.TempDir()
			root := filepath.Join(base, "users")
			agentRoot := filepath.Join(base, "agent")
			if err := os.Mkdir(agentRoot, 0700); err != nil {
				t.Fatal(err)
			}
			if row.Collision {
				fields := publicationFields()
				fields.Body = "agent policy"
				canonical, _ := NewCanonicalSkill(fields)
				publicationWrite(t, filepath.Join(agentRoot, "review", "SKILL.md"), canonical.Text())
			}
			agent, err := skills.NewCatalog(ctx, agentRoot)
			if err != nil {
				t.Fatal(err)
			}
			var masker memory.Masker
			if row.SecretMode != "" {
				registry := secrets.New(secrets.Config{})
				masker = registry
				switch row.SecretMode {
				case "long":
					registry.RegisterValue("TEST_SECRET", "secret-token")
				case "raw":
					registry.RegisterValue("TEST_SECRET", " secret-token ")
				case "short":
					registry.RegisterValue("TEST_SECRET", "1234")
				case "unresolved":
					registry.RegisterLookup("TEST_SECRET", func() (string, error) { return "", errors.New("private lookup failure") })
				default:
					t.Fatal(row.SecretMode)
				}
			}
			resolver, err := NewResolver(ctx, root, agent, masker)
			if err != nil {
				t.Fatal(err)
			}
			previous := map[OwnerID]Resources{}
			for _, step := range row.Steps {
				if step.Owner != "" {
					if _, ok := previous[step.Owner]; !ok {
						value, err := resolver.ForOwner(ctx, step.Owner)
						if err != nil {
							t.Fatal(err)
						}
						previous[step.Owner] = value
					}
				}
			}
			key, _ := OwnerDirectoryKey("alice")
			target := filepath.Join(root, string(key), "skills", "review", "SKILL.md")
			victim := filepath.Join(base, "victim")
			if err := os.Mkdir(victim, 0700); err != nil {
				t.Fatal(err)
			}
			switch row.Seed {
			case "alternate":
				canonical, _ := NewCanonicalSkill(publicationFields())
				seedPath := filepath.Join(filepath.Dir(filepath.Dir(target)), "aaa", "SKILL.md")
				publicationWrite(t, seedPath, canonical.Text())
				if err := os.Chmod(seedPath, 0644); err != nil {
					t.Fatal(err)
				}
			case "directory-link":
				if err := os.Symlink(victim, filepath.Dir(target)); err != nil {
					t.Fatal(err)
				}
			case "file-link":
				publicationWrite(t, filepath.Join(victim, "SKILL.md"), "untouched")
				if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(victim, "SKILL.md"), target); err != nil {
					t.Fatal(err)
				}
			case "file-directory":
				if err := os.MkdirAll(target, 0700); err != nil {
					t.Fatal(err)
				}
			case "":
			default:
				t.Fatal(row.Seed)
			}
			for i, step := range row.Steps {
				publication, err := resolver.PublishSkill(ctx, step.Owner, step.Fields)
				if step.Error != nil {
					var failure *PublicationError
					if !errors.As(err, &failure) || failure.Receipt() != *step.Error {
						t.Fatalf("step %d: %v, want %+v", i, err, step.Error)
					}
					continue
				}
				if err != nil {
					t.Fatal(i, err)
				}
				if step.Receipt == nil || !reflect.DeepEqual(publication.Receipt(), *step.Receipt) {
					t.Fatalf("step %d receipt: %+v, want %+v", i, publication.Receipt(), step.Receipt)
				}
				future, err := resolver.ForOwner(ctx, step.Owner)
				if err != nil {
					t.Fatal(err)
				}
				if future != publication.Resources() || future.Memory() != previous[step.Owner].Memory() || !step.MemoryReused || future.Skills().Descriptions() != step.FutureDescriptions || previous[step.Owner].Skills().Descriptions() != step.OldDescriptions {
					t.Fatal(i, "future/old resource binding differs")
				}
				loaded, err := future.Skills().Load(ctx, protocol.LoadSkillInput{Name: "user:" + step.Fields.Name})
				if err != nil || digest(loaded) != step.LoadSHA256 {
					t.Fatal(i, "loaded instructions differ", err)
				}
				encoded, err := json.Marshal(publication.Receipt())
				if err != nil || strings.Contains(string(encoded), root) || strings.Contains(string(encoded), "resources") {
					t.Fatal("unsafe receipt", string(encoded), err)
				}
				if warning := publication.Receipt().CollisionWarning; warning != nil {
					*warning = "caller mutation"
					if *publication.Receipt().CollisionWarning == *warning {
						t.Fatal("receipt warning aliases publication")
					}
				}
			}
			actual := []publicationFile{}
			for owner := range previous {
				key, _ := OwnerDirectoryKey(string(owner))
				skillRoot := filepath.Join(root, string(key), "skills")
				err := filepath.WalkDir(skillRoot, func(path string, entry fs.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if entry.Name() != "SKILL.md" || entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
						return nil
					}
					data, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					info, err := entry.Info()
					if err != nil {
						return err
					}
					rel, _ := filepath.Rel(skillRoot, path)
					actual = append(actual, publicationFile{owner, rel, digest(string(data)), uint32(info.Mode().Perm())})
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			if len(actual) != len(row.Files) {
				t.Fatal("committed file count differs", actual, row.Files)
			}
			for _, expected := range row.Files {
				found := false
				for _, file := range actual {
					if file == expected {
						found = true
						break
					}
				}
				if !found {
					t.Fatal("committed file differs", expected, actual)
				}
			}
			data, err := os.ReadFile(filepath.Join(victim, "SKILL.md"))
			unchanged := os.IsNotExist(err) || err == nil && string(data) == "untouched"
			if unchanged != row.VictimUnchanged {
				t.Fatal("victim changed")
			}
		})
	}
}

func TestIndependentPublishersArbitrateCreateOnly(t *testing.T) {
	for _, identical := range []bool{true, false} {
		t.Run(map[bool]string{true: "identical", false: "conflicting"}[identical], func(t *testing.T) {
			root := t.TempDir()
			resolvers := []*Resolver{publicationResolver(t, root, nil), publicationResolver(t, root, nil)}
			const attempts = 20
			results := make([]Publication, attempts)
			failures := make([]error, attempts)
			var group sync.WaitGroup
			start := make(chan struct{})
			for i := range results {
				group.Add(1)
				go func(i int) {
					defer group.Done()
					<-start
					fields := publicationFields()
					if !identical {
						fields.Body = strings.Repeat("x", i+1)
					}
					results[i], failures[i] = resolvers[i%2].PublishSkill(context.Background(), "alice", fields)
				}(i)
			}
			close(start)
			group.Wait()
			created, retried, conflicts := 0, 0, 0
			for i, err := range failures {
				if err == nil {
					if results[i].Receipt().Idempotent {
						retried++
					} else {
						created++
					}
				} else {
					var failure *PublicationError
					if !errors.As(err, &failure) || failure.Code() != UserSkillExists {
						t.Fatal(err)
					}
					conflicts++
				}
			}
			if created != 1 || identical && (retried != attempts-1 || conflicts != 0) || !identical && (conflicts != attempts-1 || retried != 0) {
				t.Fatal(created, retried, conflicts)
			}
		})
	}
}

type maskerOnly struct{ mask func(string) string }

func (m maskerOnly) MaskText(value string) string { return m.mask(value) }

type namedMasker struct {
	maskerOnly
	names func() []secrets.Name
}

func (m namedMasker) Names() []secrets.Name { return m.names() }

type healthyMasker struct {
	namedMasker
	unresolved, short func() []secrets.Name
}

func (m healthyMasker) Unresolved() []secrets.Name  { return m.unresolved() }
func (m healthyMasker) ShortValues() []secrets.Name { return m.short() }

func TestPublicationSecretHealthFaultsAreSafeAndDoNotCreateOwnerFiles(t *testing.T) {
	identity := func(value string) string { return value }
	registered := func() []secrets.Name { return []secrets.Name{"PRIVATE_SECRET_NAME"} }
	none := func() []secrets.Name { return nil }
	for _, row := range []struct {
		name    string
		masker  memory.Masker
		code    PublicationCode
		message string
	}{
		{"mask-panic", maskerOnly{func(string) string { panic("private host value") }}, SecretCheckFailed, "Skill secret screening failed"},
		{"no-registration-surface", maskerOnly{identity}, SecretCheckFailed, "Skill secret screening is unavailable"},
		{"no-health-surface", namedMasker{maskerOnly{identity}, registered}, SecretCheckFailed, "Skill secret screening is unavailable"},
		{"names-panic", namedMasker{maskerOnly{identity}, func() []secrets.Name { panic("private name") }}, SecretCheckFailed, "Skill secret screening is unavailable"},
		{"health-panic", healthyMasker{namedMasker{maskerOnly{identity}, registered}, func() []secrets.Name { panic("private health") }, none}, SecretCheckFailed, "Skill secret screening is unavailable"},
		{"unresolved", healthyMasker{namedMasker{maskerOnly{identity}, registered}, registered, none}, SecretCheckFailed, "Skill secret screening is unavailable"},
		{"short", healthyMasker{namedMasker{maskerOnly{identity}, registered}, none, registered}, SecretCheckFailed, "Skill secret screening is unavailable"},
		{"detected", maskerOnly{func(string) string { return "masked" }}, SecretDetected, "Skill content contains a registered secret"},
	} {
		t.Run(row.name, func(t *testing.T) {
			root := t.TempDir()
			resolver := publicationResolver(t, root, row.masker)
			_, err := resolver.PublishSkill(context.Background(), "alice", publicationFields())
			var failure *PublicationError
			if !errors.As(err, &failure) || failure.Code() != row.code || failure.Error() != row.message {
				t.Fatal(err)
			}
			files, err := os.ReadDir(root)
			if err != nil || len(files) != 0 {
				t.Fatal("screening failure wrote owner files", files, err)
			}
		})
	}
	for _, masker := range []memory.Masker{nil, secrets.Null{}, namedMasker{maskerOnly{identity}, none}, healthyMasker{namedMasker{maskerOnly{identity}, registered}, none, none}} {
		resolver := publicationResolver(t, t.TempDir(), masker)
		if _, err := resolver.PublishSkill(context.Background(), "alice", publicationFields()); err != nil {
			t.Fatal("healthy screening refused", err)
		}
	}
}

func TestPublicationCommitPointDoesNotReturnLateCancellation(t *testing.T) {
	for _, afterLink := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-link", true: "after-link"}[afterLink], func(t *testing.T) {
			resolver := publicationResolver(t, t.TempDir(), nil)
			before, err := resolver.ForOwner(context.Background(), "alice")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			publication, err := resolver.publishSkill(ctx, "alice", publicationFields(), func(ctx context.Context, path, text string) (durable.FileIdentity, error) {
				if !afterLink {
					cancel()
				}
				identity, err := durable.CreateText(ctx, path, text)
				if afterLink {
					cancel()
				}
				return identity, err
			})
			future, readErr := resolver.ForOwner(context.Background(), "alice")
			if readErr != nil {
				t.Fatal(readErr)
			}
			target := filepath.Join(before.Directories().Skills(), "review", "SKILL.md")
			_, fileErr := os.Stat(target)
			if afterLink {
				if err != nil || fileErr != nil || future != publication.Resources() || future == before || future.Memory() != before.Memory() {
					t.Fatal("committed success was revoked", err, fileErr)
				}
			} else {
				if !errors.Is(err, context.Canceled) || !os.IsNotExist(fileErr) || future != before {
					t.Fatal("cancelled preparation published", err, fileErr)
				}
			}
		})
	}
}

func TestPublicationCreateFaultsPreserveCacheAndHideHostErrors(t *testing.T) {
	for _, row := range []struct {
		name  string
		cause error
		code  PublicationCode
	}{
		{"permission", os.ErrPermission, PublishFailed},
		{"cancelled", context.Canceled, ""},
		{"deadline", context.DeadlineExceeded, ""},
	} {
		t.Run(row.name, func(t *testing.T) {
			resolver := publicationResolver(t, t.TempDir(), nil)
			before, err := resolver.ForOwner(context.Background(), "alice")
			if err != nil {
				t.Fatal(err)
			}
			_, err = resolver.publishSkill(context.Background(), "alice", publicationFields(), func(context.Context, string, string) (durable.FileIdentity, error) {
				return durable.FileIdentity{}, &os.PathError{Op: "create", Path: "PRIVATE_HOST_PATH", Err: row.cause}
			})
			if row.code == "" {
				if err != row.cause {
					t.Fatal("wrapped cancellation escaped", err)
				}
			} else {
				var failure *PublicationError
				if !errors.As(err, &failure) || failure.Code() != row.code {
					t.Fatal(err)
				}
				encoded, marshalErr := json.Marshal(failure.Receipt())
				if marshalErr != nil || strings.Contains(string(encoded), "PRIVATE_HOST_PATH") {
					t.Fatal("host error escaped", string(encoded), marshalErr)
				}
			}
			future, readErr := resolver.ForOwner(context.Background(), "alice")
			if readErr != nil || future != before {
				t.Fatal("failed create changed cache", readErr)
			}
		})
	}
}

func TestPublishedSnapshotRetainsRestartIdentityAndTamperRefusal(t *testing.T) {
	root := t.TempDir()
	resolver := publicationResolver(t, root, nil)
	publication, err := resolver.PublishSkill(context.Background(), "alice", publicationFields())
	if err != nil {
		t.Fatal(err)
	}
	refreshed := publicationResolver(t, root, nil)
	restarted, err := refreshed.ForOwner(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	input := protocol.LoadSkillInput{Name: "user:review"}
	first, err := publication.Resources().Skills().Load(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := restarted.Skills().Load(context.Background(), input)
	if err != nil || first != second || publication.Resources().Skills().Descriptions() != restarted.Skills().Descriptions() {
		t.Fatal("restart differs", err)
	}
	publicationWrite(t, filepath.Join(restarted.Directories().Skills(), "review", "SKILL.md"), "changed")
	if _, err := publication.Resources().Skills().Load(context.Background(), input); err == nil {
		t.Fatal("prepared published entry skipped source verification")
	}
}
