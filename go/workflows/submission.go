package workflows

import "github.com/luoyjx/mini-loop/go/internal/jsonvalue"

type ArtifactSubmission struct {
	value    Value
	toolName ToolName
}

func NewArtifactSubmission(value Value, toolName ToolName) ArtifactSubmission {
	return ArtifactSubmission{value, toolName}
}
func (s ArtifactSubmission) Value() Value       { return s.value }
func (s ArtifactSubmission) ToolName() ToolName { return s.toolName }

type ArtifactBinding struct {
	Run          RunID
	Node         NodeID
	Attempt      AttemptID
	Schema       Value
	Verification VerificationStatus
}

func ReturnArtifact(value Value) ArtifactSubmission {
	return ArtifactSubmission{value, "return_artifact"}
}

// ArtifactFromSubmission requires a structured worker completion. Binding comes
// from the controller's attempt, never from the worker's artifact payload.
func ArtifactFromSubmission(submission *ArtifactSubmission, binding ArtifactBinding) (Artifact, error) {
	if submission == nil {
		return Artifact{}, invalid(ArtifactFailure, "worker must finish through the synthetic return_artifact tool")
	}
	if submission.toolName != "return_artifact" {
		return Artifact{}, invalid(ArtifactFailure, "unexpected structured artifact tool")
	}
	if err := ValidateValue(binding.Schema, submission.value); err != nil {
		return Artifact{}, err
	}
	return NewArtifact(ArtifactInput{Run: binding.Run, Node: binding.Node, Attempt: binding.Attempt, Value: submission.value, Schema: binding.Schema, Verification: binding.Verification})
}
func VerificationFromValue(value Value) VerificationStatus {
	if value.Kind() != jsonvalue.Object {
		return Unverified
	}
	raw, _ := value.Lookup("status")
	text, ok := raw.Text()
	status := VerificationStatus(text)
	if !ok || !status.Valid() {
		return Unverified
	}
	return status
}
