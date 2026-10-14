package provider_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/provider"
)

type requiredIDNAFrame struct {
	URL           string  `json:"url"`
	RawPath       string  `json:"raw_path"`
	Method        string  `json:"method"`
	APIKey        *string `json:"api_key"`
	Authorization *string `json:"authorization"`
}

type requiredIDNAError struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type requiredIDNACase struct {
	Name     string               `json:"name"`
	BaseURL  string               `json:"base_url"`
	Mode     string               `json:"mode"`
	Profile  string               `json:"profile"`
	Identity provider.Description `json:"identity"`
	Stage    string               `json:"stage"`
	Error    *requiredIDNAError   `json:"error"`
	Calls    []requiredIDNAFrame  `json:"calls"`
	Text     *string              `json:"text"`
}

type requiredIDNAFixture struct {
	Cases                   []requiredIDNACase `json:"cases"`
	SDKVersion              string             `json:"sdk_version"`
	HTTPXVersion            string             `json:"httpx_version"`
	IDNAVersion             string             `json:"idna_version"`
	IDNATableUnicodeVersion string             `json:"idna_table_unicode_version"`
	UnicodeVersion          string             `json:"unicode_version"`
	PythonImplementation    string             `json:"python_implementation"`
	PythonVersion           string             `json:"python_version"`
	Scope                   string             `json:"scope"`
	SourceSHA256            map[string]string  `json:"source_sha256"`
	Message                 jsonvalue.Value    `json:"message"`
	SSE                     string             `json:"sse"`
}

type requiredIDNAObservation struct {
	Stage    string                `json:"stage"`
	Identity *provider.Description `json:"identity"`
	Error    *requiredIDNAError    `json:"error"`
	Calls    []requiredIDNAFrame   `json:"calls"`
	Text     *string               `json:"text"`
}

type requiredIDNATransport func(*http.Request) (*http.Response, error)

func (transport requiredIDNATransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

// These stages/profiles are from the actual Source fixture, never from the
// native hostname implementation. Both modes must occur for every recipe.
var requiredIDNAProfiles = []struct{ name, profile, stage string }{
	{"unicode-host", "unicode", "complete"},
	{"punycode-host", "ascii", "complete"},
	{"ascii-underscore", "ascii-bypass", "complete"},
	{"ascii-malformed-alabel", "ascii-bypass", "request"},
	{"ascii-long-label", "ascii-bypass", "complete"},
	{"uppercase-unicode-host", "lower-order", "complete"},
	{"uppercase-sharp-s-host", "lower-order", "complete"},
	{"dotted-i-host", "lower-order", "complete"},
	{"greek-across-dot-host", "lower-order", "complete"},
	{"lower-becomes-ascii-with-invalid-label", "lower-order", "constructor"},
	{"nfc-composed", "nfc", "complete"},
	{"nfc-decomposed", "nfc", "constructor"},
	{"leading-combining", "leading-combining", "constructor"},
	{"ideographic-dot", "separator", "complete"},
	{"fullwidth-dot", "separator", "complete"},
	{"halfwidth-dot", "separator", "complete"},
	{"trailing-unicode-dot", "separator", "complete"},
	{"empty-unicode-label", "separator", "constructor"},
	{"label-63-with-unicode", "length", "complete"},
	{"label-64-with-unicode", "length", "constructor"},
	{"zwnj-joining-context", "contextj", "complete"},
	{"zwnj-nonjoining-context", "contextj", "constructor"},
	{"zwj-virama-context", "contextj", "complete"},
	{"zwj-without-virama", "contextj", "constructor"},
	{"middle-dot-context", "contexto", "complete"},
	{"middle-dot-outside-context", "contexto", "constructor"},
	{"greek-numeral-context", "contexto", "complete"},
	{"greek-numeral-outside-context", "contexto", "constructor"},
	{"hebrew-punctuation-context", "contexto", "complete"},
	{"hebrew-punctuation-outside-context", "contexto", "constructor"},
	{"katakana-middle-dot-context", "contexto", "complete"},
	{"katakana-middle-dot-outside-context", "contexto", "constructor"},
	{"arabic-indic-digits", "contexto-digits", "complete"},
	{"extended-arabic-indic-digits", "contexto-digits", "complete"},
	{"mixed-arabic-digit-sets", "contexto-digits", "constructor"},
	{"numeric-label-before-rtl", "labelwise-bidi", "complete"},
	{"rtl-with-european-digit", "bidi", "complete"},
	{"rtl-mixed-numeral-directions", "bidi", "constructor"},
	{"rtl-mixed-latin", "bidi", "constructor"},
	{"disallowed-snowman", "disallowed", "constructor"},
	{"unicode-with-ascii-underscore", "ascii-bypass", "constructor"},
	{"newer-unicode-table-codepoint", "unicode-version", "constructor"},
}

func requiredIDNAHeader(header http.Header, name string) *string {
	values, present := header[http.CanonicalHeaderKey(name)]
	if !present {
		return nil
	}
	value := strings.Join(values, ", ")
	return &value
}

func TestRequiredIDNAActualSourceDirectAndStream(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-idna.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture requiredIDNAFixture
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.SDKVersion != "0.107.1" || fixture.HTTPXVersion != "0.28.1" || fixture.IDNAVersion != "3.18" ||
		fixture.IDNATableUnicodeVersion != "17.0.0" || fixture.UnicodeVersion != "14.0.0" ||
		fixture.PythonImplementation != "CPython" || fixture.PythonVersion != "3.11.3" ||
		fixture.Scope != "Actual provider/SDK direct and SSE request objects via MockTransport; no live network or wire-host claim." ||
		fixture.SourceSHA256["tools/provider_idna_contracts.py"] != "192d8a52ed25e4c4c70f554437c55e61e267cc6dfc65fd1bd306b2bb5047d5c7" ||
		fixture.SourceSHA256["tools/provider_url_contracts.py"] != "e11c51c6d291a512dc640d2ca2bbf1b631f42f053a41ff941484e9881bc354fe" ||
		len(fixture.Cases) != 84 || len(requiredIDNAProfiles) != 42 {
		t.Fatal("fixed actual Source IDNA profile changed")
	}
	message, err := fixture.Message.MarshalUTF8()
	if err != nil || fixture.SSE == "" {
		t.Fatal("Source reply documents unavailable", err)
	}
	seen := make(map[string]bool, len(fixture.Cases))
	for _, row := range fixture.Cases {
		key := row.Mode + "/" + row.Name
		if (row.Mode != "direct" && row.Mode != "stream") || seen[key] {
			t.Fatal("invalid or duplicate Source IDNA mode/recipe", key)
		}
		seen[key] = true
		profileFound := false
		for _, expected := range requiredIDNAProfiles {
			if expected.name == row.Name {
				profileFound = true
				if row.Profile != expected.profile || row.Stage != expected.stage {
					t.Fatal("captured Source stage/profile changed", key, row.Profile, row.Stage)
				}
			}
		}
		if !profileFound || row.Identity != (provider.Description{Name: "anthropic-compatible", Wire: "anthropic", Endpoint: row.BaseURL, Credential: "<set>"}) {
			t.Fatal("Source identity or profile inventory changed", key)
		}
		if row.Stage == "complete" {
			if row.Error != nil || row.Text == nil || *row.Text != "ok" || len(row.Calls) != 1 {
				t.Fatal("Source positive recipe changed", key)
			}
		} else if row.Error == nil || row.Error.Text == "" || len(row.Calls) != 0 || row.Text != nil ||
			(row.Stage == "constructor" && row.Error.Type != "InvalidURL") ||
			(row.Stage == "request" && row.Error.Type != "InvalidCodepoint") {
			t.Fatal("Source negative phase/error recipe changed", key)
		}

		t.Run(key, func(t *testing.T) {
			native := requiredIDNAObservation{Stage: "constructor", Calls: []requiredIDNAFrame{}}
			transport := requiredIDNATransport(func(request *http.Request) (*http.Response, error) {
				if request.Body != nil {
					_, readErr := io.Copy(io.Discard, request.Body)
					closeErr := request.Body.Close()
					if readErr != nil || closeErr != nil {
						t.Error("owned mock request body did not finish", readErr, closeErr)
					}
				}
				// This compares the custom-transport request object captured by
				// HTTPX, not net/http's DNS, Host header, TLS SNI or on-wire target.
				native.Calls = append(native.Calls, requiredIDNAFrame{URL: request.URL.String(),
					RawPath: request.URL.RawPath, Method: request.Method,
					APIKey:        requiredIDNAHeader(request.Header, "x-api-key"),
					Authorization: requiredIDNAHeader(request.Header, "authorization")})
				body, contentType := string(message), "application/json"
				if row.Mode == "stream" {
					body, contentType = fixture.SSE, "text/event-stream"
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}},
					Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
			})
			zero := 0 // Source injects max_retries=0; no retry hides an admission difference.
			cfg := provider.Config{BaseURL: row.BaseURL, APIKey: "fixture-key", MaxRetries: &zero,
				HTTPClient: &http.Client{Transport: transport}}
			request := protocol.ModelRequest{Model: "fixture-model", MaxTokens: 8, Purpose: protocol.PurposeAgentTurn,
				Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("hello")}}}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			var reply protocol.ModelReply
			var callErr error
			if row.Mode == "stream" {
				client, constructorErr := provider.NewStreaming(cfg)
				callErr = constructorErr
				if constructorErr == nil {
					defer client.CloseIdleConnections()
					description := client.Describe()
					native.Identity, native.Stage = &description, "request"
					reply, callErr = client.CompleteStream(ctx, request, nil)
				}
			} else {
				client, constructorErr := provider.New(cfg)
				callErr = constructorErr
				if constructorErr == nil {
					defer client.CloseIdleConnections()
					description := client.Describe()
					native.Identity, native.Stage = &description, "request"
					reply, callErr = client.Complete(ctx, request)
				}
			}
			if callErr != nil {
				native.Error = &requiredIDNAError{Type: fmt.Sprintf("%T", callErr), Text: callErr.Error()}
				if strings.Contains(callErr.Error(), "fixture-key") {
					t.Error("admission diagnostic disclosed the API key")
				}
			} else {
				native.Stage = "complete"
				if len(reply.Content) != 1 {
					t.Error("reply must contain the captured single text block")
				} else if text, ok := reply.Content[0].Text(); !ok {
					t.Error("reply must contain the captured text variant")
				} else {
					native.Text = &text.Text
				}
			}
			observation, marshalErr := json.Marshal(native)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			t.Logf("actual native=%s; Source phase=%s error=%v", observation, row.Stage, row.Error)
			if native.Stage != row.Stage || (native.Error != nil) != (row.Error != nil) {
				t.Errorf("Source admission phase/error-presence differs: native=%s Source=%s", native.Stage, row.Stage)
			}
			if !reflect.DeepEqual(native.Calls, row.Calls) {
				t.Errorf("Source exact target/credential/request count differs: native=%+v Source=%+v", native.Calls, row.Calls)
			}
			if !reflect.DeepEqual(native.Text, row.Text) {
				t.Error("Source final text/presence differs")
			}
			// Source describes its Provider before SDK construction. Native
			// Describe belongs to an admitted Client, so no such public object
			// exists after constructor refusal; do not fabricate its identity.
			if native.Identity != nil && *native.Identity != row.Identity {
				t.Errorf("Source admitted-client identity differs: native=%+v Source=%+v", native.Identity, row.Identity)
			}
			// Go admission errors are opaque. Phase, absence of dispatch and
			// non-nil error are compared; Python exception names/messages are
			// retained as Source evidence, not asserted as a Go exception API.
		})
	}
	for _, profile := range requiredIDNAProfiles {
		for _, mode := range []string{"direct", "stream"} {
			if !seen[mode+"/"+profile.name] {
				t.Error("missing actual Source recipe", mode, profile.name)
			}
		}
	}
}
