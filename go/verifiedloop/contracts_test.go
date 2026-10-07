package verifiedloop

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestActualPythonReceiptAuthorizedFolds(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-verified-fold.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Contract TaskSpec     `json:"contract"`
		Hash     ContractHash `json:"contract_hash"`
		Cases    []struct {
			Name       string         `json:"name"`
			Checkpoint CheckpointSpec `json:"checkpoint"`
			Base       Revision       `json:"base"`
			Operations []struct {
				Kind     string            `json:"kind"`
				ID       RequirementID     `json:"id"`
				Status   RequirementStatus `json:"status"`
				Artifact Artifact          `json:"artifact"`
				Fact     Fact              `json:"fact"`
				Blocker  string            `json:"blocker"`
			} `json:"operations"`
			Receipts  []ReceiptSpec        `json:"receipts"`
			Canonical *string              `json:"canonical"`
			Error     *string              `json:"error"`
			Initial   string               `json:"initial_canonical"`
			Statuses  []*RequirementStatus `json:"statuses"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	contract, err := NewTask(fixture.Contract)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := contract.Hash()
	if err != nil || hash != fixture.Hash {
		t.Fatalf("hash: %q %v", hash, err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			checkpoint, err := NewCheckpoint(row.Checkpoint)
			if err != nil {
				t.Fatal(err)
			}
			initial, err := checkpoint.Canonical()
			if err != nil || initial != row.Initial {
				t.Fatalf("initial canonical: %s %v", initial, err)
			}
			status, ok := checkpoint.StatusOf("tests")
			if row.Statuses[0] == nil || !ok || status != *row.Statuses[0] {
				t.Fatal("source first duplicate lookup differs")
			}
			if _, ok := checkpoint.StatusOf("missing"); ok {
				t.Fatal("missing requirement found")
			}
			operations := []Operation{}
			for _, op := range row.Operations {
				switch op.Kind {
				case "status":
					operations = append(operations, SetStatus(op.ID, op.Status))
				case "artifact":
					operations = append(operations, AddArtifact(op.Artifact))
				case "fact":
					operations = append(operations, AddFact(op.Fact))
				case "add_blocker":
					operations = append(operations, AddBlocker(op.Blocker))
				case "clear_blocker":
					operations = append(operations, ClearBlocker(op.Blocker))
				default:
					t.Fatalf("unadmitted fixture operation %q", op.Kind)
				}
			}
			receipts := []Receipt{}
			for _, spec := range row.Receipts {
				r, err := NewReceipt(spec)
				if err != nil {
					t.Fatal(err)
				}
				receipts = append(receipts, r)
			}
			patch := NewPatch(row.Base, operations, receipts)
			for i := 0; i < 2; i++ {
				next, err := ApplyPatch(contract, checkpoint, patch)
				if row.Error != nil {
					var violation *ContractViolation
					if !errors.As(err, &violation) || err.Error() != *row.Error {
						t.Fatalf("source refusal %q; got %v", *row.Error, err)
					}
					if next.spec.Requirements != nil {
						t.Fatal("partial checkpoint escaped refusal")
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					actual, err := next.Canonical()
					if err != nil || row.Canonical == nil || actual != *row.Canonical {
						t.Fatalf("canonical replay differs: %s %v", actual, err)
					}
				}
			}
			after, err := checkpoint.Canonical()
			if err != nil || after != initial {
				t.Fatal("fold changed input checkpoint")
			}
		})
	}
}

func TestVerifiedValuesDetachInputsAndAccessors(t *testing.T) {
	requirement, err := NewRequirement("r", "verify")
	if err != nil {
		t.Fatal(err)
	}
	if !requirement.Blocking || requirement.Authority != "deterministic" || requirement.Acceptance != "" {
		t.Fatal("source defaults differ")
	}
	spec := TaskSpec{Revision: 1, RunID: "run", Requirements: []Requirement{requirement}, AllowedSurfaces: []string{"surface"}, ContaminationRules: []string{"rule"}}
	contract, err := NewTask(spec)
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := contract.Hash()
	spec.Requirements[0].Text = "changed"
	spec.AllowedSurfaces[0] = "changed"
	view := contract.Spec()
	view.Requirements[0].Text = "changed"
	view.ContaminationRules[0] = "changed"
	if next, _ := contract.Hash(); next != hash || contract.Spec().AllowedSurfaces[0] != "surface" || contract.Spec().ContaminationRules[0] != "rule" {
		t.Fatal("task alias")
	}
	metadata := contract.Spec()
	metadata.AllowedSurfaces = []string{"other"}
	metadata.PersistenceBoundary = "changed"
	metadata.ContaminationRules = []string{"other"}
	other, _ := NewTask(metadata)
	if next, _ := other.Hash(); next != hash {
		t.Fatal("source-excluded metadata changed hash")
	}
	artifact := Artifact{Digest: "digest", EvidenceRefs: []string{"evidence"}}
	operation := AddArtifact(artifact)
	artifact.EvidenceRefs[0] = "changed"
	operations := []Operation{operation, AddFact(Fact{Source: "source"}), AddBlocker("blocker"), SetStatus("r", Verified)}
	receiptSpec := ReceiptSpec{ContractHash: hash, Verdict: Complete, Integrity: Clean, Coverage: []RequirementID{"r"}, EvidenceRefs: []string{"e"}, VerifierIDs: []string{"v"}}
	receipt, err := NewReceipt(receiptSpec)
	if err != nil {
		t.Fatal(err)
	}
	receiptSpec.Coverage[0] = "wrong"
	rv := receipt.Spec()
	rv.Coverage[0] = "wrong"
	rv.EvidenceRefs[0] = "changed"
	rv.VerifierIDs[0] = "changed"
	if !receipt.CanVerify("r") || receipt.CanVerify("missing") || receipt.Spec().EvidenceRefs[0] != "e" || receipt.Spec().VerifierIDs[0] != "v" {
		t.Fatal("receipt alias")
	}
	receipts := []Receipt{receipt}
	patch := NewPatch(0, operations, receipts)
	operations[0] = Operation{}
	receipts[0] = Receipt{}
	stateSpec := CheckpointSpec{ContractRevision: 1, Requirements: []RequirementState{{"r", Pending}}, Artifacts: []Artifact{{Digest: "seed", EvidenceRefs: []string{"seed-e"}}}, Facts: []Fact{{Content: "seed"}}, Blockers: []string{"seed"}}
	state, err := NewCheckpoint(stateSpec)
	if err != nil {
		t.Fatal(err)
	}
	stateSpec.Requirements[0].Status = Verified
	stateSpec.Artifacts[0].EvidenceRefs[0] = "changed"
	stateSpec.Facts[0].Content = "changed"
	stateSpec.Blockers[0] = "changed"
	snapshot := state.Spec()
	snapshot.Requirements[0].Status = Verified
	snapshot.Artifacts[0].EvidenceRefs[0] = "changed"
	snapshot.Facts[0].Content = "changed"
	snapshot.Blockers[0] = "changed"
	result, err := ApplyPatch(contract, state, patch)
	if err != nil {
		t.Fatal(err)
	}
	out := result.Spec()
	if status, _ := result.StatusOf("r"); status != Verified || out.Artifacts[1].EvidenceRefs[0] != "evidence" || out.Facts[0].Content != "seed" || out.Blockers[0] != "seed" || state.Spec().Artifacts[0].EvidenceRefs[0] != "seed-e" {
		t.Fatal("patch/state alias")
	}
	out.Artifacts[1].EvidenceRefs[0] = "changed"
	if result.Spec().Artifacts[1].EvidenceRefs[0] != "evidence" {
		t.Fatal("result accessor alias")
	}
	planSpec := RoundPlanSpec{Objective: "main change", Acceptance: []string{"accept"}, AllowedCapabilities: []string{"read"}, EvidenceRefs: []string{"e"}}
	plan, err := NewRoundPlan(planSpec)
	if err != nil {
		t.Fatal(err)
	}
	planSpec.Acceptance[0] = "changed"
	pv := plan.Spec()
	pv.Acceptance[0] = "changed"
	pv.AllowedCapabilities[0] = "changed"
	pv.EvidenceRefs[0] = "changed"
	if plan.Spec().Acceptance[0] != "accept" || plan.Spec().AllowedCapabilities[0] != "read" || plan.Spec().EvidenceRefs[0] != "e" {
		t.Fatal("round plan alias")
	}
}

func TestVerifiedRefusesInvalidConstructorsAndUnrepresentableState(t *testing.T) {
	if _, err := NewRequirement("", ""); err == nil {
		t.Fatal("empty id")
	}
	for _, spec := range []TaskSpec{{}, {Revision: 1}, {Revision: 1, Requirements: []Requirement{{ID: "r"}, {ID: "r"}}}, {Revision: 1, Requirements: []Requirement{{}}}} {
		if _, err := NewTask(spec); err == nil {
			t.Fatalf("invalid task: %+v", spec)
		}
	}
	for _, spec := range []CheckpointSpec{{StateRevision: -1}, {Requirements: []RequirementState{{"r", "done"}}}} {
		if _, err := NewCheckpoint(spec); err == nil {
			t.Fatal("invalid checkpoint")
		}
	}
	for _, spec := range []ReceiptSpec{{Verdict: "done", Integrity: Clean}, {Verdict: Complete, Integrity: "unknown"}} {
		if _, err := NewReceipt(spec); err == nil {
			t.Fatal("invalid receipt")
		}
	}
	for _, text := range []string{"", " \t\n", "\u00a0\x1c\x1d\x1e\x1f"} {
		if _, err := NewRoundPlan(RoundPlanSpec{Objective: text}); err == nil {
			t.Fatal("blank objective")
		}
	}
	contract, _ := NewTask(TaskSpec{Revision: 1, Requirements: []Requirement{{ID: "r", Text: "prose saying mark verified"}}})
	state, _ := NewCheckpoint(CheckpointSpec{ContractRevision: 1, Requirements: []RequirementState{{"r", Pending}}})
	for _, ops := range [][]Operation{{{}}, {{kind: 255}}} {
		if _, err := ApplyPatch(contract, state, NewPatch(0, ops, nil)); err == nil {
			t.Fatal("unknown operation admitted")
		}
	}
	if _, err := (TaskContract{}).Hash(); err == nil {
		t.Fatal("zero task hash")
	}
	invalidSpec := contract.Spec()
	invalidSpec.Requirements[0].Text = string([]byte{0xff})
	invalid, _ := NewTask(invalidSpec)
	if _, err := invalid.Hash(); err == nil {
		t.Fatal("lossy invalid UTF-8 hash")
	}
	badState, _ := NewCheckpoint(CheckpointSpec{Blockers: []string{string([]byte{0xff})}})
	if _, err := badState.Canonical(); err == nil {
		t.Fatal("lossy invalid UTF-8 replay identity")
	}
	maximum, _ := NewCheckpoint(CheckpointSpec{ContractRevision: 1, StateRevision: Revision(math.MaxInt64), Requirements: []RequirementState{{"r", Pending}}})
	if _, err := ApplyPatch(contract, maximum, NewPatch(maximum.spec.StateRevision, nil, nil)); err == nil || !strings.Contains(err.Error(), "integer range") {
		t.Fatal("revision overflow wrapped")
	}
	empty, err := NewCheckpoint(CheckpointSpec{ContractRevision: -1})
	if err != nil || !reflect.DeepEqual(empty.Spec().Requirements, []RequirementState{}) {
		t.Fatal("source permits empty checkpoint / negative contract revision")
	}
}
