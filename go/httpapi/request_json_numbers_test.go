package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"
)

func TestActualPythonPersonalSkillHTTPNumbers(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-personal-skill-http-numbers.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		IntegerDigitLimit int `json:"integer_digit_limit"`
		Cases             []struct {
			Kind, Name, Raw string
			Status          int
			Response        json.RawMessage
			Text            *string
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || len(fixture.Cases) != 288 {
		t.Fatal(err, len(fixture.Cases))
	}
	if fixture.IntegerDigitLimit != requestIntegerDigitLimit {
		t.Fatalf("source digit limit changed: %d", fixture.IntegerDigitLimit)
	}
	s := testServer(t, Config{Manager: testManager(t, doneProvider{}), Auth: tokenAuth(t)})
	for _, row := range fixture.Cases {
		t.Run(row.Kind+"/"+row.Name, func(t *testing.T) {
			body := []byte(row.Raw)
			suffix := "preview"
			if row.Kind == "commit" {
				suffix = "draft/commit"
			}
			r := httptest.NewRequest("POST", "/sessions/missing/personal-skills/"+suffix, bytes.NewReader(body))
			r.Header.Set("Authorization", "Bearer token-a")
			r.Header.Set("Content-Type", "application/json; charset=utf-8")
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != row.Status {
				t.Fatal(w.Code, w.Body.String(), row.Status)
			}
			if row.Text != nil {
				if w.Body.String() != *row.Text || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
					t.Fatal(w.Body.String(), *row.Text)
				}
				return
			}
			got, want := normalizeJSON(t, w.Body.Bytes(), nil, nil), normalizeJSON(t, row.Response, nil, nil)
			if !bytes.Equal(got, want) {
				t.Fatalf("numeric response differs\ngot  %s\nwant %s", got, want)
			}
		})
	}
}
