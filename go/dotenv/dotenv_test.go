package dotenv

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestPinnedSourceParsingResolutionAndPartialApplication(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-dotenv.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Version string
		Cases   []struct {
			Name, Text  string
			Environment Environment
			Override    bool
			Bindings    []Binding
			Values      []Variable
			Application
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Version != SourceVersion || len(fixture.Cases) != 48 {
		t.Fatal("Source dotenv corpus changed")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name+map[bool]string{true: "-override", false: "-preserve"}[row.Override], func(t *testing.T) {
			before, _ := json.Marshal(row)
			bindings, err := Parse(row.Text)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(bindings, row.Bindings) {
				t.Fatalf("bindings: %#v; want %#v", bindings, row.Bindings)
			}
			values := Resolve(bindings, row.Environment, row.Override)
			if !reflect.DeepEqual(values, row.Values) {
				t.Fatalf("values: %#v; want %#v", values, row.Values)
			}
			application := Apply(values, row.Environment, row.Override)
			if !reflect.DeepEqual(application, row.Application) {
				t.Fatalf("application: %#v; want %#v", application, row.Application)
			}
			after, _ := json.Marshal(row)
			if string(before) != string(after) {
				t.Fatal("input containers mutated")
			}
			application.Environment["sentinel"] = "changed"
			if _, exists := row.Environment["sentinel"]; exists {
				t.Fatal("application aliases input environment")
			}
			if len(values) > 0 && values[0].Value != nil {
				*values[0].Value = "changed"
				if !reflect.DeepEqual(bindings, row.Bindings) {
					t.Fatal("resolved values alias raw bindings")
				}
			}
		})
	}
}

func TestScalarBoundaryAndEmptyInput(t *testing.T) {
	if _, err := Parse(string([]byte{0xff})); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
	bindings, err := Parse("")
	if err != nil || len(bindings) != 0 {
		t.Fatal(bindings, err)
	}
	result := Apply(Resolve(bindings, nil, true), nil, true)
	if result.Loaded == nil || *result.Loaded || result.Failure != nil || result.Environment == nil {
		t.Fatal(result)
	}
}
