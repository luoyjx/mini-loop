package workflows

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func recordProjection(kind string, input Value) ([]byte, *bool, error) {
	data, err := input.MarshalJSON()
	if err != nil {
		return nil, nil, err
	}
	switch kind {
	case "run":
		r, err := DecodeWorkflowRun(data)
		if err != nil {
			return nil, nil, err
		}
		if _, supplied := input.Lookup("created_at"); !supplied {
			if r.CreatedAt <= 0 {
				return nil, nil, ErrRecord
			}
			r.CreatedAt = 0
		}
		terminal := r.Terminal()
		out, err := json.Marshal(r)
		return out, &terminal, err
	case "node":
		r, err := DecodeNodeState(data)
		if err != nil {
			return nil, nil, err
		}
		out, err := json.Marshal(r)
		return out, nil, err
	case "claim":
		r, err := DecodeAttemptClaim(data)
		if err != nil {
			return nil, nil, err
		}
		out, err := json.Marshal(r)
		return out, nil, err
	case "attempt":
		r, err := DecodeNodeAttempt(data)
		if err != nil {
			return nil, nil, err
		}
		out, err := json.Marshal(r)
		return out, nil, err
	case "outbox":
		r, err := DecodeOutboxSnapshot(data)
		if err != nil {
			return nil, nil, err
		}
		if _, supplied := input.Lookup("created_at"); !supplied {
			if r.CreatedAt <= 0 {
				return nil, nil, ErrRecord
			}
			r.CreatedAt = 0
		}
		out, err := json.Marshal(r)
		return out, nil, err
	}
	return nil, nil, ErrRecord
}

func TestRecordsMatchActualPython(t *testing.T) {
	var fixture struct {
		Cases []struct {
			Kind, Name    string
			Input, Output Value
			Terminal      *bool
		}
		Errors []struct {
			Kind, Error string
			Input       Value
		}
	}
	data, err := os.ReadFile("../testdata/python-workflow-records.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Kind+"/"+row.Name, func(t *testing.T) {
			out, terminal, err := recordProjection(row.Kind, row.Input)
			if err != nil {
				t.Fatal(err)
			}
			got, err := jsonvalue.Decode(string(out))
			if err != nil {
				t.Fatal(err)
			}
			actual, _ := CanonicalJSON(got)
			want, _ := CanonicalJSON(row.Output)
			if string(actual) != string(want) {
				t.Fatalf("%s want %s", actual, want)
			}
			if (terminal == nil) != (row.Terminal == nil) || terminal != nil && *terminal != *row.Terminal {
				t.Fatal("terminal predicate changed")
			}
		})
	}
	for _, row := range fixture.Errors {
		if _, _, err := recordProjection(row.Kind, row.Input); err == nil {
			t.Fatalf("accepted Python %s for %s", row.Error, row.Kind)
		}
	}
}

func TestRecordClonesDetachEveryMutableField(t *testing.T) {
	run, err := DecodeWorkflowRun([]byte(`{"run_id":"r","definition_revision":"d","session_id":"s","idempotency_key":"k","args":{"nested":[1]},"run_context":{"actor_id":"owner","delegated_by":"parent","parent_message_id":"msg","approved_capabilities":["workflow.launch"]},"parent_run_id":"p","launch_action_id":"l","started_at":1.25,"ended_at":2.25,"active_node_ids":["n"],"workspace_baseline":"b","final_artifact_id":"a","error":"e","cancel_reason":"c"}`))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(run)
	clone := run.Clone()
	*clone.RunContext.ActorID = "other"
	*clone.RunContext.DelegatedBy = "other"
	*clone.RunContext.ParentMessageID = "other"
	clone.RunContext.ApprovedCapabilities[0] = "other"
	*clone.ParentRunID = "other"
	*clone.LaunchActionID = "other"
	*clone.StartedAt = 3.25
	*clone.EndedAt = 4.25
	clone.ActiveNodeIDs[0] = "other"
	*clone.WorkspaceBaseline = "other"
	*clone.FinalArtifactID = "other"
	*clone.Error = "other"
	*clone.CancelReason = "other"
	nested, _ := clone.Args.Lookup("nested")
	items, _ := nested.Array()
	items[0] = jsonvalue.TextValue("other")
	after, _ := json.Marshal(run)
	if string(before) != string(after) {
		t.Fatal("run clone mutation escaped")
	}

	node, _ := DecodeNodeState([]byte(`{"run_id":"r","node_id":"n","attempt_ids":["a"],"result_artifact_ids":["b"],"error":"e"}`))
	nodeCopy := node.Clone()
	nodeCopy.AttemptIDs[0] = "other"
	nodeCopy.ResultArtifactIDs[0] = "other"
	*nodeCopy.Error = "other"
	if node.AttemptIDs[0] != "a" || node.ResultArtifactIDs[0] != "b" || *node.Error != "e" {
		t.Fatal("node clone mutation escaped")
	}
	claim, _ := DecodeAttemptClaim([]byte(`{"node_id":"n","agent_id":"a","spawn_index":0,"parent_agent_id":"p"}`))
	claimCopy := claim.Clone()
	*claimCopy.ParentAgentID = "other"
	if *claim.ParentAgentID != "p" {
		t.Fatal("claim clone mutation escaped")
	}
	attempt, _ := DecodeNodeAttempt([]byte(`{"attempt_id":"a","run_id":"r","node_id":"n","attempt":1,"agent_id":"w","spawn_index":0,"parent_agent_id":"p","started_at":1.25,"heartbeat_at":2.25,"ended_at":3.25,"result_artifact_id":"f","error":"e"}`))
	attemptCopy := attempt.Clone()
	*attemptCopy.ParentAgentID = "other"
	*attemptCopy.StartedAt = 4.25
	*attemptCopy.HeartbeatAt = 5.25
	*attemptCopy.EndedAt = 6.25
	*attemptCopy.ResultArtifactID = "other"
	*attemptCopy.Error = "other"
	if *attempt.ParentAgentID != "p" || *attempt.StartedAt != 1.25 || *attempt.HeartbeatAt != 2.25 || *attempt.EndedAt != 3.25 || *attempt.ResultArtifactID != "f" || *attempt.Error != "e" {
		t.Fatal("attempt clone mutation escaped")
	}
	outbox, _ := DecodeOutboxSnapshot([]byte(`{"message_id":"m","run_id":"r","session_id":"s","kind":"custom","payload":{},"claim_token":"t","claimed_at":1.25,"delivered_at":2.25}`))
	outboxCopy := outbox.Clone()
	*outboxCopy.ClaimToken = "other"
	*outboxCopy.ClaimedAt = 3.25
	*outboxCopy.DeliveredAt = 4.25
	if *outbox.ClaimToken != "t" || *outbox.ClaimedAt != 1.25 || *outbox.DeliveredAt != 2.25 {
		t.Fatal("outbox clone mutation escaped")
	}
}

func TestRecordsRefuseMalformedNativeBoundary(t *testing.T) {
	for _, raw := range []string{
		`[]`, `{"run_id":null,"node_id":"n"}`, `{"run_id":"r"}`,
		`{"run_id":"r","node_id":"n","status":null}`,
		`{"run_id":"r","node_id":"n","attempt_ids":null}`,
		`{"run_id":"r","node_id":"n","attempt_ids":[null]}`,
		`{"run_id":"r","node_id":"n","unknown":true}`,
		`{"run_id":"r","node_id":"n","version":null}`,
		`{"run_id":"r","node_id":"n","version":1.25}`,
	} {
		if _, err := DecodeNodeState([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if _, err := DecodeWorkflowRun([]byte(`{"run_id":"r","definition_revision":"d","session_id":"s","idempotency_key":"k","run_context":{},"args":[]}`)); err == nil {
		t.Fatal("accepted non-object args")
	}
	if _, err := DecodeOutboxSnapshot([]byte(`{"message_id":"m","run_id":"r","session_id":"s","kind":"x","payload":[]}`)); err == nil {
		t.Fatal("accepted non-object payload")
	}
	if _, err := DecodeAttemptClaim([]byte(`{"node_id":"n","agent_id":"a","spawn_index":false}`)); err == nil {
		t.Fatal("accepted boolean index")
	}
}
