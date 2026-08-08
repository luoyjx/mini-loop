"""Every process this harness starts, classified.

Round 58 found `run_in_background` reaching the same shell as `run_bash` with
none of its guards. That is a shape -- *a sibling path to a dangerous
primitive* -- and the answer to a shape is a scan, not another module read.

There are four spawn sites. Two run **model-supplied commands** and must go
through the sandbox and a scrubbed environment. Two run **harness-controlled
argv** and need not be sandboxed, but must never interpolate into a shell.

The scan found the fourth was worse than unsandboxed. An MCP server -- the least
trusted process here, since its command comes from config and its *behaviour*
comes from someone else entirely -- was started with no `env` at all. A probe
server reported reading:

    saw: ANTHROPIC_API_KEY, HOMEBREW_GITHUB_API_TOKEN, PROBE_API_KEY

including the harness's own model credential. And `list_tools()` had no timeout,
so an unresponsive server hung registration, which runs while the agent is being
built.
"""

import ast
import asyncio
import os
import pathlib
import sys
import time

import pytest

from mini_loop.mcp import StdioMCP
from mini_loop.secrets import SecretRegistry

PACKAGE = pathlib.Path(__file__).resolve().parent.parent / "mini_loop"
SECRET = "sk-MCP-SPAWN-0123456789abcdefgh"

SPAWN_CALLS = {"run", "Popen", "call", "check_call", "check_output",
               "create_subprocess_exec", "create_subprocess_shell",
               "system", "popen"}

#: Runs a command the model supplied: must be confined and scrubbed.
MODEL_SUPPLIED = {"tools.py", "background.py"}
#: Runs a fixed argv the harness controls: no shell, and the arguments are
#: validated before they get here.
HARNESS_CONTROLLED = {"worktrees.py", "mcp.py"}


def _spawn_sites() -> dict[str, list[int]]:
    found: dict[str, list[int]] = {}
    for path in sorted(PACKAGE.rglob("*.py")):
        for node in ast.walk(ast.parse(path.read_text())):
            if not isinstance(node, ast.Call):
                continue
            rendered = ast.unparse(node.func)
            if (rendered.rsplit(".", 1)[-1] in SPAWN_CALLS
                    and any(m in rendered for m in ("subprocess", "asyncio.", "os."))):
                found.setdefault(path.name, []).append(node.lineno)
    return found


def test_every_spawn_site_is_classified():
    """A new one fails here until someone says which kind it is."""
    sites = _spawn_sites()
    assert sites, "no spawn sites found -- the scan broke, not the package"
    unclassified = sorted(set(sites) - MODEL_SUPPLIED - HARNESS_CONTROLLED)
    assert not unclassified, (
        "these modules start a process and nobody has said whether the command "
        f"comes from the model (confine it) or the harness: {unclassified}"
    )


def test_the_classification_has_no_dead_entries():
    sites = _spawn_sites()
    stale = sorted((MODEL_SUPPLIED | HARNESS_CONTROLLED) - set(sites))
    assert not stale, f"classified but no longer spawns anything: {stale}"


def test_no_model_supplied_command_goes_through_a_shell():
    """`create_subprocess_shell` takes a string; the sandbox owns argv instead.

    Checked against the AST, not the source text. The first version searched for
    the substring and failed on a *docstring* in `background.py` describing the
    behaviour it had just been fixed away from -- a scan that reads prose is a
    scan that reports history.
    """
    for name in MODEL_SUPPLIED:
        tree = ast.parse((PACKAGE / name).read_text())
        for node in ast.walk(tree):
            if not isinstance(node, ast.Call):
                continue
            rendered = ast.unparse(node.func)
            assert not rendered.endswith("create_subprocess_shell"), f"{name}: {rendered}"
            assert not any(
                keyword.arg == "shell"
                and getattr(keyword.value, "value", None) is True
                for keyword in node.keywords
            ), f"{name}: shell=True"


# --- the site the scan caught --------------------------------------------

@pytest.fixture
def probe_server(tmp_path, monkeypatch):
    monkeypatch.setenv("PROBE_API_KEY", SECRET)
    script = tmp_path / "srv.py"
    script.write_text(
        "import json, os, sys\n"
        "for line in sys.stdin:\n"
        "    m = json.loads(line)\n"
        "    if m.get('method') == 'initialize':\n"
        "        print(json.dumps({'jsonrpc':'2.0','id':m['id'],'result':{}}), flush=True)\n"
        "    elif m.get('method') == 'tools/list':\n"
        "        seen = sorted(k for k in os.environ if 'API_KEY' in k)\n"
        "        print(json.dumps({'jsonrpc':'2.0','id':m['id'],'result':{'tools':[\n"
        "            {'name':'env_report','description':'saw:' + ','.join(seen),\n"
        "             'inputSchema':{'type':'object'}}]}}), flush=True)\n"
    )
    return [sys.executable, str(script)]


def _describe(client):
    async def run():
        try:
            tools = await client.list_tools()
            return tools[0]["description"]
        finally:
            await client.close()

    return asyncio.run(run())


def test_a_server_does_not_inherit_registered_credentials(probe_server):
    client = StdioMCP("probe", probe_server, secrets=SecretRegistry.from_environ())
    assert "PROBE_API_KEY" not in _describe(client)


def test_the_harnesss_own_model_credential_is_withheld(probe_server, monkeypatch):
    monkeypatch.setenv("ANTHROPIC_API_KEY", "sk-ant-the-harnesss-own-credential")
    client = StdioMCP("probe", probe_server, secrets=SecretRegistry.from_environ())
    assert "ANTHROPIC_API_KEY" not in _describe(client)


def test_a_server_gets_what_it_is_configured_to_get(probe_server):
    """Scrubbing that breaks every real server is not a fix."""
    client = StdioMCP("probe", probe_server, secrets=SecretRegistry.from_environ(),
                      env_passthrough=["PROBE_API_KEY"])
    assert "PROBE_API_KEY" in _describe(client)


def test_what_was_withheld_is_reported(probe_server):
    """"My server cannot see its token" is otherwise a mystery."""
    client = StdioMCP("probe", probe_server, secrets=SecretRegistry.from_environ())
    _describe(client)
    assert "PROBE_API_KEY" in client.withheld


def test_an_unresponsive_server_cannot_hang_registration(tmp_path):
    script = tmp_path / "hang.py"
    script.write_text("import time\ntime.sleep(3600)\n")
    client = StdioMCP("slow", [sys.executable, str(script)], timeout=1.0)

    async def run():
        started = time.monotonic()
        timed_out = False
        try:
            # An *outer* bound as well as the inner one. Without it, a build
            # whose timeout is missing hangs for the server's full hour, and the
            # mutation runner reports that as its own failure rather than as a
            # caught mutation. A test for a timeout must not depend on the
            # timeout it is testing.
            await asyncio.wait_for(client.list_tools(), timeout=6.0)
        except asyncio.TimeoutError:
            timed_out = True
        elapsed = time.monotonic() - started
        await client.close()
        return timed_out, elapsed

    timed_out, elapsed = asyncio.run(run())
    assert timed_out, "an unresponsive server was not refused at all"
    assert elapsed < 4.0, (
        f"registration took {elapsed:.1f}s; the client's own timeout did not fire, "
        "the outer bound did"
    )


def test_a_server_with_no_registry_still_runs(probe_server):
    """The seam is optional, as everywhere else here."""
    assert "saw:" in _describe(StdioMCP("probe", probe_server))
