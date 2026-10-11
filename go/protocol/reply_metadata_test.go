package protocol

import (
	"encoding/json"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestReplyMetadataAccessorsAndCloneAreDetached(t *testing.T) {
	var container ContainerMetadata
	if err := json.Unmarshal([]byte(`{"id":"original","expires_at":"2030-01-02T03:04:05Z","future":{"items":[1,2]}}`), &container); err != nil {
		t.Fatal(err)
	}
	var refusal StopDetailsMetadata
	if err := json.Unmarshal([]byte(`{"type":"refusal","category":"cyber","explanation":"original","future":["original"]}`), &refusal); err != nil {
		t.Fatal(err)
	}
	reply := ModelReply{Container: container, StopDetails: refusal}
	clone := reply.Clone()
	body, ok := clone.Container.Container()
	if !ok {
		t.Fatal("structured container absent")
	}
	body.ID = MetadataText("changed")
	ext, _ := body.Extensions().Lookup("future")
	items, _ := ext.Lookup("items")
	detached, _ := items.Array()
	detached[0] = jsonvalue.IntegerValue(999)
	detail, ok := clone.StopDetails.Refusal()
	if !ok {
		t.Fatal("structured refusal absent")
	}
	detail.Explanation = MetadataText("changed")
	original, _ := reply.Container.Container()
	id, ok := original.ID.Text()
	if !ok || id != "original" {
		t.Fatal("container accessor shared mutation")
	}
	encoded, _ := json.Marshal(reply.Container)
	if string(encoded) != `{"id":"original","expires_at":"2030-01-02T03:04:05Z","future":{"items":[1,2]}}` {
		t.Fatal(string(encoded))
	}
	encoded, _ = json.Marshal(reply.StopDetails)
	if string(encoded) != `{"type":"refusal","category":"cyber","explanation":"original","future":["original"]}` {
		t.Fatal(string(encoded))
	}
}

func TestReplyMetadataRejectsNonfiniteAndSurrogatesAtomically(t *testing.T) {
	for _, invalid := range []string{`NaN`, `{"future":Infinity}`, `{"id":"\ud800"}`, `["\udfff"]`} {
		var container ContainerMetadata
		_ = json.Unmarshal([]byte(`{"id":"retained"}`), &container)
		if err := container.UnmarshalJSON([]byte(invalid)); err == nil {
			t.Fatal("accepted invalid container", invalid)
		}
		body, _ := container.Container()
		text, _ := body.ID.Text()
		if text != "retained" {
			t.Fatal("failed decode changed container")
		}
		var refusal StopDetailsMetadata
		_ = json.Unmarshal([]byte(`{"type":"retained"}`), &refusal)
		if err := refusal.UnmarshalJSON([]byte(invalid)); err == nil {
			t.Fatal("accepted invalid stop details", invalid)
		}
		detail, _ := refusal.Refusal()
		text, _ = detail.Type.Text()
		if text != "retained" {
			t.Fatal("failed decode changed stop details")
		}
	}
}

func TestSDKExpiryNormalizationIsExplicit(t *testing.T) {
	var container ContainerMetadata
	if err := json.Unmarshal([]byte(`{"id":"c","expires_at":1893553445}`), &container); err != nil {
		t.Fatal(err)
	}
	before, _ := container.Container()
	if before.ExpiresAt.Observation().Kind() != jsonvalue.Integer {
		t.Fatal("archive decode normalized SDK field")
	}
	after, _ := container.WithSDKDatetime().Container()
	text, ok := after.ExpiresAt.Text()
	if !ok || text != "2030-01-02T03:04:05Z" {
		t.Fatal(text)
	}
	before, _ = container.Container()
	if before.ExpiresAt.Observation().Kind() != jsonvalue.Integer {
		t.Fatal("SDK conversion mutated original")
	}
}
