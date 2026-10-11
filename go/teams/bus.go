package teams

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/problems"
)

const MaxContent = 16000
const MaxInbox = 100
const MaxReadBytes = MaxInbox * (MaxContent + 4096)

var ErrKey = errors.New("mailbox keys must be '<safe-team>/<safe-name>'")
var ErrEncoding = errors.New("mailbox is not valid UTF-8")
var ErrMasking = errors.New("team message masking failed")
var ErrFields = errors.New("message extension contains a Python parameter name")
var safeComponent = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

type TextMasker interface{ MaskText(string) string }
type Config struct {
	// Nil selects in-memory storage. An established empty root means cwd.
	Root   *string
	Masker TextMasker
}

// Bus serializes send/peek/read within one instance. Persisted files are not a
// cross-process transaction or fenced queue, and the trusted root is not a sandbox.
type Bus struct {
	mu       sync.Mutex
	root     *string
	masker   TextMasker
	inboxes  map[MailboxKey][]Message
	problems problems.Log
	now      func() float64
}

func New(config Config) *Bus {
	var root *string
	if config.Root != nil {
		value := *config.Root
		root = &value
	}
	return &Bus{root: root, masker: config.Masker, inboxes: make(map[MailboxKey][]Message), now: func() float64 { return float64(time.Now().UnixNano()) / 1e9 }}
}
func (bus *Bus) Problems() problems.Snapshot { return bus.problems.Snapshot() }
func (bus *Bus) path(key MailboxKey) (string, error) {
	team, name, separator := strings.Cut(string(key), "/")
	if !separator || team == "." || team == ".." || name == "." || name == ".." || !safeComponent.MatchString(team) || !safeComponent.MatchString(name) {
		return "", ErrKey
	}
	return filepath.Join(*bus.root, team, "inboxes", name+".jsonl"), nil
}
func (bus *Bus) appendProblem(message string) { _ = bus.problems.Append(message) }
func decimalGrouped(value int) string {
	raw := fmt.Sprint(value)
	for i := len(raw) - 3; i > 0; i -= 3 {
		raw = raw[:i] + "," + raw[i:]
	}
	return raw
}
func (bus *Bus) Send(ctx context.Context, request SendRequest) (SendResult, error) {
	if err := ctx.Err(); err != nil {
		return SendResult{}, err
	}
	for _, field := range request.Extra {
		switch field.Name {
		case "frm", "to", "content", "msg_type", "metadata":
			return SendResult{}, ErrFields
		}
	}
	length := jsonvalue.RuneCount(request.Content)
	if length > MaxContent {
		return SendResult{Refused, fmt.Sprintf("Error: message is %s characters; the limit is 16,000", decimalGrouped(length))}, nil
	}
	kind := MessageText
	if request.Type != nil {
		kind = *request.Type
	}
	fields := []Field{{"from", Text(string(request.From))}, {"to", Text(string(request.To))}, {"content", Text(request.Content)}, {"type", Text(string(kind))}, {"metadata", request.Metadata.data()}, {"ts", jsonvalue.FloatValue(bus.now())}}
	fields = append(fields, request.Extra...)
	message := Message{Object(fields...)}
	bus.mu.Lock()
	defer bus.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return SendResult{}, err
	}
	if bus.root == nil {
		inbox := append(bus.inboxes[request.To], message)
		if len(inbox) > MaxInbox {
			inbox = append([]Message{}, inbox[len(inbox)-MaxInbox:]...)
		}
		bus.inboxes[request.To] = inbox
	} else {
		path, err := bus.path(request.To)
		if err != nil {
			return SendResult{Refused, "Error: " + err.Error()}, nil
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return SendResult{}, err
		}
		data, err := maskedMessage(message, bus.masker)
		if err != nil {
			return SendResult{}, err
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
		if err != nil {
			return SendResult{}, err
		}
		_, writeErr := file.Write(append(data, '\n'))
		closeErr := file.Close()
		if writeErr != nil {
			return SendResult{}, writeErr
		}
		if closeErr != nil {
			return SendResult{}, closeErr
		}
	}
	parts := strings.Split(string(request.To), "/")
	return SendResult{Sent, fmt.Sprintf("Sent %s to %s", kind, parts[len(parts)-1])}, nil
}
func maskedMessage(message Message, masker TextMasker) (data []byte, err error) {
	defer func() {
		if recover() != nil {
			data = nil
			err = ErrMasking
		}
	}()
	value := message.data
	if masker != nil {
		value = value.MapStrings(masker.MaskText)
	}
	return jsonvalue.AppendLegacySpaced(value)
}
func (bus *Bus) Peek(ctx context.Context, key MailboxKey) ([]Message, error) {
	return bus.load(ctx, key, false)
}
func (bus *Bus) Read(ctx context.Context, key MailboxKey) ([]Message, error) {
	return bus.load(ctx, key, true)
}
func (bus *Bus) load(ctx context.Context, key MailboxKey, consume bool) ([]Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	bus.mu.Lock()
	defer bus.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if bus.root == nil {
		messages := append([]Message{}, bus.inboxes[key]...)
		if consume {
			bus.inboxes[key] = []Message{}
		}
		if len(messages) > MaxInbox {
			if !consume {
				return messages[len(messages)-MaxInbox:], nil
			}
			dropped := len(messages) - (MaxInbox - 1)
			bus.appendProblem(fmt.Sprintf("inbox %s: %d messages dropped unread", key, dropped))
			return append([]Message{overflowNotice(key, &dropped)}, messages[len(messages)-(MaxInbox-1):]...), nil
		}
		return messages, nil
	}
	path, err := bus.path(key)
	if err != nil {
		action := "peek"
		if consume {
			action = "read"
		}
		bus.appendProblem(fmt.Sprintf("%s(%s) refused: %s", action, pytext.Repr(string(key)), err))
		return []Message{}, nil
	}
	text, truncated, exists, err := readTail(path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return []Message{}, nil
	}
	if consume {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	messages := []Message{}
	for _, line := range splitLines(text) {
		value, err := jsonvalue.Decode(line)
		if errors.Is(err, jsonvalue.ErrSyntax) {
			if consume {
				bus.appendProblem(path + ": a malformed message was dropped")
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		if message, ok := messageFromData(value); ok {
			messages = append(messages, message)
		}
	}
	if !consume {
		if len(messages) > MaxInbox {
			messages = messages[len(messages)-MaxInbox:]
		}
		return messages, nil
	}
	if truncated || len(messages) > MaxInbox {
		keep := MaxInbox - 1
		var dropped *int
		if len(messages) > keep {
			n := len(messages) - keep
			dropped = &n
		}
		if truncated {
			bus.appendProblem(fmt.Sprintf("%s: mailbox exceeded %s bytes; older messages dropped unread", path, decimalGrouped(MaxReadBytes)))
		} else {
			bus.appendProblem(fmt.Sprintf("%s: %d messages delivered at once; %d dropped", path, len(messages), *dropped))
		}
		if len(messages) > keep {
			messages = messages[len(messages)-keep:]
		}
		messages = append([]Message{overflowNotice(key, dropped)}, messages...)
	}
	return messages, nil
}
func readTail(path string) (string, bool, bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, false, nil
	}
	if err != nil {
		return "", false, false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", false, true, err
	}
	truncated := info.Size() > MaxReadBytes
	if truncated {
		if _, err := file.Seek(info.Size()-MaxReadBytes, io.SeekStart); err != nil {
			return "", true, true, err
		}
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxReadBytes))
	if err != nil {
		return "", truncated, true, err
	}
	if !truncated {
		if !utf8.Valid(data) {
			return "", false, true, ErrEncoding
		}
		return string(data), false, true, nil
	}
	// Source ignores invalid UTF-8 at the seek boundary, then discards the first
	// partial physical line, even if the seek happened to land on a line start.
	var decoded strings.Builder
	for len(data) > 0 {
		r, n, issue := pytext.DecodeRune(data)
		data = data[n:]
		if issue == pytext.ValidUTF8 {
			decoded.WriteRune(r)
		}
	}
	text := decoded.String()
	if newline := strings.IndexByte(text, '\n'); newline >= 0 {
		return text[newline+1:], true, true, nil
	}
	return "", true, true, nil
}
func splitLines(text string) []string {
	if text == "" {
		return []string{}
	}
	// Unlike FieldsFunc, empty logical lines remain malformed JSON records.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\v' || r == '\f' || r == 0x1c || r == 0x1d || r == 0x1e || r == 0x85 || r == 0x2028 || r == 0x2029 {
			return '\n'
		}
		return r
	}, text)
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
func overflowNotice(key MailboxKey, dropped *int) Message {
	detail := "the mailbox exceeded its size bound; an unknown number of older messages were dropped unread"
	if dropped != nil {
		detail = fmt.Sprintf("%d older messages were dropped unread", *dropped)
	}
	return Message{Object(Field{"from", Text("mailbox")}, Field{"to", Text(string(key))}, Field{"type", Text(string(MessageNotice))}, Field{"content", Text("[inbox overflow: " + detail + ". Ask senders to resend anything critical.]")})}
}
