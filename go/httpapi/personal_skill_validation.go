package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"unicode/utf8"
)

type validationInputKind uint8

const (
	validationNull validationInputKind = iota
	validationText
	validationNumber
	validationBool
	validationArray
	validationObject
)

// ValidationInput is a closed JSON projection used only in HTTP diagnostics.
// It never enters session state or confers ownership/publication authority.
type ValidationInput struct {
	kind    validationInputKind
	text    string
	number  json.Number
	boolean bool
	items   []ValidationInput
	members []validationMember
}
type validationMember struct {
	key   string
	value ValidationInput
}

func (v ValidationInput) MarshalJSON() ([]byte, error) {
	switch v.kind {
	case validationNull:
		return []byte("null"), nil
	case validationText:
		return json.Marshal(v.text)
	case validationNumber:
		return json.Marshal(v.number)
	case validationBool:
		return json.Marshal(v.boolean)
	case validationArray:
		return json.Marshal(v.items)
	case validationObject:
		// Raw bytes remain local to serialization, never retained in diagnostics.
		fields := make(map[string]json.RawMessage, len(v.members))
		for _, m := range v.members {
			raw, err := m.value.MarshalJSON()
			if err != nil {
				return nil, err
			}
			fields[m.key] = raw
		}
		return json.Marshal(fields)
	default:
		return nil, errPersonalSkillRequest
	}
}

func readValidationInput(d *json.Decoder, depth int) (ValidationInput, error) {
	if depth > 256 {
		return ValidationInput{}, errPersonalSkillRequest
	}
	token, err := d.Token()
	if err != nil {
		return ValidationInput{}, err
	}
	switch v := token.(type) {
	case nil:
		return ValidationInput{}, nil
	case string:
		return ValidationInput{kind: validationText, text: v}, nil
	case bool:
		return ValidationInput{kind: validationBool, boolean: v}, nil
	case json.Number:
		return ValidationInput{kind: validationNumber, number: v}, nil
	case json.Delim:
		result := ValidationInput{}
		positions := make(map[string]int)
		switch v {
		case '[':
			result.kind = validationArray
			result.items = []ValidationInput{}
		case '{':
			result.kind = validationObject
		default:
			return result, errPersonalSkillRequest
		}
		for d.More() {
			key := ""
			if v == '{' {
				k, e := d.Token()
				if e != nil {
					return result, e
				}
				var ok bool
				key, ok = k.(string)
				if !ok {
					return result, errPersonalSkillRequest
				}
			}
			item, e := readValidationInput(d, depth+1)
			if e != nil {
				return result, e
			}
			if v == '[' {
				result.items = append(result.items, item)
			} else {
				if index, found := positions[key]; found {
					result.members[index].value = item
				} else {
					positions[key] = len(result.members)
					result.members = append(result.members, validationMember{key, item})
				}
			}
		}
		end, e := d.Token()
		if e != nil || (v == '[' && end != json.Delim(']')) || (v == '{' && end != json.Delim('}')) {
			return result, errPersonalSkillRequest
		}
		return result, nil
	default:
		return ValidationInput{}, errPersonalSkillRequest
	}
}

type RequestValidationCode string

const (
	validationMissing RequestValidationCode = "missing"
	validationModel   RequestValidationCode = "model_attributes_type"
	validationString  RequestValidationCode = "string_type"
	validationShort   RequestValidationCode = "string_too_short"
	validationLong    RequestValidationCode = "string_too_long"
	validationPattern RequestValidationCode = "string_pattern_mismatch"
	validationExtra   RequestValidationCode = "extra_forbidden"
)

type RequestValidationContext struct {
	MinLength *int   `json:"min_length,omitempty"`
	MaxLength *int   `json:"max_length,omitempty"`
	Pattern   string `json:"pattern,omitempty"`
}
type RequestValidationDetail struct {
	Type     RequestValidationCode     `json:"type"`
	Location []string                  `json:"loc"`
	Message  string                    `json:"msg"`
	Input    ValidationInput           `json:"input"`
	Context  *RequestValidationContext `json:"ctx,omitempty"`
}
type RequestValidationResponse struct {
	Detail []RequestValidationDetail `json:"detail"`
}

func personalSkillValidation(input ValidationInput, preview bool) []RequestValidationDetail {
	issue := func(code RequestValidationCode, field, message string, value ValidationInput, ctx *RequestValidationContext) RequestValidationDetail {
		loc := []string{"body"}
		if field != "" {
			loc = append(loc, field)
		}
		return RequestValidationDetail{code, loc, message, value, ctx}
	}
	if input.kind == validationNull {
		return []RequestValidationDetail{issue(validationMissing, "", "Field required", input, nil)}
	}
	if input.kind != validationObject {
		return []RequestValidationDetail{issue(validationModel, "", "Input should be a valid dictionary or object to extract fields from", input, nil)}
	}
	fields := []string{"digest"}
	if preview {
		fields = []string{"name", "focus"}
	}
	errors := []RequestValidationDetail{}
	for _, field := range fields {
		value := ValidationInput{}
		present := false
		for _, m := range input.members {
			if m.key == field {
				value = m.value
				present = true
				break
			}
		}
		if !present {
			if field != "focus" {
				errors = append(errors, issue(validationMissing, field, "Field required", input, nil))
			}
			continue
		}
		if value.kind != validationText {
			errors = append(errors, issue(validationString, field, "Input should be a valid string", value, nil))
			continue
		}
		min, max := 64, 64
		pattern := personalSkillDigest
		if field == "name" {
			min, max, pattern = 1, 64, personalSkillName
		} else if field == "focus" {
			min, max, pattern = 0, 2000, nil
		}
		length := utf8.RuneCountInString(value.text)
		if length < min {
			plural := "characters"
			if min == 1 {
				plural = "character"
			}
			errors = append(errors, issue(validationShort, field, fmt.Sprintf("String should have at least %d %s", min, plural), value, &RequestValidationContext{MinLength: &min}))
		} else if length > max {
			errors = append(errors, issue(validationLong, field, fmt.Sprintf("String should have at most %d characters", max), value, &RequestValidationContext{MaxLength: &max}))
		} else if pattern != nil && !pattern.MatchString(value.text) {
			errors = append(errors, issue(validationPattern, field, "String should match pattern '"+pattern.String()+"'", value, &RequestValidationContext{Pattern: pattern.String()}))
		}
	}
	for _, m := range input.members {
		known := false
		for _, field := range fields {
			if m.key == field {
				known = true
				break
			}
		}
		if !known {
			errors = append(errors, issue(validationExtra, m.key, "Extra inputs are not permitted", m.value, nil))
		}
	}
	return errors
}

func decodePersonalSkillBody[T PersonalSkillPreviewRequest | PersonalSkillCommitRequest](s *Server, w http.ResponseWriter, r *http.Request, preview bool) (T, bool) {
	var result T
	raw, err := io.ReadAll(r.Body)
	if err != nil || !utf8.Valid(raw) {
		writeJSON(s, w, 422, ErrorResponse{"invalid request body"})
		return result, false
	}
	input := ValidationInput{}
	if len(bytes.TrimSpace(raw)) > 0 {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		input, err = readValidationInput(d, 0)
		if err == nil {
			_, err = d.Token()
			if err == io.EOF {
				err = nil
			} else {
				err = errPersonalSkillRequest
			}
		}
		if err != nil {
			writeJSON(s, w, 422, ErrorResponse{"invalid request body"})
			return result, false
		}
	}
	if details := personalSkillValidation(input, preview); len(details) > 0 {
		writeJSON(s, w, 422, RequestValidationResponse{Detail: details})
		return result, false
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		writeJSON(s, w, 422, ErrorResponse{"invalid request body"})
		return result, false
	}
	return result, true
}
