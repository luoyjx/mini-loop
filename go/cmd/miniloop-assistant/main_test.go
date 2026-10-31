package main

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRequireLoopback(t *testing.T) {
	for addr, ok := range map[string]bool{"127.0.0.1:8790": true, "localhost:0": true, "[::1]:8790": true, "0.0.0.0:8790": false, "192.168.1.2:8790": false, ":8790": false} {
		if err := requireLoopback(addr); (err == nil) != ok {
			t.Errorf("%s: err=%v, want ok=%v", addr, err, ok)
		}
	}
}

func TestRunRefusesUnsafeStartup(t *testing.T) {
	empty := func(string) string { return "" }
	cases := map[string][]string{
		"non-loopback bind":        {"-addr", "0.0.0.0:8790"},
		"anthropic without secret": {"-addr", "127.0.0.1:0", "-model", "anthropic", "-data", t.TempDir()},
		"unknown model":            {"-addr", "127.0.0.1:0", "-model", "gpt", "-data", t.TempDir()},
	}
	for name, args := range cases {
		var stderr bytes.Buffer
		if code := run(context.Background(), args, empty, &stderr); code != 1 || !strings.Contains(stderr.String(), "miniloop-assistant:") {
			t.Errorf("%s: code=%d stderr=%q", name, code, stderr.String())
		}
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestRunServesUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stderr := &lockedBuffer{}
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"-addr", "127.0.0.1:0", "-data", t.TempDir()}, func(string) string { return "" }, stderr)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(stderr.String(), "demo on http://127.0.0.1:") {
		if time.Now().After(deadline) {
			t.Fatalf("daemon did not start: %q", stderr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "scripted") {
		t.Fatalf("startup line should name the offline model: %q", stderr.String())
	}
}
