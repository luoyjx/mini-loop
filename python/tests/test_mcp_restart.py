"""A crashed MCP server must not be permanently broken.

An MCP server is a subprocess -- the least-trusted component the harness runs,
and the one most likely to die: it can crash, be OOM-killed, or simply exit.
`_start` short-circuits when `self._proc is not None`, so once the process died
the client kept writing to a closed pipe and *every* subsequent call failed
forever. A single transient crash bricked the server's tools for the whole
session, even though restarting the subprocess would recover it.

`_start` now treats a process whose `returncode` is set as gone and starts a
fresh one on the next call. The in-flight call that hit the dead server still
fails -- deliberately: a `tools/call` that died mid-flight may already have
taken effect, so silently re-issuing it could double-execute a side effect. The
guarantee is only that the *next* call reaches a live server.
"""

import asyncio
import sys

import pytest

from mini_loop.mcp import StdioMCP

# A persistent stdin loop: handles the handshake and answers every tools/call
# with "ok". Restartable -- a fresh process behaves identically, which is what
# recovery relies on.
SERVER = """
import json, sys
for line in sys.stdin:
    m = json.loads(line)
    if m.get("id") is None:
        continue
    if m.get("method") == "tools/call":
        print(json.dumps({"jsonrpc":"2.0","id":m["id"],"result":{"content":[
          {"type":"text","text":"ok"}]}}), flush=True)
    else:
        print(json.dumps({"jsonrpc":"2.0","id":m["id"],"result":{}}), flush=True)
"""


@pytest.fixture
def script(tmp_path):
    path = tmp_path / "srv.py"
    path.write_text(SERVER)
    return path


def test_a_dead_server_is_restarted_on_the_next_call(script):
    client = StdioMCP("crash", [sys.executable, str(script)], timeout=20)

    async def run():
        try:
            assert await client.call_tool("t", {}) == "ok"

            # The server process dies (stand-in for a crash / OOM-kill).
            corpse = client._proc
            corpse.kill()
            await corpse.wait()
            assert corpse.returncode is not None

            # Reusing the corpse would raise forever; a fresh process answers.
            assert await client.call_tool("t", {}) == "ok"
            assert client._proc is not corpse, "the dead process was reused"
            assert client._proc.returncode is None, "the replacement is alive"
        finally:
            await client.close()

    asyncio.run(run())


def test_a_live_server_is_never_restarted(script):
    """The restart must trigger only on death -- a healthy server is reused
    across calls, not respawned every time (which would lose its state and
    thrash processes)."""
    client = StdioMCP("stable", [sys.executable, str(script)], timeout=20)

    async def run():
        try:
            assert await client.call_tool("t", {}) == "ok"
            proc = client._proc
            assert await client.call_tool("t", {}) == "ok"
            assert client._proc is proc, "a live server was needlessly respawned"
        finally:
            await client.close()

    asyncio.run(run())
