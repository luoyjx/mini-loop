package sqlitestate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type migrationCase struct {
	Name     string              `json:"name"`
	SeedSQL  string              `json:"seed_sql"`
	Error    string              `json:"error"`
	Columns  map[string][]string `json:"columns"`
	Indexes  []string            `json:"indexes"`
	Versions []string            `json:"versions"`
	Saved    []string            `json:"saved"`
}

func rawDatabase(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", (&url.URL{Scheme: "file", Path: path}).String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

func stringRows(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	result := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertMigrationEffects(t *testing.T, db *sql.DB, recipe migrationCase) {
	t.Helper()
	for table, expected := range recipe.Columns {
		// The table names are the fixed Source capture, not runtime input.
		got := stringRows(t, db, "SELECT name FROM pragma_table_info('"+table+"')")
		if !reflect.DeepEqual(got, expected) {
			t.Errorf("%s columns: got %v, want %v", table, got, expected)
		}
	}
	indexes := stringRows(t, db, "SELECT name FROM sqlite_master WHERE type='index' AND name NOT LIKE 'sqlite_autoindex%' ORDER BY name")
	if !reflect.DeepEqual(indexes, recipe.Indexes) {
		t.Errorf("indexes: got %v, want %v", indexes, recipe.Indexes)
	}
	rows, err := db.Query("SELECT quote(version) FROM schema_version")
	if err != nil {
		t.Fatal(err)
	}
	versions := []string{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		versions = append(versions, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(versions, recipe.Versions) {
		t.Errorf("versions: got %v, want %v", versions, recipe.Versions)
	}
	saved := []string{}
	for _, column := range recipe.Columns["sessions"] {
		values := stringRows(t, db, "SELECT quote(\""+column+"\") FROM sessions WHERE session_id='saved'")
		saved = append(saved, values...)
	}
	if !reflect.DeepEqual(saved, recipe.Saved) {
		t.Errorf("saved row: got %v, want %v", saved, recipe.Saved)
	}
}

func TestMigrationEffectsMatchSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-sqlite-migrations.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []migrationCase `json:"rows"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Rows) != 22 {
		t.Fatalf("expected all 22 Source cases, got %d", len(fixture.Rows))
	}
	for _, recipe := range fixture.Rows {
		t.Run(recipe.Name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.db")
			db := rawDatabase(t, path)
			if _, err := db.Exec(recipe.SeedSQL); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			store, err := Open(context.Background(), path)
			gotError := ""
			if err != nil {
				gotError = strings.ReplaceAll(err.Error(), path, "$DB")
			}
			if gotError != recipe.Error {
				t.Fatalf("error: got %q, want %q", gotError, recipe.Error)
			}
			if store != nil {
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
			}
			db = rawDatabase(t, path)
			assertMigrationEffects(t, db, recipe)
			if recipe.Error == "" {
				store, err := Open(context.Background(), path)
				if err != nil {
					t.Fatal(err)
				}
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
				assertMigrationEffects(t, db, recipe)
			}
		})
	}
}

func TestSchemaSnapshotMatchesSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-sqlite-schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if schemaSQL != string(data) {
		t.Fatal("embedded production schema differs from actual Python export")
	}
}

func TestPinnedConnectionConfigurationAndLiteralPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state?mode=ro#fragment.db")
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, item := range []struct{ query, want string }{
		{"PRAGMA journal_mode", "wal"}, {"PRAGMA synchronous", "1"},
		{"PRAGMA foreign_keys", "1"}, {"PRAGMA busy_timeout", "5000"},
	} {
		var got string
		if err := store.conn.QueryRowContext(context.Background(), item.query).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != item.want {
			t.Errorf("%s: got %q, want %q", item.query, got, item.want)
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if store.conn != nil || store.db != nil {
		t.Fatal("close retained connection")
	}
	store, err = Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenFailureKinds(t *testing.T) {
	t.Run("corrupt", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.db")
		if err := os.WriteFile(path, []byte("a foreign file, not SQLite"), 0600); err != nil {
			t.Fatal(err)
		}
		store, err := Open(context.Background(), path)
		var schemaError *SchemaError
		if store != nil || !errors.As(err, &schemaError) || schemaError.Cause == nil {
			t.Fatalf("got %v / %v", store, err)
		}
		if !strings.Contains(err.Error(), "Move it aside to start fresh") {
			t.Fatal(err)
		}
		data, readError := os.ReadFile(path)
		if readError != nil || string(data) != "a foreign file, not SQLite" {
			t.Fatal("failed open replaced the foreign file")
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		store, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
		if store != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v / %v", store, err)
		}
	})
	t.Run("parent-is-file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "parent")
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
		store, err := Open(context.Background(), filepath.Join(path, "state.db"))
		var schemaError *SchemaError
		if store != nil || err == nil || errors.As(err, &schemaError) {
			t.Fatalf("got %v / %v", store, err)
		}
	})
}
