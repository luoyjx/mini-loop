package agent

import (
	"context"
	"math/big"
	"testing"

	"github.com/luoyjx/mini-loop/go/efficiency"
)

func TestRawArtifactStoreStatusOwnsLimitsAndReportsShutdown(t *testing.T) {
	config := managerTestConfig(t.TempDir(), &FakeProvider{})
	selection := DefaultManagerEfficiencyConfig()
	selection.Mode, selection.ArtifactTTLSeconds = efficiency.Enforce, 7
	selection.MaxArtifactBytes, selection.MaxTotalBytes = big.NewInt(20000), big.NewInt(40000)
	config.Efficiency = &selection
	config.Defaults.PermissionMode = ModeAuto
	manager := makeManager(t, config)
	session := createManaged(t, manager, CreateSessionRequest{Owner: "operator"})
	before, present := session.RawArtifactStoreStatus()
	if !present || before.Closed || before.Limits.TTLSeconds != 7 || before.Limits.MaxArtifactBytes.Cmp(big.NewInt(20000)) != 0 || before.Limits.MaxTotalBytes.Cmp(big.NewInt(40000)) != 0 {
		t.Fatalf("effective store posture: %+v, present=%t", before, present)
	}
	before.Limits.MaxArtifactBytes.SetInt64(-1)
	before.Limits.MaxTotalBytes.SetInt64(-1)
	again, present := session.RawArtifactStoreStatus()
	if !present || again.Limits.MaxArtifactBytes.Int64() != 20000 || again.Limits.MaxTotalBytes.Int64() != 40000 {
		t.Fatal("status observation mutated the live store limits")
	}
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, present := session.RawArtifactStoreStatus()
	if !present || !after.Closed || after.Limits.TTLSeconds != again.Limits.TTLSeconds || after.Limits.MaxArtifactBytes.Cmp(again.Limits.MaxArtifactBytes) != 0 || after.Limits.MaxTotalBytes.Cmp(again.Limits.MaxTotalBytes) != 0 {
		t.Fatalf("retained session shutdown posture: %+v, present=%t", after, present)
	}

	disabled := createManaged(t, makeManager(t, managerTestConfig(t.TempDir(), &FakeProvider{})), CreateSessionRequest{Owner: "operator"})
	if _, present := disabled.RawArtifactStoreStatus(); present {
		t.Fatal("default-off runtime acquired an artifact store")
	}
}
