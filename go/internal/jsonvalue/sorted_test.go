package jsonvalue

import (
	"reflect"
	"testing"
)

func TestSortedProjectionPreservesOriginalOrderAndDetachedArrays(t *testing.T) {
	value, err := Decode(`{"z":{"b":1,"a":2},"a":[{"界":3,"z":4}],"n":null}`)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := value.MarshalJSON()
	sorted := value.Sorted()
	got, err := sorted.MarshalJSON()
	if err != nil || string(got) != `{"a":[{"z":4,"\u754c":3}],"n":null,"z":{"a":2,"b":1}}` {
		t.Fatal(string(got), err)
	}
	after, _ := value.MarshalJSON()
	if string(after) != string(before) || !reflect.DeepEqual(value.Keys(), []string{"z", "a", "n"}) {
		t.Fatal("sorted projection mutated source", string(before), string(after))
	}
	nested, _ := value.Lookup("z")
	if !reflect.DeepEqual(nested.Keys(), []string{"b", "a"}) {
		t.Fatal("nested source order changed")
	}
	array, _ := sorted.Lookup("a")
	items, _ := array.Array()
	items[0] = NullValue()
	again, _ := sorted.MarshalJSON()
	if string(again) != string(got) {
		t.Fatal("array accessor exposed projection")
	}
}
