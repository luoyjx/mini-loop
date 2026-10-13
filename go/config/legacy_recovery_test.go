package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExplicitLoadDetachesLegacyRecoveryInputAndExcludesSnapshot(t *testing.T) {
	t.Setenv("FALLBACK_MODEL_ID", "ambient-must-not-enter")
	for _, row := range []struct {
		name, value string
		present     bool
	}{
		{name: "absent"},
		{name: "empty", present: true},
		{name: "value", present: true, value: "captured-legacy"},
	} {
		t.Run(row.name, func(t *testing.T) {
			environment := map[string]string{"MINILOOP_FALLBACK_MODEL": "modern-model"}
			if row.present {
				environment["FALLBACK_MODEL_ID"] = row.value
			}
			settings, err := Load(environment, LoadOptions{Directory: t.TempDir(), HomeDirectory: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			environment["FALLBACK_MODEL_ID"] = "caller-map-mutation"
			legacy := settings.LegacyFallbackModel()
			if legacy == nil || *legacy != row.value {
				t.Fatal("explicit environment was not captured", legacy)
			}
			*legacy = "caller-pointer-mutation"
			if again := settings.LegacyFallbackModel(); again == nil || *again != row.value {
				t.Fatal("legacy getter aliases settings", again)
			}
			snapshot := settings.Snapshot()
			if snapshot.Settings.LegacyFallbackModel() != nil || snapshot.FallbackModel == nil || *snapshot.FallbackModel != "modern-model" {
				t.Fatal("snapshot changed modern fallback or retained private legacy input")
			}
			encoded, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), `"legacy_fallback_model"`) || strings.Contains(string(encoded), "captured-legacy") || strings.Contains(string(encoded), "FALLBACK_MODEL_ID") {
				t.Fatal("legacy input expanded the Settings snapshot", string(encoded))
			}
		})
	}
}

func TestAmbientLoadDefersLegacyRecoveryLookup(t *testing.T) {
	t.Setenv("FALLBACK_MODEL_ID", "settings-time-legacy")
	t.Setenv("MINILOOP_FALLBACK_MODEL", "")
	settings, err := Load(nil, LoadOptions{Directory: t.TempDir(), HomeDirectory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if settings.LegacyFallbackModel() != nil || settings.FallbackModel != nil {
		t.Fatal("ambient legacy became a Settings value")
	}
	t.Setenv("FALLBACK_MODEL_ID", "recovery-time-legacy")
	if settings.LegacyFallbackModel() != nil || settings.Snapshot().FallbackModel != nil {
		t.Fatal("Settings eagerly captured ambient legacy")
	}
}
