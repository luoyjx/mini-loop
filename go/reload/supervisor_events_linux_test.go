//go:build linux

package reload

import "testing"

func TestNativeSupervisorRetainsSocketAcrossRecoveryAndJoinsChildren(t *testing.T) {
	profile, err := SelectObserverProfile(map[string]string{}, ObserverHost{System: "Linux"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	testSupervisorRecovery(t, &profile)
}
