package protocol

import (
	"encoding/json"
	"errors"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type WebFetchErrorCode string

const (
	WebFetchInvalidInput           WebFetchErrorCode = "invalid_tool_input"
	WebFetchURLTooLong             WebFetchErrorCode = "url_too_long"
	WebFetchURLNotAllowed          WebFetchErrorCode = "url_not_allowed"
	WebFetchURLNotInPriorContext   WebFetchErrorCode = "url_not_in_prior_context"
	WebFetchURLNotAccessible       WebFetchErrorCode = "url_not_accessible"
	WebFetchUnsupportedContentType WebFetchErrorCode = "unsupported_content_type"
	WebFetchTooManyRequests        WebFetchErrorCode = "too_many_requests"
	WebFetchMaxUsesExceeded        WebFetchErrorCode = "max_uses_exceeded"
	WebFetchUnavailable            WebFetchErrorCode = "unavailable"
)

type WebFetchError struct {
	Code  WebFetchErrorCode
	extra jsonvalue.Value
}

func (e WebFetchError) MarshalJSON() ([]byte, error) {
	return marshalMetadataObject(struct {
		Type string            `json:"type"`
		Code WebFetchErrorCode `json:"error_code"`
	}{"web_fetch_tool_result_error", e.Code}, e.extra)
}
func decodeWebFetchError(data []byte, bounded bool) (WebFetchError, error) {
	var wire struct {
		Type *string
		Code *WebFetchErrorCode `json:"error_code"`
	}
	_, extra, err := decodeMetadataObject(data, &wire, []string{"type", "error_code"}, bounded)
	if err != nil {
		return WebFetchError{}, err
	}
	if wire.Type == nil || *wire.Type != "web_fetch_tool_result_error" || wire.Code == nil {
		return WebFetchError{}, errors.New("web fetch error requires type and error_code")
	}
	return WebFetchError{Code: *wire.Code, extra: extra}, nil
}
func (e *WebFetchError) UnmarshalJSON(data []byte) error {
	next, err := decodeWebFetchError(data, true)
	if err == nil {
		*e = next
	}
	return err
}

type WebFetchResult struct {
	Document        FetchedDocument
	URL             string
	RetrievedAt     *string
	retrievedAtNull bool
	extra           jsonvalue.Value
}

func (r WebFetchResult) clone() WebFetchResult {
	r.Document = r.Document.clone()
	r.RetrievedAt = copyMetadataPointer(r.RetrievedAt)
	return r
}
func (r WebFetchResult) Validate() error { return r.Document.Validate() }
func (r WebFetchResult) WithSDKDefaults() WebFetchResult {
	r = r.clone()
	r.Document = r.Document.WithSDKDefaults()
	r.retrievedAtNull = r.RetrievedAt == nil
	return r
}
func (r WebFetchResult) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return marshalMetadataObject(struct {
		Type        string                `json:"type"`
		Document    FetchedDocument       `json:"content"`
		URL         string                `json:"url"`
		RetrievedAt *wireOptional[string] `json:"retrieved_at,omitempty"`
	}{"web_fetch_result", r.Document, r.URL, optionalWireValue(r.RetrievedAt, r.retrievedAtNull)}, r.extra)
}
func decodeWebFetchResult(data []byte, bounded bool) (WebFetchResult, error) {
	var wire struct {
		Type        *string
		Document    json.RawMessage `json:"content"`
		URL         *string
		RetrievedAt *string `json:"retrieved_at"`
	}
	value, extra, err := decodeMetadataObject(data, &wire, []string{"type", "content", "url", "retrieved_at"}, bounded)
	if err != nil {
		return WebFetchResult{}, err
	}
	if wire.Type == nil || *wire.Type != "web_fetch_result" || wire.URL == nil || len(wire.Document) == 0 {
		return WebFetchResult{}, errors.New("web fetch result requires type, url and document")
	}
	document, err := decodeFetchedDocument(wire.Document, bounded)
	if err != nil {
		return WebFetchResult{}, err
	}
	retrieved, present := value.Lookup("retrieved_at")
	return WebFetchResult{Document: document, URL: *wire.URL, RetrievedAt: wire.RetrievedAt, retrievedAtNull: present && retrieved.Kind() == jsonvalue.Null, extra: extra}, nil
}
func (r *WebFetchResult) UnmarshalJSON(data []byte) error {
	next, err := decodeWebFetchResult(data, true)
	if err == nil {
		*r = next
	}
	return err
}

type WebFetchContentKind string

const (
	WebFetchSuccess WebFetchContentKind = "result"
	WebFetchFailure WebFetchContentKind = "error"
)

// WebFetchContent is the closed document-result/error union.
type WebFetchContent struct {
	kind    WebFetchContentKind
	result  *WebFetchResult
	failure *WebFetchError
}

func NewWebFetchSuccess(result WebFetchResult) WebFetchContent {
	r := result.clone()
	return WebFetchContent{kind: WebFetchSuccess, result: &r}
}
func NewWebFetchFailure(code WebFetchErrorCode) WebFetchContent {
	return WebFetchContent{kind: WebFetchFailure, failure: &WebFetchError{Code: code}}
}
func (c WebFetchContent) Kind() WebFetchContentKind { return c.kind }
func (c WebFetchContent) Result() (WebFetchResult, bool) {
	if c.kind != WebFetchSuccess || c.result == nil {
		return WebFetchResult{}, false
	}
	return c.result.clone(), true
}
func (c WebFetchContent) Error() (WebFetchError, bool) {
	if c.kind != WebFetchFailure || c.failure == nil {
		return WebFetchError{}, false
	}
	return *c.failure, true
}
func (c WebFetchContent) clone() WebFetchContent {
	if c.result != nil {
		r := c.result.clone()
		c.result = &r
	}
	c.failure = copyMetadataPointer(c.failure)
	return c
}
func (c WebFetchContent) Validate() error {
	if c.kind == WebFetchSuccess && c.result != nil && c.failure == nil {
		return c.result.Validate()
	}
	if c.kind == WebFetchFailure && c.failure != nil && c.result == nil {
		return nil
	}
	return errors.New("web fetch content requires one result/error variant")
}
func (c WebFetchContent) MarshalJSON() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if c.kind == WebFetchSuccess {
		return json.Marshal(c.result)
	}
	return json.Marshal(c.failure)
}
func decodeWebFetchContent(data []byte, bounded bool) (WebFetchContent, error) {
	var wire struct{ Type *string }
	if _, _, err := decodeMetadataObject(data, &wire, []string{"type"}, bounded); err != nil {
		return WebFetchContent{}, err
	}
	if wire.Type == nil {
		return WebFetchContent{}, errors.New("web fetch content requires type")
	}
	switch *wire.Type {
	case "web_fetch_result":
		r, err := decodeWebFetchResult(data, bounded)
		if err != nil {
			return WebFetchContent{}, err
		}
		return WebFetchContent{kind: WebFetchSuccess, result: &r}, nil
	case "web_fetch_tool_result_error":
		e, err := decodeWebFetchError(data, bounded)
		if err != nil {
			return WebFetchContent{}, err
		}
		return WebFetchContent{kind: WebFetchFailure, failure: &e}, nil
	default:
		return WebFetchContent{}, errors.New("unsupported web fetch content type")
	}
}
func (c *WebFetchContent) UnmarshalJSON(data []byte) error {
	next, err := decodeWebFetchContent(data, true)
	if err == nil {
		*c = next
	}
	return err
}

type WebFetchToolResultBlock struct {
	ToolUseID                                 string
	Content                                   WebFetchContent
	Caller                                    *ToolCaller
	callerPresent                             bool
	textNull, citationsNull, parsedOutputNull bool
	cache                                     *CacheControl
	extra                                     jsonvalue.Value
}

func (r WebFetchToolResultBlock) clone() WebFetchToolResultBlock {
	r.Content = r.Content.clone()
	r.Caller = copyMetadataPointer(r.Caller)
	r.cache = copyMetadataPointer(r.cache)
	return r
}
func (r WebFetchToolResultBlock) Validate() error {
	if err := r.Content.Validate(); err != nil {
		return err
	}
	if r.Caller != nil {
		if err := r.Caller.Validate(); err != nil {
			return err
		}
	}
	if r.cache != nil {
		return r.cache.Validate()
	}
	return nil
}
func (r WebFetchToolResultBlock) WithSDKDefaults(streamed bool) WebFetchToolResultBlock {
	r = r.clone()
	r.callerPresent = true
	if r.Content.result != nil {
		value := r.Content.result.WithSDKDefaults()
		r.Content.result = &value
	}
	if streamed {
		r.textNull = true
		r.citationsNull = true
		r.parsedOutputNull = true
	}
	return r
}
func (r WebFetchToolResultBlock) marshal(control *CacheControl) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if control == nil {
		control = r.cache
	}
	return marshalMetadataObject(struct {
		Type         BlockKind                 `json:"type"`
		ToolUseID    string                    `json:"tool_use_id"`
		Content      WebFetchContent           `json:"content"`
		Caller       *wireOptional[ToolCaller] `json:"caller,omitempty"`
		Text         *wireOptional[string]     `json:"text,omitempty"`
		Citations    *wireOptional[Citations]  `json:"citations,omitempty"`
		ParsedOutput *wireOptional[string]     `json:"parsed_output,omitempty"`
		Cache        *CacheControl             `json:"cache_control,omitempty"`
	}{BlockWebFetchToolResult, r.ToolUseID, r.Content, optionalWireValue(r.Caller, r.callerPresent), optionalWireValue[string](nil, r.textNull), optionalWireValue[Citations](nil, r.citationsNull), optionalWireValue[string](nil, r.parsedOutputNull), control}, r.extra)
}
func (r WebFetchToolResultBlock) MarshalJSON() ([]byte, error) { return r.marshal(nil) }
func decodeWebFetchBlock(data []byte, bounded bool) (WebFetchToolResultBlock, error) {
	var wire struct {
		Type            *BlockKind
		ToolUseID       *string `json:"tool_use_id"`
		Content         json.RawMessage
		Caller          json.RawMessage
		Text, Citations json.RawMessage
		ParsedOutput    json.RawMessage `json:"parsed_output"`
		Cache           *CacheControl   `json:"cache_control"`
	}
	_, extra, err := decodeMetadataObject(data, &wire, []string{"type", "tool_use_id", "content", "caller", "text", "citations", "parsed_output", "cache_control"}, bounded)
	if err != nil {
		return WebFetchToolResultBlock{}, err
	}
	if wire.Type == nil || *wire.Type != BlockWebFetchToolResult || wire.ToolUseID == nil || len(wire.Content) == 0 {
		return WebFetchToolResultBlock{}, errors.New("web fetch block requires type, tool_use_id and content")
	}
	content, err := decodeWebFetchContent(wire.Content, bounded)
	if err != nil {
		return WebFetchToolResultBlock{}, err
	}
	var caller *ToolCaller
	if len(wire.Caller) > 0 && string(wire.Caller) != "null" {
		caller = &ToolCaller{}
		if err := json.Unmarshal(wire.Caller, caller); err != nil {
			return WebFetchToolResultBlock{}, err
		}
	}
	for _, raw := range []json.RawMessage{wire.Text, wire.Citations, wire.ParsedOutput} {
		if len(raw) > 0 && string(raw) != "null" {
			return WebFetchToolResultBlock{}, errors.New("web fetch stream metadata must be null")
		}
	}
	next := WebFetchToolResultBlock{ToolUseID: *wire.ToolUseID, Content: content, Caller: caller, callerPresent: len(wire.Caller) > 0, textNull: len(wire.Text) > 0, citationsNull: len(wire.Citations) > 0, parsedOutputNull: len(wire.ParsedOutput) > 0, cache: wire.Cache, extra: extra}
	return next, next.Validate()
}
func (r *WebFetchToolResultBlock) UnmarshalJSON(data []byte) error {
	next, err := decodeWebFetchBlock(data, true)
	if err == nil {
		*r = next
	}
	return err
}
func (r WebFetchToolResultBlock) Block() Block {
	r = r.clone()
	return Block{kind: BlockWebFetchToolResult, webFetch: &r}
}
func NewWebFetchToolResultBlock(id string, content WebFetchContent) Block {
	return (WebFetchToolResultBlock{ToolUseID: id, Content: content}).Block()
}
func (b Block) WebFetchToolResult() (WebFetchToolResultBlock, bool) {
	if b.kind != BlockWebFetchToolResult || b.webFetch == nil {
		return WebFetchToolResultBlock{}, false
	}
	return b.webFetch.clone(), true
}
