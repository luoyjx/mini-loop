package agent

import (
	"errors"
	"math"
	"math/big"

	"github.com/luoyjx/mini-loop/go/efficiency"
)

const EventOptimizationReceipt SessionEventKind = "optimization_receipt"

// OptimizationMetrics is the Source Agent's public receipt projection. Digests,
// warning strings and content have no fields in this event variant.
type OptimizationMetrics struct {
	ComponentID          string               `json:"component_id"`
	ComponentVersion     string               `json:"component_version"`
	Stage                efficiency.Stage     `json:"stage"`
	Mode                 efficiency.Mode      `json:"mode"`
	Status               efficiency.Status    `json:"status"`
	Reason               string               `json:"reason"`
	RawBytes             *big.Int             `json:"raw_bytes"`
	ProjectedBytes       *big.Int             `json:"projected_bytes"`
	TokensBeforeEstimate *big.Int             `json:"tokens_before_estimate"`
	TokensAfterEstimate  *big.Int             `json:"tokens_after_estimate"`
	Lossiness            efficiency.Lossiness `json:"lossiness"`
	Deterministic        bool                 `json:"deterministic"`
	RawRef               *string              `json:"raw_ref"`
	ElapsedMS            float64              `json:"elapsed_ms"`
	Changed              bool                 `json:"changed"`
	WarningCount         int                  `json:"warning_count"`
	ParentSpanID         *SpanID              `json:"parent_span_id"`
}

func copyMetricInteger(value *big.Int) *big.Int {
	if value == nil {
		return nil
	}
	return new(big.Int).Set(value)
}
func (m OptimizationMetrics) clone() OptimizationMetrics {
	m.RawBytes = copyMetricInteger(m.RawBytes)
	m.ProjectedBytes = copyMetricInteger(m.ProjectedBytes)
	m.TokensBeforeEstimate = copyMetricInteger(m.TokensBeforeEstimate)
	m.TokensAfterEstimate = copyMetricInteger(m.TokensAfterEstimate)
	m.RawRef, m.ParentSpanID = clonePointer(m.RawRef), clonePointer(m.ParentSpanID)
	return m
}
func optimizationMetrics(r efficiency.ReceiptProjection) OptimizationMetrics {
	return (OptimizationMetrics{
		ComponentID: r.ComponentID, ComponentVersion: r.ComponentVersion,
		Stage: r.Stage, Mode: r.Mode, Status: r.Status, Reason: r.Reason,
		RawBytes: r.RawBytes, ProjectedBytes: r.ProjectedBytes,
		TokensBeforeEstimate: r.TokensBeforeEstimate, TokensAfterEstimate: r.TokensAfterEstimate,
		Lossiness: r.Lossiness, Deterministic: r.Deterministic, RawRef: r.RawRef,
		ElapsedMS: r.ElapsedMS, Changed: r.Changed, WarningCount: len(r.Warnings),
	}).clone()
}
func (m OptimizationMetrics) validateStored() error {
	if m.ComponentID == "" || m.ComponentVersion == "" || m.Reason == "" || m.WarningCount < 0 || math.IsNaN(m.ElapsedMS) || math.IsInf(m.ElapsedMS, 0) || m.ElapsedMS < 0 {
		return errors.New("invalid stored optimization receipt")
	}
	if err := m.Stage.Validate(); err != nil {
		return err
	}
	if err := m.Mode.Validate(); err != nil {
		return err
	}
	if err := m.Status.Validate(); err != nil {
		return err
	}
	if err := m.Lossiness.Validate(); err != nil {
		return err
	}
	for _, n := range []*big.Int{m.RawBytes, m.ProjectedBytes, m.TokensBeforeEstimate, m.TokensAfterEstimate} {
		if n == nil || n.Sign() < 0 {
			return errors.New("stored optimization receipt requires nonnegative metrics")
		}
	}
	return nil
}
func (event SessionEvent) OptimizationReceipt() (OptimizationMetrics, bool) {
	return event.optimizationReceipt.clone(), event.kind == EventOptimizationReceipt
}

const EventRequestOptimizationRejected SessionEventKind = "request_optimization_rejected"

type RequestOptimizationRejectedEvent struct {
	Reason string `json:"reason"`
}

func (event SessionEvent) RequestOptimizationRejected() (RequestOptimizationRejectedEvent, bool) {
	return event.requestRejected, event.kind == EventRequestOptimizationRejected
}
