package teams

import "github.com/luoyjx/mini-loop/go/internal/jsonvalue"

type TeamID string
type MemberName string
type MailboxKey string
type MessageType string

const (
	MessagePlanApprovalRequest MessageType = "plan_approval_request"
	MessageText                MessageType = "message"
	MessageBroadcast           MessageType = "broadcast"
	MessageNotice              MessageType = "notice"
	MessageShutdownRequest     MessageType = "shutdown_request"
	MessageShutdownResponse    MessageType = "shutdown_response"
	MessagePlanRequest         MessageType = "plan_request"
	MessagePlanResponse        MessageType = "plan_approval_response"
)

type Identity struct {
	Team TeamID
	Name MemberName
}

func (identity Identity) Key() MailboxKey { return Key(identity.Team, identity.Name) }
func Key(team TeamID, name MemberName) MailboxKey {
	return MailboxKey(string(team) + "/" + string(name))
}

// Data and Field are closed JSON variants for source-defined metadata and
// historical rows. They contain no arbitrary Go object or retained raw JSON.
type Data = jsonvalue.Value
type Field struct {
	Name  string
	Value Data
}

func Text(value string) Data { return jsonvalue.TextValue(value) }
func Bool(value bool) Data   { return jsonvalue.BoolValue(value) }
func Object(fields ...Field) Data {
	values := make([]jsonvalue.Field, len(fields))
	for i, field := range fields {
		values[i] = jsonvalue.Field{Name: field.Name, Value: field.Value}
	}
	return jsonvalue.ObjectValue(values)
}
func Array(items ...Data) Data             { return jsonvalue.ArrayValue(items) }
func DecodeData(data string) (Data, error) { return jsonvalue.Decode(data) }

// Metadata is always an object for newly produced messages. Historical mailbox
// rows may have a different shape and remain data rather than protocol authority.
type Metadata struct{ fields Data }

func NewMetadata(fields ...Field) Metadata { return Metadata{Object(fields...)} }
func (metadata Metadata) data() Data {
	if metadata.fields.Kind() != jsonvalue.Object {
		return Object()
	}
	return metadata.fields
}

type SendRequest struct {
	From     MailboxKey
	To       MailboxKey
	Content  string
	Type     *MessageType
	Metadata Metadata
	Extra    []Field
}

type SendStatus string

const (
	Sent    SendStatus = "sent"
	Refused SendStatus = "refused"
)

type SendResult struct {
	Status SendStatus
	Text   string
}

// Message retains an immutable historical object, including missing/unknown
// fields. SendRequest is the concrete producer; mailbox data grants no authority.
type Message struct{ data Data }

func (message Message) Data() Data                   { return message.data }
func (message Message) MarshalJSON() ([]byte, error) { return message.data.MarshalJSON() }
func (message Message) Content() (string, bool) {
	value, present := message.data.Lookup("content")
	text, ok := value.Text()
	return text, present && ok
}
func messageFromData(data Data) (Message, bool) {
	return Message{data}, data.Kind() == jsonvalue.Object
}
