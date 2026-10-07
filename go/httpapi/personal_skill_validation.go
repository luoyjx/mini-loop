package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
		if !utf8.ValidString(v.text) {
			return nil, errPersonalSkillRequest
		}
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
			if !utf8.ValidString(m.key) {
				return nil, errPersonalSkillRequest
			}
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

type RequestValidationCode string

const (
	validationMissing RequestValidationCode = "missing"
	validationModel   RequestValidationCode = "model_attributes_type"
	validationString  RequestValidationCode = "string_type"
	validationShort   RequestValidationCode = "string_too_short"
	validationLong    RequestValidationCode = "string_too_long"
	validationPattern RequestValidationCode = "string_pattern_mismatch"
	validationExtra   RequestValidationCode = "extra_forbidden"
	validationJSON    RequestValidationCode = "json_invalid"
)

type RequestValidationContext struct {
	MinLength *int   `json:"min_length,omitempty"`
	MaxLength *int   `json:"max_length,omitempty"`
	Pattern   string `json:"pattern,omitempty"`
	Error     string `json:"error,omitempty"`
}
type ValidationLocation struct {
	field    string
	position *int
}

func (v ValidationLocation) MarshalJSON() ([]byte, error) {
	if v.position != nil {
		return json.Marshal(*v.position)
	}
	return json.Marshal(v.field)
}

type RequestValidationDetail struct {
	Type     RequestValidationCode     `json:"type"`
	Location []ValidationLocation      `json:"loc"`
	Message  string                    `json:"msg"`
	Input    ValidationInput           `json:"input"`
	Context  *RequestValidationContext `json:"ctx,omitempty"`
}
type RequestValidationResponse struct {
	Detail []RequestValidationDetail `json:"detail"`
}

func personalSkillValidation(input ValidationInput, preview bool) []RequestValidationDetail {
	issue := func(code RequestValidationCode, field, message string, value ValidationInput, ctx *RequestValidationContext) RequestValidationDetail {
		loc := []ValidationLocation{{field: "body"}}
		if field != "" {
			loc = append(loc, ValidationLocation{field: field})
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
	if err != nil {
		writeJSON(s, w, 400, ErrorResponse{"There was an error parsing the body"})
		return result, false
	}
	input := ValidationInput{}
	contentType := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0]))
	jsonBody := contentType == "application/json" || (strings.HasPrefix(contentType, "application/") && strings.HasSuffix(contentType, "+json"))
	if len(raw) > 0 && !jsonBody {
		if !utf8.Valid(raw) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(500)
			w.Write([]byte("Internal Server Error"))
			return result, false
		}
		input = ValidationInput{kind: validationText, text: string(raw)}
	} else if len(raw) > 0 {
		raw, err = decodeRequestJSONEncoding(raw)
		if err != nil {
			writeJSON(s, w, 400, ErrorResponse{"There was an error parsing the body"})
			return result, false
		}
		if failure := requestJSONSyntax(raw); failure != nil {
			if failure.kind == jsonNestingLimit {
				writeJSON(s, w, 400, ErrorResponse{"There was an error parsing the body"})
				return result, false
			}
			writeJSON(s, w, 422, RequestValidationResponse{Detail: []RequestValidationDetail{{Type: validationJSON, Location: []ValidationLocation{{field: "body"}, {position: &failure.position}}, Message: "JSON decode error", Input: ValidationInput{kind: validationObject}, Context: &RequestValidationContext{Error: failure.message()}}}})
			return result, false
		}
		input, err = readRequestJSONValue(raw)
		if err != nil {
			writeJSON(s, w, 422, ErrorResponse{"invalid request body"})
			return result, false
		}
	}
	if input.hasSurrogate() {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(500)
		w.Write([]byte("Internal Server Error"))
		return result, false
	}
	if details := personalSkillValidation(input, preview); len(details) > 0 {
		writeJSON(s, w, 422, RequestValidationResponse{Detail: details})
		return result, false
	}
	canonical, err := input.MarshalJSON()
	if err != nil {
		writeJSON(s, w, 500, ErrorResponse{"response encoding failed"})
		return result, false
	}
	if err := json.Unmarshal(canonical, &result); err != nil {
		writeJSON(s, w, 422, ErrorResponse{"invalid request body"})
		return result, false
	}
	return result, true
}
