"""Synthetic structured-artifact transport for workflow workers."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Mapping

from .models import Artifact, NodeAttempt, VerificationStatus, WorkflowNode
from .validation import ArtifactValidationError, validate_json_value


@dataclass(frozen=True)
class ArtifactSubmission:
    """The value returned by the synthetic ``return_artifact`` tool."""

    value: Any
    tool_name: str = "return_artifact"


def return_artifact(value: Any) -> ArtifactSubmission:
    return ArtifactSubmission(value=value)


def artifact_from_submission(
    submission: ArtifactSubmission,
    *,
    attempt: NodeAttempt,
    node: WorkflowNode,
    verification_status: VerificationStatus = VerificationStatus.NOT_APPLICABLE,
) -> Artifact:
    if not isinstance(submission, ArtifactSubmission):
        raise ArtifactValidationError(
            "worker must finish through the synthetic return_artifact tool"
        )
    if submission.tool_name != "return_artifact":
        raise ArtifactValidationError("unexpected structured artifact tool")
    validate_json_value(node.output_schema, submission.value)
    return Artifact.create(
        run_id=attempt.run_id,
        node_id=attempt.node_id,
        attempt_id=attempt.attempt_id,
        value=submission.value,
        schema=node.output_schema,
        verification_status=verification_status,
    )


def verification_status_from_value(value: Any) -> VerificationStatus:
    if not isinstance(value, Mapping):
        return VerificationStatus.UNVERIFIED
    raw = value.get("status")
    try:
        return VerificationStatus(raw)
    except ValueError:
        return VerificationStatus.UNVERIFIED
