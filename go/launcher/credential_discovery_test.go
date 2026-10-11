package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type credentialTransport func(*http.Request) (*http.Response, error)

func (f credentialTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLauncherDiscoveryMatchesSourceConstructorsAndManagedTurns(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-provider-auth.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Spec struct {
				Name, Profile       string
				Config, Credentials jsonvalue.Value
				Pointer             *string
				IdentityFile        *string `json:"identity_file"`
				RotateIdentity      string  `json:"rotate_identity"`
				AdvanceClock        int64   `json:"advance_clock"`
				RotateLiteral       string  `json:"rotate_literal"`
				UnsetLiteral        bool    `json:"unset_literal"`
				Mode                *uint32
				Symlink             bool
				TokenStatus         *int  `json:"token_status"`
				ModelStatuses       []int `json:"model_statuses"`
				Force               bool  `json:"force_after_first"`
				Reopen              bool  `json:"reopen_after_first"`
				WriteFailure        bool  `json:"write_failure"`
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
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 51 {
		t.Fatal("Source discovery inventory changed")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Spec.Name, func(t *testing.T) {
			if row.Spec.Force {
				t.Skip("same-client forced invalidation is covered by provider tests; Launcher exposes no invalidation handle")
			}
			if runtime.GOOS == "windows" && (row.Spec.Mode != nil || row.Spec.Symlink) {
				t.Skip("Source POSIX file guard")
			}
			root := t.TempDir()
			caseRoot := filepath.Join(root, row.Spec.Name)
			home := filepath.Join(caseRoot, "home")
			directory := filepath.Join(home, ".config", "anthropic")
			for _, part := range []string{"configs", "credentials"} {
				if err := os.MkdirAll(filepath.Join(directory, part), 0700); err != nil {
					t.Fatal(err)
				}
			}
			profile := row.Spec.Profile
			if profile == "" {
				profile = "default"
			}
			identityPath := filepath.Join(caseRoot, "identity.jwt")
			parentFile := filepath.Join(caseRoot, "cache-parent-file")
			if row.Spec.Config.Kind() != jsonvalue.Null {
				wire, err := row.Spec.Config.MarshalJSON()
				if err != nil {
					t.Fatal(err)
				}
				if text, ok := row.Spec.Config.Text(); ok {
					wire = []byte(text)
				}
				text := strings.ReplaceAll(string(wire), "<identity-file>", identityPath)
				text = strings.ReplaceAll(text, "<cache-parent-file>", parentFile)
				if err := os.WriteFile(filepath.Join(directory, "configs", profile+".json"), []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cache := filepath.Join(directory, "credentials", profile+".json")
			if row.Spec.Credentials.Kind() != jsonvalue.Null {
				wire, _ := row.Spec.Credentials.MarshalJSON()
				mode := os.FileMode(0600)
				if row.Spec.Mode != nil {
					mode = os.FileMode(*row.Spec.Mode)
				}
				target := cache
				if row.Spec.Symlink {
					target = filepath.Join(filepath.Dir(cache), "secret-target.json")
				}
				if err := os.WriteFile(target, wire, mode); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(target, mode); err != nil {
					t.Fatal(err)
				}
				if row.Spec.Symlink {
					if err := os.Symlink(target, cache); err != nil {
						t.Fatal(err)
					}
				}
			}
			if row.Spec.Pointer != nil {
				if err := os.WriteFile(filepath.Join(directory, "active_config"), []byte(*row.Spec.Pointer), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if row.Spec.IdentityFile != nil {
				if err := os.WriteFile(identityPath, []byte(*row.Spec.IdentityFile), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if row.Spec.WriteFailure {
				if err := os.WriteFile(parentFile, []byte("not a directory"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			env := map[string]string{"MINILOOP_FAKE_LLM": "0", "MINILOOP_TRAJECTORIES": "0", "MINILOOP_SPILL_DIR": ""}
			for name, value := range row.Environment {
				env[name] = strings.ReplaceAll(value, "<scratch>", root)
			}
			loadOptions := config.LoadOptions{Directory: caseRoot, HomeDirectory: home}
			literal, literalPresent := env["ANTHROPIC_IDENTITY_TOKEN"]
			if row.Spec.RotateLiteral != "" || row.Spec.UnsetLiteral {
				loadOptions.IdentityTokenReader = func(ctx context.Context) (string, bool, error) {
					if err := ctx.Err(); err != nil {
						return "", false, err
					}
					return literal, literalPresent, nil
				}
			}
			settings, err := config.Load(env, loadOptions)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Unix(1700000000, 0)
			requests, exchanges := 0, 0
			upstream := credentialTransport(func(r *http.Request) (*http.Response, error) {
				status := 200
				body := `{"id":"fixture","type":"message","role":"assistant","model":"fixture-model","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
				if strings.HasSuffix(r.URL.Path, "/oauth/token") {
					if exchanges >= len(row.Exchanges) {
						t.Fatal("unexpected token exchange")
					}
					expected := row.Exchanges[exchanges]
					exchanges++
					wire, _ := io.ReadAll(r.Body)
					value, err := jsonvalue.Decode(string(wire))
					if err != nil || !value.PythonEqual(expected.Body) || r.URL.String() != expected.URL || r.Header.Get("Anthropic-Beta") != expected.Beta {
						t.Fatal("discovered Source exchange differs")
					}
					body = `{"access_token":"exchanged-token","token_type":"Bearer","expires_in":3600}`
					if row.Spec.TokenStatus != nil {
						status = *row.Spec.TokenStatus
						body = `{"error":"invalid_grant","access_token":"must-redact"}`
					}
				} else {
					if requests >= len(row.Requests) {
						t.Fatal("unexpected model request")
					}
					expected := row.Requests[requests]
					requests++
					headers := map[string]string{}
					for _, name := range []string{"x-api-key", "authorization", "anthropic-workspace-id", "anthropic-beta"} {
						if values, ok := r.Header[http.CanonicalHeaderKey(name)]; ok {
							headers[name] = values[0]
						}
					}
					if !reflect.DeepEqual(headers, expected.Headers) || r.URL.String() != expected.URL {
						t.Fatal("discovered Source model headers/URL differ", headers, r.URL.String())
					}
					if len(row.Spec.ModelStatuses) > 0 {
						status = row.Spec.ModelStatuses[requests-1]
					}
					if status != 200 {
						body = `{"error":{"type":"authentication_error","message":"revoked fixture token"}}`
					}
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			options := Options{ProviderTransport: upstream, ProviderNow: func() time.Time { return now }}
			app, err := NewWithOptions(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil, options)
			if (err == nil) != row.Constructed {
				t.Fatal("Source constructor phase differs", err)
			}
			if err != nil {
				return
			}
			defer func() { stopApp(t, app) }()
			if requests != 0 || exchanges != 0 {
				t.Fatal("constructor contacted provider")
			}
			if snapshot := settings.Snapshot().Settings.ProviderCredentials(); snapshot.Literal.Present() || snapshot.IdentityReader != nil {
				t.Fatal("snapshot retained live credentials")
			}
			if strings.Contains(fmt.Sprintf("%#v", settings), "fixture.jwt.assertion") {
				t.Fatal("settings formatting exposed identity")
			}
			var answer string
			for turn := 0; turn < 2; turn++ {
				if turn == 1 && row.Spec.Reopen {
					stopApp(t, app)
					app, err = NewWithOptions(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil, options)
					if err != nil {
						t.Fatal(err)
					}
				}
				if turn == 1 && row.Spec.AdvanceClock != 0 {
					now = now.Add(time.Duration(row.Spec.AdvanceClock) * time.Second)
					if row.Spec.RotateLiteral != "" {
						literal = row.Spec.RotateLiteral
					}
					if row.Spec.UnsetLiteral {
						literalPresent = false
					}
					if row.Spec.RotateIdentity != "" {
						if err := os.WriteFile(identityPath, []byte(row.Spec.RotateIdentity), 0644); err != nil {
							t.Fatal(err)
						}
					}
				}
				handle, err := app.manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "fixture-owner"})
				if err != nil {
					t.Fatal(err)
				}
				answer, err = handle.Run(context.Background(), "hello")
				if err != nil {
					t.Fatal(err)
				}
				if strings.HasPrefix(answer, "[Error] ") {
					break
				}
				if answer != "ok" {
					t.Fatal("managed Source reply differs", answer)
				}
			}
			if (row.Fault != nil) != strings.HasPrefix(answer, "[Error] ") {
				t.Fatal("Source request success/error phase differs", answer)
			}
			if requests != len(row.Requests) || exchanges != len(row.Exchanges) {
				t.Fatal("Source discovery calls differ", requests, exchanges)
			}
			if row.Mode != nil {
				data, err := os.ReadFile(cache)
				if err != nil {
					t.Fatal(err)
				}
				value, err := jsonvalue.Decode(string(data))
				info, _ := os.Stat(cache)
				if err != nil || !value.PythonEqual(row.After) || uint32(info.Mode().Perm()) != *row.Mode {
					t.Fatal("Source discovered cache/mode differs")
				}
			}
		})
	}
}
