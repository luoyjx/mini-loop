package userresources

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
)

type captureNamedMasker struct{}

func (captureNamedMasker) Names() []secrets.Name       { return []secrets.Name{"registered"} }
func (captureNamedMasker) MaskText(text string) string { return text }

type captureBrokenReports struct{ captureNamedMasker }

func (captureBrokenReports) Unresolved() []secrets.Name  { panic("private screening failure") }
func (captureBrokenReports) ShortValues() []secrets.Name { return nil }

func TestSkillCaptureMatchesActualPython(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-skill-capture.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Flavor string
			Minimum      int
			Values       []string
			Steps        []struct {
				User, Final   string
				Repeat, Turns int
				Recover       bool
				History       []struct {
					Role    protocol.Role
					Content protocol.Content
				}
			}
			Results []struct {
				SHA256           string
				ProjectionSHA256 string `json:"projection_sha256"`
				Count, Omitted   int
				Error            CaptureError
			}
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || len(fixture.Cases) != 20 {
		t.Fatal("incomplete fixture", err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			registry := secrets.New(secrets.Config{MinLength: &row.Minimum})
			for i, value := range row.Values {
				registry.RegisterValue(secrets.Name(string(rune('0'+i))), value)
			}
			var masker memory.Masker = registry
			switch row.Flavor {
			case "unresolved":
				registry.RegisterLookup("missing", func() (string, error) { return "", errors.New("private missing") })
			case "missing_reports":
				masker = captureNamedMasker{}
			case "crashing_reports":
				masker = captureBrokenReports{}
			}
			var ledger CaptureLedger
			if len(row.Steps) != len(row.Results) {
				t.Fatal("incomplete steps")
			}
			for i, step := range row.Steps {
				history := make([]protocol.Message, len(step.History))
				for index, message := range step.History {
					history[index] = protocol.Message{Role: message.Role, Content: message.Content}
				}
				if step.Recover {
					registry.RegisterValue("0", "healthy-replacement-secret")
				}
				for turn := 0; turn < max(1, step.Turns); turn++ {
					ledger.Record(strings.Repeat(step.User, max(1, step.Repeat)), strings.Repeat(step.Final, max(1, step.Repeat)), history, masker)
				}
				state, expected := ledger.Snapshot(), row.Results[i]
				if captureHash(t, state) != expected.SHA256 || len(state.Messages) != expected.Count || state.Omitted != expected.Omitted || state.Error != expected.Error {
					t.Fatalf("source state differs at %d: %+v", i, state)
				}
				projected, err := ledger.Project(masker, MaxProjectionChars)
				if expected.Error != "" {
					var fault *DraftError
					if !errors.As(err, &fault) || fault.Code() != DraftCaptureSourceUnavailable || fault.StatusCode() != 503 {
						t.Fatal("capture latch did not refuse projection", err)
					}
				} else if err != nil || captureHash(t, projected) != expected.ProjectionSHA256 {
					t.Fatal("source projection differs", projected, err)
				}
			}
		})
	}
}

func captureHash[T CaptureSnapshot | SkillProjection](t *testing.T, value T) string {
	t.Helper()
	wire, err := protocol.PythonJSON(value, false, true)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(wire))
	return hex.EncodeToString(digest[:])
}

type capturePanicMasker struct{}

func (capturePanicMasker) MaskText(string) string { panic("private capture failure") }

func TestSkillCaptureContainsFaultsAndDetachesConcurrentSnapshots(t *testing.T) {
	var broken CaptureLedger
	broken.Record("human", "answer", nil, capturePanicMasker{})
	if state := broken.Snapshot(); state.Error != CaptureFailed || state.Established {
		t.Fatal(state)
	}
	var invalid CaptureLedger
	invalid.Record(string([]byte{0xff}), "answer", nil, nil)
	if invalid.Snapshot().Error != CaptureFailed {
		t.Fatal("invalid UTF-8 admitted")
	}
	var ledger CaptureLedger
	var wait sync.WaitGroup
	for i := 0; i < 40; i++ {
		wait.Add(1)
		go func() { defer wait.Done(); ledger.Record("human", "answer", nil, nil); _ = ledger.Snapshot() }()
	}
	wait.Wait()
	state := ledger.Snapshot()
	if len(state.Messages) != 64 || state.Omitted != 16 {
		t.Fatal(state)
	}
	state.Messages[0].Content = "changed"
	if ledger.Snapshot().Messages[0].Content == "changed" {
		t.Fatal("snapshot aliases ledger")
	}
}
