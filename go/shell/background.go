package shell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

type ProcessID int

// Started runs after native process creation and before capture admission. It
// must return promptly. A panic ends and joins the group. Foreground Interrupt excludes this
// execution; the service owns cancellation through its independent context.
type BackgroundCommand struct {
	Command string
	Timeout time.Duration
	Started func(ProcessID)
}

type executionPolicy struct {
	background bool
	timeout    time.Duration
	started    func(ProcessID)
}

func notifyStarted(callback func(ProcessID), pid ProcessID) (err error) {
	defer func() {
		if fault := recover(); fault != nil {
			err = fmt.Errorf("background start observer panicked (%T)", fault)
		}
	}()
	callback(pid)
	return nil
}

// ExecuteBackground preserves one merged stream and counts raw bytes, matching
// Python BackgroundManager rather than foreground universal-newline capture.
// Deadline/cancellation joins capture and reaps the shell; group cleanup remains
// bounded if a descendant deliberately detaches while retaining our pipe.
func (executor *Executor) ExecuteBackground(ctx context.Context, request BackgroundCommand) (BackgroundResult, error) {
	if request.Timeout == 0 {
		request.Timeout = 300 * time.Second
	}
	result, err := executor.execute(ctx, request.Command, executionPolicy{true, request.Timeout, request.Started})
	output := result.Stdout
	if result.Projection != nil {
		output = *result.Projection
	}
	return BackgroundResult{Output: output, ExitCode: result.ExitCode, TimedOut: result.TimedOut, Overflowed: result.Overflowed, DurationMS: result.DurationMS, Error: result.Error, CaptureLimitBytes: result.CaptureLimit}, err
}

// BackgroundResult has one merged stream and a byte budget. Foreground Result
// has separate streams and a decoded-character budget; the render contracts
// are distinct types rather than a caller-selected interpretation.
type BackgroundResult struct {
	Output            string
	ExitCode          *int
	TimedOut          bool
	Overflowed        bool
	DurationMS        int64
	Error             *string
	CaptureLimitBytes int
}

func (result BackgroundResult) Render() string {
	if result.Error != nil {
		return *result.Error
	}
	out := pytext.Strip(result.Output)
	rendered := capOutput(out)
	appendNote := func(note string) {
		if rendered != "" {
			rendered += "\n"
		}
		rendered += note
	}
	if result.Overflowed {
		appendNote(fmt.Sprintf("[output exceeded %s bytes; capture stopped and the command was ended]", comma(result.CaptureLimitBytes)))
	} else if result.ExitCode != nil && *result.ExitCode != 0 {
		appendNote(fmt.Sprintf("(exit %d)", *result.ExitCode))
	}
	if rendered == "" {
		return "(no output)"
	}
	return rendered
}

type byteCapture struct {
	mu         sync.Mutex
	limit      int
	data       []byte
	overflowed bool
	overflow   chan struct{}
}

func (capture *byteCapture) drain(stream *os.File) error {
	chunk := make([]byte, 64*1024)
	for {
		n, err := stream.Read(chunk)
		capture.mu.Lock()
		room := capture.limit - len(capture.data)
		capture.data = append(capture.data, chunk[:min(n, room)]...)
		over := n > room
		if over {
			capture.overflowed = true
			capture.overflow <- struct{}{}
		}
		capture.mu.Unlock()
		if over {
			return nil
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) {
				return nil
			}
			return err
		}
	}
}

func (capture *byteCapture) finish() (string, bool) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	var out strings.Builder
	for data := capture.data; len(data) > 0; {
		r, n, _ := pytext.DecodeRune(data)
		out.WriteRune(r)
		data = data[n:]
	}
	return out.String(), capture.overflowed
}
