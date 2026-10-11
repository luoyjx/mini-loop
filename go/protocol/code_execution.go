package protocol

import (
	"encoding/json"
	"errors"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// ExecutionReturnCode preserves the SDK's Python integer without machine-width
// truncation. Only the integer variant is valid; JSON strings/floats are refused.
type ExecutionReturnCode struct{ value jsonvalue.Value }

func NewExecutionReturnCode(code int64) ExecutionReturnCode {
	return ExecutionReturnCode{value: jsonvalue.IntegerValue(code)}
}
func (c ExecutionReturnCode) Decimal() string { value, _ := c.value.Integer(); return value }
func (c ExecutionReturnCode) Validate() error {
	if c.value.Kind() != jsonvalue.Integer {
		return errors.New("execution return code must be an integer")
	}
	return nil
}
func (c ExecutionReturnCode) MarshalJSON() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c.value.MarshalJSON()
}
func (c *ExecutionReturnCode) UnmarshalJSON(data []byte) error {
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return err
	}
	next := ExecutionReturnCode{value: value}
	if err := next.Validate(); err != nil {
		return err
	}
	*c = next
	return nil
}

type CodeExecutionOutput struct {
	FileID string
	extra  jsonvalue.Value
}

func (f CodeExecutionOutput) MarshalJSON() ([]byte, error) {
	return marshalMetadataObject(struct {
		Type   string `json:"type"`
		FileID string `json:"file_id"`
	}{"code_execution_output", f.FileID}, f.extra)
}
func decodeCodeExecutionOutput(data []byte, bounded bool) (CodeExecutionOutput, error) {
	var wire struct {
		Type   *string
		FileID *string `json:"file_id"`
	}
	_, extra, err := decodeMetadataObject(data, &wire, []string{"type", "file_id"}, bounded)
	if err != nil {
		return CodeExecutionOutput{}, err
	}
	if wire.Type == nil || *wire.Type != "code_execution_output" || wire.FileID == nil {
		return CodeExecutionOutput{}, errors.New("code execution output requires type and file_id")
	}
	return CodeExecutionOutput{FileID: *wire.FileID, extra: extra}, nil
}
func (f *CodeExecutionOutput) UnmarshalJSON(data []byte) error {
	next, err := decodeCodeExecutionOutput(data, true)
	if err == nil {
		*f = next
	}
	return err
}
func decodeCodeExecutionOutputs(rows *[]json.RawMessage, bounded bool) ([]CodeExecutionOutput, error) {
	if rows == nil {
		return nil, errors.New("code execution result requires output array")
	}
	result := []CodeExecutionOutput{}
	for _, data := range *rows {
		output, err := decodeCodeExecutionOutput(data, bounded)
		if err != nil {
			return nil, err
		}
		result = append(result, output)
	}
	return result, nil
}

type CodeExecutionResult struct {
	OutputFiles    []CodeExecutionOutput
	ReturnCode     ExecutionReturnCode
	Stdout, Stderr string
	extra          jsonvalue.Value
}

func (r CodeExecutionResult) clone() CodeExecutionResult {
	r.OutputFiles = append([]CodeExecutionOutput{}, r.OutputFiles...)
	return r
}
func (r CodeExecutionResult) Validate() error { return r.ReturnCode.Validate() }
func (r CodeExecutionResult) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return marshalMetadataObject(struct {
		Type       string                `json:"type"`
		Outputs    []CodeExecutionOutput `json:"content"`
		ReturnCode ExecutionReturnCode   `json:"return_code"`
		Stdout     string                `json:"stdout"`
		Stderr     string                `json:"stderr"`
	}{"code_execution_result", r.clone().OutputFiles, r.ReturnCode, r.Stdout, r.Stderr}, r.extra)
}
func decodeCodeExecutionResult(data []byte, bounded bool) (CodeExecutionResult, error) {
	var wire struct {
		Type           *string
		Outputs        *[]json.RawMessage   `json:"content"`
		Code           *ExecutionReturnCode `json:"return_code"`
		Stdout, Stderr *string
	}
	_, extra, err := decodeMetadataObject(data, &wire, []string{"type", "content", "return_code", "stdout", "stderr"}, bounded)
	if err != nil {
		return CodeExecutionResult{}, err
	}
	if wire.Type == nil || *wire.Type != "code_execution_result" || wire.Code == nil || wire.Stdout == nil || wire.Stderr == nil {
		return CodeExecutionResult{}, errors.New("code execution result requires type, return_code, stdout and stderr")
	}
	files, err := decodeCodeExecutionOutputs(wire.Outputs, bounded)
	if err != nil {
		return CodeExecutionResult{}, err
	}
	return CodeExecutionResult{OutputFiles: files, ReturnCode: *wire.Code, Stdout: *wire.Stdout, Stderr: *wire.Stderr, extra: extra}, nil
}
func (r *CodeExecutionResult) UnmarshalJSON(data []byte) error {
	next, err := decodeCodeExecutionResult(data, true)
	if err == nil {
		*r = next
	}
	return err
}

type EncryptedCodeExecutionResult struct {
	OutputFiles             []CodeExecutionOutput
	ReturnCode              ExecutionReturnCode
	EncryptedStdout, Stderr string
	extra                   jsonvalue.Value
}

func (r EncryptedCodeExecutionResult) clone() EncryptedCodeExecutionResult {
	r.OutputFiles = append([]CodeExecutionOutput{}, r.OutputFiles...)
	return r
}
func (r EncryptedCodeExecutionResult) Validate() error { return r.ReturnCode.Validate() }
func (r EncryptedCodeExecutionResult) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return marshalMetadataObject(struct {
		Type       string                `json:"type"`
		Outputs    []CodeExecutionOutput `json:"content"`
		ReturnCode ExecutionReturnCode   `json:"return_code"`
		Stdout     string                `json:"encrypted_stdout"`
		Stderr     string                `json:"stderr"`
	}{"encrypted_code_execution_result", r.clone().OutputFiles, r.ReturnCode, r.EncryptedStdout, r.Stderr}, r.extra)
}
func decodeEncryptedCodeExecutionResult(data []byte, bounded bool) (EncryptedCodeExecutionResult, error) {
	var wire struct {
		Type    *string
		Outputs *[]json.RawMessage   `json:"content"`
		Code    *ExecutionReturnCode `json:"return_code"`
		Stdout  *string              `json:"encrypted_stdout"`
		Stderr  *string
	}
	_, extra, err := decodeMetadataObject(data, &wire, []string{"type", "content", "return_code", "encrypted_stdout", "stderr"}, bounded)
	if err != nil {
		return EncryptedCodeExecutionResult{}, err
	}
	if wire.Type == nil || *wire.Type != "encrypted_code_execution_result" || wire.Code == nil || wire.Stdout == nil || wire.Stderr == nil {
		return EncryptedCodeExecutionResult{}, errors.New("encrypted execution result requires type, return_code, encrypted_stdout and stderr")
	}
	files, err := decodeCodeExecutionOutputs(wire.Outputs, bounded)
	if err != nil {
		return EncryptedCodeExecutionResult{}, err
	}
	return EncryptedCodeExecutionResult{OutputFiles: files, ReturnCode: *wire.Code, EncryptedStdout: *wire.Stdout, Stderr: *wire.Stderr, extra: extra}, nil
}
func (r *EncryptedCodeExecutionResult) UnmarshalJSON(data []byte) error {
	next, err := decodeEncryptedCodeExecutionResult(data, true)
	if err == nil {
		*r = next
	}
	return err
}

type CodeExecutionErrorCode string

const (
	CodeExecutionInvalidInput    CodeExecutionErrorCode = "invalid_tool_input"
	CodeExecutionUnavailable     CodeExecutionErrorCode = "unavailable"
	CodeExecutionTooManyRequests CodeExecutionErrorCode = "too_many_requests"
	CodeExecutionTimeExceeded    CodeExecutionErrorCode = "execution_time_exceeded"
)

type CodeExecutionError struct {
	Code  CodeExecutionErrorCode
	extra jsonvalue.Value
}

func (e CodeExecutionError) MarshalJSON() ([]byte, error) {
	return marshalMetadataObject(struct {
		Type string                 `json:"type"`
		Code CodeExecutionErrorCode `json:"error_code"`
	}{"code_execution_tool_result_error", e.Code}, e.extra)
}
func decodeCodeExecutionError(data []byte, bounded bool) (CodeExecutionError, error) {
	var wire struct {
		Type *string
		Code *CodeExecutionErrorCode `json:"error_code"`
	}
	_, extra, err := decodeMetadataObject(data, &wire, []string{"type", "error_code"}, bounded)
	if err != nil {
		return CodeExecutionError{}, err
	}
	if wire.Type == nil || *wire.Type != "code_execution_tool_result_error" || wire.Code == nil {
		return CodeExecutionError{}, errors.New("code execution error requires type and error_code")
	}
	return CodeExecutionError{Code: *wire.Code, extra: extra}, nil
}
func (e *CodeExecutionError) UnmarshalJSON(data []byte) error {
	next, err := decodeCodeExecutionError(data, true)
	if err == nil {
		*e = next
	}
	return err
}

type CodeExecutionContentKind string

const (
	CodeExecutionPlain     CodeExecutionContentKind = "plain"
	CodeExecutionEncrypted CodeExecutionContentKind = "encrypted"
	CodeExecutionFailure   CodeExecutionContentKind = "error"
)

// CodeExecutionContent is the closed ordinary/encrypted/error result union.
type CodeExecutionContent struct {
	kind      CodeExecutionContentKind
	plain     *CodeExecutionResult
	encrypted *EncryptedCodeExecutionResult
	failure   *CodeExecutionError
}

func NewCodeExecutionPlain(result CodeExecutionResult) CodeExecutionContent {
	r := result.clone()
	return CodeExecutionContent{kind: CodeExecutionPlain, plain: &r}
}
func NewCodeExecutionEncrypted(result EncryptedCodeExecutionResult) CodeExecutionContent {
	r := result.clone()
	return CodeExecutionContent{kind: CodeExecutionEncrypted, encrypted: &r}
}
func NewCodeExecutionFailure(code CodeExecutionErrorCode) CodeExecutionContent {
	return CodeExecutionContent{kind: CodeExecutionFailure, failure: &CodeExecutionError{Code: code}}
}
func (c CodeExecutionContent) Kind() CodeExecutionContentKind { return c.kind }
func (c CodeExecutionContent) Plain() (CodeExecutionResult, bool) {
	if c.kind != CodeExecutionPlain || c.plain == nil {
		return CodeExecutionResult{}, false
	}
	return c.plain.clone(), true
}
func (c CodeExecutionContent) Encrypted() (EncryptedCodeExecutionResult, bool) {
	if c.kind != CodeExecutionEncrypted || c.encrypted == nil {
		return EncryptedCodeExecutionResult{}, false
	}
	return c.encrypted.clone(), true
}
func (c CodeExecutionContent) Error() (CodeExecutionError, bool) {
	if c.kind != CodeExecutionFailure || c.failure == nil {
		return CodeExecutionError{}, false
	}
	return *c.failure, true
}
func (c CodeExecutionContent) clone() CodeExecutionContent {
	if c.plain != nil {
		r := c.plain.clone()
		c.plain = &r
	}
	if c.encrypted != nil {
		r := c.encrypted.clone()
		c.encrypted = &r
	}
	c.failure = copyMetadataPointer(c.failure)
	return c
}
func (c CodeExecutionContent) Validate() error {
	if c.kind == CodeExecutionPlain && c.plain != nil && c.encrypted == nil && c.failure == nil {
		return c.plain.Validate()
	}
	if c.kind == CodeExecutionEncrypted && c.encrypted != nil && c.plain == nil && c.failure == nil {
		return c.encrypted.Validate()
	}
	if c.kind == CodeExecutionFailure && c.failure != nil && c.plain == nil && c.encrypted == nil {
		return nil
	}
	return errors.New("code execution content requires one plain/encrypted/error variant")
}
func (c CodeExecutionContent) MarshalJSON() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	switch c.kind {
	case CodeExecutionPlain:
		return json.Marshal(c.plain)
	case CodeExecutionEncrypted:
		return json.Marshal(c.encrypted)
	default:
		return json.Marshal(c.failure)
	}
}
func decodeCodeExecutionContent(data []byte, bounded bool) (CodeExecutionContent, error) {
	var wire struct{ Type *string }
	if _, _, err := decodeMetadataObject(data, &wire, []string{"type"}, bounded); err != nil {
		return CodeExecutionContent{}, err
	}
	if wire.Type == nil {
		return CodeExecutionContent{}, errors.New("code execution content requires type")
	}
	switch *wire.Type {
	case "code_execution_result":
		r, err := decodeCodeExecutionResult(data, bounded)
		if err != nil {
			return CodeExecutionContent{}, err
		}
		return CodeExecutionContent{kind: CodeExecutionPlain, plain: &r}, nil
	case "encrypted_code_execution_result":
		r, err := decodeEncryptedCodeExecutionResult(data, bounded)
		if err != nil {
			return CodeExecutionContent{}, err
		}
		return CodeExecutionContent{kind: CodeExecutionEncrypted, encrypted: &r}, nil
	case "code_execution_tool_result_error":
		e, err := decodeCodeExecutionError(data, bounded)
		if err != nil {
			return CodeExecutionContent{}, err
		}
		return CodeExecutionContent{kind: CodeExecutionFailure, failure: &e}, nil
	default:
		return CodeExecutionContent{}, errors.New("unsupported code execution content type")
	}
}
func (c *CodeExecutionContent) UnmarshalJSON(data []byte) error {
	next, err := decodeCodeExecutionContent(data, true)
	if err == nil {
		*c = next
	}
	return err
}

type CodeExecutionToolResultBlock struct {
	ToolUseID                                 string
	Content                                   CodeExecutionContent
	textNull, citationsNull, parsedOutputNull bool
	cache                                     *CacheControl
	extra                                     jsonvalue.Value
}

func (r CodeExecutionToolResultBlock) clone() CodeExecutionToolResultBlock {
	r.Content = r.Content.clone()
	r.cache = copyMetadataPointer(r.cache)
	return r
}
func (r CodeExecutionToolResultBlock) Validate() error {
	if err := r.Content.Validate(); err != nil {
		return err
	}
	if r.cache != nil {
		return r.cache.Validate()
	}
	return nil
}
func (r CodeExecutionToolResultBlock) WithSDKDefaults(streamed bool) CodeExecutionToolResultBlock {
	r = r.clone()
	if streamed {
		r.textNull, r.citationsNull, r.parsedOutputNull = true, true, true
	}
	return r
}
func (r CodeExecutionToolResultBlock) marshal(control *CacheControl) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if control == nil {
		control = r.cache
	}
	return marshalMetadataObject(struct {
		Type         BlockKind                `json:"type"`
		ToolUseID    string                   `json:"tool_use_id"`
		Content      CodeExecutionContent     `json:"content"`
		Text         *wireOptional[string]    `json:"text,omitempty"`
		Citations    *wireOptional[Citations] `json:"citations,omitempty"`
		ParsedOutput *wireOptional[string]    `json:"parsed_output,omitempty"`
		Cache        *CacheControl            `json:"cache_control,omitempty"`
	}{BlockCodeExecutionToolResult, r.ToolUseID, r.Content, optionalWireValue[string](nil, r.textNull), optionalWireValue[Citations](nil, r.citationsNull), optionalWireValue[string](nil, r.parsedOutputNull), control}, r.extra)
}
func (r CodeExecutionToolResultBlock) MarshalJSON() ([]byte, error) { return r.marshal(nil) }
func decodeCodeExecutionBlock(data []byte, bounded bool) (CodeExecutionToolResultBlock, error) {
	var wire struct {
		Type            *BlockKind
		ToolUseID       *string `json:"tool_use_id"`
		Content         json.RawMessage
		Text, Citations json.RawMessage
		ParsedOutput    json.RawMessage `json:"parsed_output"`
		Cache           *CacheControl   `json:"cache_control"`
	}
	_, extra, err := decodeMetadataObject(data, &wire, []string{"type", "tool_use_id", "content", "text", "citations", "parsed_output", "cache_control"}, bounded)
	if err != nil {
		return CodeExecutionToolResultBlock{}, err
	}
	if wire.Type == nil || *wire.Type != BlockCodeExecutionToolResult || wire.ToolUseID == nil || len(wire.Content) == 0 {
		return CodeExecutionToolResultBlock{}, errors.New("code execution result requires type, tool_use_id and content")
	}
	content, err := decodeCodeExecutionContent(wire.Content, bounded)
	if err != nil {
		return CodeExecutionToolResultBlock{}, err
	}
	for _, raw := range []json.RawMessage{wire.Text, wire.Citations, wire.ParsedOutput} {
		if len(raw) > 0 && string(raw) != "null" {
			return CodeExecutionToolResultBlock{}, errors.New("code execution stream metadata must be null")
		}
	}
	next := CodeExecutionToolResultBlock{ToolUseID: *wire.ToolUseID, Content: content, textNull: len(wire.Text) > 0, citationsNull: len(wire.Citations) > 0, parsedOutputNull: len(wire.ParsedOutput) > 0, cache: wire.Cache, extra: extra}
	return next, next.Validate()
}
func (r *CodeExecutionToolResultBlock) UnmarshalJSON(data []byte) error {
	next, err := decodeCodeExecutionBlock(data, true)
	if err == nil {
		*r = next
	}
	return err
}
func (r CodeExecutionToolResultBlock) Block() Block {
	result := r.clone()
	return Block{kind: BlockCodeExecutionToolResult, codeExecution: &result}
}
func NewCodeExecutionToolResultBlock(toolUseID string, content CodeExecutionContent) Block {
	return (CodeExecutionToolResultBlock{ToolUseID: toolUseID, Content: content}).Block()
}
func (b Block) CodeExecutionToolResult() (CodeExecutionToolResultBlock, bool) {
	if b.kind != BlockCodeExecutionToolResult || b.codeExecution == nil {
		return CodeExecutionToolResultBlock{}, false
	}
	return b.codeExecution.clone(), true
}
