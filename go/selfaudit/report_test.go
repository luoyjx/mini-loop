package selfaudit_test

import (
	"encoding/json"
	"errors"
	"github.com/luoyjx/mini-loop/go/benchmark"
	"github.com/luoyjx/mini-loop/go/selfaudit"
	"os"
	"reflect"
	"strings"
	"testing"
)

type contract struct {
	Cases []struct {
		Name            string                     `json:"name"`
		Observation     selfaudit.Observations     `json:"observation"`
		Owner           *string                    `json:"owner"`
		IncludeGlobal   bool                       `json:"include_global"`
		Limit           int                        `json:"limit"`
		Report          string                     `json:"report"`
		Objectives      []selfaudit.Suggestion     `json:"objectives"`
		ObjectivesError *string                    `json:"objectives_error"`
		Drafts          []selfaudit.BenchTaskDraft `json:"drafts"`
		DraftsError     *string                    `json:"drafts_error"`
	}
}

func TestActualSourceSelfAudit(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-self-audit.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture contract
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 26 {
		t.Fatalf("source inventory %d", len(fixture.Cases))
	}
	for _, test := range fixture.Cases {
		t.Run(test.Name, func(t *testing.T) {
			before, _ := json.Marshal(test.Observation)
			report := selfaudit.BuildReport(test.Observation, selfaudit.Scope{test.Owner, test.IncludeGlobal})
			if report != test.Report {
				t.Fatalf("report mismatch\ngot %s\nwant %s", report, test.Report)
			}
			objectives, err := selfaudit.SuggestObjectives(test.Observation, test.Owner, test.Limit)
			checkError(t, err, test.ObjectivesError)
			if !reflect.DeepEqual(objectives, test.Objectives) {
				t.Fatalf("objectives %+v vs %+v", objectives, test.Objectives)
			}
			drafts, err := selfaudit.SuggestBenchTasks(test.Observation, test.Owner, test.Limit)
			checkError(t, err, test.DraftsError)
			got, _ := json.Marshal(drafts)
			want, _ := json.Marshal(test.Drafts)
			if string(got) != string(want) {
				t.Fatalf("drafts %s vs %s", got, want)
			}
			after, _ := json.Marshal(test.Observation)
			if string(before) != string(after) {
				t.Fatal("observation mutated")
			}
			for _, draft := range drafts {
				if _, err := benchmark.NewTask(benchmark.TaskConfig{Name: draft.Name, Prompt: draft.PromptDraft}); !errors.Is(err, benchmark.ErrInvalidTask) {
					t.Fatal("draft admitted without human predicate")
				}
			}
		})
	}
}
func checkError(t *testing.T, err error, want *string) {
	t.Helper()
	if want == nil {
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	var failure *selfaudit.ObservationError
	if !errors.As(err, &failure) || failure.Class != *want {
		t.Fatalf("error %v vs %s", err, *want)
	}
	if strings.Contains(err.Error(), "private failure content") {
		t.Fatal("private error leaked")
	}
}

func TestDraftExpectationIsClosed(t *testing.T) {
	for _, input := range []string{`{}`, `true`, `"callable"`, `[]`} {
		var expectation selfaudit.NoExpectation
		if err := json.Unmarshal([]byte(input), &expectation); err == nil {
			t.Fatalf("draft expectation accepted %s", input)
		}
	}
	var expectation selfaudit.NoExpectation
	if err := json.Unmarshal([]byte(" null \n"), &expectation); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(expectation)
	if err != nil || string(encoded) != "null" {
		t.Fatal("draft gained expectation")
	}
}
