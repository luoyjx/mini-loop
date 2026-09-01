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

from mini_loop.mining import bash_profile, mine, render, render_bash  # noqa: E402
from mini_loop.trajectory import TrajectoryStore  # noqa: E402


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--root", default="workspaces/.trajectories")
    parser.add_argument("--session", default=None)
    parser.add_argument("--limit", type=int, default=50)
    args = parser.parse_args(argv)

    root = pathlib.Path(args.root)
    if not root.exists():
        print(f"no trajectory root at {root}", file=sys.stderr)
        return 2
    store = TrajectoryStore(root)
    print(render(mine(store, session_id=args.session, limit=args.limit)))
    print()
    print(render_bash(bash_profile(
        store, session_id=args.session, limit=args.limit)))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
