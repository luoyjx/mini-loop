//go:build darwin && cgo

package reload

import "testing"

func TestNativeSupervisorRetainsSocketAcrossRecoveryAndJoinsChildren(t *testing.T) {
	profile, err := SelectObserverProfile(map[string]string{}, ObserverHost{System: "Darwin"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	testSupervisorRecovery(t, &profile)
}
