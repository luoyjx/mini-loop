//go:build linux

package reload

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func linuxInotifyDescriptors(t *testing.T) int {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		target, _ := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if strings.Contains(target, "inotify") {
			count++
		}
	}
	return count
}

func TestLinuxNativeDirectoryReplacementAndDescriptorRelease(t *testing.T) {
	before := linuxInotifyDescriptors(t)
	root := t.TempDir()
	observer, err := NewNativeEvents([]string{root}, GoSource)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = observer.Close() })
	await := func(want []string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		batch, err := observer.Next(ctx)
		if err != nil || !reflect.DeepEqual(batch.Paths, want) {
			t.Fatalf("batch=%v want=%v error=%v", batch.Paths, want, err)
		}
	}
	directory := filepath.Join(root, "new")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	await([]string{})
	path := filepath.Join(directory, "file.go")
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	await([]string{path})
	moved := filepath.Join(root, "renamed")
	if err := os.Rename(directory, moved); err != nil {
		t.Fatal(err)
	}
	await([]string{})
	path = filepath.Join(moved, "file.go")
	if err := os.WriteFile(path, []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	await([]string{path})
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.Rename(moved, outside); err != nil {
		t.Fatal(err)
	}
	await([]string{})
	if err := os.WriteFile(filepath.Join(outside, "file.go"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	assertNoSourceBatch(t, observer, 150*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	done := make(chan error, 1)
	go func() { _, err := observer.Next(ctx); done <- err }()
	if err := observer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrEventsClosed) {
		t.Fatal("blocked read close", err)
	}
	cancel()
	if err := observer.Close(); err != nil {
		t.Fatal(err)
	}
	if after := linuxInotifyDescriptors(t); after != before {
		t.Fatalf("inotify descriptors before=%d after=%d", before, after)
	}
}

func TestLinuxNativeKernelRecordAdmission(t *testing.T) {
	source := &NativeEvents{eventReader: newEventReader(EventFilter{extension: ".go"}, NativeBackend, nil)}
	subscription := &inotifySubscription{fd: -1, paths: map[int]string{1: "/source.go"}, source: source}
	packet := make([]byte, 16)
	binary.NativeEndian.PutUint32(packet[:4], 1)
	binary.NativeEndian.PutUint32(packet[4:8], syscall.IN_MODIFY)
	if err := subscription.consume(packet); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	batch, err := source.Next(ctx)
	if err != nil || !reflect.DeepEqual(batch.Paths, []string{"/source.go"}) {
		t.Fatal("Linux modify is not a Darwin loss flag", batch, err)
	}
	for _, data := range [][]byte{packet[:8], append(append([]byte{}, packet[:12]...), 1, 0, 0, 0)} {
		if err := subscription.consume(data); err == nil {
			t.Fatal("truncated kernel packet admitted")
		}
	}
	binary.NativeEndian.PutUint32(packet[4:8], syscall.IN_Q_OVERFLOW)
	if err := subscription.consume(packet); err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatal("kernel overflow lost", err)
	}
	binary.NativeEndian.PutUint32(packet[4:8], syscall.IN_UNMOUNT)
	if err := subscription.consume(packet); err == nil || !strings.Contains(err.Error(), "unmounted") {
		t.Fatal("kernel unmount lost", err)
	}
}

func TestLinuxNativeCycleMatchesSource(t *testing.T) {
	var fixture struct {
		Cycle struct {
			Entered, Closed bool
			LeafLoop        bool `json:"leaf_loop"`
			Outcome         string
		} `json:"native_cycle"`
	}
	data, err := os.ReadFile("../testdata/python-watchfiles-linux.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if !fixture.Cycle.Entered || !fixture.Cycle.Closed || !fixture.Cycle.LeafLoop || fixture.Cycle.Outcome != "timeout" {
		t.Fatal("Source native cycle contract changed", fixture.Cycle)
	}
	root := t.TempDir()
	if err := os.Symlink(".", filepath.Join(root, "cycle")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("file-loop.go", filepath.Join(root, "file-loop.go")); err != nil {
		t.Fatal(err)
	}
	observer, err := NewNativeEvents([]string{root}, GoSource)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := observer.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("native cycle must not fault or spin", err)
	}
}

func TestLinuxNativeWriteOpenWithoutWriteMatchesSource(t *testing.T) {
	var fixture struct {
		Noop struct {
			Outcome string
			Closed  bool
		} `json:"native_noop_open"`
	}
	data, err := os.ReadFile("../testdata/python-watchfiles-linux.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if !fixture.Noop.Closed || fixture.Noop.Outcome != "timeout" {
		t.Fatal("Source noop access contract changed", fixture.Noop)
	}
	root := t.TempDir()
	path := filepath.Join(root, "noop.go")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	observer, err := NewNativeEvents([]string{root}, GoSource)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := observer.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("access-only event caused a batch", err)
	}
}

func TestLinuxNativeConstructionCancellationReleasesDescriptor(t *testing.T) {
	root := t.TempDir()
	for index := 0; index < 2000; index++ {
		if err := os.Mkdir(filepath.Join(root, fmt.Sprintf("dir-%04d", index)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	before := linuxInotifyDescriptors(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	profile, err := SelectObserverProfile(map[string]string{}, ObserverHost{System: "Linux"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	type admission struct {
		observer Observer
		err      error
	}
	done := make(chan admission, 1)
	go func() {
		observer, err := NewObserver(ctx, ObserverConfig{Roots: []string{root}, Language: GoSource, Profile: profile})
		done <- admission{observer, err}
	}()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for linuxInotifyDescriptors(t) == before {
		select {
		case result := <-done:
			if result.observer != nil {
				_ = result.observer.Close()
			}
			t.Fatalf("construction ended before open-descriptor cancellation: %v", result.err)
		case <-timeout.C:
			cancel()
			result := <-done
			if result.observer != nil {
				_ = result.observer.Close()
			}
			t.Fatal("constructor descriptor did not become observable")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case result := <-done:
		if result.observer != nil {
			_ = result.observer.Close()
			t.Fatal("cancelled constructor returned a subscription")
		}
		if !errors.Is(result.err, context.Canceled) {
			t.Fatal("construction cancellation lost", result.err)
		}
	case <-timeout.C:
		t.Fatal("cancelled constructor did not join")
	}
	if after := linuxInotifyDescriptors(t); after != before {
		t.Fatalf("construction leaked descriptors: before=%d after=%d", before, after)
	}
}

func TestLinuxNativeRootMoveRetainsSourceLexicalProjection(t *testing.T) {
	var fixture struct {
		Move struct {
			RootMove   []string `json:"root_move"`
			Subsequent []string `json:"subsequent_write"`
			Closed     bool
		} `json:"native_root_move"`
	}
	data, err := os.ReadFile("../testdata/python-watchfiles-linux.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if !fixture.Move.Closed || !reflect.DeepEqual(fixture.Move.RootMove, []string{"<root>"}) || !reflect.DeepEqual(fixture.Move.Subsequent, []string{"a.py"}) {
		t.Fatal("Source root-move contract changed", fixture.Move)
	}
	base := t.TempDir()
	root := filepath.Join(base, "root")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	observer, err := NewNativeEvents([]string{root}, GoSource)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	moved := filepath.Join(base, "moved")
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if batch, err := observer.Next(ctx); err != nil || len(batch.Paths) != 0 {
		t.Fatal("root-only event should be filtered", batch, err)
	}
	if err := os.WriteFile(filepath.Join(moved, "a.go"), []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	batch, err := observer.Next(ctx)
	if err != nil || !reflect.DeepEqual(batch.Paths, []string{filepath.Join(root, "a.go")}) {
		t.Fatal("root projection was resolved or silently revoked", batch, err)
	}
}
