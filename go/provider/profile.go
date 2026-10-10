package provider

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// ProfileConfig is a detached operator selection. It never reads environment
// variables. BaseURL binds model requests; a profile base_url owns token exchange.
type ProfileConfig struct {
	Directory          string
	Name               string
	HomeDirectory      string
	BaseURL            string
	DefaultBaseURL     string
	DefaultWorkspaceID string
	HTTPClient         *http.Client     `json:"-"`
	Now                func() time.Time `json:"-"`
}

// FileProfile eagerly validates configuration, and reads credentials only on
// FetchToken. One instance serializes refreshes; different instances remain
// independent last-writer-wins caches, as in Source.
type FileProfile struct {
	gate            chan struct{}
	kind            string
	clientID        string
	credentialsPath string
	tokenBaseURL    string
	modelBaseURL    string
	workspaceID     string
	http            *http.Client
	now             func() time.Time
}

func (*FileProfile) String() string         { return "<file profile>" }
func (*FileProfile) GoString() string       { return "<file profile>" }
func (p *FileProfile) ModelBaseURL() string { return p.modelBaseURL }
func (p *FileProfile) Headers() []Header {
	if p.workspaceID == "" || p.kind == "oidc_federation" {
		return nil
	}
	return []Header{{Name: "anthropic-workspace-id", Value: p.workspaceID}}
}

func profileError(kind CredentialErrorKind) error { return &CredentialError{Kind: kind} }

func NewFileProfile(cfg ProfileConfig) (*FileProfile, error) {
	if cfg.Name == "" || strings.TrimSpace(cfg.Name) != cfg.Name || strings.HasPrefix(cfg.Name, ".") || strings.ContainsAny(cfg.Name, "/\\\x00") || cfg.Directory == "" {
		return nil, profileError(CredentialInvalid)
	}
	path, err := profilePath(cfg.Directory, "configs", cfg.Name)
	if err != nil {
		return nil, err
	}
	raw, err := readProfileJSON(path, false)
	if err != nil {
		return nil, err
	}
	auth, ok := raw.Lookup("authentication")
	if !ok || auth.Kind() != jsonvalue.Object {
		return nil, profileError(CredentialInvalid)
	}
	// Named fields leave the ingress JSON behind; arbitrary config members never
	// enter the provider service. Non-string auth values are rejected explicitly.
	kind, err := profileText(auth, "type")
	if err != nil {
		return nil, err
	}
	clientID, err := profileText(auth, "client_id")
	if err != nil {
		return nil, err
	}
	override, err := profileText(auth, "credentials_path")
	if err != nil {
		return nil, err
	}
	base, err := profileText(raw, "base_url")
	if err != nil {
		return nil, err
	}
	workspace, err := profileText(raw, "workspace_id")
	if err != nil {
		return nil, err
	}
	if base == "" {
		base = cfg.DefaultBaseURL
	}
	if base == "" {
		base = cfg.BaseURL
	}
	if base == "" {
		base = DefaultEndpoint
	}
	base = strings.TrimRight(base, "/")
	if !credentialEndpoint(base) {
		return nil, profileError(CredentialInvalid)
	}
	modelBase := cfg.BaseURL
	if modelBase == "" {
		modelBase = base
	}
	if workspace == "" {
		workspace = cfg.DefaultWorkspaceID
	}
	if err := validateAuth("", []Header{{Name: "anthropic-workspace-id", Value: workspace}}); err != nil {
		return nil, profileError(CredentialInvalid)
	}
	var credentialPath string
	if override != "" {
		// Source explicitly permits paths outside the config root for this override.
		credentialPath = override
		if override == "~" || strings.HasPrefix(override, "~/") {
			if cfg.HomeDirectory == "" {
				return nil, profileError(CredentialInvalid)
			}
			credentialPath = filepath.Join(cfg.HomeDirectory, strings.TrimPrefix(strings.TrimPrefix(override, "~"), "/"))
		}
	} else {
		credentialPath, err = profilePath(cfg.Directory, "credentials", cfg.Name)
		if err != nil {
			return nil, err
		}
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	detached := *client
	// Token secrets must not follow redirects, including same-origin redirects.
	detached.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &FileProfile{gate: make(chan struct{}, 1), kind: kind, clientID: clientID, credentialsPath: credentialPath, tokenBaseURL: base, modelBaseURL: modelBase, workspaceID: workspace, http: &detached, now: now}, nil
}

func credentialEndpoint(base string) bool {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	return u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
}

// resolveProfilePath resolves existing ancestors too, preserving containment
// checks when the final credential file does not exist yet.
func resolveProfilePath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	parent := filepath.Dir(absolute)
	if parent == absolute {
		return "", err
	}
	resolved, err = resolveProfilePath(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, filepath.Base(absolute)), nil
}

func profilePath(root, group, name string) (string, error) {
	path := filepath.Join(root, group, name+".json")
	base, err := resolveProfilePath(root)
	if err != nil {
		return "", profileError(CredentialUnavailable)
	}
	target, err := resolveProfilePath(path)
	if err != nil {
		return "", profileError(CredentialUnavailable)
	}
	relative, err := filepath.Rel(base, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", profileError(CredentialInvalid)
	}
	return path, nil
}

func readProfileJSON(path string, secret bool) (jsonvalue.Value, error) {
	var before os.FileInfo
	if secret && runtime.GOOS != "windows" {
		var err error
		before, err = os.Lstat(path)
		if err != nil {
			return jsonvalue.Value{}, profileError(CredentialUnavailable)
		}
		if !before.Mode().IsRegular() || before.Mode().Perm()&0004 != 0 {
			return jsonvalue.Value{}, profileError(CredentialInvalid)
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return jsonvalue.Value{}, profileError(CredentialUnavailable)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return jsonvalue.Value{}, profileError(CredentialUnavailable)
	}
	if before != nil && (!os.SameFile(before, info) || info.Mode().Perm()&0004 != 0) {
		return jsonvalue.Value{}, profileError(CredentialInvalid)
	}
	data, err := io.ReadAll(io.LimitReader(f, DefaultWireLimit+1))
	if err != nil {
		return jsonvalue.Value{}, profileError(CredentialUnavailable)
	}
	if len(data) > DefaultWireLimit || !utf8.Valid(data) {
		return jsonvalue.Value{}, profileError(CredentialInvalid)
	}
	v, err := jsonvalue.Decode(string(data))
	if err != nil || v.Kind() != jsonvalue.Object {
		return jsonvalue.Value{}, profileError(CredentialInvalid)
	}
	return v, nil
}

func profileText(v jsonvalue.Value, name string) (string, error) {
	field, present := v.Lookup(name)
	if !present || !field.Truth() {
		return "", nil
	}
	text, ok := field.Text()
	if !ok {
		return "", profileError(CredentialInvalid)
	}
	return text, nil
}

type profileCredentials struct {
	access     string
	refresh    string
	expiry     time.Time
	expires    bool
	extensions []jsonvalue.Field
}

func profileExpiry(v jsonvalue.Value) (time.Time, error) {
	integer, err := v.PythonInteger()
	// Native time values retain a precise civil-time range. Invalid/out-of-range
	// input is refused instead of wrapping an expiry into a misleading cache TTL.
	if err != nil || !integer.IsInt64() {
		return time.Time{}, profileError(CredentialInvalid)
	}
	seconds := integer.Int64()
	if seconds < -62135596800 || seconds > 253402300799 {
		return time.Time{}, profileError(CredentialInvalid)
	}
	return time.Unix(seconds, 0), nil
}

func (p *FileProfile) readCredentials() (profileCredentials, error) {
	raw, err := readProfileJSON(p.credentialsPath, true)
	if err != nil {
		return profileCredentials{}, err
	}
	if discriminator, ok := raw.Lookup("type"); ok && discriminator.Kind() != jsonvalue.Null {
		text, isText := discriminator.Text()
		if !isText || text != "oauth_token" {
			return profileCredentials{}, profileError(CredentialInvalid)
		}
	}
	access, err := profileText(raw, "access_token")
	if err != nil || access == "" {
		return profileCredentials{}, profileError(CredentialInvalid)
	}
	refresh := ""
	if p.clientID != "" {
		refresh, err = profileText(raw, "refresh_token")
		if err != nil || refresh == "" {
			return profileCredentials{}, profileError(CredentialExchange)
		}
	}
	c := profileCredentials{access: access, refresh: refresh}
	if expiry, ok := raw.Lookup("expires_at"); ok && expiry.Kind() != jsonvalue.Null {
		c.expiry, err = profileExpiry(expiry)
		if err != nil {
			return profileCredentials{}, err
		}
		c.expires = true
	}
	for _, key := range raw.Keys() {
		switch key {
		case "access_token", "refresh_token", "expires_at", "type", "version":
			continue
		}
		value, _ := raw.Lookup(key)
		c.extensions = append(c.extensions, jsonvalue.Field{Name: key, Value: value})
	}
	return c, nil
}

func (p *FileProfile) FetchToken(ctx context.Context, force bool) (AccessToken, error) {
	select {
	case p.gate <- struct{}{}:
		defer func() { <-p.gate }()
	case <-ctx.Done():
		return AccessToken{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return AccessToken{}, err
	}
	if p.kind != "user_oauth" {
		return AccessToken{}, profileError(CredentialInvalid)
	}
	c, err := p.readCredentials()
	if err != nil {
		return AccessToken{}, err
	}
	if p.clientID == "" || (!force && c.expires && p.now().Before(c.expiry)) {
		var expiry *time.Time
		if c.expires {
			expiry = &c.expiry
		}
		token, err := NewAccessToken(c.access, expiry)
		if err != nil {
			return AccessToken{}, profileError(CredentialInvalid)
		}
		return token, nil
	}
	return p.refresh(ctx, c)
}
