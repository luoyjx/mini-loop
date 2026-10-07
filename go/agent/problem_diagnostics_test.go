package agent

import (
	"context"
	"fmt"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/selfaudit"
	"strings"
	"testing"
	"time"
)

func TestApprovalRepeatedReviewerFaultDiagnostics(t *testing.T) {
	broker, err := NewApprovalBroker(ApprovalBrokerConfig{Timeout: time.Millisecond, Reviewer: approvalReviewerFunc(func(context.Context, ApprovalRequest) (ReviewVerdict, error) { panic("private callback data") })})
	if err != nil {
		t.Fatal(err)
	}
	initial := broker.SelfAuditProblems()
	if initial.Total.String() != "0" || len(initial.Entries) != 0 {
		t.Fatal(initial)
	}
	binding := gateTestAuthority(ModeInteractive)
	surface, _ := broker.ForSession(binding, nil)
	for i := 0; i < 3; i++ {
		allowed, err := surface.Approve(context.Background(), ApprovalRequest{binding, gateTestCall("echo x"), "r", "ask"})
		if allowed || err != nil {
			t.Fatal("fault changed approval outcome")
		}
	}
	snapshot := broker.SelfAuditProblems()
	if snapshot.Total.String() != "3" || len(snapshot.Entries) != 1 || !strings.HasSuffix((*snapshot.Summary)[0], " (x3)") || len(broker.Problems()) != 1 {
		t.Fatal("repeat count or legacy API mismatch", snapshot)
	}
	if strings.Contains(strings.Join(snapshot.Entries, " "), "private callback data") {
		t.Fatal("private panic leaked")
	}
	snapshot.Entries[0] = "changed"
	snapshot.Total.BigInt().SetInt64(0)
	if broker.SelfAuditProblems().Total.String() != "3" || broker.SelfAuditProblems().Entries[0] == "changed" {
		t.Fatal("snapshot aliases holder")
	}
}
func TestActionResultSheddingCountsEveryOccurrence(t *testing.T) {
	journal, err := NewInMemoryActionJournal(1)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		id := ActionID(fmt.Sprintf("a%d", i))
		_, err = journal.Begin(context.Background(), ActionRequest{ActionID: id, SessionID: "s", MessageID: MessageID(fmt.Sprintf("m%d", i)), ToolUseID: fmt.Sprintf("t%d", i), Input: protocol.BashToolInput(protocol.BashInput{Command: "true"})})
		if err != nil {
			t.Fatal(err)
		}
		result := "done"
		_, err = journal.Finish(context.Background(), ActionSettlement{ActionID: id, Status: ActionCompleted, Result: &result})
		if err != nil {
			t.Fatal(err)
		}
	}
	snapshot := journal.SelfAuditProblems()
	if snapshot.Total.String() != "4" || len(snapshot.Entries) != 1 || !strings.HasSuffix((*snapshot.Summary)[0], " (x4)") || len(journal.Problems()) != 1 {
		t.Fatal(snapshot)
	}
	report := selfaudit.BuildReport(selfaudit.Observations{Problems: selfaudit.GlobalLedgers{Actions: &snapshot}}, selfaudit.Scope{IncludeGlobal: true})
	if !strings.Contains(report, "### actions: 4 reported") || !strings.Contains(report, " (x4)") {
		t.Fatal(report)
	}
	record, found, err := journal.Get(context.Background(), "a0")
	if err != nil || !found || record.Status != ActionCompleted || record.Result == nil || *record.Result != ShedActionResult {
		t.Fatal("diagnostic changed replay authority")
	}
}
