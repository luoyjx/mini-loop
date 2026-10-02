"""One value, two shapes — the trap that has bitten this codebase five times.

A response's content is provider *objects*. `_content_payload` turns them into
dicts for the transcript. `ContinuedResponse` hands dicts back *as a response*.
Both shapes reach the same code, and every reader that assumed one has been a
bug, in five different subsystems:

1. the event stream emitted objects a JSON encoder could not serialize,
2. durable tables kept a credential — a masker walked dicts past objects,
3. compaction could not clear a tool result it did not recognize as one,
4. continuation returned an empty answer — the extractor read `.text` off a dict,
5. the compaction *summary* came back empty when its own request was truncated.

Number 5 is the one worth dwelling on. Its next line replaces the **entire**
transcript with `[Context compressed. path]\\n{summary}`, so an empty summary
means the agent discards everything it knows and receives a file path in
return. It was reached by an ordinary condition — summarizing a large
transcript against an 8k output budget truncates — and it was made *worse* by
the fix for number 4 one round earlier: before, this path silently kept the last
chunk of the summary; after, it kept nothing.

That is the argument for enforcing the rule rather than fixing sites. Fix four
addressed the writer and left the readers alone; fix five would have been the
same if it stopped at two call sites.
"""

import ast
import pathlib

import pytest

from mini_loop.agent import _content_payload
from mini_loop.blocks import block_field, block_text, blocks_of_type
from mini_loop.fake_llm import text, thinking, tool

PACKAGE = pathlib.Path(__file__).resolve().parent.parent / "mini_loop"

#: Field names that only exist on a content block.
BLOCK_FIELDS = {"text", "type", "name", "input", "id", "signature", "thinking"}

#: `_content_payload` is the normalizer: converting objects to dicts is its job,
#: and it tests `isinstance(block, dict)` before any attribute read.
ALLOWED = {("blocks.py", None), ("agent.py", "_content_payload")}


def _enclosing_function(tree, target):
    for node in ast.walk(tree):
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
            for child in ast.walk(node):
                if child is target:
                    return node.name
    return None


def test_the_scan_reaches_the_package():
    """The anchor for the negative assertion below.

    "No module does X" is trivially true of no modules. Round 82 showed a scan
    losing its subject to an unrelated refactor and going green, and
    `tools/verify_scans.py` found this module to be the one whose *entire*
    scanning surface passed with nothing to scan. Without this, deleting or
    moving the package would make the guard below report success forever.
    """

    modules = sorted(PACKAGE.rglob("*.py"))
    assert len(modules) > 20, f"the block-access scan sees {len(modules)} modules"
    assert any(p.name == "blocks.py" for p in modules)
    # The rule exists because blocks are read all over the package; a scan that
    # cannot see a single `getattr` is not looking at this codebase.
    assert any("getattr(" in p.read_text() for p in modules)


def test_no_module_reads_a_block_by_attribute():
    """The rule, enforced. A sixth occurrence fails here instead of in production."""
    offenders = []
    for path in sorted(PACKAGE.rglob("*.py")):
        source = path.read_text()
        tree = ast.parse(source)
        for node in ast.walk(tree):
            if not (isinstance(node, ast.Call)
                    and getattr(node.func, "id", None) == "getattr"
                    and len(node.args) >= 2
                    and isinstance(node.args[1], ast.Constant)
                    and node.args[1].value in BLOCK_FIELDS):
                continue
            target = ast.unparse(node.args[0])
            if not any(hint in target for hint in ("block", "part", "b", "item")):
                continue
            function = _enclosing_function(tree, node)
            if (path.name, function) in ALLOWED or (path.name, None) in ALLOWED:
                continue
            offenders.append(
                f"{path.relative_to(PACKAGE)}:{node.lineno} in {function}(): "
                f"{ast.unparse(node)}"
            )
    assert not offenders, (
        "read blocks through mini_loop.blocks, not by attribute:\n  "
        + "\n  ".join(offenders)
    )


# --- the accessors behave identically on either shape ---------------------

@pytest.fixture(params=["object", "dict"])
def shape(request):
    return request.param


def _maybe_dicts(blocks, shape):
    return _content_payload(blocks) if shape == "dict" else blocks


def test_block_text_joins_only_text_blocks(shape):
    content = _maybe_dicts(
        [thinking("reasoning"), text("hello "), tool("run_bash", command="x"),
         text("world")],
        shape,
    )
    assert block_text(content) == "hello world"


def test_block_text_of_a_thinking_only_response_is_empty(shape):
    assert block_text(_maybe_dicts([thinking("just reasoning")], shape)) == ""


def test_block_text_survives_empty_and_missing_content():
    assert block_text([]) == ""
    assert block_text(None) == ""


def test_blocks_of_type_selects_shape_agnostically(shape):
    content = _maybe_dicts(
        [text("a"), tool("run_bash", _id="t1", command="x"), thinking("r")], shape
    )
    found = blocks_of_type(content, "tool_use")
    assert len(found) == 1
    assert block_field(found[0], "name") == "run_bash"
    assert block_field(found[0], "id") == "t1"


def test_block_field_falls_back_the_same_way(shape):
    block = _maybe_dicts([text("hi")], shape)[0]
    assert block_field(block, "nope") is None
    assert block_field(block, "nope", "default") == "default"


def test_a_truncated_summary_still_produces_a_summary(tmp_path):
    """Guarding the consequence, not just the accessor.

    This is the read whose failure discards a whole transcript.
    """
    import asyncio

    from mini_loop import SessionManager, Settings
    from mini_loop.compaction import DefaultCompactor
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.recovery import DefaultRecovery

    turns = [([text("SUMMARY-ONE. ")], "max_tokens"),
             ([text("SUMMARY-TWO.")], "end_turn")]
    state = {"i": 0}

    def responder(request):
        index = state["i"]
        state["i"] += 1
        return turns[index] if index < len(turns) else ([text("x")], "end_turn")

    agent = SessionManager(
        Settings(fake_llm=True, workspace_root=tmp_path / "ws",
                 skills_dir=pathlib.Path(__file__).resolve().parent.parent / "skills"),
        FakeAsyncAnthropic(responder=responder),
        recovery=DefaultRecovery(escalate=False),
    ).create().agent
    agent.messages.extend(
        [{"role": "user", "content": "work"},
         {"role": "assistant", "content": [{"type": "text", "text": "ok"}]}] * 6
    )

    asyncio.run(DefaultCompactor().compact(agent))
    remembered = agent.messages[0]["content"]
    assert "SUMMARY-ONE" in remembered and "SUMMARY-TWO" in remembered, (
        f"the transcript was replaced by a summary of nothing: {remembered!r}"
    )
