package config

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
)

// IdentityTokenReader is an explicit rotating environment boundary. Other
// discovery fields are captured once. Snapshot/JSON discard this live reader.
type IdentityTokenReader func(context.Context) (string, bool, error)

func ReadProcessIdentityToken(ctx context.Context) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	value, present := os.LookupEnv("ANTHROPIC_IDENTITY_TOKEN")
	return value, present, nil
}

// ProviderCredentialSettings carries named operator selection, never a raw env
// map. Directory/profile discovery happens later in Launcher construction.
type ProviderCredentialSettings struct {
	SkipDiscovery     bool
	Directory         string
	ExplicitDirectory bool
	Profile           string
	HomeDirectory     string
	OrganizationID    string
	FederationRuleID  string
	ServiceAccountID  *string
	WorkspaceID       *string
	Scope             *string
	IdentityFile      string
	LiteralDefined    bool
	Literal           Secret              `json:"-"`
	IdentityReader    IdentityTokenReader `json:"-"`
}

func (ProviderCredentialSettings) String() string   { return "<provider credential selection>" }
func (ProviderCredentialSettings) GoString() string { return "<provider credential selection>" }

func (s Settings) ProviderCredentials() ProviderCredentialSettings {
	result := s.providerCredentials
	result.SkipDiscovery = result.SkipDiscovery || s.apiKeyDefined || s.authTokenDefined || s.APIKey.Present() || s.AuthToken.Present()
	result.ServiceAccountID = clone(result.ServiceAccountID)
	result.WorkspaceID = clone(result.WorkspaceID)
	result.Scope = clone(result.Scope)
	return result
}

func captureProviderCredentials(get func(string) (string, bool), options LoadOptions, ambient bool) ProviderCredentialSettings {
	read := func(name string) string { value, _ := get(name); return value }
	selection := ProviderCredentialSettings{Profile: read("ANTHROPIC_PROFILE"), OrganizationID: read("ANTHROPIC_ORGANIZATION_ID"), FederationRuleID: read("ANTHROPIC_FEDERATION_RULE_ID"), IdentityFile: read("ANTHROPIC_IDENTITY_TOKEN_FILE")}
	optional := func(name string) *string {
		value, present := get(name)
		if !present {
			return nil
		}
		return &value
	}
	selection.ServiceAccountID = optional("ANTHROPIC_SERVICE_ACCOUNT_ID")
	selection.WorkspaceID = optional("ANTHROPIC_WORKSPACE_ID")
	selection.Scope = optional("ANTHROPIC_SCOPE")
	literal, present := get("ANTHROPIC_IDENTITY_TOKEN")
	selection.Literal, selection.LiteralDefined = Secret{literal}, present
	selection.IdentityReader = options.IdentityTokenReader
	if selection.IdentityReader == nil {
		if ambient {
			selection.IdentityReader = ReadProcessIdentityToken
		} else {
			selection.IdentityReader = func(ctx context.Context) (string, bool, error) {
				if err := ctx.Err(); err != nil {
					return "", false, err
				}
				return literal, present, nil
			}
		}
	}
	home := options.HomeDirectory
	if home == "" {
		home = read("HOME")
	}
	if home == "" {
		home = read("USERPROFILE")
	}
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	selection.HomeDirectory = home
	directory := read("ANTHROPIC_CONFIG_DIR")
	selection.ExplicitDirectory = directory != ""
	if directory == "" && home != "" {
		if runtime.GOOS == "windows" {
			base := read("APPDATA")
			if base == "" {
				base = filepath.Join(home, "AppData", "Roaming")
			}
			directory = filepath.Join(base, "Anthropic")
		} else {
			directory = filepath.Join(home, ".config", "anthropic")
		}
	}
	if directory != "" && !filepath.IsAbs(directory) {
		directory = filepath.Join(options.Directory, directory)
	}
	selection.Directory = directory
	return selection
}
