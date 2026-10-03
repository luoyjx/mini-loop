package agent

import (
	"github.com/luoyjx/mini-loop/go/protocol"
	"sort"
)

// TextMasker acts only on projections/results. Executed calls and live model
// request arguments retain their original concrete values.
type TextMasker interface{ MaskText(string) string }

func maskedText(masker TextMasker, value string) string {
	if masker == nil {
		return value
	}
	return masker.MaskText(value)
}
func maskedEvent(masker TextMasker, event SessionEvent) SessionEvent {
	event = event.clone()
	if masker == nil {
		return event
	}
	mask := masker.MaskText
	event.sessionForked.Child = SessionID(mask(string(event.sessionForked.Child)))
	event.runError.detail = mask(event.runError.detail)
	event.recovery.Error = mask(event.recovery.Error)
	event.assistantText.Text = mask(event.assistantText.Text)
	event.delta.Text = mask(event.delta.Text)
	event.delta.StreamID = StreamID(mask(string(event.delta.StreamID)))
	event.streamStart.StreamID = StreamID(mask(string(event.streamStart.StreamID)))
	event.assistantText.StreamID = StreamID(mask(string(event.assistantText.StreamID)))
	event.done.Text = mask(event.done.Text)
	event.steeringDelivered.Text = mask(event.steeringDelivered.Text)
	event.postureUpdate.Text = mask(event.postureUpdate.Text)
	event.cancelled.Reason = mask(event.cancelled.Reason)
	for i := range event.cancelled.RepairedToolUses {
		event.cancelled.RepairedToolUses[i] = mask(event.cancelled.RepairedToolUses[i])
	}
	event.activity.Title = mask(event.activity.Title)
	event.activity.ID = ActivityID(mask(string(event.activity.ID)))
	event.modelStart.Model = mask(event.modelStart.Model)
	maskStringPointer(mask, &event.modelStart.ToolCatalogFingerprint)
	maskStringPointer(mask, &event.modelStart.SystemHash)
	maskStringPointer(mask, &event.modelStart.CapabilityFingerprint)
	event.modelStart.SpanID = SpanID(mask(string(event.modelStart.SpanID)))
	event.modelEnd.SpanID = SpanID(mask(string(event.modelEnd.SpanID)))
	maskStringPointer(mask, &event.modelEnd.Error)
	maskStringPointer(mask, &event.modelEnd.ServedModel)
	maskStringPointer(mask, &event.modelEnd.ToolCatalogFingerprint)
	if event.modelEnd.StopReason != nil {
		v := protocol.StopReason(mask(string(*event.modelEnd.StopReason)))
		event.modelEnd.StopReason = &v
	}
	if event.modelEnd.TokenMeter != nil {
		maskStringPointer(mask, &event.modelEnd.TokenMeter.AnchorEnvelope)
	}
	event.toolUse.Name = protocol.ToolName(mask(string(event.toolUse.Name)))
	event.toolUse.ID = mask(event.toolUse.ID)
	event.toolUse.SpanID = SpanID(mask(string(event.toolUse.SpanID)))
	event.toolUse.ParentSpanID = SpanID(mask(string(event.toolUse.ParentSpanID)))
	event.toolUse.ActionID = ActionID(mask(string(event.toolUse.ActionID)))
	event.toolUse.ActivityID = ActivityID(mask(string(event.toolUse.ActivityID)))
	if event.kind == EventToolUse {
		event.toolUse.Input = protocol.MapToolInputStrings(event.toolUse.Input, mask)
		event.toolUse.Display = ToolLabel(event.toolUse.Input)
	}
	event.toolResult.Name = protocol.ToolName(mask(string(event.toolResult.Name)))
	event.toolResult.Output, event.toolResult.ID = mask(event.toolResult.Output), mask(event.toolResult.ID)
	event.toolResult.SpanID = SpanID(mask(string(event.toolResult.SpanID)))
	event.toolResult.ParentSpanID = SpanID(mask(string(event.toolResult.ParentSpanID)))
	event.toolResult.ActionID = ActionID(mask(string(event.toolResult.ActionID)))
	event.toolCatalog.Fingerprint = mask(event.toolCatalog.Fingerprint)
	for i := range event.toolCatalog.Schemas {
		v := &event.toolCatalog.Schemas[i]
		v.Name = protocol.ToolName(mask(string(v.Name)))
		v.Description = mask(v.Description)
		v.InputSchema = maskedSchema(v.InputSchema, mask)
	}
	event.systemPrompt.Hash, event.systemPrompt.System = mask(event.systemPrompt.Hash), mask(event.systemPrompt.System)
	if event.systemPrompt.Cache != nil {
		event.systemPrompt.Cache.Type = protocol.CacheType(mask(string(event.systemPrompt.Cache.Type)))
		event.systemPrompt.Cache.TTL = protocol.CacheTTL(mask(string(event.systemPrompt.Cache.TTL)))
	}
	event.capabilityPlan.Fingerprint = mask(event.capabilityPlan.Fingerprint)
	event.capabilityPlan.CatalogFingerprint = mask(event.capabilityPlan.CatalogFingerprint)
	event.capabilityPlan.Sandbox = mask(event.capabilityPlan.Sandbox)
	event.reconcile.Name = protocol.ToolName(mask(string(event.reconcile.Name)))
	event.reconcile.ActionID = ActionID(mask(string(event.reconcile.ActionID)))
	event.stuck.signal.Pattern = StuckPattern(mask(string(event.stuck.signal.Pattern)))
	event.stuck.signal.Detail = mask(event.stuck.signal.Detail)
	event.stuck.signal.Advice = mask(event.stuck.signal.Advice)
	if event.stuck.signal.Tool != nil {
		name := protocol.ToolName(mask(string(*event.stuck.signal.Tool)))
		event.stuck.signal.Tool = &name
	}
	event.stop.detail = mask(event.stop.detail)
	event.stop.reason = protocol.StopReason(mask(string(event.stop.reason)))
	for i := range event.todos {
		event.todos[i].Content = mask(event.todos[i].Content)
		event.todos[i].ActiveForm = mask(event.todos[i].ActiveForm)
	}
	event.compact.failure = mask(event.compact.failure)
	event.compact.summary.Transcript = mask(event.compact.summary.Transcript)
	event.compact.summary.Model = mask(event.compact.summary.Model)
	event.subagent.prompt = mask(event.subagent.prompt)
	event.subagent.summary = mask(event.subagent.summary)
	event.subagent.role = AgentRole(mask(string(event.subagent.role)))
	approval := &event.approval
	approval.snapshot.ApprovalID = ApprovalID(mask(string(approval.snapshot.ApprovalID)))
	approval.snapshot.SessionID = SessionID(mask(string(approval.snapshot.SessionID)))
	approval.snapshot.ToolUseID = mask(approval.snapshot.ToolUseID)
	approval.snapshot.Rule = mask(approval.snapshot.Rule)
	approval.snapshot.Message = mask(approval.snapshot.Message)
	approval.snapshot.InputPreview = mask(approval.snapshot.InputPreview)
	approval.snapshot.GrantCandidate = maskedGrant(masker, approval.snapshot.GrantCandidate)
	approval.id = ApprovalID(mask(string(approval.id)))
	approval.rule = mask(approval.rule)
	approval.grant = maskedGrant(masker, approval.grant)
	return event
}

func maskStringPointer(mask func(string) string, p **string) {
	if *p != nil {
		v := mask(**p)
		*p = &v
	}
}
func maskedSchema(schema protocol.InputSchema, mask func(string) string) protocol.InputSchema {
	schema = schema.Clone()
	schema.Description = mask(schema.Description)
	for i := range schema.Enum {
		schema.Enum[i] = mask(schema.Enum[i])
	}
	for i := range schema.Required {
		schema.Required[i] = mask(schema.Required[i])
	}
	if schema.Items != nil {
		v := maskedSchema(*schema.Items, mask)
		schema.Items = &v
	}
	if schema.Properties != nil {
		v := make(protocol.SchemaProperties, len(*schema.Properties))
		keys := make([]string, 0, len(*schema.Properties))
		for key := range *schema.Properties {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			v[mask(key)] = maskedSchema((*schema.Properties)[key], mask)
		}
		schema.Properties = &v
	}
	return schema
}
func maskedGrant(masker TextMasker, grant GrantCandidate) GrantCandidate {
	grant = grant.clone()
	for i, value := range grant.prefix {
		grant.prefix[i] = maskedText(masker, value)
	}
	return grant
}
func maskedScope(masker TextMasker, scope EventScope) EventScope {
	scope = scope.clone()
	if masker == nil {
		return scope
	}
	mask := masker.MaskText
	scope.Label = mask(scope.Label)
	run := &scope.RunContext
	run.messageID = MessageID(mask(string(run.messageID)))
	run.origin = mask(run.origin)
	run.channel = mask(run.channel)
	run.stampedBy = mask(run.stampedBy)
	if run.actorID != nil {
		value := ActorID(mask(string(*run.actorID)))
		run.actorID = &value
	}
	if run.parentMessageID != nil {
		value := MessageID(mask(string(*run.parentMessageID)))
		run.parentMessageID = &value
	}
	if run.delegatedBy != nil {
		value := mask(*run.delegatedBy)
		run.delegatedBy = &value
	}
	for i, capability := range run.approved {
		run.approved[i] = RunCapability(mask(string(capability)))
	}
	return scope
}
