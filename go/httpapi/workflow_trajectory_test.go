package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/trajectory"
)

func TestWorkflowTrajectoryScalarHTTPMatchesSourceRoutes(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-workflow-trajectory.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			Name     string
			Capture  bool
			Records  []string
			Document string `json:"document_wire"`
			HTTP     []struct {
				Suffix, Token string
				Status        int
			}
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Rows) != 10 {
		t.Fatal(err)
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name+map[bool]string{true: "/capture", false: "/private"}[row.Capture], func(t *testing.T) {
			root := t.TempDir()
			store, err := trajectory.New(trajectory.Config{Root: filepath.Join(root, "traces"), CaptureContent: true})
			if err != nil {
				t.Fatal(err)
			}
			id := "traj_" + strings.Repeat("c", 24)
			lines := []string{}
			for _, raw := range row.Records {
				value, err := jsonvalue.Decode(raw)
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := value.MarshalLegacyUTF8()
				if err != nil {
					t.Fatal(err)
				}
				lines = append(lines, string(encoded))
			}
			if err := os.WriteFile(filepath.Join(store.Root(), id+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			manager := recordingManager(t, filepath.Join(root, "ws"), store)
			server := testServer(t, Config{Manager: manager, Auth: tokenAuth(t)})
			for _, route := range row.HTTP {
				response := request(server, "GET", "/trajectories/"+id+route.Suffix, "", route.Token)
				if response.Code != route.Status {
					t.Fatalf("%s: %d expected %d: %s", route.Suffix, response.Code, route.Status, response.Body.String())
				}
				if response.Code == 200 && route.Suffix != "/export?format=jsonl" {
					actual, err := jsonvalue.Decode(response.Body.String())
					if err != nil {
						t.Fatal(err)
					}
					want, err := jsonvalue.Decode(row.Document)
					if err != nil {
						t.Fatal(err)
					}
					a, _ := jsonvalue.AppendLegacyDefault(actual.Sorted())
					b, _ := jsonvalue.AppendLegacyDefault(want.Sorted())
					if string(a) != string(b) {
						t.Fatalf("HTTP document differs: %s; %s", a, b)
					}
				}
			}
		})
	}
}
