package protocol

import (
	"encoding/json"
	"fmt"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// CitationKind selects a documented SDK variant or preserved extension data.
// Citations describe provider text; they grant no tool or execution authority.
type CitationKind string

const (
	CitationChar         CitationKind = "char_location"
	CitationPage         CitationKind = "page_location"
	CitationContentBlock CitationKind = "content_block_location"
	CitationWebSearch    CitationKind = "web_search_result_location"
	CitationSearchResult CitationKind = "search_result_location"
	CitationExtension    CitationKind = "extension"
)

// CitationIndex retains signed Python integers without machine-width truncation.
type CitationIndex struct{ decimal string }

func (i CitationIndex) Decimal() string { return i.decimal }
func (i CitationIndex) value() jsonvalue.Value {
	v, _ := jsonvalue.Decode(i.decimal)
	return v
}

// CitationText distinguishes absent, null and string optional SDK fields.
type CitationText struct {
	present bool
	null    bool
	text    string
}

func (t CitationText) Present() bool        { return t.present }
func (t CitationText) Null() bool           { return t.present && t.null }
func (t CitationText) Text() (string, bool) { return t.text, t.present && !t.null }
func (t CitationText) value() jsonvalue.Value {
	if t.null {
		return jsonvalue.NullValue()
	}
	return jsonvalue.TextValue(t.text)
}

type CitationDocument struct {
	CitedText     string
	DocumentIndex CitationIndex
	DocumentTitle CitationText
	FileID        CitationText
}
type CitationCharLocation struct {
	CitationDocument
	StartCharIndex, EndCharIndex CitationIndex
}
type CitationPageLocation struct {
	CitationDocument
	StartPageNumber, EndPageNumber CitationIndex
}
type CitationContentBlockLocation struct {
	CitationDocument
	StartBlockIndex, EndBlockIndex CitationIndex
}
type CitationWebSearchLocation struct {
	CitedText, EncryptedIndex, URL string
	Title                          CitationText
}
type CitationSearchResultLocation struct {
	CitedText, Source                                 string
	SearchResultIndex, StartBlockIndex, EndBlockIndex CitationIndex
	Title                                             CitationText
}

// Citation is an immutable union. Malformed declared variants and unknown data
// accepted by the pinned SDK are preserved in a closed JSON extension variant.
// No mutable or raw JSON representation crosses this boundary.
type Citation struct {
	kind     CitationKind
	char     *CitationCharLocation
	page     *CitationPageLocation
	block    *CitationContentBlockLocation
	web      *CitationWebSearchLocation
	search   *CitationSearchResultLocation
	extra    jsonvalue.Value
	fallback jsonvalue.Value
}

func (c Citation) Kind() CitationKind { return c.kind }
func (c Citation) Char() (CitationCharLocation, bool) {
	if c.char == nil {
		return CitationCharLocation{}, false
	}
	return *c.char, true
}
func (c Citation) Page() (CitationPageLocation, bool) {
	if c.page == nil {
		return CitationPageLocation{}, false
	}
	return *c.page, true
}
func (c Citation) ContentBlock() (CitationContentBlockLocation, bool) {
	if c.block == nil {
		return CitationContentBlockLocation{}, false
	}
	return *c.block, true
}
func (c Citation) WebSearch() (CitationWebSearchLocation, bool) {
	if c.web == nil {
		return CitationWebSearchLocation{}, false
	}
	return *c.web, true
}
func (c Citation) SearchResult() (CitationSearchResultLocation, bool) {
	if c.search == nil {
		return CitationSearchResultLocation{}, false
	}
	return *c.search, true
}

// ExtraJSON returns an owned encoding of extension members only. ExtensionJSON
// returns the entire preserved value only for the extension variant.
func (c Citation) ExtraJSON() ([]byte, error) { return c.extra.MarshalJSON() }
func (c Citation) ExtensionJSON() ([]byte, bool, error) {
	if c.kind != CitationExtension {
		return nil, false, nil
	}
	b, err := c.fallback.MarshalJSON()
	return b, true, err
}
func (c Citation) MarshalJSON() ([]byte, error) { return c.value().MarshalJSON() }
func (c *Citation) UnmarshalJSON(data []byte) error {
	v, err := jsonvalue.Decode(string(data))
	if err != nil {
		return err
	}
	*c = citationFromValue(v)
	return nil
}

// Citations preserves absent/null/array/non-array metadata observed in the SDK.
// The zero value is absent. Its arrays and named variant accessors are detached.
type Citations struct {
	present bool
	array   bool
	items   []Citation
	other   jsonvalue.Value
}

func (c Citations) Present() bool                { return c.present }
func (c Citations) Items() ([]Citation, bool)    { return append([]Citation{}, c.items...), c.array }
func (c Citations) MarshalJSON() ([]byte, error) { return c.value().MarshalJSON() }
func (c *Citations) UnmarshalJSON(data []byte) error {
	next, err := DecodeCitations(data)
	if err != nil {
		return err
	}
	*c = next
	return nil
}

// DecodeCitations is a transport/storage boundary. Nil means absent, while JSON
// null is explicit null. Data is decoded immediately, never retained as bytes.
func DecodeCitations(data []byte) (Citations, error) {
	if len(data) == 0 {
		return Citations{}, nil
	}
	v, err := jsonvalue.Decode(string(data))
	if err != nil {
		return Citations{}, fmt.Errorf("citations: %w", err)
	}
	return citationsFromValue(v), nil
}
func citationsFromValue(v jsonvalue.Value) Citations {
	c := Citations{present: true, other: v}
	if items, ok := v.Array(); ok {
		c.array = true
		c.other = jsonvalue.Value{}
		c.items = make([]Citation, len(items))
		for i, item := range items {
			c.items[i] = citationFromValue(item)
		}
	}
	return c
}
func (c Citations) value() jsonvalue.Value {
	if !c.array {
		return c.other
	}
	items := make([]jsonvalue.Value, len(c.items))
	for i, item := range c.items {
		items[i] = item.value()
	}
	return jsonvalue.ArrayValue(items)
}

// Append mirrors SDK citation delta accumulation, allocating a fresh list.
func (c Citations) Append(citation Citation) (Citations, error) {
	if c.present && !c.array && c.other.Kind() != jsonvalue.Null {
		return Citations{}, fmt.Errorf("citation delta requires null or array metadata")
	}
	items := append([]Citation{}, c.items...)
	items = append(items, citation)
	return Citations{present: true, array: true, items: items}, nil
}

// MapStrings masks keys and values before escaping, including extension data.
// Recording projections may no longer have the original variant discriminator.
func (c Citations) MapStrings(mask func(string) string) Citations {
	if !c.present || mask == nil {
		return c
	}
	return citationsFromValue(c.value().MapStrings(mask))
}

func citationFromValue(v jsonvalue.Value) Citation {
	fallback := Citation{kind: CitationExtension, fallback: v}
	if v.Kind() != jsonvalue.Object {
		return fallback
	}
	tag, ok := v.Lookup("type")
	if !ok {
		return fallback
	}
	name, ok := tag.Text()
	if !ok {
		return fallback
	}
	c := Citation{kind: CitationKind(name)}
	consumed := []string{"type"}
	valid := true
	text := func(key string) string {
		consumed = append(consumed, key)
		field, found := v.Lookup(key)
		result, typed := field.Text()
		valid = valid && found && typed
		return result
	}
	index := func(key string) CitationIndex {
		consumed = append(consumed, key)
		field, found := v.Lookup(key)
		result, typed := field.Integer()
		valid = valid && found && typed
		return CitationIndex{result}
	}
	optional := func(key string) CitationText {
		consumed = append(consumed, key)
		field, found := v.Lookup(key)
		if !found {
			return CitationText{}
		}
		if field.Kind() == jsonvalue.Null {
			return CitationText{present: true, null: true}
		}
		result, typed := field.Text()
		valid = valid && typed
		return CitationText{present: true, text: result}
	}
	document := func() CitationDocument {
		return CitationDocument{CitedText: text("cited_text"), DocumentIndex: index("document_index"), DocumentTitle: optional("document_title"), FileID: optional("file_id")}
	}
	switch c.kind {
	case CitationChar:
		c.char = &CitationCharLocation{document(), index("start_char_index"), index("end_char_index")}
	case CitationPage:
		c.page = &CitationPageLocation{document(), index("start_page_number"), index("end_page_number")}
	case CitationContentBlock:
		c.block = &CitationContentBlockLocation{document(), index("start_block_index"), index("end_block_index")}
	case CitationWebSearch:
		c.web = &CitationWebSearchLocation{CitedText: text("cited_text"), EncryptedIndex: text("encrypted_index"), URL: text("url"), Title: optional("title")}
	case CitationSearchResult:
		c.search = &CitationSearchResultLocation{CitedText: text("cited_text"), Source: text("source"), SearchResultIndex: index("search_result_index"), StartBlockIndex: index("start_block_index"), EndBlockIndex: index("end_block_index"), Title: optional("title")}
	default:
		return fallback
	}
	if !valid {
		return fallback
	}
	fields := []jsonvalue.Field{}
	for _, key := range v.Keys() {
		used := false
		for _, known := range consumed {
			if key == known {
				used = true
				break
			}
		}
		if !used {
			field, _ := v.Lookup(key)
			fields = append(fields, jsonvalue.Field{Name: key, Value: field})
		}
	}
	c.extra = jsonvalue.ObjectValue(fields)
	return c
}
func (c Citation) value() jsonvalue.Value {
	if c.kind == CitationExtension {
		return c.fallback
	}
	fields := []jsonvalue.Field{{Name: "type", Value: jsonvalue.TextValue(string(c.kind))}}
	text := func(key, value string) {
		fields = append(fields, jsonvalue.Field{Name: key, Value: jsonvalue.TextValue(value)})
	}
	index := func(key string, value CitationIndex) {
		fields = append(fields, jsonvalue.Field{Name: key, Value: value.value()})
	}
	optional := func(key string, value CitationText) {
		if value.present {
			fields = append(fields, jsonvalue.Field{Name: key, Value: value.value()})
		}
	}
	document := func(d CitationDocument) {
		text("cited_text", d.CitedText)
		index("document_index", d.DocumentIndex)
		optional("document_title", d.DocumentTitle)
		optional("file_id", d.FileID)
	}
	switch c.kind {
	case CitationChar:
		if c.char == nil {
			return jsonvalue.NullValue()
		}
		document(c.char.CitationDocument)
		index("start_char_index", c.char.StartCharIndex)
		index("end_char_index", c.char.EndCharIndex)
	case CitationPage:
		if c.page == nil {
			return jsonvalue.NullValue()
		}
		document(c.page.CitationDocument)
		index("start_page_number", c.page.StartPageNumber)
		index("end_page_number", c.page.EndPageNumber)
	case CitationContentBlock:
		if c.block == nil {
			return jsonvalue.NullValue()
		}
		document(c.block.CitationDocument)
		index("start_block_index", c.block.StartBlockIndex)
		index("end_block_index", c.block.EndBlockIndex)
	case CitationWebSearch:
		if c.web == nil {
			return jsonvalue.NullValue()
		}
		text("cited_text", c.web.CitedText)
		text("encrypted_index", c.web.EncryptedIndex)
		text("url", c.web.URL)
		optional("title", c.web.Title)
	case CitationSearchResult:
		if c.search == nil {
			return jsonvalue.NullValue()
		}
		text("cited_text", c.search.CitedText)
		text("source", c.search.Source)
		index("search_result_index", c.search.SearchResultIndex)
		index("start_block_index", c.search.StartBlockIndex)
		index("end_block_index", c.search.EndBlockIndex)
		optional("title", c.search.Title)
	default:
		return jsonvalue.NullValue()
	}
	for _, key := range c.extra.Keys() {
		value, _ := c.extra.Lookup(key)
		fields = append(fields, jsonvalue.Field{Name: key, Value: value})
	}
	return jsonvalue.ObjectValue(fields)
}

// Keep json.Marshaler contracts explicit for transport and generic projections.
var _ json.Marshaler = Citation{}
var _ json.Marshaler = Citations{}
