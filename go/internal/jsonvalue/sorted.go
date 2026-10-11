package jsonvalue

import "sort"

// Sorted returns an immutable recursive key-order projection for Python's
// sort_keys identity. Arrays retain order; decoded source objects are unchanged.
func (v Value) Sorted() Value {
	switch v.kind {
	case Array:
		items := make([]Value, len(v.items))
		for i, item := range v.items {
			items[i] = item.Sorted()
		}
		v.items = items
	case Object:
		members := make([]member, len(v.members))
		for i, item := range v.members {
			members[i] = member{item.name, item.value.Sorted()}
		}
		sort.Slice(members, func(i, j int) bool { return members[i].name < members[j].name })
		v.members = members
	}
	return v
}
