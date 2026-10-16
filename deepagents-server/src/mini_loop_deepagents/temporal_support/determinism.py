"""Pinned Deep Agents adapters shared by Task execution and the fault prototype.

Only Workflow-clock file metadata and missing tool-result IDs are adapted. The
classes keep the original prototype behavior; they do not schedule Activities,
open a database, or import either execution profile.
"""

from __future__ import annotations

import base64
from dataclasses import replace
from typing import Any

from temporalio import workflow

with workflow.unsafe.imports_passed_through():
    from deepagents.backends import StateBackend
    from deepagents.backends.protocol import EditResult, FileData, FileUploadResponse, WriteResult
    from deepagents.backends.utils import file_data_to_string, perform_string_replacement
    from langchain.agents.middleware import AgentMiddleware
    from langchain_core.messages import ToolMessage
    from langgraph.types import Command


class DeterministicStateBackend(StateBackend):
    """Pinned Deep Agents 0.7.25 StateBackend with Workflow-clock file metadata.

    `_read_files`, `_send_files_update`, `_prepare_for_storage` are experimental
    private seams. The real built-in file tools still drive the real StateBackend
    channels. Never install a global clock monkeypatch or disable the sandbox.
    """

    def _prepare_for_storage(self, file_data: FileData) -> dict[str, Any]:
        value = super()._prepare_for_storage(file_data)
        now = workflow.now().isoformat()
        if not value.get("created_at"):
            value["created_at"] = now
        value["modified_at"] = now
        return value

    def write(self, file_path: str, content: str) -> WriteResult:
        # Mirror the pinned write semantics without calling datetime.now at all.
        existing = self._read_files().get(file_path) or {}
        now = workflow.now().isoformat()
        value: FileData = {"content": content, "encoding": "utf-8",
                           "created_at": existing.get("created_at") or now, "modified_at": now}
        self._send_files_update({file_path: self._prepare_for_storage(value)})
        return WriteResult(path=file_path)

    def edit(self, file_path: str, old_string: str, new_string: str, replace_all: bool = False) -> EditResult:
        # Keep pinned replacement/error semantics, replacing only its clock seam.
        file_data = self._read_files().get(file_path)
        if file_data is None:
            return EditResult(error=f"Error: File '{file_path}' not found")
        result = perform_string_replacement(file_data_to_string(file_data), old_string, new_string, replace_all)
        if isinstance(result, str):
            return EditResult(error=result)
        content, occurrences = result
        value: FileData = {"content": content, "encoding": file_data.get("encoding", "utf-8")}
        if "created_at" in file_data:
            value["created_at"] = file_data["created_at"]
        self._send_files_update({file_path: self._prepare_for_storage(value)})
        return EditResult(path=file_path, occurrences=int(occurrences))

    def upload_files(self, files: list[tuple[str, bytes]]) -> list[FileUploadResponse]:
        # StateBackend.upload_files bypasses _prepare_for_storage in 0.7.25.
        existing, update, responses = self._read_files(), {}, []
        for path, content in files:
            try:
                text, encoding = content.decode("utf-8"), "utf-8"
            except UnicodeDecodeError:
                text, encoding = base64.b64encode(content).decode("ascii"), "base64"
            value: FileData = {"content": text, "encoding": encoding}
            if (existing.get(path) or {}).get("created_at"):
                value["created_at"] = existing[path]["created_at"]
            update[path] = self._prepare_for_storage(value)
            responses.append(FileUploadResponse(path=path, error=None))
        if update:
            self._send_files_update(update)
        return responses


class StableToolMessageIDs(AgentMiddleware):
    """Give missing native tool-result IDs a stable identity before reduction.

    The second fault run proved that native ToolMessages with no ID receive new
    UUIDs during each cold replay. This public middleware preserves the native
    tool/Command result and every field, supplying only a missing message ID.
    It does not rewrite or normalize snapshots after the fact.
    """

    def __init__(self, workflow_id: str) -> None:
        super().__init__()
        if not workflow_id:
            raise ValueError("A stable workflow ID is required")
        self.workflow_id = workflow_id

    def _message(self, message: Any, call_id: str, ordinal: int) -> Any:
        if isinstance(message, ToolMessage) and message.id is None:
            return message.model_copy(update={"id": f"{self.workflow_id}:tool:{call_id}:{ordinal}"})
        return message

    def _result(self, result: Any, call_id: str) -> Any:
        if isinstance(result, ToolMessage):
            return self._message(result, call_id, 0)
        if isinstance(result, Command) and isinstance(result.update, dict):
            messages = result.update.get("messages")
            if isinstance(messages, list):
                update = {**result.update, "messages": [
                    self._message(message, call_id, ordinal) for ordinal, message in enumerate(messages)
                ]}
                return replace(result, update=update)
        return result

    def wrap_tool_call(self, request: Any, handler: Any) -> Any:
        return self._result(handler(request), request.tool_call["id"])

    async def awrap_tool_call(self, request: Any, handler: Any) -> Any:
        return self._result(await handler(request), request.tool_call["id"])
