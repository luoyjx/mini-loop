package httpapi

import (
	"math"
	"strings"
	"unicode/utf8"
)

// cronScheduleValidation lowers the public CronReq model only after collecting
// its ordered field diagnostics. Invalid input never reaches owner lookup or
// the scheduler; the ordinary authentication middleware still runs first.
func cronScheduleValidation(input ValidationInput) (CronScheduleRequest, []RequestValidationDetail) {
	var result CronScheduleRequest
	issue := func(code RequestValidationCode, field, message string, value ValidationInput, context *RequestValidationContext) RequestValidationDetail {
		location := []ValidationLocation{{field: "body"}}
		if field != "" {
			location = append(location, ValidationLocation{field: field})
		}
		return RequestValidationDetail{Type: code, Location: location, Message: message, Input: value, Context: context}
	}
	if input.kind == validationNull {
		return result, []RequestValidationDetail{issue(validationMissing, "", "Field required", input, nil)}
	}
	if input.kind != validationObject {
		return result, []RequestValidationDetail{issue(validationModel, "", "Input should be a valid dictionary or object to extract fields from", input, nil)}
	}
	var details []RequestValidationDetail
	for _, field := range []string{"cron", "prompt", "recurring", "durable"} {
		var value ValidationInput
		present := false
		for _, member := range input.members {
			if member.key == field {
				value, present = member.value, true
				break
			}
		}
		if !present {
			if field == "cron" || field == "prompt" {
				details = append(details, issue(validationMissing, field, "Field required", input, nil))
			}
			continue
		}
		if field == "recurring" || field == "durable" {
			boolean, code := cronBoolean(value)
			if code != "" {
				message := "Input should be a valid boolean"
				if code == "bool_parsing" {
					message += ", unable to interpret input"
				}
				details = append(details, issue(code, field, message, value, nil))
			} else if field == "recurring" {
				result.Recurring = &boolean
			} else {
				result.Durable = &boolean
			}
			continue
		}
		if value.kind != validationText {
			details = append(details, issue(validationString, field, "Input should be a valid string", value, nil))
			continue
		}
		length := utf8.RuneCountInString(value.text)
		if length < 1 {
			minimum := 1
			details = append(details, issue(validationShort, field, "String should have at least 1 character", value, &RequestValidationContext{MinLength: &minimum}))
			continue
		}
		if field == "cron" && length > 100 {
			maximum := 100
			details = append(details, issue(validationLong, field, "String should have at most 100 characters", value, &RequestValidationContext{MaxLength: &maximum}))
			continue
		}
		if field == "cron" {
			result.Cron = value.text
		} else {
			result.Prompt = value.text
		}
	}
	return result, details
}

func cronBoolean(value ValidationInput) (bool, RequestValidationCode) {
	switch value.kind {
	case validationBool:
		return value.boolean, ""
	case validationText:
		switch strings.ToLower(value.text) {
		case "0", "off", "f", "false", "n", "no":
			return false, ""
		case "1", "on", "t", "true", "y", "yes":
			return true, ""
		}
		return false, "bool_parsing"
	case validationNumber:
		number, err := value.number.Float64()
		if err != nil || math.IsInf(number, 0) || math.IsNaN(number) || math.Trunc(number) != number {
			return false, "bool_type"
		}
		if number == 0 || number == 1 {
			return number == 1, ""
		}
		return false, "bool_parsing"
	default:
		return false, "bool_type"
	}
}
