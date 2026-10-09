// Package runtimeconfig translates validated host settings into typed runtime selections.
// It performs no environment reads, resource allocation or tool installation.
package runtimeconfig

import (
	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/efficiency"
	"math/big"
)

// ManagerEfficiency snapshots settings for both standalone and benchmark managers.
// Callers validate Settings before runtime construction; integers are newly owned.
func ManagerEfficiency(settings config.Settings) agent.ManagerEfficiencyConfig {
	return agent.ManagerEfficiencyConfig{
		Mode:               efficiency.Mode(settings.TokenEfficiencyMode),
		ResponseStyle:      agent.EfficiencyResponseStyle(settings.TokenEfficiencyResponseStyle),
		PersistRaw:         settings.TokenEfficiencyPersistRaw,
		RawMinBytes:        big.NewInt(int64(settings.TokenEfficiencyRawMinBytes)),
		ArtifactTTLSeconds: float64(settings.TokenEfficiencyArtifactTTLSeconds),
		MaxArtifactBytes:   big.NewInt(int64(settings.TokenEfficiencyMaxArtifactBytes)),
		MaxTotalBytes:      big.NewInt(int64(settings.TokenEfficiencyMaxTotalBytes)),
	}
}
