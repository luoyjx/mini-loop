"""Trusted deterministic model for the current TaskWorkflow fault acceptance.

Only model Activities access these local test gates. The production Workflow,
native task/subagent tools, summarization, files and checkpointer stay in use.
No fixture selection is exposed through the production HTTP API or Worker CLI.
"""

from __future__ import annotations

import argparse
import asyncio
import hashlib
import html
import json
import os
from pathlib import Path
import re
from typing import Any

from langchain_core.language_models import BaseChatModel
from langchain_core.messages import AIMessage, BaseMessage, messages_to_dict
from langchain_core.outputs import ChatGeneration, ChatResult
from temporalio import activity

from mini_loop_deepagents.task_server.runtime import MODEL_ALIAS
from mini_loop_deepagents.task_server.worker import serve


CALL = re.compile(r"can-(main|child\d{3})-(\d{3})")
SUMMARY = re.compile(r"CAN_SUMMARY:(\{[^\n]+\})")
MAIN_STEPS = 37


def task_stem(task_id: str) -> str:
    return hashlib.sha256(task_id.encode()).hexdigest()


def gate_paths(directory: Path, task_id: str, call_id: str) -> tuple[Path, Path]:
    stem = task_stem(task_id)
    key = hashlib.sha256(call_id.encode()).hexdigest()
    return directory / f"{stem}.latest.json", directory / f"{stem}.{key}.release"


def private_json(path: Path, value: Any) -> None:
    temporary = path.with_suffix(f".{os.getpid()}.tmp")
    descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
        json.dump(value, stream, sort_keys=True)
    temporary.replace(path)


def progress(messages: list[BaseMessage], *, summary_request: bool = False) -> tuple[str, set[int]]:
    """Read completed call identities, including genuine prior summary content.

    The official summary prompt renders a completed, tool-pair-safe prefix as
    XML. Its tool-call IDs can therefore be carried as deterministic test-model
    prose. Ordinary model calls use only ToolMessages and prior summaries.
    """
    text = html.unescape("\n".join(m.text for m in messages))
    summaries = [json.loads(match.group(1)) for match in SUMMARY.finditer(text)]
    roles = {item["role"] for item in summaries}
    if "[can-main]" in text:
        role = "main"
    elif summaries:
        role = summaries[0]["role"]
    else:
        match = re.search(r"\[can-child:(\d{3})\]", text)
        if match is None:
            raise ValueError("CAN model request lacks its persisted role marker")
        role = "child" + match.group(1)
    if roles - {role}:
        raise ValueError("A summary mixed independent agent roles")
    completed = {int(index) for item in summaries for index in item["done"]}
    if summary_request:
        completed.update(int(index) for found_role, index in CALL.findall(text) if found_role == role)
    else:
        for message in messages:
            if message.type == "tool":
                if getattr(message, "status", "success") == "error":
                    raise ValueError("A real CAN fixture tool returned an error")
                match = CALL.fullmatch(getattr(message, "tool_call_id", ""))
                if match and match.group(1) == role:
                    completed.add(int(match.group(2)))
    return role, completed


def action(role: str, step: int) -> tuple[str, dict[str, Any]] | None:
    if role != "main":
        path = f"/{role}.md"
        if step == 0:
            return "write_file", {"file_path": path, "content": f"verified:{role}"}
        if step == 1:
            return "read_file", {"file_path": path}
        return None
    if step == 0:
        return "write_todos", {"todos": [{"content": "finish 37 native tool steps with two subagents", "status": "in_progress"}]}
    if step == MAIN_STEPS - 1:
        return "write_todos", {"todos": [{"content": "finish 37 native tool steps with two subagents", "status": "completed"}]}
    if step in (3, 18):
        return "task", {"description": f"[can-child:{step:03d}] Write and read your own evidence file, then return its verification marker.", "subagent_type": "general-purpose"}
    if step >= MAIN_STEPS:
        return None
    if step in (2, 5, 7, 9, 11, 13, 15, 17, 20, 22, 24, 26, 28, 30, 32, 34):
        previous = step - 1
        return "read_file", {"file_path": f"/phase-{previous:03d}.md"}
    return "write_file", {"file_path": f"/phase-{step:03d}.md", "content": f"phase:{step:03d}"}


def expected_files() -> dict[str, str]:
    result = {f"/child{index:03d}.md": f"verified:child{index:03d}" for index in (3, 18)}
    for step in range(MAIN_STEPS):
        operation = action("main", step)
        if operation is not None and operation[0] == "write_file":
            result[operation[1]["file_path"]] = operation[1]["content"]
    return result


class TaskCANFixtureModel(BaseChatModel):
    barrier_directory: str
    has_tools: bool = False

    @property
    def _llm_type(self) -> str:
        return "mini-loop-task-can-fault-fixture"

    def bind_tools(self, tools: Any, **kwargs: Any) -> TaskCANFixtureModel:
        return self.model_copy(update={"has_tools": bool(tools)})

    def reply(self, messages: list[BaseMessage]) -> tuple[AIMessage, str, int, bool]:
        role, done = progress(messages, summary_request=not self.has_tools)
        if not self.has_tools:
            content = "CAN_SUMMARY:" + json.dumps({"role": role, "done": sorted(done)}, separators=(",", ":"))
            digest = hashlib.sha256(content.encode()).hexdigest()[:20]
            return AIMessage(content=content, id="fixture:summary:" + digest), role, -1, True
        step = 0
        while step in done:
            step += 1
        if done != set(range(step)):
            raise ValueError("The real transcript or summary lost a completed fixture step")
        call_id = f"can-{role}-{step:03d}"
        operation = action(role, step)
        if operation is None:
            return AIMessage(content=f"CAN_COMPLETE:{role}", id="fixture:" + call_id), role, step, False
        name, args = operation
        return AIMessage(content="", id="fixture:" + call_id, tool_calls=[{"id": call_id, "name": name, "args": args}]), role, step, False

    def _generate(self, messages: list[BaseMessage], stop: Any = None,
                  run_manager: Any = None, **kwargs: Any) -> ChatResult:
        raise RuntimeError("CAN fault fixture requires an asynchronous model Activity")

    async def _agenerate(self, messages: list[BaseMessage], stop: Any = None,
                         run_manager: Any = None, **kwargs: Any) -> ChatResult:
        reply, role, step, is_summary = self.reply(messages)
        info = activity.info()
        directory = Path(self.barrier_directory)
        directory.mkdir(parents=True, exist_ok=True, mode=0o700)
        call_id = f"can-{role}-{step:03d}" if not is_summary else str(reply.id)
        request = json.dumps(messages_to_dict(messages), sort_keys=True, separators=(",", ":"))
        details = {"workflow_id": info.workflow_id, "run_id": info.workflow_run_id,
                   "pid": os.getpid(), "attempt": info.attempt, "activity_id": info.activity_id,
                   "role": role, "step": step, "call_id": call_id, "summary": is_summary,
                   "request_sha256": hashlib.sha256(request.encode()).hexdigest()}
        audit_key = hashlib.sha256(f"{info.workflow_id}:{info.workflow_run_id}:{info.activity_id}:{info.attempt}".encode()).hexdigest()
        private_json(directory / (audit_key + ".activity.json"), details)
        if not is_summary:
            marker, release = gate_paths(directory, info.workflow_id, call_id)
            private_json(marker, details)
            while not release.exists():
                activity.heartbeat("task-can-model-barrier")
                await asyncio.sleep(0.1)
        return ChatResult(generations=[ChatGeneration(message=reply)])


def make_fixture_provider(directory: str):
    def model_provider(name: str) -> TaskCANFixtureModel:
        if name != MODEL_ALIAS:
            raise ValueError("Unexpected CAN fixture model alias")
        return TaskCANFixtureModel(barrier_directory=directory)
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
