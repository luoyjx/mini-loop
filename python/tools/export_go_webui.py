#!/usr/bin/env python3
"""Copy the Python browser sources into the independent Go binary's embed inputs."""
from __future__ import annotations

import argparse
import ast
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / 'python' / 'mini_loop'
TARGET = ROOT / 'go' / 'httpapi' / 'uiassets'


def snapshot() -> dict[str, bytes]:
    module = ast.parse((SOURCE / 'server.py').read_text(encoding='utf-8'))
    assignments = [node for node in module.body if isinstance(node, ast.Assign)
                   and any(isinstance(target, ast.Name) and target.id == 'CONSOLE_HTML'
                           for target in node.targets)]
    if len(assignments) != 1:
        raise ValueError('expected one literal CONSOLE_HTML source assignment')
    console = ast.literal_eval(assignments[0].value)
    if not isinstance(console, str):
        raise ValueError('console source must be a string')
    return {'console.html': console.encode('utf-8'),
            **{name: (SOURCE / 'webui' / name).read_bytes()
               for name in ('index.html', 'app.css', 'app.js')}}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    assets = snapshot()
    if args.check:
        stale = [name for name, body in assets.items()
                 if not (TARGET / name).is_file() or (TARGET / name).read_bytes() != body]
        if stale:
            print('Stale Go browser assets: ' + ', '.join(stale), file=sys.stderr)
            return 1
        print(f'Python→Go browser assets current: {len(assets)} files')
        return 0
    TARGET.mkdir(parents=True, exist_ok=True)
    for name, body in assets.items():
        (TARGET / name).write_bytes(body)
        print(f'wrote {TARGET / name} ({len(body)} bytes)')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
