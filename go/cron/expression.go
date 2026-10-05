// Package cron implements the explicitly bound operator scheduler. It does not
// install model tools or authorize restored jobs on behalf of a caller.
package cron

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

type field struct {
	values [64]bool
	free   bool
}
type Expression struct {
	text   string
	fields [5]field
}

func (e Expression) String() string { return e.text }
func integer(value string) (*big.Int, error) {
	original := value
	value = strings.TrimFunc(value, pytext.Space)
	sign := ""
	if strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") {
		sign = value[:1]
		value = value[1:]
	}
	var normalized strings.Builder
	previousDigit := false
	digits := 0
	for _, r := range value {
		if r == '_' && previousDigit {
			previousDigit = false
			continue
		}
		d, ok := pytext.DecimalDigit(r)
		if !ok {
			return nil, invalidInteger(original)
		}
		normalized.WriteByte(d)
		previousDigit = true
		digits++
	}
	if !previousDigit {
		return nil, invalidInteger(original)
	}
	if digits > 4300 {
		return nil, fmt.Errorf("Exceeds the limit (4300 digits) for integer string conversion: value has %d digits; use sys.set_int_max_str_digits() to increase the limit", digits)
	}
	n, ok := new(big.Int).SetString(sign+normalized.String(), 10)
	if !ok {
		return nil, invalidInteger(original)
	}
	return n, nil
}
func invalidInteger(value string) error {
	return fmt.Errorf("invalid literal for int() with base 10: %s", pythonRepr(value))
}
func pythonRepr(value string) string {
	quote := "'"
	if strings.Contains(value, "'") && !strings.Contains(value, "\"") {
		quote = "\""
	}
	value = strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\r", "\\r", "\t", "\\t").Replace(value)
	value = strings.ReplaceAll(value, quote, "\\"+quote)
	return quote + value + quote
}
func parseField(value string, lower, upper int) (field, error) {
	f := field{free: value == "*"}
	for _, part := range strings.Split(value, ",") {
		if part == "" {
			return field{}, errors.New("empty list item")
		}
		base, stepText, separated := strings.Cut(part, "/")
		step := big.NewInt(1)
		var err error
		if separated {
			step, err = integer(stepText)
			if err != nil {
				return field{}, err
			}
		}
		if step.Sign() <= 0 {
			return field{}, errors.New("step must be greater than zero")
		}
		start, end := big.NewInt(int64(lower)), big.NewInt(int64(upper))
		if base != "*" {
			left, right, ranged := strings.Cut(base, "-")
			if !ranged && separated {
				return field{}, errors.New("step requires '*' or a range")
			}
			start, err = integer(left)
			if err != nil {
				return field{}, err
			}
			end = new(big.Int).Set(start)
			if ranged {
				end, err = integer(right)
				if err != nil {
					return field{}, err
				}
			}
		}
		if start.Cmp(big.NewInt(int64(lower))) < 0 || start.Cmp(big.NewInt(int64(upper))) > 0 || end.Cmp(big.NewInt(int64(lower))) < 0 || end.Cmp(big.NewInt(int64(upper))) > 0 {
			return field{}, fmt.Errorf("value must be in %d-%d", lower, upper)
		}
		if start.Cmp(end) > 0 {
			return field{}, errors.New("range start must not exceed range end")
		}
		// An arbitrary precision step above the field span selects only start.
		stride := upper + 1
		if step.IsInt64() && step.Int64() <= int64(upper+1) {
			stride = int(step.Int64())
		}
		for n := int(start.Int64()); n <= int(end.Int64()); n += stride {
			f.values[n] = true
		}
	}
	return f, nil
}
func Parse(value string) (Expression, error) {
	parts := strings.FieldsFunc(value, pytext.Space)
	if len(parts) != 5 {
		return Expression{}, errors.New("cron must have 5 fields: minute hour day-of-month month day-of-week")
	}
	e := Expression{text: value}
	bounds := [5][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	names := [5]string{"minute", "hour", "day-of-month", "month", "day-of-week"}
	for i, part := range parts {
		f, err := parseField(part, bounds[i][0], bounds[i][1])
		if err != nil {
			return Expression{}, fmt.Errorf("%s: %w", names[i], err)
		}
		e.fields[i] = f
	}
	return e, nil
}
func (e Expression) Matches(now time.Time) bool {
	if !e.fields[0].values[now.Minute()] || !e.fields[1].values[now.Hour()] || !e.fields[3].values[int(now.Month())] {
		return false
	}
	dom, dow := e.fields[2], e.fields[4]
	if dom.free && dow.free {
		return true
	}
	if dom.free {
		return dow.values[int(now.Weekday())]
	}
	if dow.free {
		return dom.values[now.Day()]
	}
	return dom.values[now.Day()] || dow.values[int(now.Weekday())]
}
