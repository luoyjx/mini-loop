package jsonvalue

// Field is a named object member. Object construction retains insertion order;
// repeated keys replace their value at the first key's position, as Python does.
type Field struct {
	Name  string
	Value Value
}

func NullValue() Value               { return Value{} }
func TextValue(text string) Value    { return Value{kind: Text, text: text} }
func BoolValue(value bool) Value     { return Value{kind: Boolean, boolean: value} }
func FloatValue(value float64) Value { return Value{kind: Float, number: value} }
func ArrayValue(items []Value) Value { return Value{kind: Array, items: append([]Value{}, items...)} }
func ObjectValue(fields []Field) Value {
	members := make([]member, 0, len(fields))
	positions := make(map[string]int, len(fields))
	for _, field := range fields {
		if index, exists := positions[field.Name]; exists {
			members[index].value = field.Value
		} else {
			positions[field.Name] = len(members)
			members = append(members, member{field.Name, field.Value})
		}
	}
	return Value{kind: Object, members: members}
}

// MapStrings masks the structure before JSON escaping. Key collisions retain
// the last value in the first key's position. No raw fallback exists.
func (v Value) MapStrings(mask func(string) string) Value {
	if mask == nil {
		return v
	}
	switch v.kind {
	case Text:
		return TextValue(mask(v.text))
	case Array:
		items := make([]Value, len(v.items))
		for i, item := range v.items {
			items[i] = item.MapStrings(mask)
		}
		return ArrayValue(items)
	case Object:
		fields := make([]Field, len(v.members))
		for i, m := range v.members {
			fields[i] = Field{mask(m.name), m.value.MapStrings(mask)}
		}
		return ObjectValue(fields)
	default:
		return v
	}
}

// RuneCount includes legacy surrogatepass code points as single Python chars.
func RuneCount(text string) int {
	count := 0
	for len(text) > 0 {
		_, n := textRune(text)
		text = text[n:]
		count++
	}
	return count
}

// AppendLegacySpaced matches json.dumps' default separators for persisted rows.
func AppendLegacySpaced(value Value) ([]byte, error) {
	compact, err := AppendLegacy(nil, value)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(compact))
	quoted, escaped := false, false
	for _, c := range compact {
		out = append(out, c)
		if quoted {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
		} else if c == '"' {
			quoted = true
		} else if c == ',' || c == ':' {
			out = append(out, ' ')
		}
	}
	return out, nil
}

// UnmarshalJSON lowers ordinary boundary input immediately into closed variants.
func (v *Value) UnmarshalJSON(data []byte) error {
	value, err := Decode(string(data))
	if err == nil {
		*v = value
	}
	return err
}
