package efficiency

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type observationSource struct {
	Observations []struct {
		Name   string
		Input  ObservationConfig
		Output *ObservationConfig
		Error  *sourceFault
	}
	Reductions []struct {
		Name   string
		Input  ReductionConfig
		Output *ReductionConfig
		Error  *sourceFault
	}
	Descriptors []DescriptorConfig
	Reducers    []struct {
		Name, Content string
		Outputs       []struct {
			Component string
			Output    ReductionConfig
		}
	}
}

func readObservationSource(t *testing.T) observationSource {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-efficiency-observations.json")
	if err != nil {
		t.Fatal(err)
	}
	var source observationSource
	if err = json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	if len(source.Observations) != 12 || len(source.Reductions) != 8 || len(source.Reducers) != 69 || len(source.Descriptors) != 2 {
		t.Fatal("Source inventory changed")
	}
	return source
}
func TestObservationConstructorsMatchSource(t *testing.T) {
	for _, row := range readObservationSource(t).Observations {
		t.Run(row.Name, func(t *testing.T) {
			observation, err := NewObservation(row.Input)
			if row.Error != nil {
				if err == nil || err.Error() != row.Error.Message {
					t.Fatal(err, row.Error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := observation.Snapshot()
			if err != nil || !reflect.DeepEqual(snapshot, *row.Output) {
				t.Fatal(snapshot, row.Output, err)
			}
			data, err := json.Marshal(observation)
			if err != nil {
				t.Fatal(err)
			}
			var projected ObservationConfig
			if err = json.Unmarshal(data, &projected); err != nil || !reflect.DeepEqual(projected, *row.Output) {
				t.Fatal("wire differs", string(data), err)
			}
		})
	}
}
func TestReductionConstructorsMatchSource(t *testing.T) {
	for _, row := range readObservationSource(t).Reductions {
		t.Run(row.Name, func(t *testing.T) {
			reduction, err := NewReduction(row.Input)
			if row.Error != nil {
				if err == nil || err.Error() != row.Error.Message {
					t.Fatal(err, row.Error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := reduction.Snapshot()
			if err != nil || !reflect.DeepEqual(snapshot, *row.Output) {
				t.Fatal(snapshot, row.Output, err)
			}
		})
	}
}
func TestBuiltinObservationReducersMatchSource(t *testing.T) {
	source := readObservationSource(t)
	reducers := []ObservationReducer{PassthroughObservationReducer{}, DeterministicLosslessReducer{}}
	for i, reducer := range reducers {
		descriptor, err := reducer.Descriptor().Snapshot()
		if err != nil || !reflect.DeepEqual(descriptor, source.Descriptors[i]) {
			t.Fatal("descriptor differs", descriptor, err)
		}
	}
	query := "unused query"
	options := ReductionOptions{Query: &query, BudgetTokens: big.NewInt(1)}
	for _, row := range source.Reducers {
		t.Run(row.Name, func(t *testing.T) {
			observation, err := NewObservation(DefaultObservation(row.Content))
			if err != nil {
				t.Fatal(err)
			}
			for i, reducer := range reducers {
				result, err := reducer.Reduce(context.Background(), observation, options)
				if err != nil {
					t.Fatal(err)
				}
				snapshot, err := result.Snapshot()
				if err != nil || !reflect.DeepEqual(snapshot, row.Outputs[i].Output) {
					t.Fatalf("%s differs: got %+v want %+v err %v", row.Outputs[i].Component, snapshot, row.Outputs[i].Output, err)
				}
				again, err := reducer.Reduce(context.Background(), observation, options)
				if err != nil {
					t.Fatal(err)
				}
				repeat, err := again.Snapshot()
				if err != nil || !reflect.DeepEqual(repeat, snapshot) {
					t.Fatal("nondeterministic reducer", repeat, err)
				}
			}
		})
	}
}
func TestObservationDetachedMetadataAndReductionWarnings(t *testing.T) {
	ref, digest := "legacy ref", "legacy digest"
	array := []jsonvalue.Value{jsonvalue.TextValue("first")}
	config := DefaultObservation("masked")
	config.ReducedBy = []string{"one", "one", "two"}
	config.RawRef = &ref
	config.RawDigest = &digest
	config.Metadata = []ObservationMetadata{{"items", jsonvalue.ArrayValue(array)}}
	observation, err := NewObservation(config)
	if err != nil {
		t.Fatal(err)
	}
	array[0] = jsonvalue.TextValue("replaced")
	config.ReducedBy[0] = "changed"
	config.Metadata[0].Key = "changed"
	ref = "changed"
	digest = "changed"
	snapshot, err := observation.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot.ReducedBy, []string{"one", "two"}) || *snapshot.RawRef != "legacy ref" || snapshot.Metadata[0].Key != "items" {
		t.Fatal("caller config aliases")
	}
	values, ok := snapshot.Metadata[0].Value.Array()
	if !ok {
		t.Fatal("metadata shape changed")
	}
	text, ok := values[0].Text()
	if !ok || text != "first" {
		t.Fatal("metadata aliases array input")
	}
	values[0] = jsonvalue.TextValue("changed")
	snapshot.ReducedBy[0] = "changed"
	*snapshot.RawRef = "changed"
	snapshot.Metadata[0].Key = "changed"
	fresh, err := observation.Snapshot()
	if err != nil || fresh.ReducedBy[0] != "one" || *fresh.RawRef != "legacy ref" || fresh.Metadata[0].Key != "items" {
		t.Fatal("snapshot aliases", err)
	}
	warnings := []string{"folded:2"}
	reduction, err := NewReduction(ReductionConfig{Content: "text", Warnings: warnings})
	if err != nil {
		t.Fatal(err)
	}
	warnings[0] = "changed"
	projected, err := reduction.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	projected.Warnings[0] = "changed"
	unchanged, err := reduction.Snapshot()
	if err != nil || unchanged.Warnings[0] != "folded:2" {
		t.Fatal("warnings alias", err)
	}
	for _, zero := range []json.Marshaler{MaskedObservation{}, ObservationReduction{}} {
		if _, err = zero.MarshalJSON(); err == nil {
			t.Fatal("zero object marshaled")
		}
	}
}
func TestStatelessObservationReducerConcurrentAndCancelled(t *testing.T) {
	reducer := DeterministicLosslessReducer{}
	observation, err := NewObservation(DefaultObservation("building\nbuilding\nbuilding\n"))
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for i := 0; i < 32; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			result, e := reducer.Reduce(context.Background(), observation, ReductionOptions{})
			if e != nil {
				t.Error(e)
				return
			}
			s, e := result.Snapshot()
			if e != nil || s.Content != "building\n[repeated 2 additional times]\n" {
				t.Error(s, e)
			}
		}()
	}
	group.Wait()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = reducer.Reduce(ctx, observation, ReductionOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = reducer.Reduce(context.Background(), MaskedObservation{}, ReductionOptions{}); err == nil {
		t.Fatal("zero input admitted")
	}
}
