// Package verifiedloop defines pure receipted state transitions. It installs no
// executor, verifier command, model tool, persistence or completion shortcut.
package verifiedloop

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type RequirementID string
type RunID string
type RoundID string
type ContractHash string
type Revision int64
type RequirementStatus string
type Verdict string
type Integrity string

const (
	Pending        RequirementStatus = "pending"
	Verified       RequirementStatus = "verified"
	Blocked        RequirementStatus = "blocked"
	Untrusted      RequirementStatus = "untrusted"
	Complete       Verdict           = "complete"
	Incomplete     Verdict           = "incomplete"
	VerdictBlocked Verdict           = "blocked"
	Clean          Integrity         = "clean"
	Suspect        Integrity         = "suspect"
	Violation      Integrity         = "violation"
)

func (s RequirementStatus) valid() bool {
	return s == Pending || s == Verified || s == Blocked || s == Untrusted
}
func (v Verdict) valid() bool   { return v == Complete || v == Incomplete || v == VerdictBlocked }
func (i Integrity) valid() bool { return i == Clean || i == Suspect || i == Violation }

// ContractViolation identifies a source authority rule refusal, not an IO fault.
type ContractViolation struct{ Rule string }

func (e *ContractViolation) Error() string { return e.Rule }
func refuse(text string) error             { return &ContractViolation{Rule: text} }

type Requirement struct {
	ID         RequirementID `json:"id"`
	Text       string        `json:"text"`
	Blocking   bool          `json:"blocking"`
	Acceptance string        `json:"acceptance"`
	Authority  string        `json:"authority"`
}

func NewRequirement(id RequirementID, text string) (Requirement, error) {
	if id == "" {
		return Requirement{}, refuse("a requirement needs an id")
	}
	return Requirement{ID: id, Text: text, Blocking: true, Authority: "deterministic"}, nil
}

type TaskSpec struct {
	RunID               RunID         `json:"run_id"`
	Revision            Revision      `json:"revision"`
	OriginalRequestHash string        `json:"original_request_hash"`
	Requirements        []Requirement `json:"requirements"`
	AllowedSurfaces     []string      `json:"allowed_surfaces"`
	PersistenceBoundary string        `json:"persistence_boundary"`
	ContaminationRules  []string      `json:"contamination_rules"`
}
type TaskContract struct{ spec TaskSpec }

func NewTask(spec TaskSpec) (TaskContract, error) {
	if spec.Revision < 1 {
		return TaskContract{}, refuse("contract revisions start at 1")
	}
	if len(spec.Requirements) == 0 {
		return TaskContract{}, refuse("a contract without requirements gates nothing")
	}
	ids := map[RequirementID]bool{}
	for _, r := range spec.Requirements {
		if r.ID == "" {
			return TaskContract{}, refuse("a requirement needs an id")
		}
		if ids[r.ID] {
			return TaskContract{}, refuse("requirement ids must be unique")
		}
		ids[r.ID] = true
	}
	spec.Requirements = append([]Requirement{}, spec.Requirements...)
	spec.AllowedSurfaces = append([]string{}, spec.AllowedSurfaces...)
	spec.ContaminationRules = append([]string{}, spec.ContaminationRules...)
	return TaskContract{spec}, nil
}
func (c TaskContract) Spec() TaskSpec {
	out := c.spec
	out.Requirements = append([]Requirement{}, out.Requirements...)
	out.AllowedSurfaces = append([]string{}, out.AllowedSurfaces...)
	out.ContaminationRules = append([]string{}, out.ContaminationRules...)
	return out
}
func (c TaskContract) Hash() (ContractHash, error) {
	if _, err := NewTask(c.spec); err != nil {
		return "", err
	}
	texts := []string{string(c.spec.RunID), c.spec.OriginalRequestHash}
	rows := make([]requirementTuple, len(c.spec.Requirements))
	for i, r := range c.spec.Requirements {
		rows[i] = requirementTuple(r)
		texts = append(texts, string(r.ID), r.Text, r.Acceptance, r.Authority)
	}
	if err := scalarTexts(texts...); err != nil {
		return "", err
	}
	// The source hash excludes allowed surfaces, persistence and contamination metadata.
	payload := struct {
		Original     string             `json:"original_request_hash"`
		Requirements []requirementTuple `json:"requirements"`
		Revision     Revision           `json:"revision"`
		RunID        RunID              `json:"run_id"`
	}{c.spec.OriginalRequestHash, rows, c.spec.Revision, c.spec.RunID}
	canonical, err := protocol.PythonJSON(payload, false, false)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(canonical))
	return ContractHash(hex.EncodeToString(sum[:8])), nil
}

type Artifact struct {
	Digest       string   `json:"digest"`
	Producer     string   `json:"producer"`
	EvidenceRefs []string `json:"evidence_refs"`
}
type Fact struct {
	Source    string `json:"source"`
	Content   string `json:"content"`
	Freshness string `json:"freshness"`
	Trust     string `json:"trust"`
}
type RequirementState struct {
	ID     RequirementID     `json:"id"`
	Status RequirementStatus `json:"status"`
}
type CheckpointSpec struct {
	ContractRevision Revision           `json:"contract_revision"`
	StateRevision    Revision           `json:"state_revision"`
	Requirements     []RequirementState `json:"requirements"`
	Artifacts        []Artifact         `json:"artifacts"`
	Facts            []Fact             `json:"facts"`
	Blockers         []string           `json:"blockers"`
}
type Checkpoint struct{ spec CheckpointSpec }

func cloneArtifacts(in []Artifact) []Artifact {
	out := append([]Artifact{}, in...)
	for i := range out {
		out[i].EvidenceRefs = append([]string{}, out[i].EvidenceRefs...)
	}
	return out
}
func cloneCheckpoint(in CheckpointSpec) CheckpointSpec {
	in.Requirements = append([]RequirementState{}, in.Requirements...)
	in.Artifacts = cloneArtifacts(in.Artifacts)
	in.Facts = append([]Fact{}, in.Facts...)
	in.Blockers = append([]string{}, in.Blockers...)
	return in
}
func NewCheckpoint(spec CheckpointSpec) (Checkpoint, error) {
	if spec.StateRevision < 0 {
		return Checkpoint{}, refuse("state revisions start at 0")
	}
	for _, r := range spec.Requirements {
		if !r.Status.valid() {
			return Checkpoint{}, refuse("unknown requirement status " + pytext.Repr(string(r.Status)))
		}
	}
	return Checkpoint{cloneCheckpoint(spec)}, nil
}
func (c Checkpoint) Spec() CheckpointSpec { return cloneCheckpoint(c.spec) }
func (c Checkpoint) StatusOf(id RequirementID) (RequirementStatus, bool) {
	for _, r := range c.spec.Requirements {
		if r.ID == id {
			return r.Status, true
		}
	}
	return "", false
}
func (c Checkpoint) Canonical() (string, error) {
	reqs := make([]stateTuple, len(c.spec.Requirements))
	arts := make([]artifactTuple, len(c.spec.Artifacts))
	facts := make([]factTuple, len(c.spec.Facts))
	texts := append([]string{}, c.spec.Blockers...)
	for i, r := range c.spec.Requirements {
		reqs[i] = stateTuple(r)
		texts = append(texts, string(r.ID), string(r.Status))
	}
	for i, a := range c.spec.Artifacts {
		arts[i] = artifactTuple(a)
		texts = append(texts, a.Digest, a.Producer)
		texts = append(texts, a.EvidenceRefs...)
	}
	for i, f := range c.spec.Facts {
		facts[i] = factTuple(f)
		texts = append(texts, f.Source, f.Content, f.Freshness, f.Trust)
	}
	if err := scalarTexts(texts...); err != nil {
		return "", err
	}
	payload := struct {
		Artifacts        []artifactTuple `json:"artifacts"`
		Blockers         []string        `json:"blockers"`
		ContractRevision Revision        `json:"contract_revision"`
		Facts            []factTuple     `json:"facts"`
		Requirements     []stateTuple    `json:"requirements"`
		StateRevision    Revision        `json:"state_revision"`
	}{arts, append([]string{}, c.spec.Blockers...), c.spec.ContractRevision, facts, reqs, c.spec.StateRevision}
	return protocol.PythonJSON(payload, false, false)
}

type RoundPlanSpec struct {
	RoundID             RoundID
	BaseStateRevision   Revision
	Objective           string
	Acceptance          []string
	AllowedCapabilities []string
	Budget              int64
	EvidenceRefs        []string
}
type RoundPlan struct{ spec RoundPlanSpec }

func NewRoundPlan(spec RoundPlanSpec) (RoundPlan, error) {
	if pytext.Strip(spec.Objective) == "" {
		return RoundPlan{}, refuse("a round needs its one main state change spelled out")
	}
	spec.Acceptance = append([]string{}, spec.Acceptance...)
	spec.AllowedCapabilities = append([]string{}, spec.AllowedCapabilities...)
	spec.EvidenceRefs = append([]string{}, spec.EvidenceRefs...)
	return RoundPlan{spec}, nil
}
func (p RoundPlan) Spec() RoundPlanSpec {
	out := p.spec
	out.Acceptance = append([]string{}, out.Acceptance...)
	out.AllowedCapabilities = append([]string{}, out.AllowedCapabilities...)
	out.EvidenceRefs = append([]string{}, out.EvidenceRefs...)
	return out
}

type ReceiptSpec struct {
	ContractHash        ContractHash    `json:"contract_hash"`
	RoundID             RoundID         `json:"round_id"`
	Verdict             Verdict         `json:"verdict"`
	Integrity           Integrity       `json:"integrity"`
	Coverage            []RequirementID `json:"coverage"`
	EvidenceRefs        []string        `json:"evidence_refs"`
	VerifierIDs         []string        `json:"verifier_ids"`
	WorkspaceDiffDigest string          `json:"workspace_diff_digest"`
}
type Receipt struct{ spec ReceiptSpec }

func cloneReceipt(in ReceiptSpec) ReceiptSpec {
	in.Coverage = append([]RequirementID{}, in.Coverage...)
	in.EvidenceRefs = append([]string{}, in.EvidenceRefs...)
	in.VerifierIDs = append([]string{}, in.VerifierIDs...)
	return in
}
func NewReceipt(spec ReceiptSpec) (Receipt, error) {
	if !spec.Verdict.valid() {
		return Receipt{}, refuse("unknown verdict " + pytext.Repr(string(spec.Verdict)))
	}
	if !spec.Integrity.valid() {
		return Receipt{}, refuse("unknown integrity " + pytext.Repr(string(spec.Integrity)))
	}
	return Receipt{cloneReceipt(spec)}, nil
}
func (r Receipt) Spec() ReceiptSpec { return cloneReceipt(r.spec) }
func (r Receipt) CanVerify(id RequirementID) bool {
	if r.spec.Verdict != Complete || r.spec.Integrity != Clean {
		return false
	}
	for _, covered := range r.spec.Coverage {
		if covered == id {
			return true
		}
	}
	return false
}

type operationKind uint8

const (
	opEmpty operationKind = iota
	opStatus
	opArtifact
	opFact
	opAddBlocker
	opClearBlocker
)

// Operation is a closed union; callers cannot insert tuples, arbitrary objects or
// forged mutation names. Every constructor establishes one supported payload.
type Operation struct {
	kind     operationKind
	id       RequirementID
	status   RequirementStatus
	artifact Artifact
	fact     Fact
	blocker  string
}

func SetStatus(id RequirementID, status RequirementStatus) Operation {
	return Operation{kind: opStatus, id: id, status: status}
}
func AddArtifact(artifact Artifact) Operation {
	artifact.EvidenceRefs = append([]string{}, artifact.EvidenceRefs...)
	return Operation{kind: opArtifact, artifact: artifact}
}
func AddFact(fact Fact) Operation           { return Operation{kind: opFact, fact: fact} }
func AddBlocker(blocker string) Operation   { return Operation{kind: opAddBlocker, blocker: blocker} }
func ClearBlocker(blocker string) Operation { return Operation{kind: opClearBlocker, blocker: blocker} }

type Patch struct {
	base       Revision
	operations []Operation
	receipts   []Receipt
}

func NewPatch(base Revision, operations []Operation, receipts []Receipt) Patch {
	ops := append([]Operation{}, operations...)
	for i := range ops {
		ops[i].artifact.EvidenceRefs = append([]string{}, ops[i].artifact.EvidenceRefs...)
	}
	return Patch{base: base, operations: ops, receipts: append([]Receipt{}, receipts...)}
}

// ApplyPatch is deterministic and atomic: refusal yields no partial checkpoint.
// Receipt hashes are checked before operations, including unused receipts. An
// empty successful patch still increments state revision, as in source.
func ApplyPatch(contract TaskContract, checkpoint Checkpoint, patch Patch) (Checkpoint, error) {
	if patch.base != checkpoint.spec.StateRevision {
		return Checkpoint{}, refuse(fmt.Sprintf("patch targets revision %d but the checkpoint is at %d; propose against the current state (CAS, authority rule 2)", patch.base, checkpoint.spec.StateRevision))
	}
	if checkpoint.spec.ContractRevision != contract.spec.Revision {
		return Checkpoint{}, refuse("checkpoint and contract revisions disagree")
	}
	hash, err := contract.Hash()
	if err != nil {
		return Checkpoint{}, err
	}
	for _, receipt := range patch.receipts {
		if receipt.spec.ContractHash != hash {
			return Checkpoint{}, refuse("a supporting receipt was issued against a different contract")
		}
		if _, err := NewReceipt(receipt.spec); err != nil {
			return Checkpoint{}, err
		}
	}
	out := checkpoint.Spec()
	positions := map[RequirementID]int{}
	out.Requirements = []RequirementState{}
	// Source dict collapse retains the first position and last duplicate value.
	for _, r := range checkpoint.spec.Requirements {
		if i, ok := positions[r.ID]; ok {
			out.Requirements[i] = r
		} else {
			positions[r.ID] = len(out.Requirements)
			out.Requirements = append(out.Requirements, r)
		}
	}
	for _, op := range patch.operations {
		switch op.kind {
		case opEmpty:
			return Checkpoint{}, refuse("an empty operation says nothing")
		case opStatus:
			if !op.status.valid() {
				return Checkpoint{}, refuse("unknown requirement status " + pytext.Repr(string(op.status)))
			}
			i, ok := positions[op.id]
			if !ok {
				return Checkpoint{}, refuse("no requirement " + pytext.Repr(string(op.id)) + " in the contract state")
			}
			if op.status == Verified {
				verified := false
				for _, r := range patch.receipts {
					verified = verified || r.CanVerify(op.id)
				}
				if !verified {
					return Checkpoint{}, refuse("requirement " + pytext.Repr(string(op.id)) + " may only become verified through a clean complete receipt covering it (authority rule 6)")
				}
			}
			out.Requirements[i].Status = op.status
		case opArtifact:
			out.Artifacts = append(out.Artifacts, cloneArtifacts([]Artifact{op.artifact})[0])
		case opFact:
			out.Facts = append(out.Facts, op.fact)
		case opAddBlocker:
			out.Blockers = append(out.Blockers, op.blocker)
		case opClearBlocker:
			found := -1
			for i, b := range out.Blockers {
				if b == op.blocker {
					found = i
					break
				}
			}
			if found < 0 {
				return Checkpoint{}, refuse("cannot clear a blocker that is not present: " + pytext.Repr(op.blocker))
			}
			out.Blockers = append(out.Blockers[:found], out.Blockers[found+1:]...)
		default:
			return Checkpoint{}, refuse("unknown patch operation")
		}
	}
	if out.StateRevision == Revision(math.MaxInt64) {
		return Checkpoint{}, errors.New("verified state revision exceeds the native integer range")
	}
	out.StateRevision++
	return NewCheckpoint(out)
}

// The tuple projections are transient JSON, never retained domain payloads.
func tuple(parts ...[]byte) []byte {
	out := []byte{'['}
	for i, p := range parts {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, p...)
	}
	return append(out, ']')
}
func wire[T any](value T) []byte { out, _ := json.Marshal(value); return out }

type requirementTuple Requirement

func (r requirementTuple) MarshalJSON() ([]byte, error) {
	return tuple(wire(r.ID), wire(r.Text), wire(r.Blocking), wire(r.Acceptance), wire(r.Authority)), nil
}

type stateTuple RequirementState

func (r stateTuple) MarshalJSON() ([]byte, error) { return tuple(wire(r.ID), wire(r.Status)), nil }

type artifactTuple Artifact

func (a artifactTuple) MarshalJSON() ([]byte, error) {
	return tuple(wire(a.Digest), wire(a.Producer), wire(append([]string{}, a.EvidenceRefs...))), nil
}

type factTuple Fact

func (f factTuple) MarshalJSON() ([]byte, error) {
	return tuple(wire(f.Source), wire(f.Content), wire(f.Freshness), wire(f.Trust)), nil
}
func scalarTexts(texts ...string) error {
	for _, text := range texts {
		if !utf8.ValidString(text) {
			return errors.New("verified canonical identity requires scalar UTF-8 text")
		}
	}
	return nil
}
