"""Readonly-mounted model fixture using the production container Worker entry."""
from __future__ import annotations

import argparse
import asyncio
import errno
import hashlib
import json
import os
from pathlib import Path
import socket
import time

from langchain_core.messages import messages_to_dict
from langchain_core.outputs import ChatGeneration, ChatResult
from temporalio import activity

from task_rollout_worker_fixture import RolloutModel, canonical, task_stem
from mini_loop_deepagents.task_server.connection import TemporalConnectionSettings
from mini_loop_deepagents.task_server.launch import load_role_secrets
from mini_loop_deepagents.task_server.runtime import MODEL_ALIAS
from mini_loop_deepagents.task_server.worker import run_process


def emit(kind, value):
    print("TASK_CONTAINER_" + kind + " " + json.dumps(value, sort_keys=True), flush=True)


def security_probe():
    root_errno = None
    try:
        descriptor = os.open("/var/tmp/task-container-root-write", os.O_CREAT | os.O_WRONLY, 0o600)
    except OSError as error:
        root_errno = error.errno
    else:
        os.close(descriptor)
        os.unlink("/var/tmp/task-container-root-write")
    temporary = Path("/tmp/task-container-write")
    temporary.write_text("temporary fixture probe")
    temporary.unlink()
    stats = os.statvfs("/tmp")
    value = {"uid": os.getuid(), "gid": os.getgid(), "hostname": socket.gethostname(),
             "root_write_errno": root_errno, "tmp_writable": True,
             "tmp_capacity_bytes": stats.f_blocks * stats.f_frsize,
             "cgroup_v2": {name: Path("/sys/fs/cgroup", name).read_text().strip()
                           for name in ("memory.max", "memory.swap.max", "cpu.max", "pids.max")},
             "model_key_loaded": bool(os.environ.get("OPENAI_API_KEY")),
             "http_secret_env_present": any(name.startswith("MINILOOP_DEEPAGENTS_TOKENS") or name.startswith("MINILOOP_TASK_SERVER_CURSOR_SECRET") for name in os.environ),
             "http_secret_files_present": any(Path("/run/secrets", name).exists() for name in ("tokens.json", "cursor.txt"))}
    if (value["uid"] != 10001 or value["gid"] != 10001 or root_errno != errno.EROFS
            or not value["model_key_loaded"] or value["http_secret_env_present"] or value["http_secret_files_present"]
            or value["tmp_capacity_bytes"] > 64 * 1024 * 1024):
        raise RuntimeError("container_fixture_security_contract_failed")
    emit("SECURITY", value)


class ContainerModel(RolloutModel):
    async def _agenerate(self, messages, stop=None, run_manager=None, **kwargs):
        reply, step = self.reply(messages)
        info = activity.info()
        details = {"workflow_id": info.workflow_id, "run_id": info.workflow_run_id, "activity_id": info.activity_id,
                   "attempt": info.attempt, "step": step, "pid": os.getpid(), "hostname": socket.gethostname(),
                   "worker_identity": f"{socket.gethostname()}:{os.getpid()}", "started_monotonic_ns": time.monotonic_ns(),
                   "request_sha256": hashlib.sha256(canonical({"messages": messages_to_dict(messages),
                                                              "tool_schema_sha256": self.tool_schema_sha256})).hexdigest()}
        emit("MODEL_START", details)
        outcome = "failed"
        try:
            if step == 3:
                emit("MODEL_GATE", details)
                gate = Path(self.barrier_directory) / (task_stem(info.workflow_id) + ".release")
                while not gate.is_file():
                    activity.heartbeat("container-model-gate")
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
    args = parser.parse_args()
    load_role_secrets("worker")
    security_probe()
    def provider(name):
        if name != MODEL_ALIAS:
            raise ValueError("container_fixture_model_alias_mismatch")
        return ContainerModel(barrier_directory=args.gate_directory)
    run_process(args.address, args.task_queue, f"{socket.gethostname()}:{os.getpid()}",
                model_provider=provider, connection=TemporalConnectionSettings.from_env(), activity_slots=1,
                workflow_slots=2, shutdown_grace_seconds=1, shutdown_timeout_seconds=5)


if __name__ == "__main__":
    main()
