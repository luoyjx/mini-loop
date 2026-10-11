package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// Explicit diagnostic capture, not a passing parity gate. Retain differences so
// the next implementation slice can turn the complete corpus into assertions.
func TestActualSourceRoutingAudit(t *testing.T) {
	output := os.Getenv("MINILOOP_ROUTING_AUDIT_OUTPUT")
	if output == "" {
		t.Skip("routing audit output was not requested")
	}
	baseline := os.Getenv("MINILOOP_ROUTING_AUDIT_BASELINE")
	if baseline == "" {
		t.Fatal("routing audit requires an explicit production baseline")
	}
	type result struct {
		Status   int               `json:"status"`
		Allowed  []string          `json:"allowed_methods"`
		Location *string           `json:"location"`
		Body     jsonvalue.Value   `json:"body_json"`
		Security map[string]string `json:"security_headers"`
	}
	var fixture struct {
		Cases []struct {
			Name, Method, Path, Authorization string
			Oversized                         bool `json:"oversized_body"`
			result
		}
	}
	data, err := os.ReadFile("../testdata/python-http-routing.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 160 {
		t.Fatal("routing fixture inventory changed")
	}
	auth, err := NewTokenAuth([]TokenBinding{{Token: "fixture-token", Principal: "owner"}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(testServer(t, Config{Manager: testManager(t, doneProvider{}), Auth: auth}))
	defer server.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	type observation struct {
		Name     string `json:"name"`
		Path     string `json:"path"`
		Expected result `json:"expected"`
		Actual   result `json:"actual"`
		Same     bool   `json:"same"`
	}
	rows := make([]observation, 0, len(fixture.Cases))
	for _, row := range fixture.Cases {
		body := ""
		if row.Oversized {
			body = strings.Repeat("x", MaxRequestBytes+1)
		}
		req, err := http.NewRequest(row.Method, server.URL+row.Path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if row.Authorization != "" {
			req.Header.Set("Authorization", row.Authorization)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		actual := result{Status: response.StatusCode, Allowed: []string{}, Security: map[string]string{}}
		if len(data) != 0 {
			actual.Body, err = jsonvalue.Decode(string(data))
			if err != nil {
				t.Fatal("unexpected non-JSON routing reply", err)
			}
		}
		if value := response.Header.Get("Location"); value != "" {
			value = strings.Replace(value, server.URL, "http://fixture", 1)
			actual.Location = &value
		}
		for _, value := range strings.Split(response.Header.Get("Allow"), ",") {
			if value = strings.TrimSpace(value); value != "" {
				actual.Allowed = append(actual.Allowed, value)
			}
		}
		sort.Strings(actual.Allowed)
		for key := range row.Security {
			actual.Security[key] = response.Header.Get(key)
		}
		same := actual.Status == row.Status && reflect.DeepEqual(actual.Allowed, row.Allowed) &&
			reflect.DeepEqual(actual.Location, row.Location) && actual.Body.PythonEqual(row.Body) &&
			reflect.DeepEqual(actual.Security, row.Security)
		rows = append(rows, observation{row.Name, row.Path, row.result, actual, same})
	}
	report := struct {
		Baseline string        `json:"production_baseline"`
		Cases    []observation `json:"cases"`
	}{baseline, rows}
	data, err = json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
