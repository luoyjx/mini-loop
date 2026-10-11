//go:build darwin && cgo

package reload

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeObserverDuringRealCgoBuildInsideWatchedRoot(t *testing.T) {
	root := buildFixture(t)
	source := "package main\n/* int fixture_value(void) { return 7; } */\nimport \"C\"\nfunc main() { if C.fixture_value() != 7 { panic(\"bad result\") } }\n"
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	workspace, err := NewBuildWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	toolchain := filepath.Join(root, "capture-go")
	// -work retains genuine Cgo-generated .go files for the ownership assertion.
	script := "#!/bin/sh\nprintf '%s\\n%s\\n' \"$GOTMPDIR\" \"$TMPDIR\" > \"$CAPTURE_TMP_PATH\"\nshift\nexec \"$ACTUAL_GO\" build -work \"$@\"\n"
	if err = os.WriteFile(toolchain, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	actualGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(root, "compiler-env.txt")
	external := t.TempDir()
	env := append(os.Environ(), "CGO_ENABLED=1", "ACTUAL_GO="+actualGo, "CAPTURE_TMP_PATH="+capture, "GOTMPDIR="+external, "TMPDIR="+external)
	builder, err := NewBuilder(BuildConfig{ModuleDirectory: root, Package: ".", Toolchain: toolchain, Environment: env, Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := SelectObserverProfile(nil, ObserverHost{System: "Darwin"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	observer, err := NewObserver(context.Background(), ObserverConfig{Roots: []string{root}, Profile: profile, Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	awaitNativeFixtureReady(t, observer, root)

	artifact, err := builder.Build(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer artifact.Close()
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(artifact.root, "compiler-work")
	if string(data) != scratch+"\n"+scratch+"\n" {
		t.Fatal("compiler temporary overrides escaped", string(data))
	}
	found := false
	if err = filepath.WalkDir(scratch, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".cgo1.go") {
			found = true
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("no actual Cgo source retained")
	}
	executable, err := artifact.Executable(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = exec.Command(executable).Run(); err != nil {
		t.Fatal("real Cgo artifact failed", err)
	}
	assertNoSourceBatch(t, observer, 250*time.Millisecond)
	if err = artifact.Close(); err != nil {
		t.Fatal(err)
	}
	assertNoSourceBatch(t, observer, 250*time.Millisecond)
	if err = observer.Close(); err != nil {
		t.Fatal(err)
	}
	if err = workspace.Close(); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(external); err != nil || len(entries) != 0 {
		t.Fatal("caller tmp directory was used", entries, err)
	}
}
