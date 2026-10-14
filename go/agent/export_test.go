package agent

import "testing"

// WithTeamlessBindingForTest adapts Source's controlled missing/None team state
// for external HTTP tests. It exists only in Go's augmented test package, not in
// a production build. Callers must perform synchronous observations, without
// parallel requests, team workers, deletion or shutdown. The binding is restored
// before returning, including when the observation calls testing.Fatal.
func WithTeamlessBindingForTest(t *testing.T, manager *SessionManager, owner OwnerID, id SessionID, observe func()) {
	t.Helper()
	manager.mu.Lock()
	session := manager.sessions[id]
	valid := manager.state == ManagerActive && session != nil && owner != "" && session.Owner() == owner
	manager.mu.Unlock()
	if !valid || observe == nil {
		t.Fatal("teamless test assembly requires an active owned session and observation")
	}
	// Follow the runtime's admission -> session -> core order. Holding the
	// admission token keeps model turns out while the synchronous view runs.
	select {
	case <-session.admission:
	default:
		t.Fatal("teamless test assembly requires an idle session")
	}
	defer func() { session.admission <- struct{}{} }()
	session.mu.Lock()
	if !session.accepting || session.active != nil || session.teamRun != nil {
		session.mu.Unlock()
		t.Fatal("teamless test assembly refuses closed, active or teammate sessions")
	}
	session.core.mu.Lock()
	saved := clonePointer(session.core.team)
	if saved == nil {
		session.core.mu.Unlock()
		session.mu.Unlock()
		t.Fatal("teamless test control had no original managed identity")
	}
	session.core.team = nil
	session.core.mu.Unlock()
	session.mu.Unlock()
	defer func() {
		session.mu.Lock()
		session.core.mu.Lock()
		session.core.team = saved
		session.core.mu.Unlock()
		session.mu.Unlock()
	}()
	observe()
}
