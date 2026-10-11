package verifiedloop

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/improvement"
	"github.com/luoyjx/mini-loop/go/shell"
)

type workerFunc func(context.Context, string) (string, error)

func (f workerFunc) RunWorker(ctx context.Context, s string) (string, error) { return f(ctx, s) }

type acceptanceFunc func(context.Context, string) (shell.Result, error)

func (f acceptanceFunc) RunAcceptance(ctx context.Context, s string) (shell.Result, error) {
	return f(ctx, s)
}

type probeFunc func(context.Context) (*improvement.InstrumentFingerprint, error)

func (f probeFunc) ObserveIntegrity(ctx context.Context) (*improvement.InstrumentFingerprint, error) {
	return f(ctx)
}

type eventFunc func(context.Context, VerifiedEvent) error

func (f eventFunc) EmitVerifiedEvent(ctx context.Context, e VerifiedEvent) error { return f(ctx, e) }

type commandRecipe struct {
	Stdout       string  `json:"stdout"`
	Stderr       string  `json:"stderr"`
	ExitCode     *int    `json:"exit_code"`
	TimedOut     bool    `json:"timed_out"`
	Overflowed   bool    `json:"overflowed"`
	DurationMS   int64   `json:"duration_ms"`
	Error        *string `json:"error"`
	Projection   *string `json:"projection"`
	CaptureLimit int     `json:"capture_limit"`
}

func (r commandRecipe) result() shell.Result {
	return shell.Result{Stdout: r.Stdout, Stderr: r.Stderr, ExitCode: r.ExitCode, TimedOut: r.TimedOut, Overflowed: r.Overflowed, DurationMS: r.DurationMS, Error: r.Error, Projection: r.Projection, CaptureLimit: r.CaptureLimit}
}

func TestActualPythonVerifiedService(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-verified-service.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name       string            `json:"name"`
			Request    *string           `json:"request"`
			Command    *string           `json:"command"`
			Maximum    *int64            `json:"maximum"`
			Results    []commandRecipe   `json:"results"`
			Summaries  []string          `json:"summaries"`
			Probes     []*string         `json:"probes"`
			Failure    string            `json:"failure"`
			Events     []json.RawMessage `json:"events"`
			Objectives []string          `json:"objectives"`
			Trace      []string          `json:"trace"`
			Error      *string           `json:"error"`
			Outcome    *struct {
				Status     TaskStatus    `json:"status"`
				Rounds     int64         `json:"rounds"`
				Checkpoint string        `json:"checkpoint"`
				Receipts   []ReceiptSpec `json:"receipts"`
				Summary    string        `json:"summary"`
				Integrity  Integrity     `json:"integrity"`
			} `json:"outcome"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			request, command := "repair the workspace", "accept"
			if row.Request != nil {
				request = *row.Request
			}
			if row.Command != nil {
				command = *row.Command
			}
			trace, objectives := []string{}, []string{}
			events := []VerifiedEvent{}
			workers, commands, probes := 0, 0, 0
			config := ServiceConfig{RunID: "run-service", Worker: workerFunc(func(_ context.Context, objective string) (string, error) {
				trace = append(trace, "worker")
				objectives = append(objectives, objective)
				if row.Failure == "worker" {
					return "", errors.New("worker failed")
				}
				summaries := row.Summaries
				if len(summaries) == 0 {
					summaries = []string{"worker-summary"}
				}
				index := workers
				if index >= len(summaries) {
					index = len(summaries) - 1
				}
				workers++
				return summaries[index], nil
			}), Acceptance: acceptanceFunc(func(_ context.Context, received string) (shell.Result, error) {
				trace = append(trace, "acceptance")
				if received != command {
					t.Fatal("wrong command")
				}
				if row.Failure == "acceptance" {
					return shell.Result{}, errors.New("acceptance failed")
				}
				if commands >= len(row.Results) {
					t.Fatal("unexpected acceptance")
				}
				result := row.Results[commands].result()
				commands++
				return result, nil
			}), Events: eventFunc(func(_ context.Context, event VerifiedEvent) error {
				trace = append(trace, string(event.Kind()))
				if string(event.Kind()) == row.Failure {
					return errors.New(row.Failure + " failed")
				}
				events = append(events, event)
				return nil
			})}
			if row.Probes != nil {
				config.Probe = probeFunc(func(context.Context) (*improvement.InstrumentFingerprint, error) {
					trace = append(trace, "probe")
					if row.Failure == "probe" {
						return nil, errors.New("probe failed")
					}
					if probes >= len(row.Probes) {
						t.Fatal("unexpected probe")
					}
					value := row.Probes[probes]
					probes++
					if value == nil {
						return nil, nil
					}
					var sample improvement.InstrumentFingerprint
					copy(sample[:], *value)
					return &sample, nil
				})
			}
			service, err := NewService(config)
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := service.RunTask(context.Background(), request, TaskOptions{AcceptanceCommand: command, MaxRounds: row.Maximum})
			if row.Error != nil {
				if err == nil || err.Error() != *row.Error || outcome.Status != "" {
					t.Fatalf("source error %q; got %+v %v", *row.Error, outcome, err)
				}
			} else {
				if err != nil || row.Outcome == nil {
					t.Fatal(err)
				}
				want := row.Outcome
				canonical, err := outcome.Checkpoint.Canonical()
				if err != nil {
					t.Fatal(err)
				}
				specs := []ReceiptSpec{}
				for _, receipt := range outcome.Receipts {
					specs = append(specs, receipt.Spec())
				}
				if outcome.Status != want.Status || outcome.Rounds != want.Rounds || outcome.Summary != want.Summary || outcome.Integrity != want.Integrity || canonical != want.Checkpoint || !reflect.DeepEqual(specs, want.Receipts) {
					t.Fatalf("source outcome differs: %+v / %+v", outcome, want)
				}
			}
			if !reflect.DeepEqual(trace, row.Trace) || !reflect.DeepEqual(objectives, row.Objectives) || len(events) != len(row.Events) {
				t.Fatalf("source effect order differs: %v != %v", trace, row.Trace)
			}
			for i, event := range events {
				actual, err := json.Marshal(event)
				if err != nil {
					t.Fatal(err)
				}
				var got, want map[string]json.RawMessage
				if err := json.Unmarshal(actual, &got); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(row.Events[i], &want); err != nil {
					t.Fatal(err)
				}
				if len(got) != len(want) {
					t.Fatal("event keys differ")
				}
				for key, expected := range want {
					var a, b interface{}
					if err := json.Unmarshal(got[key], &a); err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal(expected, &b); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(a, b) {
						t.Fatalf("event %s: %s != %s", key, got[key], expected)
					}
				}
			}
		})
	}
}

func TestVerifiedServiceRealWorkspaceAcceptanceAndIntegrity(t *testing.T) {
	root := t.TempDir()
	executor, err := shell.New(shell.Config{Workspace: root, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tools"), 0700); err != nil {
		t.Fatal(err)
	}
	verifier := filepath.Join(root, "tools", "verify_accept")
	if err := os.WriteFile(verifier, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	rounds := 0
	prompts := []string{}
	worker := workerFunc(func(_ context.Context, objective string) (string, error) {
		prompts = append(prompts, objective)
		rounds++
		if rounds == 1 {
			if err := os.WriteFile(verifier, []byte("tampered"), 0600); err != nil {
				return "", err
			}
		} else {
			if err := os.WriteFile(verifier, []byte("original"), 0600); err != nil {
				return "", err
			}
		}
		return "executor says complete", os.WriteFile(filepath.Join(root, "right.txt"), []byte("fixed"), 0600)
	})
	service, err := NewService(ServiceConfig{RunID: "workspace", Worker: worker, Acceptance: ShellAcceptance{Executor: executor}, Probe: WorkspaceIntegrity{Workspace: root}})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := service.RunTask(context.Background(), "create right.txt", TaskOptions{AcceptanceCommand: "test -f right.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != TaskComplete || outcome.Rounds != 2 || outcome.Integrity != Clean || outcome.Receipts[0].Spec().Integrity != Suspect || outcome.Receipts[1].Spec().Verdict != Complete || !strings.Contains(prompts[1], integrityFeedback) {
		t.Fatalf("actual tamper/restore: %+v", outcome)
	}
	// Restoring during acceptance cannot undo the pre-acceptance tamper decision.
	worker = workerFunc(func(context.Context, string) (string, error) {
		return "done", os.WriteFile(verifier, []byte("tampered"), 0600)
	})
	accept := acceptanceFunc(func(ctx context.Context, command string) (shell.Result, error) {
		if err := os.WriteFile(verifier, []byte("original"), 0600); err != nil {
			return shell.Result{}, err
		}
		return (ShellAcceptance{Executor: executor}).RunAcceptance(ctx, command)
	})
	service, _ = NewService(ServiceConfig{Worker: worker, Acceptance: accept, Probe: WorkspaceIntegrity{Workspace: root}})
	maximum := int64(1)
	outcome, err = service.RunTask(context.Background(), "work", TaskOptions{AcceptanceCommand: "test -f right.txt", MaxRounds: &maximum})
	if err != nil || outcome.Status != TaskUnverified || outcome.Integrity != Suspect {
		t.Fatalf("post-run restore verified: %+v %v", outcome, err)
	}
}

func TestVerifiedServiceCallbackCancellationAndTelemetryDetachment(t *testing.T) {
	zero := 0
	worker := workerFunc(func(context.Context, string) (string, error) { return "done", nil })
	accept := acceptanceFunc(func(context.Context, string) (shell.Result, error) { return shell.Result{ExitCode: &zero}, nil })
	if _, err := NewService(ServiceConfig{}); err == nil {
		t.Fatal("missing effects accepted")
	}
	var absent *Service
	if _, err := absent.RunTask(context.Background(), "", TaskOptions{}); err == nil {
		t.Fatal("nil service accepted")
	}
	service, _ := NewService(ServiceConfig{Worker: worker, Acceptance: accept})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.RunTask(ctx, "work", TaskOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	calls := 0
	ctx, cancel = context.WithCancel(context.Background())
	service, _ = NewService(ServiceConfig{Worker: workerFunc(func(context.Context, string) (string, error) { cancel(); return "done", nil }), Acceptance: acceptanceFunc(func(context.Context, string) (shell.Result, error) {
		calls++
		return shell.Result{ExitCode: &zero}, nil
	})})
	if result, err := service.RunTask(ctx, "work", TaskOptions{}); !errors.Is(err, context.Canceled) || calls != 0 || result.Status != "" {
		t.Fatalf("late canceled success: %+v %v", result, err)
	}
	service, _ = NewService(ServiceConfig{Worker: workerFunc(func(context.Context, string) (string, error) { panic("private callback secret") }), Acceptance: accept})
	if result, err := service.RunTask(context.Background(), "work", TaskOptions{}); err == nil || strings.Contains(err.Error(), "private callback secret") || result.Status != "" {
		t.Fatal("panic escaped/fake success")
	}
	var captured VerifiedEvent
	service, _ = NewService(ServiceConfig{Worker: worker, Acceptance: accept, Events: eventFunc(func(_ context.Context, event VerifiedEvent) error {
		switch event.Kind() {
		case EventRound:
			if _, ok := event.Round(); !ok {
				t.Fatal("round variant")
			}
		case EventReceipt:
			captured = event
			view, ok := event.Receipt()
			if !ok {
				t.Fatal("receipt variant")
			}
			*view.ExitCode = 9
			zero = 9
		case EventCheckpoint:
			if view, ok := event.Checkpoint(); !ok || view.Status != TaskComplete {
				t.Fatal("checkpoint variant")
			}
		}
		return nil
	})})
	outcome, err := service.RunTask(context.Background(), "work", TaskOptions{})
	if err != nil || outcome.Status != TaskComplete {
		t.Fatal(err)
	}
	view, _ := captured.Receipt()
	if *view.ExitCode != 0 {
		t.Fatal("telemetry alias")
	}
	if _, err := json.Marshal(VerifiedEvent{}); err == nil {
		t.Fatal("unknown telemetry variant")
	}
	if _, err := (ShellAcceptance{}).RunAcceptance(context.Background(), "true"); err == nil {
		t.Fatal("nil shell accepted")
	}
	if _, err := (WorkspaceIntegrity{}).ObserveIntegrity(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
