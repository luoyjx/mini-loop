package provider

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// Profile federation selection retains named fields only; malformed selection
// is deferred to the request, before inspecting any apparently fresh disk token.
type profileFederationSettings struct {
	ruleID           string
	organizationID   string
	serviceAccountID *string
	workspaceID      *string
	scope            *string
	identitySource   string
	identityPath     string
	identityPresent  bool
	invalid          bool
}

func captureProfileFederation(cfg ProfileConfig, raw, auth jsonvalue.Value) profileFederationSettings {
	s := profileFederationSettings{}
	if kind, _ := profileText(auth, "type"); kind != "oidc_federation" {
		return s
	}
	var err error
	s.ruleID, err = profileText(auth, "federation_rule_id")
	s.invalid = s.invalid || err != nil
	if s.ruleID == "" {
		s.ruleID = cfg.DefaultFederationRuleID
	}
	s.organizationID, err = profileText(raw, "organization_id")
	s.invalid = s.invalid || err != nil
	if s.organizationID == "" {
		s.organizationID = cfg.DefaultOrganizationID
	}
	s.serviceAccountID, err = optionalProfileText(auth, "service_account_id", cfg.DefaultServiceAccountID)
	s.invalid = s.invalid || err != nil
	s.workspaceID, err = optionalProfileText(raw, "workspace_id", cfg.DefaultWorkspaceID)
	s.invalid = s.invalid || err != nil
	s.scope, err = optionalProfileText(auth, "scope", cfg.DefaultScope)
	s.invalid = s.invalid || err != nil
	identity, present := auth.Lookup("identity_token")
	if (!present || !identity.Truth()) && cfg.DefaultIdentityTokenFile != "" {
		s.identityPresent = true
		s.identitySource = "file"
		s.identityPath = cfg.DefaultIdentityTokenFile
	} else if present && identity.Kind() != jsonvalue.Null {
		s.identityPresent = true
		if identity.Kind() != jsonvalue.Object {
			s.invalid = true
			return s
		}
		s.identitySource, err = profileText(identity, "source")
		s.invalid = s.invalid || err != nil
		s.identityPath, err = profileText(identity, "path")
		s.invalid = s.invalid || err != nil
	} else {
		s.identityPath = cfg.DefaultIdentityTokenFile
	}
	return s
}

func optionalProfileText(object jsonvalue.Value, name, fallback string) (*string, error) {
	value, present := object.Lookup(name)
	if !value.Truth() && fallback != "" {
		return copyFederationString(&fallback), nil
	}
	if !present || value.Kind() == jsonvalue.Null {
		return nil, nil
	}
	text, ok := value.Text()
	if !ok {
		return nil, profileError(CredentialInvalid)
	}
	return &text, nil
}

func (p *FileProfile) buildFederation() error {
	s := p.federationSettings
	if s.ruleID == "" || s.organizationID == "" {
		return profileError(CredentialExchange)
	}
	if s.invalid || (s.identityPresent && (s.identitySource != "file" || s.identityPath == "")) {
		return profileError(CredentialInvalid)
	}
	identity, err := NewIdentityTokenFile(s.identityPath)
	if err != nil {
		return err
	}
	p.federation, err = NewFederation(FederationConfig{Identity: identity, BaseURL: p.tokenBaseURL, FederationRuleID: s.ruleID, OrganizationID: s.organizationID, ServiceAccountID: s.serviceAccountID, WorkspaceID: s.workspaceID, Scope: s.scope, HTTPClient: p.http, Now: p.now})
	return err
}

type federationDiskRecord struct {
	access     string
	expiry     time.Time
	expires    bool
	diskExpiry float64
	refresh    jsonvalue.Value
	hasRefresh bool
	extensions []jsonvalue.Field
}

func validateProfileCredentialType(raw jsonvalue.Value) error {
	if value, present := raw.Lookup("type"); present && value.Kind() != jsonvalue.Null {
		text, ok := value.Text()
		if !ok || text != "oauth_token" {
			return profileError(CredentialInvalid)
		}
	}
	return nil
}

func (p *FileProfile) readFederationDisk() (federationDiskRecord, error) {
	// Path.exists in Source treats an inaccessible/missing parent as absent.
	// Existing readable files still pass all private-file guards below.
	if _, err := os.Stat(p.credentialsPath); err != nil {
		return federationDiskRecord{}, nil
	}
	raw, err := readProfileJSON(p.credentialsPath, true)
	if err != nil {
		if _, statErr := os.Stat(p.credentialsPath); os.IsNotExist(statErr) {
			return federationDiskRecord{}, nil
		}
		return federationDiskRecord{}, err
	}
	if err := validateProfileCredentialType(raw); err != nil {
		return federationDiskRecord{}, err
	}
	access, err := profileText(raw, "access_token")
	if err != nil {
		return federationDiskRecord{}, err
	}
	record := federationDiskRecord{access: access}
	// Invalid expiry is a cache miss for federation, not a fatal OAuth-file error.
	if value, present := raw.Lookup("expires_at"); present && value.Kind() != jsonvalue.Null {
		if expiry, err := profileExpiry(value); err == nil {
			record.expiry = expiry
			record.expires = true
			record.diskExpiry = float64(expiry.Unix())
			if fraction, ok := value.Float(); ok {
				record.diskExpiry = fraction
			}
		}
	}
	record.refresh, record.hasRefresh = raw.Lookup("refresh_token")
	for _, key := range raw.Keys() {
		switch key {
		case "access_token", "expires_at", "refresh_token", "type", "version":
			continue
		}
		value, _ := raw.Lookup(key)
		record.extensions = append(record.extensions, jsonvalue.Field{Name: key, Value: value})
	}
	return record, nil
}

func (p *FileProfile) fetchFederation(ctx context.Context, force bool) (AccessToken, error) {
	if p.federation == nil {
		if err := p.buildFederation(); err != nil {
			return AccessToken{}, err
		}
	}
	record, err := p.readFederationDisk()
	if err != nil {
		return AccessToken{}, err
	}
	now := p.now()
	seconds := float64(now.Unix()) + float64(now.Nanosecond())/1e9
	if !force && record.access != "" && record.expires && seconds < record.diskExpiry-30 {
		token, err := NewAccessToken(record.access, &record.expiry)
		if err != nil {
			return AccessToken{}, profileError(CredentialInvalid)
		}
		return token, nil
	}
	token, err := p.federation.FetchToken(ctx, force)
	if err != nil {
		return AccessToken{}, err
	}
	if err := ctx.Err(); err != nil {
		return AccessToken{}, err
	}
	fields := []jsonvalue.Field{
		{Name: "version", Value: jsonvalue.TextValue("1.0")},
		{Name: "type", Value: jsonvalue.TextValue("oauth_token")},
		{Name: "access_token", Value: jsonvalue.TextValue(token.value)},
		{Name: "expires_at", Value: jsonvalue.IntegerValue(token.expiry.Unix())},
	}
	if record.hasRefresh {
		fields = append(fields, jsonvalue.Field{Name: "refresh_token", Value: record.refresh})
	}
	fields = append(fields, record.extensions...)
	if err := p.writeCredentialFields(fields); err != nil {
		var failure *CredentialError
		if !errors.As(err, &failure) || failure.Kind != CredentialUnavailable {
			return AccessToken{}, err
		}
		// A federation disk cache is best effort. Keep the exchanged token when
		// filesystem writes fail; Client's in-memory cache still owns reuse.
	}
	return token, nil
}
