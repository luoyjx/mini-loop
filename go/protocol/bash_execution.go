package protocol

import (
	"encoding/json"
	"errors"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type BashExecutionOutput struct {
	FileID string
	extra  jsonvalue.Value
}

func (f BashExecutionOutput) MarshalJSON() ([]byte, error) {
	return marshalMetadataObject(struct {
		Type   string `json:"type"`
		FileID string `json:"file_id"`
	}{"bash_code_execution_output", f.FileID}, f.extra)
}
func decodeBashExecutionOutput(data []byte, bounded bool) (BashExecutionOutput, error) {
	var wire struct {
		Type   *string
		FileID *string `json:"file_id"`
	}
	_, extra, err := decodeMetadataObject(data, &wire, []string{"type", "file_id"}, bounded)
	if err != nil {
		return BashExecutionOutput{}, err
	}
	if wire.Type == nil || *wire.Type != "bash_code_execution_output" || wire.FileID == nil {
		return BashExecutionOutput{}, errors.New("bash execution output requires type and file_id")
	}
	return BashExecutionOutput{FileID: *wire.FileID, extra: extra}, nil
}
func (f *BashExecutionOutput) UnmarshalJSON(data []byte) error {
	next, err := decodeBashExecutionOutput(data, true)
	if err == nil {
		*f = next
	}
	return err
}
func decodeBashExecutionOutputs(rows *[]json.RawMessage, bounded bool) ([]BashExecutionOutput, error) {
	if rows == nil {
		return nil, errors.New("bash execution result requires output array")
	}
	result := []BashExecutionOutput{}
	for _, data := range *rows {
		output, err := decodeBashExecutionOutput(data, bounded)
		if err != nil {
			return nil, err
		}
		result = append(result, output)
	}
	return result, nil
}

type BashExecutionResult struct {
	OutputFiles    []BashExecutionOutput
	ReturnCode     ExecutionReturnCode
	Stdout, Stderr string
	extra          jsonvalue.Value
}

func (r BashExecutionResult) clone() BashExecutionResult {
	r.OutputFiles = append([]BashExecutionOutput{}, r.OutputFiles...)
	return r
}
func (r BashExecutionResult) Validate() error { return r.ReturnCode.Validate() }
func (r BashExecutionResult) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return marshalMetadataObject(struct {
		Type       string                `json:"type"`
		Outputs    []BashExecutionOutput `json:"content"`
		ReturnCode ExecutionReturnCode   `json:"return_code"`
		Stdout     string                `json:"stdout"`
		Stderr     string                `json:"stderr"`
	}{"bash_code_execution_result", r.clone().OutputFiles, r.ReturnCode, r.Stdout, r.Stderr}, r.extra)
}
func decodeBashExecutionResult(data []byte, bounded bool) (BashExecutionResult, error) {
	var wire struct {
		Type           *string
		Outputs        *[]json.RawMessage   `json:"content"`
		Code           *ExecutionReturnCode `json:"return_code"`
		Stdout, Stderr *string
	}
	_, extra, err := decodeMetadataObject(data, &wire, []string{"type", "content", "return_code", "stdout", "stderr"}, bounded)
	if err != nil {
		return BashExecutionResult{}, err
	}
	if wire.Type == nil || *wire.Type != "bash_code_execution_result" || wire.Code == nil || wire.Stdout == nil || wire.Stderr == nil {
		return BashExecutionResult{}, errors.New("bash execution result requires type, return_code, stdout and stderr")
	}
	files, err := decodeBashExecutionOutputs(wire.Outputs, bounded)
	if err != nil {
		return BashExecutionResult{}, err
	}
	return BashExecutionResult{OutputFiles: files, ReturnCode: *wire.Code, Stdout: *wire.Stdout, Stderr: *wire.Stderr, extra: extra}, nil
}
func (r *BashExecutionResult) UnmarshalJSON(data []byte) error {
	next, err := decodeBashExecutionResult(data, true)
	if err == nil {
		*r = next
	}
	return err
}

type BashExecutionErrorCode string

const (
	BashExecutionInvalidInput       BashExecutionErrorCode = "invalid_tool_input"
	BashExecutionUnavailable        BashExecutionErrorCode = "unavailable"
	BashExecutionTooManyRequests    BashExecutionErrorCode = "too_many_requests"
	BashExecutionTimeExceeded       BashExecutionErrorCode = "execution_time_exceeded"
	BashExecutionOutputFileTooLarge BashExecutionErrorCode = "output_file_too_large"
)

type BashExecutionError struct {
	Code  BashExecutionErrorCode
	extra jsonvalue.Value
}

func (e BashExecutionError) MarshalJSON() ([]byte, error) {
	return marshalMetadataObject(struct {
		Type string                 `json:"type"`
		Code BashExecutionErrorCode `json:"error_code"`
	}{"bash_code_execution_tool_result_error", e.Code}, e.extra)
}
func decodeBashExecutionError(data []byte, bounded bool) (BashExecutionError, error) {
	var wire struct {
		Type *string
		Code *BashExecutionErrorCode `json:"error_code"`
	}
	_, extra, err := decodeMetadataObject(data, &wire, []string{"type", "error_code"}, bounded)
	if err != nil {
		return BashExecutionError{}, err
	}
	if wire.Type == nil || *wire.Type != "bash_code_execution_tool_result_error" || wire.Code == nil {
		return BashExecutionError{}, errors.New("bash execution error requires type and error_code")
	}
	return BashExecutionError{Code: *wire.Code, extra: extra}, nil
}
func (e *BashExecutionError) UnmarshalJSON(data []byte) error {
	next, err := decodeBashExecutionError(data, true)
	if err == nil {
		*e = next
	}
	return err
}

type BashExecutionContentKind string

const (
	BashExecutionSuccess BashExecutionContentKind = "result"
	BashExecutionFailure BashExecutionContentKind = "error"
)

// BashExecutionContent is provider-owned result/error metadata, not BashInput.
type BashExecutionContent struct {
	kind    BashExecutionContentKind
	result  *BashExecutionResult
	failure *BashExecutionError
}

func NewBashExecutionSuccess(result BashExecutionResult) BashExecutionContent {
	r := result.clone()
	return BashExecutionContent{kind: BashExecutionSuccess, result: &r}
}
func NewBashExecutionFailure(code BashExecutionErrorCode) BashExecutionContent {
	return BashExecutionContent{kind: BashExecutionFailure, failure: &BashExecutionError{Code: code}}
}
func (c BashExecutionContent) Kind() BashExecutionContentKind { return c.kind }
func (c BashExecutionContent) Result() (BashExecutionResult, bool) {
	if c.kind != BashExecutionSuccess || c.result == nil {
		return BashExecutionResult{}, false
	}
	return c.result.clone(), true
}
func (c BashExecutionContent) Error() (BashExecutionError, bool) {
	if c.kind != BashExecutionFailure || c.failure == nil {
		return BashExecutionError{}, false
	}
	return *c.failure, true
}
func (c BashExecutionContent) clone() BashExecutionContent {
	if c.result != nil {
		r := c.result.clone()
		c.result = &r
	}
	c.failure = copyMetadataPointer(c.failure)
	return c
}
func (c BashExecutionContent) Validate() error {
	if c.kind == BashExecutionSuccess && c.result != nil && c.failure == nil {
		return c.result.Validate()
	}
	if c.kind == BashExecutionFailure && c.failure != nil && c.result == nil {
		return nil
	}
	return errors.New("bash execution content requires one result/error variant")
}
func (c BashExecutionContent) MarshalJSON() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if c.kind == BashExecutionSuccess {
		return json.Marshal(c.result)
	}
	return json.Marshal(c.failure)
}
func decodeBashExecutionContent(data []byte, bounded bool) (BashExecutionContent, error) {
	var wire struct{ Type *string }
	if _, _, err := decodeMetadataObject(data, &wire, []string{"type"}, bounded); err != nil {
		return BashExecutionContent{}, err
	}
	if wire.Type == nil {
		return BashExecutionContent{}, errors.New("bash execution content requires type")
	}
	switch *wire.Type {
	case "bash_code_execution_result":
		r, err := decodeBashExecutionResult(data, bounded)
		if err != nil {
			return BashExecutionContent{}, err
		}
		return BashExecutionContent{kind: BashExecutionSuccess, result: &r}, nil
	case "bash_code_execution_tool_result_error":
		e, err := decodeBashExecutionError(data, bounded)
		if err != nil {
			return BashExecutionContent{}, err
		}
		return BashExecutionContent{kind: BashExecutionFailure, failure: &e}, nil
	default:
		return BashExecutionContent{}, errors.New("unsupported bash execution content type")
	}
}
func (c *BashExecutionContent) UnmarshalJSON(data []byte) error {
	next, err := decodeBashExecutionContent(data, true)
	if err == nil {
		*c = next
	}
	return err
}

type BashExecutionToolResultBlock struct {
	ToolUseID                                 string
	Content                                   BashExecutionContent
	textNull, citationsNull, parsedOutputNull bool
	cache                                     *CacheControl
	extra                                     jsonvalue.Value
}

func (r BashExecutionToolResultBlock) clone() BashExecutionToolResultBlock {
	r.Content = r.Content.clone()
	r.cache = copyMetadataPointer(r.cache)
	return r
}
func (r BashExecutionToolResultBlock) Validate() error {
	if err := r.Content.Validate(); err != nil {
		return err
	}
	if r.cache != nil {
		return r.cache.Validate()
	}
	return nil
}
func (r BashExecutionToolResultBlock) WithSDKDefaults(streamed bool) BashExecutionToolResultBlock {
	r = r.clone()
	if streamed {
		r.textNull, r.citationsNull, r.parsedOutputNull = true, true, true
	}
	return r
}
func (r BashExecutionToolResultBlock) marshal(control *CacheControl) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if control == nil {
		control = r.cache
	}
	return marshalMetadataObject(struct {
		Type         BlockKind                `json:"type"`
		ToolUseID    string                   `json:"tool_use_id"`
		Content      BashExecutionContent     `json:"content"`
		Text         *wireOptional[string]    `json:"text,omitempty"`
		Citations    *wireOptional[Citations] `json:"citations,omitempty"`
		ParsedOutput *wireOptional[string]    `json:"parsed_output,omitempty"`
		Cache        *CacheControl            `json:"cache_control,omitempty"`
	}{BlockBashExecutionToolResult, r.ToolUseID, r.Content, optionalWireValue[string](nil, r.textNull), optionalWireValue[Citations](nil, r.citationsNull), optionalWireValue[string](nil, r.parsedOutputNull), control}, r.extra)
}
func (r BashExecutionToolResultBlock) MarshalJSON() ([]byte, error) { return r.marshal(nil) }
func decodeBashExecutionBlock(data []byte, bounded bool) (BashExecutionToolResultBlock, error) {
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
		return BashExecutionToolResultBlock{}, err
	}
	if wire.Type == nil || *wire.Type != BlockBashExecutionToolResult || wire.ToolUseID == nil || len(wire.Content) == 0 {
		return BashExecutionToolResultBlock{}, errors.New("bash execution result requires type, tool_use_id and content")
	}
	content, err := decodeBashExecutionContent(wire.Content, bounded)
	if err != nil {
		return BashExecutionToolResultBlock{}, err
	}
	for _, raw := range []json.RawMessage{wire.Text, wire.Citations, wire.ParsedOutput} {
		if len(raw) > 0 && string(raw) != "null" {
			return BashExecutionToolResultBlock{}, errors.New("bash execution stream metadata must be null")
		}
	}
	next := BashExecutionToolResultBlock{ToolUseID: *wire.ToolUseID, Content: content, textNull: len(wire.Text) > 0, citationsNull: len(wire.Citations) > 0, parsedOutputNull: len(wire.ParsedOutput) > 0, cache: wire.Cache, extra: extra}
	return next, next.Validate()
}
func (r *BashExecutionToolResultBlock) UnmarshalJSON(data []byte) error {
	next, err := decodeBashExecutionBlock(data, true)
	if err == nil {
		*r = next
	}
	return err
}
func (r BashExecutionToolResultBlock) Block() Block {
	result := r.clone()
	return Block{kind: BlockBashExecutionToolResult, bashExecution: &result}
}
func NewBashExecutionToolResultBlock(toolUseID string, content BashExecutionContent) Block {
	return (BashExecutionToolResultBlock{ToolUseID: toolUseID, Content: content}).Block()
}
func (b Block) BashExecutionToolResult() (BashExecutionToolResultBlock, bool) {
	if b.kind != BlockBashExecutionToolResult || b.bashExecution == nil {
		return BashExecutionToolResultBlock{}, false
	}
	return b.bashExecution.clone(), true
}
