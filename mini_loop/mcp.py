"""MCP plugin support (s19): pull external tools into the registry.

An `MCPClient` abstracts a tool server's two operations -- list tools, call a
tool. `connect_mcp` discovers a server's tools and registers each into the
agent's `ToolRegistry`, namespaced `mcp__<server>__<tool>`, with a handler that
routes calls back to the client. Because tools are just registry entries, MCP
tools compose with built-ins and custom tools seamlessly.

Two transports:
  * `InProcessMCP` -- Python handlers in-process (great for tests and embedding);
  * `StdioMCP`     -- a real subprocess speaking newline-delimited JSON-RPC
                      (initialize / tools/list / tools/call).
"""

from __future__ import annotations

import asyncio
import json
import asyncio
import re

from .problems import ProblemLog
from .registry import Tool, ToolContext, ToolRegistry


#: Registered as `mcp__<server>__<tool>`, so `__` is the separator -- and it was
#: also legal *inside* a component. Server `alpha__beta` with tool `gamma`
#: produced the same key as server `alpha` with tool `beta__gamma`, and the
#: second registration silently replaced the first. Runs of underscore collapse
#: to one so a component can never contain the separator.
def normalize_name(name: str) -> str:
    return re.sub(r"_+", "_", re.sub(r"[^a-zA-Z0-9_-]", "_", name)).strip("_") or "unnamed"


#: A tool schema is sent on *every* request. One server declared a 2,000,000
#: character description -- roughly 500,000 tokens per call.
MAX_TOOL_DESCRIPTION = 4_000

#: A hung server otherwise blocks the turn forever; `run_bash` has had a timeout
#: since the beginning and this had none.
DEFAULT_MCP_TIMEOUT = 60.0

#: asyncio's stream reader defaults to 64 KiB per line, and MCP frames one JSON
#: message per line. Any tool result over that raised
#: ``ValueError: Separator is found, but chunk is longer than limit`` -- and
#: 64 KiB is small for a tool returning file contents or search results:
#:
#:     server returns   60,000 chars -> ok
#:     server returns   70,000 chars -> ValueError
#:     server returns  500,000 chars -> ValueError
MAX_RPC_LINE = 8 * 1024 * 1024

#: What a single tool result may contribute to the conversation. The raised line
#: limit stops a legitimate large result from failing; this stops an unbounded
#: one from becoming the context. Same bound `run_bash` output uses.
MAX_TOOL_RESULT = 50_000


class MCPClient:
    name: str

    async def list_tools(self) -> list[dict]:
        raise NotImplementedError

    async def call_tool(self, tool: str, args: dict) -> str:
        raise NotImplementedError

    async def close(self) -> None:
        pass


class InProcessMCP(MCPClient):
    """Tools backed by local (async or sync) Python callables.

    tools = [{"name","description","input_schema","handler"}], where handler is
    `(**args) -> str | awaitable`.
    """

    def __init__(self, name: str, tools: list[dict]) -> None:
        self.name = name
        self._defs = [{k: t[k] for k in ("name", "description", "input_schema", "annotations") if k in t}
                      for t in tools]
        self._handlers = {t["name"]: t["handler"] for t in tools}

    async def list_tools(self) -> list[dict]:
        return list(self._defs)

    async def call_tool(self, tool: str, args: dict) -> str:
        handler = self._handlers.get(tool)
        if handler is None:
            return f"Error: unknown MCP tool {tool}"
        try:
            res = handler(**args)
            if asyncio.iscoroutine(res):
                res = await res
            return str(res)
        except Exception as e:
            return f"Error: {e}"


class StdioMCP(MCPClient):
    """A subprocess MCP server over newline-delimited JSON-RPC (best-effort)."""

    #: An MCP server is the least trusted process this harness starts, and it
    #: inherited the whole environment. A probe server reported reading
    #: ``ANTHROPIC_API_KEY, HOMEBREW_GITHUB_API_TOKEN, PROBE_API_KEY`` -- the
    #: harness's own model credential among them. The environment is scrubbed of
    #: registered secrets and a server names what it actually needs, which is the
    #: same narrow-injection rule `run_bash` follows.
    def __init__(self, name: str, command: list[str], *, secrets=None,
                 env_passthrough: "tuple[str, ...] | list[str]" = (),
                 timeout: float = DEFAULT_MCP_TIMEOUT) -> None:
        self.name = name
        self.command = command
        self._proc: asyncio.subprocess.Process | None = None
        self._id = 0
        self._lock = asyncio.Lock()
        self._start_lock = asyncio.Lock()
        from .secrets import NullSecretRegistry

        self.secrets = secrets or NullSecretRegistry()
        self.env_passthrough = tuple(env_passthrough)
        self.timeout = timeout
        #: Credentials withheld from this server. Reported, because "my server
        #: cannot see its token" is otherwise a mystery.
        self.withheld: tuple[str, ...] = ()

    async def _start(self) -> None:
        async with self._start_lock:
            if self._proc is not None and self._proc.returncode is None:
                return
            # A previous process died -- a crash, an OOM-kill, or the server
            # simply exited. Reusing the corpse writes to a closed pipe and
            # fails forever, so a transient failure of the least-trusted
            # component permanently bricks its tools. Drop it and start fresh.
            # We restart the *process* but never auto-retry an in-flight call:
            # a `tools/call` that died mid-flight may already have taken effect,
            # and re-issuing it could double-execute a side effect. The failed
            # call surfaces as an error; the next call gets a live server.
            self._proc = await asyncio.create_subprocess_exec(
                *self.command, env=self._environment(), limit=MAX_RPC_LINE,
                stdin=asyncio.subprocess.PIPE, stdout=asyncio.subprocess.PIPE)
            try:
                # Bounded: an unresponsive server otherwise hangs registration,
                # which happens while the agent is being built.
                await asyncio.wait_for(self._handshake(), timeout=self.timeout)
            except BaseException:
                process, self._proc = self._proc, None
                if process is not None and process.returncode is None:
                    process.kill()
                    await process.wait()
                raise

    def _environment(self) -> dict:
        """The process environment minus registered secrets, plus what is asked for."""

        import os

        environment = self.secrets.scrub_env(os.environ)
        withheld = []
        for name in self.env_passthrough:
            value = os.environ.get(name)
            if value is not None:
                environment[name] = value
        for name in self.secrets.names():
            if name not in self.env_passthrough:
                withheld.append(name)
        self.withheld = tuple(sorted(withheld))
        return environment

    async def _handshake(self) -> None:
            try:
                await self._rpc("initialize", {
                    "protocolVersion": "2024-11-05",
                    "capabilities": {}, "clientInfo": {"name": "mini-loop", "version": "0.1.0"},
                })
                await self._notify("notifications/initialized", {})
            except BaseException:
                raise

    async def _notify(self, method: str, params: dict) -> None:
        assert self._proc and self._proc.stdin
        message = {"jsonrpc": "2.0", "method": method, "params": params}
        self._proc.stdin.write((json.dumps(message) + "\n").encode())
        await self._proc.stdin.drain()

    async def _rpc(self, method: str, params: dict) -> dict:
        assert self._proc and self._proc.stdin and self._proc.stdout
        async with self._lock:
            self._id += 1
            request_id = self._id
            msg = {"jsonrpc": "2.0", "id": request_id, "method": method, "params": params}
            self._proc.stdin.write((json.dumps(msg) + "\n").encode())
            await self._proc.stdin.drain()
            while True:
                try:
                    line = await self._proc.stdout.readline()
                except ValueError as error:
                    # Past even the raised limit. Reported as a server fault
                    # rather than surfacing asyncio's "Separator is found, but
                    # chunk is longer than limit", which reads like a harness bug.
                    raise RuntimeError(
                        f"MCP server '{self.name}' sent a message larger than "
                        f"{MAX_RPC_LINE:,} bytes: {error}"
                    ) from error
                if not line:
                    raise RuntimeError(f"MCP server '{self.name}' closed stdout")
                response = json.loads(line.decode())
                if response.get("id") != request_id:
                    continue  # server notification or another out-of-band message
                if "error" in response:
                    raise RuntimeError(f"MCP error: {response['error']}")
                return response.get("result", {})

    async def list_tools(self) -> list[dict]:
        await self._start()
        result = await asyncio.wait_for(
            self._rpc("tools/list", {}), timeout=self.timeout
        )
        out = []
        for t in result.get("tools", []):
            out.append({"name": t["name"], "description": t.get("description", ""),
                        "input_schema": t.get("inputSchema", {"type": "object", "properties": {}}),
                        "annotations": t.get("annotations", {})})
        return out

    async def call_tool(self, tool: str, args: dict) -> str:
        await self._start()
        result = await self._rpc("tools/call", {"name": tool, "arguments": args})
        content = result.get("content", [])
        rendered = "\n".join(
            c.get("text", "") for c in content if isinstance(c, dict)
        ) or json.dumps(result)[:MAX_TOOL_RESULT]
        if len(rendered) > MAX_TOOL_RESULT:
            # Capped like any other tool output. The raised line limit lets a
            # legitimately large result arrive; this stops it becoming the
            # whole context.
            rendered = (
                rendered[:MAX_TOOL_RESULT]
                + f"\n[truncated from {len(rendered):,} characters]"
            )
        return rendered

    async def close(self) -> None:
        if self._proc:
            process, self._proc = self._proc, None
            if process.returncode is None:
                process.terminate()
                try:
                    await asyncio.wait_for(process.wait(), timeout=2)
                except asyncio.TimeoutError:
                    process.kill()
                    await process.wait()


async def register_mcp(agent, client: MCPClient, *, timeout: float | None = None) -> list[str]:
    """Discover a client's tools and register them into the agent's registry.

    Refusals and collisions are recorded in ``agent.state["mcp_problems"]``
    rather than swallowed: a tool that quietly failed to appear, or one server's
    tool quietly replaced by another's, is indistinguishable from a server that
    was never connected.
    """

    server = normalize_name(client.name)
    owners = agent.state.setdefault("mcp_tool_owner", {})
    problems = agent.state.setdefault("mcp_problems", ProblemLog())
    limit = DEFAULT_MCP_TIMEOUT if timeout is None else timeout
    added = []
    for t in await client.list_tools():
        prefixed = f"mcp__{server}__{normalize_name(t['name'])}"
        owner = owners.get(prefixed)
        if owner is not None and owner != client.name:
            problems.append(
                f"{prefixed}: refused, already provided by server {owner!r}"
            )
            continue
        owners[prefixed] = client.name

        def make_handler(orig: str, c: MCPClient, seconds: float):
            async def handler(ctx: ToolContext, **kwargs):
                # Bounded. Without this a server that accepts a call and never
                # answers holds the turn open indefinitely, and the only way out
                # is to kill the process.
                try:
                    return await asyncio.wait_for(
                        c.call_tool(orig, kwargs), timeout=seconds
                    )
                except asyncio.TimeoutError:
                    return f"Error: MCP tool {orig!r} timed out after {seconds}s"
            return handler

        annotations = t.get("annotations") or {}
        description = str(t.get("description", ""))
        if len(description) > MAX_TOOL_DESCRIPTION:
            problems.append(
                f"{prefixed}: description truncated from {len(description):,} to "
                f"{MAX_TOOL_DESCRIPTION:,} characters"
            )
            description = description[:MAX_TOOL_DESCRIPTION] + " [truncated]"
        agent.tools.register(
            Tool(prefixed, f"[mcp:{client.name}] {description}", t["input_schema"],
                 make_handler(t["name"], client, limit),
                 # The hint is kept as advisory metadata, but risk is pinned:
                 # every MCP call crosses to a server outside this process, and
                 # a claim written by the untrusted side of a boundary must not
                 # lower what the boundary enforces.
                 readonly=bool(annotations.get("readOnlyHint")),
                 risk="external"),
            replace=True,
        )
        added.append(prefixed)
    agent.state.setdefault("mcp_clients", {})[client.name] = client
    return added


_CONNECT = {"type": "object", "properties": {"name": {"type": "string"}}, "required": ["name"]}


def install_mcp(registry: ToolRegistry, servers: dict) -> ToolRegistry:
    """Add `connect_mcp`. `servers` maps a name -> MCPClient (or a 0-arg factory)."""
    async def connect_mcp(ctx: ToolContext, name):
        connected = ctx.state.setdefault("mcp_server_names", {})
        if name in connected:
            return f"MCP server '{name}' already connected as '{connected[name]}'"
        spec = servers.get(name)
        if spec is None:
            return f"Error: unknown MCP server '{name}'. Available: {', '.join(servers) or '(none)'}"
        client = spec() if callable(spec) and not isinstance(spec, MCPClient) else spec
        # A server built without a registry gets one, so a server configured by
        # an operator who never thought about credentials still runs scrubbed.
        if getattr(client, "secrets", None) is None and hasattr(client, "secrets"):
            client.secrets = getattr(ctx.agent, "secrets", None) or client.secrets
        added = await register_mcp(ctx.agent, client)
        withheld = tuple(getattr(client, "withheld", ()))
        if withheld:
            ctx.agent.state.setdefault("mcp_problems", ProblemLog()).append(
                f"{client.name}: {len(withheld)} credential(s) withheld from the "
                f"server environment ({', '.join(withheld[:3])}); add them to "
                "env_passthrough if it needs them"
            )
        connected[name] = client.name
        return f"Connected '{name}'. Added tools: {', '.join(added) or '(none)'}"

    registry.register(Tool(
        "connect_mcp",
        f"Connect an MCP server and add its tools. Available: {', '.join(servers) or '(none)'}.",
        # Connecting is itself an external act: it reaches a server and pulls
        # a tool surface from it.
        _CONNECT, connect_mcp, risk="external"))
    return registry
