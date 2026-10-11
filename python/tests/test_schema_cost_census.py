"""Schema-cost census: the fixed per-request price of the tool catalogue.

Every provider request carries every tool schema. This census measures
that fixed cost deterministically and pins a band, so the price stays a
number nobody is surprised by -- a future tool addition, or an enum dump
into an input_schema, trips a named pin instead of silently taxing every
request forever (docs/RSI_RESEARCH_AND_PLAN.md §5).

Measured 2026-09-01: 10 tools, ~3.3k serialized chars (~840 estimated
tokens) per request. Verdict: lean, no slimming experiment warranted --
the heaviest tool (bash, ~730 chars) spends its weight on the
approval_prefix guidance, which is load-bearing prompt engineering from
the approval-as-learning round, not bloat. The census exists so that
verdict stays earned rather than assumed.
"""

import json
import pathlib

import pytest

from mini_loop import SessionManager, Settings
from mini_loop.fake_llm import FakeAsyncAnthropic

SKILLS = pathlib.Path(__file__).resolve().parent.parent / "skills"


@pytest.fixture
def schemas(tmp_path, monkeypatch):
    # Optional surfaces (ast-outline, workflows) add tools; the census
    # prices the always-on core catalogue, so they are pinned off here.
    monkeypatch.delenv("MINILOOP_AST_OUTLINE_ENABLED", raising=False)
    settings = Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                        skills_dir=SKILLS, spill_dir=None)
    manager = SessionManager(settings, FakeAsyncAnthropic())
    return manager.create().agent.tools.schemas()


def test_the_catalogue_price_is_a_number_not_a_surprise(schemas):
    total = sum(len(json.dumps(schema)) for schema in schemas)
    assert len(schemas) == 10, (
        "the core catalogue changed size -- price the change: re-measure "
        "and update this census deliberately, it costs every request"
    )
    assert 2_000 < total < 5_000, (
        f"catalogue cost drifted out of its band: {total:,} chars"
    )


def test_no_single_tool_dominates_the_catalogue(schemas):
    for schema in schemas:
        size = len(json.dumps(schema))
        assert size < 1_200, (
            f"{schema['name']} costs {size:,} chars of every request -- "
            "an enum dump or a description essay; trim it or re-earn the "
            "census verdict"
        )


def test_every_tool_pays_for_a_description(schemas):
    """A schema without a description is context spent teaching nothing:
    the model gets the name and the shape and has to guess the point."""

    for schema in schemas:
        assert schema.get("description"), (
            f"{schema['name']} ships no description"
        )
