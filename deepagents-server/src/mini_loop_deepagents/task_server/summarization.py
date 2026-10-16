"""Pinned Deep Agents summary behavior with Temporal-owned clock and IDs.

These narrow private hooks match Deep Agents 0.7.25. Keep native summary model
calls, cutoff selection, media handling, backend effects and private state. This
adapter changes neither global datetime/UUID functions nor the original v1 graph.
"""

from __future__ import annotations

import hashlib
import json
import logging
from typing import Any

from temporalio import workflow

with workflow.unsafe.imports_passed_through():
    from deepagents.middleware.summarization import (
        SUMMARIZATION_SESSION_ID_KEY,
        SummarizationMiddleware,
    )
    from deepagents.middleware.patch_tool_calls import PatchToolCallsMiddleware
    from langchain_core.messages import get_buffer_string
    from mini_loop_deepagents.temporal_support.determinism import DeterministicStateBackend, StableToolMessageIDs

LOG = logging.getLogger(__name__)
SUMMARY_TRIGGER = [("messages", 40), ("tokens", 16000)]
SUMMARY_KEEP = ("messages", 12)


class StablePatchToolCalls(StableToolMessageIDs, PatchToolCallsMiddleware):
    """Preserve native dangling-call repair and stable IDs in parent and GP."""

    @property
    def name(self) -> str:
        return "PatchToolCallsMiddleware"

    def before_agent(self, state: Any, runtime: Any) -> Any:
        update = PatchToolCallsMiddleware.before_agent(self, state, runtime)
        if update and isinstance(update.get("messages"), list):
            return {**update, "messages": [
                self._message(message, getattr(message, "tool_call_id", ""), ordinal)
                for ordinal, message in enumerate(update["messages"])
            ]}
        return update


class DeterministicSummarization(SummarizationMiddleware):
    """Use the same native summary slot in both the parent and GP subagent."""

    def __init__(self, workflow_id: str, model: Any, backend: Any) -> None:
        self.workflow_id = workflow_id
        super().__init__(model=model, backend=backend, trigger=SUMMARY_TRIGGER, keep=SUMMARY_KEEP)

    @property
    def name(self) -> str:
        # create_deep_agent replaces the default by name and carries that slot
        # into the real default general-purpose subagent.
        return "SummarizationMiddleware"

    def _get_session_id(self, state: Any) -> str:
        existing = state.get(SUMMARIZATION_SESSION_ID_KEY)
        if isinstance(existing, str) and existing:
            return existing
        return f"session_{workflow.uuid4().hex}"

    def _build_new_messages_with_path(self, summary: str, file_path: str | None) -> list[Any]:
        messages = super()._build_new_messages_with_path(summary, file_path)
        for message in messages:
            if message.id is None:
                canonical = json.dumps(message.model_dump(mode="json"), sort_keys=True,
                                       ensure_ascii=False, separators=(",", ":"), allow_nan=False)
                message.id = f"{self.workflow_id}:summary:{hashlib.sha256(canonical.encode()).hexdigest()}"
        return messages

    def _section(self, messages: list[Any]) -> str:
        filtered = self._filter_summary_messages(messages)
        return f"## Summarized at {workflow.now().isoformat()}\n\n{get_buffer_string(filtered, format='xml')}\n\n"

    @staticmethod
    def _existing(responses: Any) -> str:
        if responses and responses[0].content is not None and responses[0].error is None:
            return responses[0].content.decode("utf-8")
        return ""

    def _offload_to_backend(self, backend: Any, messages: list[Any], session_id: str) -> str | None:
        # Preserve pinned native best-effort append semantics; only the section
        # timestamp differs. Error text and task content are not logged here.
        path = self._get_history_path(session_id)
        section = self._section(messages)
        try:
            existing = self._existing(backend.download_files([path]))
        except Exception:
            existing = ""
        try:
            result = backend.edit(path, existing, existing + section) if existing else backend.write(path, section)
            if result is not None and not result.error:
                return path
        except Exception as error:
            LOG.warning("Summary offload unavailable: %s", type(error).__name__)
        return None

    async def _aoffload_to_backend(self, backend: Any, messages: list[Any], session_id: str) -> str | None:
        path = self._get_history_path(session_id)
        section = self._section(messages)
        try:
            existing = self._existing(await backend.adownload_files([path]))
        except Exception:
            existing = ""
        try:
            result = (await backend.aedit(path, existing, existing + section)
                      if existing else await backend.awrite(path, section))
            if result is not None and not result.error:
                return path
        except Exception as error:
            LOG.warning("Summary offload unavailable: %s", type(error).__name__)
        return None


def make_summarization_middleware(workflow_id: str, model: Any, backend: Any = None) -> DeterministicSummarization:
    return DeterministicSummarization(workflow_id, model, backend if backend is not None else DeterministicStateBackend())
