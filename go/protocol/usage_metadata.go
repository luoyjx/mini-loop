package protocol

import (
	"encoding/json"
	"errors"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// Named nested SDK usage. Counts retain named scalar/structured observations.
type CacheCreationUsage struct {
	Ephemeral1hInputTokens *UsageCount `json:"ephemeral_1h_input_tokens"`
	Ephemeral5mInputTokens *UsageCount `json:"ephemeral_5m_input_tokens"`
	extra                  jsonvalue.Value
	nulls                  usageNulls
	observation            *UsageMetadataObservation
}
type OutputTokenDetails struct {
	ThinkingTokens *UsageCount `json:"thinking_tokens"`
	extra          jsonvalue.Value
	nulls          usageNulls
	observation    *UsageMetadataObservation
}
type ServerToolUsage struct {
	WebFetchRequests  *UsageCount `json:"web_fetch_requests"`
	WebSearchRequests *UsageCount `json:"web_search_requests"`
	extra             jsonvalue.Value
	nulls             usageNulls
	observation       *UsageMetadataObservation
}

// TokenUsage keeps absent/null/value for optional fields. Closed extension members
// are immutable; recognized fields always enter named types before runtime use.
type TokenUsage struct {
	InputTokens              UsageCount          `json:"input_tokens"`
	OutputTokens             UsageCount          `json:"output_tokens"`
	CacheReadInputTokens     *UsageCount         `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens *UsageCount         `json:"cache_creation_input_tokens,omitempty"`
	ServiceTier              *ServiceTier        `json:"service_tier,omitempty"`
	CacheCreation            *CacheCreationUsage `json:"cache_creation,omitempty"`
	InferenceGeo             *string             `json:"inference_geo,omitempty"`
	OutputTokensDetails      *OutputTokenDetails `json:"output_tokens_details,omitempty"`
	ServerToolUse            *ServerToolUsage    `json:"server_tool_use,omitempty"`
	nulls                    usageNulls
	extra                    jsonvalue.Value
	tierObservation          *UsageMetadataObservation
	geoObservation           *UsageMetadataObservation
	whole                    *wholeUsageObservation
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

func (u TokenUsage) countPointers() []*UsageCount {
	counts := []*UsageCount{&u.InputTokens, &u.OutputTokens, u.CacheReadInputTokens, u.CacheCreationInputTokens}
	if u.CacheCreation != nil {
		counts = append(counts, u.CacheCreation.Ephemeral1hInputTokens, u.CacheCreation.Ephemeral5mInputTokens)
	}
	if u.OutputTokensDetails != nil {
		counts = append(counts, u.OutputTokensDetails.ThinkingTokens)
	}
	if u.ServerToolUse != nil {
		counts = append(counts, u.ServerToolUse.WebFetchRequests, u.ServerToolUse.WebSearchRequests)
	}
	return counts
}
func (u TokenUsage) Validate() error {
	if u.whole != nil {
		if u.InputTokens.Kind() != UsageNull || u.OutputTokens.Kind() != UsageNull || u.CacheReadInputTokens != nil || u.CacheCreationInputTokens != nil {
			return errors.New("whole usage observation cannot carry counts")
		}
		return nil
	}
	return validateUsageCounts(u.countPointers()...)
}
func validateUsageCounts(counts ...*UsageCount) error {
	for _, count := range counts {
		if count != nil {
			if err := count.Validate(); err != nil {
				return err
			}
		}
	}
	return nil
}
func (u TokenUsage) clone() TokenUsage {
	u.whole = copyMetadataPointer(u.whole)
	u.CacheReadInputTokens = copyMetadataPointer(u.CacheReadInputTokens)
	u.CacheCreationInputTokens = copyMetadataPointer(u.CacheCreationInputTokens)
	u.ServiceTier = copyMetadataPointer(u.ServiceTier)
	u.tierObservation = copyMetadataPointer(u.tierObservation)
	u.geoObservation = copyMetadataPointer(u.geoObservation)
	u.CacheCreation = copyMetadataPointer(u.CacheCreation)
	u.InferenceGeo = copyMetadataPointer(u.InferenceGeo)
	u.OutputTokensDetails = copyMetadataPointer(u.OutputTokensDetails)
	u.ServerToolUse = copyMetadataPointer(u.ServerToolUse)
	if u.CacheCreation != nil {
		u.CacheCreation.observation = copyMetadataPointer(u.CacheCreation.observation)
		u.CacheCreation.Ephemeral1hInputTokens = copyMetadataPointer(u.CacheCreation.Ephemeral1hInputTokens)
		u.CacheCreation.Ephemeral5mInputTokens = copyMetadataPointer(u.CacheCreation.Ephemeral5mInputTokens)
	}
	if u.OutputTokensDetails != nil {
		u.OutputTokensDetails.observation = copyMetadataPointer(u.OutputTokensDetails.observation)
		u.OutputTokensDetails.ThinkingTokens = copyMetadataPointer(u.OutputTokensDetails.ThinkingTokens)
	}
	if u.ServerToolUse != nil {
		u.ServerToolUse.observation = copyMetadataPointer(u.ServerToolUse.observation)
		u.ServerToolUse.WebFetchRequests = copyMetadataPointer(u.ServerToolUse.WebFetchRequests)
		u.ServerToolUse.WebSearchRequests = copyMetadataPointer(u.ServerToolUse.WebSearchRequests)
	}

	return u
}

// WithSDKDefaults materializes the seven nullable SDK fields. Native/fake usage
// constructors retain their original sparse shape until this explicit selection.
func (u TokenUsage) WithSDKDefaults() TokenUsage {
	if u.whole != nil {
		return u.clone()
	}
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
	if u.ServiceTier == nil && u.tierObservation == nil {
		u.nulls |= nullTier
	} else {
		u.nulls &^= nullTier
	}
	if u.CacheCreation == nil {
		u.nulls |= nullCacheCreation
	} else {
		u.nulls &^= nullCacheCreation
	}
	if u.InferenceGeo == nil && u.geoObservation == nil {
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
func (u TokenUsage) MarshalJSON() ([]byte, error) {
	if u.whole != nil {
		if err := u.Validate(); err != nil {
			return nil, err
		}
		if u.whole.kind == wholeUsageNull {
			return []byte("null"), nil
		}
		return json.Marshal(struct {
			Value string `json:"value"`
		}{u.whole.text})
	}
	wire := struct {
		Input         UsageCount                              `json:"input_tokens"`
		Output        UsageCount                              `json:"output_tokens"`
		Read          *wireOptional[UsageCount]               `json:"cache_read_input_tokens,omitempty"`
		CreationInput *wireOptional[UsageCount]               `json:"cache_creation_input_tokens,omitempty"`
		Tier          *wireOptional[UsageMetadataObservation] `json:"service_tier,omitempty"`
		Creation      *wireOptional[CacheCreationUsage]       `json:"cache_creation,omitempty"`
		Geo           *wireOptional[UsageMetadataObservation] `json:"inference_geo,omitempty"`
		Details       *wireOptional[OutputTokenDetails]       `json:"output_tokens_details,omitempty"`
		Server        *wireOptional[ServerToolUsage]          `json:"server_tool_use,omitempty"`
	}{u.InputTokens, u.OutputTokens, optionalWireValue(u.CacheReadInputTokens, u.nulls&nullCacheRead != 0), optionalWireValue(u.CacheCreationInputTokens, u.nulls&nullCacheInput != 0), optionalWireValue(u.sdkTierObservation(), u.nulls&nullTier != 0), optionalWireValue(u.CacheCreation, u.nulls&nullCacheCreation != 0), optionalWireValue(u.sdkGeoObservation(), u.nulls&nullGeo != 0), optionalWireValue(u.OutputTokensDetails, u.nulls&nullOutputDetails != 0), optionalWireValue(u.ServerToolUse, u.nulls&nullServerTool != 0)}
	return marshalMetadataObject(wire, u.extra)
}
func (u *TokenUsage) UnmarshalJSON(data []byte) error {
	if len(data) > MaxWireBytes {
		return errors.New("usage exceeds protocol limit")
	}
	projection, err := jsonvalue.Decode(string(data))
	if err != nil {
		return err
	}
	if projection.Kind() == jsonvalue.Null {
		*u = newWholeUsage(wholeUsageObservation{kind: wholeUsageNull})
		return nil
	}
	if projection.Kind() == jsonvalue.Object && len(projection.Keys()) == 1 {
		if value, ok := projection.Lookup("value"); ok {
			if text, ok := value.Text(); ok {
				*u = newWholeUsage(wholeUsageObservation{kind: wholeUsageArchivedText, text: text})
				return nil
			}
		}
	}
	var wire struct {
		Input         UsageCount                `json:"input_tokens"`
		Output        UsageCount                `json:"output_tokens"`
		Read          *UsageCount               `json:"cache_read_input_tokens"`
		CreationInput *UsageCount               `json:"cache_creation_input_tokens"`
		Tier          *UsageMetadataObservation `json:"service_tier"`
		Creation      *CacheCreationUsage       `json:"cache_creation"`
		Geo           *UsageMetadataObservation `json:"inference_geo"`
		Details       *OutputTokenDetails       `json:"output_tokens_details"`
		Server        *ServerToolUsage          `json:"server_tool_use"`
	}
	keys := []string{"input_tokens", "output_tokens", "cache_read_input_tokens", "cache_creation_input_tokens", "service_tier", "cache_creation", "inference_geo", "output_tokens_details", "server_tool_use"}
	value, extra, err := decodeMetadataObject(data, &wire, keys, true)
	if err != nil {
		return err
	}
	_, inputPresent := value.Lookup("input_tokens")
	_, outputPresent := value.Lookup("output_tokens")
	if !inputPresent || !outputPresent {
		return errors.New("usage requires input_tokens and output_tokens")
	}
	next := TokenUsage{InputTokens: wire.Input, OutputTokens: wire.Output, CacheReadInputTokens: wire.Read, CacheCreationInputTokens: wire.CreationInput, CacheCreation: wire.Creation, OutputTokensDetails: wire.Details, ServerToolUse: wire.Server, extra: extra}
	if wire.Tier != nil {
		if wire.Tier.kind == UsageMetadataText {
			value := ServiceTier(wire.Tier.text)
			next.ServiceTier = &value
		} else {
			next.tierObservation = wire.Tier
		}
	}
	if wire.Geo != nil {
		if wire.Geo.kind == UsageMetadataText {
			value := wire.Geo.text
			next.InferenceGeo = &value
		} else {
			next.geoObservation = wire.Geo
		}
	}
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
	if v.observation != nil {
		return v.observation.MarshalJSON()
	}
	return marshalMetadataObject(struct {
		Hour   *wireOptional[UsageCount] `json:"ephemeral_1h_input_tokens,omitempty"`
		Minute *wireOptional[UsageCount] `json:"ephemeral_5m_input_tokens,omitempty"`
	}{optionalWireValue(v.Ephemeral1hInputTokens, v.nulls&1 != 0), optionalWireValue(v.Ephemeral5mInputTokens, v.nulls&2 != 0)}, v.extra)
}
func (v *CacheCreationUsage) UnmarshalJSON(data []byte) error {
	observation, err := nonObjectUsageMetadata(data)
	if err != nil {
		return err
	}
	if observation != nil {
		*v = CacheCreationUsage{observation: observation}
		return nil
	}
	var wire struct {
		Hour   *UsageCount `json:"ephemeral_1h_input_tokens"`
		Minute *UsageCount `json:"ephemeral_5m_input_tokens"`
	}
	value, extra, err := decodeMetadataObject(data, &wire, []string{"ephemeral_1h_input_tokens", "ephemeral_5m_input_tokens"}, true)
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
	if v.observation != nil {
		return v.observation.MarshalJSON()
	}
	return marshalMetadataObject(struct {
		Thinking *wireOptional[UsageCount] `json:"thinking_tokens,omitempty"`
	}{optionalWireValue(v.ThinkingTokens, v.nulls&1 != 0)}, v.extra)
}
func (v *OutputTokenDetails) UnmarshalJSON(data []byte) error {
	observation, err := nonObjectUsageMetadata(data)
	if err != nil {
		return err
	}
	if observation != nil {
		*v = OutputTokenDetails{observation: observation}
		return nil
	}
	var wire struct {
		Thinking *UsageCount `json:"thinking_tokens"`
	}
	value, extra, err := decodeMetadataObject(data, &wire, []string{"thinking_tokens"}, true)
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
	if v.observation != nil {
		return v.observation.MarshalJSON()
	}
	return marshalMetadataObject(struct {
		Fetch  *wireOptional[UsageCount] `json:"web_fetch_requests,omitempty"`
		Search *wireOptional[UsageCount] `json:"web_search_requests,omitempty"`
	}{optionalWireValue(v.WebFetchRequests, v.nulls&1 != 0), optionalWireValue(v.WebSearchRequests, v.nulls&2 != 0)}, v.extra)
}
func (v *ServerToolUsage) UnmarshalJSON(data []byte) error {
	observation, err := nonObjectUsageMetadata(data)
	if err != nil {
		return err
	}
	if observation != nil {
		*v = ServerToolUsage{observation: observation}
		return nil
	}
	var wire struct {
		Fetch  *UsageCount `json:"web_fetch_requests"`
		Search *UsageCount `json:"web_search_requests"`
	}
	value, extra, err := decodeMetadataObject(data, &wire, []string{"web_fetch_requests", "web_search_requests"}, true)
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
	if u.whole != nil {
		u.whole.text = mask(u.whole.text)
		return u
	}
	if u.InferenceGeo != nil {
		*u.InferenceGeo = mask(*u.InferenceGeo)
	}
	if u.ServiceTier != nil {
		*u.ServiceTier = ServiceTier(mask(string(*u.ServiceTier)))
	}
	u.InputTokens = u.InputTokens.MapStrings(mask)
	u.OutputTokens = u.OutputTokens.MapStrings(mask)
	for _, count := range u.countPointers()[2:] {
		if count != nil {
			*count = count.MapStrings(mask)
		}
	}
	u.tierObservation = mapUsageMetadata(u.tierObservation, mask)
	u.geoObservation = mapUsageMetadata(u.geoObservation, mask)
	u.extra = u.extra.MapStrings(mask)
	if u.CacheCreation != nil {
		u.CacheCreation.extra = u.CacheCreation.extra.MapStrings(mask)
		u.CacheCreation.observation = mapUsageMetadata(u.CacheCreation.observation, mask)
	}
	if u.OutputTokensDetails != nil {
		u.OutputTokensDetails.extra = u.OutputTokensDetails.extra.MapStrings(mask)
		u.OutputTokensDetails.observation = mapUsageMetadata(u.OutputTokensDetails.observation, mask)
	}
	if u.ServerToolUse != nil {
		u.ServerToolUse.extra = u.ServerToolUse.extra.MapStrings(mask)
		u.ServerToolUse.observation = mapUsageMetadata(u.ServerToolUse.observation, mask)
	}
	return u
}

// WithSDKDirectCounts normalizes optional SDK integer fields while keeping the
// required direct fields as actual observed values.
func (u TokenUsage) WithSDKDirectCounts() TokenUsage {
	u = u.clone()
	if u.CacheCreation != nil {
		u.CacheCreation.observation = sdkMetadataArrayDefaults(u.CacheCreation.observation, "ephemeral_1h_input_tokens", "ephemeral_5m_input_tokens")
	}
	if u.OutputTokensDetails != nil {
		u.OutputTokensDetails.observation = sdkMetadataArrayDefaults(u.OutputTokensDetails.observation, "thinking_tokens")
	}
	if u.ServerToolUse != nil {
		u.ServerToolUse.observation = sdkMetadataArrayDefaults(u.ServerToolUse.observation, "web_fetch_requests", "web_search_requests")
	}
	for _, count := range []*UsageCount{u.CacheReadInputTokens, u.CacheCreationInputTokens} {
		if count != nil {
			if next, ok := count.sdkInteger(); ok {
				*count = next
			}
		}
	}
	if u.CacheCreation != nil && u.CacheCreation.observation == nil {
		normalizeSDKRequiredCounts(u.CacheCreation.Ephemeral1hInputTokens, u.CacheCreation.Ephemeral5mInputTokens)
	}
	if u.OutputTokensDetails != nil && u.OutputTokensDetails.observation == nil {
		normalizeSDKRequiredCounts(u.OutputTokensDetails.ThinkingTokens)
	}
	if u.ServerToolUse != nil && u.ServerToolUse.observation == nil {
		normalizeSDKRequiredCounts(u.ServerToolUse.WebFetchRequests, u.ServerToolUse.WebSearchRequests)
	}
	return u.WithSDKDefaults()
}

// WithSDKStreamCounts performs the captured full Usage integer validation before
// fallback construction. If a field cannot validate, required values stay raw.
func (u TokenUsage) WithSDKStreamCounts() TokenUsage {
	u = u.clone()
	valid := u.sdkMetadataValid()
	for _, count := range u.countPointers() {
		if count != nil {
			if _, ok := count.sdkInteger(); !ok {
				valid = false
			}
		}
	}
	if valid {
		u.InputTokens, _ = u.InputTokens.sdkInteger()
		u.OutputTokens, _ = u.OutputTokens.sdkInteger()
	}
	return u.WithSDKDirectCounts()
}

func (u TokenUsage) sdkTierObservation() *UsageMetadataObservation {
	if u.ServiceTier != nil {
		return &UsageMetadataObservation{kind: UsageMetadataText, text: string(*u.ServiceTier)}
	}
	return u.tierObservation
}

func (u TokenUsage) sdkGeoObservation() *UsageMetadataObservation {
	if u.InferenceGeo != nil {
		return &UsageMetadataObservation{kind: UsageMetadataText, text: *u.InferenceGeo}
	}
	return u.geoObservation
}

func (u TokenUsage) sdkMetadataValid() bool {
	if tier := u.sdkTierObservation(); tier != nil {
		if tier.kind != UsageMetadataText || (tier.text != "standard" && tier.text != "priority" && tier.text != "batch") {
			return false
		}
	}
	if geo := u.sdkGeoObservation(); geo != nil && geo.kind != UsageMetadataText {
		return false
	}
	if v := u.CacheCreation; v != nil && !validSDKMetadataModel(v.observation, v.Ephemeral1hInputTokens, v.Ephemeral5mInputTokens) {
		return false
	}
	if v := u.OutputTokensDetails; v != nil && !validSDKMetadataModel(v.observation, v.ThinkingTokens) {
		return false
	}
	if v := u.ServerToolUse; v != nil && !validSDKMetadataModel(v.observation, v.WebFetchRequests, v.WebSearchRequests) {
		return false
	}
	return true
}
