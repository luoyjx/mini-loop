"""Actual dev HTTP/SIGKILL fault checks, never a PostgreSQL durability claim.

Uses the pinned inmem runtime's periodic snapshot, then kills the owned process
group. Its cache acceptance gap is intentionally left visible in the receipt.
Production queue/redeployment acceptance is a separate licensed-stack gate.
"""

import asyncio
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import secrets
import shutil
import signal
import socket
import subprocess
import tempfile
import time
from uuid import uuid4

from mini_loop_deepagents.jobs import AsyncJobsClient, JobHandle, JobRequestError, JobWaitTimeout
from smoke import MODULE, Server


def message(name, **options):
    return "JOB:" + json.dumps({"name": name, **options})


async def eventually(call, predicate, *, timeout=25):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        result = await call()
        if predicate(result):
            return result
        await asyncio.sleep(0.1)
    raise TimeoutError("Expected job condition was not observed")


async def progress(client, job, name):
    return await eventually(lambda: client.state(job), lambda s: "/progress-" + name + ".txt" in s.get("values", {}).get("files", {}))


def tool_count(state, name):
    messages = state["values"]["messages"]
    return sum(m.get("type") == "tool" and m.get("tool_call_id") == "progress-" + name for m in messages)


async def exercise(directory, server, env, port, alice_token, bob_token):
    url = f"http://127.0.0.1:{port}"
    passed = []
    await asyncio.to_thread(server.start)
    # A separate CLI process submits and exits, preserving only its JSON handle.
    output = directory / "a.job.json"
    cli = await asyncio.to_thread(subprocess.run, [str(MODULE / ".venv/bin/python"),
        str(MODULE / "scripts/jobs.py"), "submit", message("a", block=True),
        "--url", url, "--output", str(output)],
        env={**env, "MINILOOP_DEEPAGENTS_CLIENT_TOKEN": alice_token}, capture_output=True, text=True, timeout=20)
    assert cli.returncode == 0, cli.stderr
    a = JobHandle.from_json(output.read_text())
    assert output.stat().st_mode & 0o777 == 0o600
    async with AsyncJobsClient(url, alice_token, poll_interval=0.05) as client:
        # A direct caller asks for exit-only persistence. Server-side policy must
        # still make this blocked Run's intermediate tool checkpoint observable.
        b_thread = (await client._sdk.threads.create())["thread_id"]
        request_id = str(uuid4())
        b_run = await client._sdk.runs.create(b_thread, "agent", durability="exit",
            input={"messages": [{"role": "user", "content": message("b", block=True)}]},
            metadata={"request_id": request_id})
        b = JobHandle(url, b_thread, b_run["run_id"], request_id)
        await progress(client, a, "a")
        await progress(client, b, "b")
        assert (await client.status(a))["status"] == (await client.status(b))["status"] == "running"
        passed.append("detached CLI submit plus two concurrent long Runs; direct exit-only request still receives server-enforced intermediate sync checkpoints")
        try:
            await client.wait(a, timeout=0.2)
        except JobWaitTimeout:
            pass
        else:
            raise AssertionError("Local wait should time out while the Run continues")
        assert (await client.status(a))["status"] == "running"
        queued = await client.submit(message("queued"), thread_id=a.thread_id)
        backlog = await client.submit(message("backlog"))
        assert (await client.status(queued))["status"] == (await client.status(backlog))["status"] == "pending"
        passed.append("wait timeout does not cancel; same-Thread enqueue and exhausted worker slots retain pending Runs")
        async with AsyncJobsClient(url, bob_token) as foreign:
            for operation in [foreign.status, foreign.cancel, foreign.resume]:
                try:
                    await operation(a)
                except JobRequestError as error:
                    assert error.status_code in {403, 404}
                else:
                    raise AssertionError("Foreign user accessed an owned long job")
        passed.append("long-job status/cancel/resume preserve server-enforced owner isolation")
        # Dev cache flush is ten seconds, NOT transactional acknowledgement.
        # Deliberately wait for it: this gate cannot establish immediate durability.
        await asyncio.sleep(12)
        snapshots = list((directory / ".langgraph_api").rglob("*"))
        assert any(p.is_file() and p.stat().st_size for p in snapshots)
        killed_pid = server.process.pid
        os.killpg(killed_pid, signal.SIGKILL)
        await asyncio.to_thread(server.process.wait, timeout=5)
        assert server.process.returncode == -signal.SIGKILL
        await asyncio.to_thread(server.stop)
        for name in ["a", "b"]:
            (directory / ("release-" + name)).touch()
        # Reload a compatible graph from a different code path, retaining graph ID
        # and state schema. This is a dev-process replacement, not a Docker rollout.
        new_graph = directory / "long_job_revision2.py"
        shutil.copyfile(MODULE / "tests/long_job_graph.py", new_graph)
        config = json.loads(server.config.read_text())
        config["graphs"]["agent"] = str(new_graph) + ":agent"
        server.config.write_text(json.dumps(config))
        await asyncio.to_thread(server.start)
        for job in [a, b, queued, backlog]:
            assert (await client.wait(job, timeout=35))["status"] == "success"
        for job, name in [(a, "a"), (b, "b")]:
            state = await client.state(job)
            assert state["values"]["files"]["/progress-" + name + ".txt"]["content"] == "checkpoint:" + name
            assert tool_count(state, name) == 1
        passed.append("SIGKILL after periodic dev snapshot; replacement process reclaims same Run IDs and drains pending jobs without repeating completed tool checkpoints")

        failed = await client.submit(message("failure", fail=True))
        assert (await client.wait(failed, timeout=20))["status"] == "error"
        before = await client.state(failed)
        assert tool_count(before, "failure") == 1 and before["next"]
        (directory / "release-failure").touch()
        resumed = await client.resume(failed)
        assert resumed.thread_id == failed.thread_id and resumed.run_id != failed.run_id
        assert (await client.wait(resumed, timeout=20))["status"] == "success"
        after = await client.state(resumed)
        assert tool_count(after, "failure") == 1
        assert sum(m.get("type") == "human" for m in after["values"]["messages"]) == 1
        assert (await client.status(failed))["status"] == "error"
        passed.append("terminal model failure resumes unfinished checkpoint as a new Run with null input; original prompt and completed tool occur once")

        cancelled = await client.submit(message("cancelled", block=True))
        await progress(client, cancelled, "cancelled")
        await client.cancel(cancelled)
        assert (await client.wait(cancelled, timeout=20))["status"] == "interrupted"
        (directory / "release-cancelled").touch()
        continuation = await client.resume(cancelled)
        assert (await client.wait(continuation, timeout=20))["status"] == "success"
        assert tool_count(await client.state(continuation), "cancelled") == 1
        passed.append("explicit cancellation followed by checkpoint continuation retains completed tool state")
    return {"passed": passed, "sigkill_pid": killed_pid, "storage_mode": "dev periodic inmem snapshot", "production_acceptance": False}


def write_receipt(path, receipt):
    """Atomically replace the latest attempt, including running/failed attempts."""
    descriptor, temporary = tempfile.mkstemp(prefix=".fault-receipt-", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as output:
            json.dump(receipt, output, indent=2)
            output.write("\n")
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def main():
    log_dir = MODULE.parent / "var/deepagents-delivery"
    log_dir.mkdir(parents=True, exist_ok=True)
    log = log_dir / "fault-smoke.log"
    receipt_path = log_dir / "fault-smoke.json"
    started = time.monotonic()
    receipt = {
        "run_id": str(uuid4()), "status": "running",
        "started_at": datetime.now(timezone.utc).isoformat(),
        "storage_mode": "dev periodic inmem snapshot", "production_acceptance": False,
        "passed": [],
    }
    # Replace an older pass before any setup or test can fail. A forced kill of
    # this harness leaves this attempt marked running, never an earlier pass.
    write_receipt(receipt_path, receipt)
    server = None
    try:
        log.write_text("")
        alice_token, bob_token = secrets.token_urlsafe(32), secrets.token_urlsafe(32)
        env = {k: v for k, v in os.environ.items() if not any(s in k for s in ("API_KEY", "TOKEN", "SECRET", "PASSWORD", "MINILOOP_DEEPAGENTS"))}
        env.update(MINILOOP_DEEPAGENTS_TOKENS=json.dumps({alice_token: "alice", bob_token: "bob"}),
                   LANGSMITH_TRACING="false", LANGGRAPH_CLI_NO_ANALYTICS="1", BG_JOB_MAX_RETRIES="3", OMP_NUM_THREADS="1")
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        with tempfile.TemporaryDirectory(prefix="mini-loop-long-job-fault-") as temporary:
            directory = Path(temporary)
            env["MINILOOP_DEEPAGENTS_TEST_CONTROL_DIR"] = str(directory)
            config = directory / "langgraph.json"
            config.write_text(json.dumps({"dependencies": [str(MODULE)],
                "graphs": {"agent": str(MODULE / "tests/long_job_graph.py") + ":agent"},
                "auth": {"path": str(MODULE / "src/mini_loop_deepagents/auth.py") + ":auth", "disable_studio_auth": True}}))
            server = Server(directory, config, env, port, log)
            try:
                result = asyncio.run(exercise(directory, server, env, port, alice_token, bob_token))
            finally:
                server.stop()
        receipt.update(result, status="passed")
    except BaseException as error:
        blocked = server.blocked if server is not None else None
        receipt.update(status="resource_blocked" if blocked else "failed", error_type=type(error).__name__)
        if blocked:
            receipt["resource_block_reason"] = blocked
        raise
    finally:
        receipt.update(
            finished_at=datetime.now(timezone.utc).isoformat(),
            peak_server_tree_rss_mib=round(server.peak_rss_kib / 1024, 1) if server is not None else 0,
            seconds=round(time.monotonic() - started, 2),
        )
        write_receipt(receipt_path, receipt)
        print(json.dumps(receipt, indent=2))


if __name__ == "__main__":
    main()
