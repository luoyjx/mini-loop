package protocol

import (
	"encoding/json"
	"errors"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// Named nested SDK usage. Counts retain the native non-negative integer boundary.
type CacheCreationUsage struct {
	Ephemeral1hInputTokens *int `json:"ephemeral_1h_input_tokens"`
	Ephemeral5mInputTokens *int `json:"ephemeral_5m_input_tokens"`
	extra                  jsonvalue.Value
	nulls                  usageNulls
}
type OutputTokenDetails struct {
	ThinkingTokens *int `json:"thinking_tokens"`
	extra          jsonvalue.Value
	nulls          usageNulls
}
type ServerToolUsage struct {
	WebFetchRequests  *int `json:"web_fetch_requests"`
	WebSearchRequests *int `json:"web_search_requests"`
	extra             jsonvalue.Value
	nulls             usageNulls
}

// TokenUsage keeps absent/null/value for optional fields. Closed extension members
// are immutable; recognized fields always enter named types before runtime use.
type TokenUsage struct {
	InputTokens              int                 `json:"input_tokens"`
	OutputTokens             int                 `json:"output_tokens"`
	CacheReadInputTokens     *int                `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens *int                `json:"cache_creation_input_tokens,omitempty"`
	ServiceTier              *ServiceTier        `json:"service_tier,omitempty"`
	CacheCreation            *CacheCreationUsage `json:"cache_creation,omitempty"`
	InferenceGeo             *string             `json:"inference_geo,omitempty"`
	OutputTokensDetails      *OutputTokenDetails `json:"output_tokens_details,omitempty"`
	ServerToolUse            *ServerToolUsage    `json:"server_tool_use,omitempty"`
	nulls                    usageNulls
	extra                    jsonvalue.Value
}

type usageNulls uint8

const (
	nullCacheRead usageNulls = 1 << iota
	nullCacheInput
	nullTier
	nullCacheCreation
	nullGeo
	nullOutputDetails
	nullServerTool
)

func (u TokenUsage) Validate() error {
	counts := []*int{&u.InputTokens, &u.OutputTokens, u.CacheReadInputTokens, u.CacheCreationInputTokens}
	if u.CacheCreation != nil {
		counts = append(counts, u.CacheCreation.Ephemeral1hInputTokens, u.CacheCreation.Ephemeral5mInputTokens)
	}
	if u.OutputTokensDetails != nil {
		counts = append(counts, u.OutputTokensDetails.ThinkingTokens)
	}
	if u.ServerToolUse != nil {
		counts = append(counts, u.ServerToolUse.WebFetchRequests, u.ServerToolUse.WebSearchRequests)
	}
	return validateUsageCounts(counts...)
}
func validateUsageCounts(counts ...*int) error {
	for _, count := range counts {
		if count != nil && *count < 0 {
			return errors.New("usage counts must be non-negative")
		}
	}
	return nil
}
func copyUsagePointer[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
func (u TokenUsage) clone() TokenUsage {
	u.CacheReadInputTokens = copyUsagePointer(u.CacheReadInputTokens)
	u.CacheCreationInputTokens = copyUsagePointer(u.CacheCreationInputTokens)
	u.ServiceTier = copyUsagePointer(u.ServiceTier)
	u.CacheCreation = copyUsagePointer(u.CacheCreation)
	u.InferenceGeo = copyUsagePointer(u.InferenceGeo)
	u.OutputTokensDetails = copyUsagePointer(u.OutputTokensDetails)
	u.ServerToolUse = copyUsagePointer(u.ServerToolUse)
	if u.CacheCreation != nil {
		u.CacheCreation.Ephemeral1hInputTokens = copyUsagePointer(u.CacheCreation.Ephemeral1hInputTokens)
		u.CacheCreation.Ephemeral5mInputTokens = copyUsagePointer(u.CacheCreation.Ephemeral5mInputTokens)
	}
	if u.OutputTokensDetails != nil {
		u.OutputTokensDetails.ThinkingTokens = copyUsagePointer(u.OutputTokensDetails.ThinkingTokens)
	}
	if u.ServerToolUse != nil {
		u.ServerToolUse.WebFetchRequests = copyUsagePointer(u.ServerToolUse.WebFetchRequests)
		u.ServerToolUse.WebSearchRequests = copyUsagePointer(u.ServerToolUse.WebSearchRequests)
	}

	return u
}

// WithSDKDefaults materializes the seven nullable SDK fields. Native/fake usage
// constructors retain their original sparse shape until this explicit selection.
func (u TokenUsage) WithSDKDefaults() TokenUsage {
	u = u.clone()
	if u.CacheReadInputTokens == nil {
		u.nulls |= nullCacheRead
	} else {
		u.nulls &^= nullCacheRead
	}
	if u.CacheCreationInputTokens == nil {
		u.nulls |= nullCacheInput
	} else {
		u.nulls &^= nullCacheInput
	}
	if u.ServiceTier == nil {
		u.nulls |= nullTier
	} else {
		u.nulls &^= nullTier
	}
	if u.CacheCreation == nil {
		u.nulls |= nullCacheCreation
	} else {
		u.nulls &^= nullCacheCreation
	}
	if u.InferenceGeo == nil {
		u.nulls |= nullGeo
	} else {
		u.nulls &^= nullGeo
	}
	if u.OutputTokensDetails == nil {
		u.nulls |= nullOutputDetails
	} else {
		u.nulls &^= nullOutputDetails
	}
	if u.ServerToolUse == nil {
		u.nulls |= nullServerTool
	} else {
		u.nulls &^= nullServerTool
	}
	if u.CacheCreation != nil {
		if u.CacheCreation.Ephemeral1hInputTokens == nil {
			u.CacheCreation.nulls |= 1
		}
		if u.CacheCreation.Ephemeral5mInputTokens == nil {
			u.CacheCreation.nulls |= 2
		}
	}
	if u.OutputTokensDetails != nil && u.OutputTokensDetails.ThinkingTokens == nil {
		u.OutputTokensDetails.nulls |= 1
	}
	if u.ServerToolUse != nil {
		if u.ServerToolUse.WebFetchRequests == nil {
			u.ServerToolUse.nulls |= 1
		}
		if u.ServerToolUse.WebSearchRequests == nil {
			u.ServerToolUse.nulls |= 2
		}
	}
	return u
}
func usageOptional[T any](p *T, null bool) *wireOptional[T] {
	if p != nil {
		return &wireOptional[T]{value: p}
	}
	if null {
		return &wireOptional[T]{}
	}
	return nil
}
func marshalUsageObject[T any](wire T, extra jsonvalue.Value) ([]byte, error) {
	data, err := json.Marshal(wire)
	if err != nil {
		return nil, err
	}
	if len(extra.Keys()) == 0 {
		return data, nil
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return nil, err
	}
	fields := make([]jsonvalue.Field, 0, len(value.Keys())+len(extra.Keys()))
	for _, key := range value.Keys() {
		v, _ := value.Lookup(key)
		fields = append(fields, jsonvalue.Field{Name: key, Value: v})
	}
	for _, key := range extra.Keys() {
		v, _ := extra.Lookup(key)
		fields = append(fields, jsonvalue.Field{Name: key, Value: v})
	}
	return jsonvalue.ObjectValue(fields).MarshalJSON()
}
func decodeUsageObject[T any](data []byte, wire *T, known []string) (jsonvalue.Value, jsonvalue.Value, error) {
	if len(data) > MaxWireBytes {
		return jsonvalue.Value{}, jsonvalue.Value{}, errors.New("usage JSON exceeds wire bound")
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return jsonvalue.Value{}, jsonvalue.Value{}, err
	}
	if value.Kind() != jsonvalue.Object {
		return jsonvalue.Value{}, jsonvalue.Value{}, errors.New("usage must be an object")
	}
	if err := json.Unmarshal(data, wire); err != nil {
		return jsonvalue.Value{}, jsonvalue.Value{}, err
	}
	fields := []jsonvalue.Field{}
	for _, key := range value.Keys() {
		recognized := false
		for _, name := range known {
			if name == key {
				recognized = true
				break
			}
		}
		if !recognized {
			v, _ := value.Lookup(key)
			fields = append(fields, jsonvalue.Field{Name: key, Value: v})
		}
	}
	if len(fields) == 0 {
		return value, jsonvalue.Value{}, nil
	}
	return value, jsonvalue.ObjectValue(fields), nil
}

func (u TokenUsage) MarshalJSON() ([]byte, error) {
	wire := struct {
		Input         int                               `json:"input_tokens"`
		Output        int                               `json:"output_tokens"`
		Read          *wireOptional[int]                `json:"cache_read_input_tokens,omitempty"`
		CreationInput *wireOptional[int]                `json:"cache_creation_input_tokens,omitempty"`
		Tier          *wireOptional[ServiceTier]        `json:"service_tier,omitempty"`
		Creation      *wireOptional[CacheCreationUsage] `json:"cache_creation,omitempty"`
		Geo           *wireOptional[string]             `json:"inference_geo,omitempty"`
		Details       *wireOptional[OutputTokenDetails] `json:"output_tokens_details,omitempty"`
		Server        *wireOptional[ServerToolUsage]    `json:"server_tool_use,omitempty"`
	}{u.InputTokens, u.OutputTokens, usageOptional(u.CacheReadInputTokens, u.nulls&nullCacheRead != 0), usageOptional(u.CacheCreationInputTokens, u.nulls&nullCacheInput != 0), usageOptional(u.ServiceTier, u.nulls&nullTier != 0), usageOptional(u.CacheCreation, u.nulls&nullCacheCreation != 0), usageOptional(u.InferenceGeo, u.nulls&nullGeo != 0), usageOptional(u.OutputTokensDetails, u.nulls&nullOutputDetails != 0), usageOptional(u.ServerToolUse, u.nulls&nullServerTool != 0)}
	return marshalUsageObject(wire, u.extra)
}
func (u *TokenUsage) UnmarshalJSON(data []byte) error {
	var wire struct {
		Input         *int                `json:"input_tokens"`
		Output        *int                `json:"output_tokens"`
		Read          *int                `json:"cache_read_input_tokens"`
		CreationInput *int                `json:"cache_creation_input_tokens"`
		Tier          *ServiceTier        `json:"service_tier"`
		Creation      *CacheCreationUsage `json:"cache_creation"`
		Geo           *string             `json:"inference_geo"`
		Details       *OutputTokenDetails `json:"output_tokens_details"`
		Server        *ServerToolUsage    `json:"server_tool_use"`
	}
	keys := []string{"input_tokens", "output_tokens", "cache_read_input_tokens", "cache_creation_input_tokens", "service_tier", "cache_creation", "inference_geo", "output_tokens_details", "server_tool_use"}
	value, extra, err := decodeUsageObject(data, &wire, keys)
	if err != nil {
		return err
	}
	if wire.Input == nil || wire.Output == nil {
		return errors.New("usage requires input_tokens and output_tokens")
	}
	next := TokenUsage{InputTokens: *wire.Input, OutputTokens: *wire.Output, CacheReadInputTokens: wire.Read, CacheCreationInputTokens: wire.CreationInput, ServiceTier: wire.Tier, CacheCreation: wire.Creation, InferenceGeo: wire.Geo, OutputTokensDetails: wire.Details, ServerToolUse: wire.Server, extra: extra}
	for i, key := range keys[2:] {
		if v, present := value.Lookup(key); present && v.Kind() == jsonvalue.Null {
			next.nulls |= 1 << i
		}
	}
	if err := next.Validate(); err != nil {
		return err
	}
	*u = next
	return nil
}

func usageNullFields(value jsonvalue.Value, keys ...string) usageNulls {
	var flags usageNulls
	for i, key := range keys {
		if v, present := value.Lookup(key); present && v.Kind() == jsonvalue.Null {
			flags |= 1 << i
		}
	}
	return flags
}
func (v CacheCreationUsage) MarshalJSON() ([]byte, error) {
	return marshalUsageObject(struct {
		Hour   *wireOptional[int] `json:"ephemeral_1h_input_tokens,omitempty"`
		Minute *wireOptional[int] `json:"ephemeral_5m_input_tokens,omitempty"`
	}{usageOptional(v.Ephemeral1hInputTokens, v.nulls&1 != 0), usageOptional(v.Ephemeral5mInputTokens, v.nulls&2 != 0)}, v.extra)
}
func (v *CacheCreationUsage) UnmarshalJSON(data []byte) error {
	var wire struct {
		Hour   *int `json:"ephemeral_1h_input_tokens"`
		Minute *int `json:"ephemeral_5m_input_tokens"`
	}
	value, extra, err := decodeUsageObject(data, &wire, []string{"ephemeral_1h_input_tokens", "ephemeral_5m_input_tokens"})
	if err != nil {
		return err
	}
	if err := validateUsageCounts(wire.Hour, wire.Minute); err != nil {
		return err
	}
	*v = CacheCreationUsage{Ephemeral1hInputTokens: wire.Hour, Ephemeral5mInputTokens: wire.Minute, extra: extra, nulls: usageNullFields(value, "ephemeral_1h_input_tokens", "ephemeral_5m_input_tokens")}
	return nil
}
func (v OutputTokenDetails) MarshalJSON() ([]byte, error) {
	return marshalUsageObject(struct {
		Thinking *wireOptional[int] `json:"thinking_tokens,omitempty"`
	}{usageOptional(v.ThinkingTokens, v.nulls&1 != 0)}, v.extra)
}
func (v *OutputTokenDetails) UnmarshalJSON(data []byte) error {
	var wire struct {
		Thinking *int `json:"thinking_tokens"`
	}
	value, extra, err := decodeUsageObject(data, &wire, []string{"thinking_tokens"})
	if err != nil {
		return err
	}
	if err := validateUsageCounts(wire.Thinking); err != nil {
		return err
	}
	*v = OutputTokenDetails{ThinkingTokens: wire.Thinking, extra: extra, nulls: usageNullFields(value, "thinking_tokens")}
	return nil
}
func (v ServerToolUsage) MarshalJSON() ([]byte, error) {
	return marshalUsageObject(struct {
		Fetch  *wireOptional[int] `json:"web_fetch_requests,omitempty"`
		Search *wireOptional[int] `json:"web_search_requests,omitempty"`
	}{usageOptional(v.WebFetchRequests, v.nulls&1 != 0), usageOptional(v.WebSearchRequests, v.nulls&2 != 0)}, v.extra)
}
func (v *ServerToolUsage) UnmarshalJSON(data []byte) error {
	var wire struct {
		Fetch  *int `json:"web_fetch_requests"`
		Search *int `json:"web_search_requests"`
	}
	value, extra, err := decodeUsageObject(data, &wire, []string{"web_fetch_requests", "web_search_requests"})
	if err != nil {
		return err
	}
	if err := validateUsageCounts(wire.Fetch, wire.Search); err != nil {
		return err
	}
	*v = ServerToolUsage{WebFetchRequests: wire.Fetch, WebSearchRequests: wire.Search, extra: extra, nulls: usageNullFields(value, "web_fetch_requests", "web_search_requests")}
	return nil
}

// MapStrings projects credential-safe metadata without changing authoritative
// counts. Extension keys and nested values are mapped before JSON escaping.
func (u TokenUsage) MapStrings(mask func(string) string) TokenUsage {
	u = u.clone()
	if mask == nil {
		return u
	}
	if u.InferenceGeo != nil {
		*u.InferenceGeo = mask(*u.InferenceGeo)
	}
	if u.ServiceTier != nil {
		*u.ServiceTier = ServiceTier(mask(string(*u.ServiceTier)))
	}
	u.extra = u.extra.MapStrings(mask)
	if u.CacheCreation != nil {
		u.CacheCreation.extra = u.CacheCreation.extra.MapStrings(mask)
	}
	if u.OutputTokensDetails != nil {
		u.OutputTokensDetails.extra = u.OutputTokensDetails.extra.MapStrings(mask)
	}
	if u.ServerToolUse != nil {
		u.ServerToolUse.extra = u.ServerToolUse.extra.MapStrings(mask)
	}
	return u
}
