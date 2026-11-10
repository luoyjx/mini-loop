#!/usr/bin/env python3
"""Compatibility entry point for the P1.5/P1.6 local acceptance scenario."""

from tooling.acceptance.scenarios.task_protocol_failures import main


if __name__ == "__main__":
    raise SystemExit(main())
