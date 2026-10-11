package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditProcessEntry(t *testing.T) {
	if os.Getenv("MINILOOP_AUDIT_PROBE") != "1" {
		return
	}
	for index, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{os.Args[0]}, os.Args[index+1:]...)
			main()
			return
		}
	}
	t.Fatal("missing CLI boundary")
}

func TestActualAuditMainPreparesBeforeRemoteOrLocalDispatch(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".env")
	if err := os.WriteFile(path, []byte("BAD=\xff\n"), 0600); err != nil {
		t.Fatal(err)
	}
	command := func(args ...string) *exec.Cmd {
		child := exec.Command(os.Args[0], append([]string{"-test.run=^TestAuditProcessEntry$", "--"}, args...)...)
		child.Dir = root
		child.Env = []string{"MINILOOP_AUDIT_PROBE=1", "PATH=" + os.Getenv("PATH"), "GOMAXPROCS=2", "MINILOOP_WORKSPACE_ROOT=" + filepath.Join(root, "workspaces")}
		return child
	}
	output, err := command("--url").CombinedOutput()
	if err == nil || !strings.Contains(string(output), "invalid start byte") || strings.Contains(string(output), "usage:") {
		t.Fatal(err, string(output))
	}
	if _, err := os.Stat(filepath.Join(root, "workspaces")); !os.IsNotExist(err) {
		t.Fatal("bootstrap failure constructed deployment", err)
	}
	if err := os.WriteFile(path, []byte("MINILOOP_FAKE_LLM=1\nPORT=not-a-port\n"), 0600); err != nil {
		t.Fatal(err)
	}
	output, err = command().CombinedOutput()
	if err == nil {
		t.Fatal("expected blocking local findings", string(output))
	}
	if !strings.Contains(string(output), "[HIGH") || strings.Contains(string(output), "PORT must") {
		t.Fatal(string(output))
	}
	if _, err := os.Stat(filepath.Join(root, "workspaces")); err != nil {
		t.Fatal("local audit did not construct prepared deployment", err)
	}
}
