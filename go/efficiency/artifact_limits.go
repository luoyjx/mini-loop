package efficiency

import (
	"errors"
	"math"
	"math/big"
)

// ArtifactLimits contains settings only; it grants no store or workspace authority.
type ArtifactLimits struct {
	TTLSeconds       float64
	MaxArtifactBytes *big.Int
	MaxTotalBytes    *big.Int
}

func DefaultArtifactLimits() ArtifactLimits {
	return ArtifactLimits{TTLSeconds: 3600, MaxArtifactBytes: big.NewInt(2_000_000), MaxTotalBytes: big.NewInt(20_000_000)}
}
func (c ArtifactLimits) Clone() ArtifactLimits {
	c.MaxArtifactBytes = copyInteger(c.MaxArtifactBytes)
	c.MaxTotalBytes = copyInteger(c.MaxTotalBytes)
	return c
}
func (c ArtifactLimits) Validate() error {
	if math.IsNaN(c.TTLSeconds) || math.IsInf(c.TTLSeconds, 0) || c.TTLSeconds <= 0 {
		return errors.New("ttl_seconds must be positive")
	}
	if c.MaxArtifactBytes == nil || c.MaxTotalBytes == nil {
		return errors.New("artifact size limits must be integers")
	}
	if c.MaxArtifactBytes.Sign() <= 0 || c.MaxTotalBytes.Sign() <= 0 {
		return errors.New("artifact size limits must be positive")
	}
	if c.MaxArtifactBytes.Cmp(c.MaxTotalBytes) > 0 {
		return errors.New("max_artifact_bytes cannot exceed max_total_bytes")
	}
	return nil
}

// Limits returns detached settings. Raw content and references stay private.
func (s *RawStore) Limits() ArtifactLimits {
	return ArtifactLimits{TTLSeconds: s.ttl, MaxArtifactBytes: copyInteger(s.maxItem), MaxTotalBytes: copyInteger(s.maxTotal)}
}
