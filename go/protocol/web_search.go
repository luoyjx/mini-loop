package protocol

import (
	"encoding/json"
	"errors"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// Server search results are observations, never executable client tool results.
type WebSearchErrorCode string

const (
	WebSearchInvalidInput    WebSearchErrorCode = "invalid_tool_input"
	WebSearchUnavailable     WebSearchErrorCode = "unavailable"
	WebSearchMaxUses         WebSearchErrorCode = "max_uses_exceeded"
	WebSearchTooManyRequests WebSearchErrorCode = "too_many_requests"
	WebSearchQueryTooLong    WebSearchErrorCode = "query_too_long"
	WebSearchRequestTooLarge WebSearchErrorCode = "request_too_large"
)

type WebSearchResult struct {
	Title            string
	URL              string
	EncryptedContent string
	PageAge          *string
	pageAgeNull      bool
	extra            jsonvalue.Value
}

func (r WebSearchResult) clone() WebSearchResult {
	r.PageAge = copyMetadataPointer(r.PageAge)
	return r
}
func (r WebSearchResult) MarshalJSON() ([]byte, error) {
	return marshalMetadataObject(struct {
		Type             string                `json:"type"`
		Title            string                `json:"title"`
		URL              string                `json:"url"`
		EncryptedContent string                `json:"encrypted_content"`
		PageAge          *wireOptional[string] `json:"page_age,omitempty"`
	}{"web_search_result", r.Title, r.URL, r.EncryptedContent, optionalWireValue(r.PageAge, r.pageAgeNull)}, r.extra)
}
func decodeWebSearchResult(data []byte, bounded bool) (WebSearchResult, error) {
	var wire struct {
		Type, Title, URL *string
		EncryptedContent *string `json:"encrypted_content"`
		PageAge          *string `json:"page_age"`
	}
	value, extra, err := decodeMetadataObject(data, &wire, []string{"type", "title", "url", "encrypted_content", "page_age"}, bounded)
	if err != nil {
		return WebSearchResult{}, err
	}
	if wire.Type == nil || *wire.Type != "web_search_result" || wire.Title == nil || wire.URL == nil || wire.EncryptedContent == nil {
		return WebSearchResult{}, errors.New("web search result requires type, title, url and encrypted_content")
	}
	page, present := value.Lookup("page_age")
	return WebSearchResult{Title: *wire.Title, URL: *wire.URL, EncryptedContent: *wire.EncryptedContent, PageAge: wire.PageAge, pageAgeNull: present && page.Kind() == jsonvalue.Null, extra: extra}, nil
}
func (r *WebSearchResult) UnmarshalJSON(data []byte) error {
	next, err := decodeWebSearchResult(data, true)
	if err == nil {
		*r = next
	}
	return err
}

type WebSearchError struct {
	Code  WebSearchErrorCode
	extra jsonvalue.Value
}

func (e WebSearchError) MarshalJSON() ([]byte, error) {
	return marshalMetadataObject(struct {
		Type string             `json:"type"`
		Code WebSearchErrorCode `json:"error_code"`
	}{"web_search_tool_result_error", e.Code}, e.extra)
}
func decodeWebSearchError(data []byte, bounded bool) (WebSearchError, error) {
	var wire struct {
		Type *string
		Code *WebSearchErrorCode `json:"error_code"`
	}
	_, extra, err := decodeMetadataObject(data, &wire, []string{"type", "error_code"}, bounded)
	if err != nil {
		return WebSearchError{}, err
	}
	if wire.Type == nil || *wire.Type != "web_search_tool_result_error" || wire.Code == nil {
		return WebSearchError{}, errors.New("web search error requires type and error_code")
	}
	return WebSearchError{Code: *wire.Code, extra: extra}, nil
}
func (e *WebSearchError) UnmarshalJSON(data []byte) error {
	next, err := decodeWebSearchError(data, true)
	if err == nil {
		*e = next
	}
	return err
}

type WebSearchContentKind string

const (
	WebSearchResults WebSearchContentKind = "results"
	WebSearchFailure WebSearchContentKind = "error"
)

// WebSearchContent is the SDK's list-or-error closed union; [] is a valid result.
type WebSearchContent struct {
	kind    WebSearchContentKind
	results []WebSearchResult
	failure *WebSearchError
}

func NewWebSearchResults(results []WebSearchResult) WebSearchContent {
	return (WebSearchContent{kind: WebSearchResults, results: append([]WebSearchResult{}, results...)}).clone()
}
func NewWebSearchFailure(code WebSearchErrorCode) WebSearchContent {
	return WebSearchContent{kind: WebSearchFailure, failure: &WebSearchError{Code: code}}
}
func (c WebSearchContent) Kind() WebSearchContentKind { return c.kind }
func (c WebSearchContent) Results() ([]WebSearchResult, bool) {
	return c.clone().results, c.kind == WebSearchResults
}
func (c WebSearchContent) Error() (WebSearchError, bool) {
	if c.kind != WebSearchFailure || c.failure == nil {
		return WebSearchError{}, false
	}
	return *c.failure, true
}
func (c WebSearchContent) clone() WebSearchContent {
	if c.results != nil {
		c.results = append([]WebSearchResult{}, c.results...)
		for i := range c.results {
			c.results[i] = c.results[i].clone()
		}
	}
	c.failure = copyMetadataPointer(c.failure)
	return c
}
func (c WebSearchContent) Validate() error {
	if c.kind == WebSearchResults && c.results != nil && c.failure == nil {
		return nil
	}
	if c.kind == WebSearchFailure && c.failure != nil && c.results == nil {
		return nil
	}
	return errors.New("web search content requires exactly one results/error variant")
}
func (c WebSearchContent) MarshalJSON() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if c.kind == WebSearchResults {
		return json.Marshal(c.results)
	}
	return json.Marshal(c.failure)
}
func decodeWebSearchContent(data []byte, bounded bool) (WebSearchContent, error) {
	if bounded && len(data) > MaxWireBytes {
		return WebSearchContent{}, errors.New("web search content exceeds wire bound")
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return WebSearchContent{}, err
	}
	if value.Kind() == jsonvalue.Array {
		var rows []json.RawMessage
		if err := json.Unmarshal(data, &rows); err != nil {
			return WebSearchContent{}, err
		}
		results := []WebSearchResult{}
		for _, row := range rows {
			result, err := decodeWebSearchResult(row, bounded)
			if err != nil {
				return WebSearchContent{}, err
			}
			results = append(results, result)
		}
		return NewWebSearchResults(results), nil
	}
	if value.Kind() == jsonvalue.Object {
		failure, err := decodeWebSearchError(data, bounded)
		if err != nil {
			return WebSearchContent{}, err
		}
		return WebSearchContent{kind: WebSearchFailure, failure: &failure}, nil
	}
	return WebSearchContent{}, errors.New("web search content must be results array or error object")
}
func (c *WebSearchContent) UnmarshalJSON(data []byte) error {
	next, err := decodeWebSearchContent(data, true)
	if err == nil {
		*c = next
	}
	return err
}

type WebSearchToolResultBlock struct {
	ToolUseID     string
	Content       WebSearchContent
	Caller        *ToolCaller
	callerPresent bool
	cache         *CacheControl
	extra         jsonvalue.Value
}

func (r WebSearchToolResultBlock) clone() WebSearchToolResultBlock {
	r.Content = r.Content.clone()
	r.Caller = copyMetadataPointer(r.Caller)
	r.cache = copyMetadataPointer(r.cache)
	return r
}
func (r WebSearchToolResultBlock) Validate() error {
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
func (r WebSearchToolResultBlock) WithSDKDefaults() WebSearchToolResultBlock {
	r = r.clone()
	r.callerPresent = true
	for i := range r.Content.results {
		r.Content.results[i].pageAgeNull = r.Content.results[i].PageAge == nil
	}
	return r
}
func (r WebSearchToolResultBlock) marshal(control *CacheControl) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if control == nil {
		control = r.cache
	}
	return marshalMetadataObject(struct {
		Type      BlockKind                 `json:"type"`
		ToolUseID string                    `json:"tool_use_id"`
		Content   WebSearchContent          `json:"content"`
		Caller    *wireOptional[ToolCaller] `json:"caller,omitempty"`
		Cache     *CacheControl             `json:"cache_control,omitempty"`
	}{BlockWebSearchToolResult, r.ToolUseID, r.Content, optionalWireValue(r.Caller, r.callerPresent), control}, r.extra)
}
func (r WebSearchToolResultBlock) MarshalJSON() ([]byte, error) { return r.marshal(nil) }
func decodeWebSearchBlock(data []byte, bounded bool) (WebSearchToolResultBlock, error) {
	var wire struct {
		Type      *BlockKind
		ToolUseID *string `json:"tool_use_id"`
		Content   json.RawMessage
		Caller    json.RawMessage
		Cache     *CacheControl `json:"cache_control"`
	}
	_, extra, err := decodeMetadataObject(data, &wire, []string{"type", "tool_use_id", "content", "caller", "cache_control"}, bounded)
	if err != nil {
		return WebSearchToolResultBlock{}, err
	}
	if wire.Type == nil || *wire.Type != BlockWebSearchToolResult || wire.ToolUseID == nil || len(wire.Content) == 0 {
		return WebSearchToolResultBlock{}, errors.New("web search tool result requires type, tool_use_id and content")
	}
	content, err := decodeWebSearchContent(wire.Content, bounded)
	if err != nil {
		return WebSearchToolResultBlock{}, err
	}
	var caller *ToolCaller
	if len(wire.Caller) > 0 && string(wire.Caller) != "null" {
		decoded, err := decodeToolCaller(wire.Caller, bounded)
		if err != nil {
			return WebSearchToolResultBlock{}, err
		}
		caller = &decoded
	}
	next := WebSearchToolResultBlock{ToolUseID: *wire.ToolUseID, Content: content, Caller: caller, callerPresent: len(wire.Caller) > 0, cache: wire.Cache, extra: extra}
	return next, next.Validate()
}
func (r *WebSearchToolResultBlock) UnmarshalJSON(data []byte) error {
	next, err := decodeWebSearchBlock(data, true)
	if err == nil {
		*r = next
	}
	return err
}
func (r WebSearchToolResultBlock) Block() Block {
	result := r.clone()
	return Block{kind: BlockWebSearchToolResult, webSearch: &result}
}
func NewWebSearchToolResultBlock(toolUseID string, content WebSearchContent, caller *ToolCaller) Block {
	r := (WebSearchToolResultBlock{ToolUseID: toolUseID, Content: content, Caller: caller, callerPresent: true}).clone()
	return Block{kind: BlockWebSearchToolResult, webSearch: &r}
}
func (b Block) WebSearchToolResult() (WebSearchToolResultBlock, bool) {
	if b.kind != BlockWebSearchToolResult || b.webSearch == nil {
		return WebSearchToolResultBlock{}, false
	}
	return b.webSearch.clone(), true
}
