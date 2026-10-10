//go:build !linux && (!darwin || !cgo)

package reload

import (
	"context"
	"errors"
)

func (source *NativeEvents) start(ctx context.Context, roots []string) error {
	close(source.done)
	return errors.New("native event SDK requires Linux or macOS with CGO")
}
