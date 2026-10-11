package benchmark

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"os"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

func canonicalJSON(t *testing.T, raw json.RawMessage) json.RawMessage {
	t.Helper()
	raw = bytes.TrimSpace(raw)
	if !json.Valid(raw) {
		t.Fatalf("invalid JSON: %s", raw)
	}
	switch raw[0] {
	case '{':
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		for key, value := range fields {
			fields[key] = canonicalJSON(t, value)
		}
		out, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		return out
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			t.Fatal(err)
		}
		for i, value := range items {
			items[i] = canonicalJSON(t, value)
		}
		out, err := json.Marshal(items)
		if err != nil {
			t.Fatal(err)
		}
		return out
	case '"':
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			t.Fatal(err)
		}
		out, err := json.Marshal(text)
		if err != nil {
			t.Fatal(err)
		}
		return out
	default:
		return raw
	}
}
func checkJSON[T any](t *testing.T, actual T, expected json.RawMessage) {
	t.Helper()
	raw, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonicalJSON(t, raw), canonicalJSON(t, expected)) {
		t.Fatalf("got %s; want %s", raw, expected)
	}
}

func TestActualPythonStatisticsAndMotion(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-benchmark-statistics.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Aggregates []struct {
			Name   string
			Runs   [][]TaskResult
			Result json.RawMessage
		}
		Comparisons []struct {
			Name                string
			Baseline, Candidate []TaskResult
			Result              json.RawMessage
			Error               *string
		}
		Behaviors []struct {
			Name     string
			Messages []protocol.Message
			Result   json.RawMessage
		}
		Rounding []struct {
			Value  float64
			Digits int
			Result json.RawMessage
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Aggregates) != 10 || len(fixture.Comparisons) != 15 || len(fixture.Behaviors) != 6 || len(fixture.Rounding) != 20 {
		t.Fatal("source case inventory changed")
	}
	for _, row := range fixture.Aggregates {
		t.Run("aggregate/"+row.Name, func(t *testing.T) {
			actual, err := AggregateRuns(row.Runs)
			if err != nil {
				t.Fatal(err)
			}
			checkJSON(t, actual, row.Result)
		})
	}
	for _, row := range fixture.Comparisons {
		t.Run("compare/"+row.Name, func(t *testing.T) {
			actual, err := Compare(row.Baseline, row.Candidate)
			if row.Error != nil {
				if err == nil || err.Error() != *row.Error {
					t.Fatal(err, *row.Error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			checkJSON(t, actual, row.Result)
		})
	}
	for _, row := range fixture.Behaviors {
		t.Run("behavior/"+row.Name, func(t *testing.T) { checkJSON(t, BehavioralMetrics(row.Messages), row.Result) })
	}
	for _, row := range fixture.Rounding {
		number, err := Float(roundDecimal(row.Value, row.Digits))
		if err != nil {
			t.Fatal(err)
		}
		checkJSON(t, number, row.Result)
	}
}

func TestMetricWireRefusalAndFiniteArithmetic(t *testing.T) {
	for _, spelling := range []string{"0", "-9223372036854775808", "9223372036854775808", "true", "false", "1.0", "-0.0", "1e+16", "1e-05"} {
		var number Number
		if err := json.Unmarshal([]byte(spelling), &number); err != nil {
			t.Fatal(spelling, err)
		}
		checkJSON(t, number, json.RawMessage(spelling))
	}
	for _, raw := range []string{"1e400", "NaN", "Infinity", "null", `"1"`, "{}", "[]", "+1", "01"} {
		number := Integer(7)
		if err := number.UnmarshalJSON([]byte(raw)); !errors.Is(err, ErrMetric) {
			t.Fatal(raw, err)
		}
		if integer, ok := number.Int64(); !ok || integer != 7 {
			t.Fatal("failed decode mutated number", raw)
		}
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := Float(value); !errors.Is(err, ErrMetric) {
			t.Fatal(err)
		}
	}
	max := Integer(math.MaxInt64)
	one := Integer(1)
	comparison, err := Compare([]TaskResult{{Task: "task", Measurements: Measurements{Rounds: &max}}, {Task: "task", Measurements: Measurements{Rounds: &one}}}, []TaskResult{{Task: "task"}})
	if err != nil {
		t.Fatal(err)
	}
	large, ok := comparison.Dimensions[Rounds].Baseline.BigInt()
	if !ok || large.String() != "9223372036854775808" {
		t.Fatal("large sum lost precision", large)
	}
	large.SetInt64(0)
	if comparison.Dimensions[Rounds].Baseline.Float64() == 0 {
		t.Fatal("BigInt accessor aliases state")
	}
	tooLarge := new(big.Int).Exp(big.NewInt(10), big.NewInt(integerDigitLimit), nil)
	if _, err := BigInteger(tooLarge).MarshalJSON(); !errors.Is(err, ErrMetric) {
		t.Fatal("integer digit bound bypassed", err)
	}
	if _, err := Compare([]TaskResult{{Task: "task", Measurements: Measurements{Rounds: numberPointer(BigInteger(tooLarge))}}}, []TaskResult{{Task: "task"}}); !errors.Is(err, ErrMetric) {
		t.Fatal("overflowing float conversion accepted", err)
	}
	huge, _ := Float(math.MaxFloat64)
	if _, err := AggregateRuns([][]TaskResult{{{Task: "task", Measurements: Measurements{Rounds: &huge}}}, {{Task: "task", Measurements: Measurements{Rounds: &huge}}}}); !errors.Is(err, ErrMetric) {
		t.Fatal("nonfinite midpoint accepted", err)
	}
	tiny, _ := Float(math.SmallestNonzeroFloat64)
	if _, err := Compare([]TaskResult{{Task: "task", Measurements: Measurements{Rounds: &tiny}}}, []TaskResult{{Task: "task", Measurements: Measurements{Rounds: &huge}}}); !errors.Is(err, ErrMetric) {
		t.Fatal("nonfinite delta accepted", err)
	}
}

func numberPointer(value Number) *Number { return &value }

func TestAggregateDetachedFieldsAndDimensionOrder(t *testing.T) {
	metric := Integer(3)
	text := "failure"
	input := [][]TaskResult{{{Arm: "arm", Task: "task", Error: &text, Measurements: Measurements{Rounds: &metric}}}}
	out, err := AggregateRuns(input)
	if err != nil {
		t.Fatal(err)
	}
	text = "mutated"
	metric = Integer(9)
	if *out[0].Error != "failure" || out[0].Rounds.Float64() != 3 {
		t.Fatal("aggregate aliases caller state", out)
	}
	*out[0].Error = "caller mutation"
	*out[0].Rounds = Integer(20)
	if text != "mutated" || metric.Float64() != 9 {
		t.Fatal("returned fields mutate source")
	}
	order := Dimensions()
	order[0] = "unknown"
	if Dimensions()[0] != Duration {
		t.Fatal("dimension order aliases caller")
	}
}
