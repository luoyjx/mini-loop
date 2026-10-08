// Package traceview folds recorded JSON into typed, escaped inspection views.
package traceview

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"strings"
	"unicode/utf8"
)

const MaxRows = 2000
const PreviewChars = 240
const InspectorChars = 20000

var ErrDocument = errors.New("invalid trajectory document")

type Kind string

const (
	User       Kind = "user"
	Step       Kind = "step"
	Model      Kind = "model"
	Tool       Kind = "tool"
	Assistant  Kind = "assistant"
	Reference  Kind = "reference"
	Steer      Kind = "steer"
	Compaction Kind = "compaction"
	Error      Kind = "error"
	Event      Kind = "event"
	Final      Kind = "final"
)

type Field struct{ Name, Text string }
type Metrics struct {
	agent.TrajectoryMetrics
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}
type Row struct {
	Kind                   Kind
	Label, Content, Status string
	Sequence               *int64
	Timestamp, DurationMS  *float64
	HasDuration, Error     bool
	Depth                  int
	Agent                  *string
	Detail                 []Field
}
type Ledger struct {
	TrajectoryID                   agent.TrajectoryID
	Session                        agent.SessionID
	RunIndex                       int
	Status                         agent.TrajectoryStatus
	Partial                        bool
	StartedAt, EndedAt, DurationMS *float64
	Input                          *string
	Metrics                        Metrics
	Rows                           []Row
	Omitted                        int
}

var envelope = map[string]bool{"type": true, "seq": true, "ts": true, "session": true, "trajectory_id": true, "trace_id": true, "group_id": true, "agent": true, "depth": true, "record_type": true}

// PrettyJSON is a transient serialization projection, not a retained JSON domain.
func PrettyJSON(raw []byte) string {
	value, err := jsonvalue.Decode(string(raw))
	if err != nil {
		return "null"
	}
	if text, ok := value.Text(); ok {
		return text
	}
	text, err := value.LegacyStringIndent()
	if err != nil {
		return "null"
	}
	return text
}

func payload(raw []byte) []byte {
	value, err := jsonvalue.Decode(string(raw))
	if err != nil {
		return []byte(`{}`)
	}
	fields := []jsonvalue.Field{}
	for _, name := range value.Keys() {
		if !envelope[name] {
			child, _ := value.Lookup(name)
			fields = append(fields, jsonvalue.Field{Name: name, Value: child})
		}
	}
	data, _ := jsonvalue.AppendLegacy(nil, jsonvalue.ObjectValue(fields))
	return data
}

func Build(document []byte) (Ledger, error) {
	var out Ledger
	if !utf8.Valid(document) || len(bytes.TrimSpace(document)) == 0 || bytes.TrimSpace(document)[0] != '{' {
		return out, ErrDocument
	}
	// Encoded values live only in this decoder boundary; the resulting ledger has
	// named rows/metrics and plain inspector strings, including unknown payloads.
	var wire struct {
		TrajectoryID         agent.TrajectoryID `json:"trajectory_id"`
		Session              agent.SessionID
		RunIndex             int `json:"run_index"`
		Status               *agent.TrajectoryStatus
		Partial              bool
		StartedAt            *float64 `json:"started_at"`
		EndedAt              *float64 `json:"ended_at"`
		DurationMS           *float64 `json:"duration_ms"`
		Input, Output, Error *string
		Metrics              Metrics
		Events               []json.RawMessage
	}

	value, err := jsonvalue.Decode(string(document))
	if err != nil || value.Kind() != jsonvalue.Object {
		return out, ErrDocument
	}
	events, present := value.Lookup("events")
	if !present {
		events, _ = value.Lookup("Events")
	}
	eventValues, eventsArray := events.Array()
	if events.Kind() != jsonvalue.Null && !eventsArray {
		return out, ErrDocument
	}
	header, err := replaceEvents(value, jsonvalue.ArrayValue(nil)).MarshalJSON()
	if err != nil || json.Unmarshal(header, &wire) != nil {
		return out, ErrDocument
	}
	for _, event := range eventValues {
		raw, err := jsonvalue.AppendLegacy(nil, event)
		if err != nil {
			return out, ErrDocument
		}
		wire.Events = append(wire.Events, raw)
	}
	out = Ledger{TrajectoryID: wire.TrajectoryID, Session: wire.Session, RunIndex: wire.RunIndex, Status: agent.TrajectoryCompleted, Partial: wire.Partial, StartedAt: wire.StartedAt, EndedAt: wire.EndedAt, DurationMS: wire.DurationMS, Input: wire.Input, Metrics: wire.Metrics, Rows: []Row{}}
	if wire.Status != nil {
		out.Status = *wire.Status
	}
	out.Metrics.InputTokens = 0
	out.Metrics.OutputTokens = 0
	add := func(kind Kind, label, content string, seq *int64, ts *float64) int {
		out.Rows = append(out.Rows, Row{Kind: kind, Label: label, Content: content, Sequence: seq, Timestamp: ts, Detail: []Field{}})
		return len(out.Rows) - 1
	}
	if wire.Input != nil {
		seq := int64(0)
		i := add(User, "user", *wire.Input, &seq, wire.StartedAt)
		out.Rows[i].Detail = append(out.Rows[i].Detail, Field{"Input", *wire.Input})
	}
	open := make(map[string]int)
	step, requestNo := 0, 0
	for _, raw := range wire.Events {
		var fields map[string]json.RawMessage

		event, err := jsonvalue.Decode(string(raw))
		if err != nil || event.Kind() != jsonvalue.Object {
			return Ledger{}, ErrDocument
		}
		fields = make(map[string]json.RawMessage, len(event.Keys()))
		for _, name := range event.Keys() {
			child, _ := event.Lookup(name)
			fields[name], err = jsonvalue.AppendLegacy(nil, child)
			if err != nil {
				return Ledger{}, ErrDocument
			}
		}
		text := func(key, fallback string) string {
			v, ok := fields[key]
			if !ok {
				return fallback
			}
			if string(v) == "null" {
				return "None"
			}

			decoded, err := jsonvalue.Decode(string(v))
			if err == nil {
				if s, ok := decoded.Text(); ok {
					return s
				}
			}
			if bytes.Equal(v, []byte("true")) {
				return "True"
			}
			if bytes.Equal(v, []byte("false")) {
				return "False"
			}
			return PrettyJSON(v)
		}
		number := func(key string) *float64 {
			var v *float64
			if json.Unmarshal(fields[key], &v) != nil {
				return nil
			}
			return v
		}
		truth := func(key string) bool {
			v := strings.TrimSpace(string(fields[key]))
			return v != "" && v != "null" && v != "false" && v != "0" && v != "0.0" && v != `""` && v != "[]" && v != "{}"
		}
		detail := func(row *Row, name, key string) {
			v, ok := fields[key]
			if ok && !bytes.Equal(v, []byte("null")) {
				row.Detail = append(row.Detail, Field{name, PrettyJSON(v)})
			}
		}
		var seq *int64
		if value, present := fields["seq"]; present && json.Unmarshal(value, &seq) != nil {
			return Ledger{}, ErrDocument
		}
		ts := number("ts")
		depth := 0
		if v := number("depth"); v != nil {
			depth = int(*v)
		}
		var label *string
		if depth != 0 && fields["agent"] != nil && string(fields["agent"]) != "null" {
			v := text("agent", "")
			label = &v
		}
		nest := func(row *Row) { row.Depth = depth; row.Agent = label }
		span := text("span_id", "")
		if span == "None" {
			span = ""
		}
		kind := text("type", "event")
		switch kind {
		case "model_start":
			requestNo++
			if text("purpose", "") == "agent_turn" && depth == 0 {
				step++
				add(Step, fmt.Sprintf("step %d", step), "", seq, nil)
			}
			i := add(Model, fmt.Sprintf("#%d %s", requestNo, text("model", "model")), fmt.Sprintf("purpose=%s messages=%s", text("purpose", "None"), text("message_count", "None")), seq, ts)
			row := &out.Rows[i]
			nest(row)
			row.HasDuration = true
			row.Status = "in flight"
			row.Detail = append(row.Detail, Field{"Request", fmt.Sprint(requestNo)})
			for _, p := range [][2]string{{"Purpose", "purpose"}, {"Model", "model"}, {"Messages", "message_count"}, {"Tool catalog", "tool_count"}, {"Max tokens", "max_tokens"}} {
				detail(row, p[0], p[1])
			}
			if span != "" {
				open[span] = i
			}
		case "model_end", "tool_result":
			i, ok := open[span]
			if !ok {
				continue
			}
			delete(open, span)
			row := &out.Rows[i]
			row.DurationMS = number("duration_ms")
			if kind == "model_end" {
				row.Status = text("status", "completed")
				if truth("served_model") {
					served := text("served_model", "")
					row.Detail = append(row.Detail, Field{"Served by", served})
					requested := ""
					for _, f := range row.Detail {
						if f.Name == "Model" {
							requested = f.Text
						}
					}
					if requested != "" && requested != served {
						row.Content += " · served by " + served
					}
				}
				detail(row, "Stop reason", "stop_reason")
				row.Detail = append(row.Detail, Field{"Status", row.Status}, Field{"Duration", FormatDuration(row.DurationMS)})
				if truth("error") {
					row.Error = true
					detail(row, "Error", "error")
				}
				if truth("usage") {
					detail(row, "Usage", "usage")
					var usage struct {
						Input  int64 `json:"input_tokens"`
						Output int64 `json:"output_tokens"`
					}
					if json.Unmarshal(fields["usage"], &usage) != nil {
						return Ledger{}, ErrDocument
					}
					out.Metrics.InputTokens += usage.Input
					out.Metrics.OutputTokens += usage.Output
				}
				value := bytes.TrimSpace(fields["model_output"])
				if len(value) > 0 && (value[0] == '[' || value[0] == '{') {
					detail(row, "Output", "model_output")
				}
			} else {
				row.Error = truth("denied") || truth("error")
				row.Status = "completed"
				if truth("error") {
					row.Status = "error"
				}
				if truth("denied") {
					row.Status = "denied"
				}
				row.Content = row.Label + " -> " + text("output", "")
				if _, exists := fields["output"]; !exists {
					row.Detail = append(row.Detail, Field{"Output", ""})
				} else {
					detail(row, "Output", "output")
				}
				row.Detail = append(row.Detail, Field{"Status", row.Status}, Field{"Duration", FormatDuration(row.DurationMS)})
				if truth("replayed") {
					row.Detail = append(row.Detail, Field{"Replayed", "reused a recorded outcome"})
				}
				value := bytes.TrimSpace(fields["command_result"])
				if len(value) > 0 && value[0] == '{' {
					detail(row, "Command", "command_result")
				}
			}
		case "tool_use":
			content := PrettyJSON(fields["input"])
			if fields["input"] == nil {
				content = "{}"
			}
			i := add(Tool, text("name", "tool"), content, seq, ts)
			row := &out.Rows[i]
			nest(row)
			row.HasDuration = true
			row.Status = "in flight"
			for _, p := range [][2]string{{"Tool", "name"}, {"Input", "input"}, {"Call id", "id"}, {"Action id", "action_id"}} {
				detail(row, p[0], p[1])
			}
			if span != "" {
				open[span] = i
			}
		case "assistant_text", "steering_delivered":
			k, l, n := Assistant, "assistant", "Text"
			if kind == "steering_delivered" {
				k, l, n = Steer, "steer x"+text("count", "1"), "Interjection"
			}
			i := add(k, l, text("text", ""), seq, ts)
			row := &out.Rows[i]
			nest(row)
			row.Detail = append(row.Detail, Field{n, row.Content})
			if k == Steer {
				detail(row, "Steers delivered", "count")
			}
		case "tool_catalog", "capability_plan", "system_prompt":
			l, content := "catalog", ""
			if kind == "tool_catalog" {
				var schemas []json.RawMessage
				if truth("schemas") && json.Unmarshal(fields["schemas"], &schemas) != nil {
					return Ledger{}, ErrDocument
				}
				content = fmt.Sprintf("%d tools · fingerprint %s", len(schemas), text("fingerprint", "?"))
			} else if kind == "capability_plan" {
				l = "capability"
				confinement := "unconfined"
				if truth("sandbox_confined") {
					confinement = "confined"
				}
				content = text("permission_mode", "?") + " · " + confinement + " · " + text("fingerprint", "?")
			} else {
				l = "system"
				content = fmt.Sprintf("%s chars · hash %s", Comma(int64(utf8.RuneCountInString(text("text", "")))), text("hash", "?"))
			}
			i := add(Reference, l, content, seq, ts)
			row := &out.Rows[i]
			nest(row)
			if kind == "tool_catalog" {
				detail(row, "Fingerprint", "fingerprint")
				if truth("schemas") {
					detail(row, "Schemas", "schemas")
				} else {
					row.Detail = append(row.Detail, Field{"Schemas", "[]"})
				}
			} else if kind == "system_prompt" {
				detail(row, "Hash", "hash")
				detail(row, "System prompt", "text")
			} else {
				row.Detail = append(row.Detail, Field{"Capability plan", PrettyJSON(payload(raw))})
			}
		case "trajectory_start", "trajectory_end":
			continue
		case "done":
			if wire.Output != nil {
				continue
			}
			fallthrough
		default:
			k, l, n := Event, kind, "Payload"
			bad := false
			if kind == "compact" {
				k, l, n = Compaction, "compact/"+text("kind", "None"), "Compaction"
				bad = text("kind", "") == "failed"
			}
			if kind == "error" {
				k, l, n = Error, "error", "Error"
				bad = true
			}
			content := PrettyJSON(payload(raw))
			i := add(k, l, content, seq, ts)
			row := &out.Rows[i]
			nest(row)
			row.Error = bad
			row.Detail = append(row.Detail, Field{n, content})
		}
	}
	if wire.Output != nil || wire.Error != nil {
		final := ""
		if wire.Output != nil {
			final = *wire.Output
		}
		if wire.Error != nil && *wire.Error != "" {
			final = *wire.Error
		}
		i := add(Final, "final/"+string(out.Status), final, nil, wire.EndedAt)
		out.Rows[i].Error = wire.Error != nil
		out.Rows[i].Detail = append(out.Rows[i].Detail, Field{"Final", final})
	}
	if len(out.Rows) > MaxRows {
		out.Omitted = len(out.Rows) - MaxRows
		out.Rows = out.Rows[len(out.Rows)-MaxRows:]
	}
	if out.EndedAt == nil {
		out.EndedAt = out.StartedAt
		for _, row := range out.Rows {
			if row.Timestamp != nil && (out.EndedAt == nil || *row.Timestamp > *out.EndedAt) {
				out.EndedAt = row.Timestamp
			}
		}
	}
	return out, nil
}
