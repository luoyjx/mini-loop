package config

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderCredentialCapturePrivacyAndPresence(t *testing.T) {
	for _, name := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"} {
		t.Run(name, func(t *testing.T) {
			settings, err := Load(map[string]string{name: "", "ANTHROPIC_PROFILE": "broken"}, LoadOptions{Directory: t.TempDir(), HomeDirectory: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			if !settings.ProviderCredentials().SkipDiscovery {
				t.Fatal("defined empty static credential must suppress discovery")
			}
		})
	}
	home, root := t.TempDir(), t.TempDir()
	env := map[string]string{"ANTHROPIC_CONFIG_DIR": "profiles", "ANTHROPIC_IDENTITY_TOKEN": "private-assertion", "ANTHROPIC_SERVICE_ACCOUNT_ID": "", "ANTHROPIC_SCOPE": "scope"}
	settings, err := Load(env, LoadOptions{Directory: root, HomeDirectory: home})
	if err != nil {
		t.Fatal(err)
	}
	env["ANTHROPIC_IDENTITY_TOKEN"] = "changed"
	selected := settings.ProviderCredentials()
	if !selected.ExplicitDirectory || selected.Directory != filepath.Join(root, "profiles") || !selected.LiteralDefined || selected.ServiceAccountID == nil || *selected.ServiceAccountID != "" {
		t.Fatal("captured presence/path differs")
	}
	value, present, err := selected.IdentityReader(context.Background())
	if err != nil || !present || value != "private-assertion" {
		t.Fatal("explicit map must remain captured")
	}
	*selected.Scope = "mutated"
	if *settings.ProviderCredentials().Scope != "scope" {
		t.Fatal("selection aliases settings")
	}
	wire, err := json.Marshal(settings.Snapshot())
	if err != nil || strings.Contains(string(wire), "private-assertion") {
		t.Fatal("snapshot leaked identity")
	}
	snapshot := settings.Snapshot().Settings.ProviderCredentials()
	if snapshot.IdentityReader != nil || snapshot.LiteralDefined || snapshot.Literal.Present() {
		t.Fatal("snapshot retained live selection")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := selected.IdentityReader(ctx); err != context.Canceled {
		t.Fatal("reader ignored cancellation")
	}
}

func TestProviderCredentialExplicitReaderRotates(t *testing.T) {
	value, present := "first", true
	settings, err := Load(map[string]string{"ANTHROPIC_IDENTITY_TOKEN": "first"}, LoadOptions{Directory: t.TempDir(), HomeDirectory: t.TempDir(), IdentityTokenReader: func(context.Context) (string, bool, error) { return value, present, nil }})
	if err != nil {
		t.Fatal(err)
	}
	reader := settings.ProviderCredentials().IdentityReader
	value = "second"
	if got, ok, err := reader(context.Background()); err != nil || !ok || got != "second" {
		t.Fatal("explicit reader did not rotate")
	}
	present = false
	if _, ok, _ := reader(context.Background()); ok {
		t.Fatal("reader hid disappearance")
	}
}
