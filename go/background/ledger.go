package background

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/luoyjx/mini-loop/go/shell"
)

// Wire records are concrete. Wrong field types are reported as unreadable;
// legacy records may omit fields. The filename is the identity, as in source.
type ledgerRecord struct {
	ID        ID               `json:"bg_id"`
	Command   string           `json:"command"`
	PID       *shell.ProcessID `json:"pid"`
	StartedAt float64          `json:"started_at"`
}

const maxLedgerBytes = 1 << 20
const unreadableCommand = "(unreadable ledger record)"

func (manager *Manager) writeLedger(id ID, command string, pid *shell.ProcessID) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	if err := os.MkdirAll(manager.ledgerDir, 0700); err != nil {
		return false
	}
	data, err := json.Marshal(ledgerRecord{id, prefix(manager.secrets.MaskText(command), 200), pid, float64(time.Now().UnixNano()) / 1e9})
	if err != nil {
		return false
	}
	file, err := os.CreateTemp(manager.ledgerDir, ".record-*")
	if err != nil {
		return false
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return false
	}
	if err = file.Close(); err != nil {
		return false
	}
	return os.Rename(file.Name(), filepath.Join(manager.ledgerDir, string(id)+".json")) == nil
}
func (manager *Manager) unledger(id ID) {
	_ = os.Remove(filepath.Join(manager.ledgerDir, string(id)+".json"))
}
func readLedger(path string) (ledgerRecord, bool) {
	file, err := os.Open(path)
	if err != nil {
		return ledgerRecord{}, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxLedgerBytes+1))
	if err != nil || len(data) > maxLedgerBytes {
		return ledgerRecord{}, false
	}
	var record ledgerRecord
	if json.Unmarshal(data, &record) != nil {
		return ledgerRecord{}, false
	}
	return record, true
}
func (manager *Manager) adoptOrphans() {
	// ReadDir treats the workspace as a literal path, as Python Path.glob does.
	// Joining it into a glob would interpret brackets in legitimate root names.
	entries, _ := os.ReadDir(manager.ledgerDir)
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "bg_") || !strings.HasSuffix(name, ".json") {
			continue
		}
		path := filepath.Join(manager.ledgerDir, name)
		id := ID(strings.TrimSuffix(name, ".json"))
		digits := strings.TrimPrefix(string(id), "bg_")
		if n, ok := decimalCounter(digits); ok && n.Cmp(&manager.counter) > 0 {
			manager.counter.Set(n)
		}
		command := unreadableCommand
		var pid *shell.ProcessID
		if record, ok := readLedger(path); ok {
			if record.Command != "" {
				command = record.Command
			}
			pid = record.PID
		}
		text := "orphaned by a restart: " + command
		if pid != nil && *pid > 0 && processAlive(*pid) {
			text += " -- a process with pid " + pidString(*pid) + " is still alive and unsupervised; its output will never be delivered. Re-run if the work matters, or kill the pid if it should stop."
		} else {
			text += " -- no live process; whether it completed is unknown. Verify its effects before re-running."
		}
		manager.tasks[id] = &slot{record: Record{id, Orphaned, command, &text}}
		manager.order = append(manager.order, id)
		manager.completed = append(manager.completed, Notification{id, Orphaned, text})
		manager.settle(id)
	}
}
