package sessionquery

import (
	"errors"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type RequestEventKind uint8

const (
	RequestEventIgnored RequestEventKind = iota
	RequestModelStart
	RequestToolCatalog
	RequestSystemPrompt
	RequestCapabilityPlan
)

type requestStart struct{ epoch, count, tools, system, capability, model, maxTokens ArchiveValue }
type requestCatalog struct{ fingerprint, schemas ArchiveValue }
type requestSystem struct{ hash, text ArchiveValue }
type requestCapability struct {
	fingerprint ArchiveValue
	projection  CapabilityProjection
}

// RequestEvent is a closed historical union. Sequence presence is preserved;
// a missing seq defaults to zero during scanning but never matches model_start.
type RequestEvent struct {
	kind            RequestEventKind
	sequence        ArchiveValue
	sequencePresent bool
	start           requestStart
	catalog         requestCatalog
	system          requestSystem
	capability      requestCapability
}

func (event RequestEvent) Kind() RequestEventKind { return event.kind }

// DecodeRequestEvent projects only reconstruction fields, never live authority.
// Unknown event kinds retain sequence ordering but carry no actionable payload.
func DecodeRequestEvent(value ArchiveValue) (RequestEvent, error) {
	if value.Kind() != jsonvalue.Object {
		return RequestEvent{}, errors.New("request event must be an archive object")
	}
	get := func(name string) ArchiveValue { field, _ := value.Lookup(name); return field }
	event := RequestEvent{}
	event.sequence, event.sequencePresent = value.Lookup("seq")
	kind, _ := get("type").Text()
	switch kind {
	case "model_start":
		event.kind = RequestModelStart
		event.start = requestStart{get("transcript_epoch"), get("message_count"), get("tool_catalog_fingerprint"), get("system_hash"), get("capability_fingerprint"), get("model"), get("max_tokens")}
	case "tool_catalog":
		event.kind = RequestToolCatalog
		event.catalog = requestCatalog{get("fingerprint"), get("schemas")}
	case "system_prompt":
		event.kind = RequestSystemPrompt
		event.system = requestSystem{get("hash"), get("text")}
	case "capability_plan":
		event.kind = RequestCapabilityPlan
		event.capability = requestCapability{get("fingerprint"), CapabilityProjection{get("permission_mode"), get("sandbox"), get("sandbox_confined"), get("catalog_fingerprint")}}
	}
	return event, nil
}
