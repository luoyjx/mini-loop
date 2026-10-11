package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

const FederationAPIBeta = "oidc-federation-2026-04-01"
const MaxFederationResponseBytes = 1 << 20
const FederationTimeout = 30 * time.Second

// FederationConfig binds a detached identity source to one exchange deployment.
// Optional strings distinguish absent from explicitly empty request fields.
// Scope is informational; the federation rule determines the effective scope.
type FederationConfig struct {
	Identity         IdentityTokenProvider `json:"-"`
	BaseURL          string
	FederationRuleID string
	OrganizationID   string
	ServiceAccountID *string
	WorkspaceID      *string
	Scope            *string
	HTTPClient       *http.Client     `json:"-"`
	Now              func() time.Time `json:"-"`
}

type Federation struct {
	identity         IdentityTokenProvider
	endpoint         string
	ruleID           string
	organizationID   string
	serviceAccountID *string
	workspaceID      *string
	scope            *string
	http             *http.Client
	now              func() time.Time
}

func (*Federation) String() string         { return "<federation credentials>" }
func (*Federation) GoString() string       { return "<federation credentials>" }
func (p *Federation) ModelBaseURL() string { return p.endpoint }
func (p *Federation) Scope() *string       { return copyFederationString(p.scope) }
func copyFederationString(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func NewFederation(cfg FederationConfig) (*Federation, error) {
	if cfg.Identity == nil {
		return nil, profileError(CredentialInvalid)
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = DefaultEndpoint
	}
	if !credentialEndpoint(base) {
		return nil, profileError(CredentialInvalid)
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: FederationTimeout}
	}
	detached := *client
	detached.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Federation{identity: cfg.Identity, endpoint: base, ruleID: cfg.FederationRuleID, organizationID: cfg.OrganizationID, serviceAccountID: copyFederationString(cfg.ServiceAccountID), workspaceID: copyFederationString(cfg.WorkspaceID), scope: copyFederationString(cfg.Scope), http: &detached, now: now}, nil
}

type federationRequest struct {
	GrantType        string  `json:"grant_type"`
	Assertion        string  `json:"assertion"`
	FederationRuleID string  `json:"federation_rule_id"`
	OrganizationID   string  `json:"organization_id"`
	ServiceAccountID *string `json:"service_account_id,omitempty"`
	WorkspaceID      *string `json:"workspace_id,omitempty"`
}

// FetchToken performs a fresh exchange. Client's credentialCache owns reuse and
// force policy; this driver re-invokes the identity source even on forced calls.
func (p *Federation) FetchToken(ctx context.Context, _ bool) (AccessToken, error) {
	if err := ctx.Err(); err != nil {
		return AccessToken{}, err
	}
	assertion, err := p.identityToken(ctx)
	if err != nil {
		return AccessToken{}, err
	}
	if err := ctx.Err(); err != nil {
		return AccessToken{}, err
	}
	if !utf8.ValidString(assertion.value) || len(assertion.value) > MaxIdentityAssertionBytes {
		return AccessToken{}, profileError(CredentialExchange)
	}
	body, err := json.Marshal(federationRequest{GrantType: "urn:ietf:params:oauth:grant-type:jwt-bearer", Assertion: assertion.value, FederationRuleID: p.ruleID, OrganizationID: p.organizationID, ServiceAccountID: p.serviceAccountID, WorkspaceID: p.workspaceID})
	if err != nil {
		return AccessToken{}, profileError(CredentialExchange)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint+"/v1/oauth/token", strings.NewReader(string(body)))
	if err != nil {
		return AccessToken{}, profileError(CredentialExchange)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Anthropic-Beta", OAuthAPIBeta+","+FederationAPIBeta)
	resp, err := p.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return AccessToken{}, ctx.Err()
		}
		return AccessToken{}, profileError(CredentialExchange)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxFederationResponseBytes+1))
	if ctx.Err() != nil {
		return AccessToken{}, ctx.Err()
	}
	failure := &CredentialError{Kind: CredentialExchange, Status: resp.StatusCode}
	if err != nil || len(data) > MaxFederationResponseBytes || !utf8.Valid(data) || resp.StatusCode >= 400 {
		return AccessToken{}, failure
	}
	payload, err := jsonvalue.Decode(string(data))
	if err != nil || payload.Kind() != jsonvalue.Object {
		return AccessToken{}, failure
	}
	if tokenType, present := payload.Lookup("token_type"); present && tokenType.Kind() != jsonvalue.Null {
		if !strings.EqualFold(tokenType.PythonString(), "bearer") {
			return AccessToken{}, failure
		}
	}
	access, err := profileText(payload, "access_token")
	if err != nil || access == "" {
		return AccessToken{}, failure
	}
	duration, present := payload.Lookup("expires_in")
	if !present {
		return AccessToken{}, failure
	}
	seconds, err := duration.PythonInteger()
	if err != nil {
		return AccessToken{}, failure
	}
	nowValue := jsonvalue.IntegerValue(p.now().Unix())
	nowSeconds, _ := nowValue.PythonInteger()
	seconds.Add(seconds, nowSeconds)
	absolute, err := jsonvalue.Decode(seconds.String())
	if err != nil {
		return AccessToken{}, failure
	}
	expiry, err := profileExpiry(absolute)
	if err != nil {
		return AccessToken{}, failure
	}
	token, err := NewAccessToken(access, &expiry)
	if err != nil {
		return AccessToken{}, failure
	}
	return token, nil
}

// Keep caller-owned identity callback errors/panics out of public diagnostics.
func (p *Federation) identityToken(ctx context.Context) (assertion IdentityAssertion, err error) {
	defer func() {
		if recover() != nil {
			assertion = IdentityAssertion{}
			err = profileError(CredentialCallback)
		}
	}()
	assertion, err = p.identity.IdentityToken(ctx)
	if err == nil {
		return assertion, nil
	}
	if ctx.Err() != nil {
		return IdentityAssertion{}, ctx.Err()
	}
	if errors.Is(err, context.Canceled) {
		return IdentityAssertion{}, context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return IdentityAssertion{}, context.DeadlineExceeded
	}
	var classified *CredentialError
	if errors.As(err, &classified) {
		return IdentityAssertion{}, &CredentialError{Kind: classified.Kind, Status: classified.Status}
	}
	return IdentityAssertion{}, profileError(CredentialCallback)
}
