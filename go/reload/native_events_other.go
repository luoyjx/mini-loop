//go:build !darwin || !cgo

package reload

import "errors"

func (source *NativeEvents) start(roots []string) error {
	close(source.done)
	return errors.New("native event SDK requires macOS with CGO")
}
