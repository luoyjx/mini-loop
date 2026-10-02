package agent

import "github.com/luoyjx/mini-loop/go/protocol"

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
