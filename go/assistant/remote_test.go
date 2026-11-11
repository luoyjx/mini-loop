package assistant

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRemote is an in-memory remote channel. Queued messages arrive on the
// next receive; sends are recorded and get IDs out-1, out-2, ...
type fakeRemote struct {
	name string

	mu       sync.Mutex
	queued   []RemoteInbound
	received int
	sent     []RemoteOutbound
	sendErrs []error
}

func newFakeRemote(name string) *fakeRemote { return &fakeRemote{name: name} }

func (f *fakeRemote) Name() string            { return f.name }
func (f *fakeRemote) Label() string           { return "Fake " + f.name }
func (f *fakeRemote) Account() string         { return "owner" }
func (f *fakeRemote) Interval() time.Duration { return 10 * time.Millisecond }

func (f *fakeRemote) queue(messages ...RemoteInbound) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queued = append(f.queued, messages...)
}

func (f *fakeRemote) failNext(errs ...error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendErrs = append(f.sendErrs, errs...)
}

func (f *fakeRemote) Receive(_ context.Context, cursor string) (RemoteBatch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if cursor == "" {
		// Starting at the head skips whatever waited before.
		f.queued = nil
		return RemoteBatch{Cursor: "head"}, nil
	}
	batch := RemoteBatch{Messages: f.queued, Cursor: cursor}
	f.received += len(f.queued)
	if len(f.queued) > 0 {
		batch.Cursor = "after-" + strconv.Itoa(f.received)
	}
	f.queued = nil
	return batch, nil
}

func (f *fakeRemote) Send(_ context.Context, message RemoteOutbound) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sendErrs) > 0 {
		err := f.sendErrs[0]
		f.sendErrs = f.sendErrs[1:]
		if err != nil {
			return nil, err
		}
	}
	f.sent = append(f.sent, message)
	return []string{fmt.Sprintf("out-%d", len(f.sent))}, nil
}

func (f *fakeRemote) sentTexts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	texts := make([]string, len(f.sent))
	for i, message := range f.sent {
		texts[i] = message.Text
	}
	return texts
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenStore(context.Background(), filepath.Join(t.TempDir(), "assistant.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func storeLedgerHas(t *testing.T, store *Store, kind LedgerKind, fragment string) bool {
	t.Helper()
	entries, err := store.Ledger(context.Background(), 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Kind == kind && strings.Contains(entry.Detail, fragment) {
			return true
		}
	}
	return false
}

func TestRemoteMessagesJoinTheChatThreadOnce(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	remote := newFakeRemote(RemoteTelegram)
	woken := 0
	clock := newClock()
	bridge := NewBridge(remote, store, func() { woken++ }, NewHealth(), clock.Now, nil)

	remote.queue(RemoteInbound{ID: "1", Text: "before the first start"})
	if accepted, err := bridge.ReceiveOnce(ctx); err != nil || accepted != 0 {
		t.Fatalf("a first start only pins the head: %d %v", accepted, err)
	}
	remote.queue(
		RemoteInbound{ID: "7", Text: "记住 周三下午不开会", Sender: "你"},
		RemoteInbound{Sender: "chat 99", Refused: "不是配置的私聊"},
		RemoteInbound{ID: "8", Text: "  ", Sender: "你"},
	)
	accepted, err := bridge.ReceiveOnce(ctx)
	if err != nil || accepted != 1 || woken != 1 {
		t.Fatalf("one message is accepted and wakes the router: %d %v woken=%d", accepted, err, woken)
	}
	channel, err := store.Channel(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	last := channel[len(channel)-1]
	if last.Role != RoleUser || last.Text != "记住 周三下午不开会" || last.Source != RemoteTelegram {
		t.Fatalf("the line is the user's, tagged with its channel: %+v", last)
	}
	triggers, err := store.Triggers(ctx, 10)
	if err != nil || len(triggers) != 1 || triggers[0].Kind != TriggerChat || triggers[0].Thread != ChatThread {
		t.Fatalf("the message is a chat trigger on the chat thread: %+v %v", triggers, err)
	}
	if source, err := store.ChatSource(ctx, triggers[0].Ref); err != nil || source != RemoteTelegram {
		t.Fatalf("the trigger's source is the channel: %q %v", source, err)
	}
	if !storeLedgerHas(t, store, LedgerRemote, "收到你的消息") || !storeLedgerHas(t, store, LedgerRemote, "拒收 1 条消息（chat 99：不是配置的私聊）") {
		t.Fatal("receipts and refusals are in the ledger")
	}
	if status := bridge.Status(); status.Received != 1 || status.Refused != 1 || status.LastReceived == nil {
		t.Fatalf("status %+v", status)
	}

	// A service that hands the same message out again adds nothing.
	remote.queue(RemoteInbound{ID: "7", Text: "记住 周三下午不开会", Sender: "你"})
	if accepted, err := bridge.ReceiveOnce(ctx); err != nil || accepted != 0 {
		t.Fatalf("a repeated message is skipped by its ID: %d %v", accepted, err)
	}
	if triggers, _ := store.Triggers(ctx, 10); len(triggers) != 1 {
		t.Fatalf("still one trigger: %+v", triggers)
	}
	cursor, _, _ := store.metaValue(ctx, remoteCursorKey(RemoteTelegram))
	if cursor != "after-4" {
		t.Fatalf("the receive cursor is stored with the batch: %q", cursor)
	}
}

func TestRemoteDeliveryCopiesAssistantLinesInOrder(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	clock := newClock()
	remote := newFakeRemote(RemoteDiscord)
	bridge := NewBridge(remote, store, nil, NewHealth(), clock.Now, nil)
	post := func(message ChannelMessage) int64 {
		t.Helper()
		id, err := store.Post(ctx, message, clock.Now())
		if err != nil {
			t.Fatal(err)
		}
		return id
	}

	post(ChannelMessage{Role: RoleSystem, Text: "history before the channel was enabled"})
	if sent, err := bridge.DeliverOnce(ctx); err != nil || sent != 0 {
		t.Fatalf("enabling a channel does not replay history: %d %v", sent, err)
	}
	post(ChannelMessage{Role: RoleUser, Text: "typed on the page"})
	post(ChannelMessage{Role: RoleAssistant, Text: "好的，记住了"})
	proposal := int64(5)
	post(ChannelMessage{Role: RoleAssistant, Text: "待你批准：Re: 周会改期", ProposalID: &proposal})
	if sent, err := bridge.DeliverOnce(ctx); err != nil || sent != 2 {
		t.Fatalf("assistant lines go out, the user's do not: %d %v", sent, err)
	}
	texts := remote.sentTexts()
	if texts[0] != "好的，记住了" || !strings.HasPrefix(texts[1], "待你批准：Re: 周会改期\n") || !strings.Contains(texts[1], "这里不能批准") {
		t.Fatalf("sent %q", texts)
	}
	if subject := remote.sent[0].Subject; subject != "mini-loop 助理：好的，记住了" {
		t.Fatalf("subject %q", subject)
	}

	if _, err := store.SetPaused(ctx, true, "test", clock.Now()); err != nil {
		t.Fatal(err)
	}
	post(ChannelMessage{Role: RoleAssistant, Text: "while paused"})
	if sent, _ := bridge.DeliverOnce(ctx); sent != 0 {
		t.Fatal("delivery waits while paused")
	}
	if _, err := store.SetPaused(ctx, false, "test", clock.Now()); err != nil {
		t.Fatal(err)
	}

	// A failure stops the round so order holds; the next round resends.
	remote.failNext(errors.New("network down"))
	post(ChannelMessage{Role: RoleAssistant, Text: "after resume"})
	if sent, err := bridge.DeliverOnce(ctx); err == nil || sent != 0 {
		t.Fatalf("a failed send stops the round: %d %v", sent, err)
	}
	if status := bridge.Status(); status.SendError == "" {
		t.Fatal("the failure shows on the page")
	}
	// The next round sends the pause line, the line written while paused,
	// the resume line and the last one. The service refuses the second; it
	// is skipped, not retried forever.
	remote.failNext(nil, &RemoteRejectedError{errors.New("400 bad content")})
	if sent, err := bridge.DeliverOnce(ctx); err != nil || sent != 3 {
		t.Fatalf("the round recovers and skips the refused line: %d %v", sent, err)
	}
	texts = remote.sentTexts()
	if got := texts[2:]; len(got) != 3 || got[0] != pauseText || got[1] != resumeText || got[2] != "after resume" {
		t.Fatalf("sent in channel order without the refused line: %q", texts)
	}
	if !storeLedgerHas(t, store, LedgerRemote, "服务拒收信道消息") {
		t.Fatal("a skipped line is in the ledger")
	}
	if sent, err := bridge.DeliverOnce(ctx); err != nil || sent != 0 {
		t.Fatalf("nothing is left: %d %v", sent, err)
	}
	if status := bridge.Status(); status.SendError != "" || status.Sent != 5 {
		t.Fatalf("status %+v", status)
	}
}

// A channel first started while the user has paused still delivers what was
// written during the pause once the user resumes.
func TestRemoteFirstStartWhilePausedKeepsPausedLines(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	clock := newClock()
	remote := newFakeRemote(RemoteTelegram)
	bridge := NewBridge(remote, store, nil, NewHealth(), clock.Now, nil)
	if _, err := store.SetPaused(ctx, true, "test", clock.Now()); err != nil {
		t.Fatal(err)
	}
	if sent, err := bridge.DeliverOnce(ctx); err != nil || sent != 0 {
		t.Fatalf("nothing goes out while paused: %d %v", sent, err)
	}
	if _, err := store.Post(ctx, ChannelMessage{Role: RoleAssistant, Text: "written while paused"}, clock.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPaused(ctx, false, "test", clock.Now()); err != nil {
		t.Fatal(err)
	}
	if sent, err := bridge.DeliverOnce(ctx); err != nil || sent != 2 {
		t.Fatalf("the paused line and the resume line go out: %d %v %q", sent, err, remote.sentTexts())
	}
	if texts := remote.sentTexts(); texts[0] != "written while paused" || texts[1] != resumeText {
		t.Fatalf("sent %q", texts)
	}
}

func TestRemoteReplyAnswersTheQuestion(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	clock := newClock()
	remote := newFakeRemote(RemoteEmail)
	bridge := NewBridge(remote, store, nil, NewHealth(), clock.Now, nil)
	if _, err := bridge.DeliverOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.ReceiveOnce(ctx); err != nil {
		t.Fatal(err)
	}
	asked, err := enqueueTrigger(ctx, store.db, Trigger{Kind: TriggerEmail, Thread: "mail:thread-1", Ref: "mail-1", Summary: "Dave 问哪天开会"}, clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	question, _, err := store.AskQuestion(ctx, Question{TriggerID: asked, Thread: "mail:thread-1", SessionID: "worker-1", Text: "周二还是周三？"},
		[]ChannelMessage{{Role: RoleAssistant, Text: "Dave 约会\n❓ 周二还是周三？", TriggerID: asked}}, clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Post(ctx, ChannelMessage{Role: RoleAssistant, Text: "another line", TriggerID: asked}, clock.Now()); err != nil {
		t.Fatal(err)
	}
	if sent, err := bridge.DeliverOnce(ctx); err != nil || sent != 2 {
		t.Fatalf("deliver %d %v", sent, err)
	}
	texts := remote.sentTexts()
	if !strings.HasSuffix(texts[0], remoteQuestionHint) || strings.Contains(texts[1], remoteQuestionHint) {
		t.Fatalf("only the question line invites a reply: %q", texts)
	}

	remote.queue(RemoteInbound{ID: "in-1", Text: "周三", ReplyTo: "out-1", Sender: "你"})
	if accepted, err := bridge.ReceiveOnce(ctx); err != nil || accepted != 1 {
		t.Fatalf("receive %d %v", accepted, err)
	}
	answered, _, err := store.Question(ctx, question.ID)
	if err != nil || answered.Status != QuestionAnswered || answered.Answer != "周三" {
		t.Fatalf("the reply answers the question: %+v %v", answered, err)
	}
	triggers, err := store.Triggers(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if triggers[0].Kind != TriggerAnswer || triggers[0].Thread != "mail:thread-1" {
		t.Fatalf("the answer goes back to the question's thread: %+v", triggers[0])
	}
	channel, _ := store.Channel(ctx, 10)
	if last := channel[len(channel)-1]; last.Text != fmt.Sprintf("回答提问 #%d：周三", question.ID) || last.Source != RemoteEmail {
		t.Fatalf("answer line %+v", last)
	}

	// A reply to an answered question, or to any other line, is plain chat.
	remote.queue(RemoteInbound{ID: "in-2", Text: "还是周二吧", ReplyTo: "out-1", Sender: "你"},
		RemoteInbound{ID: "in-3", Text: "收到", ReplyTo: "out-2", Sender: "你"})
	if accepted, err := bridge.ReceiveOnce(ctx); err != nil || accepted != 2 {
		t.Fatalf("receive %d %v", accepted, err)
	}
	triggers, _ = store.Triggers(ctx, 10)
	if triggers[0].Kind != TriggerChat || triggers[1].Kind != TriggerChat {
		t.Fatalf("later replies are chat: %+v", triggers[:2])
	}
}

func TestRemoteChatRunsAsTheOwnerAndRepliesThere(t *testing.T) {
	clock := newClock()
	telegram := newFakeRemote(RemoteTelegram)
	email := newFakeRemote(RemoteEmail)
	app, _ := launchApp(t, t.TempDir(), clock, appOptions{remotes: []RemoteChannel{telegram, email}})
	ctx := context.Background()
	waitFor(t, "the first receive", func() bool {
		cursor, _, _ := app.Store.metaValue(ctx, remoteCursorKey(RemoteTelegram))
		return cursor != ""
	})
	waitFor(t, "the first delivery", func() bool { _, ok, _ := app.Store.metaValue(ctx, remoteDeliveryKey(RemoteEmail)); return ok })

	telegram.queue(RemoteInbound{ID: "41", Text: "记住 周五下午不安排会", Sender: "你"})
	waitFor(t, "the replies", func() bool { return len(telegram.sentTexts()) > 0 && len(email.sentTexts()) > 0 })
	settle(t, app.Store)
	if reply := telegram.sentTexts()[0]; !strings.Contains(reply, "好的，记住了") {
		t.Fatalf("the reply reaches Telegram: %q", reply)
	}
	if reply := email.sentTexts()[0]; !strings.Contains(reply, "好的，记住了") {
		t.Fatalf("and every other remote channel: %q", reply)
	}
	for _, text := range append(telegram.sentTexts(), email.sentTexts()...) {
		if strings.Contains(text, "记住 周五") {
			t.Fatalf("the user's own line is not echoed back: %q", text)
		}
	}
	records, err := app.Memory.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, record := range records {
		found = found || strings.Contains(record.Body, "周五下午不安排会")
	}
	if !found {
		t.Fatal("the chat worker acted on the remote message")
	}
	statuses := app.Remotes()
	if len(statuses) != 2 || statuses[0].Received != 1 || statuses[0].Sent == 0 {
		t.Fatalf("statuses %+v", statuses)
	}
}

func TestSplitRunesPrefersLineBreaks(t *testing.T) {
	parts := splitRunes("第一行\n第二行很长很长", 6)
	if len(parts) != 3 || parts[0] != "第一行" || parts[1] != "第二行很长很" || parts[2] != "长" {
		t.Fatalf("parts %q", parts)
	}
	if parts := splitRunes("short", 10); len(parts) != 1 || parts[0] != "short" {
		t.Fatalf("parts %q", parts)
	}
}

func TestRemoteBaseNeedsHTTPSExceptOnLoopback(t *testing.T) {
	for base, ok := range map[string]bool{
		"":                          true,
		"https://example.test/api/": true,
		"http://127.0.0.1:8081":     true,
		"http://localhost:8081":     true,
		"http://example.test":       false,
		"ftp://127.0.0.1":           false,
		"not a url":                 false,
	} {
		got, err := remoteBase(base, "https://default.test")
		if (err == nil) != ok {
			t.Errorf("%q: %q %v", base, got, err)
		}
		if ok && strings.HasSuffix(got, "/") {
			t.Errorf("%q keeps a trailing slash: %q", base, got)
		}
	}
}
