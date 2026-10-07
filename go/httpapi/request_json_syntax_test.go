package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"
)

func TestActualPythonPersonalSkillHTTPParsing(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-personal-skill-http-parsing.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Kind, Name, Body string
			Media            *string
			Status           int
			Response         json.RawMessage
			Text             *string
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || len(fixture.Cases) != 63 {
		t.Fatal(err, len(fixture.Cases))
	}
	s := testServer(t, Config{Manager: testManager(t, doneProvider{}), Auth: tokenAuth(t)})
	for _, row := range fixture.Cases {
		t.Run(row.Kind+"/"+row.Name, func(t *testing.T) {
			body, err := base64.StdEncoding.DecodeString(row.Body)
			if err != nil {
				t.Fatal(err)
			}
			suffix := "preview"
			if row.Kind == "commit" {
				suffix = "draft/commit"
			}
			r := httptest.NewRequest("POST", "/sessions/missing/personal-skills/"+suffix, bytes.NewReader(body))
			r.Header.Set("Authorization", "Bearer token-a")
			if row.Media != nil {
				r.Header.Set("Content-Type", *row.Media)
			}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != row.Status {
				t.Fatal(w.Code, w.Body.String(), row.Status)
			}
			if row.Text != nil {
				if w.Body.String() != *row.Text {
					t.Fatal(w.Body.String(), *row.Text)
				}
				return
			}
			got, want := normalizeJSON(t, w.Body.Bytes(), nil, nil), normalizeJSON(t, row.Response, nil, nil)
			if !bytes.Equal(got, want) {
				t.Fatalf("parsing response differs\ngot  %s\nwant %s", got, want)
			}
		})
	}
}
