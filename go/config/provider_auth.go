package config

import "strings"

// ProviderHeader keeps environment header values behind the credential boundary.
type ProviderHeader struct {
	Name  string
	Value Secret
}

// ProviderAuthentication mirrors Source Settings -> provider_for -> SDK:
// a nonempty Settings API key becomes explicit and suppresses bearer discovery.
// Custom headers still override static authentication at the provider boundary.
func (s Settings) ProviderAuthentication() (Secret, []ProviderHeader) {
	token := s.AuthToken
	if s.APIKey.Present() {
		token = Secret{}
	}
	var headers []ProviderHeader
	// SDK distinguishes an undefined auth token from a defined empty token.
	// The latter still produces a bearer header and suppresses discovery.
	if s.authTokenDefined && !s.AuthToken.Present() && !s.APIKey.Present() {
		headers = append(headers, ProviderHeader{Name: "Authorization", Value: Secret{"Bearer "}})
	}
	if s.apiKeyDefined && !s.APIKey.Present() {
		headers = append(headers, ProviderHeader{Name: "X-Api-Key", Value: Secret{}})
	}
	for _, line := range strings.Split(s.ProviderHeaders.Reveal(), "\n") {
		name, value, ok := strings.Cut(line, ":")
		if ok {
			headers = append(headers, ProviderHeader{Name: strings.TrimSpace(name), Value: Secret{strings.TrimSpace(value)}})
		}
	}
	return token, headers
}
