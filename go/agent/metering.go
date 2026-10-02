package agent

import (
	"github.com/luoyjx/mini-loop/go/protocol"
	"math"
)

func EstimateTokens(messages []protocol.Message) int {
	text, err := protocol.PythonJSON(messages, true, false)
	if err != nil {
		return 0
	} // callers validate the typed transcript first
	return len(text) / 4
}

type TokenMeterSnapshot struct {
	Calibrated     bool    `json:"calibrated"`
	AnchorTokens   *int    `json:"anchor_tokens"`
	AnchorEnvelope *string `json:"anchor_envelope"`
	Calibration    float64 `json:"calibration"`
	Observations   int     `json:"observations"`
}

// TokenMeter is owned by the serialized session. Its signed deltas account for
// context shrinking; an envelope change discards the stale prefix estimate.
type TokenMeter struct {
	anchor       int
	estimate     int
	envelope     string
	calibration  float64
	observations int
}

func (meter TokenMeter) scale() float64 {
	if meter.observations == 0 {
		return 1
	}
	return meter.calibration
}
func PromptTokens(usage protocol.TokenUsage) (int, bool) {
	count := usage.InputTokens
	if usage.CacheReadInputTokens != nil {
		count += *usage.CacheReadInputTokens
	}
	if usage.CacheCreationInputTokens != nil {
		count += *usage.CacheCreationInputTokens
	}
	return count, count != 0
}
func (meter *TokenMeter) Observe(usage protocol.TokenUsage, messages []protocol.Message, envelope string) {
	actual, ok := PromptTokens(usage)
	if !ok {
		return
	}
	estimate := EstimateTokens(messages)
	calibration := meter.scale()
	grew := estimate - meter.estimate
	sameEnvelope := envelope == "" || meter.envelope == "" || envelope == meter.envelope
	if meter.observations != 0 && sameEnvelope && grew > 0 && actual > meter.anchor {
		ratio := float64(actual-meter.anchor) / float64(grew)
		calibration = min(6., max(.2, .5*calibration+.5*ratio))
	}
	meter.anchor, meter.estimate, meter.envelope, meter.calibration = actual, estimate, envelope, calibration
	meter.observations++
}
func (meter TokenMeter) UsedFor(messages []protocol.Message, envelope string) int {
	estimate := EstimateTokens(messages)
	if meter.observations == 0 || (envelope != "" && meter.envelope != "" && envelope != meter.envelope) {
		return estimate
	}
	return max(0, int(float64(meter.anchor)+float64(estimate-meter.estimate)*meter.scale()))
}
func (meter TokenMeter) Snapshot() TokenMeterSnapshot {
	snapshot := TokenMeterSnapshot{Calibrated: meter.observations != 0, Calibration: math.RoundToEven(meter.scale()*1000) / 1000, Observations: meter.observations}
	if snapshot.Calibrated {
		anchor := meter.anchor
		snapshot.AnchorTokens = &anchor
	}
	if meter.envelope != "" {
		envelope := meter.envelope
		snapshot.AnchorEnvelope = &envelope
	}
	return snapshot
}
