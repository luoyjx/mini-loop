package protocol_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

func unpublishedHistory() []protocol.Message {
	return []protocol.Message{
		{Role: protocol.RoleUser, Content: protocol.PlainContent("first request")},
		{Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewBashUse("answered", "echo first"))},
		{Role: protocol.RoleUser, Content: protocol.BlockContent(protocol.NewToolResult("answered", "first", false))},
		{Role: protocol.RoleAssistant, Content: protocol.BlockContent(
			protocol.NewTextBlock("two pending tools"),
			protocol.NewBashUse("pending-a", "echo a"),
			protocol.NewBashUse("pending-b", "echo b"),
		)},
	}
}

func unpublishedRequest(messages []protocol.Message) protocol.ModelRequest {
	return protocol.ModelRequest{Model: "model", MaxTokens: 64, Messages: messages, Purpose: protocol.PurposeAgentTurn}
}

func observeUnpublished(t *testing.T, messages []protocol.Message) protocol.UnpublishedToolBatch {
	t.Helper()
	batch, err := protocol.ObserveUnpublishedToolBatch(messages)
	if err != nil {
		t.Fatal(err)
	}
	return batch
}

func TestUnpublishedBatchKeepsPublicValidationStrict(t *testing.T) {
	history := unpublishedHistory()
	batch := observeUnpublished(t, history)
	request := unpublishedRequest(history)
	var missing *protocol.UnansweredToolUsesError
	if err := request.Validate(); !errors.As(err, &missing) {
		t.Fatalf("public validation must retain the pairing error: %v", err)
	}
	if err := request.ValidateForDispatch(); err == nil {
		t.Fatal("unregistered history passed dispatch validation")
	}
	registered, err := request.WithUnpublishedToolBatch(batch)
	if err != nil || !registered.HasUnpublishedToolBatch() {
		t.Fatal("complete final batch was not registered", err)
	}
	if request.HasUnpublishedToolBatch() {
		t.Fatal("registration mutated the original request")
	}
	if err := registered.Validate(); err == nil {
		t.Fatal("registration weakened public request validation")
	}
	if err := protocol.ValidateTranscript(registered.Messages); err == nil {
		t.Fatal("registration weakened public transcript validation")
	}
	registered.Messages = append(registered.Messages,
		protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent("next request")},
		protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewBashUse("next", "echo next"))},
		protocol.Message{Role: protocol.RoleUser, Content: protocol.BlockContent(protocol.NewToolResult("next", "next", false))},
	)
	if err := registered.ValidateForDispatch(); err != nil {
		t.Fatal("registered gap prevented a later fully paired batch", err)
	}
}

func TestUnpublishedBatchObservationRejectsOtherHistoryShapes(t *testing.T) {
	cases := []struct {
		name     string
		messages []protocol.Message
	}{
		{"empty", nil},
		{"ordinary", []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("hello")}}},
		{"answered", unpublishedHistory()[:3]},
		{"partial-results", append(unpublishedHistory(), protocol.Message{Role: protocol.RoleUser,
			Content: protocol.BlockContent(protocol.NewToolResult("pending-a", "a", false))})},
		{"earlier-gap", append(unpublishedHistory(),
			protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent("after gap")},
			protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewBashUse("later", "echo later"))})},
		{"user-tool-use", []protocol.Message{{Role: protocol.RoleUser, Content: protocol.BlockContent(protocol.NewBashUse("wrong-role", "echo x"))}}},
		{"empty-content", []protocol.Message{{Role: protocol.RoleAssistant}}},
		{"invalid-tool-input", []protocol.Message{{Role: protocol.RoleAssistant,
			Content: protocol.BlockContent(protocol.NewToolUse("invalid", protocol.ToolInput{}))}}},
	}
	for _, row := range cases {
		t.Run(row.name, func(t *testing.T) {
			if _, err := protocol.ObserveUnpublishedToolBatch(row.messages); err == nil {
				t.Fatal("history outside the final complete-gap contract was registered")
			}
		})
	}
}

func TestUnpublishedBatchBindsWholePrefixAndRejectsAdditionalGaps(t *testing.T) {
	history := unpublishedHistory()
	batch := observeUnpublished(t, history)
	cases := []struct {
		name   string
		change func([]protocol.Message) []protocol.Message
	}{
		{"earlier-text", func(messages []protocol.Message) []protocol.Message {
			messages[0].Content = protocol.PlainContent("rewritten request")
			return messages
		}},
		{"earlier-result", func(messages []protocol.Message) []protocol.Message {
			messages[2].Content = protocol.BlockContent(protocol.NewToolResult("answered", "rewritten result", false))
			return messages
		}},
		{"final-input", func(messages []protocol.Message) []protocol.Message {
			messages[3].Content = protocol.BlockContent(protocol.NewBashUse("pending-a", "echo changed"), protocol.NewBashUse("pending-b", "echo b"))
			return messages
		}},
		{"removed-prefix", func(messages []protocol.Message) []protocol.Message { return messages[1:] }},
		{"partial-results", func(messages []protocol.Message) []protocol.Message {
			return append(messages, protocol.Message{Role: protocol.RoleUser,
				Content: protocol.BlockContent(protocol.NewToolResult("pending-a", "a", false))})
		}},
		{"unregistered-gap", func(messages []protocol.Message) []protocol.Message {
			return append(messages,
				protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent("next request")},
				protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewBashUse("extra", "echo extra"))})
		}},
		{"unexpected-result", func(messages []protocol.Message) []protocol.Message {
			return append(messages, protocol.Message{Role: protocol.RoleUser,
				Content: protocol.BlockContent(protocol.NewToolResult("unoffered", "x", false))})
		}},
		{"invalid-later-role", func(messages []protocol.Message) []protocol.Message {
			return append(messages, protocol.Message{Role: protocol.Role("invalid"), Content: protocol.PlainContent("x")})
		}},
	}
	for _, row := range cases {
		t.Run(row.name, func(t *testing.T) {
			messages := row.change(unpublishedRequest(history).Clone().Messages)
			if err := batch.ValidateTranscript(messages); err == nil {
				t.Fatal("batch admitted modified or additionally malformed history")
			}
			if _, err := unpublishedRequest(messages).WithUnpublishedToolBatch(batch); err == nil {
				t.Fatal("request registration admitted modified or additionally malformed history")
			}
		})
	}
	if err := batch.ValidateTranscript(history); err != nil {
		t.Fatal("negative controls mutated the observed history", err)
	}
}

func TestUnpublishedRequestCloneDetachesPublicState(t *testing.T) {
	history := unpublishedHistory()
	request := unpublishedRequest(history)
	system := "stable system"
	request.System = &system
	registered, err := request.WithUnpublishedToolBatch(observeUnpublished(t, history))
	if err != nil {
		t.Fatal(err)
	}
	history[0].Content = protocol.PlainContent("changed original")
	system = "changed original system"
	if err := registered.ValidateForDispatch(); err != nil || *registered.System != "stable system" {
		t.Fatal("registration retained caller-owned mutable request state", err)
	}
	cloned := registered.Clone()
	if !cloned.HasUnpublishedToolBatch() || cloned.ValidateForDispatch() != nil {
		t.Fatal("clone lost its valid local observation")
	}
	cloned.Messages[0].Content = protocol.PlainContent("changed clone")
	*cloned.System = "changed clone system"
	if err := cloned.ValidateForDispatch(); err == nil {
		t.Fatal("clone's rewritten prefix still matched its observation")
	}
	if err := registered.ValidateForDispatch(); err != nil || *registered.System != "stable system" {
		t.Fatal("clone mutation reached the original", err)
	}
}

func TestUnpublishedObservationDoesNotSurviveJSON(t *testing.T) {
	history := unpublishedHistory()
	registered, err := unpublishedRequest(history).WithUnpublishedToolBatch(observeUnpublished(t, history))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(registered)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(bytes.ToLower(encoded), []byte("unpublished")) {
		t.Fatal("local observation escaped into request JSON")
	}
	var restored protocol.ModelRequest
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	restored.Purpose = protocol.PurposeAgentTurn
	if restored.HasUnpublishedToolBatch() || restored.ValidateForDispatch() == nil {
		t.Fatal("JSON restored the local gap exception")
	}
	for _, data := range [][]byte{encoded, []byte(`{}`)} {
		reused := registered.Clone()
		if err := json.Unmarshal(data, &reused); err != nil {
			t.Fatal(err)
		}
		if reused.HasUnpublishedToolBatch() || reused.ValidateForDispatch() == nil {
			t.Fatal("decoding into an existing request retained its observation")
		}
	}
}

func TestUnpublishedObservationPreservesRequestAdmission(t *testing.T) {
	history := unpublishedHistory()
	batch := observeUnpublished(t, history)
	var zero protocol.UnpublishedToolBatch
	if err := zero.ValidateTranscript(history); err == nil {
		t.Fatal("zero observation admitted an unpaired transcript")
	}
	if _, err := unpublishedRequest(history).WithUnpublishedToolBatch(zero); err == nil {
		t.Fatal("zero observation registered")
	}
	for _, purpose := range []protocol.RequestPurpose{
		protocol.PurposeCompaction, protocol.PurposeDecision, protocol.PurposeMemorySelection,
		protocol.PurposeMemoryConsolidation, protocol.PurposePersonalSkillPreview, "",
	} {
		request := unpublishedRequest(history)
		request.Purpose = purpose
		if _, err := request.WithUnpublishedToolBatch(batch); err == nil {
			t.Fatalf("observation admitted non-agent purpose %q", purpose)
		}
	}
	request := unpublishedRequest(history)
	request.MaxTokens = 0
	if _, err := request.WithUnpublishedToolBatch(batch); err == nil {
		t.Fatal("observation bypassed positive token admission")
	}
}
