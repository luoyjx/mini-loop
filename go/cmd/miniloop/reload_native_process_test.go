//go:build linux || (darwin && cgo)

package main

import (
	"github.com/luoyjx/mini-loop/go/reload"
	"testing"
)

func TestActualCLIRebuildsSourceRetainsPortAndReloadsDotenv(t *testing.T) {
	testActualCLIRebuildsSource(t, reload.NativeBackend)
}
