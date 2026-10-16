#!/usr/bin/env python3
"""Compatibility entry point; implementation lives in tooling.acceptance.scenarios.langgraph_smoke."""
from pathlib import Path
import sys

# Direct script execution puts scripts/, rather than the checkout root, on sys.path.
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from tooling.acceptance.scenarios.langgraph_smoke import run

if __name__ == "__main__":
    raise SystemExit(run())
