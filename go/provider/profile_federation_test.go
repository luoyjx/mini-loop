package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestFederationProfilesMatchSourceDiskCacheRecipes(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-provider-auth.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Spec struct {
				Name                string
				Config, Credentials json.RawMessage
				IdentityFile        string `json:"identity_file"`
				Force               bool   `json:"force_after_first"`
				Reopen              bool   `json:"reopen_after_first"`
				WriteFailure        bool   `json:"write_failure"`
				Mode                *uint32
				Symlink             bool
			}
			Environment map[string]string
			Constructed bool
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
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	tested := 0
	for _, row := range fixture.Cases {
		if !strings.HasPrefix(row.Spec.Name, "profile-federation-") {
			continue
		}
		tested++
		t.Run(row.Spec.Name, func(t *testing.T) {
			if runtime.GOOS == "windows" && (row.Spec.Mode != nil || row.Spec.Symlink) {
				t.Skip("Source POSIX private-file guard")
			}
			root := t.TempDir()
			directory := filepath.Join(root, "profiles")
			identityPath := filepath.Join(root, "identity.jwt")
			parentFile := filepath.Join(root, "cache-parent-file")
			if err := os.WriteFile(identityPath, []byte(row.Spec.IdentityFile), 0644); err != nil {
				t.Fatal(err)
			}
			config := strings.ReplaceAll(string(row.Spec.Config), "<identity-file>", identityPath)
			config = strings.ReplaceAll(config, "<cache-parent-file>", parentFile)
			mode := os.FileMode(0600)
			if row.Spec.Mode != nil {
				mode = os.FileMode(*row.Spec.Mode)
			}
			credentials := row.Spec.Credentials
			if len(credentials) == 0 {
				credentials = []byte(`{}`)
			}
			path := writeProfileFixture(t, directory, "default", []byte(config), credentials, mode)
			if len(row.Spec.Credentials) == 0 {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			if row.Spec.Symlink {
				target := filepath.Join(filepath.Dir(path), "secret-target.json")
				if err := os.Rename(path, target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			}
			if row.Spec.WriteFailure {
				if err := os.WriteFile(parentFile, []byte("not a directory"), 0600); err != nil {
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
					if err != nil || !value.PythonEqual(expected.Body) || r.URL.String() != expected.URL || r.Header.Get("Anthropic-Beta") != expected.Beta {
						t.Fatal("Source profile exchange differs")
					}
					body = `{"access_token":"exchanged-token","expires_in":3600,"token_type":"Bearer"}`
				} else {
					if requests >= len(row.Requests) {
						t.Fatal("unexpected model request")
					}
					expected := row.Requests[requests]
					requests++
					headers := map[string]string{}
					for _, key := range []string{"authorization", "anthropic-beta", "anthropic-workspace-id", "x-api-key"} {
						if value := r.Header.Get(key); value != "" {
							headers[key] = value
						}
					}
					if r.URL.String() != expected.URL || !reflect.DeepEqual(headers, expected.Headers) {
						t.Fatal("Source profile model auth differs", headers)
					}
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			clock := func() time.Time { return time.Unix(1700000000, 0) }
			env := row.Environment
			cfg := ProfileConfig{Directory: directory, Name: "default", DefaultOrganizationID: env["ANTHROPIC_ORGANIZATION_ID"], DefaultFederationRuleID: env["ANTHROPIC_FEDERATION_RULE_ID"], DefaultWorkspaceID: env["ANTHROPIC_WORKSPACE_ID"], DefaultServiceAccountID: env["ANTHROPIC_SERVICE_ACCOUNT_ID"], DefaultScope: env["ANTHROPIC_SCOPE"], DefaultIdentityTokenFile: identityPath, HTTPClient: httpClient, Now: clock}
			build := func() (*Client, error) {
				profile, err := NewFileProfile(cfg)
				if err != nil {
					return nil, err
				}
				return New(Config{BaseURL: profile.ModelBaseURL(), Headers: profile.Headers(), Credentials: profile, HTTPClient: httpClient, Now: clock})
			}
			client, err := build()
			if (err == nil) != row.Constructed {
				t.Fatal("Source profile construction phase differs", err)
			}
			if err != nil {
				return
			}
			var failure error
			for turn := 0; turn < 2; turn++ {
				if turn == 1 && row.Spec.Force {
					client.InvalidateCredentials()
				}
				if turn == 1 && row.Spec.Reopen {
					client, err = build()
					if err != nil {
						t.Fatal(err)
					}
				}
				_, failure = client.Complete(context.Background(), minimalRequest())
				if failure != nil {
					break
				}
			}
			if row.Fault != nil {
				var typed *Failure
				if !errors.As(failure, &typed) || string(typed.Class()) != row.Fault.Type {
					t.Fatal("Source profile error phase/class differs", failure)
				}
			} else if failure != nil {
				t.Fatal(failure)
			}
			if requests != len(row.Requests) || exchanges != len(row.Exchanges) {
				t.Fatal("Source profile cache calls differ", requests, exchanges)
			}
			if row.Mode != nil {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				after, err := jsonvalue.Decode(string(data))
				if err != nil || !after.PythonEqual(row.After) || uint32(info.Mode().Perm()) != *row.Mode {
					t.Fatal("Source cache preservation/mode differs")
				}
			} else if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("Source absent cache unexpectedly written", err)
			}
		})
	}
	if tested != 17 {
		t.Fatal("federation profile Source inventory changed", tested)
	}
}

func TestProfileDanglingSymlinkContainmentAndSourceTimeout(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "credentials"), 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "credentials", "default.json")
	if err := os.Symlink(filepath.Join(outside, "missing.json"), link); err != nil {
		t.Fatal(err)
	}
	if _, err := profilePath(root, "credentials", "default"); err == nil {
		t.Fatal("dangling symlink escaped root containment")
	}
	root = t.TempDir()
	writeProfileFixture(t, root, "default", []byte(`{"authentication":{"type":"user_oauth"}}`), []byte(`{"access_token":"token"}`), 0600)
	profile, err := NewFileProfile(ProfileConfig{Directory: root, Name: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if profile.http.Timeout != 30*time.Second {
		t.Fatal("Source credential HTTP timeout differs", profile.http.Timeout)
	}
}
