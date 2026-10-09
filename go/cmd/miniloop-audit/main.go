// miniloop-audit audits public runtime posture reported by a running server.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/luoyjx/mini-loop/go/audit"
)

func execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "--url" || len(args) < 2 {
		fmt.Fprintln(stderr, "usage: miniloop-audit --url http://host:port (local audit is not implemented)")
		return 2
	}
	base := strings.TrimRight(args[1], "/")
	report, err := audit.Fetch(ctx, base, nil)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	build, pid := "None", "None"
	if report.Build != nil {
		build = *report.Build
	}
	if report.PID != nil {
		pid = strconv.Itoa(*report.PID)
	}
	findings := audit.Remote(report, base)
	text, err := audit.Render(findings)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if _, err := fmt.Fprintf(stdout, "auditing %s  build=%s pid=%s\n%s\n", base, build, pid, text); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, finding := range findings {
		if finding.Severity == audit.Critical || finding.Severity == audit.High {
			return 1
		}
	}
	return 0
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := execute(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
