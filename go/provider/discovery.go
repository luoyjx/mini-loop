package provider

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

// DiscoveryConfig supplies detached selectors and one explicitly owned rotating
// identity reader. It does not read global environment variables.
type DiscoveryConfig struct {
	Skip              bool
	Directory         string
	ExplicitDirectory bool
	Profile           string
	HomeDirectory     string
	BaseURL           string
	OrganizationID    string
	FederationRuleID  string
	ServiceAccountID  *string
	WorkspaceID       *string
	Scope             *string
	IdentityFile      string
	LiteralDefined    bool
	Identity          IdentityTokenProvider `json:"-"`
	HTTPClient        *http.Client          `json:"-"`
	Now               func() time.Time      `json:"-"`
}

type CredentialSelection struct {
	Credentials CredentialProvider `json:"-"`
	BaseURL     string
	Headers     []Header
}

func (CredentialSelection) String() string   { return "<credential selection>" }
func (CredentialSelection) GoString() string { return "<credential selection>" }

func DiscoverCredentials(ctx context.Context, cfg DiscoveryConfig) (CredentialSelection, error) {
	result := CredentialSelection{BaseURL: cfg.BaseURL}
	if cfg.Skip {
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	pointer, err := readActiveProfile(cfg.Directory)
	if err != nil {
		return result, err
	}
	profile := cfg.Profile
	if profile == "" {
		profile = pointer
	}
	if profile == "" {
		profile = "default"
	}
	explicit := cfg.Profile != "" || cfg.ExplicitDirectory || pointer != ""
	bound := cfg.BaseURL
	if bound == "" {
		bound = DefaultEndpoint
	}
	buildProfile := func() (CredentialSelection, error) {
		// SDK bind validates the model deployment before loading profile fields.
		if !credentialEndpoint(bound) {
			return result, profileError(CredentialInvalid)
		}
		value := func(v *string) string {
			if v == nil {
				return ""
			}
			return *v
		}
		file, err := NewFileProfile(ProfileConfig{Directory: cfg.Directory, Name: profile, HomeDirectory: cfg.HomeDirectory, BaseURL: cfg.BaseURL, DefaultOrganizationID: cfg.OrganizationID, DefaultFederationRuleID: cfg.FederationRuleID, DefaultWorkspaceID: value(cfg.WorkspaceID), DefaultServiceAccountID: value(cfg.ServiceAccountID), DefaultScope: value(cfg.Scope), DefaultIdentityTokenFile: cfg.IdentityFile, HTTPClient: cfg.HTTPClient, Now: cfg.Now})
		if err != nil {
			return result, err
		}
		return CredentialSelection{Credentials: file, BaseURL: file.ModelBaseURL(), Headers: file.Headers()}, nil
	}
	if explicit {
		return buildProfile()
	}
	if cfg.FederationRuleID != "" && cfg.OrganizationID != "" && (cfg.IdentityFile != "" || cfg.LiteralDefined) {
		identity := cfg.Identity
		if cfg.IdentityFile != "" {
			identity, err = NewIdentityTokenFile(cfg.IdentityFile)
			if err != nil {
				return result, err
			}
		}
		workspace := cfg.WorkspaceID
		if workspace != nil && *workspace == "" {
			workspace = nil
		}
		federation, err := NewFederation(FederationConfig{Identity: identity, BaseURL: bound, FederationRuleID: cfg.FederationRuleID, OrganizationID: cfg.OrganizationID, ServiceAccountID: cfg.ServiceAccountID, WorkspaceID: workspace, Scope: cfg.Scope, HTTPClient: cfg.HTTPClient, Now: cfg.Now})
		if err != nil {
			return result, err
		}
		result.Credentials = federation
		return result, nil
	}
	if cfg.Directory == "" {
		return result, nil
	}
	path, err := profilePath(cfg.Directory, "configs", profile)
	if err != nil {
		return result, nil
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return result, nil
	}
	// A bad explicit model binding is eager even for an unselected disk profile.
	if !credentialEndpoint(bound) {
		return result, profileError(CredentialInvalid)
	}
	selected, err := buildProfile()
	if err != nil {
		return result, nil
	}
	return selected, nil
}

func readActiveProfile(directory string) (string, error) {
	if directory == "" {
		return "", nil
	}
	file, err := os.Open(filepath.Join(directory, "active_config"))
	if err != nil {
		return "", nil
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", nil
	}
	data, err := io.ReadAll(io.LimitReader(file, DefaultWireLimit+1))
	if err != nil {
		return "", nil
	}
	if len(data) > DefaultWireLimit || !utf8.Valid(data) {
		return "", profileError(CredentialInvalid)
	}
	return strings.TrimFunc(string(data), pytext.Space), nil
}
