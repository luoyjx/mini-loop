package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/userresources"
)

type skillCaptureProvider struct{ text string }

func (p skillCaptureProvider) Complete(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
	return fakeReply([]protocol.Block{protocol.NewTextBlock(p.text)}, protocol.StopEndTurn), nil
}

func TestManagedSkillCaptureRequiresAdmittedCapabilityAndCompletedTurn(t *testing.T) {
	const secret = "capture-private-secret"
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("TOKEN", secret)
	cfg := runtimeConfig(t.TempDir(), skillCaptureProvider{" " + secret + " "})
	cfg.Secrets = registry
	session, err := NewManagedSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(context.Background(), "ordinary"); err != nil {
		t.Fatal(err)
	}
	if session.SkillCapture().Established {
		t.Fatal("ordinary run minted provenance")
	}
	run, err := AuthenticatedHTTPRunContext("owner")
	if err != nil {
		t.Fatal(err)
	}
	peer, err := run.DerivePeerAgent("peer")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.RunWithContext(context.Background(), "peer", peer); err != nil {
		t.Fatal(err)
	}
	if session.SkillCapture().Established {
		t.Fatal("peer inherited capture authority")
	}
	if _, err := session.RunWithContext(context.Background(), "\u0085<runtime-state>human "+secret+" ", run); err != nil {
		t.Fatal(err)
	}
	state := session.SkillCapture()
	if !state.Established || state.Error != "" || len(state.Messages) != 2 || state.Messages[0].Content != "<runtime-state>human "+secrets.Mask || state.Messages[1].Content != secrets.Mask {
		t.Fatal("completed admitted pair missing or not masked", state)
	}
	state.Messages[0].Content = "changed"
	if session.SkillCapture().Messages[0].Content == "changed" {
		t.Fatal("ledger view aliases session")
	}
	if _, err := session.RunWithContext(context.Background(), "   ", run); err != nil {
		t.Fatal(err)
	}
	if len(session.SkillCapture().Messages) != 2 {
		t.Fatal("blank user captured")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := session.RunWithContext(ctx, "cancelled", run); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(session.SkillCapture().Messages) != 2 {
		t.Fatal("failed admission captured")
	}
}

func TestManagedSkillCaptureCancellationAndScreeningLatch(t *testing.T) {
	run, err := AuthenticatedHTTPRunContext("owner")
	if err != nil {
		t.Fatal(err)
	}
	parked := &parkedProvider{started: make(chan struct{})}
	session := managedForTest(t, parked)
	done := make(chan error, 1)
	go func() { _, err := session.RunWithContext(context.Background(), "cancel me", run); done <- err }()
	<-parked.started
	if ok, err := session.Cancel(context.Background(), "operator"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if session.SkillCapture().Established {
		t.Fatal("cancelled turn captured")
	}
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("TOKEN", "tiny")
	cfg := runtimeConfig(t.TempDir(), skillCaptureProvider{"answer"})
	cfg.Secrets = registry
	session, err = NewManagedSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if final, err := session.RunWithContext(context.Background(), "human", run); final != "answer" || err != nil {
		t.Fatal("capture screening changed completed turn", final, err)
	}
	if state := session.SkillCapture(); state.Established || state.Error != userresources.CaptureScreeningUnavailable {
		t.Fatal(state)
	}
	registry.RegisterValue("TOKEN", "healthy-replacement-secret")
	if _, err := session.RunWithContext(context.Background(), "healthy", run); err != nil {
		t.Fatal(err)
	}
	if state := session.SkillCapture(); len(state.Messages) != 2 || state.Error != userresources.CaptureScreeningUnavailable {
		t.Fatal("latch was cleared", state)
	}
}

func TestManagedSkillCaptureFollowsTerminalCommit(t *testing.T) {
	sink := terminalSink{make(chan struct{}), make(chan struct{})}
	cfg := runtimeConfig(t.TempDir(), skillCaptureProvider{"answer"})
	cfg.EventSink = sink
	session, err := NewManagedSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	run, err := AuthenticatedHTTPRunContext("owner")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := session.RunWithContext(context.Background(), "human", run); done <- err }()
	<-sink.started
	before := session.SkillCapture()
	close(sink.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if before.Established || len(session.SkillCapture().Messages) != 2 {
		t.Fatal("capture did not follow terminal commitment", before)
	}
}
