package protocol

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"
)

func TestStopReasonsTrackPythonManifest(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-contract-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		KnownStopReasons []StopReason `json:"known_stop_reasons"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	known := []StopReason{StopEndTurn, StopToolUse, StopMaxTokens, StopSequence, StopPauseTurn, StopRefusal}
	sort.Slice(known, func(i, j int) bool { return known[i] < known[j] })
	if !reflect.DeepEqual(manifest.KnownStopReasons, known) {
		t.Fatalf("Python stop reasons=%v, Go=%v", manifest.KnownStopReasons, known)
	}
	for _, reason := range known {
		if !reason.Known() {
			t.Fatalf("known Python stop reason %q rejected", reason)
		}
	}
	if StopReason("new_provider_reason").Known() || !StopPauseTurn.Resumable() || StopMaxTokens.Resumable() {
		t.Fatal("stop reason classification changed")
	}
}

func TestRealPythonFakeReplyFixturesDecode(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-fake-replies.json")
	if err != nil {
		t.Fatal(err)
	}
	var replies []ModelReply
	if err := json.Unmarshal(data, &replies); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 3 || replies[0].StopReason != StopToolUse || replies[1].StopReason != StopEndTurn || replies[2].StopReason != StopRefusal {
		t.Fatalf("Python fake response shape drifted: %+v", replies)
	}
	if replies[0].Usage.InputTokens != 13 || replies[0].Usage.OutputTokens != 2 {
		t.Fatalf("Python usage fields were lost: %+v", replies[0].Usage)
	}
	use, ok := replies[0].Content[1].ToolUse()
	if !ok || use.Caller != nil {
		t.Fatalf("Python fake's null caller changed: %+v", use)
	}
	input, ok := use.Input.Bash()
	if !ok || input.Command != "echo handled: go" {
		t.Fatalf("Python fake tool input changed: %+v", use)
	}
	if replies[2].Content == nil || len(replies[2].Content) != 0 {
		t.Fatal("empty Python refusal content was lost")
	}
}

func TestModelReplyRoundTripPreservesTypedUsageAndUnknownStop(t *testing.T) {
	const wire = `{"id":"msg_1","type":"message","role":"assistant","model":"fake-model","content":[{"type":"text","text":"hello"}],"stop_reason":"new_provider_reason","stop_sequence":null,"usage":{"input_tokens":11,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"service_tier":"standard"}}`
	var reply ModelReply
	if err := json.Unmarshal([]byte(wire), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.StopReason.Known() || reply.Usage.CacheReadInputTokens == nil || *reply.Usage.CacheReadInputTokens != 0 {
		t.Fatalf("reply lost unknown reason or explicit cache zero: %+v", reply)
	}
	clone := reply.Clone()
	*clone.Usage.CacheReadInputTokens = 10
	if *reply.Usage.CacheReadInputTokens != 0 {
		t.Fatal("clone retained a caller-mutable usage pointer")
	}
	encoded, err := json.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	var restored ModelReply
	if err := json.Unmarshal(encoded, &restored); err != nil || restored.StopReason != reply.StopReason {
		t.Fatalf("model reply roundtrip: %v, %+v", err, restored)
	}
}

func TestModelReplyAllowsEmptyRefusalAndRejectsInvalidWire(t *testing.T) {
	const refusal = `{"id":"msg_r","type":"message","role":"assistant","model":"fake-model","content":[],"stop_reason":"refusal","usage":{"input_tokens":0,"output_tokens":0}}`
	var reply ModelReply
	if err := json.Unmarshal([]byte(refusal), &reply); err != nil || len(reply.Content) != 0 || reply.Content == nil {
		t.Fatalf("empty refusal was lost: %v, %+v", err, reply)
	}
	bad := []string{
		`{"id":"m","type":"message","role":"assistant","model":"x","content":[],"stop_reason":"end_turn"}`,
		`{"id":"m","type":"message","role":"user","model":"x","content":[],"stop_reason":"end_turn","usage":{"input_tokens":0,"output_tokens":0}}`,
		`{"id":"m","type":"message","role":"assistant","model":"x","content":[],"stop_reason":"end_turn","usage":{"input_tokens":-1,"output_tokens":0}}`,
		`{"id":"m","type":"message","role":"assistant","model":"x","content":[],"stop_reason":"end_turn","usage":{"input_tokens":0,"output_tokens":0},"unknown":true}`,
		`null`,
	}
	for _, body := range bad {
		if err := json.Unmarshal([]byte(body), &reply); err == nil {
			t.Errorf("accepted invalid model reply: %s", body)
		}
	}
}
