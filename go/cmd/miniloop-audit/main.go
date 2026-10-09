// miniloop-audit audits a local deployment or public runtime health metadata.
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
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/internal/commandenv"
	"github.com/luoyjx/mini-loop/go/launcher"
	"github.com/luoyjx/mini-loop/go/secrets"
)

func execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return executeWithEnvironment(ctx, args, nil, stdout, stderr)
}

func executeWithEnvironment(ctx context.Context, args []string, environment map[string]string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "--url" {
		return executeLocal(ctx, environment, stdout, stderr)
	}
	if len(args) < 2 {
		fmt.Fprintln(stderr, "usage: miniloop-audit --url http://host:port")
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
	return findingStatus(findings)
}

func executeLocal(ctx context.Context, environment map[string]string, stdout, stderr io.Writer) int {
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	if environment == nil {
		environment = make(map[string]string)
		for name, value := range secrets.CurrentEnvironment() {
			environment[string(name)] = value
		}
	}
	settings, err := config.Load(environment, config.LoadOptions{})
	if err != nil {
		return fail(err)
	}
	findings, err := launcher.AuditDeployment(ctx, settings, environment, launcher.Options{})
	if err != nil {
		return fail(err)
	}
	text, err := audit.Render(findings)
	if err != nil {
		return fail(err)
	}
	if _, err := fmt.Fprintln(stdout, text); err != nil {
		return fail(err)
	}
	return findingStatus(findings)
}

func findingStatus(findings []audit.Finding) int {
	for _, finding := range findings {
		if finding.Severity == audit.Critical || finding.Severity == audit.High {
			return 1
		}
	}
	return 0
}

func main() {
	environment, err := commandenv.Initialize()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := executeWithEnvironment(ctx, os.Args[1:], environment.Reveal(), os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
