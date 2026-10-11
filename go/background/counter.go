package background

import (
	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"math/big"
)

// decimalCounter preserves the Python Unicode counter contract.
func decimalCounter(value string) (*big.Int, bool) { return pytext.Decimal(value) }
