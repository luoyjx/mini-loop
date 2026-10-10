package provider

import (
	"context"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxIdentityAssertionBytes = 16 * 1024

// IdentityAssertion is a detached UTF-8 JWT assertion, never a parsed claim or
// an authority grant. Its formatting cannot disclose the token.
type IdentityAssertion struct{ value string }

func (IdentityAssertion) String() string   { return "<identity assertion>" }
func (IdentityAssertion) GoString() string { return "<identity assertion>" }
func NewIdentityAssertion(value string) (IdentityAssertion, error) {
	if !utf8.ValidString(value) || len(value) > MaxIdentityAssertionBytes {
		return IdentityAssertion{}, profileError(CredentialExchange)
	}
	return IdentityAssertion{value: value}, nil
}

// IdentityTokenProvider is called on every exchange, including a forced retry.
// Implementations must honor contexts and must not share mutable assertions.
type IdentityTokenProvider interface {
	IdentityToken(context.Context) (IdentityAssertion, error)
}

// LiteralIdentityToken captures one operator-supplied assertion. It performs no
// environment reads and can be replaced by a caller-owned rotating provider.
type LiteralIdentityToken struct{ value string }

// Construction captures the literal; request-time validation matches Source.
func NewLiteralIdentityToken(value string) *LiteralIdentityToken {
	return &LiteralIdentityToken{value: value}
}
func (*LiteralIdentityToken) String() string   { return "<literal identity provider>" }
func (*LiteralIdentityToken) GoString() string { return "<literal identity provider>" }
func (p *LiteralIdentityToken) IdentityToken(ctx context.Context) (IdentityAssertion, error) {
	if err := ctx.Err(); err != nil {
		return IdentityAssertion{}, err
	}
	return NewIdentityAssertion(p.value)
}

// IdentityTokenFile deliberately follows projected-token symlinks, unlike the
// private OAuth credential cache. It reopens the file for every exchange.
type IdentityTokenFile struct{ path string }

func NewIdentityTokenFile(path string) (*IdentityTokenFile, error) {
	if path == "" || strings.IndexByte(path, 0) >= 0 {
		return nil, profileError(CredentialInvalid)
	}
	return &IdentityTokenFile{path: path}, nil
}
func (*IdentityTokenFile) String() string   { return "<file identity provider>" }
func (*IdentityTokenFile) GoString() string { return "<file identity provider>" }
func (p *IdentityTokenFile) IdentityToken(ctx context.Context) (IdentityAssertion, error) {
	if err := ctx.Err(); err != nil {
		return IdentityAssertion{}, err
	}
	f, err := os.Open(p.path)
	if err != nil {
		return IdentityAssertion{}, profileError(CredentialUnavailable)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return IdentityAssertion{}, profileError(CredentialUnavailable)
	}
	raw, err := io.ReadAll(io.LimitReader(f, DefaultWireLimit+1))
	if err := ctx.Err(); err != nil {
		return IdentityAssertion{}, err
	}
	if err != nil {
		return IdentityAssertion{}, profileError(CredentialUnavailable)
	}
	if len(raw) > DefaultWireLimit || !utf8.Valid(raw) {
		return IdentityAssertion{}, profileError(CredentialInvalid)
	}
	// Python str.strip also treats the four ASCII information separators as space.
	value := strings.TrimFunc(string(raw), func(r rune) bool { return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f) })
	if value == "" {
		return IdentityAssertion{}, profileError(CredentialInvalid)
	}
	return NewIdentityAssertion(value)
}
