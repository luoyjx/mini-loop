package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"

	"github.com/luoyjx/mini-loop/go/efficiency"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// ObservationRawConfig snapshots post-mask persistence and child store limits.
// Shared runtimes and explicitly installed stores retain caller-owned lifetimes.
type ObservationRawConfig struct {
	Persist     bool
	MinBytes    *big.Int
	StoreLimits *efficiency.ArtifactLimits
}

func normalizedObservationRaw(config *ObservationRawConfig) (ObservationRawConfig, error) {
	limits := efficiency.DefaultArtifactLimits()
	result := ObservationRawConfig{Persist: true, MinBytes: big.NewInt(4096), StoreLimits: &limits}
	if config != nil {
		result.Persist = config.Persist
		if config.StoreLimits != nil {
			limits = config.StoreLimits.Clone()
			result.StoreLimits = &limits
		}
		if config.MinBytes != nil {
			result.MinBytes = new(big.Int).Set(config.MinBytes)
		}
	}
	if result.MinBytes.Sign() < 0 {
		return ObservationRawConfig{}, errors.New("observation raw byte threshold must be nonnegative")
	}
	if err := result.StoreLimits.Validate(); err != nil {
		return ObservationRawConfig{}, err
	}
	return result, nil
}

func (c ObservationRawConfig) clone() ObservationRawConfig {
	if c.MinBytes != nil {
		c.MinBytes = new(big.Int).Set(c.MinBytes)
	}
	if c.StoreLimits != nil {
		limits := c.StoreLimits.Clone()
		c.StoreLimits = &limits
	}
	return c
}

type sessionObservationProjector struct {
	session *Session
	span    SpanID
}

func observationContentType(content string) string {
	trimmed := strings.TrimLeftFunc(content, pytext.Space)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		if _, err := jsonvalue.Decode(content); err == nil {
			return "application/json"
		}
	}
	return "text/plain"
}

func observationTextDigest(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func observationRecoveryMarker(observation efficiency.ObservationConfig) string {
	if len(observation.ReducedBy) == 0 || observation.RawRef == nil {
		return ""
	}
	digest := "None"
	if observation.RawDigest != nil {
		digest = *observation.RawDigest
	}
	return "[token-efficiency projection; components=" + strings.Join(observation.ReducedBy, ",") +
		"; full masked result: read_token_artifact(raw_ref=\"" + *observation.RawRef + "\"); digest=" + digest + "]"
}

func (p sessionObservationProjector) projectToolOutcome(ctx context.Context, authority ToolAuthority, call ToolCall, outcome ToolOutcome) (projected ToolOutcome, err error) {
	s := p.session
	if s.tokenEfficiency == nil || outcome.Replayed || call.Name() == protocol.ToolReadTokenArtifact {
		return outcome, nil
	}
	projected = outcome
	defer func() {
		if recover() != nil {
			s.events.append(SessionEvent{kind: EventOptimizationStageError, optimizationError: OptimizationStageErrorEvent{efficiency.Observation, p.span, "RuntimePanic"}})
			projected, err = outcome, nil
		}
	}()
	input, err := call.Input.Observation()
	if err != nil {
		return outcome, err
	}
	query, err := input.MapStrings(func(value string) string { return maskedText(s.secrets, value) }).SortedPythonJSON()
	if err != nil {
		return outcome, err
	}
	config := efficiency.DefaultObservation(outcome.Output)
	config.ContentType = observationContentType(outcome.Output)
	if command, ok := outcome.CommandResult(); ok {
		config.ContentType = "text/x-command-output"
		exit := jsonvalue.NullValue()
		if command.ExitCode != nil {
			exit = jsonvalue.IntegerValue(int64(*command.ExitCode))
		}
		config.Metadata = []efficiency.ObservationMetadata{
			{Key: "exit_code", Value: exit},
			{Key: "timed_out", Value: jsonvalue.BoolValue(command.TimedOut)},
			{Key: "overflowed", Value: jsonvalue.BoolValue(command.Overflowed)},
			{Key: "duration_ms", Value: jsonvalue.IntegerValue(command.DurationMS)},
		}
	}
	observation, err := efficiency.NewObservation(config)
	if err != nil {
		return outcome, err
	}
	_, recoveryAvailable := s.gate.catalog.Lookup(protocol.ToolReadTokenArtifact)
	persist := s.observationRaw.Persist && authority.Mode != ModeReadonly && recoveryAvailable &&
		new(big.Int).SetUint64(uint64(len(outcome.Output))).Cmp(s.observationRaw.MinBytes) >= 0
	result, err := s.tokenEfficiency.ReduceObservation(ctx, observation, efficiency.ReductionOptions{Query: &query, BudgetTokens: big.NewInt(int64(s.tokenThreshold))}, persist)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return outcome, err
		}
		s.events.append(SessionEvent{kind: EventOptimizationStageError, optimizationError: OptimizationStageErrorEvent{efficiency.Observation, p.span, "RuntimeError"}})
		return outcome, nil
	}
	snapshot, err := result.Snapshot()
	if err != nil {
		return outcome, err
	}
	candidate := maskedText(s.secrets, snapshot.Observation.Content)
	if marker := observationRecoveryMarker(snapshot.Observation); marker != "" {
		candidate = marker + "\n" + candidate
	}
	candidate = maskedText(s.secrets, candidate)
	applied := false
	for _, receipt := range snapshot.Receipts {
		applied = applied || receipt.Status == efficiency.Applied
	}
	inflated := applied && len(candidate) >= len(outcome.Output)
	if inflated {
		if ref := snapshot.Observation.RawRef; ref != nil {
			if store := s.tokenEfficiency.RawStore(); store != nil {
				_, _ = store.Discard(*ref)
			}
		}
		candidate = outcome.Output
	}
	for _, receipt := range snapshot.Receipts {
		if receipt.Status == efficiency.Applied {
			receipt.ProjectedBytes = new(big.Int).SetUint64(uint64(len(candidate)))
			receipt.TokensAfterEstimate = efficiency.EstimateObservationTokens(candidate)
			receipt.OutputDigest = observationTextDigest(candidate)
			receipt.Changed = receipt.InputDigest != receipt.OutputDigest
			if inflated {
				receipt.Status, receipt.Reason = efficiency.Degraded, "recovery_envelope_inflation"
				receipt.RawRef, receipt.RawDigest = nil, nil
			}
		}
		metrics := optimizationMetrics(receipt)
		metrics.ParentSpanID = &p.span
		s.events.append(SessionEvent{kind: EventOptimizationReceipt, optimizationReceipt: metrics})
	}
	outcome.Output = candidate
	return outcome, nil
}
