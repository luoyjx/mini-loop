//go:build darwin || linux

package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestDeepGlobSurvivesLimitedDirectoryDescriptors(t *testing.T) {
	files, err := NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := strings.Repeat("d/", 120) + "needle.txt"
	fullPath := filepath.Join(files.Root(), path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var previous syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &previous); err != nil {
		t.Skipf("cannot read process descriptor limit: %v", err)
	}
	limited := previous
	if limited.Cur > 64 {
		limited.Cur = 64
	}
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &limited); err != nil {
		t.Skipf("cannot adjust process descriptor limit: %v", err)
	}
	defer func() {
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &previous); err != nil {
			t.Errorf("restore descriptor limit: %v", err)
		}
	}()
	for attempt := 0; attempt < 3; attempt++ {
		output, err := files.Glob(context.Background(), protocol.GlobInput{Pattern: "**/needle.txt"})
		if err != nil || output != path {
			t.Fatalf("deep result was lost on attempt %d: %q %v", attempt, output, err)
		}
	}
}
