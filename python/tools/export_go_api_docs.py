"""Copy/check Source documentation resources embedded by the independent Go server."""
from __future__ import annotations

import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from tools.api_documentation_contracts import documentation_assets

ROOT = Path(__file__).resolve().parents[2]
TARGET = ROOT / 'go/httpapi/docsassets'


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    assets = documentation_assets()
    if args.check:
        stale = [name for name, body in assets.items()
                 if not (TARGET / name).is_file() or (TARGET / name).read_bytes() != body]
        if stale:
            print('Stale Go documentation resources: ' + ', '.join(stale), file=sys.stderr)
            return 1
        print(f'Python→Go documentation resources current: {len(assets)} files')
        return 0
    TARGET.mkdir(parents=True, exist_ok=True)
    for name, body in assets.items():
        (TARGET / name).write_bytes(body)
        print(f'wrote {TARGET / name} ({len(body)} bytes)')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
