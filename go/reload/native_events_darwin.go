//go:build darwin && cgo

package reload

/*
#cgo LDFLAGS: -framework CoreServices -framework CoreFoundation
#include <stdlib.h>
#include "native_events_darwin.h"
*/
import "C"

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"
)

// Only integer keys cross into C; registry entries remain typed Go ownership.
var nativeRegistry = struct {
	sync.Mutex
	next    uintptr
	sources map[uintptr]*NativeEvents
}{sources: make(map[uintptr]*NativeEvents)}

//export goMiniLoopNativeEvent
func goMiniLoopNativeEvent(key C.uintptr_t, path *C.char, flags C.uint32_t) {
	nativeRegistry.Lock()
	source := nativeRegistry.sources[uintptr(key)]
	nativeRegistry.Unlock()
	if source != nil {
		source.recordDarwin(C.GoString(path), uint32(flags))
	}
}

func (source *NativeEvents) start(ctx context.Context, roots []string) error {
	nativeRegistry.Lock()
	nativeRegistry.next++
	key := nativeRegistry.next
	nativeRegistry.sources[key] = source
	nativeRegistry.Unlock()
	ready := make(chan error, 1)
	go func() {
		defer close(source.done)
		defer func() { nativeRegistry.Lock(); delete(nativeRegistry.sources, key); nativeRegistry.Unlock() }()
		names := C.CString(strings.Join(roots, "\x00") + "\x00")
		events := C.mini_loop_events_start(C.uintptr_t(key), names)
		C.free(unsafe.Pointer(names))
		if events == nil {
			ready <- errors.New("could not start native FSEvents subscription")
			return
		}
		defer C.mini_loop_events_close(events)
		// The stream owns a serial dispatch queue; Close joins its callbacks
		// before removing this subscription's typed registry entry.
		ready <- nil
		<-source.stop
	}()
	if err := <-ready; err != nil {
		<-source.done
		return err
	}
	return nil
}

// recordDarwin applies notify 8's recursive subscription fence and precise
// change admission before the filename filter. Leaf paths remain lexical.
func (source *NativeEvents) recordDarwin(path string, flags uint32) {
	const historyDone uint32 = 0x10
	const changeFlags uint32 = 0xffe0 // root/mount/unmount and item changes
	const droppedFlags uint32 = 7
	if flags&historyDone != 0 || flags&(changeFlags|droppedFlags) == 0 {
		return
	}
	for _, root := range source.roots {
		relative, err := filepath.Rel(root, path)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			source.record(path, flags)
			return
		}
	}
}
