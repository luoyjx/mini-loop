#!/usr/bin/env python3
"""Compatibility entry point; implementation lives in tooling.build_task_bundle."""
from pathlib import Path
import sys

# Direct script execution puts scripts/, rather than the checkout root, on sys.path.
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from tooling.build_task_bundle import main

if __name__ == "__main__":
    raise SystemExit(main())
