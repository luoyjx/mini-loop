package efficiency

import (
	"encoding/json"
	"errors"
	"math/big"
	"strings"
)

// DescriptorConfig is the named construction boundary. Integer limits retain
// Source's unbounded values; the runtime will separately admit duration/capacity.
type DescriptorConfig struct {
	ID            string    `json:"id"`
	Version       string    `json:"version"`
	Stage         Stage     `json:"stage"`
	ContentTypes  []string  `json:"content_types"`
	Deterministic bool      `json:"deterministic"`
	Lossiness     Lossiness `json:"lossiness"`
	Recoverable   bool      `json:"recoverable"`
	CostTier      CostTier  `json:"cost_tier"`
	NetworkAccess bool      `json:"network_access"`
	TimeoutMS     *big.Int  `json:"timeout_ms"`
	MaxInputBytes *big.Int  `json:"max_input_bytes"`
	Capabilities  []string  `json:"capabilities"`
}

func DefaultDescriptor(id, version string, stage Stage) DescriptorConfig {
	return DescriptorConfig{ID: id, Version: version, Stage: stage,
		ContentTypes: []string{"*/*"}, Deterministic: true, Lossiness: Lossless,
		CostTier: Fast, TimeoutMS: big.NewInt(1000), Capabilities: []string{}}
}

// Descriptor owns a detached immutable configuration. Zero values are not admitted.
type Descriptor struct {
	config DescriptorConfig
	valid  bool
}

func copyInteger(value *big.Int) *big.Int {
	if value == nil {
		return nil
	}
	return new(big.Int).Set(value)
}
func (config DescriptorConfig) clone() DescriptorConfig {
	config.ContentTypes = append([]string{}, config.ContentTypes...)
	config.Capabilities = append([]string{}, config.Capabilities...)
	config.TimeoutMS = copyInteger(config.TimeoutMS)
	config.MaxInputBytes = copyInteger(config.MaxInputBytes)
	return config
}

func NewDescriptor(config DescriptorConfig) (Descriptor, error) {
	if !componentID.MatchString(config.ID) {
		return Descriptor{}, errors.New("component id must be 1-128 safe identifier characters")
	}
	if !componentVersion.MatchString(config.Version) {
		return Descriptor{}, errors.New("component version must be a 1-64 character safe identifier")
	}
	if err := config.Stage.Validate(); err != nil {
		return Descriptor{}, err
	}
	if err := config.Lossiness.Validate(); err != nil {
		return Descriptor{}, err
	}
	if len(config.ContentTypes) == 0 {
		return Descriptor{}, errors.New("content_types must contain MIME-like values")
	}
	for _, value := range config.ContentTypes {
		if value == "" || !strings.Contains(value, "/") {
			return Descriptor{}, errors.New("content_types must contain MIME-like values")
		}
	}
	seen := make(map[string]bool, len(config.Capabilities))
	capabilities := []string{}
	for _, value := range config.Capabilities {
		if value == "" {
			return Descriptor{}, errors.New("capabilities must be non-empty strings")
		}
		if !seen[value] {
			seen[value] = true
			capabilities = append(capabilities, value)
		}
	}
	if config.TimeoutMS == nil || config.TimeoutMS.Sign() <= 0 {
		return Descriptor{}, errors.New("timeout_ms must be positive")
	}
	if config.MaxInputBytes != nil && config.MaxInputBytes.Sign() <= 0 {
		return Descriptor{}, errors.New("max_input_bytes must be positive when set")
	}
	switch config.CostTier {
	case Fast, ML, Remote:
	default:
		return Descriptor{}, errors.New("cost_tier must be fast, ml, or remote")
	}
	if config.Lossiness == Recoverable && !config.Recoverable {
		return Descriptor{}, errors.New("recoverable lossiness requires recoverable=True")
	}
	config = config.clone()
	config.Capabilities = capabilities
	return Descriptor{config: config, valid: true}, nil
}

func (descriptor Descriptor) Snapshot() (DescriptorConfig, error) {
	if !descriptor.valid {
		return DescriptorConfig{}, errors.New("uninitialized component descriptor")
	}
	return descriptor.config.clone(), nil
}
func (descriptor Descriptor) MarshalJSON() ([]byte, error) {
	config, err := descriptor.Snapshot()
	if err != nil {
		return nil, err
	}
	return json.Marshal(config)
}
