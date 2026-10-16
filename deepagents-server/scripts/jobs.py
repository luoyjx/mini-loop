"""Submit detached jobs and inspect saved handles; token comes from the environment."""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import stat
import sys
import tempfile
from pathlib import Path

from mini_loop_deepagents.jobs import (
    AsyncJobsClient, JobHandle, JobRequestError, JobResumeError, JobWaitTimeout, SubmissionUncertain,
)


def read_handle(path: Path) -> JobHandle:
    with path.open(encoding="utf-8") as source:
        if not stat.S_ISREG(os.fstat(source.fileno()).st_mode):
            raise ValueError("Job handle must be a regular file")
        text = source.read(65537)
    if len(text) > 65536:
        raise ValueError("Job handle file is too large")
    return JobHandle.from_json(text)


def write_handle(path: Path, job: JobHandle, *, overwrite: bool = False) -> None:
    """Publish a complete 0600 file atomically; never clobber without opting in."""
    descriptor, temporary = tempfile.mkstemp(prefix=".job-", suffix=".json", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as target:
            target.write(job.to_json())
            target.flush()
            os.fsync(target.fileno())
        if overwrite:
            os.replace(temporary, path)
        else:
            os.link(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def parser() -> argparse.ArgumentParser:
    command = argparse.ArgumentParser(description=__doc__)
    actions = command.add_subparsers(dest="action", required=True)
    for name in ("submit", "status", "wait", "state", "cancel", "resume"):
        action = actions.add_parser(
            name, description="Read current Thread state, which may include later Runs" if name == "state" else None,
        )
        action.add_argument("--url", default="http://127.0.0.1:2024", help="Trusted Agent Server URL")
        if name == "submit":
            action.add_argument("message")
            action.add_argument("--thread", help="Explicitly enqueue on an existing owned Thread")
        else:
            action.add_argument("--job-file", type=Path, required=True)
        if name in {"submit", "resume"}:
            action.add_argument("--output", type=Path, help="Also save the new handle with mode 0600")
            action.add_argument("--overwrite", action="store_true", help="Explicitly replace --output")
        if name == "wait":
            action.add_argument("--timeout", type=float, default=None, help="Local wait limit; never cancels the job")
    return command


async def run(args: argparse.Namespace, token: str) -> JobHandle | dict:
    async with AsyncJobsClient(args.url, token) as client:
        if args.action == "submit":
            return await client.submit(args.message, thread_id=args.thread)
        job = read_handle(args.job_file)
        if args.action == "wait":
            return await client.wait(job, timeout=args.timeout)
        if args.action == "cancel":
            await client.cancel(job)
            return {"cancellation_requested": True, "job": job.to_dict()}
        return await getattr(client, args.action)(job)


def main(argv: list[str] | None = None) -> int:
    command = parser()
    args = command.parse_args(argv)
    token = os.environ.get("MINILOOP_DEEPAGENTS_CLIENT_TOKEN")
    if not token:
        command.error("Set MINILOOP_DEEPAGENTS_CLIENT_TOKEN to your bearer token")
    output = getattr(args, "output", None)
    if getattr(args, "overwrite", False) and output is None:
        command.error("--overwrite requires --output")
    if output is not None:
        # Catch the common failure before creating any remote work; atomic
        # publication below also checks races without overwriting existing data.
        if not output.parent.is_dir():
            command.error("--output parent directory must exist")
        if os.path.lexists(output) and not args.overwrite:
            command.error("--output exists; choose another file or use --overwrite")
        if output.exists() and not output.is_file():
            command.error("--output must name a file")
    try:
        result = asyncio.run(run(args, token))
    except SubmissionUncertain as exc:
        print(json.dumps(exc.to_dict(), ensure_ascii=False), file=sys.stderr)
        return 3
    except JobWaitTimeout as exc:
        print(json.dumps({"error": "wait_timeout", "message": str(exc), "job": exc.job.to_dict()}), file=sys.stderr)
        return 2
    except (JobRequestError, JobResumeError) as exc:
        print(str(exc), file=sys.stderr)
        return 1
    except ValueError:
        # Server bodies, exception chains and filesystem path text can contain
        # sensitive data. Keep errors separate from normal requested results.
        print("Job operation failed; check the trusted URL, handle, Run status and credentials", file=sys.stderr)
        return 1
    except OSError:
        print("Could not read the job handle file", file=sys.stderr)
        return 1
    if isinstance(result, JobHandle):
        print(result.to_json(), end="", flush=True)
        if output is not None:
            try:
                write_handle(output, result, overwrite=args.overwrite)
            except OSError:
                print("Job accepted; keep the JSON handle above. Could not save --output; do not resubmit", file=sys.stderr)
                return 1
        return 0
    print(json.dumps(result, ensure_ascii=False, indent=2, default=str))
    return 1 if args.action == "wait" and result.get("status") != "success" else 0


if __name__ == "__main__":
    raise SystemExit(main())
