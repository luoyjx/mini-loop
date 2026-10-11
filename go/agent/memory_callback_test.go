package agent

import (
	"context"
	"errors"
	"fmt"
	"github.com/luoyjx/mini-loop/go/memory"
	"testing"
	"time"
)

// Arming occurs only when the main model answers, after seeding and recall.
type memoryCallbackMasker struct {
	cause                          error
	at, calls                      int
	armed, repeated, cancelOnFault bool
	cancel                         context.CancelFunc
}

func (m *memoryCallbackMasker) MaskText(text string) string {
	if m.armed {
		m.calls++
		if m.at > 0 && (m.calls == m.at || m.repeated && m.calls >= m.at) {
			if m.cancelOnFault {
				m.cancel()
			}
			if m.cause != nil {
				panic(m.cause)
			}
			panic("private-mask-fault")
		}
	}
	return text
}

func memoryCaptureContractRun(t *testing.T, session *Session, ctx context.Context) (err error) {
	t.Helper()
	defer func() {
		if recover() != nil {
			t.Error("callback panic escaped session")
			err = errors.New("capture panic escaped")
		}
	}()
	_, err = session.Run(ctx, "learn")
	return err
}

func TestMemoryCaptureCallbackAuthorityFailures(t *testing.T) {
	for _, row := range []struct {
		name  string
		cause error
		fatal bool
	}{
		{"ordinary", errors.New("private-mask-fault"), false},
		{"transcript", ErrStateTranscript, true},
		{"lease", ErrSessionLeaseLost, true},
	} {
		for _, at := range []int{1, 3} {
			t.Run(fmt.Sprintf("%s-%d", row.name, at), func(t *testing.T) {
				ctx := context.Background()
				masker := &memoryCallbackMasker{at: at, cause: fmt.Errorf("callback: %w", row.cause)}
				raw, err := memory.NewStore(ctx, t.TempDir(), masker)
				if err != nil {
					t.Fatal(err)
				}
				bound, _ := memory.Bind(raw, "owner")
				for i := 0; i < 10; i++ {
					if _, err := bound.Write(ctx, memory.Input{Name: fmt.Sprintf("seed-%d", i), Body: "fact"}); err != nil {
						t.Fatal(err)
					}
				}
				provider := &captureMemoryProvider{name: "normal", masker: masker}
				cfg := runtimeConfig(t.TempDir(), provider)
				cfg.Memory, cfg.MemoryTools, cfg.Mode, cfg.CachePolicy, cfg.Recovery = bound, true, ModeAuto, NullCachePolicy{}, DirectRecovery{}
				session, err := NewRuntimeSession(cfg)
				if err != nil {
					t.Fatal(err)
				}
				err = memoryCaptureContractRun(t, session, ctx)
				if row.fatal {
					if !errors.Is(err, row.cause) {
						t.Fatalf("authority failure lost: %v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				probe, stop := context.WithTimeout(ctx, time.Second)
				defer stop()
				if err := bound.WithLifecycle(probe, func() error { _, e := bound.List(probe); return e }); err != nil {
					t.Fatal("lifecycle not released", err)
				}
				for _, event := range session.Events() {
					if m, ok := event.Event.Memory(); ok && row.fatal && m.Action == MemoryExtract {
						t.Fatal("authority loss became successful capture")
					}
				}
			})
		}
	}
}
