package runmeta

import (
	"encoding/json"
	"testing"
)

func TestSnapshotCloneDetachesDecodedRecord(t *testing.T) {
	const wire = `{"message_id":"msg_fixed","origin":"explicit_human","actor_id":"owner","channel":"local","authority":"explicit_human","stamped_by":"trusted_local","delegated_by":"parent","parent_message_id":"msg_parent","approved_capabilities":["workflow.launch","workflow.manage"]}`
	var original Snapshot
	if err := json.Unmarshal([]byte(wire), &original); err != nil {
		t.Fatal(err)
	}
	clone := original.Clone()
	encoded, err := json.Marshal(clone)
	if err != nil || string(encoded) != wire {
		t.Fatalf("wire shape changed: %s, %v", encoded, err)
	}
	*clone.ActorID = "other"
	*clone.DelegatedBy = "other"
	*clone.ParentMessageID = "msg_other"
	clone.ApprovedCapabilities[0] = "other"
	encoded, err = json.Marshal(original)
	if err != nil || string(encoded) != wire {
		t.Fatalf("clone mutation reached original: %s, %v", encoded, err)
	}
}

func TestSnapshotCloneRetainsAbsentAndEmptyCapabilities(t *testing.T) {
	for _, wire := range []string{`null`, `[]`} {
		var original Snapshot
		if err := json.Unmarshal([]byte(`{"approved_capabilities":`+wire+`}`), &original); err != nil {
			t.Fatal(err)
		}
		clone := original.Clone()
		encoded, err := json.Marshal(clone.ApprovedCapabilities)
		if err != nil || string(encoded) != wire {
			t.Fatalf("capabilities %s changed to %s, %v", wire, encoded, err)
		}
		if clone.ActorID != nil || clone.ParentMessageID != nil || clone.DelegatedBy != nil {
			t.Fatal("clone invented provenance")
		}
	}
}
