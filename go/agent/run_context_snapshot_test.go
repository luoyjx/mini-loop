package agent

import (
	"encoding/json"
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
