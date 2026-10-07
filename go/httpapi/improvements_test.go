package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/secrets"
)

func TestActualPythonImprovementLineageHTTP(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-improvement-http-read.json")
	if err != nil {
		t.Fatal(err)
	}
	// RawMessage belongs only to the transient fixture boundary, not service state.
	var fixture struct {
		Scenarios []struct {
			Mode          string `json:"mode"`
			StartupExists bool   `json:"startup_archive_exists"`
			Cases         []struct {
				Name        string          `json:"name"`
				Content     string          `json:"content"`
				Fault       string          `json:"fault"`
				Token       *string         `json:"token"`
				Path        *string         `json:"path"`
				Method      *string         `json:"method"`
				Body        string          `json:"body"`
				Records     int             `json:"records"`
				Status      int             `json:"status"`
				ContentType string          `json:"content_type"`
				Challenge   *string         `json:"challenge"`
				Allow       *string         `json:"allow"`
				Response    json.RawMessage `json:"response"`
				Digest      string          `json:"response_digest"`
				Rows        int             `json:"response_rows"`
			} `json:"cases"`
		} `json:"scenarios"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range fixture.Scenarios {
		t.Run(scenario.Mode, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, ".improvements", "archive.jsonl")
			masker := secrets.New(secrets.Config{})
			masker.RegisterValue("ARCHIVE_TOKEN", "archive-secret-123")
			provider := &countedProvider{}
			manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: root, Services: agent.ManagerServices{Provider: provider, Secrets: masker}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := manager.Stop(context.Background()); err != nil {
					t.Error(err)
				}
			})
			_, err = os.Stat(filepath.Dir(path))
			exists := err == nil
			if exists != scenario.StartupExists {
				t.Fatal("constructor created archive root")
			}
			config := Config{Manager: manager, RateLimitPerMinute: 1}
			if scenario.Mode == "authenticated" {
				config.Auth = tokenAuth(t)
			}
			server := testServer(t, config)
			for _, row := range scenario.Cases {
				t.Run(row.Name, func(t *testing.T) {
					if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
						t.Fatal(err)
					}
					if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
						t.Fatal(err)
					}
					content := row.Content
					if row.Records > 0 {
						records := make([]string, row.Records)
						for i := range records {
							records[i] = `{"owner":"alice","index":` + strconv.Itoa(i) + `}`
						}
						content = strings.Join(records, "\n")
					}
					switch row.Fault {
					case "missing":
					case "directory":
						if err := os.Mkdir(path, 0700); err != nil {
							t.Fatal(err)
						}
					case "invalid-utf8":
						if err := os.WriteFile(path, []byte{0xff}, 0600); err != nil {
							t.Fatal(err)
						}
					default:
						if err := os.WriteFile(path, []byte(content), 0600); err != nil {
							t.Fatal(err)
						}
					}
					token := "token-a"
					if row.Token != nil {
						token = *row.Token
					}
					route := "/improvements"
					if row.Path != nil {
						route = *row.Path
					}
					method := "GET"
					if row.Method != nil {
						method = *row.Method
					}
					w := request(server, method, route, row.Body, token)
					challenge, allow := "", ""
					if row.Challenge != nil {
						challenge = *row.Challenge
					}
					if row.Allow != nil {
						allow = *row.Allow
					}
					if w.Code != row.Status || w.Header().Get("Content-Type") != row.ContentType || w.Header().Get("WWW-Authenticate") != challenge || w.Header().Get("Allow") != allow {
						t.Fatalf("source status/headers differ: %d %v %s", w.Code, w.Header(), w.Body.String())
					}
					if row.ContentType != "application/json" {
						var expected string
						if err := json.Unmarshal(row.Response, &expected); err != nil {
							t.Fatal(err)
						}
						if w.Body.String() != expected {
							t.Fatalf("private error differs: %q", w.Body.String())
						}
						return
					}
					actual := improvementFixtureJSON(t, w.Body.Bytes())
					if row.Digest != "" {
						canonical, err := json.Marshal(actual)
						if err != nil {
							t.Fatal(err)
						}
						hash := sha256.Sum256(canonical)
						if hex.EncodeToString(hash[:]) != row.Digest {
							t.Fatal("complete response digest differs")
						}
						return
					}
					expected := improvementFixtureJSON(t, row.Response)
					if !reflect.DeepEqual(actual, expected) {
						t.Fatalf("source response differs: %.500s != %.500s", w.Body.Bytes(), row.Response)
					}
				})
			}
			if provider.calls.Load() != 0 || manager.Summary().Sessions != 0 {
				t.Fatal("lineage GET launched model/session work")
			}
		})
	}
}

// This generic value is confined to a test wire comparison. UseNumber retains
// integers beyond double precision; no such object enters the running service.
func improvementFixtureJSON(t *testing.T, raw []byte) interface{} {
	t.Helper()
	var value interface{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestImprovementHTTPAdmissionAndNoRateSpending(t *testing.T) {
	manager := testManager(t, doneProvider{})
	server := testServer(t, Config{Manager: manager, Auth: tokenAuth(t), RateLimitPerMinute: 1})
	id := create(t, server, "token-a").ID
	for i := 0; i < 3; i++ {
		if w := request(server, "GET", "/improvements", "{broken", "token-a"); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	reader := &countReader{Data: strings.NewReader("not read")}
	req := httptest.NewRequest("GET", "/improvements", reader)
	req.ContentLength = MaxRequestBytes + 1
	w := httptest.NewRecorder()
	server.ServeHTTP(w, req)
	if w.Code != 413 || reader.Count != 0 {
		t.Fatalf("body admission: %d / %d", w.Code, reader.Count)
	}
	route := "/sessions/" + string(id) + "/messages"
	if w := request(server, "POST", route, `{"message":"first"}`, "token-a"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(server, "GET", "/improvements?owner=bob&limit=1", "", "token-a"); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := request(server, "POST", route, `{"message":"second"}`, "token-a"); w.Code != 429 {
		t.Fatal(w.Code, w.Body.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.ListImprovements(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	var absent *agent.SessionManager
	if _, err := absent.ListImprovements(context.Background(), nil); err == nil {
		t.Fatal("nil manager accepted")
	}
}
