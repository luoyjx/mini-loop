package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/trajectory"
)

func TestActualPythonMetricHTTPViewsMatchSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-trace-metrics.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			Name, Raw string
			HTTP      []struct {
				Suffix, Token string
				Status        int
				BodyHash      *string `json:"body_sha256"`
			}
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Rows) != 71 {
		t.Fatal(err)
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			root := t.TempDir()
			store, err := trajectory.New(trajectory.Config{Root: filepath.Join(root, "traces"), CaptureContent: true})
			if err != nil {
				t.Fatal(err)
			}
			id := "traj_ffffffffffffffffffffffff"
			if err := os.WriteFile(filepath.Join(store.Root(), id+".jsonl"), []byte(row.Raw), 0600); err != nil {
				t.Fatal(err)
			}
			manager := recordingManager(t, filepath.Join(root, "ws"), store)
			server := testServer(t, Config{Manager: manager, Auth: tokenAuth(t)})
			now := time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)
			server.now = func() time.Time { return now }
			for _, route := range row.HTTP {
				response := request(server, "GET", "/trajectories/"+id+route.Suffix, "", route.Token)
				if response.Code != route.Status {
					t.Fatalf("%s: %d; expected %d: %s", route.Suffix, response.Code, route.Status, response.Body.String())
				}
				if route.Suffix == "/view" && response.Code == 200 {
					b := sha256.Sum256(response.Body.Bytes())
					if route.BodyHash == nil || *route.BodyHash != hex.EncodeToString(b[:]) {
						t.Fatal("complete owned Source HTML differs")
					}
				}
			}
		})
	}
}
