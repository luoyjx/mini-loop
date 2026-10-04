// Command traceview renders an exported run, stored trajectory or session to HTML.
package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/traceview"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func execute(ctx context.Context, args []string, env map[string]string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("traceview", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var output, root string
	flags.StringVar(&output, "output", "", "output HTML path")
	flags.StringVar(&output, "o", "", "output HTML path")
	flags.StringVar(&root, "root", "", "trajectory store root")
	// argparse accepts flags after the target; preserve that source CLI contract.
	reordered, targets := []string{}, []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			targets = append(targets, args[i+1:]...)
			break
		}
		if strings.HasPrefix(arg, "-") {
			reordered = append(reordered, arg)
			if arg == "-o" || arg == "--output" || arg == "-output" || arg == "--root" || arg == "-root" {
				if i+1 < len(args) {
					i++
					reordered = append(reordered, args[i])
				}
			}
		} else {
			targets = append(targets, arg)
		}
	}
	if err := flags.Parse(reordered); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if len(targets) != 1 {
		fmt.Fprintln(stderr, "traceview requires one export path, trajectory id or session id")
		return 2
	}
	target := targets[0]
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	if root == "" {
		if info, err := os.Stat(target); err != nil || !info.Mode().IsRegular() {
			settings, err := config.Load(env, config.LoadOptions{})
			if err != nil {
				return fail(err)
			}
			root = filepath.Join(settings.WorkspaceRoot, ".trajectories")
			if settings.TrajectoryRoot != nil {
				root = *settings.TrajectoryRoot
			}
		}
	}
	ledgers, err := traceview.Load(ctx, target, root)
	if err != nil {
		return fail(err)
	}
	if output == "" {
		output = strings.TrimSuffix(filepath.Base(target), filepath.Ext(target)) + ".trace.html"
	}
	file, err := os.OpenFile(output, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return fail(err)
	}
	page := traceview.Render(ledgers, "mini-loop trace · "+target, time.Now())
	_, err = io.WriteString(file, page)
	closeErr := file.Close()
	if err != nil {
		return fail(err)
	}
	if closeErr != nil {
		return fail(closeErr)
	}
	fmt.Fprintln(stdout, output)
	return 0
}
func main() { os.Exit(execute(context.Background(), os.Args[1:], nil, os.Stdout, os.Stderr)) }
