package background

import (
	"errors"
	"path/filepath"
	"time"

	"github.com/luoyjx/mini-loop/go/shell"
)

// Scope is an opaque 256-bit lowercase hexadecimal identity. A fresh child uses
// a distinct prefix in the shared ledger, without adopting another live owner.
type Scope string

// NewScopedWithExecutor requires the lifetime owner to initialize its root
// manager first, adopting existing evidence before any child work is admitted.
// It never adopts the shared root's records. Root constructors after a restart
// report these qualified IDs as orphans through the ordinary ledger path.
func NewScopedWithExecutor(executor *shell.Executor, scope Scope) (*Manager, error) {
	if executor == nil || executor.Workspace() == "" {
		return nil, errors.New("background requires a bound native shell executor")
	}
	if len(scope) != 64 {
		return nil, errors.New("background scope requires 64 lowercase hexadecimal characters")
	}
	for _, c := range scope {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return nil, errors.New("background scope requires 64 lowercase hexadecimal characters")
		}
	}
	return &Manager{idPrefix: "bg_" + string(scope) + "_", executor: executor,
		secrets: executor, defaultTimeout: 300 * time.Second, retention: DefaultResultsRetained,
		ledgerDir: filepath.Join(executor.Workspace(), ".background"), tasks: make(map[ID]*slot)}, nil
}
