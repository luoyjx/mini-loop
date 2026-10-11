package agent

import (
	"encoding/json"
	"errors"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// marshalTrajectoryArchive encodes a detached observation. It never changes the
// request encoder, execution inputs, or the authority of the live record.
func (record TrajectoryRecord) marshalTrajectoryArchive() ([]byte, error) {
	live := record.Record
	switch record.details.kind {
	case EventToolResult:
		live.Event.toolResult.Output = record.details.text
	case EventSubagentStart:
		live.Event.subagent.prompt = record.details.text
	case EventSubagentEnd:
		live.Event.subagent.summary = record.details.text
	}
	data, err := live.MarshalArchiveJSON()
	if live.Event.workflow.observation != nil {
		data, err = live.MarshalWorkflowArchiveJSON(nil)
	}
	if err != nil {
		return nil, err
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return nil, err
	}
	fields := make([]jsonvalue.Field, 0, len(value.Keys())+2)
	for _, name := range value.Keys() {
		if record.Record.Terminal != nil && (name == "trajectory_persisted" || name == "trajectory_recording_error") {
			continue
		}
		child, _ := value.Lookup(name)
		fields = append(fields, jsonvalue.Field{Name: name, Value: child})
	}
	fields = append(fields, jsonvalue.Field{Name: "record_type", Value: jsonvalue.TextValue("event")})
	if record.details.kind == EventModelStart {
		var input jsonvalue.Value
		if record.details.decisionRequest != nil {
			data, err = json.Marshal(record.details.decisionRequest)
			if err == nil {
				input, err = jsonvalue.Decode(string(data))
			}
		} else {
			input, err = trajectoryModelInput(record.details.request)
		}
		if err != nil {
			return nil, err
		}
		fields = append(fields, jsonvalue.Field{Name: "model_input", Value: input})
	}
	if record.details.kind == EventModelEnd {
		data, err = protocol.MarshalObservedBlocks(record.details.reply)
		if err != nil {
			return nil, err
		}
		output, err := jsonvalue.Decode(string(data))
		if err != nil {
			return nil, err
		}
		fields = append(fields, jsonvalue.Field{Name: "model_output", Value: output})
	}
	value = jsonvalue.ObjectValue(fields)
	if record.masker != nil {
		value = value.MapStrings(record.masker.MaskText)
	}
	// Keep historical scalars until the writer applies its content/UTF-8 policy.
	data, err = jsonvalue.AppendLegacyDefault(value)
	if err == nil && len(data) > protocol.MaxProjectionBytes {
		return nil, errors.New("trajectory projection exceeds byte bound")
	}
	return data, err
}

func trajectoryModelInput(request protocol.ModelRequest) (jsonvalue.Value, error) {
	wire, err := request.Wire()
	if err != nil {
		return jsonvalue.Value{}, err
	}
	messages, err := protocol.ObservedWireMessagesPythonJSON(wire.Messages, true, true)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	observed, err := jsonvalue.Decode(messages)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	// Metadata still passes its concrete finite encoder. Only the detached message
	// observation is substituted, with cache annotations retained.
	wire.Messages = nil
	data, err := json.Marshal(wire)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return jsonvalue.Value{}, err
	}
	fields := make([]jsonvalue.Field, 0, len(value.Keys())-1)
	for _, name := range value.Keys() {
		if name == "model" {
			continue
		}
		child, _ := value.Lookup(name)
		if name == "messages" {
			child = observed
		}
		fields = append(fields, jsonvalue.Field{Name: name, Value: child})
	}
	return jsonvalue.ObjectValue(fields), nil
}
