package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"math"
	"math/big"
	"reflect"

	"github.com/luoyjx/mini-loop/go/efficiency"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type protocolBlockShape struct {
	Kind   string
	Stable *string
}
type protocolMessageShape struct {
	Metadata string
	Blocks   []protocolBlockShape
}
type requestProjectionKey struct {
	Index  int
	Digest [32]byte
}

func nonfiniteProtocolValue(v jsonvalue.Value) bool {
	if n, ok := v.Float(); ok {
		return math.IsNaN(n) || math.IsInf(n, 0)
	}
	if values, ok := v.Array(); ok {
		for _, item := range values {
			if nonfiniteProtocolValue(item) {
				return true
			}
		}
	}
	for _, key := range v.Keys() {
		value, _ := v.Lookup(key)
		if nonfiniteProtocolValue(value) {
			return true
		}
	}
	return false
}
func canonicalProtocolValue(v jsonvalue.Value) string {
	if nonfiniteProtocolValue(v) {
		return v.PythonString()
	}
	text, err := v.Sorted().LegacyString()
	if err != nil {
		return v.PythonString()
	}
	return text
}
func withoutProtocolField(v jsonvalue.Value, omitted string) jsonvalue.Value {
	fields := []jsonvalue.Field{}
	for _, key := range v.Keys() {
		if key != omitted {
			value, _ := v.Lookup(key)
			fields = append(fields, jsonvalue.Field{Name: key, Value: value})
		}
	}
	return jsonvalue.ObjectValue(fields)
}
func messageProtocolShape(v jsonvalue.Value) ([]protocolMessageShape, bool) {
	messages, ok := v.Array()
	if !ok {
		return nil, false
	}
	shape := []protocolMessageShape{}
	for _, message := range messages {
		role, _ := message.Lookup("role")
		name, _ := role.Text()
		if message.Kind() != jsonvalue.Object || (name != "user" && name != "assistant") {
			return nil, false
		}
		row := protocolMessageShape{Metadata: canonicalProtocolValue(withoutProtocolField(message, "content")), Blocks: []protocolBlockShape{}}
		content, _ := message.Lookup("content")
		if blocks, ok := content.Array(); ok {
			for _, block := range blocks {
				kind := "opaque"
				stable := canonicalProtocolValue(block)
				if block.Kind() == jsonvalue.Object {
					typeValue, _ := block.Lookup("type")
					kind = typeValue.PythonString()
					if kind == "text" {
						stable = canonicalProtocolValue(withoutProtocolField(block, "text"))
					} else if kind == "tool_result" {
						stable = canonicalProtocolValue(withoutProtocolField(block, "content"))
					}
				}
				row.Blocks = append(row.Blocks, protocolBlockShape{kind, &stable})
			}
		} else if _, ok := content.Text(); ok {
			row.Blocks = append(row.Blocks, protocolBlockShape{Kind: "text"})
		} else {
			stable := canonicalProtocolValue(content)
			row.Blocks = append(row.Blocks, protocolBlockShape{"opaque-content", &stable})
		}
		shape = append(shape, row)
	}
	return shape, true
}

func protocolMessagesValue(messages []protocol.Message) (jsonvalue.Value, error) {
	data, err := json.Marshal(messages)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	return jsonvalue.Decode(string(data))
}
func messageProjectionDigest(message protocol.Message) ([32]byte, error) {
	data, err := json.Marshal(message)
	if err != nil {
		return [32]byte{}, err
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256([]byte(canonicalProtocolValue(value))), nil
}

// Only eligible text/result payloads are reconstructed. Immutable protocol
// blocks retain their named variants and tool inputs. No frame decoder size cap
// is imposed on legitimate accumulated transcript rows.
func lowerRequestProjection(original []protocol.Message, value jsonvalue.Value) ([]protocol.Message, bool) {
	rows, ok := value.Array()
	if !ok || len(rows) != len(original) {
		return nil, false
	}
	out := append([]protocol.Message(nil), original...)
	for i, row := range rows {
		content, _ := row.Lookup("content")
		if _, plain := original[i].Content.Plain(); plain {
			text, ok := content.Text()
			if !ok {
				return nil, false
			}
			out[i].Content = protocol.PlainContent(text)
			continue
		}
		blocks, _ := original[i].Content.Blocks()
		candidates, ok := content.Array()
		if !ok || len(candidates) != len(blocks) {
			return nil, false
		}
		for j, block := range blocks {
			switch block.Kind() {
			case protocol.BlockText:
				v, _ := candidates[j].Lookup("text")
				text, ok := v.Text()
				if !ok {
					return nil, false
				}
				blocks[j] = protocol.NewTextBlock(text)
			case protocol.BlockToolResult:
				v, _ := candidates[j].Lookup("content")
				text, ok := v.Text()
				if !ok {
					return nil, false
				}
				result, _ := block.ToolResult()
				blocks[j] = protocol.NewToolResult(result.ToolUseID, text, result.IsError)
			}
		}
		out[i].Content = protocol.BlockContent(blocks...)
	}
	return out, true
}

func (s *Session) optimizeRequestProjection(ctx context.Context, request protocol.ModelRequest, liveHistory bool) (protocol.ModelRequest, error) {
	if s.tokenEfficiency == nil {
		return request, nil
	}
	request = request.Clone()
	live := liveHistory && request.Purpose == protocol.PurposeAgentTurn
	keys := make([]requestProjectionKey, len(request.Messages))
	if live {
		valid := make(map[requestProjectionKey]bool)
		for i, message := range request.Messages {
			digest, err := messageProjectionDigest(message)
			if err != nil {
				return protocol.ModelRequest{}, err
			}
			key := requestProjectionKey{i, digest}
			keys[i] = key
			valid[key] = true
			if projected, ok := s.requestProjections[key]; ok {
				request.Messages[i] = protocol.Message{Role: projected.Role, Content: projected.Content.Clone()}
			}
		}
		for key := range s.requestProjections {
			if !valid[key] {
				delete(s.requestProjections, key)
			}
		}
	}
	before, err := protocolMessagesValue(request.Messages)
	if err != nil {
		return protocol.ModelRequest{}, err
	}
	document, err := efficiency.NewRequestDocument(jsonvalue.ObjectValue([]jsonvalue.Field{{Name: "messages", Value: before}}))
	if err != nil {
		return protocol.ModelRequest{}, err
	}
	frozen := len(request.Messages) - 1
	if frozen < 0 {
		frozen = 0
	}
	if len(request.Messages) > 0 && request.Messages[len(request.Messages)-1].Role == protocol.RoleAssistant {
		frozen = len(request.Messages)
	}
	input, err := efficiency.NewOptimizationContext(efficiency.OptimizationContextConfig{Request: document, FrozenPrefixMessages: big.NewInt(int64(frozen))})
	if err != nil {
		return protocol.ModelRequest{}, err
	}
	outcome, err := s.tokenEfficiency.OptimizeRequest(ctx, input, efficiency.RequestOptimizationOptions{BudgetTokens: big.NewInt(int64(s.tokenThreshold))})
	if err != nil {
		return protocol.ModelRequest{}, err
	}
	output, err := outcome.Snapshot()
	if err != nil {
		return protocol.ModelRequest{}, err
	}
	for _, receipt := range output.Receipts {
		s.events.append(SessionEvent{kind: EventOptimizationReceipt, optimizationReceipt: optimizationMetrics(receipt)})
	}
	value, err := output.Context.Request.Value()
	if err != nil {
		return protocol.ModelRequest{}, err
	}
	candidate, _ := value.Lookup("messages")
	originalShape, validOriginal := messageProtocolShape(before)
	candidateShape, validCandidate := messageProtocolShape(candidate)
	adopted := false
	if validOriginal && validCandidate && reflect.DeepEqual(originalShape, candidateShape) {
		if messages, ok := lowerRequestProjection(request.Messages, candidate); ok {
			request.Messages = messages
			adopted = true
		}
	}
	if !adopted {
		s.events.append(SessionEvent{kind: EventRequestOptimizationRejected, requestRejected: RequestOptimizationRejectedEvent{Reason: "message_protocol_guard"}})
	}
	if live {
		if s.requestProjections == nil {
			s.requestProjections = make(map[requestProjectionKey]protocol.Message)
		}
		for i, message := range request.Messages {
			digest, err := messageProjectionDigest(message)
			if err != nil {
				return protocol.ModelRequest{}, err
			}
			if digest == keys[i].Digest {
				delete(s.requestProjections, keys[i])
			} else {
				s.requestProjections[keys[i]] = protocol.Message{Role: message.Role, Content: message.Content.Clone()}
			}
		}
	}
	return request, nil
}
