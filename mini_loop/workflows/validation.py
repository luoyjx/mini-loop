"""Validation for workflow definitions and structured JSON artifacts."""

from __future__ import annotations

import math
import re
from collections import deque
from collections.abc import Mapping, Sequence
from typing import Any

from .models import (
    HARD_MAX_AGENTS_PER_RUN,
    HARD_MAX_CONCURRENT_AGENTS,
    READ_ONLY_WORKFLOW_TOOLS,
    SCHEMA_VERSION,
    NodeKind,
    WorkflowDefinition,
)


_ID = re.compile(r"[A-Za-z][A-Za-z0-9._-]{0,127}")
_JSON_TYPES = {
    "object": Mapping,
    "array": list,
    "string": str,
    "integer": int,
    "number": (int, float),
    "boolean": bool,
    "null": type(None),
}
_SCHEMA_KEYWORDS = {
    "type",
    "properties",
    "required",
    "items",
    "enum",
    "const",
    "additionalProperties",
    "title",
    "description",
}
MVP_NODE_KINDS = frozenset({NodeKind.AGENT, NodeKind.VERIFY, NodeKind.REDUCE})


class WorkflowValidationError(ValueError):
    pass


class ArtifactValidationError(ValueError):
    pass


def _require(condition: bool, message: str) -> None:
    if not condition:
        raise WorkflowValidationError(message)


def validate_schema_definition(schema: Mapping[str, Any], *, path: str = "$schema") -> None:
    if not isinstance(schema, Mapping):
        raise WorkflowValidationError(f"{path} must be an object")
    unsupported = sorted(set(schema) - _SCHEMA_KEYWORDS)
    if unsupported:
        raise WorkflowValidationError(
            f"{path} contains unsupported schema keywords: {unsupported}"
        )
    declared_type = schema.get("type")
    if declared_type is not None:
        allowed = (
            declared_type
            if isinstance(declared_type, str)
            else tuple(declared_type) if isinstance(declared_type, list) else ()
        )
        allowed = (allowed,) if isinstance(allowed, str) else allowed
        if not allowed or any(item not in _JSON_TYPES for item in allowed):
            raise WorkflowValidationError(f"{path}.type is unsupported")
    required = schema.get("required", [])
    if not isinstance(required, list) or any(not isinstance(item, str) for item in required):
        raise WorkflowValidationError(f"{path}.required must be a string array")
    properties = schema.get("properties", {})
    if not isinstance(properties, Mapping):
        raise WorkflowValidationError(f"{path}.properties must be an object")
    for name, child in properties.items():
        validate_schema_definition(child, path=f"{path}.properties.{name}")
    if "items" in schema:
        validate_schema_definition(schema["items"], path=f"{path}.items")
    if "enum" in schema and not isinstance(schema["enum"], list):
        raise WorkflowValidationError(f"{path}.enum must be an array")
    if "additionalProperties" in schema and not isinstance(
        schema["additionalProperties"], bool
    ):
        raise WorkflowValidationError(
            f"{path}.additionalProperties must be a boolean"
        )
    for annotation in ("title", "description"):
        if annotation in schema and not isinstance(schema[annotation], str):
            raise WorkflowValidationError(f"{path}.{annotation} must be a string")


def validate_json_value(schema: Mapping[str, Any], value: Any, *, path: str = "$") -> None:
    """Validate the intentionally small JSON Schema subset used by the MVP."""

    validate_schema_definition(schema)
    if "enum" in schema and value not in schema["enum"]:
        raise ArtifactValidationError(f"{path} is not one of the allowed enum values")
    if "const" in schema and value != schema["const"]:
        raise ArtifactValidationError(f"{path} does not match const")

    declared = schema.get("type")
    declared_types = [declared] if isinstance(declared, str) else list(declared or [])
    if declared_types:
        matched = False
        for item in declared_types:
            expected = _JSON_TYPES[item]
            if item in {"integer", "number"} and isinstance(value, bool):
                continue
            if isinstance(value, expected):
                matched = True
                break
        if not matched:
            raise ArtifactValidationError(
                f"{path} must have type {' | '.join(declared_types)}"
            )

    if isinstance(value, float) and not math.isfinite(value):
        raise ArtifactValidationError(f"{path} must be finite")

    if isinstance(value, Mapping):
        required = schema.get("required", ())
        missing = [key for key in required if key not in value]
        if missing:
            raise ArtifactValidationError(f"{path} is missing required keys: {missing}")
        properties = schema.get("properties", {})
        if schema.get("additionalProperties") is False:
            extras = sorted(set(value) - set(properties))
            if extras:
                raise ArtifactValidationError(f"{path} has unknown keys: {extras}")
        for key, child in properties.items():
            if key in value:
                validate_json_value(child, value[key], path=f"{path}.{key}")

    if isinstance(value, list) and "items" in schema:
        for index, item in enumerate(value):
            validate_json_value(schema["items"], item, path=f"{path}[{index}]")


def validate_definition(definition: WorkflowDefinition) -> WorkflowDefinition:
    _require(
        definition.schema_version == SCHEMA_VERSION,
        f"unsupported workflow schema_version {definition.schema_version}",
    )
    _require(bool(_ID.fullmatch(definition.name)), "workflow name is invalid")
    _require(bool(definition.nodes), "workflow must contain at least one node")
    _require(
        0 < definition.budget.max_concurrent_agents <= HARD_MAX_CONCURRENT_AGENTS,
        f"max_concurrent_agents must be between 1 and {HARD_MAX_CONCURRENT_AGENTS}",
    )
    _require(
        0 < definition.budget.max_agents <= HARD_MAX_AGENTS_PER_RUN,
        f"max_agents must be between 1 and {HARD_MAX_AGENTS_PER_RUN}",
    )
    _require(definition.budget.max_rounds > 0, "max_rounds must be positive")
    _require(definition.budget.wall_time_seconds > 0, "wall_time_seconds must be positive")
    _require(
        definition.budget.token_budget is None,
        "token_budget is not implemented by the MVP",
    )

    tools = definition.policy.allowed_tools
    _require(bool(tools), "allowed_tools must not be empty")
    _require(len(set(tools)) == len(tools), "allowed_tools must not contain duplicates")
    forbidden = sorted(set(tools) - READ_ONLY_WORKFLOW_TOOLS)
    _require(not forbidden, f"workflow tools are not read-only: {forbidden}")
    _require(
        set(tools) == set(READ_ONLY_WORKFLOW_TOOLS),
        "MVP allowed_tools must be exactly read_file and glob",
    )
    _require(
        definition.policy.agent_profile == "workflow-readonly",
        "agent_profile must be workflow-readonly",
    )

    validate_schema_definition(definition.input_schema, path="input_schema")
    validate_schema_definition(definition.output_schema, path="output_schema")

    by_id = {}
    for node in definition.nodes:
        _require(bool(_ID.fullmatch(node.id)), f"invalid node id {node.id!r}")
        _require(node.id not in by_id, f"duplicate node id {node.id}")
        _require(
            node.kind in MVP_NODE_KINDS,
            f"node kind {node.kind.value} is not implemented by the MVP engine",
        )
        _require(node.max_rounds is None or node.max_rounds > 0, f"{node.id}.max_rounds")
        _require(
            node.max_rounds is None
            or node.max_rounds <= definition.budget.max_rounds,
            f"{node.id}.max_rounds exceeds workflow budget",
        )
        _require(node.items_from is None, f"{node.id}.items_from is not implemented")
        validate_schema_definition(node.output_schema, path=f"nodes.{node.id}.output_schema")
        by_id[node.id] = node

    _require(
        len(definition.nodes) <= definition.budget.max_agents,
        "definition has more nodes than max_agents",
    )
    _require(definition.return_from in by_id, "return_from references an unknown node")
    _require(
        by_id[definition.return_from].output_schema == definition.output_schema,
        "return node output_schema must match workflow output_schema",
    )

    indegree = {node_id: 0 for node_id in by_id}
    outgoing = {node_id: [] for node_id in by_id}
    for node in definition.nodes:
        _require(len(set(node.needs)) == len(node.needs), f"{node.id} has duplicate needs")
        for dependency in node.needs:
            _require(dependency in by_id, f"{node.id} needs unknown node {dependency}")
            _require(dependency != node.id, f"{node.id} cannot depend on itself")
            indegree[node.id] += 1
            outgoing[dependency].append(node.id)
        if node.items_from:
            source = node.items_from.split(".", 1)[0]
            _require(source in node.needs, f"{node.id}.items_from must reference a dependency")

    queue = deque(node_id for node_id, degree in indegree.items() if degree == 0)
    visited = 0
    while queue:
        current = queue.popleft()
        visited += 1
        for child in outgoing[current]:
            indegree[child] -= 1
            if indegree[child] == 0:
                queue.append(child)
    _require(visited == len(by_id), "workflow graph must be acyclic")
    return definition
