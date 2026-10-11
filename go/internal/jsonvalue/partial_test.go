package jsonvalue

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestPartialDecoderMatchesSDKAtEveryDeliveredDelta(t *testing.T) {
	data, err := os.ReadFile("../../testdata/python-provider-partial-input.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Kind, Name string
			Chunks     []string
			Snapshots  []string `json:"snapshots_json"`
			Expected   string   `json:"expected_json"`
			Error      *struct{ Class, Message string }
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 98 {
		t.Fatal("SDK inventory changed")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Kind+"/"+row.Name, func(t *testing.T) {
			buffer := ""
			current, _ := Decode(`{"command":"true"}`)
			rejected := false
			for i, chunk := range row.Chunks {
				buffer += chunk
				if buffer != "" {
					var err error
					current, err = DecodePartial(buffer)
					if err != nil {
						if row.Error == nil || i != len(row.Snapshots) {
							t.Fatalf("wrong rejection at delta %d: %v", i, err)
						}
						rejected = true
						break
					}
				}
				if i >= len(row.Snapshots) {
					t.Fatal("SDK refused a delta accepted by native partial decoder")
				}
				expected := sdkInput(t, row.Snapshots[i])
				// Legacy encoding also compares NaN; Python equality deliberately treats it
				// as unequal to itself. Both codecs retain the observed spelling here.
				a := partialLegacy(t, current)
				b := partialLegacy(t, expected)
				if !current.PythonEqual(expected) && a != b {
					t.Fatalf("delta %d differs: %s vs %s", i, a, b)
				}
			}
			if rejected != (row.Error != nil) {
				t.Fatal("SDK rejection outcome differs")
			}
			if !rejected {
				expected := sdkInput(t, row.Expected)
				a := partialLegacy(t, current)
				b := partialLegacy(t, expected)
				if !current.PythonEqual(expected) && a != b {
					t.Fatalf("stopped input differs: %s vs %s", a, b)
				}
			}
		})
	}
}

func sdkInput(t *testing.T, raw string) Value {
	t.Helper()
	value, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := value.Array()
	if !ok || len(array) != 1 {
		t.Fatal("unexpected SDK content")
	}
	input, ok := array[0].Lookup("input")
	if !ok {
		t.Fatal("SDK input missing")
	}
	return input
}

func TestPartialDecoderDoesNotRelaxStrictDecode(t *testing.T) {
	for _, raw := range []string{`{"a":1`, `{"a":1]`, `{}trailing`, `[1,`} {
		if _, err := DecodePartial(raw); err != nil {
			t.Fatal(raw, err)
		}
		if _, err := Decode(raw); err == nil {
			t.Fatal("partial mode leaked into strict archival decoder", raw)
		}
	}
	if _, err := DecodePartial(strings.Repeat("[", 201) + "0"); err == nil {
		t.Fatal("partial decoder lost SDK depth bound")
	}
}

func partialLegacy(t *testing.T, value Value) string {
	t.Helper()
	data, err := AppendLegacy(nil, value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPartialDecoderMatchesPinnedJiterEdges(t *testing.T) {
	data, err := os.ReadFile("../../testdata/python-provider-partial-json.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Raw string
			Expected  string `json:"expected_json"`
			Error     *struct{ Class, Message string }
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 42 {
		t.Fatal("jiter inventory changed")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			got, err := DecodePartial(row.Raw)
			if row.Error != nil {
				if err == nil {
					t.Fatal("jiter error accepted", row.Raw)
				}
				return
			}
			if err != nil {
				t.Fatal("jiter value refused", err)
			}
			expected, err := Decode(row.Expected)
			if err != nil {
				t.Fatal(err)
			}
			if !got.PythonEqual(expected) && partialLegacy(t, got) != partialLegacy(t, expected) {
				t.Fatal("jiter edge differs", partialLegacy(t, got), row.Expected)
			}
		})
	}
}
