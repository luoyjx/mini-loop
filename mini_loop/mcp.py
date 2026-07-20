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
import re

from .registry import Tool, ToolContext, ToolRegistry


def normalize_name(name: str) -> str:
    return re.sub(r"[^a-zA-Z0-9_-]", "_", name)


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

    def __init__(self, name: str, command: list[str]) -> None:
        self.name = name
        self.command = command
        self._proc: asyncio.subprocess.Process | None = None
        self._id = 0
        self._lock = asyncio.Lock()
        self._start_lock = asyncio.Lock()

    async def _start(self) -> None:
        async with self._start_lock:
            if self._proc is not None:
                return
            self._proc = await asyncio.create_subprocess_exec(
                *self.command, stdin=asyncio.subprocess.PIPE, stdout=asyncio.subprocess.PIPE)
            try:
                await self._rpc("initialize", {
                    "protocolVersion": "2024-11-05",
                    "capabilities": {}, "clientInfo": {"name": "mini-loop", "version": "0.1.0"},
                })
                await self._notify("notifications/initialized", {})
            except BaseException:
                process, self._proc = self._proc, None
                if process is not None and process.returncode is None:
                    process.kill()
                    await process.wait()
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
                line = await self._proc.stdout.readline()
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
        result = await self._rpc("tools/list", {})
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
        return "\n".join(c.get("text", "") for c in content if isinstance(c, dict)) or json.dumps(result)

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


async def register_mcp(agent, client: MCPClient) -> list[str]:
    """Discover a client's tools and register them into the agent's registry."""
    server = normalize_name(client.name)
    added = []
    for t in await client.list_tools():
        prefixed = f"mcp__{server}__{normalize_name(t['name'])}"

        def make_handler(orig: str, c: MCPClient):
            async def handler(ctx: ToolContext, **kwargs):
                return await c.call_tool(orig, kwargs)
            return handler

        annotations = t.get("annotations") or {}
        agent.tools.register(
            Tool(prefixed, f"[mcp:{client.name}] {t['description']}", t["input_schema"],
                 make_handler(t["name"], client), readonly=bool(annotations.get("readOnlyHint"))),
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
        added = await register_mcp(ctx.agent, client)
        connected[name] = client.name
        return f"Connected '{name}'. Added tools: {', '.join(added) or '(none)'}"

    registry.register(Tool(
        "connect_mcp",
        f"Connect an MCP server and add its tools. Available: {', '.join(servers) or '(none)'}.",
        _CONNECT, connect_mcp))
    return registry
