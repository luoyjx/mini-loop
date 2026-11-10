"""Trusted fault-harness worker, never selectable from a Task HTTP request.

Runs the production TaskWorkflow with a stateless model fixture. It calls real
planning/filesystem tools, then blocks an independent model Activity on a local
file gate. Only this test process accesses the gate; Workflow code never does.
The directory is local fault-test coordination, not durable production storage.
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
from langchain_core.messages import AIMessage, BaseMessage
from langchain_core.outputs import ChatGeneration, ChatResult
from temporalio import activity

from mini_loop_deepagents.task_server.runtime import MODEL_ALIAS
from mini_loop_deepagents.task_server.worker import serve


def barrier_paths(directory: Path, workflow_id: str) -> tuple[Path, Path]:
    stem = hashlib.sha256(workflow_id.encode()).hexdigest()
    return directory / f"{stem}.blocked.json", directory / f"{stem}.release"


FAULT_PREFIXES = {
    "fault:rate-limit:": "rate-limit",
    "fault:timeout:": "timeout",
    "fault:disconnect:": "disconnect",
    "fault:race:": "race",
}


class ControlledHTTPError(RuntimeError):
    status_code = 429

    def __init__(self) -> None:
        super().__init__("controlled provider rate limit")
        self.response = type("ControlledResponse", (), {"headers": {"retry-after-ms": "100"}})()


def fault_mode(label: str) -> str | None:
    return next((mode for prefix, mode in FAULT_PREFIXES.items() if label.startswith(prefix)), None)


def record_attempt(directory: Path, mode: str, phase: str, outcome: str) -> None:
    info = activity.info()
    identity = "\0".join((info.workflow_id, info.workflow_run_id, info.activity_id, str(info.attempt)))
    name = hashlib.sha256(identity.encode()).hexdigest() + ".attempt.json"
    value = {
        "workflow_id": info.workflow_id,
        "run_id": info.workflow_run_id,
        "activity_id": info.activity_id,
        "attempt": info.attempt,
        "mode": mode,
        "phase": phase,
        "outcome": outcome,
        "pid": os.getpid(),
        "observed_monotonic_ns": time.monotonic_ns(),
    }
    temporary = directory / (name + f".{os.getpid()}.tmp")
    descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, "w") as stream:
        json.dump(value, stream, sort_keys=True)
    os.replace(temporary, directory / name)


class TaskFixtureModel(BaseChatModel):
    barrier_directory: str

    @property
    def _llm_type(self) -> str:
        return "mini-loop-task-fault-fixture"

    def bind_tools(self, tools: Any, **kwargs: Any) -> TaskFixtureModel:
        return self

    def _reply(self, messages: list[BaseMessage]) -> tuple[AIMessage, bool]:
        humans = [m for m in messages if m.type == "human"]
        if not humans or not isinstance(humans[0].content, str):
            raise ValueError("Task fixture requires a plain Human message")
        label = humans[0].content
        completed = {m.tool_call_id for m in messages if m.type == "tool"}
        for message in messages:
            if message.type == "tool" and getattr(message, "status", "success") == "error":
                raise ValueError(f"Task tool failed: {message.name}: {str(message.content)[:1200]}")
        script = [
            ("task-plan", "write_todos", {"todos": [{"content": "complete task files", "status": "in_progress"}]}),
            ("task-write", "write_file", {"file_path": "/plan.md", "content": f"plan:{label}"}),
            ("task-read", "read_file", {"file_path": "/plan.md"}),
            ("task-finish", "write_todos", {"todos": [{"content": "complete task files", "status": "completed"}]}),
        ]
        for call_id, name, args in script:
            if call_id not in completed:
                return AIMessage(content="", id=f"fixture:{call_id}",
                                 tool_calls=[{"id": call_id, "name": name, "args": args}]), call_id == "task-finish"
        return AIMessage(content=f"task-complete:{label}", id="fixture:task-complete"), False

    def _generate(self, messages: list[BaseMessage], stop: Any = None,
                  run_manager: Any = None, **kwargs: Any) -> ChatResult:
        raise RuntimeError("Task fault fixture must run as an async model Activity")

    async def _agenerate(self, messages: list[BaseMessage], stop: Any = None,
                         run_manager: Any = None, **kwargs: Any) -> ChatResult:
        reply, should_block = self._reply(messages)
        label = next(message.content for message in messages
                     if message.type == "human" and isinstance(message.content, str))
        mode = fault_mode(label)
        if mode is not None:
            directory = Path(self.barrier_directory)
            directory.mkdir(parents=True, exist_ok=True)
            info = activity.info()
            phase = "initial" if not any(message.type == "tool" for message in messages) else "continued"
            failure = (
                (mode == "rate-limit" and phase == "initial" and info.attempt <= 2)
                or (mode == "timeout" and phase == "initial" and info.attempt <= 3)
                or (mode == "disconnect" and phase == "initial" and info.attempt == 1)
            )
            record_attempt(directory, mode, phase, "failed" if failure else "succeeded")
            if failure:
                if mode == "rate-limit":
                    raise ControlledHTTPError()
                if mode == "timeout":
                    raise TimeoutError("controlled provider timeout")
                raise ConnectionError("controlled provider disconnect")
        if should_block and mode not in {"rate-limit", "timeout", "disconnect"}:
            info = activity.info()
            directory = Path(self.barrier_directory)
            directory.mkdir(parents=True, exist_ok=True)
            marker, release = barrier_paths(directory, info.workflow_id)
            details = {"workflow_id": info.workflow_id, "run_id": info.workflow_run_id,
                       "pid": os.getpid(), "attempt": info.attempt, "activity_id": info.activity_id}
            temporary = marker.with_suffix(f".{os.getpid()}.tmp")
            temporary.write_text(json.dumps(details, sort_keys=True), encoding="utf-8")
            temporary.replace(marker)
            while not release.exists():
                activity.heartbeat("task-model-barrier")
                await asyncio.sleep(0.2)
        return ChatResult(generations=[ChatGeneration(message=reply)])


def make_fixture_provider(directory: str):
    def model_provider(name: str) -> TaskFixtureModel:
        if name != MODEL_ALIAS:
            raise ValueError("Unexpected Task fixture model alias")
        return TaskFixtureModel(barrier_directory=directory)
    return model_provider


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--address", required=True)
    parser.add_argument("--task-queue", required=True)
    parser.add_argument("--identity", required=True)
    parser.add_argument("--barrier-directory", required=True)
    args = parser.parse_args()
    asyncio.run(serve(args.address, args.task_queue, args.identity,
                      model_provider=make_fixture_provider(args.barrier_directory)))


if __name__ == "__main__":
    main()
