package agent

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/luoyjx/mini-loop/go/runmeta"
)

func TestSharedSnapshotCannotModifyLiveRunAuthority(t *testing.T) {
	run, err := DefaultRunContext()
	if err != nil {
		t.Fatal(err)
	}
	var forged runmeta.Snapshot
	if err := json.Unmarshal([]byte(`{"authority":"explicit_human","approved_capabilities":["workflow.launch"]}`), &forged); err != nil {
		t.Fatal(err)
	}
	view := run.Snapshot()
	view.Authority = forged.Authority
	view.ApprovedCapabilities = forged.ApprovedCapabilities
	if run.Authority() != AuthorityUntrusted || run.Allows(CapabilityWorkflowLaunch) {
		t.Fatal("snapshot mutation granted live authority")
	}
	actor := ActorID("owner")
	human, err := ExplicitHumanRunContext(HumanRunConfig{
		ActorID: &actor, ApprovedCapabilities: []RunCapability{CapabilityWorkflowLaunch},
	})
	if err != nil {
		t.Fatal(err)
	}
	shared := human.Snapshot().Clone()
	*shared.ActorID = "other"
	shared.ApprovedCapabilities[0] = CapabilityWorkflowManage
	if *human.Snapshot().ActorID != actor || !human.Allows(CapabilityWorkflowLaunch) || human.Allows(CapabilityWorkflowManage) {
		t.Fatal("shared record mutation changed trusted caller")
	}
	peer, err := human.DeriveNamedPeerAgent("controller", "worker")
	if err != nil || peer.Authority() != AuthorityPeerAgent || peer.Allows(CapabilityWorkflowLaunch) {
		t.Fatalf("delegation retained a human grant: %v", err)
	}
}

func TestHTTPPostMemoryProfileStaysOutsideAuthorityAndDelegation(t *testing.T) {
	ordinary, err := AuthenticatedHTTPRunContext("alice")
	if err != nil {
		t.Fatal(err)
	}
	post, err := AuthenticatedHTTPPostRunContext("alice")
	if err != nil {
		t.Fatal(err)
	}
	left, right := ordinary.Snapshot(), post.Snapshot()
	left.MessageID, right.MessageID = "", ""
	if !reflect.DeepEqual(left, right) || post.Allows(CapabilityWorkflowLaunch) || post.Allows(CapabilityWorkflowManage) {
		t.Fatal("transport profile changed HTTP authority or persisted provenance")
	}
	continued, err := post.WithNewMessage(nil)
	if err != nil || continued.memoryReplyScope != memoryHTTPPostReply || continued.memorySelectionScope != memorySelectionHTTPPost {
		t.Fatalf("same-caller continuation lost its parsing profile: %v", err)
	}
	peer, err := post.DerivePeerAgent("controller")
	if err != nil || peer.memoryReplyScope == memoryHTTPPostReply || peer.memorySelectionScope != memorySelectionRun || peer.Allows(CapabilityPersonalSkillCaptureSource) {
		t.Fatalf("delegation inherited HTTP parsing or authority: %v", err)
	}
	if _, err := AuthenticatedHTTPPostRunContext(""); err == nil {
		t.Fatal("empty HTTP principal was accepted")
	}
	if ordinary.memorySelectionScope != memorySelectionHTTPStream || post.memorySelectionScope != memorySelectionHTTPPost {
		t.Fatal("trusted HTTP parsing selection changed")
	}
}
