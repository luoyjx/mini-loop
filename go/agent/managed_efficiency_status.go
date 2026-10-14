package agent

import "github.com/luoyjx/mini-loop/go/efficiency"

// RawArtifactStoreStatus is a detached observation of a session's masked RAM
// artifact store. It exposes neither raw content nor recovery references.
type RawArtifactStoreStatus struct {
	Limits efficiency.ArtifactLimits
	Closed bool
}

// RawArtifactStoreStatus reports the effective store selected during runtime
// construction. The boolean distinguishes absence from an open or closed store.
// A retained session can still report closure after its manager stops.
func (session *ManagedSession) RawArtifactStoreStatus() (RawArtifactStoreStatus, bool) {
	runtime := session.core.tokenEfficiency
	if runtime == nil || runtime.RawStore() == nil {
		return RawArtifactStoreStatus{}, false
	}
	store := runtime.RawStore()
	return RawArtifactStoreStatus{Limits: store.Limits(), Closed: store.Closed()}, true
}
