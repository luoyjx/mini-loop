package workflows

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type ValidationKind string

const (
	DefinitionFailure ValidationKind = "WorkflowValidationError"
	ArtifactFailure   ValidationKind = "ArtifactValidationError"
	TypeFailure       ValidationKind = "TypeError"
)

type ValidationError struct {
	Kind   ValidationKind
	Detail string
}

func (e *ValidationError) Error() string               { return e.Detail }
func invalid(kind ValidationKind, detail string) error { return &ValidationError{kind, detail} }
func reprNames(names []string) string {
	values := make([]Value, len(names))
	for i, name := range names {
		values[i] = jsonvalue.TextValue(name)
	}
	return jsonvalue.ArrayValue(values).PythonString()
}
func get(value Value, key string, fallback Value) Value {
	if v, ok := value.Lookup(key); ok {
		return v
	}
	return fallback
}

func ValidateSchema(schema Value) error { return validateSchemaAt(schema, "$schema") }
func validateSchemaAt(schema Value, path string) error {
	if schema.Kind() != jsonvalue.Object {
		return invalid(DefinitionFailure, path+" must be an object")
	}
	unknown := []string{}
	for _, key := range schema.Keys() {
		switch key {
		case "type", "properties", "required", "items", "enum", "const", "additionalProperties", "title", "description":
		default:
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return invalid(DefinitionFailure, path+" contains unsupported schema keywords: "+reprNames(unknown))
	}
	types := get(schema, "type", jsonvalue.NullValue())
	if types.Kind() != jsonvalue.Null {
		allowed, ok := types.Array()
		if types.Kind() == jsonvalue.Text {
			allowed = []Value{types}
			ok = true
		}
		if !ok || len(allowed) == 0 {
			return invalid(DefinitionFailure, path+".type is unsupported")
		}
		for _, item := range allowed {
			if item.Kind() == jsonvalue.Array || item.Kind() == jsonvalue.Object {
				kind := "list"
				if item.Kind() == jsonvalue.Object {
					kind = "dict"
				}
				return invalid(TypeFailure, "unhashable type: '"+kind+"'")
			}
			name, _ := item.Text()
			switch name {
			case "object", "array", "string", "integer", "number", "boolean", "null":
			default:
				return invalid(DefinitionFailure, path+".type is unsupported")
			}
		}
	}
	required, ok := get(schema, "required", jsonvalue.ArrayValue(nil)).Array()
	if !ok {
		return invalid(DefinitionFailure, path+".required must be a string array")
	}
	for _, item := range required {
		if item.Kind() != jsonvalue.Text {
			return invalid(DefinitionFailure, path+".required must be a string array")
		}
	}
	properties := get(schema, "properties", jsonvalue.ObjectValue(nil))
	if properties.Kind() != jsonvalue.Object {
		return invalid(DefinitionFailure, path+".properties must be an object")
	}
	for _, key := range properties.Keys() {
		child, _ := properties.Lookup(key)
		if err := validateSchemaAt(child, path+".properties."+key); err != nil {
			return err
		}
	}
	if child, ok := schema.Lookup("items"); ok {
		if err := validateSchemaAt(child, path+".items"); err != nil {
			return err
		}
	}
	if child, ok := schema.Lookup("enum"); ok && child.Kind() != jsonvalue.Array {
		return invalid(DefinitionFailure, path+".enum must be an array")
	}
	if child, ok := schema.Lookup("additionalProperties"); ok && child.Kind() != jsonvalue.Boolean {
		return invalid(DefinitionFailure, path+".additionalProperties must be a boolean")
	}
	for _, key := range []string{"title", "description"} {
		if child, ok := schema.Lookup(key); ok && child.Kind() != jsonvalue.Text {
			return invalid(DefinitionFailure, path+"."+key+" must be a string")
		}
	}
	return nil
}
func ValidateValue(schema, value Value) error { return validateValueAt(schema, value, "$") }
func validateValueAt(schema, value Value, path string) error {
	if err := ValidateSchema(schema); err != nil {
		return err
	}
	if options, ok := schema.Lookup("enum"); ok {
		found := false
		items, _ := options.Array()
		for _, candidate := range items {
			if pythonMemberEqual(value, candidate) {
				found = true
				break
			}
		}
		if !found {
			return invalid(ArtifactFailure, path+" is not one of the allowed enum values")
		}
	}
	if constant, ok := schema.Lookup("const"); ok && !pythonEqual(value, constant) {
		return invalid(ArtifactFailure, path+" does not match const")
	}
	types := get(schema, "type", jsonvalue.NullValue())
	allowed, _ := types.Array()
	if types.Kind() == jsonvalue.Text {
		allowed = []Value{types}
	}
	if len(allowed) > 0 {
		matched := false
		names := make([]string, len(allowed))
		for i, item := range allowed {
			names[i], _ = item.Text()
			switch names[i] {
			case "object":
				matched = matched || value.Kind() == jsonvalue.Object
			case "array":
				matched = matched || value.Kind() == jsonvalue.Array
			case "string":
				matched = matched || value.Kind() == jsonvalue.Text
			case "integer":
				matched = matched || value.Kind() == jsonvalue.Integer
			case "number":
				matched = matched || value.Kind() == jsonvalue.Integer || value.Kind() == jsonvalue.Float
			case "boolean":
				matched = matched || value.Kind() == jsonvalue.Boolean
			case "null":
				matched = matched || value.Kind() == jsonvalue.Null
			}
		}
		if !matched {
			return invalid(ArtifactFailure, path+" must have type "+strings.Join(names, " | "))
		}
	}
	if number, ok := value.Float(); ok && (math.IsNaN(number) || math.IsInf(number, 0)) {
		return invalid(ArtifactFailure, path+" must be finite")
	}
	if value.Kind() == jsonvalue.Object {
		required, _ := get(schema, "required", jsonvalue.ArrayValue(nil)).Array()
		missing := []string{}
		for _, item := range required {
			key, _ := item.Text()
			if _, ok := value.Lookup(key); !ok {
				missing = append(missing, key)
			}
		}
		if len(missing) > 0 {
			return invalid(ArtifactFailure, path+" is missing required keys: "+reprNames(missing))
		}
		properties := get(schema, "properties", jsonvalue.ObjectValue(nil))
		additional := get(schema, "additionalProperties", jsonvalue.NullValue())
		if allow, ok := additional.Bool(); ok && !allow {
			extras := []string{}
			for _, key := range value.Keys() {
				if _, ok := properties.Lookup(key); !ok {
					extras = append(extras, key)
				}
			}
			if len(extras) > 0 {
				sort.Strings(extras)
				return invalid(ArtifactFailure, path+" has unknown keys: "+reprNames(extras))
			}
		}
		for _, key := range properties.Keys() {
			if item, ok := value.Lookup(key); ok {
				child, _ := properties.Lookup(key)
				if err := validateValueAt(child, item, path+"."+key); err != nil {
					return err
				}
			}
		}
	}
	if items, ok := value.Array(); ok {
		if child, ok := schema.Lookup("items"); ok {
			for i, item := range items {
				if err := validateValueAt(child, item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Python equality admits bool/int/float equality for enum/const, while schema
// numeric type admission explicitly excludes booleans. Arbitrary integers are
// compared exactly against the float's rational value, without float rounding.
func number(value Value) (*big.Rat, bool) {
	switch value.Kind() {
	case jsonvalue.Boolean:
		b, _ := value.Bool()
		if b {
			return big.NewRat(1, 1), true
		}
		return big.NewRat(0, 1), true
	case jsonvalue.Integer:
		s, _ := value.Integer()
		i, ok := new(big.Int).SetString(s, 10)
		if !ok {
			return nil, false
		}
		return new(big.Rat).SetInt(i), true
	case jsonvalue.Float:
		f, _ := value.Float()
		r := new(big.Rat).SetFloat64(f)
		return r, r != nil
	}
	return nil, false
}
func pythonEqual(a, b Value) bool {
	if x, ok := number(a); ok {
		if y, ok := number(b); ok {
			return x.Cmp(y) == 0
		}
	}
	if a.Kind() != b.Kind() {
		return false
	}
	switch a.Kind() {
	case jsonvalue.Null:
		return true
	case jsonvalue.Text:
		x, _ := a.Text()
		y, _ := b.Text()
		return x == y
	case jsonvalue.Float:
		x, _ := a.Float()
		y, _ := b.Float()
		return x == y
	case jsonvalue.Array:
		x, _ := a.Array()
		y, _ := b.Array()
		if len(x) != len(y) {
			return false
		}
		for i := range x {
			if !pythonMemberEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	case jsonvalue.Object:
		if len(a.Keys()) != len(b.Keys()) {
			return false
		}
		for _, key := range a.Keys() {
			x, _ := a.Lookup(key)
			y, ok := b.Lookup(key)
			if !ok || !pythonMemberEqual(x, y) {
				return false
			}
		}
		return true
	}
	return false
}

// Python's JSON decoder reuses its NaN constant. Container comparisons and
// membership first accept identical objects, whereas scalar const equality
// still treats NaN as unequal. This models decoded JSON, not arbitrary Python
// live-object identity.
func pythonMemberEqual(a, b Value) bool {
	x, xok := a.Float()
	y, yok := b.Float()
	if xok && yok && math.IsNaN(x) && math.IsNaN(y) {
		return true
	}
	return pythonEqual(a, b)
}
