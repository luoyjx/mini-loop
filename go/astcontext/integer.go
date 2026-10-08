package astcontext

import (
	"fmt"
	"math/big"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// PayloadIntegerError preserves Source ValueError wording. Public operations
// catch this conversion error after snapshot cleanup, while invocation callers
// can still inspect its typed cause. Zero digits denotes integer-to-text output.
type PayloadIntegerError struct{ digits int }

func (e PayloadIntegerError) Error() string {
	message := "Exceeds the limit (4300 digits) for integer string conversion"
	if e.digits != 0 {
		message += fmt.Sprintf(": value has %d digits", e.digits)
	}
	return message + "; use sys.set_int_max_str_digits() to increase the limit"
}

func (e PayloadIntegerError) Unwrap() error { return jsonvalue.ErrInteger }

func metricText(total *big.Int) (string, error) {
	text := total.String()
	if len(text) > 4300 {
		return "", PayloadIntegerError{}
	}
	return text, nil
}
