package httpapi

import (
	"math/big"
	"strings"
)

type scalarQueryValue struct {
	Text    string
	Present bool
}

type scalarQueryErrorKind string

const (
	queryIntegerParsing  scalarQueryErrorKind = "int_parsing"
	queryIntegerTooLarge scalarQueryErrorKind = "int_parsing_size"
	queryBooleanParsing  scalarQueryErrorKind = "bool_parsing"
)

type scalarQueryIssue struct {
	Type     scalarQueryErrorKind `json:"type"`
	Location [2]string            `json:"loc"`
	Message  string               `json:"msg"`
	Input    string               `json:"input"`
}

type scalarQueryError struct {
	Detail []scalarQueryIssue `json:"detail"`
}

func queryIssue(name, raw string, kind scalarQueryErrorKind) *scalarQueryIssue {
	message := "Input should be a valid integer, unable to parse string as an integer"
	switch kind {
	case queryIntegerTooLarge:
		message = "Unable to parse input string as an integer, exceeded maximum size"
	case queryBooleanParsing:
		message = "Input should be a valid boolean, unable to interpret input"
	}
	return &scalarQueryIssue{Type: kind, Location: [2]string{"query", name}, Message: message, Input: raw}
}

// Pydantic accepts ASCII decimal integers with underscores and a zero-only
// fractional suffix. Preserve large integers until the route applies its bound.
func scalarQueryInteger(name, raw string) (*big.Int, *scalarQueryIssue) {
	invalid := func(kind scalarQueryErrorKind) (*big.Int, *scalarQueryIssue) {
		return nil, queryIssue(name, raw, kind)
	}
	value := strings.TrimSpace(raw)
	sign := ""
	if strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") {
		sign = value[:1]
		value = value[1:]
	}
	if dot := strings.IndexByte(value, '.'); dot >= 0 {
		fraction := value[dot+1:]
		if fraction == "" {
			return invalid(queryIntegerParsing)
		}
		for _, r := range fraction {
			if r != '0' {
				return invalid(queryIntegerParsing)
			}
		}
		value = value[:dot]
	}
	var digits strings.Builder
	digitBefore := false
	for _, r := range value {
		if r == '_' {
			if !digitBefore {
				return invalid(queryIntegerParsing)
			}
			digitBefore = false
			continue
		}
		if r < '0' || r > '9' {
			return invalid(queryIntegerParsing)
		}
		digitBefore = true
		digits.WriteByte(byte(r))
	}
	if !digitBefore {
		return invalid(queryIntegerParsing)
	}
	decimal := strings.TrimLeft(digits.String(), "0")
	if len(decimal) > 4300 {
		return invalid(queryIntegerTooLarge)
	}
	if decimal == "" {
		decimal = "0"
	}
	number, ok := new(big.Int).SetString(sign+decimal, 10)
	if !ok {
		return invalid(queryIntegerParsing)
	}
	return number, nil
}

func scalarQueryLimit(value scalarQueryValue) (int, *scalarQueryIssue) {
	if !value.Present {
		return 100, nil
	}
	number, issue := scalarQueryInteger("limit", value.Text)
	if issue != nil {
		return 0, issue
	}
	if number.Sign() <= 0 {
		return 1, nil
	}
	if number.Cmp(big.NewInt(500)) > 0 {
		return 500, nil
	}
	return int(number.Int64()), nil
}

func scalarQueryBoolean(name, raw string) (bool, *scalarQueryIssue) {
	switch strings.ToLower(raw) {
	case "true", "1", "t", "yes", "y", "on":
		return true, nil
	case "false", "0", "f", "no", "n", "off":
		return false, nil
	default:
		return false, queryIssue(name, raw, queryBooleanParsing)
	}
}
