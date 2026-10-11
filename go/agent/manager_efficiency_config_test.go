package agent

import (
	"context"
	"math"
	"math/big"
	"testing"

	"github.com/luoyjx/mini-loop/go/efficiency"
)

func TestManagerEfficiencySelection(t *testing.T) {
	for _, mode := range []efficiency.Mode{efficiency.Off, efficiency.Shadow, efficiency.Enforce} {
		for _, style := range []EfficiencyResponseStyle{EfficiencyResponseNormal, EfficiencyResponseConcise} {
			t.Run(string(mode)+"/"+string(style), func(t *testing.T) {
				c := DefaultManagerEfficiencyConfig()
				c.Mode, c.ResponseStyle = mode, style
				r, err := managerDefaultEfficiency(c)
				if err != nil {
					t.Fatal(err)
				}
				if r.ObservationEnforced() != (mode == efficiency.Enforce) {
					t.Fatal("observation selection")
				}
				report, err := r.Initialize(context.Background(), efficiency.LifecycleServices{})
				if err != nil {
					t.Fatal(err)
				}
				p, err := report.Projection()
				if err != nil {
					t.Fatal(err)
				}
				want := 0
				if mode != efficiency.Off {
					want++
					if style == EfficiencyResponseConcise {
						want++
					}
				}
				if len(p.Components) != want {
					t.Fatalf("enabled components: %+v", p)
				}
				if want > 0 && p.Components[0].ComponentID != "deterministic-lossless" {
					t.Fatal(p)
				}
				if want == 2 && p.Components[1].ComponentID != "concise-response" {
					t.Fatal(p)
				}
				if _, err := r.Close(context.Background(), efficiency.LifecycleCloseOptions{}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestManagerEfficiencyConfigOwnsLimits(t *testing.T) {
	c := DefaultManagerEfficiencyConfig()
	got, err := normalizeManagerEfficiency(&c)
	if err != nil {
		t.Fatal(err)
	}
	c.RawMinBytes.SetInt64(1)
	c.MaxArtifactBytes.SetInt64(1)
	c.MaxTotalBytes.SetInt64(1)
	if got.RawMinBytes.Int64() != 16384 || got.MaxArtifactBytes.Int64() != 2_000_000 || got.MaxTotalBytes.Int64() != 20_000_000 {
		t.Fatal("limits alias caller")
	}
	if got.Mode != efficiency.Off || got.ResponseStyle != EfficiencyResponseNormal || !got.PersistRaw {
		t.Fatal("defaults changed")
	}
	invalid := []ManagerEfficiencyConfig{DefaultManagerEfficiencyConfig(), DefaultManagerEfficiencyConfig(), DefaultManagerEfficiencyConfig(), DefaultManagerEfficiencyConfig(), DefaultManagerEfficiencyConfig()}
	invalid[0].Mode = "unsafe"
	invalid[1].ResponseStyle = "verbose"
	invalid[2].RawMinBytes = big.NewInt(-1)
	invalid[3].MaxTotalBytes = big.NewInt(1)
	invalid[4].ArtifactTTLSeconds = math.NaN()
	for _, c := range invalid {
		if _, err := normalizeManagerEfficiency(&c); err == nil {
			t.Fatal("invalid selection accepted", c)
		}
	}
}
