package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type oauthRefreshRequest struct {
	GrantType    string `json:"grant_type"`
	RefreshToken string `json:"refresh_token"`
	ClientID     string `json:"client_id"`
}

func (p *FileProfile) refresh(ctx context.Context, c profileCredentials) (AccessToken, error) {
	body, err := json.Marshal(oauthRefreshRequest{GrantType: "refresh_token", RefreshToken: c.refresh, ClientID: p.clientID})
	if err != nil {
		return AccessToken{}, profileError(CredentialExchange)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenBaseURL+"/v1/oauth/token", strings.NewReader(string(body)))
	if err != nil {
		return AccessToken{}, profileError(CredentialExchange)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Anthropic-Beta", OAuthAPIBeta)
	resp, err := p.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return AccessToken{}, ctx.Err()
		}
		return AccessToken{}, profileError(CredentialExchange)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return AccessToken{}, &CredentialError{Kind: CredentialExchange, Status: resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, DefaultWireLimit+1))
	if ctx.Err() != nil {
		return AccessToken{}, ctx.Err()
	}
	if err != nil || len(data) > DefaultWireLimit || !utf8.Valid(data) {
		return AccessToken{}, profileError(CredentialExchange)
	}
	payload, err := jsonvalue.Decode(string(data))
	if err != nil || payload.Kind() != jsonvalue.Object {
		return AccessToken{}, profileError(CredentialExchange)
	}
	access, err := profileText(payload, "access_token")
	if err != nil || access == "" {
		return AccessToken{}, profileError(CredentialExchange)
	}
	duration, present := payload.Lookup("expires_in")
	if !present {
		duration = jsonvalue.IntegerValue(3600)
	}
	seconds, err := duration.PythonInteger()
	if err != nil {
		return AccessToken{}, profileError(CredentialExchange)
	}
	// Arithmetic stays arbitrary-precision at ingress, before the checked time boundary.
	nowValue := jsonvalue.IntegerValue(p.now().Unix())
	nowSeconds, _ := nowValue.PythonInteger()
	seconds.Add(seconds, nowSeconds)
	absolute, err := jsonvalue.Decode(seconds.String())
	if err != nil {
		return AccessToken{}, profileError(CredentialExchange)
	}
	expiry, err := profileExpiry(absolute)
	if err != nil {
		return AccessToken{}, profileError(CredentialExchange)
	}
	token, err := NewAccessToken(access, &expiry)
	if err != nil {
		return AccessToken{}, profileError(CredentialExchange)
	}
	refresh, err := profileText(payload, "refresh_token")
	if err != nil {
		return AccessToken{}, profileError(CredentialExchange)
	}
	if refresh != "" {
		c.refresh = refresh
	}
	c.access, c.expiry, c.expires = access, expiry, true
	if err := ctx.Err(); err != nil {
		return AccessToken{}, err
	}
	if err := p.writeCredentials(c); err != nil {
		return AccessToken{}, err
	}
	return token, nil
}

func (p *FileProfile) writeCredentials(c profileCredentials) error {
	fields := []jsonvalue.Field{
		{Name: "version", Value: jsonvalue.TextValue("1.0")},
		{Name: "type", Value: jsonvalue.TextValue("oauth_token")},
		{Name: "access_token", Value: jsonvalue.TextValue(c.access)},
		{Name: "expires_at", Value: jsonvalue.IntegerValue(c.expiry.Unix())},
		{Name: "refresh_token", Value: jsonvalue.TextValue(c.refresh)},
	}
	fields = append(fields, c.extensions...)
	return p.writeCredentialFields(fields)
}

func (p *FileProfile) writeCredentialFields(fields []jsonvalue.Field) error {
	data, err := jsonvalue.AppendLegacyIndent(jsonvalue.ObjectValue(fields))
	if err != nil {
		return profileError(CredentialInvalid)
	}
	parent := filepath.Dir(p.credentialsPath)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return profileError(CredentialUnavailable)
	}
	temp, err := os.CreateTemp(parent, "."+filepath.Base(p.credentialsPath)+".*.tmp")
	if err != nil {
		return profileError(CredentialUnavailable)
	}
	defer os.Remove(temp.Name())
	if err = temp.Chmod(0600); err == nil {
		_, err = temp.Write(data)
	}
	if err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err != nil || closeErr != nil {
		return profileError(CredentialUnavailable)
	}
	if err := os.Rename(temp.Name(), p.credentialsPath); err != nil {
		return profileError(CredentialUnavailable)
	}
	if dir, err := os.Open(parent); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}
