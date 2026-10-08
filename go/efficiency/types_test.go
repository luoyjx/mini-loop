package efficiency

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type sourceFault struct{ Kind, Message string }
type sourceTypes struct {
	Descriptors []struct {
		Name   string
		Input  DescriptorConfig
		Output *DescriptorConfig
		Error  *sourceFault
	}
	Receipts []struct {
		Name   string
		Input  ReceiptConfig
		Output *ReceiptProjection
		Error  *sourceFault
	}
}

func readSourceTypes(t *testing.T) sourceTypes {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-efficiency-types.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture sourceTypes
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Descriptors) != 27 || len(fixture.Receipts) != 41 {
		t.Fatal("Source type inventory changed")
	}
	return fixture
}

func TestDescriptorsMatchSource(t *testing.T) {
	for _, row := range readSourceTypes(t).Descriptors {
		t.Run(row.Name, func(t *testing.T) {
			value, err := NewDescriptor(row.Input)
			if row.Error != nil {
				if err == nil || err.Error() != row.Error.Message {
					t.Fatal(err, row.Error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := value.Snapshot()
			if err != nil || !reflect.DeepEqual(snapshot, *row.Output) {
				t.Fatal("descriptor differs", snapshot, row.Output, err)
			}
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			var decoded DescriptorConfig
			if err := json.Unmarshal(data, &decoded); err != nil || !reflect.DeepEqual(decoded, *row.Output) {
				t.Fatal("descriptor wire projection differs", string(data), err)
			}
		})
	}
}

func TestReceiptsMatchSource(t *testing.T) {
	for _, row := range readSourceTypes(t).Receipts {
		t.Run(row.Name, func(t *testing.T) {
			value, err := NewReceipt(row.Input)
			if row.Error != nil {
				if err == nil || err.Error() != row.Error.Message {
					t.Fatal(err, row.Error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			projection, err := value.Projection()
			if err != nil || !reflect.DeepEqual(projection, *row.Output) || value.Changed() != row.Output.Changed {
				t.Fatal("receipt differs", projection, row.Output, err)
			}
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			var decoded ReceiptProjection
			if err := json.Unmarshal(data, &decoded); err != nil || !reflect.DeepEqual(decoded, *row.Output) {
				t.Fatal("receipt wire projection differs", string(data), err)
			}
		})
	}
}

func TestObjectsDetachMutableConstructionAndProjectionValues(t *testing.T) {
	fixture := readSourceTypes(t)
	descriptorInput := fixture.Descriptors[0].Input
	descriptor, err := NewDescriptor(descriptorInput)
	if err != nil {
		t.Fatal(err)
	}
	descriptorInput.TimeoutMS.SetInt64(-1)
	descriptorInput.ContentTypes[0] = "mutated"
	descriptorInput.Capabilities[0] = "mutated"
	first, _ := descriptor.Snapshot()
	first.TimeoutMS.SetInt64(-1)
	first.ContentTypes[0] = "mutated"
	first.Capabilities[0] = "mutated"
	second, _ := descriptor.Snapshot()
	if !reflect.DeepEqual(second, *fixture.Descriptors[0].Output) {
		t.Fatal("descriptor mutation escaped")
	}
	input := fixture.Receipts[0].Input
	receipt, err := NewReceipt(input)
	if err != nil {
		t.Fatal(err)
	}
	input.RawBytes.SetInt64(-1)
	input.ProjectedBytes.SetInt64(-1)
	input.TokensBeforeEstimate.SetInt64(-1)
	input.TokensAfterEstimate.SetInt64(-1)
	*input.RawRef = "mutated"
	*input.RawDigest = "mutated"
	input.Warnings[0] = "mutated"
	projection, _ := receipt.Projection()
	projection.RawBytes.SetInt64(-1)
	projection.ProjectedBytes.SetInt64(-1)
	projection.TokensBeforeEstimate.SetInt64(-1)
	projection.TokensAfterEstimate.SetInt64(-1)
	*projection.RawRef = "mutated"
	*projection.RawDigest = "mutated"
	projection.Warnings[0] = "mutated"
	again, _ := receipt.Projection()
	if !reflect.DeepEqual(again, *fixture.Receipts[0].Output) {
		t.Fatal("receipt mutation escaped")
	}
}

func TestZeroObjectsAndNonfiniteTimingCannotEnterWire(t *testing.T) {
	if _, err := json.Marshal(Descriptor{}); err == nil {
		t.Fatal("zero descriptor admitted")
	}
	if _, err := json.Marshal(Receipt{}); err == nil {
		t.Fatal("zero receipt admitted")
	}
	if _, err := NewDescriptor(DefaultDescriptor("component", "1", Observation)); err != nil {
		t.Fatal(err)
	}
	base := readSourceTypes(t).Receipts[0].Input
	for _, elapsed := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		base.ElapsedMS = elapsed
		if _, err := NewReceipt(base); err == nil || err.Error() != "elapsed_ms must be finite and non-negative" {
			t.Fatal("unsafe timing admitted", err)
		}
	}
}

func TestWarningLengthUsesPythonSurrogatepassCharacters(t *testing.T) {
	// The runtime's closed text representation preserves lone surrogate codepoints.
	value, err := jsonvalue.Decode(`"\ud800"`)
	if err != nil {
		t.Fatal(err)
	}
	text, _ := value.Text()
	var warnings []string
	for count := 256; count <= 257; count++ {
		warning := ""
		for range count {
			warning += text
		}
		warnings = []string{warning}
		err := validateWarnings(warnings)
		want := "warnings must contain bounded machine-readable codes"
		if count == 257 {
			want = "warning entries must be at most 256 characters"
		}
		if err == nil || err.Error() != want {
			t.Fatal("warning character boundary differs", count, err)
		}
	}
}
