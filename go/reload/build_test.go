package reload

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func buildFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module reload.fixture\n\ngo 1.23\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func assertNoBuildOutputs(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("build leaked private outputs", entries, err)
	}
}

func TestBuildSyntaxFailureRecoveryAndPrivateArtifacts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	root, outputRoot := buildFixture(t), t.TempDir()
	builder, err := NewBuilder(BuildConfig{ModuleDirectory: root, Package: ".", TemporaryParent: outputRoot})
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "main.go")
	if err := os.WriteFile(source, []byte("package main\nfunc main( {\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var diagnostics bytes.Buffer
	if artifact, err := builder.Build(ctx, &diagnostics); err == nil || artifact != nil || !strings.Contains(diagnostics.String(), "syntax error") {
		t.Fatal("invalid source admitted", artifact, err, diagnostics.String())
	}
	assertNoBuildOutputs(t, outputRoot)
	var firstPath string
	for _, answer := range []string{"first", "second"} {
		code := fmt.Sprintf("package main\nimport \"fmt\"\nfunc main() { fmt.Print(%q) }\n", answer)
		if err := os.WriteFile(source, []byte(code), 0600); err != nil {
			t.Fatal(err)
		}
		artifact, err := builder.Build(ctx, &diagnostics)
		if err != nil {
			t.Fatal(err, diagnostics.String())
		}
		t.Cleanup(func() {
			if err := artifact.Close(); err != nil {
				t.Error(err)
			}
		})
		path, err := artifact.Executable(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if path == firstPath {
			t.Fatal("replacement reused a published executable path")
		}
		firstPath = path
		info, err := os.Stat(filepath.Dir(path))
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatal("artifact directory is not private", info, err)
		}
		output, err := exec.CommandContext(ctx, path).CombinedOutput()
		if err != nil || string(output) != answer {
			t.Fatal("fresh compiled behavior", string(output), err)
		}
		// Integrity admission must refuse modifications to the retained bytes.
		file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := file.WriteString("changed")
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatal(writeErr, closeErr)
		}
		if _, err := artifact.Executable(ctx); err == nil {
			t.Fatal("modified executable admitted")
		}
		if err := artifact.Close(); err != nil {
			t.Fatal(err)
		}
		if err := artifact.Close(); err != nil {
			t.Fatal("repeated close", err)
		}
		if _, err := artifact.Executable(ctx); err == nil {
			t.Fatal("closed artifact admitted")
		}
		assertNoBuildOutputs(t, outputRoot)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatal("artifact cleanup removed source", err)
	}
}

func TestBuildRejectsLibraryAndCancelledAdmission(t *testing.T) {
	root, outputs := buildFixture(t), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "library.go"), []byte("package library\n"), 0600); err != nil {
		t.Fatal(err)
	}
	builder, err := NewBuilder(BuildConfig{ModuleDirectory: root, Package: ".", TemporaryParent: outputs})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if artifact, err := builder.Build(ctx, nil); err == nil || artifact != nil {
		t.Fatal("non-executable package admitted", artifact, err)
	}
	assertNoBuildOutputs(t, outputs)
	cancel()
	if artifact, err := builder.Build(ctx, nil); !errors.Is(err, context.Canceled) || artifact != nil {
		t.Fatal("cancelled build admitted", artifact, err)
	}
	assertNoBuildOutputs(t, outputs)
}

func TestBuildConfigurationAdmissionAndDetachedEnvironment(t *testing.T) {
	root := buildFixture(t)
	for _, config := range []BuildConfig{
		{}, {ModuleDirectory: root, Package: "../outside"},
		{ModuleDirectory: root, Package: "./../outside"},
		{ModuleDirectory: root, Package: "./..."},
		{ModuleDirectory: root, Package: "-o"},
		{ModuleDirectory: root, Toolchain: filepath.Join(root, "missing-go")},
	} {
		if _, err := NewBuilder(config); err == nil {
			t.Fatal("invalid build configuration admitted", config)
		}
	}
	env := []string{"RELOAD_SECRET=fixture-secret"}
	builder, err := NewBuilder(BuildConfig{ModuleDirectory: root, Environment: env})
	if err != nil {
		t.Fatal(err)
	}
	env[0] = "changed"
	if builder.environment[0] != "RELOAD_SECRET=fixture-secret" || strings.Contains(fmt.Sprintf("%v %#v", builder, builder), "fixture-secret") {
		t.Fatal("retained build environment was changed or exposed")
	}
}
