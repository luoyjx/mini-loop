"""Actual memory-selection consumers under pinned ordinary caller stacks."""

import asyncio
import copy
import hashlib
import inspect
import json
import subprocess
import sys
import tempfile
from pathlib import Path
from types import SimpleNamespace


def memory_selection_profiles() -> dict:
    """Isolate the capture entry stack from the exporter's own caller frames."""
    captured = subprocess.run(
        [sys.executable, str(Path(__file__).resolve())],
        cwd=Path(__file__).resolve().parents[1], stdout=subprocess.PIPE, check=True,
    )
    return json.loads(captured.stdout)


def _capture_memory_selection_profiles() -> dict:
    from fastapi.testclient import TestClient
    from mini_loop.agent import Agent
    from mini_loop.auth import TokenAuth
    from mini_loop.caching import NullCachePolicy
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, FakeMessage, FakeUsage, text
    from mini_loop.manager import SessionManager
    from mini_loop.memory import (
        MemoryStore, ScopedMemory, install_memory, memory_store_for,
        prepare_memory_context, select_relevant_memories, _json_array,
    )
    from mini_loop.registry import ToolRegistry
    from mini_loop.server import create_app
    from mini_loop.session import AgentSession

    depths = [0, 960, *range(965, 992), 1005]
    cases = []

    def seed(raw, owner):
        raw.write("foreign", "project", "foreign description", "foreign secret", owner="foreign")
        store = ScopedMemory(raw, owner)
        for name, kind, origin in (("alpha", "project", "explicit"),
                                   ("beta", "feedback", "imported"),
                                   ("gamma", "reference", "auto_extracted")):
            store.write(name, kind, name + " facts", name + " body", origin=origin)
        return store

    def reply_for(shape, depth):
        nested = "[" * depth + "0" + "]" * depth if shape == "array" else '{"x":' * depth + "0" + "}" * depth
        return "[2," + nested + "]"

    async def direct_or_run(mode, reply, root):
        if mode == "managed":
            calls = []

            def respond(request):
                calls.append(request["max_tokens"])
                return [text(reply if request["max_tokens"] == 200 else "done")], "end_turn"

            cfg = Settings(fake_llm=True, trajectory_enabled=False, enable_features=False,
                           workspace_root=root / "workspaces", skills_dir=root / "skills",
                           user_resources_root=None, spill_dir=None)
            manager = SessionManager(cfg, FakeAsyncAnthropic(responder=respond, thinking=False))
            session = manager.create(permission_mode="readonly", owner="owner")
            session.agent.tools = install_memory(session.agent.tools)
            session.agent.state["memory_auto"] = True
            store = seed(manager.memory, "owner")
            final = await session.run("alpha")
            prepared = next(m["content"] for m in session.agent.messages if m["role"] == "user" and isinstance(m["content"], str))
            events = list(session._backlog)
            return dict(prepared=prepared, final=final, status=200, budgets=calls,
                        memory=[{k: e[k] for k in ("action", "count")} for e in events if e["type"] == "memory"],
                        records=store.list(), foreign=manager.memory.list(owner="foreign"))
        raw = MemoryStore(root / "memory")
        store = seed(raw, "owner")
        calls, events = [], []

        async def create(**kwargs):
            calls.append(copy.deepcopy(kwargs))
            return FakeMessage([text(reply if kwargs["max_tokens"] == 200 else "done")], "end_turn", FakeUsage(777, 3))

        async def emit(event):
            events.append(copy.deepcopy(event))

        agent = Agent(client=SimpleNamespace(messages=SimpleNamespace(create=create)),
                      workspace=root, tools=install_memory(ToolRegistry()), emit=emit,
                      cache_policy=NullCachePolicy(),
                      state=dict(memory=raw, resource_owner="owner", memory_auto=True, permission_mode="readonly"),
                      settings=Settings(fake_llm=True, spill_dir=None, skills_dir=root / "empty"))
        if mode == "direct":
            prepared = await prepare_memory_context(agent, "alpha")
            final = None
        else:
            final = await agent.run("alpha")
            prepared = next(m["content"] for m in agent.messages if m["role"] == "user" and isinstance(m["content"], str))
        return dict(prepared=prepared, final=final, status=200,
                    budgets=[c["max_tokens"] for c in calls],
                    memory=[{k: e[k] for k in ("action", "count")} for e in events if e["type"] == "memory"],
                    records=store.list(), foreign=raw.list(owner="foreign"))

    def http_turn(mode, reply, root):
        calls = []

        def respond(request):
            calls.append(request["max_tokens"])
            return [text(reply if request["max_tokens"] == 200 else "done")], "end_turn"

        cfg = Settings(fake_llm=True, trajectory_enabled=False, enable_features=False,
                       workspace_root=root / "workspaces", skills_dir=root / "skills",
                       user_resources_root=None, spill_dir=None)
        manager = SessionManager(cfg, FakeAsyncAnthropic(responder=respond, thinking=False))
        app = create_app(settings=cfg, manager=manager)
        headers = {"Authorization": "Bearer token"}
        with TestClient(app, raise_server_exceptions=False) as client:
            app.state.auth = TokenAuth({"token": "owner", "foreign-token": "foreign"})
            response = client.post("/sessions", json={"mode": "readonly"}, headers=headers)
            assert response.status_code == 200, response.text
            sid = response.json()["id"]
            session = manager.get(sid)
            agent = session.agent
            agent.tools = install_memory(agent.tools)
            agent.state["memory_auto"] = True
            store = seed(manager.memory, "owner")
            path = "/sessions/" + sid + "/messages" + ("/stream" if mode == "stream" else "")
            response = client.post(path, json={"message": "alpha"}, headers=headers)
            assert response.status_code == 200, response.text
            events = list(session._backlog)
            final = response.json()["final"] if mode == "post" else next(
                json.loads(line[6:])["text"] for line in response.text.splitlines()
                if line.startswith("data: ") and json.loads(line[6:]).get("type") == "done")
            prepared = next(m["content"] for m in agent.messages if m["role"] == "user" and isinstance(m["content"], str))
            foreign_status = client.get("/sessions/" + sid + "/memory", headers={"Authorization": "Bearer foreign-token"}).status_code
            assert memory_store_for(agent).owner == "owner"
            return dict(prepared=prepared, final=final, status=response.status_code, foreign_status=foreign_status,
                        budgets=calls,
                        memory=[{k: e[k] for k in ("action", "count")} for e in events if e["type"] == "memory"],
                        records=store.list(), foreign=manager.memory.list(owner="foreign"))

    with tempfile.TemporaryDirectory(prefix="mini-loop-selection-profile-") as scratch:
        for mode in ("direct", "run", "managed", "post", "stream"):
            for shape in ("array", "object"):
                for depth in depths:
                    name = f"{mode}-{shape}-{depth}"
                    root = Path(scratch) / name
                    root.mkdir()
                    reply = reply_for(shape, depth)
                    result = asyncio.run(direct_or_run(mode, reply, root)) if mode in ("direct", "run", "managed") else http_turn(mode, reply, root)
                    cases.append(dict(name=name, mode=mode, shape=shape, depth=depth,
                                      reply_sha256=hashlib.sha256(reply.encode()).hexdigest(), **result))
    functions = {"Agent.run": Agent.run, "Agent._run_one_turn": Agent._run_one_turn,
                 "AgentSession.run": AgentSession.run,
                 "prepare_memory_context": prepare_memory_context, "select_relevant_memories": select_relevant_memories,
                 "_json_array": _json_array, "capture.memory_selection_profiles": _capture_memory_selection_profiles}
    return dict(cases=cases, recursion_limit=sys.getrecursionlimit(), integer_digits=sys.get_int_max_str_digits(),
                source_sha256={name: hashlib.sha256(inspect.getsource(fn).encode()).hexdigest() for name, fn in functions.items()},
                limits=["Pinned ordinary Source caller stacks with readonly execution, not custom interpreter/stack settings.",
                        "A standalone producer process fixes capture entry frames independently of the exporter invocation.",
                        "No observed input can choose a caller profile or grant execution authority."])


if __name__ == "__main__":
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
    print(json.dumps(_capture_memory_selection_profiles()))
