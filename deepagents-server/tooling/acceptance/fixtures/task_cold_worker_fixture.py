"""Trusted readonly model fixture for real multi-segment cold-restart Tasks."""
from __future__ import annotations

import argparse
import asyncio
import hashlib
import json
import os
from pathlib import Path
import socket
import time

from langchain_core.messages import messages_to_dict
from langchain_core.outputs import ChatGeneration, ChatResult
from temporalio import activity

from .task_can_worker_fixture import TaskCANFixtureModel, task_stem
from .task_container_worker_fixture import emit, security_probe
from mini_loop_deepagents.task_server.connection import TemporalConnectionSettings
from mini_loop_deepagents.task_server.launch import load_role_secrets
from mini_loop_deepagents.task_server.runtime import MODEL_ALIAS
from mini_loop_deepagents.task_server.worker import run_process


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()


class ColdModel(TaskCANFixtureModel):
    gate_task_ids: tuple[str, ...] = ()
    tool_schema_sha256: str = ""

    def bind_tools(self, tools, **kwargs):
        return self.model_copy(update={"has_tools": bool(tools),
            "tool_schema_sha256": hashlib.sha256(canonical(tools)).hexdigest()})

    async def _agenerate(self, messages, stop=None, run_manager=None, **kwargs):
        reply, role, step, summary = self.reply(messages)
        info = activity.info()
        details = {"workflow_id": info.workflow_id, "run_id": info.workflow_run_id,
            "activity_id": info.activity_id, "attempt": info.attempt, "role": role, "step": step, "summary": summary,
            "hostname": socket.gethostname(), "pid": os.getpid(), "worker_identity": f"{socket.gethostname()}:{os.getpid()}",
            "started_monotonic_ns": time.monotonic_ns(),
            "request_sha256": hashlib.sha256(canonical({"messages": messages_to_dict(messages),
                "tool_schema_sha256": self.tool_schema_sha256})).hexdigest()}
        emit("MODEL_START", details)
        outcome = "failed"
        try:
            # Real model Activity pacing, with production Workflow/tool/summary
            # control flow intact. Only the first two Tasks stop at parent 24.
            activity.heartbeat("cold-model-pacing")
            await asyncio.sleep(0.12)
            if not summary and role == "main" and step == 24 and info.workflow_id in self.gate_task_ids:
                emit("MODEL_GATE", details)
                release = Path(self.barrier_directory) / (task_stem(info.workflow_id) + ".release")
                while not release.is_file():
                    activity.heartbeat("cold-parent-24")
                    await asyncio.sleep(0.1)
            outcome = "completed"
            return ChatResult(generations=[ChatGeneration(message=reply)])
        except asyncio.CancelledError:
            outcome = "cancelled"
            raise
        finally:
            emit("MODEL_FINISH", {**details, "finished_monotonic_ns": time.monotonic_ns(), "outcome": outcome})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--address", required=True)
    parser.add_argument("--task-queue", required=True)
    parser.add_argument("--gate-directory", required=True)
    parser.add_argument("--gate-task-ids", required=True)
    args = parser.parse_args()
    load_role_secrets("worker")
    security_probe()
    gate_ids = tuple(args.gate_task_ids.split(","))
    if len(gate_ids) != 2 or any(not value.startswith("task-") for value in gate_ids):
        raise ValueError("cold_fixture_gate_identity_invalid")
    def provider(name):
        if name != MODEL_ALIAS:
            raise ValueError("cold_fixture_model_alias_invalid")
        return ColdModel(barrier_directory=args.gate_directory, gate_task_ids=gate_ids)
    run_process(args.address, args.task_queue, f"{socket.gethostname()}:{os.getpid()}",
        activity_slots=1, workflow_slots=2, model_provider=provider,
        connection=TemporalConnectionSettings.from_env(), shutdown_grace_seconds=1, shutdown_timeout_seconds=5)


if __name__ == "__main__":
    main()
