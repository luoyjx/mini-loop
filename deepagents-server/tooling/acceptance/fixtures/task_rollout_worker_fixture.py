"""Trusted model Activities for production Worker shutdown/rollout acceptance.

The real TaskWorkflowV2 executes eleven parent model calls, native planning/files,
and at least one Continue-As-New. Only model Activities read test gates. A local
per-task flag can deliberately ignore Activity cancellation to test the process
hard deadline; it is never an HTTP parameter or a production Workflow branch.
"""

from __future__ import annotations

import argparse
import asyncio
import hashlib
import json
import os
from pathlib import Path
import time
from typing import Any

from langchain_core.language_models import BaseChatModel
from langchain_core.messages import AIMessage, BaseMessage, messages_to_dict
from langchain_core.outputs import ChatGeneration, ChatResult
from temporalio import activity

from mini_loop_deepagents.task_server.runtime import MODEL_ALIAS


STEPS = 10
BARRIER_STEP = 3


def canonical(value: Any) -> bytes:
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False, allow_nan=False).encode()


def task_stem(task_id: str) -> str:
    return hashlib.sha256(task_id.encode()).hexdigest()


def paths(directory: Path, task_id: str) -> dict[str, Path]:
    stem = task_stem(task_id)
    return {name: directory / f"{stem}.{name}" for name in ("blocked.json", "release", "ignore-cancel")}


def private_json(path: Path, value: Any) -> None:
    temporary = path.with_suffix(f".{os.getpid()}.tmp")
    descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, "wb") as stream:
        stream.write(canonical(value))
    temporary.replace(path)


def operation(step: int) -> tuple[str, dict[str, Any]] | None:
    if step == 0:
        return "write_todos", {"todos": [{"content": "finish durable rollout task", "status": "in_progress"}]}
    if step == STEPS - 1:
        return "write_todos", {"todos": [{"content": "finish durable rollout task", "status": "completed"}]}
    if step >= STEPS:
        return None
    if step % 2:
        return "write_file", {"file_path": f"/rollout-{step}.md", "content": f"rollout:{step}"}
    return "read_file", {"file_path": f"/rollout-{step - 1}.md"}


class RolloutModel(BaseChatModel):
    barrier_directory: str
    tool_schema_sha256: str = ""

    @property
    def _llm_type(self) -> str:
        return "mini-loop-worker-rollout-fixture"

    def bind_tools(self, tools: Any, **kwargs: Any) -> RolloutModel:
        return self.model_copy(update={"tool_schema_sha256": hashlib.sha256(canonical(tools)).hexdigest()})

    def reply(self, messages: list[BaseMessage]) -> tuple[AIMessage, int]:
        humans = [message for message in messages if message.type == "human"]
        if len(humans) != 1 or not humans[0].text.startswith("[rollout]"):
            raise ValueError("Rollout fixture requires one persisted original Human message")
        completed = []
        for message in messages:
            if message.type == "tool":
                if getattr(message, "status", "success") == "error":
                    raise ValueError("A native rollout tool failed")
                completed.append(message.tool_call_id)
        step = len(completed)
        if completed != [f"rollout-{index:03d}" for index in range(step)]:
            raise ValueError("Rollout transcript duplicated, lost or reordered a native tool")
        call_id = f"rollout-{step:03d}"
        selected = operation(step)
        if selected is None:
            return AIMessage(content="ROLLOUT_COMPLETE", id="fixture:" + call_id), step
        name, args = selected
        return AIMessage(content="", id="fixture:" + call_id, tool_calls=[{"id": call_id, "name": name, "args": args}]), step

    def _generate(self, messages: list[BaseMessage], stop: Any = None,
                  run_manager: Any = None, **kwargs: Any) -> ChatResult:
        raise RuntimeError("The rollout fixture requires a real asynchronous model Activity")

    async def _agenerate(self, messages: list[BaseMessage], stop: Any = None,
                         run_manager: Any = None, **kwargs: Any) -> ChatResult:
        reply, step = self.reply(messages)
        info = activity.info()
        directory = Path(self.barrier_directory)
        directory.mkdir(parents=True, exist_ok=True, mode=0o700)
        gate = paths(directory, info.workflow_id)
        request = {"messages": messages_to_dict(messages), "tool_schema_sha256": self.tool_schema_sha256}
        details = {"workflow_id": info.workflow_id, "run_id": info.workflow_run_id, "pid": os.getpid(),
                   "activity_id": info.activity_id, "attempt": info.attempt, "step": step,
                   "request_sha256": hashlib.sha256(canonical(request)).hexdigest(),
                   "started_monotonic_ns": time.monotonic_ns()}
        audit = hashlib.sha256(f"{info.workflow_id}:{info.workflow_run_id}:{info.activity_id}:{info.attempt}".encode()).hexdigest()
        private_json(directory / f"{audit}.started.json", details)
        outcome = "failed"
        try:
            if step == BARRIER_STEP:
                private_json(gate["blocked.json"], details)
                while not gate["release"].exists():
                    try:
                        activity.heartbeat("rollout-model-barrier")
                        await asyncio.sleep(0.1)
                    except asyncio.CancelledError:
                        if not gate["ignore-cancel"].exists():
                            outcome = "cancelled"
                            raise
                        private_json(directory / f"{audit}.ignored-cancellation.json",
                                     {**details, "ignored_at_monotonic_ns": time.monotonic_ns()})
            outcome = "completed"
            return ChatResult(generations=[ChatGeneration(message=reply)])
        finally:
            private_json(directory / f"{audit}.finished.json",
                         {**details, "finished_monotonic_ns": time.monotonic_ns(), "outcome": outcome})


def make_fixture_provider(directory: str):
    def provider(name: str) -> RolloutModel:
        if name != MODEL_ALIAS:
            raise ValueError("Unexpected rollout fixture model alias")
        return RolloutModel(barrier_directory=directory)
    return provider


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--address", required=True)
    parser.add_argument("--task-queue", required=True)
    parser.add_argument("--identity", required=True)
    parser.add_argument("--barrier-directory", required=True)
    parser.add_argument("--shutdown-grace-seconds", type=float, default=1.0)
    parser.add_argument("--shutdown-timeout-seconds", type=float, default=5.0)
    args = parser.parse_args()
    from mini_loop_deepagents.task_server.worker import run_process
    run_process(args.address, args.task_queue, args.identity, model_provider=make_fixture_provider(args.barrier_directory),
                activity_slots=1, workflow_slots=2, shutdown_grace_seconds=args.shutdown_grace_seconds,
                shutdown_timeout_seconds=args.shutdown_timeout_seconds)


if __name__ == "__main__":
    main()
