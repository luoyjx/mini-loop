package audit

import (
	"encoding/json"
	"math/big"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/problems"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/selfaudit"
)

func TestComposeLiveMatchesFullActualSourceManagerAudits(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-audit-live.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name          string
			Environment   secrets.Environment
			Configuration []Finding
			Observation   LiveObservation
			Findings      []Finding
			Rendered      string
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 16 {
		t.Fatal("unexpected Source live audit corpus")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			before, err := json.Marshal(row.Observation)
			if err != nil {
				t.Fatal(err)
			}
			findings, err := ComposeLive(row.Configuration, row.Observation, row.Environment)
			if err != nil || !reflect.DeepEqual(findings, row.Findings) {
				t.Fatalf("%#v want %#v; %v", findings, row.Findings, err)
			}
			text, err := Render(findings)
			if err != nil || text != row.Rendered {
				t.Fatal(text, row.Rendered, err)
			}
			after, err := json.Marshal(row.Observation)
			if err != nil || string(after) != string(before) {
				t.Fatal("composition mutated observation")
			}
		})
	}
}

func TestComposeLiveRejectsIncompleteObservationRatherThanReturningPartialFindings(t *testing.T) {
	empty := []string{}
	cases := []LiveObservation{
		{MCP: selfaudit.Ledger{Failure: &selfaudit.Failure{Class: "RuntimeError"}}},
		{Cron: selfaudit.Ledger{Entries: []string{"fault"}, Summary: &empty}},
		{Skills: selfaudit.Ledger{Entries: []string{"fault"}, Summary: &[]string{"fault"}, Churning: true}},
		{Channels: []ProblemChannel{{Name: "same"}, {Name: "same"}}},
		{Channels: []ProblemChannel{{Name: ""}}},
		{Catalog: &CatalogObservation{Tools: []ToolObservation{{Name: protocol.ToolName("tool"), Risk: agent.ToolRisk("invalid")}}}},
	}
	for _, observation := range cases {
		if result, err := ComposeLive(nil, observation, secrets.Environment{}); err == nil || result != nil {
			t.Fatal("incomplete observation became findings", result, err)
		}
	}
	if result, err := ComposeLive([]Finding{{Severity: "unknown"}}, LiveObservation{}, secrets.Environment{}); err == nil || result != nil {
		t.Fatal(result, err)
	}
}

func TestComposeLiveKeepsExactUnboundedChurnTotalsAndPrivateChannelExclusions(t *testing.T) {
	totalValue, ok := new(big.Int).SetString("123456789012345678901234567890", 10)
	if !ok {
		t.Fatal("invalid constant")
	}
	total, err := problems.FromBigInt(totalValue)
	if err != nil {
		t.Fatal(err)
	}
	summary := []string{"fault (x3)"}
	ledger := selfaudit.Ledger{Entries: []string{"fault"}, Summary: &summary, Total: &total, Churning: true}
	observation := LiveObservation{Catalog: &CatalogObservation{}, Channels: []ProblemChannel{
		{Name: "zeta", Ledger: ledger}, {Name: "alpha", Ledger: ledger},
		{Name: "_private", Ledger: ledger}, {Name: "cron", Ledger: ledger}, {Name: "skills", Ledger: ledger},
	}}
	configuration := []Finding{{Severity: Info, Check: FakeLLM, Detail: "original"}}
	before := append([]Finding(nil), configuration...)
	findings, err := ComposeLive(configuration, observation, secrets.Environment{})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 3 || findings[0].Check != "alpha-problems" || findings[1].Check != "zeta-problems" {
		t.Fatal(findings)
	}
	if !strings.Contains(findings[0].Detail, "123,456,789,012,345,678,901,234,567,890 occurrences seen") {
		t.Fatal(findings[0])
	}
	if !reflect.DeepEqual(configuration, before) {
		t.Fatal("configuration findings mutated")
	}
}
