package userresources

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
)

func TestSkillCandidatesMatchActualPython(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-skill-candidate.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		IntegerDigits int `json:"integer_digit_limit"`
		Cases         []struct {
			Name, Raw     string
			RequestedName string `json:"requested_name"`
			MessageCount  int    `json:"message_count"`
			Minimum       int
			Values        []string
			RepeatText    string `json:"repeat_text"`
			RepeatCount   int    `json:"repeat_count"`
			Expected      struct {
				Error    CandidateCode
				Decision CandidateDecision
				SHA256   string
			}
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || fixture.IntegerDigits != candidateIntegerDigits || len(fixture.Cases) != 63 {
		t.Fatal("incomplete source corpus", err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			wire := row.Raw
			if row.RepeatCount != 0 {
				wire = strings.ReplaceAll(wire, "__REPEAT__", strings.Repeat(row.RepeatText, row.RepeatCount))
			}
			registry := secrets.New(secrets.Config{MinLength: &row.Minimum})
			for i, value := range row.Values {
				registry.RegisterValue(secrets.Name(string(rune('0'+i))), value)
			}
			candidate, err := ParseSkillCandidate(wire, row.RequestedName, row.MessageCount, registry)
			if row.Expected.Error != "" {
				var failure *CandidateError
				if !errors.As(err, &failure) || failure.Code() != row.Expected.Error {
					t.Fatal("source rejection differs", err, row.Expected.Error)
				}
				if candidate.Decision() != "" {
					t.Fatal("failed candidate retained")
				}
				return
			}
			if err != nil || candidate.Decision() != row.Expected.Decision {
				t.Fatal("source decision differs", candidate, err)
			}
			projected := struct {
				Body        string            `json:"body"`
				Decision    CandidateDecision `json:"decision"`
				Description string            `json:"description"`
				Evidence    []int             `json:"evidence_indexes"`
			}{candidate.Body(), candidate.Decision(), candidate.Description(), candidate.EvidenceIndexes()}
			encoded, err := protocol.PythonJSON(projected, false, true)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256([]byte(encoded))
			if hex.EncodeToString(digest[:]) != row.Expected.SHA256 {
				t.Fatal("source fields differ", row.Name)
			}
			indexes := candidate.EvidenceIndexes()
			if len(indexes) > 0 {
				original := indexes[0]
				indexes[0] = -999
				if candidate.EvidenceIndexes()[0] != original {
					t.Fatal("candidate evidence aliases output")
				}
			}
		})
	}
}

func TestSkillCandidateNativeBoundaryRefusesLossyUnicodeAndMaskingPanic(t *testing.T) {
	const base = `{"schema":"mini-loop.personal-skill-draft/v1","decision":"create","description":"recipe","body":"body","evidence_indexes":[0]}`
	for _, body := range []string{`\ud800`, `\udc00`, string([]byte{0xff})} {
		_, err := ParseSkillCandidate(strings.Replace(base, `"body":"body"`, `"body":"`+body+`"`, 1), "recipe", 1, nil)
		var failure *CandidateError
		if !errors.As(err, &failure) || failure.Code() != CandidateMalformedJSON {
			t.Fatal("lossy JSON accepted", err)
		}
	}
	_, err := ParseSkillCandidate(base, "recipe", 1, capturePanicMasker{})
	var failure *CandidateError
	if !errors.As(err, &failure) || failure.Code() != CandidateMaskingUnavailable || strings.Contains(err.Error(), "private") {
		t.Fatal("masking panic not contained", err)
	}
}
