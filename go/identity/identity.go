// Package identity reports the running native artifact and process instance.
package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strconv"
	"sync"
	"time"
)

// Fingerprint is a short SHA256 digest of the executable's bytes. It identifies
// the native artifact, rather than pretending to hash the Python source tree.
type Fingerprint string

// ProcessSnapshot describes this process, independently of any server or clock
// injected for request rate limiting. StartedAt is captured at package startup.
type ProcessSnapshot struct {
	PID       int     `json:"pid"`
	StartedAt float64 `json:"started_at"`
	Uptime    float64 `json:"uptime_s"`
}

// Snapshot is the native counterpart of Source's manager-free runtime_identity.
// Manager-owned posture remains the responsibility of the serving application.
type Snapshot struct {
	Build Fingerprint `json:"build"`
	ProcessSnapshot
}

var (
	processStarted = float64(time.Now().UnixNano()) / 1e9
	processPID     = os.Getpid()
	currentBuildID = cachedBuildID(os.Executable)
)

// BuildID retains a successful executable fingerprint. Failed reads remain
// retryable, matching Source's lru_cache exception behavior; they never fall
// back to a plausible development/VCS label.
func BuildID() (Fingerprint, error) {
	return currentBuildID()
}

func cachedBuildID(executable func() (string, error)) func() (Fingerprint, error) {
	var mutex sync.Mutex
	var cached Fingerprint
	return func() (Fingerprint, error) {
		mutex.Lock()
		defer mutex.Unlock()
		if cached != "" {
			return cached, nil
		}
		path, err := executable()
		if err == nil {
			var value Fingerprint
			value, err = fingerprintFile(path)
			if err == nil {
				cached = value
				return value, nil
			}
		}
		// This error may reach inspection JSON. Keep filesystem paths and OS
		// diagnostics private, and never publish an empty fingerprint as success.
		return "", errors.New("native executable fingerprint unavailable")
	}
}

func fingerprintFile(path string) (Fingerprint, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	_, readErr := io.CopyBuffer(digest, file, make([]byte, 32*1024))
	closeErr := file.Close()
	if readErr != nil {
		return "", readErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return Fingerprint(hex.EncodeToString(digest.Sum(nil))[:12]), nil
}

// Process returns one process-start identity shared by all server instances.
// Elapsed wall time, including backwards clock changes, matches Source.
func Process() ProcessSnapshot {
	return processSnapshotAt(processPID, processStarted, float64(time.Now().UnixNano())/1e9)
}

func processSnapshotAt(pid int, startedAt, now float64) ProcessSnapshot {
	// Formatting uses the original float's decimal rounding rather than
	// multiplying first, which can introduce a new binary rounding boundary.
	// As with Python round(value, 3), a rounded negative zero is preserved.
	rounded, _ := strconv.ParseFloat(strconv.FormatFloat(now-startedAt, 'f', 3, 64), 64)
	return ProcessSnapshot{PID: pid, StartedAt: startedAt, Uptime: rounded}
}

func Runtime() (Snapshot, error) {
	build, err := BuildID()
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Build: build, ProcessSnapshot: Process()}, nil
}
