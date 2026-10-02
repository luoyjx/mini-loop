package protocol

import (
	"encoding/json"
	"testing"
)

func TestCacheAnnotationsRefuseInvalidTargetsAndDetachWire(t *testing.T) {
	system := "stable"
	request := ModelRequest{Model: "model", MaxTokens: 100, System: &system, Messages: []Message{{Role: RoleUser, Content: BlockContent(NewTextBlock("one"))}, {Role: RoleAssistant, Content: BlockContent(NewTextBlock("two"))}}, Purpose: PurposeAgentTurn}
	control := CacheControl{Type: CacheEphemeral}
	for _, annotations := range []CacheAnnotations{
		{Messages: []CacheBreakpoint{{MessageIndex: -1, BlockIndex: 0, Control: control}}},
		{Messages: []CacheBreakpoint{{MessageIndex: 0, BlockIndex: 1, Control: control}}},
		{Messages: []CacheBreakpoint{{MessageIndex: 1, BlockIndex: 0, Control: control}}},
		{Messages: []CacheBreakpoint{{MessageIndex: 0, BlockIndex: 0, Control: CacheControl{Type: "unknown"}}}},
		{Messages: []CacheBreakpoint{{MessageIndex: 0, BlockIndex: 0, Control: control}, {MessageIndex: 0, BlockIndex: 0, Control: control}}},
	} {
		request.Cache = annotations
		if request.Validate() == nil {
			t.Fatal("invalid cache targets accepted")
		}
		if _, err := request.Wire(); err == nil {
			t.Fatal("wire projected invalid cache targets")
		}
	}
	request.Cache = CacheAnnotations{System: &control}
	wire, err := request.Wire()
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(wire)
	request.Cache.System.TTL = "1h"
	after, _ := json.Marshal(wire)
	if string(before) != string(after) {
		t.Fatal("wire shares mutable annotation pointer")
	}
	copy := request.Clone()
	copy.Cache.System.TTL = "changed"
	if request.Cache.System.TTL != "1h" {
		t.Fatal("request clone shares cache control")
	}
	request.System = nil
	if request.Validate() == nil {
		t.Fatal("breakpoint without system accepted")
	}
}
