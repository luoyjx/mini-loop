// Standalone diagnostic: build from go/ using this absolute file path.
// It observes the exact Python source paths used by the paired Source process.
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
}
type result struct {
	Stage     string   `json:"stage"`
	Paths     []string `json:"paths"`
	Error     string   `json:"error"`
	ElapsedMS float64  `json:"elapsed_ms"`
}

func main() {
	root := os.Args[1]
	out := json.NewEncoder(os.Stdout)
	observer, err := reload.NewNativeEvents([]string{root}, reload.PythonSource)
	if err != nil {
		_ = out.Encode(result{Stage: "construct", Error: err.Error()})
		os.Exit(1)
	}
	defer observer.Close()
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
	_ = out.Encode(result{Stage: "constructed"})
	input := bufio.NewScanner(os.Stdin)
	for input.Scan() {
		var cmd command
		if err = json.Unmarshal(input.Bytes(), &cmd); err != nil {
			_ = out.Encode(result{Stage: "command", Error: err.Error()})
			return
		}
		start := time.Now()
		row := result{Stage: cmd.Kind}
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
				for _, p := range batch.Paths {
					if p == filepath.Join(root, "pair_ready.py") {
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
				for _, p := range batch.Paths {
					rel, e := filepath.Rel(root, p)
					if e != nil {
						err = e
						break
					}
					row.Paths = append(row.Paths, filepath.ToSlash(rel))
				}
			}
		case "close":
			err = observer.Close()
			if err != nil {
				row.Error = err.Error()
			}
			row.ElapsedMS = float64(time.Since(start)) / float64(time.Millisecond)
			_ = out.Encode(row)
			return
		default:
			row.Error = "unknown diagnostic command"
			_ = out.Encode(row)
			return
		}
		if err != nil {
			row.Error = err.Error()
		}
		row.ElapsedMS = float64(time.Since(start)) / float64(time.Millisecond)
		_ = out.Encode(row)
	}
}
