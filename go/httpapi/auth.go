package httpapi

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"github.com/luoyjx/mini-loop/go/agent"
	"os"
	"sort"
	"strings"
)

type Principal struct {
	ID        agent.OwnerID
	Anonymous bool
}
type Authenticator interface {
	Authenticate(string) (Principal, bool)
	Configured() bool
}
type NullAuth struct{}

func (NullAuth) Authenticate(string) (Principal, bool) { return Principal{"anonymous", true}, true }
func (NullAuth) Configured() bool                      { return false }

type TokenBinding struct {
	Token     string
	Principal agent.OwnerID
}
type TokenAuth struct{ bindings []TokenBinding }

func NewTokenAuth(bindings []TokenBinding) (*TokenAuth, error) {
	if len(bindings) == 0 {
		return nil, errors.New("TokenAuth needs at least one token")
	}
	seen := make(map[string]agent.OwnerID)
	result := &TokenAuth{}
	for _, b := range bindings {
		if b.Token == "" || b.Principal == "" {
			return nil, errors.New("tokens and principal ids must be non-empty")
		}
		if old, ok := seen[b.Token]; ok {
			if old != b.Principal {
				return nil, errors.New("one token cannot identify multiple principals")
			}
			continue
		}
		seen[b.Token] = b.Principal
		result.bindings = append(result.bindings, b)
	}
	return result, nil
}
func (auth *TokenAuth) Configured() bool { return true }
func (auth *TokenAuth) Authenticate(header string) (Principal, bool) {
	scheme, presented, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "bearer") || presented == "" {
		return Principal{}, false
	}
	var id agent.OwnerID
	for _, b := range auth.bindings {
		if subtle.ConstantTimeCompare([]byte(b.Token), []byte(presented)) == 1 {
			id = b.Principal
		}
	}
	return Principal{ID: id}, id != ""
}
func (auth *TokenAuth) Principals() []agent.OwnerID {
	set := make(map[agent.OwnerID]bool)
	for _, b := range auth.bindings {
		set[b.Principal] = true
	}
	result := make([]agent.OwnerID, 0, len(set))
	for id := range set {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
func AuthFromEnvironment(env map[string]string) (Authenticator, error) {
	get := func(key string) string {
		if env == nil {
			return os.Getenv(key)
		}
		return env[key]
	}
	raw := strings.TrimSpace(get("MINILOOP_API_TOKENS"))
	if raw != "" {
		var bindings []TokenBinding
		for _, entry := range strings.Split(raw, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			owner, token, ok := strings.Cut(entry, ":")
			if !ok || token == "" {
				return nil, errors.New("MINILOOP_API_TOKENS entries must be 'principal:token'")
			}
			bindings = append(bindings, TokenBinding{token, agent.OwnerID(owner)})
		}
		return NewTokenAuth(bindings)
	}
	if single := strings.TrimSpace(get("MINILOOP_API_TOKEN")); single != "" {
		return NewTokenAuth([]TokenBinding{{single, "default"}})
	}
	return NullAuth{}, nil
}
func RefuseOpenBind(host string, auth Authenticator) error {
	if auth != nil && auth.Configured() {
		return nil
	}
	switch host {
	case "127.0.0.1", "localhost", "::1", "":
		return nil
	}
	return fmt.Errorf("refusing to bind %s without authentication: this would expose session creation, shell execution and every recorded transcript to any caller. Set MINILOOP_API_TOKEN, or bind 127.0.0.1.", host)
}
