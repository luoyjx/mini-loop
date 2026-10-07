package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"
)

func TestActualPythonPersonalSkillHTTPSurrogates(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-personal-skill-http-surrogates.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Kind, Name, Body string
			Status           int
			Response         json.RawMessage
			Text             *string
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || len(fixture.Cases) != 90 {
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
			r.Header.Set("Content-Type", "application/json; charset=utf-8")
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
				t.Fatalf("surrogate response differs\ngot  %s\nwant %s", got, want)
			}
		})
	}
}

func TestPersonalSkillDecoderPreservesValidatedScalarValues(t *testing.T) {
	s := testServer(t, Config{Manager: testManager(t, doneProvider{})})
	for _, row := range []struct{ name, body, focus string }{
		{"escaped-pair", `{"name":"safe","focus":"\ud800\udc00"}`, "\U00010000"},
		{"discarded-raw", "{\"name\":\"safe\",\"focus\":\"\xed\xa0\x80\",\"focus\":\"okay\"}", "okay"},
		{"discarded-escape", `{"name":"safe","focus":"\ud800","focus":"okay"}`, "okay"},
		{"control-escapes", `{"name":"safe","focus":"\b\f\n\r\t\\\/\""}`, "\b\f\n\r\t\\/\""},
	} {
		t.Run(row.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", bytes.NewBufferString(row.body))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			got, ok := decodePersonalSkillBody[PersonalSkillPreviewRequest](s, w, r, true)
			if !ok || got.Name != "safe" || got.Focus != row.focus {
				t.Fatal(ok, got, w.Body.String())
			}
		})
	}
}
