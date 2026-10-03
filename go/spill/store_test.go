package spill

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type storedContract struct {
	Ref           Ref
	SHA256        string
	RootMode      uint32 `json:"root_mode"`
	NamespaceMode uint32 `json:"namespace_mode"`
	FileMode      uint32 `json:"file_mode"`
}

func TestActualPythonLocalStore(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-spill.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		MaxBytes int `json:"max_bytes"`
		Token    string
		Stores   []struct {
			Name, Namespace, Suggestion, Pattern string
			Repeat                               int
			Result                               *storedContract
			Error                                *string
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.MaxBytes != MaxBytes || len(fixture.Stores) != 14 {
		t.Fatal("source inventory changed")
	}
	for _, row := range fixture.Stores {
		t.Run(row.Name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "store")
			if err := os.Mkdir(root, 0777); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(root, 0777); err != nil {
				t.Fatal(err)
			}
			store, err := NewLocalStore(root)
			if err != nil {
				t.Fatal(err)
			}
			store.token = func() (string, error) { return fixture.Token, nil }
			content := strings.Repeat(row.Pattern, row.Repeat)
			ref, err := store.SaveText(context.Background(), Request{Namespace: Namespace(row.Namespace), ToolName: "bash", Label: "output", SuggestedName: row.Suggestion, Content: content})
			if row.Error != nil {
				if err == nil {
					t.Fatal("accepted source-rejected store write")
				}
				files, _ := os.ReadDir(store.Root())
				if len(files) != 0 {
					t.Fatal("overflow allocated namespace")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(ref.Locator)
			if err != nil || string(raw) != content {
				t.Fatal("content differs", err)
			}
			sum := sha256.Sum256(raw)
			if hex.EncodeToString(sum[:]) != row.Result.SHA256 {
				t.Fatal("source digest differs")
			}
			normalized := ref
			normalized.Locator = strings.ReplaceAll(ref.Locator, store.Root(), "<store>")
			normalized.RetrievalHint = strings.ReplaceAll(ref.RetrievalHint, store.Root(), "<store>")
			if normalized != row.Result.Ref {
				t.Fatal(normalized, row.Result.Ref)
			}
			for _, mode := range []struct {
				path     string
				expected uint32
			}{{store.Root(), row.Result.RootMode}, {filepath.Dir(ref.Locator), row.Result.NamespaceMode}, {ref.Locator, row.Result.FileMode}} {
				info, err := os.Stat(mode.path)
				if err != nil || uint32(info.Mode().Perm()) != mode.expected {
					t.Fatal(mode, err)
				}
			}
		})
	}
}
func TestCollisionAndPlantedLeafCannotReplaceContent(t *testing.T) {
	store, err := NewLocalStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	store.token = func() (string, error) { return "feedfacedeadbeef", nil }
	request := Request{Namespace: "s1", SuggestedName: "bash.txt", Content: "first"}
	first, err := store.SaveText(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.Content = "second"
	if _, err := store.SaveText(context.Background(), request); !os.IsExist(err) {
		t.Fatal("collision not refused", err)
	}
	raw, err := os.ReadFile(first.Locator)
	if err != nil || string(raw) != "first" {
		t.Fatal(string(raw), err)
	}
	victim := filepath.Join(t.TempDir(), "victim.txt")
	if err := os.WriteFile(victim, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(first.Locator); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, first.Locator); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveText(context.Background(), request); !os.IsExist(err) {
		t.Fatal("symlink not refused", err)
	}
	raw, err = os.ReadFile(victim)
	if err != nil || string(raw) != "original" {
		t.Fatal("victim changed", err)
	}
}
func TestConcurrentSavesHaveDistinctExactArtifacts(t *testing.T) {
	store, err := NewLocalStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	const count = 32
	results := make(chan Ref, count)
	failures := make(chan error, count)
	var workers sync.WaitGroup
	for i := 0; i < count; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			ref, err := store.SaveText(context.Background(), Request{Namespace: "shared", SuggestedName: "../same.txt", Content: "masked🙂"})
			if err != nil {
				failures <- err
				return
			}
			results <- ref
		}()
	}
	workers.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	seen := map[string]bool{}
	for ref := range results {
		if seen[ref.Locator] {
			t.Fatal("collision")
		}
		seen[ref.Locator] = true
		raw, err := os.ReadFile(ref.Locator)
		if err != nil || string(raw) != "masked🙂" || ref.Bytes != len(raw) {
			t.Fatal(ref, err)
		}
	}
	if len(seen) != count {
		t.Fatal(len(seen))
	}
}
func TestInvalidTextAndCancelledSaveHaveNoNamespaceEffects(t *testing.T) {
	store, err := NewLocalStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.SaveText(ctx, Request{Content: "masked"}); err == nil {
		t.Fatal("ignored cancellation")
	}
	if _, err := store.SaveText(context.Background(), Request{Content: string([]byte{0xff})}); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
	children, err := os.ReadDir(store.Root())
	if err != nil || len(children) != 0 {
		t.Fatal(children, err)
	}
}
