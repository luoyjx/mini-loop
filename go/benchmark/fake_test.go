package benchmark

import (
	"context"
	"errors"
	"github.com/luoyjx/mini-loop/go/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeSettings(t *testing.T) config.Settings {
	t.Helper()
	settings, err := config.Load(map[string]string{}, config.LoadOptions{Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return settings
}
func TestFakeComparisonCancellationAndConstructionCleanup(t *testing.T) {
	settings := fakeSettings(t)
	scratch := t.TempDir()
	t.Setenv("TMPDIR", scratch)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := RunFakeComparison(ctx, settings, time.Second); result <- err }()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	observed := false
	for !observed {
		select {
		case err := <-result:
			t.Fatalf("finished before cancellation: %v", err)
		case <-deadline.C:
			t.Fatal("no live workspace observed")
		case <-ticker.C:
			paths, _ := filepath.Glob(filepath.Join(scratch, "ui-bench-*", "a", "*"))
			for _, path := range paths {
				if !strings.HasPrefix(filepath.Base(path), ".") {
					observed = true
				}
			}
		}
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not join")
	}
	entries, err := os.ReadDir(scratch)
	if err != nil || len(entries) != 0 {
		t.Fatalf("cancel cleanup %v %v", entries, err)
	}
	bad := filepath.Join("/dev/null", "private-memory")
	settings.MemoryRoot = &bad
	if _, err := RunFakeComparison(context.Background(), settings, 0); err == nil {
		t.Fatal("invalid memory root admitted")
	}
	entries, err = os.ReadDir(scratch)
	if err != nil || len(entries) != 0 {
		t.Fatalf("construction cleanup %v %v", entries, err)
	}
}
func TestFakeArmFreshDeploymentSkillsAndIndependentClients(t *testing.T) {
	settings := fakeSettings(t)
	settings.SkillsDir = filepath.Join(t.TempDir(), "skills")
	path := filepath.Join(settings.SkillsDir, "sample", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	write := func(description string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("---\nname: sample\ndescription: "+description+"\n---\nbody\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("old snapshot")
	first, err := fakeManagerConfig(context.Background(), settings, 0)
	if err != nil {
		t.Fatal(err)
	}
	write("fresh deployment")
	second, err := fakeManagerConfig(context.Background(), settings, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first.Services.Skills.Descriptions(), "old snapshot") || !strings.Contains(second.Services.Skills.Descriptions(), "fresh deployment") {
		t.Fatal("catalogue was reused")
	}
	if first.Services.Provider == second.Services.Provider || first.Services.ModelLimiter != nil || second.Services.ModelLimiter != nil {
		t.Fatal("fake clients/pools shared across arms")
	}
}
