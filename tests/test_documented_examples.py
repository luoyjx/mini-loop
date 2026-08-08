"""The examples in README.md and EXTENDING.md, executed.

Seventy-two rounds changed constructor signatures, added seams, split modules
and renamed a few things, and nothing ever checked the user-facing docs against
the package. Documentation drift is invisible to every instrument here: mutation
testing, coverage and the AST sweeps all read `mini_loop/`, and a README that
describes an API from forty rounds ago passes all of them.

Two mechanical checks came back clean before this one -- every `from mini_loop
import ...` in the docs resolves, and every keyword argument the examples pass
is accepted by the current signature. That is a real negative result and it is
also the weaker question. This runs them.

Blocks that reference names the surrounding prose defines (`settings`, `client`,
`registry`, an `Agent`) get those from a preamble, which is what makes them
executable at all. Blocks whose point is a placeholder -- `MyPolicy`,
`my_search_api` -- are listed explicitly rather than stubbed into passing, so
"illustrative" stays a decision somebody made and not a silent skip.
"""

import ast
import asyncio
import pathlib
import re
import tempfile

import pytest

from mini_loop import Agent, SessionManager, Settings
from mini_loop.builtins import default_registry, full_registry
from mini_loop.fake_llm import FakeAsyncAnthropic

ROOT = pathlib.Path(__file__).resolve().parent.parent
DOCS = ("README.md", "EXTENDING.md")

#: Names an example uses that stand for something the reader supplies. Listed,
#: not stubbed: a stub would make every example "pass" and check nothing.
ILLUSTRATIVE = {
    "my_prompt", "my_metrics", "my_search_api", "MyPolicy", "my_hooks",
    "per_tenant_dir", "narrow_registry", "my_api", "my_bash_tool",
    "my_mcp_client", "AnotherHook", "workspace", "Tool",
}


def _blocks(name):
    text = (ROOT / name).read_text()
    return re.findall(r"```(?:python|py)\n(.*?)```", text, re.DOTALL)


def _all_blocks():
    return [(doc, index, code)
            for doc in DOCS
            for index, code in enumerate(_blocks(doc))]


def _preamble(tmp_path):
    """What the prose around these examples has already set up."""
    workspace = tmp_path / "ws"
    workspace.mkdir(parents=True, exist_ok=True)
    settings = Settings(
        fake_llm=True, workspace_root=workspace, skills_dir=ROOT / "skills"
    )
    return {
        "settings": settings,
        "client": FakeAsyncAnthropic(),
        "registry": full_registry(),
        "sandbox": None,
        "Agent": Agent,
        "SessionManager": SessionManager,
        "Settings": Settings,
        "Path": pathlib.Path,
    }


def _is_shorthand(code):
    """`Agent(..., cache_policy=X)` means "plus your other arguments".

    A documentation convention, not a runnable line: `...` is a real object and
    `Agent.__init__` is keyword-only, so executing it literally raises. The first
    version of this file reported eleven such blocks as broken examples -- the
    same false positive as round 59's scan that read prose and reported history.
    The convention is declared here so the executor can tell shorthand from
    staleness instead of guessing.
    """

    try:
        tree = ast.parse(code)
    except SyntaxError:
        return False
    return any(
        isinstance(node, ast.Constant) and node.value is Ellipsis
        for node in ast.walk(tree)
    )


def _uses_illustrative(code):
    try:
        tree = ast.parse(code)
    except SyntaxError:
        tree = ast.parse("async def _wrapper():\n"
                         + "\n".join("    " + line for line in code.splitlines()))
    return {
        node.id for node in ast.walk(tree)
        if isinstance(node, ast.Name) and node.id in ILLUSTRATIVE
    }


def _execute(code, namespace):
    """Run a block, wrapping it in a coroutine when it awaits."""
    try:
        compiled = compile(code, "<doc>", "exec")
    except SyntaxError as error:
        if "'await' outside function" not in str(error):
            raise
        wrapped = "async def _doc_example():\n" + "\n".join(
            "    " + line for line in code.splitlines()
        )
        exec(compile(wrapped, "<doc>", "exec"), namespace)
        asyncio.run(namespace["_doc_example"]())
        return
    exec(compiled, namespace)


IDS = [f"{doc}:{index}" for doc, index, _ in _all_blocks()]


@pytest.mark.parametrize("doc,index,code", _all_blocks(), ids=IDS)
def test_a_documented_example_still_works(tmp_path, doc, index, code):
    if _is_shorthand(code):
        pytest.skip("shorthand: `...` stands for the caller's other arguments")
    placeholders = _uses_illustrative(code)
    if placeholders:
        pytest.skip(f"illustrative: stands in for {sorted(placeholders)}")

    namespace = _preamble(tmp_path)
    namespace["__name__"] = "__doc_example__"
    try:
        _execute(code, namespace)
    except NameError as error:
        pytest.fail(
            f"{doc} block {index} uses a name the docs never define: {error}.\n"
            "Either the example is stale, or the prose lost the line that set it "
            f"up.\n\n{code.strip()[:300]}"
        )


def test_the_docs_still_contain_examples():
    """A regex that matched nothing would pass every case above."""
    assert len(_all_blocks()) >= 20


def test_most_examples_are_executable_not_illustrative():
    """If nearly everything is skipped, this file checks nothing.

    The count is asserted so that turning a broken example into an
    "illustrative" one to silence a failure shows up here.
    """
    total = len(_all_blocks())
    skipped = sum(
        1 for _, _, code in _all_blocks()
        if _uses_illustrative(code) or _is_shorthand(code)
    )
    assert skipped <= total * 2 // 3, (
        f"{skipped} of {total} examples are placeholders; this file is mostly "
        "skipping"
    )


def test_every_documented_import_resolves():
    """The cheaper check, kept: it localises a failure to the import rather than
    to whatever the example does with it."""
    import importlib

    missing = []
    for doc, index, code in _all_blocks():
        try:
            tree = ast.parse(code)
        except SyntaxError:
            continue
        for node in ast.walk(tree):
            if not isinstance(node, ast.ImportFrom):
                continue
            if not (node.module or "").startswith("mini_loop"):
                continue
            module = importlib.import_module(node.module)
            for alias in node.names:
                if not hasattr(module, alias.name):
                    missing.append(f"{doc}:{index} {node.module}.{alias.name}")
    assert not missing, "documented imports that no longer exist:\n  " + "\n  ".join(missing)
