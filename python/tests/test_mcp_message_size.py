"""Any MCP result over 64 KiB failed, and 64 KiB is small.

Round 60's instrument was an inventory of *unbounded waits*, since two previous
rounds each found one in MCP -- the tool call (49) and the startup handshake
(59). The inventory pointed at `self._proc.stdout.readline()`, and the defect
there was not the wait but the size.

asyncio's stream reader defaults to 64 KiB per line and MCP frames one JSON
message per line, so:

    server returns   60,000 chars -> ok
    server returns   70,000 chars -> ValueError: Separator is found, but chunk
                                     is longer than limit
    server returns  500,000 chars -> ValueError: Separator is not found...

A tool returning file contents, search results or a fetched page passes 64 KiB
routinely. It surfaced as `ValueError` from inside asyncio, which reads like a
harness bug rather than a limit, and it was flagged as a suspicion in round 49
and never checked.

Three parts to the fix, because raising a limit alone trades one failure for
another: the line limit is raised so a legitimate result arrives, the result is
capped at the bound `run_bash` output already uses so it cannot become the whole
context, and anything past even the raised limit is reported as a server fault
in words rather than as an asyncio internal.
"""

import asyncio
import pathlib
import sys

import pytest

from mini_loop.mcp import MAX_RPC_LINE, MAX_TOOL_RESULT, StdioMCP

SERVER = """
import json, sys
SIZE = int(sys.argv[1])
for line in sys.stdin:
    m = json.loads(line)
    if m.get("method") == "initialize":
        print(json.dumps({"jsonrpc":"2.0","id":m["id"],"result":{}}), flush=True)
    elif m.get("method") == "tools/list":
        print(json.dumps({"jsonrpc":"2.0","id":m["id"],"result":{"tools":[
          {"name":"big","description":"d","inputSchema":{"type":"object"}}]}}), flush=True)
    elif m.get("method") == "tools/call":
        print(json.dumps({"jsonrpc":"2.0","id":m["id"],"result":{"content":[
          {"type":"text","text":"X"*SIZE}]}}), flush=True)
"""


@pytest.fixture
def server(tmp_path):
    script = tmp_path / "srv.py"
    script.write_text(SERVER)
    return script


def _call(script, size):
    client = StdioMCP("big", [sys.executable, str(script), str(size)], timeout=20)

    async def run():
        try:
            await client.list_tools()
            return await client.call_tool("big", {})
        finally:
            await client.close()

    return asyncio.run(run())


@pytest.mark.parametrize("size", [1_000, 60_000, 70_000, 200_000])
def test_a_result_of_any_reasonable_size_arrives(server, size):
    """70,000 is the case that used to raise, and it is not a large result."""
    result = _call(server, size)
    assert result.startswith("X")


def test_a_small_result_is_untouched(server):
    assert _call(server, 1_000) == "X" * 1_000


@pytest.mark.parametrize("size", [70_000, 500_000, 5_000_000])
def test_an_oversized_result_is_capped_not_dropped(server, size):
    """Raising the limit alone would trade a hard failure for an unbounded
    context; capping alone would keep the hard failure at 64 KiB."""
    result = _call(server, size)
    assert len(result) < MAX_TOOL_RESULT + 200
    assert "truncated" in result
    assert f"{size:,}" in result, "the caller should learn how much there was"


def test_the_cap_matches_what_other_tool_output_uses():
    from mini_loop.tools import OUTPUT_CAP

    assert MAX_TOOL_RESULT == OUTPUT_CAP, (
        "an MCP result and a bash result land in the same context and should "
        "not have different ceilings"
    )


def test_a_message_past_even_the_raised_limit_reads_as_a_server_fault(tmp_path):
    """`ValueError: Separator is found...` from inside asyncio reads like a
    harness bug; the server is what sent an 8 MB line."""
    script = tmp_path / "huge.py"
    script.write_text(SERVER)
    client = StdioMCP(
        "huge", [sys.executable, str(script), str(MAX_RPC_LINE + 1_000)], timeout=30
    )

    async def run():
        try:
            await client.list_tools()
            with pytest.raises(RuntimeError, match="larger than"):
                await client.call_tool("big", {})
        finally:
            await client.close()

    asyncio.run(run())
