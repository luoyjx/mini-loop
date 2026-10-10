package protocol

import (
	"encoding/json"
	"errors"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// Named fields enter concrete DTOs; only unrecognized metadata stays in the
// closed immutable extension object. Historical readers choose unbounded frames.
func optionalWireValue[T any](p *T, null bool) *wireOptional[T] {
	if p != nil {
		return &wireOptional[T]{value: p}
	}
	if null {
		return &wireOptional[T]{}
	}
	return nil
}
func marshalMetadataObject[T any](wire T, extra jsonvalue.Value) ([]byte, error) {
	data, err := json.Marshal(wire)
	if err != nil {
		return nil, err
	}
	if len(extra.Keys()) == 0 {
		return data, nil
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return nil, err
	}
	fields := make([]jsonvalue.Field, 0, len(value.Keys())+len(extra.Keys()))
	for _, key := range value.Keys() {
		v, _ := value.Lookup(key)
		fields = append(fields, jsonvalue.Field{Name: key, Value: v})
	}
	for _, key := range extra.Keys() {
		v, _ := extra.Lookup(key)
		fields = append(fields, jsonvalue.Field{Name: key, Value: v})
	}
	return jsonvalue.ObjectValue(fields).MarshalJSON()
}
func decodeMetadataObject[T any](data []byte, wire *T, known []string, bounded bool) (jsonvalue.Value, jsonvalue.Value, error) {
	if bounded && len(data) > MaxWireBytes {
		return jsonvalue.Value{}, jsonvalue.Value{}, errors.New("metadata JSON exceeds wire bound")
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return jsonvalue.Value{}, jsonvalue.Value{}, err
	}
	if value.Kind() != jsonvalue.Object {
		return jsonvalue.Value{}, jsonvalue.Value{}, errors.New("metadata must be an object")
	}
	if err := json.Unmarshal(data, wire); err != nil {
		return jsonvalue.Value{}, jsonvalue.Value{}, err
	}
	fields := []jsonvalue.Field{}
	for _, key := range value.Keys() {
		recognized := false
		for _, name := range known {
			if name == key {
				recognized = true
				break
			}
		}
		if !recognized {
			v, _ := value.Lookup(key)
			fields = append(fields, jsonvalue.Field{Name: key, Value: v})
		}
	}
	if len(fields) == 0 {
		return value, jsonvalue.Value{}, nil
	}
	return value, jsonvalue.ObjectValue(fields), nil
}
func copyMetadataPointer[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
