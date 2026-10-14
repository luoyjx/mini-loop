//go:build darwin || linux

package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func parserShellQuote(text string) string {
	return "'" + strings.ReplaceAll(text, "'", "'\"'\"'") + "'"
}

func parserExecutable(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "interpreter")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCPythonParserIsolationVersionAndNoExecution(t *testing.T) {
	actual, err := NewCPythonParser(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	marker := filepath.Join(root, "source-executed")
	decoy := "open(" + strconv.Quote(marker) + ", 'w').write('imported')\n"
	for _, name := range []string{"ast.py", "sitecustomize.py"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(decoy), 0600); err != nil {
			t.Fatal(err)
		}
	}
	secret := filepath.Join(root, "inherited-secret")
	t.Setenv("MINILOOP_PARSER_SECRET", "private-marker")
	wrapper := parserExecutable(t, "cd "+parserShellQuote(root)+"\nif [ -n \"$MINILOOP_PARSER_SECRET\" ]; then printf leaked > "+parserShellQuote(secret)+"; fi\nexec "+parserShellQuote(actual.executable)+" \"$@\"")
	parser, err := NewCPythonParser(context.Background(), wrapper)
	if err != nil {
		t.Fatal(err)
	}
	result, err := parser.Parse(context.Background(), "open("+strconv.Quote(marker)+", 'w').write('executed')\n")
	if err != nil || result.Kind() != SyntaxClean {
		t.Fatal(result, err)
	}
	for _, path := range []string{marker, secret} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("parser imported/executed source or inherited a secret", path, err)
		}
	}
	wrong := parserExecutable(t, `printf '%s' '{"implementation":"cpython","version":"3.12.0","kind":"ready","class":"","line":0,"message":""}'`)
	if parser, err := NewCPythonParser(context.Background(), wrong); parser != nil || err == nil || !strings.Contains(err.Error(), "3.11.3") {
		t.Fatal("wrong interpreter version admitted", parser, err)
	}
	t.Setenv("PATH", t.TempDir())
	if parser, err := NewCPythonParser(context.Background(), ""); parser != nil || err == nil {
		t.Fatal("missing interpreter admitted", parser, err)
	}
}

func TestCPythonParserCancellationJoinsProcessAndBoundsConcurrentStarts(t *testing.T) {
	actual, err := NewCPythonParser(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	pids := t.TempDir()
	wrapper := parserExecutable(t, "if [ \"$6\" = parse ]; then\n printf '%s' \"$$\" > "+parserShellQuote(pids)+"/\"$$\"\n exec /bin/sleep 60\nfi\nexec "+parserShellQuote(actual.executable)+" \"$@\"")
	parser, err := NewCPythonParser(context.Background(), wrapper)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 2)
	for index := 0; index < 2; index++ {
		go func() { _, err := parser.Parse(ctx, "x=1"); done <- err }()
	}
	var entries []os.DirEntry
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		entries, err = os.ReadDir(pids)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(entries) != 2 {
		t.Fatal("two owned parser processes did not enter", len(entries))
	}
	queued, finish := context.WithTimeout(context.Background(), 50*time.Millisecond)
	_, err = parser.Parse(queued, "x=1")
	finish()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("queued parser did not honor cancellation", err)
	}
	if after, err := os.ReadDir(pids); err != nil || len(after) != 2 {
		t.Fatal("process budget admitted a third child", len(after), err)
	}
	cancel()
	for index := 0; index < 2; index++ {
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("cancel did not join parser")
		}
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
			t.Fatal("owned parser survived joined cancellation", pid, err)
		}
	}
	if len(cpythonProcesses) != 0 {
		t.Fatal("parser process permit leaked")
	}
}

func TestCPythonParserOutputBudgetFailsWithoutCleanResult(t *testing.T) {
	actual, err := NewCPythonParser(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	flood := fmt.Sprintf("import sys; sys.stdout.write('x' * %d)", parserOutputLimit+1)
	wrapper := parserExecutable(t, "if [ \"$6\" = parse ]; then exec "+parserShellQuote(actual.executable)+" -I -S -c "+parserShellQuote(flood)+"; fi\nexec "+parserShellQuote(actual.executable)+" \"$@\"")
	parser, err := NewCPythonParser(context.Background(), wrapper)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := parser.Parse(context.Background(), "x=1"); err == nil || result.Kind() != SyntaxInvalid || !strings.Contains(err.Error(), "byte budget") {
		t.Fatal(result, err)
	}
	if len(cpythonProcesses) != 0 {
		t.Fatal("output failure leaked a process permit")
	}
}
