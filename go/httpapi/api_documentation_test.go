package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestDocumentationRoutesMatchActualSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-api-documentation.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Path, Method string
			TokenAuth          bool `json:"token_auth"`
			Authorization      string
			BodyKind           string `json:"body_kind"`
			Status             int
			Headers            map[string]string
			Allowed            []string        `json:"allowed_methods"`
			BodySHA            string          `json:"body_sha256"`
			BodyJSON           jsonvalue.Value `json:"body_json"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 36 {
		t.Fatal("Source documentation inventory changed", len(fixture.Cases))
	}
	manager := testManager(t, doneProvider{})
	token, err := NewTokenAuth([]TokenBinding{{Token: "fixture-token", Principal: "owner"}})
	if err != nil {
		t.Fatal(err)
	}
	anonymous := httptest.NewServer(testServer(t, Config{Manager: manager}))
	defer anonymous.Close()
	authenticated := httptest.NewServer(testServer(t, Config{Manager: manager, Auth: token}))
	defer authenticated.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, row := range fixture.Cases {
		t.Run(row.Path+"/"+row.Name, func(t *testing.T) {
			host := anonymous.URL
			if row.TokenAuth {
				host = authenticated.URL
			}
			body := ""
			if row.BodyKind == "oversized" {
				body = strings.Repeat("x", MaxRequestBytes+1)
			}
			request, err := http.NewRequest(row.Method, host+row.Path, strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			if row.Authorization != "" {
				request.Header.Set("Authorization", row.Authorization)
			}
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			actual, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != row.Status {
				t.Fatalf("status=%d want=%d body=%s", response.StatusCode, row.Status, actual)
			}
			if row.BodyJSON.Kind() != jsonvalue.Null {
				value, err := jsonvalue.Decode(string(actual))
				if err != nil || !value.PythonEqual(row.BodyJSON) {
					t.Fatalf("Source JSON error differs: %s (%v)", actual, err)
				}
			} else {
				sum := sha256.Sum256(actual)
				if hex.EncodeToString(sum[:]) != row.BodySHA {
					t.Fatalf("Source document bytes differ: %s", actual)
				}
			}
			for _, key := range []string{"content-type", "content-length", "content-security-policy", "x-content-type-options", "x-frame-options", "referrer-policy", "www-authenticate", "location"} {
				value := response.Header.Get(key)
				if key == "content-length" && row.BodyJSON.Kind() != jsonvalue.Null {
					if value != strconv.Itoa(len(actual)) {
						t.Errorf("error response length=%s bytes=%d", value, len(actual))
					}
					continue
				}
				if key == "location" {
					value = strings.Replace(value, host, "http://fixture", 1)
				}
				if value != row.Headers[key] {
					t.Errorf("%s=%q want=%q", key, value, row.Headers[key])
				}
			}
			allowed := []string{}
			for _, method := range strings.Split(response.Header.Get("Allow"), ",") {
				if method = strings.TrimSpace(method); method != "" {
					allowed = append(allowed, method)
				}
			}
			sort.Strings(allowed)
			if !reflect.DeepEqual(allowed, row.Allowed) {
				t.Errorf("allowed=%v want=%v", allowed, row.Allowed)
			}
			if row.Path == "/openapi.json" && row.Name == "anonymous-get" {
				source, err := os.ReadFile("../testdata/python-openapi.json")
				if err != nil {
					t.Fatal(err)
				}
				expected, err := jsonvalue.Decode(string(source))
				if err != nil {
					t.Fatal(err)
				}
				value, err := jsonvalue.Decode(string(actual))
				if err != nil || !value.PythonEqual(expected) {
					t.Fatal("OpenAPI schema differs", err)
				}
			}
		})
	}
}
