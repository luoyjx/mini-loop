//go:build darwin && cgo

package reload

/*
#cgo LDFLAGS: -framework CoreServices -framework CoreFoundation
#include <stdlib.h>
#include "native_events_darwin.h"
*/
import "C"

import (
	"errors"
	"runtime"
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
		source.record(C.GoString(path), uint32(flags))
	}
}

func (source *NativeEvents) start(roots []string) error {
	nativeRegistry.Lock()
	nativeRegistry.next++
	key := nativeRegistry.next
	nativeRegistry.sources[key] = source
	nativeRegistry.Unlock()
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(source.done)
		defer func() { nativeRegistry.Lock(); delete(nativeRegistry.sources, key); nativeRegistry.Unlock() }()
		names := C.CString(strings.Join(roots, "\x00") + "\x00")
		events := C.mini_loop_events_start(C.uintptr_t(key), names)
		C.free(unsafe.Pointer(names))
		if events == nil {
			ready <- errors.New("could not start native FSEvents subscription")
			return
		}
		ready <- nil
		defer C.mini_loop_events_close(events)
		for {
			select {
			case <-source.stop:
				return
			default:
				C.mini_loop_events_step(events)
			}
		}
	}()
	if err := <-ready; err != nil {
		<-source.done
		return err
	}
	return nil
}
