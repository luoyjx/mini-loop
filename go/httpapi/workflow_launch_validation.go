package httpapi

import (
	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type WorkflowLaunchRequest struct {
	Definition jsonvalue.Value
	Args       jsonvalue.Value
	ActionID   *agent.ActionID
}

// Lower only retained fields. Unknown request fields have no service effect,
// including legacy JSON values that cannot be serialized as HTTP diagnostics.
func workflowRequestValue(v ValidationInput) (jsonvalue.Value, error) {
	switch v.kind {
	case validationNull:
		return jsonvalue.NullValue(), nil
	case validationText:
		return jsonvalue.TextValue(v.text), nil
	case validationBool:
		return jsonvalue.BoolValue(v.boolean), nil
	case validationNumber:
		return jsonvalue.Decode(v.number.String())
	case validationNonfinite:
		return jsonvalue.Decode(v.text)
	case validationArray:
		items := make([]jsonvalue.Value, len(v.items))
		for i, item := range v.items {
			value, err := workflowRequestValue(item)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			items[i] = value
		}
		return jsonvalue.ArrayValue(items), nil
	case validationObject:
		fields := make([]jsonvalue.Field, 0, len(v.members))
		for _, member := range v.members {
			value, err := workflowRequestValue(member.value)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			fields = append(fields, jsonvalue.Field{Name: member.key, Value: value})
		}
		return jsonvalue.ObjectValue(fields), nil
	}
	return jsonvalue.Value{}, errPersonalSkillRequest
}

func workflowLaunchValidation(input ValidationInput) (WorkflowLaunchRequest, []RequestValidationDetail, error) {
	result := WorkflowLaunchRequest{Args: jsonvalue.ObjectValue(nil)}
	issue := func(code RequestValidationCode, field, message string, value ValidationInput) RequestValidationDetail {
		loc := []ValidationLocation{{field: "body"}}
		if field != "" {
			loc = append(loc, ValidationLocation{field: field})
		}
		return RequestValidationDetail{Type: code, Location: loc, Message: message, Input: value}
	}
	if input.kind == validationNull {
		return result, []RequestValidationDetail{issue(validationMissing, "", "Field required", input)}, nil
	}
	if input.kind != validationObject {
		return result, []RequestValidationDetail{issue(validationModel, "", "Input should be a valid dictionary or object to extract fields from", input)}, nil
	}
	var details []RequestValidationDetail
	for _, name := range []string{"definition", "args", "action_id"} {
		var value ValidationInput
		found := false
		for _, member := range input.members {
			if member.key == name {
				value, found = member.value, true
				break
			}
		}
		if !found {
			if name == "definition" {
				details = append(details, issue(validationMissing, name, "Field required", input))
			}
			continue
		}
		if name == "action_id" {
			if value.kind == validationNull {
				continue
			}
			if value.kind != validationText {
				details = append(details, issue(validationString, name, "Input should be a valid string", value))
				continue
			}
			id := agent.ActionID(value.text)
			result.ActionID = &id
			continue
		}
		if value.kind != validationObject {
			details = append(details, issue("dict_type", name, "Input should be a valid dictionary", value))
			continue
		}
		lowered, err := workflowRequestValue(value)
		if err != nil {
			return result, details, err
		}
		if name == "definition" {
			result.Definition = lowered
		} else {
			result.Args = lowered
		}
	}
	return result, details, nil
}
