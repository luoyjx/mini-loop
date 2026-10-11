package sqlitestate

import (
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"math/big"
)

// schemaValue is a SQL boundary scanner, immediately lowering the driver's
// finite scalar variants into the existing Source integer conversion contract.
// Dynamic driver values never enter Store fields or record APIs.
type schemaValue struct{ integer *big.Int }

func (v *schemaValue) Scan(raw any) error {
	var value jsonvalue.Value
	switch scalar := raw.(type) {
	case int64:
		value = jsonvalue.IntegerValue(scalar)
	case float64:
		value = jsonvalue.FloatValue(scalar)
	case string:
		value = jsonvalue.TextValue(scalar)
	case []byte:
		value = jsonvalue.TextValue(string(scalar))
	case nil:
		value = jsonvalue.NullValue()
	default:
		return jsonvalue.ErrIntegerConversion
	}
	number, err := value.PythonInteger()
	if err != nil {
		return err
	}
	v.integer = number
	return nil
}
