"""Actual public prompt helpers; no model, transport, or shell calls."""

from __future__ import annotations

import copy
import hashlib
import json
import os
from pathlib import Path
import platform
import tempfile
from unittest.mock import patch


def prompt_composition_contracts() -> dict:
    from mini_loop import (
        Agent, MemoryStore, Settings, ToolRegistry, install_memory,
        memory_system_builder, runtime_facts, sections_builder,
    )
    from mini_loop.compaction import context_used
    from mini_loop.fake_llm import FakeAsyncAnthropic, FakeUsage
    from mini_loop.memory import ScopedMemory

    def static(text):
        return dict(kind="static", text=text, label="", error=False)

    def callback(label, text="", *, kind="callback", error=False):
        return dict(kind=kind, text=text, label=label, error=error)

    section_recipes = [
        dict(name="empty", sections=[]),
        dict(name="empty-static", sections=[static(""), static("")]),
        dict(name="whitespace", sections=[static(" "), static("\n"), static("end\n")]),
        dict(name="static-order", sections=[static("first"), static("中\nsecond")]),
        dict(name="mixed-empty", sections=[static("base"), callback("a", ""), callback("b", "dynamic"), static("")]),
        dict(name="callback-none", sections=[callback("none", kind="none"), static("kept")]),
        dict(name="all-callback-empty", sections=[callback("a"), callback("b", kind="none")]),
        dict(name="context", sections=[callback("where", kind="workspace"), callback("tools", kind="tools")]),
        dict(name="failure-first", sections=[callback("failed", error=True), callback("never", "late")]),
        dict(name="failure-middle", sections=[callback("first", "early"), callback("failed", error=True), callback("never", "late")]),
        dict(name="repeated-build", sections=[callback("one", "same"), callback("two", "same")], builds=2),
    ]
    todos = [dict(content="Plan 中文", status="completed", activeForm="Planning"),
             dict(content="Implement", status="in_progress", activeForm="Implementing")]
    own = dict(name="alpha", type="project", description="Owner facts", body="private body", owner="alice")
    foreign = dict(name="foreign", type="feedback", description="Foreign facts", body="foreign body", owner="bob")
    anonymous = dict(name="anonymous", type="reference", description="Anonymous facts", body="anonymous body", owner="anonymous")
    facts_recipes = [
        dict(name="empty"), dict(name="todos", todos=todos),
        dict(name="pressure-below", used=74), dict(name="pressure-75", used=75),
        dict(name="pressure-89", used=89), dict(name="pressure-90", used=90),
        dict(name="pressure-over", used=125),
        dict(name="memory-empty", memory=True, recall=True),
        dict(name="memory-owned", memory=True, recall=True, seeds=[own, foreign]),
        dict(name="memory-no-recall", memory=True, seeds=[own, foreign]),
        dict(name="memory-foreign-only", memory=True, recall=True, seeds=[foreign]),
        dict(name="memory-anonymous", memory=True, recall=True, owner="anonymous", seeds=[anonymous, foreign]),
        dict(name="combined", todos=todos, used=90, memory=True, recall=True, seeds=[own, foreign]),
    ]
    memory_recipes = [
        dict(name="empty-base-empty-store", sections=[]),
        dict(name="base-empty-store", sections=[static("base")]),
        dict(name="owner-scope", sections=[static("base")], seeds=[own, foreign]),
        dict(name="empty-base-owned", sections=[], seeds=[own, foreign]),
        dict(name="composed-base", sections=[static("one"), callback("base", "two")], seeds=[own]),
        dict(name="base-fault-before-index", sections=[callback("failed", error=True)], seeds=[own, foreign]),
        dict(name="live-index", sections=[callback("base", "base")], seeds=[own, foreign],
             second_write=dict(name="beta", type="reference", description="Added later", body="second body", owner="alice")),
    ]

    def agent_for(root, *, threshold=100, recall=False, state=None):
        client = FakeAsyncAnthropic(thinking=False)
        tools = install_memory(ToolRegistry()) if recall else ToolRegistry()
        return Agent(client=client, workspace=root / "workspace", tools=tools,
                     state=state or {}, settings=Settings(fake_llm=True, spill_dir=None,
                         workspace_root=root / "workspace", skills_dir=root / "skills",
                         token_threshold=threshold))

    def build_sections(specs, trace):
        sections = []
        for spec in specs:
            if spec["kind"] == "static":
                sections.append(spec["text"])
                continue
            def part(agent, spec=spec):
                trace.append(spec["label"])
                if spec["error"]:
                    raise ValueError("section fault")
                if spec["kind"] == "none":
                    return None
                if spec["kind"] == "workspace":
                    return "Workspace: " + str(agent.workspace)
                if spec["kind"] == "tools":
                    return "Tools: " + ", ".join(agent.tools.names())
                return spec["text"]
            sections.append(part)
        return sections_builder(*sections)

    def invoke(build, agent):
        try:
            value = build(agent)
            return dict(output=value.replace(str(agent.workspace), "<workspace>"), error=None)
        except Exception as error:
            return dict(output="", error=dict(kind=type(error).__name__, message=str(error)))

    def seed_store(root, seeds):
        store = MemoryStore(root / "memory")
        for seed in seeds:
            store.write(seed["name"], seed["type"], seed["description"], seed["body"], owner=seed["owner"])
        return store

    def capture(root):
        section_rows, facts_rows, memory_rows = [], [], []
        for recipe in section_recipes:
            agent = agent_for(root / ("sections-" + recipe["name"]))
            before = copy.deepcopy(agent.messages)
            trace = []
            build = build_sections(recipe["sections"], trace)
            results = [invoke(build, agent) for _ in range(recipe.get("builds", 1))]
            section_rows.append(dict(recipe=recipe, results=results, trace=trace,
                                     messages_unchanged=agent.messages == before))
        for partial in facts_recipes:
            recipe = dict(todos=[], used=0, threshold=100, memory=False, recall=False,
                          owner="alice", seeds=[]) | partial
            area = root / ("facts-" + recipe["name"])
            state = dict(resource_owner=recipe["owner"])
            store = seed_store(area, recipe["seeds"]) if recipe["memory"] else None
            if store is not None:
                state["memory"] = store
            agent = agent_for(area, threshold=recipe["threshold"], recall=recipe["recall"], state=state)
            agent.todo.update(recipe["todos"])
            agent.token_meter.observe(FakeUsage(recipe["used"]), agent.messages,
                                      envelope=agent._request_envelope())
            observed_used = context_used(agent)
            before_messages = copy.deepcopy(agent.messages)
            before_meter = agent.token_meter.snapshot()
            before_system = agent.system_builder(agent)
            before_records = copy.deepcopy(store.list()) if store is not None else []
            output = runtime_facts(agent)
            facts_rows.append(dict(recipe=recipe, used_tokens=observed_used,
                output=output, todos=copy.deepcopy(agent.todo.items),
                index_exists=store.index_path.exists() if store is not None else False,
                records_unchanged=store is None or store.list() == before_records,
                messages_unchanged=agent.messages == before_messages,
                meter_unchanged=agent.token_meter.snapshot() == before_meter,
                default_system_unchanged=agent.system_builder(agent) == before_system))
        for partial in memory_recipes:
            recipe = dict(seeds=[], owner="alice", second_write=None) | partial
            area = root / ("memory-" + recipe["name"])
            store = seed_store(area, recipe["seeds"])
            agent = agent_for(area)
            trace = []
            build = memory_system_builder(build_sections(recipe["sections"], trace), ScopedMemory(store, recipe["owner"]))
            before_messages = copy.deepcopy(agent.messages)
            before_records = copy.deepcopy(store.list())
            results = [invoke(build, agent)]
            first_index_exists = store.index_path.exists()
            first_records_unchanged = store.list() == before_records
            if recipe["second_write"] is not None:
                seed = recipe["second_write"]
                store.write(seed["name"], seed["type"], seed["description"], seed["body"], owner=seed["owner"])
                results.append(invoke(build, agent))
            memory_rows.append(dict(recipe=recipe, results=results, trace=trace,
                first_index_exists=first_index_exists, first_records_unchanged=first_records_unchanged,
                messages_unchanged=agent.messages == before_messages))
        return dict(sections=section_rows, runtime_facts=facts_rows, memory_system=memory_rows)

    with tempfile.TemporaryDirectory(prefix="prompt-composition-") as directory:
        with patch.dict(os.environ, {}, clear=True):
            result = capture(Path(directory))
    package = Path(__file__).resolve().parents[1] / "mini_loop"
    result.update(python_version=platform.python_version(),
        scope="Actual root public helpers with real Agent/TodoManager/owner-scoped MemoryStore; static strings and string/None/error callbacks only. No arbitrary Python str(object) contract. Runtime used_tokens is actual context_used observation; token_threshold stays within the public frozen Settings constructor domain (at least 1). No provider, shell, default system mutation or new runtime wiring.",
        source_sha256={name: hashlib.sha256((package / name).read_bytes()).hexdigest()
                       for name in ("prompts.py", "memory.py", "agent.py", "metering.py", "compaction.py", "config.py")},
        producer_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest())
    # The fixture boundary itself must remain finite JSON.
    json.dumps(result, allow_nan=False)
    return result
