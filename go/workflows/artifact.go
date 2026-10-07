package workflows

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"
)

type ArtifactInput struct {
	Run          RunID
	Node         NodeID
	Attempt      AttemptID
	Value        Value
	Schema       Value
	Verification VerificationStatus
	SchemaValid  *bool
}
type ArtifactSnapshot struct {
	ArtifactID         ArtifactID         `json:"artifact_id"`
	RunID              RunID              `json:"run_id"`
	NodeID             NodeID             `json:"node_id"`
	AttemptID          AttemptID          `json:"attempt_id"`
	Value              Value              `json:"value"`
	ContentHash        Digest             `json:"content_hash"`
	SchemaHash         Digest             `json:"schema_hash"`
	VerificationStatus VerificationStatus `json:"verification_status"`
	SchemaValid        bool               `json:"schema_valid"`
	MediaType          string             `json:"media_type"`
	CreatedAt          float64            `json:"created_at"`
}
type Artifact struct{ snapshot ArtifactSnapshot }

func (a Artifact) Snapshot() ArtifactSnapshot   { return a.snapshot }
func (a Artifact) MarshalJSON() ([]byte, error) { return json.Marshal(a.snapshot) }
func NewArtifact(input ArtifactInput) (Artifact, error) {
	status := input.Verification
	if status == "" {
		status = NotApplicable
	}
	if !status.Valid() {
		return Artifact{}, ErrDefinition
	}
	hash, err := ContentHash(input.Value, "artifact")
	if err != nil {
		return Artifact{}, err
	}
	schemaHash, err := ContentHash(input.Schema, "schema")
	if err != nil {
		return Artifact{}, err
	}
	var id [10]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Artifact{}, err
	}
	// Python takes the first twenty hex characters of a UUIDv4.
	id[6] = id[6]&0x0f | 0x40
	id[8] = id[8]&0x3f | 0x80
	valid := true
	if input.SchemaValid != nil {
		valid = *input.SchemaValid
	}
	return Artifact{ArtifactSnapshot{ArtifactID("artifact_" + hex.EncodeToString(id[:])), input.Run, input.Node, input.Attempt, input.Value, hash, schemaHash, status, valid, "application/json", float64(time.Now().UnixNano()) / 1e9}}, nil
}
