package protocol

import (
	"errors"
	"math"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type UsageMetadataKind uint8

const (
	UsageMetadataNull UsageMetadataKind = iota
	UsageMetadataInteger
	UsageMetadataFloat
	UsageMetadataBoolean
	UsageMetadataText
	UsageMetadataArray
	UsageMetadataObject
)

// UsageMetadataObservation retains wrong-shaped known SDK metadata. Unlike
// UsageCount it has no metering conversion; integer lexemes and finite floats
// are observations even beyond the native count range. Containers are immutable.
type UsageMetadataObservation struct {
	kind    UsageMetadataKind
	integer string
	number  float64
	boolean bool
	text    string
	items   []UsageMetadataObservation
	members []usageMetadataMember
}

type usageMetadataMember struct {
	name  string
	value UsageMetadataObservation
}

func usageMetadataFromValue(value jsonvalue.Value) (UsageMetadataObservation, error) {
	var observation UsageMetadataObservation
	switch value.Kind() {
	case jsonvalue.Null:
	case jsonvalue.Integer:
		observation.kind = UsageMetadataInteger
		observation.integer, _ = value.Integer()
	case jsonvalue.Float:
		observation.kind = UsageMetadataFloat
		observation.number, _ = value.Float()
		if math.IsNaN(observation.number) || math.IsInf(observation.number, 0) {
			return observation, errors.New("usage metadata requires finite observations")
		}
	case jsonvalue.Boolean:
		observation.kind = UsageMetadataBoolean
		observation.boolean, _ = value.Bool()
	case jsonvalue.Text:
		observation.kind = UsageMetadataText
		observation.text, _ = value.Text()
	case jsonvalue.Array:
		observation.kind = UsageMetadataArray
		values, _ := value.Array()
		for _, value := range values {
			next, err := usageMetadataFromValue(value)
			if err != nil {
				return observation, err
			}
			observation.items = append(observation.items, next)
		}
	case jsonvalue.Object:
		observation.kind = UsageMetadataObject
		for _, key := range value.Keys() {
			value, _ := value.Lookup(key)
			next, err := usageMetadataFromValue(value)
			if err != nil {
				return observation, err
			}
			observation.members = append(observation.members, usageMetadataMember{key, next})
		}
	}
	return observation, nil
}

func (v UsageMetadataObservation) jsonValue() (jsonvalue.Value, error) {
	switch v.kind {
	case UsageMetadataNull:
		return jsonvalue.NullValue(), nil
	case UsageMetadataInteger:
		return jsonvalue.Decode(v.integer)
	case UsageMetadataFloat:
		return jsonvalue.FloatValue(v.number), nil
	case UsageMetadataBoolean:
		return jsonvalue.BoolValue(v.boolean), nil
	case UsageMetadataText:
		return jsonvalue.TextValue(v.text), nil
	case UsageMetadataArray:
		items := make([]jsonvalue.Value, len(v.items))
		for i, item := range v.items {
			next, err := item.jsonValue()
			if err != nil {
				return jsonvalue.Value{}, err
			}
			items[i] = next
		}
		return jsonvalue.ArrayValue(items), nil
	case UsageMetadataObject:
		fields := make([]jsonvalue.Field, len(v.members))
		for i, member := range v.members {
			next, err := member.value.jsonValue()
			if err != nil {
				return jsonvalue.Value{}, err
			}
			fields[i] = jsonvalue.Field{Name: member.name, Value: next}
		}
		return jsonvalue.ObjectValue(fields), nil
	}
	return jsonvalue.Value{}, errors.New("unknown usage metadata observation")
}

func (v UsageMetadataObservation) MarshalJSON() ([]byte, error) {
	value, err := v.jsonValue()
	if err != nil {
		return nil, err
	}
	return value.MarshalJSON()
}

func (v *UsageMetadataObservation) UnmarshalJSON(data []byte) error {
	if len(data) > MaxWireBytes {
		return errors.New("usage metadata exceeds wire bound")
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return err
	}
	next, err := usageMetadataFromValue(value)
	if err != nil {
		return err
	}
	*v = next
	return nil
}

func (v UsageMetadataObservation) MapStrings(mask func(string) string) UsageMetadataObservation {
	if mask == nil {
		return v
	}
	switch v.kind {
	case UsageMetadataText:
		v.text = mask(v.text)
	case UsageMetadataArray:
		items := make([]UsageMetadataObservation, len(v.items))
		for i, item := range v.items {
			items[i] = item.MapStrings(mask)
		}
		v.items = items
	case UsageMetadataObject:
		members := make([]usageMetadataMember, len(v.members))
		for i, member := range v.members {
			members[i] = usageMetadataMember{mask(member.name), member.value.MapStrings(mask)}
		}
		v.members = members
	}
	return v
}

// Object metadata follows its named model decoder. Other shapes retain a named
// observation, including objects nested inside wrong-shaped array observations.
func nonObjectUsageMetadata(data []byte) (*UsageMetadataObservation, error) {
	if len(data) > MaxWireBytes {
		return nil, errors.New("usage metadata exceeds wire bound")
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return nil, err
	}
	if value.Kind() == jsonvalue.Object {
		return nil, nil
	}
	observation, err := usageMetadataFromValue(value)
	if err != nil {
		return nil, err
	}
	return &observation, nil
}

func mapUsageMetadata(p *UsageMetadataObservation, mask func(string) string) *UsageMetadataObservation {
	if p == nil {
		return nil
	}
	value := p.MapStrings(mask)
	return &value
}

// SDK BaseModel fallback constructs mapping entries at the top level of a list.
// It supplies missing model fields but does not validate/normalize their counts.
// Nested lists and non-mapping entries remain their original observations.
func sdkMetadataArrayDefaults(p *UsageMetadataObservation, names ...string) *UsageMetadataObservation {
	if p == nil || p.kind != UsageMetadataArray {
		return p
	}
	next := *p
	next.items = append([]UsageMetadataObservation{}, p.items...)
	for i, item := range next.items {
		if item.kind != UsageMetadataObject {
			continue
		}
		item.members = append([]usageMetadataMember{}, item.members...)
		for _, name := range names {
			present := false
			for _, member := range item.members {
				if member.name == name {
					present = true
					break
				}
			}
			if !present {
				item.members = append(item.members, usageMetadataMember{name, UsageMetadataObservation{}})
			}
		}
		next.items[i] = item
	}
	return &next
}

func validSDKRequiredCounts(counts ...*UsageCount) bool {
	for _, count := range counts {
		if count == nil {
			return false
		}
		if _, ok := count.sdkInteger(); !ok {
			return false
		}
	}
	return true
}

func normalizeSDKRequiredCounts(counts ...*UsageCount) {
	if !validSDKRequiredCounts(counts...) {
		return
	}
	for _, count := range counts {
		*count, _ = count.sdkInteger()
	}
}

func validSDKMetadataModel(observation *UsageMetadataObservation, counts ...*UsageCount) bool {
	if observation != nil {
		return observation.kind == UsageMetadataNull
	}
	return validSDKRequiredCounts(counts...)
}
