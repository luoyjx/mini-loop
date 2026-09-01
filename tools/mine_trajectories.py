"""Mine recorded trajectories for friction hotspots (read-only).

    python tools/mine_trajectories.py [--root workspaces/.trajectories]
                                      [--session SESSION_ID] [--limit N]

The report speaks the benchmark's behavioral vocabulary (rounds,
tool_calls, tool_errors, repeated_reads) computed over real recorded
sessions, so experiment selection can follow observed friction.
"""

from __future__ import annotations

import argparse
import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent.parent))

from mini_loop.mining import (  # noqa: E402
    bash_profile, mine, model_profile, render, render_bash, render_model,
    render_time, time_profile,
)
from mini_loop.trajectory import TrajectoryStore  # noqa: E402


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--root", default="workspaces/.trajectories")
    parser.add_argument("--session", default=None)
    parser.add_argument("--limit", type=int, default=50)
    parser.add_argument(
        "--since-hours", type=float, default=None, metavar="H",
        help="only trajectories started in the last H hours (era slicing "
             "for before/after readings)",
    )
    parser.add_argument(
        "--until-hours", type=float, default=None, metavar="H",
        help="only trajectories started more than H hours ago",
    )
    args = parser.parse_args(argv)

    root = pathlib.Path(args.root)
    if not root.exists():
        print(f"no trajectory root at {root}", file=sys.stderr)
        return 2
    import time

    now = time.time()
    since = now - args.since_hours * 3600 if args.since_hours else None
    until = now - args.until_hours * 3600 if args.until_hours else None
    store = TrajectoryStore(root)
    window = dict(session_id=args.session, limit=args.limit,
                  since=since, until=until)
    print(render(mine(store, **window)))
    print()
    print(render_bash(bash_profile(store, **window)))
    print()
    print(render_model(model_profile(store, **window)))
    print()
    print(render_time(time_profile(store, **window)))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
