package reload

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildWorkspaceLeasesExcludeOnlyOwnedEvents(t *testing.T) {
	root := buildFixture(t)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	workspace, err := NewBuildWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	if _, err = NewBuilder(BuildConfig{ModuleDirectory: root, Workspace: workspace, TemporaryParent: root}); err == nil {
		t.Fatal("two temporary owners accepted")
	}
	builder, err := NewBuilder(BuildConfig{ModuleDirectory: root, Package: ".", Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := SelectObserverProfile(map[string]string{"WATCHFILES_FORCE_POLLING": "true", "WATCHFILES_POLL_DELAY_MS": "10"}, ObserverHost{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	observer, err := NewObserver(context.Background(), ObserverConfig{Roots: []string{root}, Workspace: workspace, Profile: profile})
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	if err = workspace.Close(); !errors.Is(err, ErrBuildWorkspaceBusy) {
		t.Fatal("observer lease missing", err)
	}
	artifact, err := builder.Build(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer artifact.Close()
	// Keep generated source and then delete it while the same subscription lives.
	generated := filepath.Join(artifact.root, "compiler-work", "generated.go")
	if err = os.WriteFile(generated, []byte("package temporary"), 0600); err != nil {
		t.Fatal(err)
	}
	assertNoSourceBatch(t, observer, 100*time.Millisecond)
	if err = artifact.Close(); err != nil {
		t.Fatal(err)
	}
	assertNoSourceBatch(t, observer, 100*time.Millisecond)
	source := filepath.Join(root, ".source", "new.go")
	if err = os.Mkdir(filepath.Dir(source), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(source, []byte("package source"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	batch, err := observer.Next(ctx)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Paths) != 1 || batch.Paths[0] != canonical {
		t.Fatal("external source hidden", batch)
	}
	if err = observer.Close(); err != nil {
		t.Fatal(err)
	}
	if err = observer.Close(); err != nil {
		t.Fatal(err)
	}
	if err = workspace.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = builder.Build(context.Background(), nil); !errors.Is(err, ErrBuildWorkspaceClosed) {
		t.Fatal("closed workspace reused", err)
	}
	if _, err = os.Stat(filepath.Join(root, "main.go")); err != nil {
		t.Fatal("source module removed", err)
	}
}

func TestBuildWorkspaceArtifactAndFailedAdmissions(t *testing.T) {
	root := buildFixture(t)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	workspace, err := NewBuildWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	builder, err := NewBuilder(BuildConfig{ModuleDirectory: root, Package: ".", Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := builder.Build(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer artifact.Close()
	if err = workspace.Close(); !errors.Is(err, ErrBuildWorkspaceBusy) {
		t.Fatal("artifact lease missing", err)
	}
	if err = artifact.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = NewObserver(context.Background(), ObserverConfig{Roots: []string{root}, Workspace: workspace}); err == nil {
		t.Fatal("zero profile accepted")
	}
	if err = os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main( {"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = builder.Build(context.Background(), nil); err == nil {
		t.Fatal("invalid source compiled")
	}
	if err = workspace.Close(); err != nil {
		t.Fatal("failed attempts leaked workspace leases", err)
	}
	if _, err = NewObserver(context.Background(), ObserverConfig{Workspace: workspace}); !errors.Is(err, ErrBuildWorkspaceClosed) {
		t.Fatal(err)
	}
	if err = (&BuildWorkspace{}).Close(); !errors.Is(err, ErrBuildWorkspaceClosed) {
		t.Fatal("zero workspace accepted", err)
	}
}

func TestOwnedEventsDoNotConsumeCapacityButKernelLossRemains(t *testing.T) {
	filter, _ := NewEventFilter(GoSource)
	reader := newEventReader(filter, NativeBackend, nil)
	reader.excludedWorkspace = filepath.Join(string(filepath.Separator), "owned")
	for i := 0; i < 5000; i++ {
		reader.record(filepath.Join(reader.excludedWorkspace, fmt.Sprintf("generated-%d.go", i)), 0)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := reader.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("owned names consumed event capacity", err)
	}
	reader.record(filepath.Join(reader.excludedWorkspace, "generated.go"), 7)
	if _, err := reader.Next(context.Background()); err == nil {
		t.Fatal("kernel loss hidden by exclusion")
	}
}

func assertNoSourceBatch(t *testing.T, observer Observer, duration time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	for {
		batch, err := observer.Next(ctx)
		if errors.Is(err, context.DeadlineExceeded) {
			return
		}
		if err != nil || len(batch.Paths) != 0 {
			t.Fatal("unexpected matching source batch", batch, err)
		}
	}
}
