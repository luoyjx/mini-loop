//go:build !(darwin || linux)

package assistant

// lockDataDir does not lock where flock is unavailable; the demo daemon
// targets macOS, and Linux shares the same implementation.
func lockDataDir(string) (func() error, error) { return func() error { return nil }, nil }
