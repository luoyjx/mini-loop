//go:build linux

package main

import (
	"github.com/luoyjx/mini-loop/go/reload"
	"syscall"
)

func captureReloadHost() (reload.ObserverHost, error) {
	var name syscall.Utsname
	if err := syscall.Uname(&name); err != nil {
		return reload.ObserverHost{}, err
	}
	release := make([]byte, 0, len(name.Release))
	for _, value := range name.Release {
		if value == 0 {
			break
		}
		release = append(release, byte(value))
	}
	return reload.ObserverHost{System: "Linux", Release: string(release)}, nil
}
