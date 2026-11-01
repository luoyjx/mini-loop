package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/mcp"
	"github.com/luoyjx/mini-loop/go/secrets"
)

const defaultConnectorTimeout = 10 * time.Second

// mcpCaller is the part of the go/mcp clients the connector uses. The stdio
// client (a child process, the default) and the in-process client (tests)
// both satisfy it.
type mcpCaller interface {
	Name() string
	ListTools(ctx context.Context) ([]mcp.ToolDescription, error)
	CallTool(ctx context.Context, name string, arguments jsonvalue.Value) (string, error)
	Close() error
}

// requiredTools is the contract the daemon needs from a mail/calendar server.
var requiredTools = []string{MCPMailList, MCPMailRead, MCPCalendarList, MCPCalendarGet, MCPMailPoll, MCPMailSend, MCPCalendarRespond}

// Connector is the daemon's only path to mail and calendar. Every read, poll
// and effect is one MCP tools/call with typed arguments and a typed result.
// It does not register MCP tools in the agent catalogue: the daemon wraps
// the read tools itself with operator-declared risk, and keeps mail_poll and
// the effect tools out of model reach.
type Connector struct {
	client    mcpCaller
	transport string
	timeout   time.Duration
}

// ToolError is a failure reported by the MCP server itself (bad arguments,
// unknown event). Retrying the same call cannot succeed.
type ToolError struct {
	Tool    string
	Message string
}

func (e *ToolError) Error() string { return fmt.Sprintf("MCP %s refused: %s", e.Tool, e.Message) }

func NewConnector(client mcpCaller, transport string) *Connector {
	return &Connector{client: client, transport: transport, timeout: defaultConnectorTimeout}
}

// NewStdioConnector starts the mock server lazily as a child process on first
// use and restarts it on the next call after it dies. Credential-shaped
// environment variables (*_API_KEY, *_TOKEN, ...) are withheld from the child.
func NewStdioConnector(command []string) (*Connector, error) {
	scrub := secrets.FromEnvironment(secrets.Config{}, nil, secrets.DefaultPatterns(), nil)
	client, err := mcp.NewStdio(mcp.StdioConfig{Name: MockServerName, Command: command, Secrets: scrub, Timeout: defaultConnectorTimeout})
	if err != nil {
		return nil, err
	}
	return NewConnector(client, "stdio"), nil
}

func (c *Connector) Server() string    { return c.client.Name() }
func (c *Connector) Transport() string { return c.transport }
func (c *Connector) Close() error      { return c.client.Close() }

// Verify starts the server if needed and checks it offers every required
// tool, so a broken or mismatched server fails at startup, not mid-trigger.
func (c *Connector) Verify(ctx context.Context) (int, error) {
	bounded, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	tools, err := c.client.ListTools(bounded)
	if err != nil {
		return 0, fmt.Errorf("MCP %s tools/list: %w", c.client.Name(), err)
	}
	offered := map[string]bool{}
	for _, tool := range tools {
		offered[tool.Name] = true
	}
	var missing []string
	for _, name := range requiredTools {
		if !offered[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return len(tools), fmt.Errorf("MCP %s lacks required tools %v", c.client.Name(), missing)
	}
	return len(tools), nil
}

// call encodes args, runs one bounded tools/call and decodes the result text.
func call[R any](ctx context.Context, c *Connector, tool string, args any) (R, error) {
	var result R
	arguments, err := toValue(args)
	if err != nil {
		return result, err
	}
	bounded, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	text, err := c.client.CallTool(bounded, tool, arguments)
	if err != nil {
		return result, fmt.Errorf("MCP %s.%s: %w", c.client.Name(), tool, err)
	}
	var fault mcpFault
	if json.Unmarshal([]byte(text), &fault) == nil && fault.Error != "" {
		return result, &ToolError{Tool: tool, Message: fault.Error}
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return result, fmt.Errorf("MCP %s returned an unexpected result: %s", tool, truncateRunes(text, 120))
	}
	return result, nil
}

func (c *Connector) ListMail(ctx context.Context, folder MailFolder, limit int) ([]Email, error) {
	return call[[]Email](ctx, c, MCPMailList, mcpMailListArgs{Folder: folder, Limit: limit})
}

func (c *Connector) ReadMail(ctx context.Context, id string) (Email, bool, error) {
	found, err := call[mailLookup](ctx, c, MCPMailRead, mcpIDArgs{ID: id})
	if err != nil || !found.Found || found.Mail == nil {
		return Email{}, false, err
	}
	return *found.Mail, true, nil
}

func (c *Connector) Events(ctx context.Context, from, to time.Time) ([]CalendarEvent, error) {
	return call[[]CalendarEvent](ctx, c, MCPCalendarList, mcpRangeArgs{From: from, To: to})
}

func (c *Connector) Event(ctx context.Context, id string) (CalendarEvent, bool, error) {
	found, err := call[eventLookup](ctx, c, MCPCalendarGet, mcpIDArgs{ID: id})
	if err != nil || !found.Found || found.Event == nil {
		return CalendarEvent{}, false, err
	}
	return *found.Event, true, nil
}

func (c *Connector) Poll(ctx context.Context, cursor int64, limit int) (MailBatch, error) {
	return call[MailBatch](ctx, c, MCPMailPoll, mcpPollArgs{Cursor: cursor, Limit: limit})
}

func (c *Connector) SendMail(ctx context.Context, out OutgoingMail) (EffectReceipt, error) {
	return call[EffectReceipt](ctx, c, MCPMailSend, out)
}

func (c *Connector) RespondInvite(ctx context.Context, reply InviteReply) (EffectReceipt, error) {
	return call[EffectReceipt](ctx, c, MCPCalendarRespond, reply)
}

// DeliverPreset and DeliverCustom drive the simulated outside world. Only the
// demo page uses them; a real connector would have no such tools.
func (c *Connector) DeliverPreset(ctx context.Context, preset Preset) (DeliveredMail, error) {
	return call[DeliveredMail](ctx, c, MCPSimDeliver, mcpPresetArgs{Preset: preset})
}

func (c *Connector) DeliverCustom(ctx context.Context, custom CustomMail) (DeliveredMail, error) {
	return call[DeliveredMail](ctx, c, MCPSimDeliverCustom, custom)
}

// retryable reports whether an effect failure may succeed on a later attempt.
func retryable(err error) bool {
	var refused *ToolError
	return !errors.As(err, &refused)
}
