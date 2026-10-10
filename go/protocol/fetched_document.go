package protocol

import (
	"encoding/json"
	"errors"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type DocumentSourceKind string

const (
	DocumentText DocumentSourceKind = "text"
	DocumentPDF  DocumentSourceKind = "base64"
)

type DocumentMediaType string

const (
	DocumentPlainText      DocumentMediaType = "text/plain"
	DocumentApplicationPDF DocumentMediaType = "application/pdf"
)

// DocumentSource is the SDK's closed text/PDF union. Data is preserved verbatim;
// reading a provider observation does not decode or open the document.
type DocumentSource struct {
	kind      DocumentSourceKind
	Data      string
	MediaType DocumentMediaType
	extra     jsonvalue.Value
}

func TextDocumentSource(data string) DocumentSource {
	return DocumentSource{kind: DocumentText, Data: data, MediaType: DocumentPlainText}
}
func PDFDocumentSource(data string) DocumentSource {
	return DocumentSource{kind: DocumentPDF, Data: data, MediaType: DocumentApplicationPDF}
}
func (s DocumentSource) Kind() DocumentSourceKind { return s.kind }
func (s DocumentSource) Validate() error {
	if s.kind != DocumentText && s.kind != DocumentPDF {
		return errors.New("document source requires text/PDF variant")
	}
	return nil
}
func (s DocumentSource) MarshalJSON() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return marshalMetadataObject(struct {
		Type      DocumentSourceKind `json:"type"`
		Data      string             `json:"data"`
		MediaType DocumentMediaType  `json:"media_type"`
	}{s.kind, s.Data, s.MediaType}, s.extra)
}
func decodeDocumentSource(data []byte, bounded bool) (DocumentSource, error) {
	var wire struct {
		Type      *DocumentSourceKind
		Data      *string
		MediaType *DocumentMediaType `json:"media_type"`
	}
	_, extra, err := decodeMetadataObject(data, &wire, []string{"type", "data", "media_type"}, bounded)
	if err != nil {
		return DocumentSource{}, err
	}
	if wire.Type == nil || wire.Data == nil || wire.MediaType == nil {
		return DocumentSource{}, errors.New("document source requires type, data and media_type")
	}
	next := DocumentSource{kind: *wire.Type, Data: *wire.Data, MediaType: *wire.MediaType, extra: extra}
	return next, next.Validate()
}
func (s *DocumentSource) UnmarshalJSON(data []byte) error {
	next, err := decodeDocumentSource(data, true)
	if err == nil {
		*s = next
	}
	return err
}

type DocumentCitationsConfig struct {
	Enabled bool
	extra   jsonvalue.Value
}

func (c DocumentCitationsConfig) MarshalJSON() ([]byte, error) {
	return marshalMetadataObject(struct {
		Enabled bool `json:"enabled"`
	}{c.Enabled}, c.extra)
}
func decodeDocumentCitations(data []byte, bounded bool) (DocumentCitationsConfig, error) {
	var wire struct{ Enabled *bool }
	_, extra, err := decodeMetadataObject(data, &wire, []string{"enabled"}, bounded)
	if err != nil {
		return DocumentCitationsConfig{}, err
	}
	if wire.Enabled == nil {
		return DocumentCitationsConfig{}, errors.New("document citations requires enabled")
	}
	return DocumentCitationsConfig{Enabled: *wire.Enabled, extra: extra}, nil
}
func (c *DocumentCitationsConfig) UnmarshalJSON(data []byte) error {
	next, err := decodeDocumentCitations(data, true)
	if err == nil {
		*c = next
	}
	return err
}

type FetchedDocument struct {
	Source                   DocumentSource
	Title                    *string
	Citations                *DocumentCitationsConfig
	titleNull, citationsNull bool
	extra                    jsonvalue.Value
}

func (d FetchedDocument) clone() FetchedDocument {
	d.Title = copyMetadataPointer(d.Title)
	d.Citations = copyMetadataPointer(d.Citations)
	return d
}
func (d FetchedDocument) Validate() error { return d.Source.Validate() }
func (d FetchedDocument) WithSDKDefaults() FetchedDocument {
	d = d.clone()
	d.titleNull = d.Title == nil
	d.citationsNull = d.Citations == nil
	return d
}
func (d FetchedDocument) MarshalJSON() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return marshalMetadataObject(struct {
		Type      string                                 `json:"type"`
		Source    DocumentSource                         `json:"source"`
		Title     *wireOptional[string]                  `json:"title,omitempty"`
		Citations *wireOptional[DocumentCitationsConfig] `json:"citations,omitempty"`
	}{"document", d.Source, optionalWireValue(d.Title, d.titleNull), optionalWireValue(d.Citations, d.citationsNull)}, d.extra)
}
func decodeFetchedDocument(data []byte, bounded bool) (FetchedDocument, error) {
	var wire struct {
		Type      *string
		Source    json.RawMessage
		Title     *string
		Citations json.RawMessage
	}
	value, extra, err := decodeMetadataObject(data, &wire, []string{"type", "source", "title", "citations"}, bounded)
	if err != nil {
		return FetchedDocument{}, err
	}
	if wire.Type == nil || *wire.Type != "document" || len(wire.Source) == 0 {
		return FetchedDocument{}, errors.New("fetched document requires type and source")
	}
	source, err := decodeDocumentSource(wire.Source, bounded)
	if err != nil {
		return FetchedDocument{}, err
	}
	var citations *DocumentCitationsConfig
	if len(wire.Citations) > 0 && string(wire.Citations) != "null" {
		c, err := decodeDocumentCitations(wire.Citations, bounded)
		if err != nil {
			return FetchedDocument{}, err
		}
		citations = &c
	}
	title, present := value.Lookup("title")
	return FetchedDocument{Source: source, Title: wire.Title, Citations: citations, titleNull: present && title.Kind() == jsonvalue.Null, citationsNull: len(wire.Citations) > 0 && string(wire.Citations) == "null", extra: extra}, nil
}
func (d *FetchedDocument) UnmarshalJSON(data []byte) error {
	next, err := decodeFetchedDocument(data, true)
	if err == nil {
		*d = next
	}
	return err
}
