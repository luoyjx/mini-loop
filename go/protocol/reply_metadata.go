package protocol

import (
	"encoding/json"
	"errors"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// ReplyMetadataKind separates absent/null metadata, structured SDK metadata and
// a finite non-object SDK observation. None of these observations grants authority.
type ReplyMetadataKind uint8

const (
	ReplyMetadataNull ReplyMetadataKind = iota
	ReplyMetadataStructured
	ReplyMetadataObserved
)

// SDKMetadataText retains a nullable text field and finite SDK fallback values.
// Its immutable observation is separate from the typed text accessor.
type SDKMetadataText struct{ value jsonvalue.Value }

func MetadataText(text string) SDKMetadataText {
	return SDKMetadataText{value: jsonvalue.TextValue(text)}
}
func (v SDKMetadataText) Text() (string, bool)         { return v.value.Text() }
func (v SDKMetadataText) Observation() jsonvalue.Value { return v.value }
func (v SDKMetadataText) MarshalJSON() ([]byte, error) { return v.value.MarshalJSON() }
func (v *SDKMetadataText) UnmarshalJSON(data []byte) error {
	value, err := finiteReplyMetadata(data)
	if err == nil {
		v.value = value
	}
	return err
}

// ReplyExpiry is a nullable timestamp observation. SDK normalization is selected
// explicitly at provider ingress; archive decoding preserves already stored data.
type ReplyExpiry struct{ value jsonvalue.Value }

func (v ReplyExpiry) Text() (string, bool)         { return v.value.Text() }
func (v ReplyExpiry) Observation() jsonvalue.Value { return v.value }
func (v ReplyExpiry) MarshalJSON() ([]byte, error) { return v.value.MarshalJSON() }
func (v *ReplyExpiry) UnmarshalJSON(data []byte) error {
	value, err := finiteReplyMetadata(data)
	if err == nil {
		v.value = value
	}
	return err
}

type ReplyContainer struct {
	ID        SDKMetadataText
	ExpiresAt ReplyExpiry
	extra     jsonvalue.Value
}

func (v ReplyContainer) Extensions() jsonvalue.Value { return v.extra }

type RefusalStopDetails struct {
	Type        SDKMetadataText
	Category    SDKMetadataText
	Explanation SDKMetadataText
	extra       jsonvalue.Value
}

func (v RefusalStopDetails) Extensions() jsonvalue.Value { return v.extra }

// ContainerMetadata is a closed null / named container / finite fallback union.
// Accessors return value copies; all nested observations are immutable.
type ContainerMetadata struct {
	kind      ReplyMetadataKind
	container ReplyContainer
	observed  jsonvalue.Value
}

func (v ContainerMetadata) Kind() ReplyMetadataKind { return v.kind }
func (v ContainerMetadata) Container() (ReplyContainer, bool) {
	return v.container, v.kind == ReplyMetadataStructured
}
func (v ContainerMetadata) Observation() (jsonvalue.Value, bool) {
	return v.observed, v.kind == ReplyMetadataObserved
}
func (v ContainerMetadata) WithSDKDatetime() ContainerMetadata {
	if v.kind == ReplyMetadataStructured {
		v.container.ExpiresAt.value = normalizeSDKReplyExpiry(v.container.ExpiresAt.value)
	}
	return v
}
func (v ContainerMetadata) MarshalJSON() ([]byte, error) {
	switch v.kind {
	case ReplyMetadataNull:
		return []byte("null"), nil
	case ReplyMetadataObserved:
		return v.observed.MarshalJSON()
	case ReplyMetadataStructured:
		return replyMetadataObject(v.container.extra,
			jsonvalue.Field{Name: "id", Value: v.container.ID.value},
			jsonvalue.Field{Name: "expires_at", Value: v.container.ExpiresAt.value}).MarshalJSON()
	default:
		return nil, errors.New("invalid container metadata variant")
	}
}
func (v *ContainerMetadata) UnmarshalJSON(data []byte) error {
	value, err := finiteReplyMetadata(data)
	if err != nil {
		return err
	}
	next := ContainerMetadata{}
	switch value.Kind() {
	case jsonvalue.Null:
	case jsonvalue.Object:
		id, _ := value.Lookup("id")
		expiry, _ := value.Lookup("expires_at")
		next.kind = ReplyMetadataStructured
		next.container = ReplyContainer{ID: SDKMetadataText{value: id}, ExpiresAt: ReplyExpiry{value: expiry}, extra: replyMetadataExtras(value, "id", "expires_at")}
	default:
		next.kind, next.observed = ReplyMetadataObserved, value
	}
	*v = next
	return nil
}

// StopDetailsMetadata models the SDK refusal fields while retaining future type
// strings and finite permissive observations for compatibility.
type StopDetailsMetadata struct {
	kind     ReplyMetadataKind
	refusal  RefusalStopDetails
	observed jsonvalue.Value
}

func (v StopDetailsMetadata) Kind() ReplyMetadataKind { return v.kind }
func (v StopDetailsMetadata) Refusal() (RefusalStopDetails, bool) {
	return v.refusal, v.kind == ReplyMetadataStructured
}
func (v StopDetailsMetadata) Observation() (jsonvalue.Value, bool) {
	return v.observed, v.kind == ReplyMetadataObserved
}
func (v StopDetailsMetadata) MarshalJSON() ([]byte, error) {
	switch v.kind {
	case ReplyMetadataNull:
		return []byte("null"), nil
	case ReplyMetadataObserved:
		return v.observed.MarshalJSON()
	case ReplyMetadataStructured:
		return replyMetadataObject(v.refusal.extra,
			jsonvalue.Field{Name: "type", Value: v.refusal.Type.value},
			jsonvalue.Field{Name: "category", Value: v.refusal.Category.value},
			jsonvalue.Field{Name: "explanation", Value: v.refusal.Explanation.value}).MarshalJSON()
	default:
		return nil, errors.New("invalid stop details variant")
	}
}
func (v *StopDetailsMetadata) UnmarshalJSON(data []byte) error {
	value, err := finiteReplyMetadata(data)
	if err != nil {
		return err
	}
	next := StopDetailsMetadata{}
	switch value.Kind() {
	case jsonvalue.Null:
	case jsonvalue.Object:
		kind, _ := value.Lookup("type")
		category, _ := value.Lookup("category")
		explanation, _ := value.Lookup("explanation")
		next.kind = ReplyMetadataStructured
		next.refusal = RefusalStopDetails{Type: SDKMetadataText{value: kind}, Category: SDKMetadataText{value: category}, Explanation: SDKMetadataText{value: explanation}, extra: replyMetadataExtras(value, "type", "category", "explanation")}
	default:
		next.kind, next.observed = ReplyMetadataObserved, value
	}
	*v = next
	return nil
}
func finiteReplyMetadata(data []byte) (jsonvalue.Value, error) {
	if len(data) > MaxWireBytes {
		return jsonvalue.Value{}, errors.New("reply metadata exceeds wire limit")
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return jsonvalue.Value{}, err
	}
	if _, err = value.MarshalJSON(); err != nil {
		return jsonvalue.Value{}, err
	}
	return value, nil
}
func replyMetadataExtras(value jsonvalue.Value, known ...string) jsonvalue.Value {
	fields := []jsonvalue.Field{}
	for _, key := range value.Keys() {
		matched := false
		for _, name := range known {
			if key == name {
				matched = true
				break
			}
		}
		if !matched {
			item, _ := value.Lookup(key)
			fields = append(fields, jsonvalue.Field{Name: key, Value: item})
		}
	}
	return jsonvalue.ObjectValue(fields)
}
func replyMetadataObject(extra jsonvalue.Value, fields ...jsonvalue.Field) jsonvalue.Value {
	for _, key := range extra.Keys() {
		item, _ := extra.Lookup(key)
		fields = append(fields, jsonvalue.Field{Name: key, Value: item})
	}
	return jsonvalue.ObjectValue(fields)
}

// Validate preserves the same finite UTF-8 boundary as the public JSON codec.
func (v ContainerMetadata) Validate() error   { _, err := json.Marshal(v); return err }
func (v StopDetailsMetadata) Validate() error { _, err := json.Marshal(v); return err }
