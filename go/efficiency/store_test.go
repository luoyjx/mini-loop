package efficiency

import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type storeSource struct {
	Constructors []struct {
		Name   string
		Input  RawStoreConfig
		Output *struct{ Closed bool }
		Error  *sourceFault
	}
	Recipes []struct {
		Name   string
		Config RawStoreConfig
		Steps  []struct {
			Op, Value string
			Output    json.RawMessage
			Error     *sourceFault
			Closed    bool
		}
	}
}
type fixtureEntropy struct {
	counter  byte
	constant bool
}

func (e *fixtureEntropy) Read(p []byte) (int, error) {
	e.counter++
	for i := range p {
		if e.constant {
			p[i] = 1
		} else {
			p[i] = e.counter
		}
	}
	return len(p), nil
}
func sourceStores(t *testing.T) storeSource {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-efficiency-store.json")
	if err != nil {
		t.Fatal(err)
	}
	var source storeSource
	if err = json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	if len(source.Constructors) != 12 || len(source.Recipes) != 15 {
		t.Fatal("Source inventory changed")
	}
	return source
}
func TestRawStoreConstructorsMatchSource(t *testing.T) {
	for _, row := range sourceStores(t).Constructors {
		t.Run(row.Name, func(t *testing.T) {
			row.Input.Workspace = t.TempDir()
			row.Input.Clock = func() (float64, error) { return 0, nil }
			store, err := NewRawStore(row.Input)
			if row.Error != nil {
				if err == nil || err.Error() != row.Error.Message {
					t.Fatal(err, row.Error)
				}
				return
			}
			if err != nil || store.Closed() != row.Output.Closed {
				t.Fatal(store, err)
			}
		})
	}
}
func TestRawStoreOperationsMatchSource(t *testing.T) {
	for _, row := range sourceStores(t).Recipes {
		t.Run(row.Name, func(t *testing.T) {
			config := row.Config
			config.Workspace = t.TempDir()
			now := 0.0
			mode := "normal"
			config.Clock = func() (float64, error) {
				switch mode {
				case "fault":
					return 0, errors.New("offline")
				case "nan":
					return math.NaN(), nil
				case "inf":
					return math.Inf(1), nil
				}
				return now, nil
			}
			store, err := NewRawStore(config)
			if err != nil {
				t.Fatal(err)
			}
			other, err := NewRawStore(config)
			if err != nil {
				t.Fatal(err)
			}
			entropy := &fixtureEntropy{}
			store.entropy = entropy
			refs := []string{}
			for i, step := range row.Steps {
				var err error
				var pointer ArtifactPointer
				var text string
				var count int
				var flag bool
				ref := func() string {
					n, e := strconv.Atoi(step.Value)
					if e != nil {
						t.Fatal(e)
					}
					return refs[n]
				}
				switch step.Op {
				case "clock":
					mode = step.Value
					if mode != "fault" && mode != "nan" && mode != "inf" {
						mode = "normal"
						now, err = strconv.ParseFloat(step.Value, 64)
					}
				case "entropy":
					entropy.constant = true
				case "put":
					pointer, err = store.PutMasked(step.Value)
					if err == nil {
						snap, e := pointer.Snapshot()
						if e != nil {
							t.Fatal(e)
						}
						refs = append(refs, snap.Ref)
					}
				case "get":
					text, err = store.GetMasked(ref())
				case "get-other":
					text, err = other.GetMasked(ref())
				case "get-invalid":
					text, err = store.GetMasked(step.Value)
				case "get-missing":
					text, err = store.GetMasked("raw_" + strings.Repeat("x", 43))
				case "discard":
					flag, err = store.Discard(ref())
				case "discard-invalid":
					flag, err = store.Discard(step.Value)
				case "clear":
					count = store.Clear()
				case "close":
					count = store.Close()
				case "sweep":
					count, err = store.Sweep()
				case "corrupt":
					r := store.records[refs[0]]
					switch step.Value {
					case "payload":
						r.payload = "wxyz"
					case "size":
						r.size = big.NewInt(5)
					case "oversize":
						r.payload = strings.Repeat("x", 9)
					default:
						t.Fatal(step.Value)
					}
					store.records[refs[0]] = r
				default:
					t.Fatal(step.Op)
				}
				if step.Error != nil {
					var typed *ArtifactError
					if !errors.As(err, &typed) || typed.Error() != step.Error.Message || string(typed.Kind) != step.Error.Kind {
						t.Fatalf("step %d %s: %v != %+v", i, step.Op, err, step.Error)
					}
				} else {
					if err != nil {
						t.Fatalf("step %d: %v", i, err)
					}
					switch step.Op {
					case "put":
						var expected ArtifactPointerConfig
						if e := json.Unmarshal(step.Output, &expected); e != nil {
							t.Fatal(e)
						}
						actual, e := pointer.Snapshot()
						if e != nil || !reflect.DeepEqual(actual, expected) {
							t.Fatal(actual, expected, e)
						}
					case "get", "get-other", "get-invalid", "get-missing":
						var expected string
						if e := json.Unmarshal(step.Output, &expected); e != nil || text != expected {
							t.Fatal(text, expected, e)
						}
					case "discard", "discard-invalid":
						var expected bool
						if e := json.Unmarshal(step.Output, &expected); e != nil || flag != expected {
							t.Fatal(flag, expected, e)
						}
					case "sweep", "clear", "close":
						var expected int
						if e := json.Unmarshal(step.Output, &expected); e != nil || count != expected {
							t.Fatal(count, expected, e)
						}
					}
				}
				if store.Closed() != step.Closed {
					t.Fatalf("step %d closed differs", i)
				}
			}
			entries, e := os.ReadDir(config.Workspace)
			if e != nil || len(entries) != 0 {
				t.Fatal("store wrote files", entries, e)
			}
		})
	}
}
func TestRawStoreOwnershipEntropyAndConcurrency(t *testing.T) {
	config := DefaultRawStoreConfig(t.TempDir())
	config.MaxArtifactBytes = big.NewInt(8)
	config.MaxTotalBytes = big.NewInt(128)
	store, err := NewRawStore(config)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxArtifactBytes.SetInt64(0)
	config.MaxTotalBytes.SetInt64(0)
	var group sync.WaitGroup
	refs := make(chan string, 128)
	for i := 0; i < 128; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			p, e := store.PutMasked("x")
			if e != nil {
				t.Error(e)
				return
			}
			snap, e := p.Snapshot()
			if e != nil {
				t.Error(e)
				return
			}
			refs <- snap.Ref
		}()
	}
	group.Wait()
	close(refs)
	unique := make(map[string]bool)
	for ref := range refs {
		if unique[ref] || !rawReference.MatchString(ref) {
			t.Fatal("nonunique/invalid cryptographic reference", ref)
		}
		unique[ref] = true
		group.Add(1)
		go func(ref string) {
			defer group.Done()
			text, e := store.GetMasked(ref)
			if e != nil || text != "x" {
				t.Error(text, e)
			}
			removed, e := store.Discard(ref)
			if e != nil || !removed {
				t.Error(removed, e)
			}
		}(ref)
	}
	group.Wait()
	if len(unique) != 128 || store.Close() != 0 || !store.Closed() {
		t.Fatal("ownership/lifecycle failed")
	}
}
func TestRawStoreMetadataAndFailureBoundaries(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(workspace, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	c := DefaultRawStoreConfig(workspace)
	c.Directory = "linked/raw"
	store, err := NewRawStore(c)
	if err != nil {
		t.Fatal(err)
	}
	p, err := store.PutMasked("masked")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := p.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	snapshot.SizeBytes.SetInt64(999)
	again, _ := p.Snapshot()
	if again.SizeBytes.Int64() != 6 {
		t.Fatal("pointer aliased snapshot")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("metadata path used as persistence", entries, err)
	}
	if _, err = store.PutMasked(string([]byte{0xed, 0xa0, 0x80})); err == nil {
		t.Fatal("surrogate accepted")
	}
	store.clock = func() (float64, error) { return math.MaxFloat64, nil }
	store.ttl = math.MaxFloat64
	if _, err = store.PutMasked("x"); err == nil || err.Error() != "artifact pointer expiry must be finite" {
		t.Fatal(err)
	}
	if len(store.records) != 0 {
		t.Fatal("overflow published record")
	}
	c.Workspace = filepath.Join(workspace, "missing")
	if _, err = NewRawStore(c); err == nil {
		t.Fatal("missing workspace accepted")
	}
	c.Workspace = workspace
	c.Clock = nil
	if _, err = NewRawStore(c); err == nil || err.Error() != "clock must be callable" {
		t.Fatal(err)
	}
	if !errors.Is(artifactError(ArtifactExpired, "expired"), ErrArtifactNotFound) || !errors.Is(artifactError(ArtifactTooLarge, "size"), ErrArtifactStore) {
		t.Fatal("Source exception hierarchy lost")
	}
	if _, err = json.Marshal(ArtifactPointer{}); err == nil {
		t.Fatal("zero pointer marshaled")
	}
}
