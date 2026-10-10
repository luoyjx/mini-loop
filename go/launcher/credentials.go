package launcher

import (
	"context"
	"net/http"
	"time"

	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/provider"
)

type capturedIdentityReader struct{ read config.IdentityTokenReader }

func (p capturedIdentityReader) IdentityToken(ctx context.Context) (provider.IdentityAssertion, error) {
	if p.read == nil {
		return provider.IdentityAssertion{}, &provider.CredentialError{Kind: provider.CredentialUnavailable}
	}
	value, present, err := p.read(ctx)
	if err != nil {
		return provider.IdentityAssertion{}, err
	}
	if !present {
		return provider.IdentityAssertion{}, &provider.CredentialError{Kind: provider.CredentialUnavailable}
	}
	return provider.NewIdentityAssertion(value)
}

func modelConfig(ctx context.Context, settings config.Settings, transport http.RoundTripper, now func() time.Time) (provider.Config, error) {
	endpoint := ""
	if settings.BaseURL != nil {
		endpoint = *settings.BaseURL
	}
	selection := settings.ProviderCredentials()
	discovered, err := provider.DiscoverCredentials(ctx, provider.DiscoveryConfig{Now: now, Skip: selection.SkipDiscovery, Directory: selection.Directory, ExplicitDirectory: selection.ExplicitDirectory, Profile: selection.Profile, HomeDirectory: selection.HomeDirectory, BaseURL: endpoint, OrganizationID: selection.OrganizationID, FederationRuleID: selection.FederationRuleID, ServiceAccountID: selection.ServiceAccountID, WorkspaceID: selection.WorkspaceID, Scope: selection.Scope, IdentityFile: selection.IdentityFile, LiteralDefined: selection.LiteralDefined, Identity: capturedIdentityReader{read: selection.IdentityReader}, HTTPClient: &http.Client{Transport: transport, Timeout: provider.FederationTimeout}})
	if err != nil {
		return provider.Config{}, err
	}
	token, configured := settings.ProviderAuthentication()
	headers := append([]provider.Header{}, discovered.Headers...)
	for _, header := range configured {
		headers = append(headers, provider.Header{Name: header.Name, Value: header.Value.Reveal()})
	}
	return provider.Config{Now: now, BaseURL: discovered.BaseURL, Credentials: discovered.Credentials, APIKey: settings.APIKey.Reveal(), AuthToken: token.Reveal(), Headers: headers, HTTPClient: &http.Client{Transport: transport}}, nil
}
