package agent

import (
	"errors"
	"github.com/luoyjx/mini-loop/go/efficiency"
	"math"
	"math/big"
)

type EfficiencyResponseStyle string

const (
	EfficiencyResponseNormal  EfficiencyResponseStyle = "normal"
	EfficiencyResponseConcise EfficiencyResponseStyle = "concise"
)

// ManagerEfficiencyConfig is the named service selection. It is independent of
// environment parsing; explicit injected runtimes retain their own components/mode.
type ManagerEfficiencyConfig struct {
	Mode                            efficiency.Mode
	ResponseStyle                   EfficiencyResponseStyle
	PersistRaw                      bool
	RawMinBytes                     *big.Int
	ArtifactTTLSeconds              float64
	MaxArtifactBytes, MaxTotalBytes *big.Int
}

func DefaultManagerEfficiencyConfig() ManagerEfficiencyConfig {
	return ManagerEfficiencyConfig{efficiency.Off, EfficiencyResponseNormal, true, big.NewInt(16384), 3600, big.NewInt(2_000_000), big.NewInt(20_000_000)}
}

func normalizeManagerEfficiency(config *ManagerEfficiencyConfig) (ManagerEfficiencyConfig, error) {
	c := DefaultManagerEfficiencyConfig()
	if config != nil {
		c = *config
		if c.Mode == "" {
			c.Mode = efficiency.Off
		}
		if c.ResponseStyle == "" {
			c.ResponseStyle = EfficiencyResponseNormal
		}
		if c.ArtifactTTLSeconds == 0 {
			c.ArtifactTTLSeconds = 3600
		}
		if c.RawMinBytes == nil {
			c.RawMinBytes = big.NewInt(16384)
		}
		if c.MaxArtifactBytes == nil {
			c.MaxArtifactBytes = big.NewInt(2_000_000)
		}
		if c.MaxTotalBytes == nil {
			c.MaxTotalBytes = big.NewInt(20_000_000)
		}
	}
	c.RawMinBytes, c.MaxArtifactBytes, c.MaxTotalBytes = new(big.Int).Set(c.RawMinBytes), new(big.Int).Set(c.MaxArtifactBytes), new(big.Int).Set(c.MaxTotalBytes)
	if err := c.Mode.Validate(); err != nil {
		return c, err
	}
	if c.ResponseStyle != EfficiencyResponseNormal && c.ResponseStyle != EfficiencyResponseConcise {
		return c, errors.New("invalid efficiency response style")
	}
	if c.RawMinBytes.Sign() <= 0 || c.MaxArtifactBytes.Sign() <= 0 || c.MaxTotalBytes.Cmp(c.MaxArtifactBytes) < 0 || c.ArtifactTTLSeconds <= 0 || math.IsNaN(c.ArtifactTTLSeconds) || math.IsInf(c.ArtifactTTLSeconds, 0) {
		return c, errors.New("invalid efficiency artifact limits")
	}
	return c, nil
}

func managerDefaultEfficiency(c ManagerEfficiencyConfig) (*efficiency.Runtime, error) {
	var registry efficiency.Registry
	if c.Mode != efficiency.Off {
		if _, err := registry.RegisterObservation(efficiency.DeterministicLosslessReducer{}, nil); err != nil {
			return nil, err
		}
	}
	if c.ResponseStyle == EfficiencyResponseConcise {
		policyConfig := efficiency.DefaultConciseResponseConfig()
		policyConfig.RequireOptIn = false
		policy, err := efficiency.NewConciseResponsePlanner(policyConfig)
		if err != nil {
			return nil, err
		}
		if _, err = registry.RegisterResponse(policy, nil); err != nil {
			return nil, err
		}
	}
	runtimeConfig := efficiency.DefaultRuntimeConfig()
	runtimeConfig.DefaultMode = c.Mode
	return registry.Runtime(runtimeConfig)
}
