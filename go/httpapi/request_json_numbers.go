package httpapi

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// The pinned source interpreter uses CPython's default decimal integer limit.
// This is a parse-time failure, including values later discarded by duplicate keys.
const requestIntegerDigitLimit = 4300

func requestIntegerTooLong(raw []byte) bool {
	if bytes.ContainsAny(raw, ".eE") {
		return false
	}
	if len(raw) > 0 && raw[0] == '-' {
		raw = raw[1:]
	}
	return len(raw) > requestIntegerDigitLimit
}

// Python JSON distinguishes arbitrary precision integers from IEEE-754 doubles.
// Finite values retain their normalized number spelling in the closed diagnostic
// variant; nonfinite values have a separate variant and cannot be serialized.
func readRequestJSONNumber(raw string) (ValidationInput, error) {
	if !strings.ContainsAny(raw, ".eE") {
		if requestIntegerTooLong([]byte(raw)) {
			return ValidationInput{}, errPersonalSkillRequest
		}
		if raw == "-0" {
			raw = "0"
		}
		return ValidationInput{kind: validationNumber, number: json.Number(raw)}, nil
	}
	f, err := strconv.ParseFloat(raw, 64)
	if math.IsInf(f, 0) {
		return ValidationInput{kind: validationNonfinite, text: raw}, nil
	}
	if err != nil {
		return ValidationInput{}, err
	}
	if a := math.Abs(f); a != 0 && (a < 1e-4 || a >= 1e16) {
		raw = strconv.FormatFloat(f, 'e', -1, 64)
	} else {
		raw = strconv.FormatFloat(f, 'f', -1, 64)
		if !strings.Contains(raw, ".") {
			raw += ".0"
		}
	}
	return ValidationInput{kind: validationNumber, number: json.Number(raw)}, nil
}
