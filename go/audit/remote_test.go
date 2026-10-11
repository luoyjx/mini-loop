package audit

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestRemoteAuditMatchesActualSourceProfiles(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-audit-posture.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Source string `json:"source"`
		Cases  []struct {
			Name     string    `json:"name"`
			Report   Report    `json:"report"`
			Findings []Finding `json:"findings"`
			Rendered string    `json:"rendered"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 22 {
		t.Fatal("unexpected Source corpus size")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			findings := Remote(row.Report, fixture.Source)
			if !reflect.DeepEqual(findings, row.Findings) {
				t.Fatalf("findings: %#v; want %#v", findings, row.Findings)
			}
			text, err := Render(findings)
			if err != nil || text != row.Rendered {
				t.Fatal(text, err)
			}
		})
	}
}

func TestReportDecoderRejectsAmbiguousTypesWithoutChangingReceiver(t *testing.T) {
	for _, input := range []string{`[]`, `{"authenticated":"true"}`, `{"posture":true}`, `{"posture":{"sandbox":7}}`, `{"posture":{"secrets_unregistered":1.5}}`, `{"posture":{"secrets_unregistered":999999999999999999999999}}`} {
		report := Report{Authenticated: true, Posture: &Posture{SecretsUnregistered: 3}}
		before := report
		if err := json.Unmarshal([]byte(input), &report); err == nil {
			t.Fatal("accepted invalid wire type", input)
		}
		if !reflect.DeepEqual(report, before) {
			t.Fatal("failed decode changed receiver")
		}
	}
	if _, err := Render([]Finding{{Severity: "unknown"}}); err == nil {
		t.Fatal("render accepted unknown severity")
	}
}
