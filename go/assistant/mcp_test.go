package assistant

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

const mcpChildEnv = "MINILOOP_ASSISTANT_TEST_MCP_CHILD"

// TestMain lets a test spawn this binary as the mock MCP server over real
// stdio: with mcpChildEnv set to a directory it serves instead of testing.
func TestMain(m *testing.M) {
	if dir := os.Getenv(mcpChildEnv); dir != "" {
		world, err := OpenWorld(context.Background(), filepath.Join(dir, "world.db"), nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}
		err = errors.Join(ServeMCP(context.Background(), world, os.Stdin, os.Stdout), world.Close())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(4)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// flakyCaller injects transport faults into one MCP tool. lose applies the
// call on the server and then drops the answer, as a timeout after a remote
// commit would; fail drops the call before it reaches the server.
type flakyCaller struct {
	mcpCaller
	tool     string
	mu       sync.Mutex
	lose     int
	fail     int
	calls    int
	failPoll atomic.Bool
}

func (f *flakyCaller) CallTool(ctx context.Context, name string, args jsonvalue.Value) (string, error) {
	if name == MCPMailPoll && f.failPoll.Load() {
		return "", errors.New("mock server unreachable (simulated)")
	}
	if name == f.tool {
		f.mu.Lock()
		f.calls++
		lose, fail := f.lose > 0, f.lose == 0 && f.fail > 0
		if lose {
			f.lose--
		} else if fail {
			f.fail--
		}
		f.mu.Unlock()
		if fail {
			return "", errors.New("broken pipe (simulated)")
		}
		if lose {
			_, _ = f.mcpCaller.CallTool(ctx, name, args)
			return "", errors.New("connection reset after the server answered (simulated)")
		}
	}
	return f.mcpCaller.CallTool(ctx, name, args)
}

// approvedReply runs the manager preset to a pending reply and approves it.
func approvedReply(t *testing.T, app *App, clock *fixedClock) Proposal {
	t.Helper()
	ctx := context.Background()
	trigger := deliver(t, app, PresetManager)
	settle(t, app.Store)
	drafts, err := app.Store.ProposalsForTrigger(ctx, trigger)
	if err != nil || len(drafts) != 1 {
		t.Fatalf("drafts %+v %v", drafts, err)
	}
	approved, err := app.Store.DecideProposal(ctx, drafts[0].ID, true, "test", clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	return approved
}

// driveOutbox advances the fixed clock past each backoff until the proposal
// leaves approved.
func driveOutbox(t *testing.T, app *App, clock *fixedClock, id int64) Proposal {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		app.Outbox.Wake()
		time.Sleep(15 * time.Millisecond)
		p, _, err := app.Store.Proposal(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if p.Status != ProposalApproved {
			return p
		}
		clock.Set(clock.Now().Add(time.Minute))
	}
	t.Fatalf("proposal #%d stayed approved", id)
	return Proposal{}
}

func ledgerKinds(t *testing.T, app *App, kind LedgerKind) []LedgerEntry {
	t.Helper()
	entries, err := app.Store.Ledger(context.Background(), 500)
	if err != nil {
		t.Fatal(err)
	}
	var matched []LedgerEntry
	for _, entry := range entries {
		if entry.Kind == kind {
			matched = append(matched, entry)
		}
	}
	return matched
}

func TestPollerStartsAtHeadAndIngestsEachMailOnce(t *testing.T) {
	clock := newClock()
	var flaky *flakyCaller
	app, _ := startAppWith(t, t.TempDir(), clock, func(c mcpCaller) mcpCaller { flaky = &flakyCaller{mcpCaller: c}; return flaky })
	ctx := context.Background()
	inbox, err := app.Connector.ListMail(ctx, FolderInbox, 10)
	if err != nil || len(inbox) != 3 {
		t.Fatalf("seeded inbox %d %v", len(inbox), err)
	}
	if triggers, _ := app.Store.Triggers(ctx, 10); len(triggers) != 0 {
		t.Fatalf("mail that predates the daemon must not trigger: %+v", triggers)
	}
	started := false
	for _, entry := range ledgerKinds(t, app, LedgerTrigger) {
		started = started || strings.Contains(entry.Detail, "首次连接")
	}
	if !started {
		t.Fatal("ledger should record where the first poll started")
	}
	id := deliver(t, app, PresetFriend)
	if created, err := app.Poller.PollOnce(ctx); err != nil || created != 0 {
		t.Fatalf("a second poll must not re-ingest: %d %v", created, err)
	}
	if triggers := settle(t, app.Store); len(triggers) != 1 || triggers[0].ID != id || triggers[0].Kind != TriggerEmail {
		t.Fatalf("triggers %+v", triggers)
	}
	flaky.failPoll.Store(true)
	if _, err := app.Poller.PollOnce(ctx); err == nil || !strings.Contains(app.Poller.LastError(), "unreachable") {
		t.Fatalf("poll failure should surface: %v %q", err, app.Poller.LastError())
	}
	flaky.failPoll.Store(false)
	if _, err := app.Poller.PollOnce(ctx); err != nil || app.Poller.LastError() != "" {
		t.Fatalf("recovery should clear the error: %v %q", err, app.Poller.LastError())
	}
}

// The response to the first mail_send is lost after the server applied it.
// The retry reuses the proposal's key, so the server replays the receipt and
// exactly one message is sent.
func TestOutboxConvergesOnOneEffectWhenAResponseIsLost(t *testing.T) {
	clock := newClock()
	var flaky *flakyCaller
	app, _ := startAppWith(t, t.TempDir(), clock, func(c mcpCaller) mcpCaller {
		flaky = &flakyCaller{mcpCaller: c, tool: MCPMailSend, lose: 1}
		return flaky
	})
	approved := approvedReply(t, app, clock)
	done := driveOutbox(t, app, clock, approved.ID)
	if done.Status != ProposalSent || done.Attempts != 2 || done.Receipt == "" {
		t.Fatalf("proposal %+v", done)
	}
	sent, err := app.Connector.ListMail(context.Background(), FolderSent, 10)
	if err != nil || len(sent) != 1 || sent[0].ID != done.Receipt {
		t.Fatalf("exactly one external send: %+v %v", sent, err)
	}
	if failures := ledgerKinds(t, app, LedgerEffectErr); len(failures) != 1 {
		t.Fatalf("one failed attempt should be on the ledger: %+v", failures)
	}
	effects := ledgerKinds(t, app, LedgerEffect)
	if len(effects) != 1 || !strings.Contains(effects[0].Detail, "重放命中") || effects[0].ActionID != approved.ActionID || effects[0].Tool != MCPMailSend {
		t.Fatalf("effect row should record the replayed key and the gate action: %+v", effects)
	}
}

// An approved proposal whose sends keep failing survives a restart and is
// executed by the next process.
func TestOutboxResumesApprovedProposalsAfterRestart(t *testing.T) {
	clock := newClock()
	dir := t.TempDir()
	app, stop := startAppWith(t, dir, clock, func(c mcpCaller) mcpCaller {
		return &flakyCaller{mcpCaller: c, tool: MCPMailSend, fail: 100}
	})
	approved := approvedReply(t, app, clock)
	deadline := time.Now().Add(10 * time.Second)
	for {
		app.Outbox.Wake()
		p, _, _ := app.Store.Proposal(context.Background(), approved.ID)
		if p.Attempts >= 1 {
			if p.Status != ProposalApproved || !strings.Contains(p.LastError, "broken pipe") {
				t.Fatalf("a transport failure keeps the proposal approved: %+v", p)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("outbox never attempted the send")
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	clock.Set(clock.Now().Add(time.Minute))
	app, _ = startApp(t, dir, clock)
	if done := waitProposal(t, app, approved.ID, ProposalSent); done.Attempts != 2 {
		t.Fatalf("resumed proposal %+v", done)
	}
	if sent, _ := app.Connector.ListMail(context.Background(), FolderSent, 10); len(sent) != 1 {
		t.Fatalf("one send after resume: %+v", sent)
	}
}

func TestOutboxFailsServerRefusalsWithoutRetry(t *testing.T) {
	clock := newClock()
	app, _ := startApp(t, t.TempDir(), clock)
	ctx := context.Background()
	draft, _, err := app.Store.CreateProposal(ctx, Proposal{Kind: ProposalRespondInvite, TriggerID: 1, Thread: "t-x", SessionID: "s", ActionID: "a", To: "carol@acme.example", Subject: "婉拒", Body: "冲突", EventID: "e-missing", Response: InviteDecline}, clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.DecideProposal(ctx, draft.ID, true, "test", clock.Now()); err != nil {
		t.Fatal(err)
	}
	failed := waitProposal(t, app, draft.ID, ProposalFailed)
	if failed.Attempts != 1 || !strings.Contains(failed.LastError, "not found") {
		t.Fatalf("refusal should fail once: %+v", failed)
	}
	channel, _ := app.Store.Channel(ctx, 100)
	if last := channel[len(channel)-1].Text; !strings.Contains(last, "没能发出") {
		t.Fatalf("the user should hear about the failure: %q", last)
	}
}

// The mock server as a real child process: the contract check, simulator
// delivery seen through mail_poll, idempotent sends and refusals.
func TestMockMCPServerOverStdio(t *testing.T) {
	t.Setenv(mcpChildEnv, t.TempDir())
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	connector, err := NewStdioConnector([]string{self})
	if err != nil {
		t.Fatal(err)
	}
	defer connector.Close()
	ctx := context.Background()
	if tools, err := connector.Verify(ctx); err != nil || tools != len(mockTools) {
		t.Fatalf("verify: %d %v", tools, err)
	}
	head, err := connector.Poll(ctx, 0, 0)
	if err != nil || head.Head != 3 || len(head.Mail) != 0 {
		t.Fatalf("head %+v %v", head, err)
	}
	delivered, err := connector.DeliverPreset(ctx, PresetInviteConflict)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := connector.Poll(ctx, head.Head, 10)
	if err != nil || len(batch.Mail) != 1 || batch.Mail[0].Mail.ID != delivered.ID || batch.Mail[0].Mail.Category != CategoryInvite || batch.Cursor != batch.Mail[0].Seq {
		t.Fatalf("poll after delivery %+v %v", batch, err)
	}
	out := OutgoingMail{Key: "proposal-1", Thread: "t-1", To: ManagerAddress, Subject: "Re", Body: "ok"}
	first, err := connector.SendMail(ctx, out)
	if err != nil || first.Duplicate {
		t.Fatalf("first send %+v %v", first, err)
	}
	again, err := connector.SendMail(ctx, out)
	if err != nil || !again.Duplicate || again.Receipt != first.Receipt {
		t.Fatalf("replayed key should return the first receipt: %+v %v", again, err)
	}
	if sent, _ := connector.ListMail(ctx, FolderSent, 10); len(sent) != 1 {
		t.Fatalf("one message despite two calls: %+v", sent)
	}
	reply := InviteReply{Key: "proposal-1", EventID: batch.Mail[0].Mail.EventID, Response: InviteDecline}
	if _, err := connector.RespondInvite(ctx, reply); err == nil || retryable(err) {
		t.Fatalf("a key reused for another effect must be refused: %v", err)
	}
	reply.Key = "proposal-2"
	if _, err := connector.RespondInvite(ctx, reply); err != nil {
		t.Fatal(err)
	}
	if event, found, err := connector.Event(ctx, reply.EventID); err != nil || !found || event.Status != EventDeclined {
		t.Fatalf("declined invite %+v %v %v", event, found, err)
	}
	if _, err := connector.SendMail(ctx, OutgoingMail{To: ManagerAddress, Subject: "no key"}); err == nil || retryable(err) {
		t.Fatalf("a send without a key must be refused: %v", err)
	}
}

func TestServeMCPAnswersProtocolErrors(t *testing.T) {
	world, err := OpenWorld(context.Background(), filepath.Join(t.TempDir(), "world.db"), newClock().Now)
	if err != nil {
		t.Fatal(err)
	}
	defer world.Close()
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
		`not json`,
		`{"jsonrpc":"2.0","id":"x","method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"mail_read","arguments":[1]}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"mail_read","arguments":{"id":"m-seed-1","extra":1}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"mail_read","arguments":{"id":"m-seed-1"}}}`,
	}, "\n") + "\n"
	var out bytes.Buffer
	if err := ServeMCP(context.Background(), world, strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	want := [][]string{
		{`"id":1`, `"protocolVersion":"2024-11-05"`, `"name":"mock-mailcal"`},
		{`"id":null`, `"code":-32700`},
		{`"id":"x"`, `"code":-32601`},
		{`"id":2`, `"code":-32602`},
		{`"id":3`, `"isError":true`, `unknown field`},
		{`"id":4`, `"isError":false`, `Q4`},
	}
	if len(lines) != len(want) {
		t.Fatalf("want %d responses (notifications get none), got %d:\n%s", len(want), len(lines), out.String())
	}
	for i, fragments := range want {
		for _, fragment := range fragments {
			if !strings.Contains(lines[i], fragment) {
				t.Errorf("response %d lacks %s: %s", i, fragment, lines[i])
			}
		}
	}
}
