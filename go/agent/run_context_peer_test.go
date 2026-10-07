package agent

import "testing"

func TestNamedPeerContextRetainsProvenanceWithoutHumanCapabilities(t *testing.T) {
	actor := ActorID("human")
	human, err := ExplicitHumanRunContext(HumanRunConfig{ActorID: &actor, StampedBy: "operator", Channel: "cli", ApprovedCapabilities: []RunCapability{CapabilityWorkflowLaunch, CapabilityWorkflowManage}})
	if err != nil {
		t.Fatal(err)
	}
	peer, err := human.DeriveNamedPeerAgent("lead", "researcher")
	if err != nil {
		t.Fatal(err)
	}
	value := peer.Snapshot()
	if value.MessageID == human.MessageID() || value.Authority != AuthorityPeerAgent || value.ActorID == nil || *value.ActorID != "researcher" || value.ParentMessageID == nil || *value.ParentMessageID != human.MessageID() || value.DelegatedBy == nil || *value.DelegatedBy != "lead" || value.StampedBy != "operator" || value.Channel != "agent" || value.Origin != "peer_agent" || len(value.ApprovedCapabilities) != 0 {
		t.Fatal(value)
	}
	if peer.Allows(CapabilityWorkflowLaunch) || !human.Allows(CapabilityWorkflowLaunch) || *human.Snapshot().ActorID != "human" {
		t.Fatal("human authority was changed or inherited")
	}
	if _, err := (RunContext{}).DeriveNamedPeerAgent("lead", "researcher"); err == nil {
		t.Fatal("invalid parent provenance accepted")
	}
}
