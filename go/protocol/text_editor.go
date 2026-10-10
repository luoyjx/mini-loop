package protocol

import (
	"encoding/json"
	"errors"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// EditorInteger is an SDK Python integer for observational line counts/positions.
// Its representation preserves values above machine width without string coercion.
type EditorInteger struct{ value jsonvalue.Value }

func NewEditorInteger(value int64) EditorInteger {
	return EditorInteger{value: jsonvalue.IntegerValue(value)}
}
func (n EditorInteger) Decimal() string { s, _ := n.value.Integer(); return s }
func (n EditorInteger) Validate() error {
	if n.value.Kind() != jsonvalue.Integer {
		return errors.New("editor line metadata must be an integer")
	}
	return nil
}
func (n EditorInteger) MarshalJSON() ([]byte, error) {
	if err := n.Validate(); err != nil {
		return nil, err
	}
	return n.value.MarshalJSON()
}
func (n *EditorInteger) UnmarshalJSON(data []byte) error {
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return err
	}
	next := EditorInteger{value: value}
	if err := next.Validate(); err != nil {
		return err
	}
	*n = next
	return nil
}
func editorFieldNull(value jsonvalue.Value, name string) bool {
	v, present := value.Lookup(name)
	return present && v.Kind() == jsonvalue.Null
}
func validateEditorNumbers(values ...*EditorInteger) error {
	for _, n := range values {
		if n != nil {
			if err := n.Validate(); err != nil {
				return err
			}
		}
	}
	return nil
}

type EditorFileType string

const (
	EditorText  EditorFileType = "text"
	EditorImage EditorFileType = "image"
	EditorPDF   EditorFileType = "pdf"
)

type TextEditorViewResult struct {
	Content                                     string
	FileType                                    EditorFileType
	NumLines, StartLine, TotalLines             *EditorInteger
	numLinesNull, startLineNull, totalLinesNull bool
	extra                                       jsonvalue.Value
}

func (r TextEditorViewResult) clone() TextEditorViewResult {
	r.NumLines = copyMetadataPointer(r.NumLines)
	r.StartLine = copyMetadataPointer(r.StartLine)
	r.TotalLines = copyMetadataPointer(r.TotalLines)
	return r
}
func (r TextEditorViewResult) Validate() error {
	return validateEditorNumbers(r.NumLines, r.StartLine, r.TotalLines)
}
func (r TextEditorViewResult) WithSDKDefaults() TextEditorViewResult {
	r = r.clone()
	r.numLinesNull = r.NumLines == nil
	r.startLineNull = r.StartLine == nil
	r.totalLinesNull = r.TotalLines == nil
	return r
}
func (r TextEditorViewResult) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return marshalMetadataObject(struct {
		Type       string                       `json:"type"`
		Content    string                       `json:"content"`
		FileType   EditorFileType               `json:"file_type"`
		NumLines   *wireOptional[EditorInteger] `json:"num_lines,omitempty"`
		StartLine  *wireOptional[EditorInteger] `json:"start_line,omitempty"`
		TotalLines *wireOptional[EditorInteger] `json:"total_lines,omitempty"`
	}{"text_editor_code_execution_view_result", r.Content, r.FileType, optionalWireValue(r.NumLines, r.numLinesNull), optionalWireValue(r.StartLine, r.startLineNull), optionalWireValue(r.TotalLines, r.totalLinesNull)}, r.extra)
}
func decodeTextEditorView(data []byte, bounded bool) (TextEditorViewResult, error) {
	var wire struct {
		Type       *string
		Content    *string
		FileType   *EditorFileType `json:"file_type"`
		NumLines   *EditorInteger  `json:"num_lines"`
		StartLine  *EditorInteger  `json:"start_line"`
		TotalLines *EditorInteger  `json:"total_lines"`
	}
	value, extra, err := decodeMetadataObject(data, &wire, []string{"type", "content", "file_type", "num_lines", "start_line", "total_lines"}, bounded)
	if err != nil {
		return TextEditorViewResult{}, err
	}
	if wire.Type == nil || *wire.Type != "text_editor_code_execution_view_result" || wire.Content == nil || wire.FileType == nil {
		return TextEditorViewResult{}, errors.New("editor view requires type, content and file_type")
	}
	return TextEditorViewResult{Content: *wire.Content, FileType: *wire.FileType, NumLines: wire.NumLines, StartLine: wire.StartLine, TotalLines: wire.TotalLines, numLinesNull: editorFieldNull(value, "num_lines"), startLineNull: editorFieldNull(value, "start_line"), totalLinesNull: editorFieldNull(value, "total_lines"), extra: extra}, nil
}
func (r *TextEditorViewResult) UnmarshalJSON(data []byte) error {
	next, err := decodeTextEditorView(data, true)
	if err == nil {
		*r = next
	}
	return err
}

type TextEditorCreateResult struct {
	IsFileUpdate bool
	extra        jsonvalue.Value
}

func (r TextEditorCreateResult) MarshalJSON() ([]byte, error) {
	return marshalMetadataObject(struct {
		Type    string `json:"type"`
		Updated bool   `json:"is_file_update"`
	}{"text_editor_code_execution_create_result", r.IsFileUpdate}, r.extra)
}
func decodeTextEditorCreate(data []byte, bounded bool) (TextEditorCreateResult, error) {
	var wire struct {
		Type    *string
		Updated *bool `json:"is_file_update"`
	}
	_, extra, err := decodeMetadataObject(data, &wire, []string{"type", "is_file_update"}, bounded)
	if err != nil {
		return TextEditorCreateResult{}, err
	}
	if wire.Type == nil || *wire.Type != "text_editor_code_execution_create_result" || wire.Updated == nil {
		return TextEditorCreateResult{}, errors.New("editor create requires type and is_file_update")
	}
	return TextEditorCreateResult{IsFileUpdate: *wire.Updated, extra: extra}, nil
}
func (r *TextEditorCreateResult) UnmarshalJSON(data []byte) error {
	next, err := decodeTextEditorCreate(data, true)
	if err == nil {
		*r = next
	}
	return err
}

type TextEditorReplaceResult struct {
	Lines                                                             *[]string
	NewLines, NewStart, OldLines, OldStart                            *EditorInteger
	linesNull, newLinesNull, newStartNull, oldLinesNull, oldStartNull bool
	extra                                                             jsonvalue.Value
}

func (r TextEditorReplaceResult) clone() TextEditorReplaceResult {
	if r.Lines != nil {
		lines := append([]string{}, (*r.Lines)...)
		r.Lines = &lines
	}
	r.NewLines = copyMetadataPointer(r.NewLines)
	r.NewStart = copyMetadataPointer(r.NewStart)
	r.OldLines = copyMetadataPointer(r.OldLines)
	r.OldStart = copyMetadataPointer(r.OldStart)
	return r
}
func (r TextEditorReplaceResult) Validate() error {
	return validateEditorNumbers(r.NewLines, r.NewStart, r.OldLines, r.OldStart)
}
func (r TextEditorReplaceResult) WithSDKDefaults() TextEditorReplaceResult {
	r = r.clone()
	r.linesNull = r.Lines == nil
	r.newLinesNull = r.NewLines == nil
	r.newStartNull = r.NewStart == nil
	r.oldLinesNull = r.OldLines == nil
	r.oldStartNull = r.OldStart == nil
	return r
}
func (r TextEditorReplaceResult) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	r = r.clone()
	return marshalMetadataObject(struct {
		Type     string                       `json:"type"`
		Lines    *wireOptional[[]string]      `json:"lines,omitempty"`
		NewLines *wireOptional[EditorInteger] `json:"new_lines,omitempty"`
		NewStart *wireOptional[EditorInteger] `json:"new_start,omitempty"`
		OldLines *wireOptional[EditorInteger] `json:"old_lines,omitempty"`
		OldStart *wireOptional[EditorInteger] `json:"old_start,omitempty"`
	}{"text_editor_code_execution_str_replace_result", optionalWireValue(r.Lines, r.linesNull), optionalWireValue(r.NewLines, r.newLinesNull), optionalWireValue(r.NewStart, r.newStartNull), optionalWireValue(r.OldLines, r.oldLinesNull), optionalWireValue(r.OldStart, r.oldStartNull)}, r.extra)
}
func decodeTextEditorReplace(data []byte, bounded bool) (TextEditorReplaceResult, error) {
	var wire struct {
		Type     *string
		Lines    *[]string
		NewLines *EditorInteger `json:"new_lines"`
		NewStart *EditorInteger `json:"new_start"`
		OldLines *EditorInteger `json:"old_lines"`
		OldStart *EditorInteger `json:"old_start"`
	}
	value, extra, err := decodeMetadataObject(data, &wire, []string{"type", "lines", "new_lines", "new_start", "old_lines", "old_start"}, bounded)
	if err != nil {
		return TextEditorReplaceResult{}, err
	}
	if wire.Type == nil || *wire.Type != "text_editor_code_execution_str_replace_result" {
		return TextEditorReplaceResult{}, errors.New("editor replace requires type")
	}
	if lines, present := value.Lookup("lines"); present && lines.Kind() != jsonvalue.Null {
		elements, ok := lines.Array()
		if !ok {
			return TextEditorReplaceResult{}, errors.New("editor lines must be an array")
		}
		for _, line := range elements {
			if line.Kind() != jsonvalue.Text {
				return TextEditorReplaceResult{}, errors.New("editor lines must contain strings")
			}
		}
	}
	return TextEditorReplaceResult{Lines: wire.Lines, NewLines: wire.NewLines, NewStart: wire.NewStart, OldLines: wire.OldLines, OldStart: wire.OldStart, linesNull: editorFieldNull(value, "lines"), newLinesNull: editorFieldNull(value, "new_lines"), newStartNull: editorFieldNull(value, "new_start"), oldLinesNull: editorFieldNull(value, "old_lines"), oldStartNull: editorFieldNull(value, "old_start"), extra: extra}, nil
}
func (r *TextEditorReplaceResult) UnmarshalJSON(data []byte) error {
	next, err := decodeTextEditorReplace(data, true)
	if err == nil {
		*r = next
	}
	return err
}

type TextEditorErrorCode string

const (
	TextEditorInvalidInput    TextEditorErrorCode = "invalid_tool_input"
	TextEditorUnavailable     TextEditorErrorCode = "unavailable"
	TextEditorTooManyRequests TextEditorErrorCode = "too_many_requests"
	TextEditorTimeExceeded    TextEditorErrorCode = "execution_time_exceeded"
	TextEditorFileNotFound    TextEditorErrorCode = "file_not_found"
)

type TextEditorError struct {
	Code        TextEditorErrorCode
	Message     *string
	messageNull bool
	extra       jsonvalue.Value
}

func (r TextEditorError) clone() TextEditorError {
	r.Message = copyMetadataPointer(r.Message)
	return r
}
func (r TextEditorError) WithSDKDefaults() TextEditorError {
	r = r.clone()
	r.messageNull = r.Message == nil
	return r
}
func (r TextEditorError) MarshalJSON() ([]byte, error) {
	return marshalMetadataObject(struct {
		Type    string                `json:"type"`
		Code    TextEditorErrorCode   `json:"error_code"`
		Message *wireOptional[string] `json:"error_message,omitempty"`
	}{"text_editor_code_execution_tool_result_error", r.Code, optionalWireValue(r.Message, r.messageNull)}, r.extra)
}
func decodeTextEditorError(data []byte, bounded bool) (TextEditorError, error) {
	var wire struct {
		Type    *string
		Code    *TextEditorErrorCode `json:"error_code"`
		Message *string              `json:"error_message"`
	}
	value, extra, err := decodeMetadataObject(data, &wire, []string{"type", "error_code", "error_message"}, bounded)
	if err != nil {
		return TextEditorError{}, err
	}
	if wire.Type == nil || *wire.Type != "text_editor_code_execution_tool_result_error" || wire.Code == nil {
		return TextEditorError{}, errors.New("editor error requires type and error_code")
	}
	return TextEditorError{Code: *wire.Code, Message: wire.Message, messageNull: editorFieldNull(value, "error_message"), extra: extra}, nil
}
func (r *TextEditorError) UnmarshalJSON(data []byte) error {
	next, err := decodeTextEditorError(data, true)
	if err == nil {
		*r = next
	}
	return err
}

type TextEditorContentKind string

const (
	TextEditorView    TextEditorContentKind = "view"
	TextEditorCreate  TextEditorContentKind = "create"
	TextEditorReplace TextEditorContentKind = "replace"
	TextEditorFailure TextEditorContentKind = "error"
)

type TextEditorContent struct {
	kind    TextEditorContentKind
	view    *TextEditorViewResult
	create  *TextEditorCreateResult
	replace *TextEditorReplaceResult
	failure *TextEditorError
}

func NewTextEditorView(result TextEditorViewResult) TextEditorContent {
	r := result.clone()
	return TextEditorContent{kind: TextEditorView, view: &r}
}
func NewTextEditorCreate(result TextEditorCreateResult) TextEditorContent {
	return TextEditorContent{kind: TextEditorCreate, create: &result}
}
func NewTextEditorReplace(result TextEditorReplaceResult) TextEditorContent {
	r := result.clone()
	return TextEditorContent{kind: TextEditorReplace, replace: &r}
}
func NewTextEditorFailure(code TextEditorErrorCode, message *string) TextEditorContent {
	return TextEditorContent{kind: TextEditorFailure, failure: &TextEditorError{Code: code, Message: copyMetadataPointer(message), messageNull: message == nil}}
}
func (c TextEditorContent) Kind() TextEditorContentKind { return c.kind }
func (c TextEditorContent) View() (TextEditorViewResult, bool) {
	if c.kind != TextEditorView || c.view == nil {
		return TextEditorViewResult{}, false
	}
	return c.view.clone(), true
}
func (c TextEditorContent) Create() (TextEditorCreateResult, bool) {
	if c.kind != TextEditorCreate || c.create == nil {
		return TextEditorCreateResult{}, false
	}
	return *c.create, true
}
func (c TextEditorContent) Replace() (TextEditorReplaceResult, bool) {
	if c.kind != TextEditorReplace || c.replace == nil {
		return TextEditorReplaceResult{}, false
	}
	return c.replace.clone(), true
}
func (c TextEditorContent) Error() (TextEditorError, bool) {
	if c.kind != TextEditorFailure || c.failure == nil {
		return TextEditorError{}, false
	}
	return c.failure.clone(), true
}
func (c TextEditorContent) clone() TextEditorContent {
	if c.view != nil {
		r := c.view.clone()
		c.view = &r
	}
	c.create = copyMetadataPointer(c.create)
	if c.replace != nil {
		r := c.replace.clone()
		c.replace = &r
	}
	if c.failure != nil {
		r := c.failure.clone()
		c.failure = &r
	}
	return c
}
func (c TextEditorContent) Validate() error {
	if c.kind == TextEditorView && c.view != nil && c.create == nil && c.replace == nil && c.failure == nil {
		return c.view.Validate()
	}
	if c.kind == TextEditorCreate && c.create != nil && c.view == nil && c.replace == nil && c.failure == nil {
		return nil
	}
	if c.kind == TextEditorReplace && c.replace != nil && c.view == nil && c.create == nil && c.failure == nil {
		return c.replace.Validate()
	}
	if c.kind == TextEditorFailure && c.failure != nil && c.view == nil && c.create == nil && c.replace == nil {
		return nil
	}
	return errors.New("editor content requires one view/create/replace/error variant")
}
func (c TextEditorContent) MarshalJSON() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	switch c.kind {
	case TextEditorView:
		return json.Marshal(c.view)
	case TextEditorCreate:
		return json.Marshal(c.create)
	case TextEditorReplace:
		return json.Marshal(c.replace)
	default:
		return json.Marshal(c.failure)
	}
}
func (c TextEditorContent) WithSDKDefaults() TextEditorContent {
	c = c.clone()
	if c.view != nil {
		r := c.view.WithSDKDefaults()
		c.view = &r
	}
	if c.replace != nil {
		r := c.replace.WithSDKDefaults()
		c.replace = &r
	}
	if c.failure != nil {
		r := c.failure.WithSDKDefaults()
		c.failure = &r
	}
	return c
}
func decodeTextEditorContent(data []byte, bounded bool) (TextEditorContent, error) {
	var wire struct{ Type *string }
	if _, _, err := decodeMetadataObject(data, &wire, []string{"type"}, bounded); err != nil {
		return TextEditorContent{}, err
	}
	if wire.Type == nil {
		return TextEditorContent{}, errors.New("editor content requires type")
	}
	switch *wire.Type {
	case "text_editor_code_execution_view_result":
		r, err := decodeTextEditorView(data, bounded)
		if err != nil {
			return TextEditorContent{}, err
		}
		return TextEditorContent{kind: TextEditorView, view: &r}, nil
	case "text_editor_code_execution_create_result":
		r, err := decodeTextEditorCreate(data, bounded)
		if err != nil {
			return TextEditorContent{}, err
		}
		return TextEditorContent{kind: TextEditorCreate, create: &r}, nil
	case "text_editor_code_execution_str_replace_result":
		r, err := decodeTextEditorReplace(data, bounded)
		if err != nil {
			return TextEditorContent{}, err
		}
		return TextEditorContent{kind: TextEditorReplace, replace: &r}, nil
	case "text_editor_code_execution_tool_result_error":
		r, err := decodeTextEditorError(data, bounded)
		if err != nil {
			return TextEditorContent{}, err
		}
		return TextEditorContent{kind: TextEditorFailure, failure: &r}, nil
	default:
		return TextEditorContent{}, errors.New("unsupported editor content type")
	}
}
func (c *TextEditorContent) UnmarshalJSON(data []byte) error {
	next, err := decodeTextEditorContent(data, true)
	if err == nil {
		*c = next
	}
	return err
}

type TextEditorToolResultBlock struct {
	ToolUseID                                 string
	Content                                   TextEditorContent
	textNull, citationsNull, parsedOutputNull bool
	cache                                     *CacheControl
	extra                                     jsonvalue.Value
}

func (r TextEditorToolResultBlock) clone() TextEditorToolResultBlock {
	r.Content = r.Content.clone()
	r.cache = copyMetadataPointer(r.cache)
	return r
}
func (r TextEditorToolResultBlock) Validate() error {
	if err := r.Content.Validate(); err != nil {
		return err
	}
	if r.cache != nil {
		return r.cache.Validate()
	}
	return nil
}
func (r TextEditorToolResultBlock) WithSDKDefaults(streamed bool) TextEditorToolResultBlock {
	r = r.clone()
	r.Content = r.Content.WithSDKDefaults()
	if streamed {
		r.textNull, r.citationsNull, r.parsedOutputNull = true, true, true
	}
	return r
}
func (r TextEditorToolResultBlock) marshal(control *CacheControl) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if control == nil {
		control = r.cache
	}
	return marshalMetadataObject(struct {
		Type         BlockKind                `json:"type"`
		ToolUseID    string                   `json:"tool_use_id"`
		Content      TextEditorContent        `json:"content"`
		Text         *wireOptional[string]    `json:"text,omitempty"`
		Citations    *wireOptional[Citations] `json:"citations,omitempty"`
		ParsedOutput *wireOptional[string]    `json:"parsed_output,omitempty"`
		Cache        *CacheControl            `json:"cache_control,omitempty"`
	}{BlockTextEditorToolResult, r.ToolUseID, r.Content, optionalWireValue[string](nil, r.textNull), optionalWireValue[Citations](nil, r.citationsNull), optionalWireValue[string](nil, r.parsedOutputNull), control}, r.extra)
}
func (r TextEditorToolResultBlock) MarshalJSON() ([]byte, error) { return r.marshal(nil) }
func decodeTextEditorBlock(data []byte, bounded bool) (TextEditorToolResultBlock, error) {
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
		return TextEditorToolResultBlock{}, err
	}
	if wire.Type == nil || *wire.Type != BlockTextEditorToolResult || wire.ToolUseID == nil || len(wire.Content) == 0 {
		return TextEditorToolResultBlock{}, errors.New("text editor result requires type, tool_use_id and content")
	}
	content, err := decodeTextEditorContent(wire.Content, bounded)
	if err != nil {
		return TextEditorToolResultBlock{}, err
	}
	for _, raw := range []json.RawMessage{wire.Text, wire.Citations, wire.ParsedOutput} {
		if len(raw) > 0 && string(raw) != "null" {
			return TextEditorToolResultBlock{}, errors.New("text editor stream metadata must be null")
		}
	}
	next := TextEditorToolResultBlock{ToolUseID: *wire.ToolUseID, Content: content, textNull: len(wire.Text) > 0, citationsNull: len(wire.Citations) > 0, parsedOutputNull: len(wire.ParsedOutput) > 0, cache: wire.Cache, extra: extra}
	return next, next.Validate()
}
func (r *TextEditorToolResultBlock) UnmarshalJSON(data []byte) error {
	next, err := decodeTextEditorBlock(data, true)
	if err == nil {
		*r = next
	}
	return err
}
func (r TextEditorToolResultBlock) Block() Block {
	result := r.clone()
	return Block{kind: BlockTextEditorToolResult, textEditor: &result}
}
func NewTextEditorToolResultBlock(toolUseID string, content TextEditorContent) Block {
	return (TextEditorToolResultBlock{ToolUseID: toolUseID, Content: content}).Block()
}
func (b Block) TextEditorToolResult() (TextEditorToolResultBlock, bool) {
	if b.kind != BlockTextEditorToolResult || b.textEditor == nil {
		return TextEditorToolResultBlock{}, false
	}
	return b.textEditor.clone(), true
}
