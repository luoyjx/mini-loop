package teams

import (
	"errors"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"strings"
)

func RenderMessages(messages []Message) (string, error) {
	cleaned := make([]Data, 0, len(messages))
	for _, message := range messages {
		sender, ok := messageField(message, "from", Text("")).Text()
		if !ok {
			return "", errors.New("team message sender must be text")
		}
		parts := strings.Split(sender, "/")
		cleaned = append(cleaned, Object(Field{"from", Text(parts[len(parts)-1])}, Field{"type", messageField(message, "type", Text("message"))}, Field{"content", messageField(message, "content", Text(""))}, Field{"metadata", messageField(message, "metadata", Object())}))
	}
	out, err := jsonvalue.AppendLegacyIndent(Array(cleaned...))
	return string(out), err
}
func RenderProtocols(states []ProtocolState) (string, error) {
	rows := make([]Data, 0, len(states))
	for _, state := range states {
		rows = append(rows, Object(Field{"request_id", Text(string(state.RequestID))}, Field{"type", Text(string(state.Type))}, Field{"sender", Text(string(state.Sender))}, Field{"target", Text(string(state.Target))}, Field{"status", Text(string(state.Status))}, Field{"payload", Text(state.Payload)}, Field{"created_at", jsonvalue.FloatValue(state.CreatedAt)}, Field{"feedback", Text(state.Feedback)}))
	}
	out, err := jsonvalue.AppendLegacyIndent(Array(rows...))
	return string(out), err
}
