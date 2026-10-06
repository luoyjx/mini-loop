package agent

import "github.com/luoyjx/mini-loop/go/userresources"

// SkillCapture returns detached process-local evidence from completed admitted
// turns. It grants no publication permission and is not restored from history.
func (session *ManagedSession) SkillCapture() userresources.CaptureSnapshot {
	return session.skillCapture.Snapshot()
}
