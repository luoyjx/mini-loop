// Diagnostic only: reuse a process while joining each native subscription.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/luoyjx/mini-loop/go/reload"
)

type command struct {
	Kind string `json:"kind"`
	Root string `json:"root"`
}

type result struct {
	Stage     string   `json:"stage"`
	PID       int      `json:"pid"`
	Opened    int      `json:"opened"`
	Closed    int      `json:"closed"`
	Paths     []string `json:"paths"`
	Error     string   `json:"error"`
	ElapsedMS float64  `json:"elapsed_ms"`
}

func main() {
	var observer *reload.NativeEvents
	var root string
	opened, closed := 0, 0
	defer func() {
		if observer != nil {
			_ = observer.Close()
		}
	}()
	out := json.NewEncoder(os.Stdout)
	input := bufio.NewScanner(os.Stdin)
	drain := func() error {
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			_, err := observer.Next(ctx)
			cancel()
			if errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			if err != nil {
				return err
			}
		}
	}
	for input.Scan() {
		var cmd command
		if err := json.Unmarshal(input.Bytes(), &cmd); err != nil {
			return
		}
		start := time.Now()
		row := result{Stage: cmd.Kind, PID: os.Getpid()}
		var err error
		switch cmd.Kind {
		case "start":
			if observer != nil {
				err = errors.New("subscription already open")
				break
			}
			root = cmd.Root
			observer, err = reload.NewNativeEvents([]string{root}, reload.PythonSource)
			if err == nil {
				opened++
			}
		case "drain", "probe", "next", "close":
			if observer == nil {
				err = errors.New("no open subscription")
				break
			}
			switch cmd.Kind {
			case "drain":
				err = drain()
			case "probe":
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				for {
					var batch reload.EventBatch
					batch, err = observer.Next(ctx)
					if err != nil {
						break
					}
					found := false
					for _, path := range batch.Paths {
						if path == filepath.Join(root, "pair_ready.py") {
							found = true
						}
					}
					if found {
						break
					}
				}
				cancel()
				if err == nil {
					err = drain()
				}
			case "next":
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				var batch reload.EventBatch
				batch, err = observer.Next(ctx)
				cancel()
				if err == nil {
					row.Paths = make([]string, 0, len(batch.Paths))
					for _, path := range batch.Paths {
						var relative string
						relative, err = filepath.Rel(root, path)
						if err != nil {
							break
						}
						row.Paths = append(row.Paths, filepath.ToSlash(relative))
					}
				}
			case "close":
				err = observer.Close()
				if err == nil {
					closed++
					observer = nil
				}
			}
		case "exit":
			if observer != nil {
				err = errors.New("exit with open subscription")
			}
		default:
			err = errors.New("unknown diagnostic command")
		}
		if err != nil {
			row.Error = err.Error()
		}
		row.Opened, row.Closed = opened, closed
		row.ElapsedMS = float64(time.Since(start)) / float64(time.Millisecond)
		if err = out.Encode(row); err != nil {
			return
		}
		if cmd.Kind == "exit" {
			return
		}
	}
}
