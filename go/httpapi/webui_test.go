package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestActualPythonPublicBrowserShellsAndProtectedData(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-webui.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Assets map[string]struct {
			SHA256 string `json:"sha256"`
			Bytes  int
		}
		UIHash string `json:"ui_sha256"`
		Cases  []struct {
			Method, Path, Token, Body, Allow string
			Status                           int
			ContentType                      string `json:"content_type"`
			BodyHash                         string `json:"body_sha256"`
			Headers                          map[string]string
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	hash := func(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
	for name, asset := range fixture.Assets {
		data, err := browserAssets.ReadFile("uiassets/" + name)
		if err != nil || len(data) != asset.Bytes || hash(data) != asset.SHA256 {
			t.Fatalf("embedded source changed: %s, %v", name, err)
		}
	}
	if hash([]byte(webUIPage)) != fixture.UIHash {
		t.Fatal("assembled UI differs from Python")
	}
	server := testServer(t, Config{Manager: testManager(t, doneProvider{}), Auth: tokenAuth(t)})
	tcp := httptest.NewServer(server)
	defer tcp.Close()
	for _, row := range fixture.Cases {
		t.Run(row.Method+" "+row.Path+" "+row.Token, func(t *testing.T) {
			req, err := http.NewRequest(row.Method, tcp.URL+row.Path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if row.Token != "" {
				req.Header.Set("Authorization", "Bearer "+row.Token)
			}
			response, err := tcp.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != row.Status || response.Header.Get("Content-Type") != row.ContentType || response.Header.Get("Allow") != row.Allow {
				t.Fatalf("status/header mismatch: %d %v", response.StatusCode, response.Header)
			}
			for name, value := range row.Headers {
				if response.Header.Get(name) != value {
					t.Fatalf("%s mismatch", name)
				}
			}
			if row.Status == 200 || row.Method == "HEAD" {
				if hash(body) != row.BodyHash {
					t.Fatal("full source page bytes differ")
				}
			} else {
				compareTrajectoryJSON(t, body, []byte(row.Body))
			}
		})
	}
	for _, page := range []string{consolePage, webUIPage} {
		if strings.Contains(page, "/*CSS*/") || strings.Contains(page, "/*JS*/") {
			t.Fatal("unassembled shell")
		}
	}
}

func TestEmbeddedBrowserScriptInteractionRegressions(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is unavailable; browser interaction regressions not run")
	}
	// Run the real source regression harness against Go's embed inputs, not the
	// Python asset directory. The harness checks state/DOM interactions; it is not
	// a rendering or optional-server-route parity claim.
	root := t.TempDir()
	assetsRoot := filepath.Join(root, "mini_loop", "webui")
	testsRoot := filepath.Join(root, "tests")
	if err = os.MkdirAll(assetsRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(testsRoot, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.html", "app.js"} {
		data, err := browserAssets.ReadFile("uiassets/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(assetsRoot, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	harness, err := os.ReadFile("../../python/tests/webui_dom.test.cjs")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(testsRoot, "webui_dom.test.cjs")
	if err = os.WriteFile(target, harness, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(node, "--test", target).CombinedOutput()
	if err != nil {
		t.Fatalf("embedded JS interactions failed: %v\n%s", err, output)
	}
	t.Log(string(output))
}
