package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/config"
)

func TestSettingsAuditMatchesActualSourceFilesystemProfiles(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-audit-settings.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Root, State        string
			Environment              map[string]string
			FakeLLM                  bool `json:"fake_llm"`
			TrajectoryEnabled        bool `json:"trajectory_enabled"`
			TrajectoryCaptureContent bool `json:"trajectory_capture_content"`
			Findings                 []Finding
			Rendered                 *string
			Error                    *string
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 24 {
		t.Fatal("unexpected Source settings corpus")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			base := t.TempDir()
			root := filepath.Join(base, row.Root)
			if err := os.MkdirAll(filepath.Dir(root), 0700); err != nil {
				t.Fatal(err)
			}
			var err error
			switch row.State {
			case "missing":
			case "writable", "protected":
				err = os.Mkdir(root, 0700)
				if err == nil {
					mode := os.FileMode(0777)
					if row.State == "protected" {
						mode = 0775
					}
					err = os.Chmod(root, mode)
				}
			case "file":
				err = os.WriteFile(root, []byte("fixture"), 0600)
				if err == nil {
					err = os.Chmod(root, 0666)
				}
			case "linked", "broken":
				target := filepath.Join(base, "target")
				if row.State == "linked" {
					err = os.Mkdir(target, 0700)
					if err == nil {
						err = os.Chmod(target, 0777)
					}
				}
				if err == nil {
					err = os.Symlink(target, root)
				}
			case "cycle":
				err = os.Symlink(root, root)
			case "file/child":
				err = os.Remove(filepath.Dir(root))
				if err == nil {
					err = os.WriteFile(filepath.Dir(root), []byte("fixture"), 0600)
				}
			default:
				t.Fatal("unknown fixture filesystem variant", row.State)
			}
			if err != nil {
				t.Fatal(err)
			}
			settings := config.Settings{WorkspaceRoot: root, FakeLLM: row.FakeLLM, TrajectoryEnabled: row.TrajectoryEnabled, TrajectoryCaptureContent: row.TrajectoryCaptureContent}
			findings, err := Settings(settings, row.Environment)
			if row.Error != nil {
				if err == nil || findings != nil {
					t.Fatal("invalid token configuration produced findings", findings, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for i := range findings {
				findings[i].Detail = strings.ReplaceAll(findings[i].Detail, base, "ROOT")
				findings[i].Remedy = strings.ReplaceAll(findings[i].Remedy, base, "ROOT")
			}
			if !reflect.DeepEqual(findings, row.Findings) {
				t.Fatal(findings, row.Findings)
			}
			text, err := Render(findings)
			if err != nil || row.Rendered == nil || text != *row.Rendered {
				t.Fatal(text, row.Rendered, err)
			}
			if row.State == "missing" {
				if _, err := os.Stat(root); !os.IsNotExist(err) {
					t.Fatal("audit created missing workspace", err)
				}
			}
		})
	}
}

func TestSettingsAuditNilEnvironmentObservesProcessAndStatFaultsPropagate(t *testing.T) {
	t.Setenv("HOST", "0.0.0.0")
	t.Setenv("MINILOOP_API_TOKEN", "")
	t.Setenv("MINILOOP_API_TOKENS", "")
	settings := config.Settings{WorkspaceRoot: filepath.Join(t.TempDir(), "missing")}
	findings, err := Settings(settings, nil)
	if err != nil || len(findings) != 1 || findings[0].Check != HostBind {
		t.Fatal(findings, err)
	}
	findings, err = Settings(settings, map[string]string{})
	if err != nil || len(findings) != 1 || findings[0].Check != Authentication {
		t.Fatal(findings, err)
	}
	settings.WorkspaceRoot = "invalid\x00root"
	if findings, err := Settings(settings, map[string]string{}); err == nil || findings != nil {
		t.Fatal("stat failure silently audited", findings, err)
	}
}
