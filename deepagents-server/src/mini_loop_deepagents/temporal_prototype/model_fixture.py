"""Offline model provider: decisions derive from transcript, never a local cursor."""

from __future__ import annotations

import json
from typing import Any

from langchain_core.language_models.chat_models import BaseChatModel
from langchain_core.messages import AIMessage, BaseMessage
from langchain_core.outputs import ChatGeneration, ChatResult


class TranscriptModel(BaseChatModel):
    @property
    def _llm_type(self) -> str:
        return "mini-loop-temporal-transcript-fixture"

    def bind_tools(self, tools: Any, **kwargs: Any) -> TranscriptModel:
        return self

    def _reply(self, messages: list[BaseMessage]) -> AIMessage:
        control = None
        for message in reversed(messages):
            if message.type == "human" and isinstance(message.content, str):
                try:
                    candidate = json.loads(message.content)
                except (ValueError, TypeError):
                    continue
                if isinstance(candidate, dict) and "prototype_phase" in candidate:
                    control = candidate
                    break
        if control is None:
            raise ValueError("Prototype fixture requires a controlled transcript")
        phase, label, key = control["prototype_phase"], control["message"], control["memory_key"]
        completed = {m.tool_call_id for m in messages if m.type == "tool"}
        for message in messages:
            if message.type == "tool" and getattr(message, "status", "success") == "error":
                raise ValueError(f"Prototype tool failed: {message.name}: {str(message.content)[:1200]}")
        if phase == 1:
            script = [
                ("plan", "write_todos", {"todos": [{"content": "persist and recover task", "status": "in_progress"}]}),
                ("write", "write_file", {"file_path": "/plan.md", "content": f"plan:{label}"}),
                ("read", "read_file", {"file_path": "/plan.md"}),
                ("memory", "memory_put", {"request": {"key": key, "value": f"memory:{label}",
                  "operation_id": "primary-memory", "block_after_commit": control["block_after_effect"]}}),
            ]
        elif phase == 2:
            script = [
                ("read", "read_file", {"file_path": "/plan.md"}),
                ("memory", "memory_get", {"request": {"key": key}}),
                ("plan", "write_todos", {"todos": [{"content": "persist and recover task", "status": "completed"}]}),
            ]
        else:
            raise ValueError("Unknown prototype phase")
        for stage, tool, arguments in script:
            call_id = f"phase-{phase}-{stage}"
            if call_id not in completed:
                return AIMessage(content="", id=f"fixture:{call_id}",
                                 tool_calls=[{"id": call_id, "name": tool, "args": arguments}])
        return AIMessage(content=f"phase-{phase}-complete:{label}", id=f"fixture:phase-{phase}:complete")

    def _generate(self, messages: list[BaseMessage], stop: Any = None, run_manager: Any = None, **kwargs: Any) -> ChatResult:
        return ChatResult(generations=[ChatGeneration(message=self._reply(messages))])

    async def _agenerate(self, messages: list[BaseMessage], stop: Any = None, run_manager: Any = None, **kwargs: Any) -> ChatResult:
        return self._generate(messages, stop, run_manager, **kwargs)


def model_provider(name: str) -> TranscriptModel:
    if name != "fixture:transcript":
        raise ValueError("Prototype has no live model provider")
    return TranscriptModel()
