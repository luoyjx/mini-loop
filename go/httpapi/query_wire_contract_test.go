package httpapi

import (
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
)

// This observer records bytes delivered through the real Go TCP server before
// the application reads any query values. The Source side captures ASGI bytes;
// this corpus makes no claim about a Uvicorn or proxy request parser.
type queryContractWireRequest struct {
	RawQuery   string
	RequestURI string
}

// This records an exception raised by the actual Source ASGI application after
// it sent its error response. The Go TCP consumer compares that response, not
// an exception class or message that a remote HTTP client cannot observe.
type queryContractSourceFault struct {
	ExceptionClass string `json:"exception_class"`
	Message        string `json:"message"`
	Stage          string `json:"stage"`
}

func queryContractRawQuery(t *testing.T, row querySelectionCase, identities []querySelectionIdentity) string {
	t.Helper()
	if row.RawQueryHex != nil {
		if len(row.Query) != 0 {
			t.Fatal("Source case mixes raw bytes and encoded query pairs", row.Name)
		}
		data, err := hex.DecodeString(*row.RawQueryHex)
		if err != nil {
			t.Fatal("invalid Source raw query hex", row.Name, err)
		}
		// The raw corpus has no dynamic IDs in query bytes. In particular, never
		// URL-encode, Unicode-normalize or replace the captured byte sequence.
		return string(data)
	}
	query := make([]string, 0, len(row.Query))
	for _, pair := range row.Query {
		query = append(query, url.QueryEscape(pair.Name)+"="+url.QueryEscape(querySelectionResolve(pair.Value, identities)))
	}
	return strings.Join(query, "&")
}

func TestHTTPRawQueryWireMatchesActualPython(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-http-query-wire.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture querySelectionFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	expected := []string{
		"auth/percent-last",
		"auth/bad-escape-last",
		"auth/semicolon-last",
		"auth/encoded-semicolon-last",
		"auth/escaped-percent-last",
		"auth/utf8-valid-last",
		"auth/utf8-truncated-last",
		"auth/percent-first",
		"auth/header-valid",
		"auth/header-invalid",
		"auth/encoded-key",
		"auth/foreign",
		"limit/percent-last",
		"limit/bad-escape-last",
		"limit/semicolon-last",
		"limit/encoded-semicolon-last",
		"limit/escaped-percent-last",
		"limit/utf8-valid-last",
		"limit/utf8-truncated-last",
		"limit/utf8-invalid-percent-last",
		"limit/valid-literal-utf8-last",
		"limit/percent-first",
		"limit/encoded-key",
		"limit/foreign-invalid",
	}
	if len(fixture.Cases) != len(expected) {
		t.Fatalf("Source raw query inventory has %d cases, want %d", len(fixture.Cases), len(expected))
	}
	for _, name := range expected {
		count := 0
		group, _, _ := strings.Cut(name, "/")
		for _, row := range fixture.Cases {
			if row.Name != name {
				continue
			}
			count++
			if row.Group != group || row.Method != "GET" || row.RawQueryHex == nil || len(row.Query) != 0 {
				t.Fatal("Source raw query recipe changed", name)
			}
			faultExpected := name == "auth/utf8-valid-last" || name == "auth/utf8-truncated-last"
			if faultExpected {
				if row.SourceFault == nil || row.SourceFault.ExceptionClass != "TypeError" || row.SourceFault.Message == "" || row.SourceFault.Stage != "after_response_start" || row.Status != 500 || row.BodyKind != "text" {
					t.Fatal("Source non-ASCII authentication fault observation changed", name)
				}
			} else if row.SourceFault != nil {
				t.Fatal("unexpected Source ASGI fault", name, *row.SourceFault)
			}
		}
		if count != 1 {
			t.Fatalf("Source raw query case %q occurs %d times", name, count)
		}
	}
	runHTTPQueryContracts(t, fixture)
}
