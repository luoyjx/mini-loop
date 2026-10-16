"""Checkout locations shared by repository tooling, never runtime configuration."""
from pathlib import Path

MODULE = Path(__file__).resolve().parents[1]
FIXTURES = MODULE / "tooling/acceptance/fixtures"
