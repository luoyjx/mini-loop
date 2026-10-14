package agent

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type nullBoundaryRow struct {
	Layer                string          `json:"layer"`
	Construction         string          `json:"construction"`
	SecretClass          string          `json:"secret_class"`
	StateClass           *string         `json:"state_class"`
	MaskText             string          `json:"mask_text"`
	MaskPayload          jsonvalue.Value `json:"mask_payload"`
	Output               string          `json:"output"`
	ModelCalls           uint64          `json:"model_calls"`
	LiveMessages         int             `json:"live_messages"`
	SessionStatus        *SessionStatus  `json:"session_status"`
	Busy                 *bool           `json:"busy"`
	RunCount             *int            `json:"run_count"`
	StoredSessions       *int            `json:"stored_sessions"`
	StoredMessages       *int            `json:"stored_messages"`
	StoredEvents         *int            `json:"stored_events"`
	PersistError         *string         `json:"persist_error"`
	SQLiteFilesAfterStop []string        `json:"sqlite_files_after_stop"`
	SQLiteConnectCalls   int             `json:"sqlite_connect_calls"`
}

type nullBoundaryFixture struct {
	Rows         []nullBoundaryRow
	Sample       string
	Payload      jsonvalue.Value
	SourceSHA256 map[string]string `json:"source_sha256"`
	Scope        []string
}

type nullBoundaryResponder struct{ text string }

func (responder nullBoundaryResponder) Respond(context.Context, protocol.ModelRequest) (FakeGeneration, error) {
	return FakeGeneration{Content: []protocol.Block{protocol.NewTextBlock(responder.text)}, StopReason: protocol.StopEndTurn}, nil
}

func nullBoundarySQLiteFiles(t *testing.T, root string) []string {
	t.Helper()
	files := []string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		var header [16]byte
		_, readErr := io.ReadFull(file, header[:])
		closeErr := file.Close()
		if readErr == nil && string(header[:]) == "SQLite format 3\x00" {
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(relative))
		}
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			return readErr
		}
		return closeErr
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// Source owns concrete Null objects. The native equivalent is an absent typed
// projection/backend dependency, verified through actual constructors and runs.
// This does not characterize explicit registries, SQLite, or sandbox defaults.
func TestDefaultNullBoundariesMatchActualPython(t *testing.T) {
	file, err := os.Open("../testdata/python-null-boundaries.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var fixture nullBoundaryFixture
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Rows) != 6 {
		t.Fatalf("Source inventory %d; want six constructor profiles", len(fixture.Rows))
	}
	seen := make(map[string]bool)
	for _, row := range fixture.Rows {
		name := row.Layer + "/" + row.Construction
		if seen[name] {
			t.Fatalf("duplicate Source row %s", name)
		}
		seen[name] = true
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			thinking := false
			provider := NewFakeProvider(FakeProviderConfig{Responder: nullBoundaryResponder{fixture.Sample}, Thinking: &thinking})
			var err error
			config := RuntimeConfig{ID: "fixture-session", Owner: "fixture-owner", Provider: provider, Workspace: root, Mode: ModeInteractive, MaxRounds: 1, Model: "fixture-model", MaxTokens: 64, Bash: echoExecutor{}}
			if row.Construction == "none" {
				var secrets ApprovalRedactor
				var state StateStore
				config.Secrets, config.StateStore = secrets, state
			} else if row.Construction != "omitted" {
				t.Fatalf("unknown construction %s", row.Construction)
			}
			var core *Session
			var session *ManagedSession
			var manager *SessionManager
			switch row.Layer {
			case "agent":
				core, err = NewRuntimeSession(config)
			case "session":
				session, err = NewManagedSession(config)
				if session != nil {
					core = session.core
				}
			case "manager":
				services := ManagerServices{Provider: provider}
				if row.Construction == "none" {
					services.Secrets, services.StateStore = config.Secrets, config.StateStore
				}
				manager, err = NewSessionManager(ManagerConfig{WorkspaceRoot: root, Defaults: SessionDefaults{Model: "fixture-model", MaxTokens: 64, MaxRounds: 1}, Services: services})
				if err == nil {
					t.Cleanup(func() {
						if err := manager.Stop(context.Background()); err != nil {
							t.Error(err)
						}
					})
					session, err = manager.Create(context.Background(), CreateSessionRequest{Owner: "fixture-owner"})
					if session != nil {
						core = session.core
					}
				}
			default:
				t.Fatalf("unknown layer %s", row.Layer)
			}
			if err != nil {
				t.Fatal(err)
			}
			if row.SecretClass != "NullSecretRegistry" || core.secrets != nil || core.persistence != nil {
				t.Fatal("default native secret/state is not absent")
			}
			if manager != nil && (manager.RecordingMasker() != nil || manager.config.Services.StateStore != nil) {
				t.Fatal("manager installed an unrequested secret or state backend")
			}
			var output string
			if session == nil {
				output, err = core.Run(context.Background(), "show the fixture text")
			} else {
				output, err = session.Run(context.Background(), "show the fixture text")
			}
			if err != nil {
				t.Fatal(err)
			}
			if output != row.Output || provider.Calls() != row.ModelCalls || len(core.Messages()) != row.LiveMessages {
				t.Errorf("run output/calls/messages = %q/%d/%d; Source %q/%d/%d", output, provider.Calls(), len(core.Messages()), row.Output, row.ModelCalls, row.LiveMessages)
			}
			if maskedText(core.secrets, fixture.Sample) != row.MaskText || !reflect.DeepEqual(fixture.Payload.MapStrings(func(text string) string { return maskedText(core.secrets, text) }).Sorted(), row.MaskPayload.Sorted()) {
				t.Fatal("nil projection changed actual Source Null masking")
			}
			events, err := json.Marshal(core.Events())
			if err != nil || !strings.Contains(string(events), fixture.Sample) {
				t.Fatalf("recorded text did not preserve unregistered fixture secret: %v", err)
			}
			if session != nil {
				if row.StateClass == nil || *row.StateClass != "NullStateStore" || row.StoredSessions == nil || *row.StoredSessions != 0 || row.StoredMessages == nil || *row.StoredMessages != 0 || row.StoredEvents == nil || *row.StoredEvents != 0 {
					t.Fatal("Source null-store observation is incomplete")
				}
				status := session.PersistenceStatus()
				if status.Configured || status.Error != nil || status.LeaseConfirmed || status.Restored || status.RestorePending || row.PersistError != nil {
					t.Fatalf("unrequested persistence became active: %#v", status)
				}
				info := session.Info()
				if row.SessionStatus == nil || row.Busy == nil || row.RunCount == nil || info.Status != *row.SessionStatus || info.Busy != *row.Busy || info.RunCount != *row.RunCount {
					t.Fatalf("managed lifecycle differs: %#v", info)
				}
				if manager != nil {
					err = manager.Stop(context.Background())
				} else {
					session.StopAccepting("fixture stopped")
					_, err = session.Cancel(context.Background(), "fixture stopped")
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			// This tripwire is Source evidence only. Native evidence is the
			// absent backend/persistence object and the actual filesystem scan;
			// this test does not invent a native sqlite connect counter.
			if row.SQLiteConnectCalls != 0 {
				t.Fatal("Source constructor unexpectedly called its SQLite tripwire")
			}
			if core.secrets != nil || core.persistence != nil || (session != nil && session.PersistenceStatus().Configured) {
				t.Fatal("stop installed an unrequested secret/state backend")
			}
			if !reflect.DeepEqual(nullBoundarySQLiteFiles(t, root), row.SQLiteFilesAfterStop) {
				t.Fatal("native post-stop SQLite file scan differs from Source")
			}
		})
	}
}
