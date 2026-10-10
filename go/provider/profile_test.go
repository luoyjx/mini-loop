package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func writeProfileFixture(t *testing.T, root, name string, config, credentials []byte, mode os.FileMode) string {
	t.Helper()
	for _, directory := range []string{"configs", "credentials"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "configs", name+".json"), config, 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "credentials", name+".json")
	if err := os.WriteFile(path, credentials, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFileProfileMatchesSourceOAuthRecipes(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-auth.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Spec struct {
				Name                string
				Config, Credentials json.RawMessage
				Mode                *uint32
				Symlink             bool
			}
			Constructed bool
			Environment map[string]string
			Requests    []struct {
				URL     string
				Headers map[string]string
			}
			Exchanges []struct {
				URL, Beta string
				Body      jsonvalue.Value
			}
			Fault *struct{ Type string }
			After jsonvalue.Value `json:"credentials_after"`
			Mode  *uint32         `json:"credentials_mode"`
		} `json:"discovery_cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	selected := map[string]bool{"default-profile": true, "unknown-auth-kind": true, "missing-profile-token": true, "world-readable-profile": true, "symlink-profile-secret": true, "profile-endpoint-workspace": true, "environment-endpoint-wins": true, "oauth-refresh": true}
	tested := 0
	for _, row := range fixture.Cases {
		if !selected[row.Spec.Name] {
			continue
		}
		tested++
		t.Run(row.Spec.Name, func(t *testing.T) {
			if runtime.GOOS == "windows" && (row.Spec.Mode != nil || row.Spec.Symlink) {
				t.Skip("Source POSIX guard")
			}
			root := t.TempDir()
			mode := os.FileMode(0600)
			if row.Spec.Mode != nil {
				mode = os.FileMode(*row.Spec.Mode)
			}
			path := writeProfileFixture(t, root, "default", row.Spec.Config, row.Spec.Credentials, mode)
			if row.Spec.Symlink {
				target := filepath.Join(filepath.Dir(path), "real.json")
				if err := os.Rename(path, target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			}
			requests, exchanges := 0, 0
			httpClient := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				body := strings.ReplaceAll(goodReply, "done", "ok")
				if strings.HasSuffix(r.URL.Path, "/oauth/token") {
					if exchanges >= len(row.Exchanges) {
						t.Fatal("unexpected exchange")
					}
					expected := row.Exchanges[exchanges]
					exchanges++
					wire, _ := io.ReadAll(r.Body)
					value, err := jsonvalue.Decode(string(wire))
					var actual, want map[string]jsonvalue.Value
					_ = json.Unmarshal(wire, &actual)
					b, _ := expected.Body.MarshalJSON()
					_ = json.Unmarshal(b, &want)
					if err != nil || value.Kind() != jsonvalue.Object || !reflect.DeepEqual(actual, want) || r.URL.String() != expected.URL || r.Header.Get("Anthropic-Beta") != expected.Beta {
						t.Fatal("refresh differs from Source")
					}
					body = `{"access_token":"exchanged-token","expires_in":3600}`
				} else {
					if requests >= len(row.Requests) {
						t.Fatal("unexpected model request")
					}
					expected := row.Requests[requests]
					requests++
					got := map[string]string{}
					for _, key := range []string{"authorization", "anthropic-beta", "anthropic-workspace-id", "x-api-key"} {
						if value := r.Header.Get(key); value != "" {
							got[key] = value
						}
					}
					if r.URL.String() != expected.URL || !reflect.DeepEqual(got, expected.Headers) {
						t.Fatal("model headers/endpoint differ", got)
					}
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			now := func() time.Time { return time.Unix(1700000000, 0) }
			profile, err := NewFileProfile(ProfileConfig{Directory: root, Name: "default", BaseURL: row.Environment["ANTHROPIC_BASE_URL"], HTTPClient: httpClient, Now: now})
			if (err == nil) != row.Constructed {
				t.Fatal("construction phase differs", err)
			}
			if err != nil {
				return
			}
			client, err := New(Config{BaseURL: profile.ModelBaseURL(), Headers: profile.Headers(), Credentials: profile, HTTPClient: httpClient, Now: now})
			if err != nil {
				t.Fatal(err)
			}
			var failure error
			for i := 0; i < 2; i++ {
				_, failure = client.Complete(context.Background(), minimalRequest())
				if failure != nil {
					break
				}
			}
			if row.Fault != nil {
				var typed *Failure
				if !errors.As(failure, &typed) || string(typed.Class()) != row.Fault.Type {
					t.Fatal("request phase/class differs", failure)
				}
			} else if failure != nil {
				t.Fatal(failure)
			}
			if requests != len(row.Requests) || exchanges != len(row.Exchanges) {
				t.Fatal("Source call count differs", requests, exchanges)
			}
			if row.Mode != nil {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if uint32(info.Mode().Perm()) != *row.Mode {
					t.Fatal("refresh mode differs")
				}
				wire, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var expected, actual map[string]jsonvalue.Value
				a, _ := row.After.MarshalJSON()
				_ = json.Unmarshal(a, &expected)
				_ = json.Unmarshal(wire, &actual)
				if !reflect.DeepEqual(actual, expected) {
					t.Fatal("refresh document differs")
				}
			}
		})
	}
	if tested != len(selected) {
		t.Fatal("Source cases missing")
	}
}

func TestFileProfileRefreshRotationForceAndAtomicExtensions(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request oauthRefreshRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.GrantType != "refresh_token" || request.RefreshToken != "old-refresh" || request.ClientID != "client" || r.Header.Get("Anthropic-Beta") != OAuthAPIBeta {
			t.Error("unexpected refresh request")
		}
		io.WriteString(w, `{"access_token":"new-token","expires_in":"3_600"}`)
	}))
	defer server.Close()
	root := t.TempDir()
	path := writeProfileFixture(t, root, "default", []byte(fmt.Sprintf(`{"base_url":%q,"authentication":{"type":"user_oauth","client_id":"client"}}`, server.URL)), []byte(`{"access_token":"old-token","refresh_token":"old-refresh","expires_at":"2_000_000_000","opaque":{"version":7},"tag":[true,null]}`), 0640)
	profile, err := NewFileProfile(ProfileConfig{Directory: root, Name: "default", BaseURL: "https://model.invalid", Now: func() time.Time { return time.Unix(1700000000, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	token, err := profile.FetchToken(context.Background(), false)
	if err != nil || token.value != "old-token" || calls.Load() != 0 || profile.ModelBaseURL() != "https://model.invalid" {
		t.Fatal("fresh token bypass failed", err)
	}
	token, err = profile.FetchToken(context.Background(), true)
	if err != nil || token.value != "new-token" || token.expiry.Unix() != 1700003600 || calls.Load() != 1 {
		t.Fatal("forced refresh failed", err)
	}
	raw, err := readProfileJSON(path, true)
	if err != nil {
		t.Fatal(err)
	}
	extra, _ := raw.Lookup("opaque")
	version, _ := extra.Lookup("version")
	text, _ := version.Integer()
	if text != "7" {
		t.Fatal("extension lost")
	}
	refresh, _ := raw.Lookup("refresh_token")
	value, _ := refresh.Text()
	if value != "old-refresh" {
		t.Fatal("refresh token not preserved")
	}
	files, _ := filepath.Glob(filepath.Join(root, "credentials", ".*.tmp"))
	info, _ := os.Stat(path)
	if len(files) != 0 || info.Mode().Perm() != 0600 {
		t.Fatal("temp data/wrong mode")
	}
	path = writeProfileFixture(t, root, "external", []byte(`{"authentication":{"type":"user_oauth"}}`), []byte(`{"access_token":"one"}`), 0600)
	external, err := NewFileProfile(ProfileConfig{Directory: root, Name: "external"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := external.FetchToken(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"access_token":"two"}`), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := external.FetchToken(context.Background(), false)
	if err != nil || first.value != "one" || second.value != "two" {
		t.Fatal("rotation not read", err)
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", profile, token), "new-token") {
		t.Fatal("formatting leaks")
	}
}

func TestFileProfileAdmissionPathsAndExpiry(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"", "../escape", ".hidden", " leading", "trailing ", "a/b", "a\\b", "a\x00b"} {
		if _, err := NewFileProfile(ProfileConfig{Directory: root, Name: name}); err == nil {
			t.Fatal("unsafe name")
		}
	}
	for _, base := range []string{"http://remote.invalid", "http://localhost.evil", "https://user:secret@host", "https://host/?query", "https://host/#fragment"} {
		if credentialEndpoint(base) {
			t.Fatal("unsafe endpoint", base)
		}
	}
	if !credentialEndpoint("http://[::1]:8888") {
		t.Fatal("IPv6 loopback refused")
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "configs")); err != nil {
		t.Fatal(err)
	}
	if _, err := profilePath(root, "configs", "default"); err == nil {
		t.Fatal("symlink ancestor escaped")
	}
	for _, input := range []string{`"2_000_000_000"`, `2000000000.9`, `true`} {
		v, _ := jsonvalue.Decode(input)
		if _, err := profileExpiry(v); err != nil {
			t.Fatal("Python int refused", input)
		}
	}
	for _, input := range []string{`"invalid"`, `{}`, `null`, `1e100`, `253402300800`} {
		v, _ := jsonvalue.Decode(input)
		if _, err := profileExpiry(v); err == nil {
			t.Fatal("invalid expiry accepted", input)
		}
	}
}

func TestFileProfileRefreshCancellationRedirectAndFailureContainment(t *testing.T) {
	reached, release := make(chan struct{}), make(chan struct{})
	var redirected atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/secret-target" {
			redirected.Add(1)
			return
		}
		close(reached)
		<-release
		w.Header().Set("Location", "/secret-target")
		w.WriteHeader(307)
	}))
	defer server.Close()
	root := t.TempDir()
	path := writeProfileFixture(t, root, "default", []byte(fmt.Sprintf(`{"base_url":%q,"authentication":{"type":"user_oauth","client_id":"client"}}`, server.URL)), []byte(`{"access_token":"old-secret","refresh_token":"refresh-secret","expires_at":0}`), 0600)
	before, _ := os.ReadFile(path)
	profile, err := NewFileProfile(ProfileConfig{Directory: root, Name: "default"})
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { _, err := profile.FetchToken(context.Background(), false); finished <- err }()
	<-reached
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := profile.FetchToken(ctx, true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("wait ignored cancellation", err)
	}
	close(release)
	err = <-finished
	var typed *CredentialError
	if !errors.As(err, &typed) || typed.Status != 307 || redirected.Load() != 0 || strings.Contains(err.Error(), "secret") {
		t.Fatal("redirect/error leaked", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Fatal("failed refresh mutated disk")
	}
}

func TestFileProfileRefreshFaultsDoNotReplaceCredentials(t *testing.T) {
	for _, body := range []string{`{`, `[]`, `{}`, `{"access_token":"secret","expires_in":null}`, `{"access_token":"secret","expires_in":"bad"}`, `{"access_token":"secret","expires_in":1e100}`, `{"access_token":"bad\nheader"}`} {
		t.Run(body, func(t *testing.T) {
			root := t.TempDir()
			path := writeProfileFixture(t, root, "default", []byte(`{"authentication":{"type":"user_oauth","client_id":"client"}}`), []byte(`{"access_token":"old","refresh_token":"refresh","expires_at":0}`), 0600)
			before, _ := os.ReadFile(path)
			profile, err := NewFileProfile(ProfileConfig{Directory: root, Name: "default", HTTPClient: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
			})}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = profile.FetchToken(context.Background(), false)
			var typed *CredentialError
			if !errors.As(err, &typed) || typed.Kind != CredentialExchange || strings.Contains(err.Error(), "secret") {
				t.Fatal("invalid exchange not contained", err)
			}
			after, _ := os.ReadFile(path)
			if string(before) != string(after) {
				t.Fatal("bad response replaced credentials")
			}
		})
	}
	root := t.TempDir()
	writeProfileFixture(t, root, "default", []byte(`{"authentication":{"type":"user_oauth","client_id":"client"}}`), []byte(`{"access_token":"old","expires_at":"invalid"}`), 0600)
	profile, err := NewFileProfile(ProfileConfig{Directory: root, Name: "default"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = profile.FetchToken(context.Background(), false)
	var typed *CredentialError
	if !errors.As(err, &typed) || typed.Kind != CredentialExchange {
		t.Fatal("missing refresh must precede invalid expiry", err)
	}
}

func TestFileProfileConstructorAndExplicitOverride(t *testing.T) {
	root := t.TempDir()
	for _, document := range []string{`{`, `[]`, `{}`, `{"authentication":[]}`, `{"authentication":{"type":7}}`} {
		writeProfileFixture(t, root, "default", []byte(document), []byte(`{}`), 0600)
		if _, err := NewFileProfile(ProfileConfig{Directory: root, Name: "default"}); err == nil {
			t.Fatal("malformed configuration accepted")
		}
	}
	outside := t.TempDir()
	path := filepath.Join(outside, "secret.json")
	if err := os.WriteFile(path, []byte(`{"access_token":"external"}`), 0600); err != nil {
		t.Fatal(err)
	}
	writeProfileFixture(t, root, "default", []byte(`{"workspace_id":"","authentication":{"type":"user_oauth","credentials_path":"~/secret.json"}}`), []byte(`{}`), 0600)
	profile, err := NewFileProfile(ProfileConfig{Directory: root, Name: "default", HomeDirectory: outside, DefaultBaseURL: "https://fallback.invalid", DefaultWorkspaceID: "workspace"})
	if err != nil {
		t.Fatal(err)
	}
	token, err := profile.FetchToken(context.Background(), false)
	if err != nil || token.value != "external" || profile.ModelBaseURL() != "https://fallback.invalid" || profile.Headers()[0].Value != "workspace" {
		t.Fatal("override/default precedence failed", err)
	}
}

func TestFileProfileExchangeHonorsContext(t *testing.T) {
	root := t.TempDir()
	writeProfileFixture(t, root, "default", []byte(`{"authentication":{"type":"user_oauth","client_id":"client"}}`), []byte(`{"access_token":"old","refresh_token":"refresh","expires_at":0}`), 0600)
	profile, err := NewFileProfile(ProfileConfig{Directory: root, Name: "default", HTTPClient: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := profile.FetchToken(ctx, false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("exchange ignored context", err)
	}
}
