package protocol

import "github.com/luoyjx/mini-loop/go/internal/jsonvalue"

// bashObservation preserves the closed command/option/rejected-root variants.
// Policy accessors remain separate from this detached archival value.
func (input ToolInput) bashObservation() jsonvalue.Value {
	if input.bashRejected != nil {
		return input.bashRejected.observation
	}
	fields := make([]jsonvalue.Field, 0, 3+len(input.bashExtra.Keys()))
	if !input.bashCommandMissing {
		value := jsonvalue.TextValue(input.bash.Command)
		if input.bashCommand != nil {
			value = input.bashCommand.observed
		}
		fields = append(fields, jsonvalue.Field{Name: "command", Value: value})
	}
	if input.bashBackground != nil {
		fields = append(fields, jsonvalue.Field{Name: "run_in_background", Value: input.bashBackground.observed})
	} else if input.bash.RunInBackground != nil || input.nulls.Background {
		value := jsonvalue.NullValue()
		if input.bash.RunInBackground != nil {
			value = jsonvalue.BoolValue(*input.bash.RunInBackground)
		}
		fields = append(fields, jsonvalue.Field{Name: "run_in_background", Value: value})
	}
	if input.bashApproval != nil {
		fields = append(fields, jsonvalue.Field{Name: "approval_prefix", Value: input.bashApproval.observed})
	} else if input.bash.ApprovalPrefix != nil || input.nulls.ApprovalPrefix {
		value := jsonvalue.NullValue()
		if input.bash.ApprovalPrefix != nil {
			items := make([]jsonvalue.Value, len(*input.bash.ApprovalPrefix))
			for i, token := range *input.bash.ApprovalPrefix {
				items[i] = jsonvalue.TextValue(token)
			}
			value = jsonvalue.ArrayValue(items)
		}
		fields = append(fields, jsonvalue.Field{Name: "approval_prefix", Value: value})
	}
	return observationFields(jsonvalue.ObjectValue(fields), input.bashExtra)
}
