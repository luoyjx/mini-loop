package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/httpapi"
)

type publicAuthFailure struct {
	ExceptionClass string `json:"exception_class"`
	Message        string `json:"message"`
}

type publicAuthPrincipal struct {
	ID        agent.OwnerID `json:"id"`
	Anonymous bool          `json:"anonymous"`
}

type publicAuthObservation struct {
	Header    *string              `json:"header"`
	Principal *publicAuthPrincipal `json:"principal"`
	Error     *publicAuthFailure   `json:"error"`
}

type publicAuthSnapshot struct {
	AuthClass    string                  `json:"auth_class"`
	Configured   bool                    `json:"configured"`
	Principals   []agent.OwnerID         `json:"principals"`
	Observations []publicAuthObservation `json:"observations"`
}

type publicAuthConstructorRecipe struct {
	Name          string                 `json:"name"`
	Bindings      []httpapi.TokenBinding `json:"bindings"`
	Headers       []*string              `json:"headers"`
	AfterBindings []httpapi.TokenBinding `json:"after_bindings"`
}

type publicAuthConstructorRow struct {
	Recipe            publicAuthConstructorRecipe `json:"recipe"`
	ConstructionError *publicAuthFailure          `json:"construction_error"`
	Before            *publicAuthSnapshot         `json:"before"`
	After             *publicAuthSnapshot         `json:"after"`
}

type publicAuthEnvironmentRecipe struct {
	Name         string            `json:"name"`
	Environ      map[string]string `json:"environ"`
	Ambient      map[string]string `json:"ambient"`
	Headers      []*string         `json:"headers"`
	AfterEnviron map[string]string `json:"after_environ"`
	AfterAmbient map[string]string `json:"after_ambient"`
}

type publicAuthEnvironmentRow struct {
	Recipe            publicAuthEnvironmentRecipe `json:"recipe"`
	ConstructionError *publicAuthFailure          `json:"construction_error"`
	Before            *publicAuthSnapshot         `json:"before"`
	After             *publicAuthSnapshot         `json:"after"`
}

type publicAuthFixture struct {
	Constructors   []publicAuthConstructorRow `json:"constructors"`
	Environments   []publicAuthEnvironmentRow `json:"environments"`
	PythonVersion  string                     `json:"python_version"`
	Scope          []string                   `json:"scope"`
	SourceSHA256   map[string]string          `json:"source_sha256"`
	ProducerSHA256 string                     `json:"producer_sha256"`
}

func readPublicAuth(t *testing.T) publicAuthFixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "testdata", "python-public-auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture publicAuthFixture
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	var extra struct{}
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatal("unexpected trailing fixture value", err)
	}
	if len(fixture.Constructors) != 12 || len(fixture.Environments) != 24 {
		t.Fatal("public authentication fixture inventory changed")
	}
	return fixture
}

func publicAuthError(err error) *publicAuthFailure {
	if err == nil {
		return nil
	}
	// Construction errors are ordinary Go errors; this finite Source constructor
	// contract reports their diagnostic through its ValueError adaptation.
	return &publicAuthFailure{ExceptionClass: "ValueError", Message: err.Error()}
}

func observePublicAuth(t *testing.T, auth httpapi.Authenticator, headers []*string) *publicAuthSnapshot {
	t.Helper()
	result := &publicAuthSnapshot{Configured: auth.Configured(), Observations: []publicAuthObservation{}}
	switch value := auth.(type) {
	case *httpapi.TokenAuth:
		result.AuthClass, result.Principals = "TokenAuth", value.Principals()
		if len(result.Principals) != 0 {
			copyOf := value.Principals()
			copyOf[0] = "mutated returned principals"
			if !reflect.DeepEqual(result.Principals, value.Principals()) {
				t.Fatal("principal listing leaked mutable authenticator state")
			}
		}
	case httpapi.NullAuth:
		result.AuthClass = "NullAuth"
	default:
		t.Fatalf("unexpected native authenticator %T", auth)
	}
	for _, header := range headers {
		text := ""
		if header != nil {
			text = *header
		}
		var native httpapi.AuthenticationResult
		if detailed, ok := auth.(httpapi.ResultAuthenticator); ok {
			native = detailed.AuthenticateResult(text)
		} else {
			caller, accepted := auth.Authenticate(text)
			native.Disposition = httpapi.AuthenticationRejected
			if accepted {
				native.Disposition, native.Principal = httpapi.AuthenticationAccepted, caller
			}
		}
		observation := publicAuthObservation{Header: header}
		switch native.Disposition {
		case httpapi.AuthenticationAccepted:
			if native.Failure != nil || native.Principal.ID == "" {
				t.Fatal("accepted authentication has failure or empty principal")
			}
			observation.Principal = &publicAuthPrincipal{ID: native.Principal.ID, Anonymous: native.Principal.Anonymous}
		case httpapi.AuthenticationRejected:
			if native.Failure != nil || native.Principal != (httpapi.Principal{}) {
				t.Fatal("rejected authentication carried a failure or principal")
			}
		case httpapi.AuthenticationFailed:
			if native.Failure == nil || native.Principal != (httpapi.Principal{}) {
				t.Fatal("failed authentication lost its failure or carried a principal")
			}
			observation.Error = &publicAuthFailure{ExceptionClass: string(native.Failure.ExceptionClass), Message: native.Failure.Message}
		default:
			t.Fatalf("unexpected authentication disposition %q", native.Disposition)
		}
		caller, accepted := auth.Authenticate(text)
		if accepted != (native.Disposition == httpapi.AuthenticationAccepted) || caller != native.Principal {
			t.Fatal("legacy bool authentication is not an acceptance-only projection")
		}
		result.Observations = append(result.Observations, observation)
	}
	return result
}

func TestPublicTokenAuthMatchesActualPython(t *testing.T) {
	for _, row := range readPublicAuth(t).Constructors {
		t.Run(row.Recipe.Name, func(t *testing.T) {
			bindings := append([]httpapi.TokenBinding(nil), row.Recipe.Bindings...)
			auth, err := httpapi.NewTokenAuth(bindings)
			if !reflect.DeepEqual(publicAuthError(err), row.ConstructionError) {
				t.Fatalf("construction error = %#v; Source = %#v", publicAuthError(err), row.ConstructionError)
			}
			if err != nil {
				if row.Before != nil || row.After != nil {
					t.Fatal("failed Source construction has observations")
				}
				return
			}
			if actual := observePublicAuth(t, auth, row.Recipe.Headers); !reflect.DeepEqual(actual, row.Before) {
				t.Fatalf("native before = %+v; Source = %+v", actual, row.Before)
			}
			if row.Recipe.AfterBindings != nil {
				for index := range bindings {
					bindings[index] = httpapi.TokenBinding{}
				}
				bindings = append(bindings[:0], row.Recipe.AfterBindings...)
				if actual := observePublicAuth(t, auth, row.Recipe.Headers); !reflect.DeepEqual(actual, row.After) {
					t.Fatalf("native after input mutation = %+v; Source = %+v", actual, row.After)
				}
			}
		})
	}
}

func setPublicAuthEnvironment(t *testing.T, values map[string]string) {
	t.Helper()
	for _, key := range []string{"MINILOOP_API_TOKEN", "MINILOOP_API_TOKENS"} {
		t.Setenv(key, values[key])
	}
}

func TestPublicLoadAuthMatchesActualPython(t *testing.T) {
	for _, row := range readPublicAuth(t).Environments {
		t.Run(row.Recipe.Name, func(t *testing.T) {
			setPublicAuthEnvironment(t, row.Recipe.Ambient)
			var environ map[string]string
			if row.Recipe.Environ != nil {
				environ = make(map[string]string, len(row.Recipe.Environ))
				for key, value := range row.Recipe.Environ {
					environ[key] = value
				}
			}
			auth, err := httpapi.AuthFromEnvironment(environ)
			if !reflect.DeepEqual(publicAuthError(err), row.ConstructionError) {
				t.Fatalf("construction error = %#v; Source = %#v", publicAuthError(err), row.ConstructionError)
			}
			if err != nil {
				if row.Before != nil || row.After != nil {
					t.Fatal("failed Source construction has observations")
				}
				return
			}
			if actual := observePublicAuth(t, auth, row.Recipe.Headers); !reflect.DeepEqual(actual, row.Before) {
				t.Fatalf("native before = %+v; Source = %+v", actual, row.Before)
			}
			if row.Recipe.AfterEnviron != nil {
				clear(environ)
				for key, value := range row.Recipe.AfterEnviron {
					environ[key] = value
				}
			}
			if row.Recipe.AfterAmbient != nil {
				setPublicAuthEnvironment(t, row.Recipe.AfterAmbient)
			}
			if row.After != nil {
				if actual := observePublicAuth(t, auth, row.Recipe.Headers); !reflect.DeepEqual(actual, row.After) {
					t.Fatalf("native after environment mutation = %+v; Source = %+v", actual, row.After)
				}
			}
		})
	}
}

func TestPublicTokenBindingsRetainTypedDuplicateAdmission(t *testing.T) {
	// Source Mapping inputs have unique keys. The Go sequence seam deliberately
	// refuses conflicting repeated credentials rather than selecting an owner.
	if _, err := httpapi.NewTokenAuth([]httpapi.TokenBinding{{Token: "x", Principal: "alice"}, {Token: "x", Principal: "bob"}}); err == nil {
		t.Fatal("typed sequence admitted a shared credential")
	}
	if auth, err := httpapi.NewTokenAuth([]httpapi.TokenBinding{{Token: "x", Principal: "alice"}, {Token: "x", Principal: "alice"}}); err != nil || len(auth.Principals()) != 1 {
		t.Fatal("identical native bindings were not deduplicated", err)
	}
}
