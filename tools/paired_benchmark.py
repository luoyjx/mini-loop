"""Run the paired benchmark: baseline arm vs candidate arm.

Fake transport by default (free, deterministic -- exercises the harness,
not the model). A real-endpoint run is the operator's explicit, budgeted
choice:

    MINILOOP_BENCHMARK_REAL=1 python tools/paired_benchmark.py

Arms differ by environment overlays: MINILOOP_BENCH_CANDIDATE_* variables
are applied (with the prefix stripped to MINILOOP_*) to the candidate arm
only, so e.g. MINILOOP_BENCH_CANDIDATE_SUBAGENT_MAX_DEPTH=3 benchmarks a
depth-quota change against the current default.
"""

from __future__ import annotations

import asyncio
import json
import os
import pathlib
import sys
import tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent.parent))

from mini_loop.benchmark import DEFAULT_TASKS, compare, run_arm  # noqa: E402
from mini_loop.config import Settings, build_client  # noqa: E402

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


def _settings(root: pathlib.Path, real: bool) -> Settings:
    return Settings(
        fake_llm=not real,
        workspace_root=root,
        skills_dir=SKILLS,
        spill_dir=None,
    )


def main() -> int:
    real = os.getenv("MINILOOP_BENCHMARK_REAL") == "1"
    overlay = {
        key.replace("MINILOOP_BENCH_CANDIDATE_", "MINILOOP_", 1): value
        for key, value in os.environ.items()
        if key.startswith("MINILOOP_BENCH_CANDIDATE_")
    }

    async def run() -> dict:
        with tempfile.TemporaryDirectory(prefix="bench-base-") as base_dir, \
             tempfile.TemporaryDirectory(prefix="bench-cand-") as cand_dir:
            base_settings = _settings(pathlib.Path(base_dir), real)
            baseline = await run_arm(
                "baseline", base_settings,
                build_client(base_settings), DEFAULT_TASKS,
            )
            saved = {k: os.environ.get(k) for k in overlay}
            os.environ.update(overlay)
            try:
                cand_settings = _settings(pathlib.Path(cand_dir), real)
            finally:
                for key, value in saved.items():
                    if value is None:
                        os.environ.pop(key, None)
                    else:
                        os.environ[key] = value
            candidate = await run_arm(
                "candidate", cand_settings,
                build_client(cand_settings), DEFAULT_TASKS,
            )
            return {
                "real": real,
                "overlay": sorted(overlay),
                "baseline": baseline,
                "candidate": candidate,
                "comparison": compare(baseline, candidate),
            }

    report = asyncio.run(run())
    print(json.dumps(report, indent=2))
    return 0 if report["comparison"]["verdict"] != "regression" else 1


if __name__ == "__main__":
    raise SystemExit(main())
