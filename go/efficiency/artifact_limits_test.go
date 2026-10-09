package efficiency

import (
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"
)

func TestArtifactLimitsDetachAndBoundStore(t *testing.T) {
	cfg := DefaultRawStoreConfig(t.TempDir())
	cfg.TTLSeconds = 7
	cfg.MaxArtifactBytes = big.NewInt(200)
	cfg.MaxTotalBytes = big.NewInt(800)
	cfg.Clock = func() (float64, error) { return 4, nil }
	store, err := NewRawStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	limits := store.Limits()
	limits.MaxArtifactBytes.SetInt64(1)
	cfg.MaxTotalBytes.SetInt64(1)
	if _, err := store.PutMasked(strings.Repeat("x", 201)); !errors.Is(err, ErrArtifactTooLarge) {
		t.Fatal("item limit", err)
	}
	for range 4 {
		p, err := store.PutMasked(strings.Repeat("x", 200))
		if err != nil {
			t.Fatal(err)
		}
		s, err := p.Snapshot()
		if err != nil || s.ExpiresAt != 11 {
			t.Fatal("TTL", s, err)
		}
	}
	if _, err := store.PutMasked("x"); !errors.Is(err, ErrArtifactCapacity) {
		t.Fatal("total limit", err)
	}
	actual := store.Limits()
	if actual.MaxArtifactBytes.Int64() != 200 || actual.MaxTotalBytes.Int64() != 800 {
		t.Fatal("limits aliased")
	}
}
func TestArtifactLimitsValidation(t *testing.T) {
	for _, limits := range []ArtifactLimits{{TTLSeconds: math.NaN(), MaxArtifactBytes: big.NewInt(1), MaxTotalBytes: big.NewInt(1)}, {TTLSeconds: 1}, {TTLSeconds: 1, MaxArtifactBytes: big.NewInt(0), MaxTotalBytes: big.NewInt(1)}, {TTLSeconds: 1, MaxArtifactBytes: big.NewInt(2), MaxTotalBytes: big.NewInt(1)}} {
		if err := limits.Validate(); err == nil {
			t.Fatal("invalid limits accepted", limits)
		}
	}
}
