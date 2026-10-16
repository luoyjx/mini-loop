"""Bounded native saver transport for trusted TaskWorkflowV2 continuations.

Record the public saver write operations, including task_path and every
DeltaChannel ancestor. Replaying these operations preserves native routing and
private middleware state without interpreting a checkpoint's internal channels.
This codec accepts only internal Workflow History, never public HTTP input.
"""

from __future__ import annotations

import base64
import hashlib
import json
from typing import Any
import zlib

from langgraph.checkpoint.base import get_checkpoint_metadata
from langgraph.checkpoint.memory import InMemorySaver
from langgraph.checkpoint.serde.jsonplus import JsonPlusSerializer


MAX_RAW_BYTES = 16 * 1024 * 1024
MAX_COMPRESSED_BYTES = 384 * 1024
MAX_ENVELOPE_BYTES = 512 * 1024
SCHEMA_VERSION = 1


class CheckpointLimitError(ValueError):
    """A continuation exceeded its fixed serialization resource budget."""


class InvalidCheckpointError(ValueError):
    """Internal continuation data does not match its native transport contract."""


def envelope_size(value: Any) -> int:
    return len(json.dumps(value, ensure_ascii=False, separators=(",", ":"), allow_nan=False).encode("utf-8"))


def require_envelope_budget(value: Any) -> None:
    if envelope_size(value) > MAX_ENVELOPE_BYTES:
        raise CheckpointLimitError("Task continuation envelope budget exceeded")


def _config(config: dict[str, Any], thread_id: str) -> dict[str, Any]:
    configurable = config.get("configurable", {})
    if configurable.get("thread_id") != thread_id:
        raise InvalidCheckpointError("Checkpoint thread identity mismatch")
    result = {"thread_id": thread_id, "checkpoint_ns": configurable.get("checkpoint_ns", "")}
    if not isinstance(result["checkpoint_ns"], str):
        raise InvalidCheckpointError("Invalid checkpoint namespace")
    if configurable.get("checkpoint_id") is not None:
        if not isinstance(configurable["checkpoint_id"], str):
            raise InvalidCheckpointError("Invalid checkpoint identity")
        result["checkpoint_id"] = configurable["checkpoint_id"]
    return {"configurable": result}


class PortableSaver(InMemorySaver):
    """In-memory saver with a public-operation journal and bounded export.

    InMemorySaver's async methods delegate to these public synchronous hooks.
    Each record is serialized immediately, so later mutation cannot alter it.
    No storage, blob, or writes implementation attributes are inspected.
    """

    def __init__(self, thread_id: str):
        if not isinstance(thread_id, str) or not thread_id:
            raise InvalidCheckpointError("A checkpoint thread identity is required")
        super().__init__(serde=JsonPlusSerializer(pickle_fallback=False))
        self.thread_id = thread_id
        self._journal: list[tuple[str, bytes]] = []
        # BaseCheckpointSaver.with_allowlist returns a shallow saver clone.
        # Share the journal counter with that clone as well as the record list.
        self._journal_bytes = [0]
        self._restoring = False

    def _record(self, operation: dict[str, Any]) -> None:
        if self._restoring:
            return
        kind, raw = self.serde.dumps_typed(operation)
        if kind != "msgpack":
            raise InvalidCheckpointError("Native checkpoint transport requires msgpack")
        if self._journal_bytes[0] + len(raw) > MAX_RAW_BYTES:
            raise CheckpointLimitError("Task checkpoint raw budget exceeded")
        self._journal.append((kind, raw))
        self._journal_bytes[0] += len(raw)

    def put(self, config, checkpoint, metadata, new_versions):
        effective_metadata = get_checkpoint_metadata(config, metadata)
        reduced = _config(config, self.thread_id)
        self._record({"operation": "put", "config": reduced, "checkpoint": checkpoint,
                      "metadata": effective_metadata, "new_versions": new_versions})
        return super().put(config, checkpoint, metadata, new_versions)

    def put_writes(self, config, writes, task_id, task_path=""):
        writes = list(writes)
        self._record({"operation": "writes", "config": _config(config, self.thread_id),
                      "writes": writes, "task_id": task_id, "task_path": task_path})
        return super().put_writes(config, writes, task_id, task_path)

    def export(self) -> dict[str, Any]:
        if not self._journal:
            raise InvalidCheckpointError("Cannot export an empty checkpoint journal")
        kind, raw = self.serde.dumps_typed(self._journal)
        if kind != "msgpack":
            raise InvalidCheckpointError("Native checkpoint transport requires msgpack")
        if len(raw) > MAX_RAW_BYTES:
            raise CheckpointLimitError("Task checkpoint raw budget exceeded")
        compressed = zlib.compress(raw)
        if len(compressed) > MAX_COMPRESSED_BYTES:
            raise CheckpointLimitError("Task checkpoint compressed budget exceeded")
        return {"schema_version": SCHEMA_VERSION, "thread_id": self.thread_id,
                "codec": "zlib-msgpack", "raw_bytes": len(raw), "records": len(self._journal),
                "sha256": hashlib.sha256(raw).hexdigest(),
                "data": base64.b64encode(compressed).decode("ascii")}

    @classmethod
    def restore(cls, snapshot: Any, thread_id: str) -> PortableSaver:
        if (isinstance(snapshot, dict) and type(snapshot.get("raw_bytes")) is int
                and snapshot["raw_bytes"] > MAX_RAW_BYTES):
            raise CheckpointLimitError("Task checkpoint raw budget exceeded")
        fields = {"schema_version", "thread_id", "codec", "raw_bytes", "records", "sha256", "data"}
        if (not isinstance(snapshot, dict) or set(snapshot) != fields
                or type(snapshot["schema_version"]) is not int or snapshot["schema_version"] != SCHEMA_VERSION
                or snapshot["thread_id"] != thread_id or snapshot["codec"] != "zlib-msgpack"
                or type(snapshot["raw_bytes"]) is not int or not 0 < snapshot["raw_bytes"] <= MAX_RAW_BYTES
                or type(snapshot["records"]) is not int or not 0 < snapshot["records"] <= 100000
                or not isinstance(snapshot["sha256"], str) or len(snapshot["sha256"]) != 64
                or not isinstance(snapshot["data"], str)):
            raise InvalidCheckpointError("Invalid checkpoint envelope")
        if len(snapshot["data"]) > 4 * ((MAX_COMPRESSED_BYTES + 2) // 3):
            raise CheckpointLimitError("Task checkpoint compressed budget exceeded")
        try:
            compressed = base64.b64decode(snapshot["data"], validate=True)
            if len(compressed) > MAX_COMPRESSED_BYTES:
                raise CheckpointLimitError("Task checkpoint compressed budget exceeded")
            decompressor = zlib.decompressobj()
            raw = decompressor.decompress(compressed, MAX_RAW_BYTES + 1)
            if len(raw) > MAX_RAW_BYTES or decompressor.unconsumed_tail:
                raise CheckpointLimitError("Task checkpoint raw budget exceeded")
            if (not decompressor.eof or decompressor.unused_data or len(raw) != snapshot["raw_bytes"]
                    or hashlib.sha256(raw).hexdigest() != snapshot["sha256"]):
                raise InvalidCheckpointError("Checkpoint integrity mismatch")
            saver = cls(thread_id)
            journal = saver.serde.loads_typed(("msgpack", raw))
            if not isinstance(journal, list) or len(journal) != snapshot["records"]:
                raise InvalidCheckpointError("Invalid checkpoint journal")
            saver._restoring = True
            for entry in journal:
                if (not isinstance(entry, (tuple, list)) or len(entry) != 2
                        or entry[0] != "msgpack" or not isinstance(entry[1], bytes)):
                    raise InvalidCheckpointError("Invalid checkpoint record")
                operation = saver.serde.loads_typed((entry[0], entry[1]))
                if not isinstance(operation, dict):
                    raise InvalidCheckpointError("Invalid checkpoint operation")
                reduced = _config(operation.get("config", {}), thread_id)
                if operation.get("operation") == "put" and set(operation) == {"operation", "config", "checkpoint", "metadata", "new_versions"}:
                    if not all(isinstance(operation[key], dict) for key in ("checkpoint", "metadata", "new_versions")):
                        raise InvalidCheckpointError("Invalid native checkpoint")
                    saver.put(reduced, operation["checkpoint"], operation["metadata"], operation["new_versions"])
                elif operation.get("operation") == "writes" and set(operation) == {"operation", "config", "writes", "task_id", "task_path"}:
                    if (not isinstance(operation["writes"], (tuple, list))
                            or not isinstance(operation["task_id"], str) or not isinstance(operation["task_path"], str)):
                        raise InvalidCheckpointError("Invalid pending writes")
                    saver.put_writes(reduced, operation["writes"], operation["task_id"], operation["task_path"])
                else:
                    raise InvalidCheckpointError("Invalid checkpoint operation")
            saver._journal = [(entry[0], entry[1]) for entry in journal]
            saver._journal_bytes[0] = sum(len(entry[1]) for entry in journal)
            saver._restoring = False
            return saver
        except (CheckpointLimitError, InvalidCheckpointError):
            raise
        except Exception:
            raise InvalidCheckpointError("Invalid checkpoint encoding") from None
