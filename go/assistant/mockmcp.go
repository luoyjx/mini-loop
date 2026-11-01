package assistant

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/mcp"
)

// MockServerName is the simulated mail/calendar MCP server.
const MockServerName = "mock-mailcal"

// Mock MCP tool names. The daemon wraps the three read tools and calendar_get
// as typed agent tools. mail_poll and the two effect tools are called only by
// daemon code (poller, outbox), and sim_* stand in for the outside world;
// none of those reach a model catalogue.
const (
	MCPMailList         = "mail_list"
	MCPMailRead         = "mail_read"
	MCPCalendarList     = "calendar_list"
	MCPCalendarGet      = "calendar_get"
	MCPMailPoll         = "mail_poll"
	MCPMailSend         = "mail_send"
	MCPCalendarRespond  = "calendar_respond"
	MCPSimDeliver       = "sim_deliver"
	MCPSimDeliverCustom = "sim_deliver_custom"
	MCPSimAddEvent      = "sim_add_event"
)

const mcpProtocolVersion = "2024-11-05"

// ---- typed tool arguments and results ----

type mcpMailListArgs struct {
	Folder MailFolder `json:"folder"`
	Limit  int        `json:"limit"`
}

type mcpIDArgs struct {
	ID string `json:"id"`
}

type mcpRangeArgs struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type mcpPollArgs struct {
	Cursor int64 `json:"cursor"`
	Limit  int   `json:"limit"`
}

type mcpPresetArgs struct {
	Preset Preset `json:"preset"`
}

type mailLookup struct {
	Found bool   `json:"found"`
	Mail  *Email `json:"mail,omitempty"`
}

type eventLookup struct {
	Found bool           `json:"found"`
	Event *CalendarEvent `json:"event,omitempty"`
}

// mcpFault is the tool-level error body. The go/mcp clients return content
// text only, so the connector recognises failures by this shape.
type mcpFault struct {
	Error string `json:"error"`
}

type mockTool struct {
	name, description, schema string
	readOnly, idempotent      bool
	run                       func(context.Context, *World, []byte) ([]byte, error)
}

// handle decodes one closed argument object, runs fn and encodes its result.
func handle[A, R any](fn func(context.Context, *World, A) (R, error)) func(context.Context, *World, []byte) ([]byte, error) {
	return func(ctx context.Context, w *World, data []byte) ([]byte, error) {
		var args A
		if err := strictJSON(data, &args); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
		result, err := fn(ctx, w, args)
		if err != nil {
			return nil, err
		}
		return json.Marshal(result)
	}
}

func strictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("arguments must be one JSON object")
	}
	return nil
}

var mockTools = []mockTool{
	{MCPMailList, "List inbox or sent mail, newest first.", `{"type":"object","properties":{"folder":{"type":"string","enum":["inbox","sent"]},"limit":{"type":"integer","minimum":1,"maximum":50}},"required":["folder"]}`, true, true,
		handle(func(ctx context.Context, w *World, a mcpMailListArgs) ([]Email, error) {
			if a.Folder != FolderInbox && a.Folder != FolderSent {
				return nil, errors.New("folder must be inbox or sent")
			}
			if a.Limit <= 0 || a.Limit > 50 {
				a.Limit = 50
			}
			return w.ListMail(ctx, a.Folder, a.Limit)
		})},
	{MCPMailRead, "Read one message by ID.", `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`, true, true,
		handle(func(ctx context.Context, w *World, a mcpIDArgs) (mailLookup, error) {
			mail, found, err := w.Mail(ctx, a.ID)
			if err != nil || !found {
				return mailLookup{}, err
			}
			return mailLookup{Found: true, Mail: &mail}, nil
		})},
	{MCPCalendarList, "List events overlapping [from, to).", `{"type":"object","properties":{"from":{"type":"string","format":"date-time"},"to":{"type":"string","format":"date-time"}},"required":["from","to"]}`, true, true,
		handle(func(ctx context.Context, w *World, a mcpRangeArgs) ([]CalendarEvent, error) {
			if !a.To.After(a.From) || a.To.Sub(a.From) > 31*24*time.Hour {
				return nil, errors.New("to must be after from and at most 31 days later")
			}
			return w.Events(ctx, a.From, a.To)
		})},
	{MCPCalendarGet, "Read one event by ID.", `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`, true, true,
		handle(func(ctx context.Context, w *World, a mcpIDArgs) (eventLookup, error) {
			event, found, err := w.Event(ctx, a.ID)
			if err != nil || !found {
				return eventLookup{}, err
			}
			return eventLookup{Found: true, Event: &event}, nil
		})},
	{MCPMailPoll, "Return inbox mail with seq > cursor, oldest first. limit 0 returns only the head seq.", `{"type":"object","properties":{"cursor":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":0,"maximum":100}},"required":["cursor","limit"]}`, true, true,
		handle(func(ctx context.Context, w *World, a mcpPollArgs) (MailBatch, error) {
			return w.Poll(ctx, a.Cursor, a.Limit)
		})},
	{MCPMailSend, "Send one message. Idempotent per idempotency_key.", `{"type":"object","properties":{"idempotency_key":{"type":"string"},"thread":{"type":"string"},"to":{"type":"string"},"subject":{"type":"string"},"body":{"type":"string"}},"required":["idempotency_key","to","subject","body"]}`, false, true,
		handle(func(ctx context.Context, w *World, a OutgoingMail) (EffectReceipt, error) { return w.Send(ctx, a) })},
	{MCPCalendarRespond, "Accept or decline an invite and notify the organizer. Idempotent per idempotency_key.", `{"type":"object","properties":{"idempotency_key":{"type":"string"},"event_id":{"type":"string"},"response":{"type":"string","enum":["accept","decline"]},"subject":{"type":"string"},"note":{"type":"string"}},"required":["idempotency_key","event_id","response"]}`, false, true,
		handle(func(ctx context.Context, w *World, a InviteReply) (EffectReceipt, error) {
			return w.RespondInvite(ctx, a)
		})},
	{MCPSimDeliver, "Simulator: deliver a preset inbound message.", `{"type":"object","properties":{"preset":{"type":"string"}},"required":["preset"]}`, false, false,
		handle(func(ctx context.Context, w *World, a mcpPresetArgs) (DeliveredMail, error) {
			return w.Deliver(ctx, a.Preset)
		})},
	{MCPSimDeliverCustom, "Simulator: deliver a composed inbound message.", `{"type":"object","properties":{"from":{"type":"string"},"from_name":{"type":"string"},"subject":{"type":"string"},"body":{"type":"string"},"bulk":{"type":"boolean"}},"required":["from","subject"]}`, false, false,
		handle(func(ctx context.Context, w *World, a CustomMail) (DeliveredMail, error) {
			return w.DeliverCustom(ctx, a)
		})},
	{MCPSimAddEvent, "Simulator: book a confirmed meeting that starts soon.", `{"type":"object","properties":{"title":{"type":"string"},"in_minutes":{"type":"integer","minimum":1,"maximum":1440},"minutes":{"type":"integer","minimum":5,"maximum":480}}}`, false, false,
		handle(func(ctx context.Context, w *World, a SimEvent) (CalendarEvent, error) { return w.AddEvent(ctx, a) })},
}

// callText runs one tool and renders its MCP text content: the result JSON,
// or an mcpFault with isError=true.
func (w *World) callText(ctx context.Context, name string, args []byte) (string, bool) {
	for _, tool := range mockTools {
		if tool.name != name {
			continue
		}
		result, err := tool.run(ctx, w, args)
		if err != nil {
			fault, _ := json.Marshal(mcpFault{Error: err.Error()})
			return string(fault), true
		}
		return string(result), false
	}
	fault, _ := json.Marshal(mcpFault{Error: "unknown tool " + name})
	return string(fault), true
}

// ---- tool descriptions ----

type mcpAnnotations struct {
	ReadOnly    bool `json:"readOnlyHint"`
	Destructive bool `json:"destructiveHint"`
	Idempotent  bool `json:"idempotentHint"`
	OpenWorld   bool `json:"openWorldHint"`
}

type mcpToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema jsonvalue.Value `json:"inputSchema"`
	Annotations mcpAnnotations  `json:"annotations"`
}

func mockToolInfos() ([]mcpToolInfo, error) {
	infos := make([]mcpToolInfo, 0, len(mockTools))
	for _, tool := range mockTools {
		schema, err := jsonvalue.Decode(tool.schema)
		if err != nil {
			return nil, fmt.Errorf("%s schema: %w", tool.name, err)
		}
		infos = append(infos, mcpToolInfo{Name: tool.name, Description: tool.description, InputSchema: schema, Annotations: mcpAnnotations{ReadOnly: tool.readOnly, Idempotent: tool.idempotent, OpenWorld: !tool.readOnly}})
	}
	return infos, nil
}

// NewInProcessMock exposes the same tools through go/mcp's in-process client,
// so tests cross the identical JSON argument and result boundary without a
// child process.
func NewInProcessMock(w *World) (*mcp.InProcessClient, error) {
	infos, err := mockToolInfos()
	if err != nil {
		return nil, err
	}
	tools := make([]mcp.InProcessTool, 0, len(infos))
	for _, info := range infos {
		name := info.Name
		annotations, err := toValue(info.Annotations)
		if err != nil {
			return nil, err
		}
		tools = append(tools, mcp.InProcessTool{
			Definition: mcp.ToolDescription{Name: name, Description: jsonvalue.TextValue(info.Description), InputSchema: info.InputSchema, Annotations: annotations},
			Handler: func(ctx context.Context, args mcp.InProcessValue) (mcp.InProcessValue, error) {
				data, err := args.MarshalJSON()
				if err != nil {
					return jsonvalue.Value{}, err
				}
				text, _ := w.callText(ctx, name, data)
				return jsonvalue.TextValue(text), ctx.Err()
			},
		})
	}
	return mcp.NewInProcess(MockServerName, tools), nil
}

func toValue(v any) (jsonvalue.Value, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	var value jsonvalue.Value
	err = json.Unmarshal(data, &value)
	return value, err
}

// ---- stdio JSON-RPC server ----

type rpcRequest struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *jsonvalue.Value `json:"id"`
	Method  string           `json:"method"`
	Params  jsonvalue.Value  `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      jsonvalue.Value  `json:"id"`
	Result  *jsonvalue.Value `json:"result,omitempty"`
	Error   *rpcError        `json:"error,omitempty"`
}

type mcpCallParams struct {
	Name      string          `json:"name"`
	Arguments jsonvalue.Value `json:"arguments"`
}

type mcpTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mcpCallResult struct {
	Content []mcpTextContent `json:"content"`
	IsError bool             `json:"isError"`
}

type mcpServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type mcpToolsCapability struct {
	ListChanged bool `json:"listChanged"`
}

type mcpCapabilities struct {
	Tools mcpToolsCapability `json:"tools"`
}

type mcpInitializeResult struct {
	ProtocolVersion string          `json:"protocolVersion"`
	Capabilities    mcpCapabilities `json:"capabilities"`
	ServerInfo      mcpServerInfo   `json:"serverInfo"`
}

type mcpToolsList struct {
	Tools []mcpToolInfo `json:"tools"`
}

const maxRPCLine = 1 << 20

// ServeMCP answers newline-delimited JSON-RPC on in/out until in closes, as an
// MCP stdio server: initialize, ping, tools/list and tools/call. Requests are
// handled one at a time, matching the client's serialized RPCs.
func ServeMCP(ctx context.Context, w *World, in io.Reader, out io.Writer) error {
	infos, err := mockToolInfos()
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64<<10), maxRPCLine)
	writer := bufio.NewWriter(out)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var request rpcRequest
		response := rpcResponse{JSONRPC: "2.0", ID: jsonvalue.NullValue()}
		if err := json.Unmarshal(line, &request); err != nil {
			response.Error = &rpcError{-32700, "parse error"}
		} else if request.ID == nil {
			continue // notification, e.g. notifications/initialized
		} else {
			response.ID = *request.ID
			result, fault := answer(ctx, w, infos, request)
			if fault != nil {
				response.Error = fault
			} else if value, err := toValue(result); err != nil {
				response.Error = &rpcError{-32603, err.Error()}
			} else {
				response.Result = &value
			}
		}
		data, err := json.Marshal(response)
		if err != nil {
			return err
		}
		if _, err := writer.Write(append(data, '\n')); err != nil {
			return err
		}
		if err := writer.Flush(); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func answer(ctx context.Context, w *World, infos []mcpToolInfo, request rpcRequest) (any, *rpcError) {
	switch request.Method {
	case "initialize":
		return mcpInitializeResult{ProtocolVersion: mcpProtocolVersion, ServerInfo: mcpServerInfo{Name: MockServerName, Version: "0.1.0"}}, nil
	case "ping":
		return struct{}{}, nil
	case "tools/list":
		return mcpToolsList{Tools: infos}, nil
	case "tools/call":
		data, err := request.Params.MarshalJSON()
		if err != nil {
			return nil, &rpcError{-32602, err.Error()}
		}
		var params mcpCallParams
		if err := json.Unmarshal(data, &params); err != nil || params.Name == "" {
			return nil, &rpcError{-32602, "tools/call needs a name and object arguments"}
		}
		if params.Arguments.Kind() == jsonvalue.Null {
			params.Arguments = jsonvalue.ObjectValue(nil)
		}
		if params.Arguments.Kind() != jsonvalue.Object {
			return nil, &rpcError{-32602, "tools/call arguments must be an object"}
		}
		args, err := params.Arguments.MarshalJSON()
		if err != nil {
			return nil, &rpcError{-32602, err.Error()}
		}
		text, isError := w.callText(ctx, params.Name, args)
		return mcpCallResult{Content: []mcpTextContent{{Type: "text", Text: text}}, IsError: isError}, nil
	default:
		return nil, &rpcError{-32601, "method not found: " + request.Method}
	}
}
