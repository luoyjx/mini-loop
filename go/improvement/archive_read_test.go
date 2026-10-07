package improvement

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestActualPythonArchiveReads(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-improvement-archive-read.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name          string          `json:"name"`
			Content       string          `json:"content"`
			Fault         string          `json:"fault"`
			Owner         *ArchiveOwnerID `json:"owner"`
			Limit         *int            `json:"limit"`
			Width         int             `json:"width"`
			IntegerDigits int             `json:"integer_digits"`
			Depth         int             `json:"depth"`
			Records       int             `json:"records"`
			Error         *string         `json:"error"`
			Digests       []string        `json:"row_digests"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, recipe := range fixture.Cases {
		t.Run(recipe.Name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "archive.jsonl")
			content := recipe.Content
			if recipe.Width > 0 {
				content = `{"owner":"alice","body":"` + strings.Repeat("界", recipe.Width) + `"}`
			}
			if recipe.IntegerDigits > 0 {
				content = strings.Repeat("9", recipe.IntegerDigits)
			}
			if recipe.Depth > 0 {
				content = strings.Repeat("[", recipe.Depth) + "0" + strings.Repeat("]", recipe.Depth)
			}
			if recipe.Records > 0 {
				lines := make([]string, recipe.Records)
				for i := range lines {
					lines[i] = strconv.Itoa(i)
				}
				content = strings.Join(lines, "\n")
			}
			switch recipe.Fault {
			case "missing":
			case "archive-directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "invalid-utf8":
				if err := os.WriteFile(path, []byte{0xff, '\n', '1'}, 0600); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.WriteFile(path, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			archive := NewArchive(root, archiveMaskFunc(func(string) string { panic("read must not remask historical data") }))
			rows, err := archive.List(context.Background(), ArchiveQuery{Owner: recipe.Owner, Limit: recipe.Limit})
			if recipe.Error != nil {
				expected := map[string]error{"AttributeError": ErrArchiveOwnerShape, "UnicodeDecodeError": ErrArchiveEncoding, "ValueError": ErrArchiveInteger, "RecursionError": ErrArchiveDepth}[*recipe.Error]
				if expected == nil || !errors.Is(err, expected) || rows != nil {
					t.Fatalf("source %s; got %v, %d rows", *recipe.Error, err, len(rows))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if rows == nil || len(rows) != len(recipe.Digests) {
				t.Fatalf("row count %d != %d", len(rows), len(recipe.Digests))
			}
			for i, row := range rows {
				// Source list retains nonfinite numbers. Standard HTTP serialization must fail;
				// this private fixture projection matches Python json.dumps for comparison.
				encoded, err := appendArchiveValue(nil, row, true)
				if err != nil {
					t.Fatal(err)
				}
				sum := sha256.Sum256(encoded)
				if hex.EncodeToString(sum[:]) != recipe.Digests[i] {
					t.Fatalf("row %d differs from source: %.300s", i, encoded)
				}
			}
		})
	}
}

func TestArchiveReadPreservesDetachedLegacyValues(t *testing.T) {
	root := t.TempDir()
	archive := NewArchive(root, nil)
	input := `{"owner":"alice","future":{"items":["private",null,true,1,2.0]},"proposal_id":"old"}`
	if err := os.WriteFile(root+"/archive.jsonl", []byte(input+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	owner := ArchiveOwnerID("alice")
	rows, err := archive.List(context.Background(), ArchiveQuery{Owner: &owner})
	if err != nil {
		t.Fatal(err)
	}
	value := rows[0]
	if value.Kind() != ArchiveObject {
		t.Fatal("wrong variant")
	}
	keys := value.Keys()
	keys[0] = "changed"
	if value.Keys()[0] != "owner" {
		t.Fatal("key alias")
	}
	future, ok := value.Lookup("future")
	if !ok {
		t.Fatal("unknown field dropped")
	}
	items, _ := future.Lookup("items")
	array, ok := items.Array()
	if !ok || len(array) != 5 {
		t.Fatal("unknown array dropped")
	}
	if text, ok := array[0].Text(); !ok || text != "private" {
		t.Fatal("text")
	}
	if array[1].Kind() != ArchiveNull {
		t.Fatal("null")
	}
	if b, ok := array[2].Bool(); !ok || !b {
		t.Fatal("bool")
	}
	if n, ok := array[3].Integer(); !ok || n != "1" {
		t.Fatal("integer")
	}
	if n, ok := array[4].Float(); !ok || n != 2 {
		t.Fatal("float")
	}
	array[0] = ArchiveValue{}
	again, _ := items.Array()
	if text, _ := again[0].Text(); text != "private" {
		t.Fatal("array alias")
	}
	if _, ok := value.Lookup("absent"); ok {
		t.Fatal("absent member")
	}
	if array, _ := value.Array(); array != nil {
		t.Fatal("object treated as array")
	}
	if keys := items.Keys(); keys != nil {
		t.Fatal("array treated as object")
	}
	body, err := value.MarshalJSON()
	if err != nil || string(body) != input {
		t.Fatalf("legacy JSON: %s %v", body, err)
	}
	rows[0] = ArchiveValue{}
	more, err := archive.List(context.Background(), ArchiveQuery{Owner: &owner})
	if err != nil || more[0].Kind() != ArchiveObject {
		t.Fatal("rows retained mutable state")
	}
	if _, err := archive.Record(ProposalFields{}, ArchiveRecordOptions{Owner: &owner}); err != nil {
		t.Fatal(err)
	}
	more, err = archive.List(context.Background(), ArchiveQuery{Owner: &owner})
	if err != nil || len(more) != 2 {
		t.Fatalf("read after append: %d %v", len(more), err)
	}
}

func TestArchiveReadFailuresAndNonfiniteValues(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	archive := NewArchive(filepath.Join(t.TempDir(), "missing"), nil)
	if rows, err := archive.List(ctx, ArchiveQuery{}); rows != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	var absent *Archive
	if _, err := absent.List(context.Background(), ArchiveQuery{}); err == nil {
		t.Fatal("nil archive accepted")
	}
	for _, raw := range []string{"NaN", "Infinity", "-Infinity", "1e999"} {
		v, err := decodeArchiveValue(raw)
		if err != nil {
			t.Fatal(err)
		}
		number, ok := v.Float()
		if !ok || (!math.IsNaN(number) && !math.IsInf(number, 0)) {
			t.Fatal("nonfinite lost")
		}
		if _, err := v.MarshalJSON(); !errors.Is(err, ErrArchiveNonfinite) {
			t.Fatalf("nonfinite emitted: %v", err)
		}
	}
	// The parser skips only malformed JSON; resource/conversion errors propagate.
	for _, raw := range []string{"", "{", "[1,]", "[1", "{\"a\":1,}", "{\"a\" 1}", "true false", "01", "-", "1.", "1e+", "\"\u0001\"", "\"\\x\"", "\"\\u12xy\"", "\"\\u12\"", "\"\\", "\"unclosed", "[1 2]"} {
		if _, err := decodeArchiveValue(raw); !errors.Is(err, errArchiveSyntax) {
			t.Fatalf("invalid %q: %v", raw, err)
		}
	}
	v, err := decodeArchiveValue(`{"a":1,"b":2,"a":3}`)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v.Keys(), []string{"a", "b"}) {
		t.Fatal("collision changed source order")
	}
	n, _ := v.Lookup("a")
	if value, _ := n.Integer(); value != "3" {
		t.Fatal("last key lost")
	}
}
