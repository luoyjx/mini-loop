package jsonvalue

// Select projects only named wire fields, preserving source member order and
// values. Unused legacy values never cross a stricter typed decoder boundary.
func (v Value) Select(names ...string) Value {
	fields := make([]Field, 0, len(names))
	for _, name := range v.Keys() {
		for _, selected := range names {
			if name == selected {
				child, _ := v.Lookup(name)
				fields = append(fields, Field{Name: name, Value: child})
				break
			}
		}
	}
	return ObjectValue(fields)
}
