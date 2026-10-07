package teams

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/secrets"
)

func fieldsOf(value Data) []Field {
	fields := []Field{}
	for _, key := range value.Keys() {
		member, _ := value.Lookup(key)
		fields = append(fields, Field{key, member})
	}
	return fields
}
func TestActualPythonMailboxContracts(t *testing.T) {
	var fixture struct {
		Secret string `json:"secret"`
		Cases  []struct {
			Recipe struct {
				Name       string  `json:"name"`
				Memory     bool    `json:"memory"`
				Mask       bool    `json:"mask"`
				Preload    *string `json:"preload"`
				PreloadHex *string `json:"preload_hex"`
				Padding    int     `json:"padding"`
				Injected   int     `json:"injected"`
			} `json:"recipe"`
			Steps []struct {
				Operation struct {
					Action       string       `json:"action"`
					To           MailboxKey   `json:"to"`
					Content      string       `json:"content"`
					Repeat       *int         `json:"repeat"`
					Type         *MessageType `json:"type"`
					MetadataJSON *string      `json:"metadata_json"`
					ExtraJSON    *string      `json:"extra_json"`
				} `json:"operation"`
				Text     *string   `json:"text"`
				Rows     *[]string `json:"rows"`
				Error    *string   `json:"error"`
				Problems []string  `json:"problems"`
				Exists   *bool     `json:"exists"`
				DiskHash *string   `json:"disk_hash"`
			} `json:"steps"`
		} `json:"cases"`
	}
	data, err := os.ReadFile("../testdata/python-team-bus.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, recipe := range fixture.Cases {
		t.Run(recipe.Recipe.Name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "teams")
			config := Config{}
			if !recipe.Recipe.Memory {
				config.Root = &root
			}
			if recipe.Recipe.Mask {
				registry := secrets.New(secrets.Config{})
				registry.RegisterValue("P_API_KEY", fixture.Secret)
				config.Masker = registry
			}
			bus := New(config)
			bus.now = func() float64 { return 1000.0 }
			for i := 0; i < recipe.Recipe.Injected; i++ {
				bus.inboxes["team/bob"] = append(bus.inboxes["team/bob"], Message{Object(Field{"from", Text("s")}, Field{"to", Text("team/bob")}, Field{"content", Text(fmt.Sprintf("m%03d", i))})})
			}
			path := filepath.Join(root, "team", "inboxes", "bob.jsonl")
			if recipe.Recipe.Preload != nil || recipe.Recipe.PreloadHex != nil {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				payload := []byte{}
				if recipe.Recipe.Preload != nil {
					payload = []byte(*recipe.Recipe.Preload)
				}
				if recipe.Recipe.PreloadHex != nil {
					payload, err = hex.DecodeString(*recipe.Recipe.PreloadHex)
					if err != nil {
						t.Fatal(err)
					}
				}
				payload = append([]byte(strings.Repeat("x", recipe.Recipe.Padding)), payload...)
				if err := os.WriteFile(path, payload, 0600); err != nil {
					t.Fatal(err)
				}
			}
			for index, step := range recipe.Steps {
				operation := step.Operation
				key := operation.To
				if key == "" {
					key = "team/bob"
				}
				var rows []Message
				var result SendResult
				var err error
				switch operation.Action {
				case "send":
					repeat := 1
					if operation.Repeat != nil {
						repeat = *operation.Repeat
					}
					metadata, extra := Object(), Object()
					if operation.MetadataJSON != nil {
						metadata, err = DecodeData(*operation.MetadataJSON)
						if err != nil {
							t.Fatal(err)
						}
					}
					if operation.ExtraJSON != nil {
						extra, err = DecodeData(*operation.ExtraJSON)
						if err != nil {
							t.Fatal(err)
						}
					}
					result, err = bus.Send(context.Background(), SendRequest{From: "team/lead", To: key, Content: strings.Repeat(operation.Content, repeat), Type: operation.Type, Metadata: NewMetadata(fieldsOf(metadata)...), Extra: fieldsOf(extra)})
				case "peek":
					rows, err = bus.Peek(context.Background(), key)
				case "read":
					rows, err = bus.Read(context.Background(), key)
				default:
					t.Fatalf("unknown operation %q", operation.Action)
				}
				if step.Error != nil {
					expected := map[string]error{"UnicodeDecodeError": ErrEncoding, "ValueError": jsonvalue.ErrInteger, "RecursionError": jsonvalue.ErrDepth}[*step.Error]
					if expected == nil || !errors.Is(err, expected) {
						t.Fatalf("step %d error %v want %s", index, err, *step.Error)
					}
				} else if err != nil {
					t.Fatalf("step %d: %v", index, err)
				}
				if step.Text != nil && result.Text != *step.Text {
					t.Fatalf("step %d text %q want %q", index, result.Text, *step.Text)
				}
				if step.Rows != nil {
					actual := []string{}
					for _, row := range rows {
						encoded, err := jsonvalue.AppendLegacy(nil, row.Data())
						if err != nil {
							t.Fatal(err)
						}
						actual = append(actual, string(encoded))
					}
					if !reflect.DeepEqual(actual, *step.Rows) {
						t.Fatalf("step %d rows differ: got %v want %v", index, actual, *step.Rows)
					}
				}
				actualProblems := bus.Problems().Messages()
				for i, message := range actualProblems {
					actualProblems[i] = strings.ReplaceAll(message, root, "<root>")
				}
				if !reflect.DeepEqual(actualProblems, step.Problems) {
					t.Fatalf("step %d problems %v want %v", index, actualProblems, step.Problems)
				}
				if step.Exists != nil {
					_, err := os.Stat(path)
					if (err == nil) != *step.Exists {
						t.Fatalf("step %d file existence %v want %v", index, err, *step.Exists)
					}
				}
				if step.DiskHash != nil {
					bytes, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					sum := sha256.Sum256(bytes)
					if fmt.Sprintf("%x", sum) != *step.DiskHash {
						t.Fatalf("step %d persisted JSON differs: %s", index, bytes)
					}
				}
			}
		})
	}
}

func TestMailboxConcurrentPeekDoesNotDrainAndReadConsumesOnce(t *testing.T) {
	root := t.TempDir()
	bus := New(Config{Root: &root})
	var group sync.WaitGroup
	for i := 0; i < 40; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			if _, err := bus.Send(context.Background(), SendRequest{From: "t/a", To: "t/b", Content: fmt.Sprint(i)}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	group.Wait()
	for i := 0; i < 10; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			rows, err := bus.Peek(context.Background(), "t/b")
			if err != nil || len(rows) != 40 {
				t.Errorf("peek %d %v", len(rows), err)
			}
		}()
	}
	group.Wait()
	rows, err := bus.Read(context.Background(), "t/b")
	if err != nil || len(rows) != 40 {
		t.Fatalf("read %d %v", len(rows), err)
	}
	rows, err = bus.Read(context.Background(), "t/b")
	if err != nil || len(rows) != 0 {
		t.Fatalf("second read %d %v", len(rows), err)
	}
}
func TestSparseMailboxReadBoundsMemoryAndReportsLoss(t *testing.T) {
	root := t.TempDir()
	bus := New(Config{Root: &root})
	path, _ := bus.path("t/b")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Seek(1<<30, 0); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if _, err = file.WriteString("\n{\"content\":\"last\"}\n"); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	rows, err := bus.Read(context.Background(), "t/b")
	if err != nil || len(rows) != 2 {
		t.Fatalf("sparse read %d %v", len(rows), err)
	}
	if content, _ := rows[0].Content(); !strings.Contains(content, "unknown number") {
		t.Fatal(content)
	}
	if content, _ := rows[1].Content(); content != "last" {
		t.Fatal(content)
	}
}

type panicMasker struct{}

func (panicMasker) MaskText(string) string { panic("credential") }
func TestMailboxMaskFailureAndCancellationLeaveNoRawMessage(t *testing.T) {
	root := t.TempDir()
	bus := New(Config{Root: &root, Masker: panicMasker{}})
	request := SendRequest{From: "t/a", To: "t/b", Content: "credential"}
	if result, err := bus.Send(context.Background(), request); result.Status != "" || !errors.Is(err, ErrMasking) {
		t.Fatalf("mask failure %+v %v", result, err)
	}
	path, _ := bus.path(request.To)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := bus.Send(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := bus.Read(ctx, request.To); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := bus.Peek(ctx, request.To); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestMessageMetadataAndViewsRemainDetached(t *testing.T) {
	fields := []Field{{"request_id", Text("req")}}
	metadata := NewMetadata(fields...)
	fields[0].Value = Text("poison")
	bus := New(Config{})
	if _, err := bus.Send(context.Background(), SendRequest{From: "a", To: "b", Content: "original", Metadata: metadata}); err != nil {
		t.Fatal(err)
	}
	rows, _ := bus.Peek(context.Background(), "b")
	rows[0] = Message{Object(Field{"content", Text("changed")})}
	rows, _ = bus.Read(context.Background(), "b")
	if content, _ := rows[0].Content(); content != "original" {
		t.Fatal(content)
	}
	data, _ := rows[0].Data().Lookup("metadata")
	value, _ := data.Lookup("request_id")
	if text, _ := value.Text(); text != "req" {
		t.Fatal(text)
	}
	if _, err := bus.Send(context.Background(), SendRequest{To: "b", Extra: []Field{{"content", Text("invalid")}}}); !errors.Is(err, ErrFields) {
		t.Fatal(err)
	}
}
