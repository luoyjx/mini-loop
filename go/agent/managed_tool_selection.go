package agent

import (
	"context"
	"errors"
)

var ErrToolSelectionStarted = errors.New("tool selection requires a session with no prior turns or history")

// RestrictTools narrows a newly constructed session before its first turn. It
// preserves installed order and ignores unknown names; repeated calls cannot
// restore removed tools. Construction-time services and store ownership remain
// intact. Admission is nonblocking so an occupied session returns ErrSessionBusy.
func (session *ManagedSession) RestrictTools(ctx context.Context, selection ToolSelection) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-session.admission:
	default:
		return ErrSessionBusy
	}
	defer func() { session.admission <- struct{}{} }()
	session.mu.Lock()
	defer session.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := session.admissionError(); err != nil {
		return err
	}
	session.core.mu.Lock()
	defer session.core.mu.Unlock()
	if session.runCount != 0 || len(session.core.messages) != 0 {
		return ErrToolSelectionStarted
	}
	catalog, err := NewToolCatalog(selection.filter(session.core.gate.catalog.ordered)...)
	if err != nil {
		return err
	}
	session.core.gate.catalog = catalog
	return nil
}
