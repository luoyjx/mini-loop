//go:build !linux

package main

import (
	"github.com/luoyjx/mini-loop/go/reload"
	"runtime"
)

func captureReloadHost() (reload.ObserverHost, error) {
	system := runtime.GOOS
	if system == "darwin" {
		system = "Darwin"
	}
	return reload.ObserverHost{System: system}, nil
}
