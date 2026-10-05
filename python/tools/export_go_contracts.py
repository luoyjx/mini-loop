"""Freeze Python's current public contracts for the independent Go port.

From the repository root, run ``.venv/bin/python python/tools/export_go_contracts.py``
or add ``--check`` to reject drift without writing. The export uses the real
default registry, FastAPI OpenAPI builder, and SQLite schema constant. It does
not open a database, call a model, or include credentials.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import sys
import tempfile
from pathlib import Path


PYTHON_ROOT = Path(__file__).resolve().parent.parent
REPO_ROOT = PYTHON_ROOT.parent
TARGET = REPO_ROOT / "go" / "testdata"
sys.path.insert(0, str(PYTHON_ROOT))


def _json_bytes(value: object) -> bytes:
    return (json.dumps(value, ensure_ascii=False, sort_keys=True, indent=2) + "\n").encode()


def _file_contracts(scratch: Path) -> dict[str, object]:
    """Exercise real Python file effects; recipes keep large inputs compact."""
    from mini_loop.tools import Toolset, READ_CHAR_CAP, OUTPUT_CAP

    cases = []

    def add(name, tool, inputs, *, unit="", repeat=1, suffix="", hex_bytes="",
            initial=True, directories=(), initial_path=None):
        root = scratch / name
        toolset = Toolset(root)
        files = []
        if initial:
            path = str(initial_path if initial_path is not None else inputs["path"])
            recipe = {"unit": unit, "repeat": repeat, "suffix": suffix}
            payload = bytes.fromhex(hex_bytes) if hex_bytes else (
                unit * repeat + suffix
            ).encode("utf-8")
            target = root / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(payload)
            files.append({"path": path, "text": recipe, "hex": hex_bytes})
        for directory in directories:
            (root / directory).mkdir(parents=True, exist_ok=True)
        if tool == "read_file":
            output = toolset.run_read(**inputs)
        elif tool == "write_file":
            output = toolset.run_write(**inputs)
        else:
            output = toolset.run_edit(**inputs)
        expected_files = []
        for target in sorted(root.rglob("*")):
            if target.is_file():
                payload = target.read_bytes()
                expected_files.append({
                    "path": target.relative_to(root).as_posix(),
                    "bytes": len(payload),
                    "sha256": hashlib.sha256(payload).hexdigest(),
                })
        cases.append({
            "name": name, "files": files, "directories": list(directories),
            "call": {"type": "tool_use", "id": f"file_{len(cases) + 1}",
                     "name": tool, "input": inputs},
            "expected_output": output.replace(str(root.resolve()), "$WORKSPACE"),
            "expected_error": output.startswith("Error:"),
            "expected_files": expected_files,
        })

    add("read_unicode_page", "read_file",
        {"path": "note.txt", "offset": 1, "limit": 1},
        unit="标题\r\n第二行\n끝\n")
    add("read_separators", "read_file", {"path": "note.txt"},
        unit="A\x85B\u2028C\x1cD\rE\r\nF\n")
    add("read_blank_lines", "read_file", {"path": "note.txt"}, unit="a\n\n")
    add("read_negative_options", "read_file",
        {"path": "note.txt", "offset": -1, "limit": -1}, unit="a\nb\n")
    add("read_past_eof", "read_file", {"path": "note.txt", "offset": 5},
        unit="one\ntwo")
    add("read_empty_past_eof", "read_file", {"path": "note.txt", "offset": 3})
    add("read_deep_page", "read_file", {"path": "note.txt", "offset": 1},
        unit="x", repeat=READ_CHAR_CAP + 20, suffix="\ntarget\n")
    add("read_truncated_unicode", "read_file", {"path": "note.txt"},
        unit="中", repeat=READ_CHAR_CAP + 10)
    add("read_truncated_limit", "read_file", {"path": "note.txt", "limit": 2},
        unit="x\n", repeat=READ_CHAR_CAP)
    add("read_exact_cap", "read_file", {"path": "note.txt"},
        unit="x", repeat=READ_CHAR_CAP)
    add("read_invalid_utf8", "read_file", {"path": "note.txt"},
        hex_bytes="6162ff0a63")
    add("read_truncated_utf8", "read_file", {"path": "note.txt"},
        hex_bytes="61e282")
    add("read_malformed_utf8", "read_file", {"path": "note.txt"},
        hex_bytes="e08080eda080f4908080c080f08080e28241f09f")
    add("read_null_path", "read_file", {"path": "\x00"}, initial=False)
    add("read_missing", "read_file", {"path": "missing.txt"}, initial=False)
    add("read_not_directory", "read_file", {"path": "parent/child.txt"},
        initial_path="parent", unit="original")
    add("read_directory", "read_file", {"path": "folder"},
        initial=False, directories=("folder",))
    add("read_outside", "read_file", {"path": "../escape.txt"}, initial=False)
    add("write_nested_unicode", "write_file",
        {"path": "nested/note.txt", "content": "你好🌱\n"}, initial=False)
    add("write_replace", "write_file",
        {"path": "note.txt", "content": "new\n"}, unit="old\n")
    add("write_empty", "write_file",
        {"path": "note.txt", "content": ""}, unit="old")
    add("write_not_directory", "write_file",
        {"path": "parent/child.txt", "content": "bad"},
        initial_path="parent", unit="original")
    add("write_outside", "write_file",
        {"path": "../escape.txt", "content": "bad"}, initial=False)
    add("edit_unique", "edit_file",
        {"path": "note.txt", "old_text": "第二行", "new_text": "新内容"},
        unit="标题\r\n第二行\r\n")
    add("edit_ambiguous", "edit_file",
        {"path": "note.txt", "old_text": "same", "new_text": "new"},
        unit="same\nsame\n")
    add("edit_missing_anchor", "edit_file",
        {"path": "note.txt", "old_text": "missing", "new_text": "new"}, unit="old")
    add("edit_empty_anchor", "edit_file",
        {"path": "note.txt", "old_text": "", "new_text": "new"}, unit="你好")
    add("edit_empty_file", "edit_file",
        {"path": "note.txt", "old_text": "", "new_text": "new"})
    add("edit_too_large", "edit_file",
        {"path": "note.txt", "old_text": "x", "new_text": "new"},
        unit="x", repeat=READ_CHAR_CAP + 1)
    add("edit_exact_cap", "edit_file",
        {"path": "note.txt", "old_text": "z", "new_text": "!"},
        unit="x", repeat=READ_CHAR_CAP - 1, suffix="z")
    add("edit_invalid_utf8", "edit_file",
        {"path": "note.txt", "old_text": "a", "new_text": "new"},
        hex_bytes="61ff")
    add("edit_truncated_utf8", "edit_file",
        {"path": "note.txt", "old_text": "a", "new_text": "new"},
        hex_bytes="61e282")
    add("edit_missing_file", "edit_file",
        {"path": "missing.txt", "old_text": "a", "new_text": "new"},
        initial=False)
    add("edit_not_directory", "edit_file",
        {"path": "parent/child.txt", "old_text": "a", "new_text": "bad"},
        initial_path="parent", unit="original")
    return {"read_char_cap": READ_CHAR_CAP, "output_cap": OUTPUT_CAP, "cases": cases}


def _glob_contracts(scratch: Path) -> dict[str, object]:
    """Capture Python glob enumeration and fnmatch component semantics."""
    import fnmatch
    import itertools
    from mini_loop.tools import Toolset, OUTPUT_CAP

    base_files = [
        "alpha.txt", "beta.py", ".hidden.txt", "literal[.txt", "bracket].txt",
        "slash\\name.txt", "中.txt", "a-b.txt", "q!.txt", "line\nbreak.txt",
        "file", "dir/nested.txt", "dir/deep/data.py", "dir/.secret.txt",
        "other/note.txt", ".hidden_dir/inside.txt", "brackets[dir]/a.txt",
    ]
    base_directories = ["empty", "dir", "dir/deep", ".hidden_dir", "other",
                        "brackets[dir]"]
    base_links = [
        {"path": "alias", "target": "dir"},
        {"path": "dangling", "target": "missing"},
        {"path": "out", "target": "$OUTSIDE"},
    ]
    cases = []

    def add(name, pattern, *, files=None, directories=None, links=None, series=None):
        root = scratch / name / "workspace"
        outside = scratch / name / "outside"
        toolset = Toolset(root)
        root = toolset.workspace
        outside.mkdir(parents=True)
        outside = outside.resolve()
        (outside / "outside.txt").write_text("outside")
        selected_files = base_files if files is None else files
        selected_directories = base_directories if directories is None else directories
        selected_links = base_links if links is None else links
        for directory in selected_directories:
            (root / directory).mkdir(parents=True, exist_ok=True)
        expanded_files = list(selected_files)
        if series is not None:
            expanded_files.extend(
                series["directory"] + series["prefix"] + f"_{i:04d}.txt"
                for i in range(series["count"])
            )
        for filename in expanded_files:
            target = root / filename
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text("")
        for link in selected_links:
            target = link["target"].replace("$OUTSIDE", str(outside))
            (root / link["path"]).symlink_to(target)
        actual_pattern = pattern.replace("$WORKSPACE", str(root)).replace(
            "$OUTSIDE", str(outside)
        )
        output = toolset.run_glob(actual_pattern).replace(
            str(root), "$WORKSPACE"
        ).replace(str(outside), "$OUTSIDE")
        cases.append({
            "name": name, "pattern": pattern, "files": selected_files,
            "directories": selected_directories, "links": selected_links,
            "series": series, "expected_output": output,
            "expected_error": output.startswith("Error:"),
        })

    patterns = [
        "*", "*.txt", ".*", "**", "**/", "**/*.txt", "**/**/data.py",
        "dir/**", "dir/**/", "file/**", "missing/**", "*/", "*/nested.txt",
        "dir/?ested.[t][x][t]", "[ab]*.txt", "[!a]*.txt", "literal[[]*.txt",
        "bracket[]].txt", "slash\\*.txt", "中.?xt", "nonexistent*", "",
        "./**/*.py", "dir//*.txt", "dir/../*.txt", "$WORKSPACE/*.txt",
        "$OUTSIDE/*.txt", "out/*", "alias/**/*.txt", "dangling",
        "[.]hidden.txt", "empty/", "brackets[[]dir]/*.txt", ".", "[z-a]*",
    ]
    for i, pattern in enumerate(patterns):
        add(f"glob_{i:02d}", pattern)
    add("glob_loop", "loop", files=[], directories=[],
        links=[{"path": "loop", "target": "loop"}])
    add("glob_truncated", "*.txt", files=[], directories=[], links=[],
        series={"directory": "", "prefix": "中" * 80, "count": 800})
    add("glob_duplicate_budget", "**/**", files=[], directories=["dir/deep"],
        links=[], series={"directory": "dir/deep/", "prefix": "中" * 80,
                          "count": 200})

    names = ["", "abc", "aa", "ab", "a-b", "a\n", "文", "界"]
    names.extend(list("abcaz-![]^\\&|~中é😀\n"))
    names = sorted(set(names))
    components = {
        "", "*", "?", "**", "***", "a*", "*a", "a?", "?a", "[", "[!",
        "[]", "[]]", "[!]", "[!]]", "[a-z]", "[!a-z]", "[z-a]", "[!z-a]",
        "[a-b-c]", "[a--b]", "[--a]", "[a-b-c-d]", "[b-a-d-c]", "[a-]",
        "[-a]", "[!a-]", "[!-a]", "[^a]", "[[a]", "[&|~]", "[\\]",
        "中*", "[中-文]", "*[ab]*a", "*?*?*", "a**b*c",
    }
    for length in range(4):
        for body in itertools.product("!-az]", repeat=length):
            components.add("[" + "".join(body) + "]")
    matching = [
        {"pattern": pattern, "matches": fnmatch.filter(names, pattern)}
        for pattern in sorted(components)
    ]
    return {"output_cap": OUTPUT_CAP, "cases": cases, "names": names,
            "component_patterns": matching}


def _runtime_contracts() -> dict[str, object]:
    """Capture real todo updates and textual question handler outputs."""
    import asyncio
    from types import SimpleNamespace
    from mini_loop.agent import TodoManager, MAX_TODO_FIELD
    from mini_loop.builtins import _ask_user

    manager = TodoManager()
    todos = []
    cases = [
        [],
        [{"content": "  阅读资料\x1c", "status": "in_progress", "activeForm": " 正在阅读 "},
         {"content": "write", "status": "pending", "activeForm": "writing"}],
        [{"content": "read", "status": "completed", "activeForm": "reading"}],
        [{"content": "", "status": "pending", "activeForm": "a"}],
        [{"content": "a", "status": "pending", "activeForm": "\x1f\u3000"}],
        [{"content": "a", "status": "in_progress", "activeForm": "a"},
         {"content": "b", "status": "in_progress", "activeForm": "b"}],
        [{"content": f"task-{i}", "status": "pending", "activeForm": "working"} for i in range(21)],
        [{"content": "中" * (MAX_TODO_FIELD + 1), "status": "in_progress",
          "activeForm": "🌱" * (MAX_TODO_FIELD + 1)}],
        [{"content": "a" * MAX_TODO_FIELD, "status": "completed", "activeForm": "b"}],
        [],
    ]
    for items in cases:
        try:
            output, failed = manager.update(items), False
        except ValueError as exc:
            output, failed = f"Error: {exc}", True
        todos.append({"items": items, "output": output, "failed": failed,
                      "snapshot": manager.snapshot(), "has_open": manager.has_open_items()})

    questions = []
    class Broker:
        def __init__(self, answer):
            self.answer = answer
        async def ask_question(self, ctx, question):
            return self.answer
    for available, answer in [(False, None), (True, None), (True, ""), (True, "用蓝色，保留原文")]:
        state = {"manager": SimpleNamespace(approvals=Broker(answer))} if available else {}
        output = asyncio.run(_ask_user(SimpleNamespace(state=state), "哪个颜色？"))
        questions.append({"available": available, "answer": answer, "output": output})
    return {"todo_field_cap": MAX_TODO_FIELD, "todos": todos, "questions": questions}


def _skill_contracts(scratch: Path) -> dict[str, object]:
    """Capture the actual deployment skill catalogue, load and digest rules."""
    from mini_loop.skills import SkillLoader, MAX_SKILL_BODY, MAX_SKILL_DESCRIPTION, MAX_SKILL_CATALOGUE

    cases = []
    def file(path, text="", *, prefix="", repeat=1, suffix="", hex_bytes="", target=""):
        return {"path": path, "prefix": prefix, "unit": text, "repeat": repeat,
                "suffix": suffix, "hex": hex_bytes, "target": target}
    def add(name, files, loads, *, mutations=(), missing=False):
        root = scratch / name / "skills"
        outside = scratch / name / "outside"
        outside.mkdir(parents=True)
        (outside / "SKILL.md").write_text("outside")
        if not missing:
            root.mkdir(parents=True)
        def write(recipe):
            target = root / recipe["path"]
            target.parent.mkdir(parents=True, exist_ok=True)
            if recipe["target"]:
                if recipe["target"] == "$DIRECTORY":
                    target.mkdir()
                else:
                    target.symlink_to(recipe["target"].replace("$OUTSIDE", str(outside)))
            else:
                payload = bytes.fromhex(recipe["hex"]) if recipe["hex"] else (
                    recipe["prefix"] + recipe["unit"] * recipe["repeat"] + recipe["suffix"]
                ).encode("utf-8")
                target.write_bytes(payload)
        for recipe in files:
            write(recipe)
        loader = SkillLoader(root)
        descriptions = loader.descriptions()
        entries = [{"name": name, "description": skill["meta"].get("description", "-"),
                    "digest": skill["digest"], "source_digest": skill["source_digest"]}
                   for name, skill in loader.skills.items()]
        for mutation in mutations:
            if mutation.get("remove"):
                (root / mutation["path"]).unlink()
            else:
                write(mutation)
        results = []
        for request in loads:
            output = loader.load(**request)
            results.append({"input": request, "sha256": hashlib.sha256(output.encode()).hexdigest(),
                            "failed": output.startswith("Error:")})
        normalize = lambda value: value.replace(str(root), "$SKILLS").replace(str(outside), "$OUTSIDE")
        cases.append({"name": name, "files": files, "missing": missing, "mutations": list(mutations),
                      "descriptions": descriptions, "entries": entries, "loads": results,
                      "problems": [{"message": normalize(str(problem)), "count": loader.problems.counts[str(problem)]}
                                   for problem in loader.problems]})

    basic = [file("a/SKILL.md", "---\nname: read\ndescription: Read the source\n---\n\n阅读资料\n"),
             file("b/SKILL.md", "---\nname: read\n---\nshadow"),
             file("fallback/SKILL.md", " raw body \n"),
             file("empty/SKILL.md", "---\nname: empty\ndescription:\n---\nbody")]
    add("basic", basic, [{"name": name} for name in ["read", "fallback", "empty", "missing", "", "bad/name", "agent:read", "user:read"]]
        + [{"name": "read", "scope": "agent"}, {"name": "read", "scope": "user"},
           {"name": "agent:read", "scope": "user"}, {"name": "read", "scope": "invalid"}])
    for name, text in [
        ("newlines", "---\r\nname: note\rdescription: 中文\r\n---\r\n\tBody\r\nLine\r\x1c"),
        ("multiline", "---\nname: note\ndescription: one\n  two\nname: final\n---\nbody"),
        ("metadata_separators", "---\nname: note\x85description: first\u2028description: final\n---\nbody"),
        ("malformed", "---\nname: note\n---"),
        ("immediate", "---\n---\nbody"),
        ("empty_frontmatter", "---\n\n---\n  body  \n"),
        ("empty_body", "---\nname: note\n---\n\x1c\u3000\n"),
        ("invalid_name", "---\nname: x\"></skill>\n---\nbody"),
        ("exact_body", "---\nname: note\n---\n" + "中" * MAX_SKILL_BODY),
    ]:
        add(name, [file("note/SKILL.md", text)], [{"name": "note"}, {"name": "final"}])
    add("oversized", [file("note/SKILL.md", "中", prefix="---\nname: note\ndescription: " + "🌱" * 201 + "\n---\n", repeat=MAX_SKILL_BODY+100, suffix="\n")], [{"name": "note"}])
    add("unreadable", [file("broken/SKILL.md", hex_bytes="ff"), file("folder/SKILL.md", target="$DIRECTORY"), *basic[:1]], [{"name": "read"}])
    add("links", [file("escape/SKILL.md", target="$OUTSIDE/SKILL.md"),
                  file("loop", target="."), *basic[:1]], [{"name": "read"}])
    add("missing", [], [{"name": "unknown"}], missing=True)
    source = file("note/SKILL.md", "---\nname: note\ndescription: Note\n---\nbody\n")
    for name, mutation in [
        ("changed", file("note/SKILL.md", "new body")),
        ("removed", {"path": "note/SKILL.md", "remove": True}),
        ("identical", source),
        ("equivalent_newlines", file("note/SKILL.md", "---\r\nname: note\r\ndescription: Note\r\n---\r\nbody\r\n")),
    ]:
        add(name, [source], [{"name": "note"}, {"name": "note"}], mutations=[mutation])
    add("catalogue_cap", [file(f"skill-{i:03d}/SKILL.md", "---\nname: skill-"+f"{i:03d}"+"\ndescription: " + "中"*200 + "\n---\nbody") for i in range(100)], [{"name": "skill-099"}, {"name": "unknown"}])
    return {"body_cap": MAX_SKILL_BODY, "description_cap": MAX_SKILL_DESCRIPTION,
            "catalogue_cap": MAX_SKILL_CATALOGUE, "cases": cases}


def _context_contracts(scratch: Path) -> dict[str, object]:
    """Exercise request fitting, text budgets, metering and compaction source."""
    import asyncio
    import copy
    from types import SimpleNamespace
    from unittest.mock import patch
    from mini_loop.builtins import default_registry
    from mini_loop.registry import Tool, ToolRegistry
    from mini_loop.prompts import default_system_builder
    from mini_loop.compaction import (
        estimate_tokens, snip_compact, microcompact, tool_result_budget,
        DefaultCompactor,
    )
    from mini_loop.fake_llm import count_tokens
    from mini_loop.metering import TokenMeter

    defaults = default_registry()
    catalogs = []
    for names in [
        ["bash"],
        ["bash", "read_file", "write_file", "edit_file", "glob"],
        ["bash", "read_file", "write_file", "edit_file", "glob", "TodoWrite", "load_skill", "compress", "ask_user"],
        defaults.names(),
    ]:
        registry = ToolRegistry(defaults.get(name) for name in names)
        catalogs.append(registry)
    async def noop(*args, **kwargs):
        return ""
    for oversized_property in [False, True]:
        registry = ToolRegistry()
        for name in ["bash", "read_file", "write_file"]:
            schema = copy.deepcopy(defaults.get(name).input_schema)
            if oversized_property and name == "bash":
                schema["properties"]["command"]["description"] = "界" * 11_000
            registry.register(Tool(name, "界🌱" * 20_000, schema, noop))
        catalogs.append(registry)
    catalog_cases = []
    for registry in catalogs:
        snapshot = registry.snapshot()
        agent = SimpleNamespace(workspace=Path("/contract"), tools=registry,
                                _request_tool_catalog=snapshot, state={},
                                skills=SimpleNamespace(descriptions=lambda: "paint: 绘图"))
        catalog_cases.append({
            "input": [tool.schema for tool in registry._tools.values()],
            "schemas": snapshot.schemas(), "sent": snapshot.sent_names,
            "omitted": snapshot.omitted_names, "trimmed_to": snapshot.trimmed_to,
            "fingerprint": snapshot.fingerprint, "system": default_system_builder(agent),
        })

    wire_cases = []
    for text in ["ascii <>& / \\\"", "界🌱\u2028\u2029\x7f", "\b\f\n\r\t\x00\x1f"]:
        messages = [{"role": "user", "content": text}]
        tools = defaults.snapshot().schemas()
        wire_cases.append({"messages": messages, "system": text, "tools": tools,
                           "estimate": estimate_tokens(messages),
                           "fake_tokens": count_tokens({"messages": messages, "system": text, "tools": tools}),
                           "ascii_json": json.dumps(messages),
                           "unicode_json": json.dumps(messages, ensure_ascii=False)})

    def history(rounds, leading=1, pending=False):
        messages = [{"role": "user", "content": "start"} for _ in range(leading)]
        for index in range(rounds):
            messages.extend([
                {"role": "assistant", "content": [
                    {"type": "thinking", "thinking": "reason", "signature": "signed"},
                    {"type": "tool_use", "id": f"use-{index}", "name": "bash", "input": {"command": "echo"},
                     "caller": {"type": "direct"}},
                ]},
                {"role": "user", "content": [{"type": "tool_result", "tool_use_id": f"use-{index}",
                                              "content": "界🌱" * 600, "is_error": True}]},
            ])
        if not pending:
            messages.append({"role": "assistant", "content": [{"type": "text", "text": "done"}]})
        messages.append({"role": "user", "content": "<runtime-state>\nfacts\n</runtime-state>"})
        return messages
    cheap = []
    for leading, pending, maximum in [(1, False, 8), (2, True, 8), (1, True, 50), (1, False, 3)]:
        original = history(6, leading, pending)
        snipped = copy.deepcopy(original)
        removed = snip_compact(snipped, maximum)
        micro = copy.deepcopy(original)
        cleared = microcompact(micro)
        cheap.append({"messages": original, "max_messages": maximum, "snipped": snipped,
                      "removed": removed, "micro": micro, "cleared": cleared})

    meter = TokenMeter()
    meter_cases = []
    for length, actual, read, creation, envelope, probe_length, probe_envelope in [
        (10, 0, 0, 0, "a", 20, "a"),
        (100, 100, 20, 30, "a", 10, "a"),
        (200, 250, 0, 0, "a", 100, "a"),
        (300, 4000, 0, 0, "b", 20, "a"),
        (400, 10000, 0, 0, "b", 0, "b"),
        (800, 10001, 0, 0, "b", 400, "b"),
    ]:
        messages = [{"role": "user", "content": "界" * length}]
        probe = [{"role": "user", "content": "界" * probe_length}]
        usage = SimpleNamespace(input_tokens=actual, cache_read_input_tokens=read,
                                cache_creation_input_tokens=creation)
        meter.observe(usage, messages, envelope=envelope)
        meter_cases.append({"messages": messages, "usage": {**vars(usage), "output_tokens": 0},
                            "envelope": envelope, "probe": probe, "probe_envelope": probe_envelope,
                            "used": meter.used_for(probe, envelope=probe_envelope), "snapshot": meter.snapshot()})

    scratch.mkdir(parents=True, exist_ok=True)
    spill_messages = history(1, pending=True)
    with patch("mini_loop.compaction.time.time", return_value=1.234):
        persisted = tool_result_budget(spill_messages, scratch, max_bytes=1000, preview_chars=12)
    spill = {"messages": history(1, pending=True), "result": spill_messages, "persisted": persisted,
             "max_bytes": 1000, "preview_chars": 12,
             "files": [{"path": str(path.relative_to(scratch)), "content": path.read_text()}
                       for path in sorted((scratch / ".task_outputs").rglob("*.txt"))]}

    class SummaryAgent:
        def __init__(self):
            self.workspace, self.messages = scratch, history(2)
            self.events, self.requests = [], []
        async def _create(self, messages, **kwargs):
            self.requests.append({"messages": messages, **kwargs})
            return SimpleNamespace(content=[{"type": "text", "text": "handoff: 已完成"}],
                                   usage=SimpleNamespace(input_tokens=123, output_tokens=7), model="served-summary")
        async def _send(self, event, **kwargs):
            self.events.append({"event": event, **kwargs})
    summary = SummaryAgent()
    original = copy.deepcopy(summary.messages)
    with patch("mini_loop.compaction.time.time", return_value=1.234):
        asyncio.run(DefaultCompactor().compact(summary))
    summary_case = {"messages": original, "result": summary.messages, "requests": summary.requests,
                    "events": summary.events,
                    "archive": (scratch / ".transcripts/transcript_1234.jsonl").read_text()}
    # Portable artifact paths; preserve actual source-produced text otherwise.
    normalized = json.loads(json.dumps({"spill": spill, "summary": summary_case}, ensure_ascii=False).replace(str(scratch), "<workspace>"))
    return {"catalogs": catalog_cases, "wire": wire_cases, "cheap": cheap, "meter": meter_cases, **normalized}


def _subagent_contracts(scratch: Path) -> dict[str, object]:
    """Capture capability selection, derived authority and real child loops."""
    import asyncio
    import copy
    from mini_loop.agent import Agent
    from mini_loop.builtins import default_registry
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool, system_text
    from mini_loop.registry import Hook, Hooks, Tool
    from mini_loop.run_context import RunContext
    from mini_loop.stuck import NullStuckDetector
    from mini_loop.tool_policy import DEFAULT_ROLE_TOOL_POLICY

    async def noop(*args, **kwargs):
        return ""
    registry = default_registry()
    for name, capabilities in [
        ("semantic", {"repo.semantic_outline"}), ("symbol", {"repo.symbol"}),
        ("references", {"repo.references"}), ("recover", {"observation.recover"}),
        ("mixed", {"repo.read", "workspace.write"}), ("uncategorized", set()),
    ]:
        registry.register(Tool(name, name, {"type": "object", "properties": {}}, noop,
                               readonly=True, risk="read", capabilities=frozenset(capabilities)))
    role_cases = []
    for role in ["Explore", " worker ", "GENERAL-PURPOSE", "planner"]:
        try:
            names, failed = DEFAULT_ROLE_TOOL_POLICY.select(role, registry).names(), False
        except ValueError:
            names, failed = [], True
        role_cases.append({"role": role, "names": names, "failed": failed})
    definitions = [{"schema": definition.schema, "capabilities": sorted(definition.capabilities)}
                   for definition in registry._tools.values()]

    human = RunContext.explicit_human(actor_id="human-1", approved_capabilities=("workflow.launch", "a", "workflow.launch"))
    contexts = {
        "default": RunContext.default(), "human": human,
        "peer": human.derive_peer_agent(delegated_by="main"),
        "new_human": human.with_new_message(),
    }
    contexts["new_peer"] = contexts["peer"].with_new_message(approved_capabilities=("a",))
    identities = {value.message_id: name for name, value in contexts.items()}
    context_cases = []
    for name, value in contexts.items():
        snapshot = value.as_dict()
        snapshot["message_id"] = name
        if snapshot["parent_message_id"] is not None:
            snapshot["parent_message_id"] = identities[snapshot["parent_message_id"]]
        context_cases.append({"name": name, "snapshot": snapshot, "allows_a": value.allows("a")})

    children = []
    for index, (role, exhausted) in enumerate([("Explore", False), ("general-purpose", False), ("general-purpose", True)]):
        root = scratch / f"child-{index}"
        root.mkdir(parents=True)
        (root / "proof").write_text("proof content")
        child_requests, tool_contexts, events = [], [], []
        parent_context = RunContext.explicit_human(actor_id="human-1", approved_capabilities=("workflow.launch",))
        class Capture(Hook):
            async def before_tool(self, ctx, call):
                if ctx.agent.depth:
                    tool_contexts.append(ctx.run_context.as_dict())
        async def emit(event):
            events.append(event)
        def responder(kwargs):
            is_child = " subagent in " in system_text(kwargs)
            last = kwargs["messages"][-1]
            if is_child:
                child_requests.append({"system": system_text(kwargs),
                                       "names": [schema["name"] for schema in kwargs.get("tools", [])],
                                       "messages": copy.deepcopy(kwargs["messages"]),
                                       "model": kwargs["model"], "max_tokens": kwargs["max_tokens"]})
                if isinstance(last["content"], str):
                    action = tool("read_file", _id="child", path="proof") if role == "Explore" else tool("write_file", _id="child", path="made.txt", content="worker file")
                    return [text("child progress"), action], "tool_use"
                return [text("child done")], "end_turn"
            if isinstance(last["content"], str):
                return [tool("task", _id="parent", prompt="delegated prompt", agent_type=role)], "tool_use"
            return [text("parent done")], "end_turn"
        settings = Settings(fake_llm=True, workspace_root=root, skills_dir=root / "empty-skills",
                            model="model-contract", max_tokens=777, token_threshold=10_000_000,
                            subagent_max_rounds=1 if exhausted else 2)
        parent = Agent(client=FakeAsyncAnthropic(responder=responder, thinking=False),
                       settings=settings, workspace=root, label="main", hooks=Hooks([Capture()]),
                       stuck_detector=NullStuckDetector(), emit=emit)
        parent.messages.append({"role": "user", "content": "private parent history"})
        output = asyncio.run(parent.run("delegate", run_context=parent_context))
        result = next(part["content"] for message in parent.messages
                      if isinstance(message["content"], list) for part in message["content"]
                      if isinstance(part, dict) and part.get("type") == "tool_result" and part.get("tool_use_id") == "parent")
        child_context = tool_contexts[0]
        child_context["message_id"], child_context["parent_message_id"] = "child", "human"
        # Cache breakpoints belong to the provider adapter, which is a later
        # slice; assert fresh portable message content, not annotation fields.
        first = [{"role": message["role"], "content": message["content"]}
                 for message in child_requests[0]["messages"]]
        children.append({"role": role, "exhausted": exhausted, "output": output,
                         "summary": result, "system": child_requests[0]["system"],
                         "names": child_requests[0]["names"], "messages": first,
                         "model": child_requests[0]["model"], "max_tokens": child_requests[0]["max_tokens"],
                         "context": child_context,
                         "lineage": parent.subagents.last_lineage,
                         "made": (root / "made.txt").read_text() if (root / "made.txt").exists() else None})
        children[-1] = json.loads(json.dumps(children[-1], ensure_ascii=False).replace(str(root), "<workspace>"))
    return {"definitions": definitions, "roles": role_cases, "contexts": context_cases,
            "children": children, "default_max_depth": 2, "default_max_rounds": 30,
            "refusal": "(delegation refused: depth 3 exceeds subagent_max_depth=2; do the work directly)"}


def _action_contracts(scratch: Path) -> dict[str, object]:
    """Actual Python journal transitions, canonical identities and replay paths."""
    import asyncio
    import hashlib
    from dataclasses import asdict, replace
    from mini_loop.actions import (InMemoryActionJournal, DurableActionJournal,
                                   _bounded_result, _payload_hash, SHED_RESULT)
    from mini_loop.agent import Agent, _tool_action_id
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.registry import Tool, ToolCall, ToolRegistry, Hooks
    from mini_loop.run_context import RunContext
    from mini_loop.storage import SQLiteStateStore

    scratch.mkdir(parents=True)
    context = replace(RunContext.default(), message_id="m")
    payloads = [
        ("bash", {"command": "echo 汉字😀\n\u2028<>&"}),
        ("bash", {"command": "echo x", "approval_prefix": [], "run_in_background": False}),
        ("read_file", {"path": "proof", "limit": 0, "offset": 2}),
        ("write_file", {"path": "out", "content": "x\r\ny"}),
        ("edit_file", {"path": "out", "old_text": "x", "new_text": "y"}),
        ("glob", {"pattern": "**/*.go"}),
        ("TodoWrite", {"items": [{"content": "one", "status": "pending", "activeForm": "doing one"}]}),
        ("TodoWrite", {"items": []}), ("task", {"prompt": "check", "agent_type": "Explore"}),
        ("load_skill", {"name": "review", "scope": "agent"}), ("compress", {}),
        ("ask_user", {"question": "继续？"}),
    ]
    inputs = [{"block": {"type": "tool_use", "id": "u", "name": name, "input": value},
               "canonical": json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":")),
               "input_hash": _payload_hash(value),
               "action_id": _tool_action_id(session_id="s", run_context=context, call=ToolCall(name, value, "u"))}
              for name, value in payloads]
    def normalized(record):
        value = asdict(record)
        value["created_at"] = 0
        if value["completed_at"] is not None:
            value["completed_at"] = 0
        return value
    runs = []
    for backing in ("memory", "sqlite"):
        store = SQLiteStateStore(scratch / f"{backing}.db") if backing == "sqlite" else None
        journal = DurableActionJournal(store) if store else InMemoryActionJournal()
        begin = dict(action_id="a", session_id="s", message_id="m", tool_use_id="u",
                     tool_name="bash", input_value={"command": "echo x"})
        records = [normalized(journal.begin(**begin)),
                   normalized(journal.finish("a", status="completed", result="done")),
                   normalized(journal.finish("a", status="failed", result="overwrite")),
                   normalized(journal.begin(**begin)),
                   normalized(journal.attach_workflow("a", "workflow")),
                   normalized(journal.get("a"))]
        errors = []
        for operation in (lambda: journal.begin(**{**begin, "input_value": {"command": "other"}}),
                          lambda: journal.attach_workflow("a", "different"),
                          lambda: journal.finish("a", status="started")):
            try:
                operation()
                errors.append(False)
            except (ValueError, RuntimeError):
                errors.append(True)
        runs.append({"backing": backing, "records": records, "errors": errors})
        if store:
            store.close()
    bounds = [{"tool": tool_name, "result": _bounded_result("😀" * length, tool_name=tool_name)}
              for tool_name, length in [("bash", 4100), ("decision", 4100)]]
    replays = []
    for index, (status, verification) in enumerate(
            [(status, "absent") for status in ("completed", "failed", "denied", "cancelled")]
            + [("unknown", value) for value in ("absent", "yes", "no", "none", "invalid", "error")]):
        store = SQLiteStateStore(scratch / f"replay-{index}.db")
        journal = DurableActionJournal(store)
        calls, events = [], []
        async def handler(ctx, command):
            calls.append(command)
            return "effect"
        def verify(ctx, call):
            if verification == "error":
                raise RuntimeError("cannot tell")
            return {"yes": True, "no": False, "none": None, "invalid": "no"}.get(verification)
        registry = ToolRegistry()
        registry.register(Tool("bash", "test", {"type": "object", "properties": {"command": {"type": "string"}}}, handler,
                               risk="exec", verify=None if verification == "absent" else verify))
        call = ToolCall("bash", {"command": "echo x"}, "u")
        action_id = _tool_action_id(session_id="s", run_context=context, call=call)
        journal.begin(action_id=action_id, session_id="s", message_id="m", tool_use_id="u",
                      tool_name="bash", input_value=call.input)
        journal.finish(action_id, status=status, result=None if status == "unknown" else "recorded")
        async def emit(event):
            events.append(event)
        agent = Agent(client=FakeAsyncAnthropic(), settings=Settings(fake_llm=True),
                      workspace=scratch, tools=registry, hooks=Hooks(), emit=emit,
                      state={"session_id": "s", "action_journal": journal})
        output = asyncio.run(agent._exec_tool(call, run_context=context))
        result = next(event for event in reversed(events) if event["type"] == "tool_result")
        reconciliation = next((event for event in events if event["type"] == "reconcile"), None)
        replays.append({"status": status, "verification": verification, "output": output,
                        "calls": len(calls), "replayed": result.get("replayed", False),
                        "failed": result["error"],
                        "verdict": reconciliation["verdict"] if reconciliation else None,
                        "record": normalized(journal.get(action_id))})
        store.close()
    return {"inputs": inputs, "runs": runs, "bounds": bounds, "replays": replays, "shed_result": SHED_RESULT}


def _approval_contracts() -> dict[str, object]:
    """Default-tool grants and actual parked/reviewer broker outcomes."""
    import asyncio
    from types import SimpleNamespace
    from mini_loop.approvals import (ApprovalBroker, GRANT_BANNED_HEADS,
                                     grant_candidate, grant_banned, proposed_candidate)
    from mini_loop.registry import ToolCall
    from mini_loop.secrets import SecretRegistry

    grant_inputs = [
        {"command": "git status --short"}, {"command": "git"},
        {"command": "git\u001cstatus --short"},
        {"command": "git status --short", "approval_prefix": ["git", "status", "--short"]},
        {"command": "git status --short", "approval_prefix": ["git", "reset"]},
        {"command": "git status --short", "approval_prefix": ["git"]},
        {"command": "a b c d e f g", "approval_prefix": ["a", "b", "c", "d", "e", "f"]},
        {"command": "a b c d e f g", "approval_prefix": ["a", "b", "c", "d", "e", "f", "g"]},
    ] + [{"command": f"{head} one two", "approval_prefix": [head, "one"]} for head in GRANT_BANNED_HEADS]
    candidates = []
    for name, value in [("bash", value) for value in grant_inputs] + [("load_skill", {"name": "review"}), ("compress", {})]:
        default, proposed = grant_candidate(name, value), proposed_candidate(name, value)
        candidates.append({"block": {"type": "tool_use", "id": "u", "name": name, "input": value},
                           "default": default, "proposed": proposed,
                           "banned": grant_banned(default) if default else False})
    secret = 'clé-café-secret-Ω-"0123456789'
    async def one_case(name, *, kind="approval", command="git status --short", proposal=None,
                       action="allow", remember=False, answer=None, reviewer=None,
                       masking=False, broken_store=False, repeat=False):
        writes, events = [], []
        class Store:
            def write_approval(self, row):
                if broken_store:
                    raise RuntimeError("sensitive detail")
                writes.append(dict(row))
        secrets = SecretRegistry()
        if masking:
            secrets.register("KEY", secret)
        broker = ApprovalBroker(timeout=0.005, store=Store())
        broker.secrets = secrets
        review_calls = 0
        async def review(ctx, call, rule):
            nonlocal review_calls
            review_calls += 1
            if reviewer == "error":
                raise RuntimeError("sensitive detail")
            return {"allow": True, "deny": False, "abstain": None}[reviewer]
        if reviewer:
            broker.reviewer = review
        class Ctx:
            agent = SimpleNamespace(state={"session": SimpleNamespace(id="s")}, secrets=secrets)
            call = ToolCall("ask_user", {}, "u")
            async def emit_event(self, event_type, **fields):
                events.append({"type": event_type, **fields})
        ctx = Ctx()
        rule = SimpleNamespace(name="test-rule", message="approval needed")
        value = {"command": command}
        if proposal is not None:
            value["approval_prefix"] = proposal
        call = ToolCall("bash", value, "u")
        question = secret if masking else "which?"
        async def invoke():
            if kind == "question":
                return await broker.ask_question(ctx, question)
            return await broker.ask(ctx, call, rule)
        task = asyncio.create_task(invoke())
        for _ in range(100):
            await asyncio.sleep(0)
            if task.done() or broker.list("s"):
                break
        pending = broker.list("s")
        foreign, twice = None, None
        if pending and action != "timeout":
            approval_id = pending[0]["approval_id"]
            foreign = broker.resolve(approval_id, session_id="foreign", allowed=True)
            if action == "cancel":
                broker.cancel_session("s")
            else:
                broker.resolve(approval_id, session_id="s", allowed=action == "allow",
                               remember=remember, answer=answer)
            twice = broker.resolve(approval_id, session_id="s", allowed=True)
        result = await task
        second = await invoke() if repeat else None
        return {"name": name, "kind": kind, "block": {"type": "tool_use", "id": "u", "name": "bash", "input": value},
                "question": question, "action": action, "remember": remember,
                "answer": answer, "reviewer": reviewer, "masking": masking,
                "broken_store": broken_store, "repeat": repeat,
                "allowed": result if kind == "approval" else None,
                "response": result if kind == "question" else None,
                "second": second, "foreign": foreign, "twice": twice,
                "writes": writes, "events": events, "problems": len(broker.problems),
                "review_calls": review_calls, "remaining": broker.list("s")}
    async def cases():
        specs = [
            ("allowed", {}), ("denied", {"action": "deny"}),
            ("timeout", {"action": "timeout"}), ("cancelled", {"action": "cancel"}),
            ("question-empty", {"kind": "question", "answer": ""}),
            ("question-denied", {"kind": "question", "action": "deny", "answer": "ignored"}),
            ("question-missing", {"kind": "question"}),
            ("question-timeout", {"kind": "question", "action": "timeout"}),
            ("question-cancelled", {"kind": "question", "action": "cancel"}),
            ("masked-question", {"kind": "question", "masking": True, "answer": secret}),
            ("masked-preview", {"masking": True, "command": "echo " + secret}),
            ("long-preview", {"command": "echo " + "x" * 500}),
            ("grant-used", {"remember": True, "repeat": True}),
            ("proposal-used", {"remember": True, "repeat": True, "proposal": ["git", "status", "--short"]}),
            ("lying-proposal", {"remember": True, "repeat": True, "proposal": ["git", "reset"]}),
            ("banned-grant", {"remember": True, "command": "rm -rf build"}),
            ("auto-allow", {"reviewer": "allow"}), ("auto-deny", {"reviewer": "deny"}),
            ("auto-abstain", {"reviewer": "abstain", "action": "timeout"}),
            ("auto-error", {"reviewer": "error"}),
            ("store-fault", {"broken_store": True}),
        ]
        return [await one_case(name, **config) for name, config in specs]
    records = asyncio.run(cases())
    for record in records:
        ids = {}
        for row in record["writes"] + record["events"]:
            if "approval_id" in row:
                source_id = row["approval_id"]
                ids.setdefault(source_id, f"apr_{len(ids)}")
                row["approval_id"] = ids[source_id]
            if "created_at" in row:
                row["created_at"] = 0
            if row.get("resolved_at") is not None:
                row["resolved_at"] = 0
    return {"candidates": candidates, "cases": records, "secret": secret}



def _secret_contracts() -> dict[str, object]:
    """Real registry, typed input preview and Unicode name/casing contracts."""
    from mini_loop.secrets import SecretRegistry, DEFAULT_SECRET_PATTERNS
    from unittest.mock import patch
    secret = 'clé-secrète-"café"\\Ω-0123456789'
    masks = []
    specs = [
        ([('KEY', secret)], 'url=' + secret, {}),
        ([('KEY', 'TOPSECRET0123')], 'TOP\x1b[31mSECRET0123', {}),
        ([('KEY', 'TOPSECRET0123')], 'TOP\x1b]0;title\x07SECRET0123', {}),
        ([('KEY', 'TOPSECRET0123')], 'TOP\x1b]8;;url\x1b\\SECRET0123', {}),
        ([('KEY', 'TOPSECRET0123')], 'TOP\x1bMSECRET0123', {}),
        ([('SHORT', 'secret7')], 'secret7 stays', {}),
        ([('SHORT', 'Ω' * 7)], 'Ω' * 7, {}),
        ([('KEY', 'Ω' * 8)], 'Ω' * 8, {}),
        ([('A', '0123456789'), ('B', 'prefix0123456789suffix')], 'prefix0123456789suffix 0123456789', {}),
        ([('KEY', secret)], secret, {'mask_with': r'\replacement\$1'}),
        ([('KEY', 'a.b*?[Ω]')], 'a.b*?[Ω]aZbxxxx', {}),
        ([('KEY', 'line\nsecret-0123')], 'line\nsecret-0123', {}),
        ([('KEY', '😀' * 8)], '😀' * 8, {}),
        ([('SHORT', 'xy')], 'xy', {'min_length': 2}),
        ([('KEY', secret)], secret, {'mask_with': ''}),
    ]
    for values, text, settings in specs:
        registry = SecretRegistry(**settings)
        for name, value in values:
            registry.register(name, value)
        masked = registry.mask(text)
        masks.append({'values': [{'name': name, 'value': value} for name, value in values],
                      'text': text, 'settings': settings, 'masked': masked,
                      'names': registry.names(), 'short': registry.short_values(),
                      'unresolved': registry.unresolved()})
    env_cases = []
    for environment, patterns, extra, command in [
        ({'API_KEY': 'ordinary', 'X_API_KEY': secret, 'lower_token': 'token-0123', 'EMPTY_SECRET': '', 'CUSTOM': 'custom-0123', 'PATH': '/bin'}, None, ['CUSTOM'], 'echo $x_api_key $CuStOm'),
        ({'İ_KEY': secret, 'ΑΣ': 'sigma-0123', 'OTHER': 'other-0123'}, [], ['İ_KEY', 'ΑΣ', 'OTHER'], 'echo i\u0307_key ας'),
        ({'straße_token': secret, 'x_token': 'token-0123'}, ['STRASSE_TOKEN'], [], 'echo STRAßE_TOKEN'),
        ({'[X_TOKEN': secret, 'z_token': 'token-0123'}, ['[X_TOKEN', '[z-a]_TOKEN'], [], '[x_token'),
    ]:
        registry = SecretRegistry.from_environ(environ=environment, extra_names=extra, **({'patterns': patterns} if patterns is not None else {}))
        env_cases.append({'environment': environment, 'patterns': patterns, 'extra': extra, 'command': command,
                          'names': registry.names(), 'found': sorted(registry.find_in_text(command)),
                          'injected': registry.env_for_command(command), 'scrubbed': registry.scrub_env(environment)})
    registry = SecretRegistry()
    registry.register('KEY', secret)
    inputs = [
        ('bash', {'command': 'echo ' + secret, 'approval_prefix': ['echo', secret]}),
        ('read_file', {'path': secret, 'limit': 2, 'offset': 0}),
        ('write_file', {'path': secret, 'content': secret}),
        ('edit_file', {'path': secret, 'old_text': secret, 'new_text': secret}),
        ('glob', {'pattern': '*' + secret}),
        ('TodoWrite', {'items': [{'content': secret, 'status': 'pending', 'activeForm': secret}]}),
        ('task', {'prompt': secret, 'agent_type': 'Explore'}),
        ('load_skill', {'name': secret, 'scope': 'agent'}),
        ('compress', {}), ('ask_user', {'question': secret}),
    ]
    previews = [{'block': {'type': 'tool_use', 'id': 'u', 'name': name, 'input': value},
                 'preview': json.dumps(registry.mask_payload(value))} for name, value in inputs]
    payload = {'array': [secret, {'key-' + secret: secret}], 'boolean': True, 'nil': None, 'number': 1.25}
    payload_json = json.dumps(payload, sort_keys=True)
    payload_masked = json.dumps(registry.mask_payload(json.loads(payload_json)))
    # Key collision keeps the last value, with the first insertion position.
    collision = {'first-' + secret: 'first', 'first-<secret-hidden>': 'last'}
    collision_json = json.dumps(collision, sort_keys=True)
    collision_masked = json.dumps(registry.mask_payload(json.loads(collision_json)))
    rotation = SecretRegistry()
    calls = []
    def rotated():
        calls.append(1)
        return 'old-credential' if len(calls) == 1 else 'new-credential'
    rotation.register('KEY', rotated)
    injected = rotation.env_for_command('KEY')
    old_new = rotation.mask('old-credential new-credential')
    rotation_calls = len(calls)
    rotation.register('KEY', 'new-credential')
    reset = rotation.mask('old-credential new-credential')
    failure = SecretRegistry()
    tries = []
    clock = [0.0]
    def retry():
        tries.append(1)
        if len(tries) == 1:
            raise RuntimeError('sensitive vault error')
        return 'retry-credential'
    failure.register('KEY', retry)
    with patch('mini_loop.secrets.time.monotonic', side_effect=lambda: clock[0]):
        initial = failure.mask('retry-credential')
        unresolved = failure.unresolved()
        cached_failure = failure.mask('retry-credential')
        clock[0] = 60.0
        retried = failure.mask('retry-credential')
    texts = ['İ_KEY', 'STRAßE_TOKEN', 'ΑΣ', 'ΑΣΑ', "ΑΣ'Α", 'AΣ\u0301', 'Σ', 'AΣ\u0345A', '😀Σ', 'aΣⁱ', 'aΣⁱA']
    return {'secret': secret, 'default_patterns': DEFAULT_SECRET_PATTERNS,
            'masks': masks, 'environments': env_cases, 'previews': previews,
            'payload': {'input': payload_json, 'masked': payload_masked},
            'collision': {'input': collision_json, 'masked': collision_masked},
            'rotation': {'injected': injected, 'masked': old_new, 'calls': rotation_calls, 'reset': reset},
            'failure': {'initial': initial, 'cached': cached_failure, 'unresolved': unresolved,
                        'retried': retried, 'calls': len(tries), 'remaining': failure.unresolved()},
            'casing': [{'text': text, 'lower': text.lower(), 'upper': text.upper()} for text in texts]}

def _command_contracts(scratch: Path) -> dict[str, object]:
    """Real host shell results and explicit CommandResult rendering recipes."""
    import hashlib
    from dataclasses import asdict
    from unittest.mock import patch
    from mini_loop import tools as source
    scratch.mkdir(parents=True)
    cases = []
    recipes = [
        ("streams", "printf 'out\\n'; printf 'err\\n' >&2", 0),
        ("nonzero", "printf 'diagnostic' >&2; exit 7", 0),
        ("quiet_nonzero", "exit 3", 0),
        ("quiet_success", "true", 0),
        ("newlines", "printf 'a\\r\\nb\\rc\\n'", 0),
        ("invalid_utf8", "printf '\\341\\200A\\377\\342\\202'", 0),
        ("unicode_at_cap", "printf '你好🙂é'", 4),
        ("cwd", "pwd", 0),
    ]
    for name, command, cap in recipes:
        with patch.object(source, "MAX_BASH_CAPTURE", cap or 5_000_000):
            result = source.Toolset(scratch).run_bash_result(command)
        fields = asdict(result)
        fields.pop("duration_ms")
        rendered = result.render()
        if name == "cwd":
            fields["stdout"] = fields["stdout"].replace(str(scratch.resolve()), "<workspace>")
            fields["projection"] = fields["projection"].replace(str(scratch.resolve()), "<workspace>")
            rendered = "<workspace>"
        cases.append({"name": name, "command": command, "cap": cap, "result": fields, "rendered": rendered})

    rendering = []
    for name, stdout, stderr, exit_code, timed_out, overflowed, error, projection in [
        ("timeout_partial", "diagnostic", "", -9, True, False, "Error: Timeout (1s)", None),
        ("overflow", "abcd", "", -9, False, True, None, None),
        ("empty_overflow", "", "", -9, False, True, None, None),
        ("error_without_exit", "", "", None, False, False, "Error: Dangerous command blocked", None),
        ("projection_authority", "unsafe", "unsafe", 0, False, False, None, "safe"),
        ("tail_cap", "α" * 70_000 + "TAIL", "", 0, False, False, None, None),
        ("python_strip", "\x1c hello \x1f", "", 0, False, False, None, None),
    ]:
        result = source.CommandResult(stdout, stderr, exit_code, timed_out, overflowed, 0, error, projection, 4)
        rendered = result.render()
        fields = asdict(result)
        fields.pop("duration_ms")
        if name == "tail_cap":
            fields["stdout"] = "α"
        rendering.append({"name": name, "result": fields, "stdout_repeat": 70_000 if name == "tail_cap" else 0,
                          "stdout_suffix": "TAIL" if name == "tail_cap" else "",
                          "render_sha256": hashlib.sha256(rendered.encode()).hexdigest(), "render_chars": len(rendered)})
    return {"commands": cases, "rendering": rendering,
            "dangerous": [{"command": command, "blocked": source.looks_dangerous(command)} for command in
                          ["sudo echo x", "rm  -rf  /", "SUDO echo x", "r' 'm -rf /", "echo shutdown", "printf safe"]]}


def _loop_contracts(scratch: Path) -> dict[str, object]:
    """Actual caching projections, detector decisions/hashes and loop halts."""
    import asyncio
    import copy
    from dataclasses import asdict
    from types import SimpleNamespace
    from mini_loop.caching import DefaultCachePolicy, NullCachePolicy
    from mini_loop.stuck import DefaultStuckDetector, NullStuckDetector, StuckThresholds, ToolStep, step_hash
    from mini_loop.agent import Agent
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool, count_tokens
    from mini_loop.registry import Tool, ToolRegistry, Hooks

    caches = []
    def cache_case(name, system, messages, *, ttl=None, maximum=4, stride=15, null=False):
        original = copy.deepcopy(messages)
        policy = NullCachePolicy() if null else DefaultCachePolicy(ttl=ttl, max_breakpoints=maximum, stride=stride)
        cached_system, tools, cached_messages = policy.annotate(system=system, tools=None, messages=messages)
        positions = [[mi, bi] for mi, message in enumerate(cached_messages)
                     if isinstance(message["content"], list)
                     for bi, block in enumerate(message["content"])
                     if "cache_control" in block]
        caches.append({"name": name, "system": system, "messages": original, "ttl": ttl,
                       "maximum": maximum, "stride": stride, "null": null,
                       "cached_system": cached_system, "cached_messages": cached_messages,
                       "positions": positions, "source_unchanged": original == messages,
                       "fake_tokens": count_tokens({"system": cached_system, "messages": cached_messages, "tools": tools})})
    def history(width, rounds):
        messages = [{"role": "user", "content": "start"}]
        for ri in range(rounds):
            messages += [{"role": "assistant", "content": [{"type": "text", "text": "work"} for _ in range(width)]},
                         {"role": "user", "content": [{"type": "text", "text": f"result-{ri}-{bi}"} for bi in range(width)]}]
        return messages
    for width in [1, 12, 25]:
        for rounds in [1, 4]:
            cache_case(f"batch-{width}-rounds-{rounds}", "stable", history(width, rounds))
    cache_case("empty-system", "", history(2, 2))
    cache_case("absent-system", None, history(2, 2))
    cache_case("plain-only", "stable", [{"role": "user", "content": "plain"}])
    cache_case("ttl-and-stride", "stable", history(4, 4), ttl="1h", stride=3)
    cache_case("prefix-budget-only", "stable", history(12, 4), maximum=1)
    cache_case("null-policy", "stable", history(12, 4), null=True)

    decisions = []
    def step(name="bash", inp="a", out="same", failed=False, denied=False):
        return ToolStep(name, step_hash({"command": inp}), step_hash(out), failed, denied)
    def detect(name, steps, rounds=0, **overrides):
        thresholds = StuckThresholds(**overrides)
        signal = DefaultStuckDetector(thresholds).inspect(SimpleNamespace(recent_steps=steps, rounds_without_tools=rounds))
        decisions.append({"name": name, "steps": [asdict(value) for value in steps], "rounds": rounds,
                          "thresholds": asdict(thresholds), "signal": asdict(signal) if signal else None,
                          "reminder": signal.reminder() if signal else None})
    detect("before-repeat", [step()] * 3)
    detect("repeated-result", [step()] * 4)
    detect("changing-output", [step(out=str(i)) for i in range(8)])
    detect("repeated-denial", [step(denied=True)] * 3)
    detect("repeated-error-varies-output", [step(out=str(i), failed=True) for i in range(3)])
    detect("mixed-failure-and-denial", [step(failed=True), step(denied=True), step(failed=True)])
    detect("varied-input-failures", [step(inp=str(i), failed=True) for i in range(5)])
    detect("same-tool-success-suppresses", [step(inp=str(i), failed=True) for i in range(5)] + [step(inp="good")])
    detect("interleaved-workaround", [value for i in range(5) for value in [step(inp=str(i), denied=True), step("read_file", str(i), str(i))]])
    detect("insertion-order", [value for i in range(5) for value in [step("write_file", str(i), str(i), True), step("bash", str(i), str(i), True)]])
    detect("alternating", [step("read_file"), step("glob")] * 3)
    detect("unstable-alternating", [step("read_file", out=str(i)) if i % 2 == 0 else step("glob") for i in range(6)])
    detect("uniform-is-repeat", [step()] * 6)
    detect("monologue", [step()] * 4, rounds=3)
    detect("unproductive-disabled", [step(inp=str(i), failed=True) for i in range(8)], unproductive_tool=0)
    detect("custom-repeat", [step()] * 2, repeat_action_result=2)
    hashes = [{"tool": "bash" if "command" in value else "TodoWrite" if "items" in value else "edit_file" if "old_text" in value else "write_file" if "content" in value else "read_file" if "path" in value else "compress", "input": value, "input_hash": step_hash(value), "output": output, "output_hash": step_hash(output)} for value, output in [
        ({"command": "printf 你好\n"}, "你好\n"),
        ({"path": "x", "offset": 0, "limit": None}, "Error: missing"),
        ({"content": "x", "path": "out"}, "Wrote 1 bytes"),
        ({"old_text": "a", "new_text": "b", "path": "p"}, "Edited"),
        ({"items": [{"content": "Work", "status": "pending", "activeForm": "Working"}]}, "todo"),
        ({}, ""),
    ]]

    loops = []
    class DenyAll(Hooks):
        async def before_tool(self, ctx, call):
            return "Error: permission denied by policy"
    class AlwaysResume(Hooks):
        async def stop(self, agent, messages, last_text):
            return "keep going"
    async def run_loop(name, *, denied=False, max_nudges=1, monologue=False, null=False):
        calls = 0
        def responder(request):
            nonlocal calls
            calls += 1
            if monologue:
                return [text("loop commentary")], "end_turn"
            return [text("loop commentary"), tool("bash", _id=f"u{calls}", command="printf same")], "tool_use"
        async def same(ctx, command):
            return "same"
        registry = ToolRegistry()
        registry.register(Tool("bash", "shell", {"type": "object", "properties": {"command": {"type": "string"}}, "required": ["command"]}, same, risk="exec"))
        settings = Settings(fake_llm=True, workspace_root=scratch, skills_dir=scratch / "empty-skills", max_turns=12)
        events = []
        async def emit(event):
            events.append(event)
        (scratch / name).mkdir(parents=True)
        agent = Agent(client=FakeAsyncAnthropic(responder=responder), settings=settings, workspace=scratch / name,
                      tools=registry, hooks=AlwaysResume() if monologue else DenyAll() if denied else Hooks(),
                      stuck_detector=NullStuckDetector() if null else DefaultStuckDetector(StuckThresholds(max_nudges=max_nudges)), emit=emit)
        output = await agent.run("go")
        stuck_events = [{key: event.get(key) for key in ["pattern", "detail", "tool", "halted", "nudges_used"]}
                        for event in events if event.get("type") == "stuck"]
        reminders = [block["text"] for message in agent.messages if isinstance(message.get("content"), list)
                     for block in message["content"] if isinstance(block, dict) and block.get("type") == "text" and "<stuck" in block.get("text", "")]
        continuations = [message["content"] for message in agent.messages if isinstance(message.get("content"), str) and "<stuck" in message["content"]]
        loops.append({"name": name, "denied": denied, "max_nudges": max_nudges, "monologue": monologue, "null": null,
                      "calls": calls, "output": output, "events": stuck_events, "reminders": reminders, "continuations": continuations})
    async def all_loops():
        await run_loop("result")
        await run_loop("denied", denied=True)
        await run_loop("halt-immediately", max_nudges=0)
        await run_loop("monologue", monologue=True)
        await run_loop("null", null=True)
    asyncio.run(all_loops())
    return {"caches": caches, "decisions": decisions, "hashes": hashes, "loops": loops}


def _lifecycle_contracts(scratch: Path) -> dict[str, object]:
    """Actual AgentSession runs, event order, cancellation and bounded streams."""
    import asyncio
    import dataclasses
    from mini_loop.agent import Agent
    from mini_loop.activity import activity_title, tool_label
    from mini_loop.builtins import default_registry
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
    from mini_loop.registry import Hooks
    from mini_loop.session import AgentSession, BACKLOG, SUBSCRIBER_QUEUE_MAX
    from mini_loop.transport import DirectTransport

    cases = []
    class Deny(Hooks):
        async def before_tool(self, ctx, call):
            return "blocked by policy"
    async def run_case(name):
        root = scratch / name
        root.mkdir(parents=True)
        started = asyncio.Event()
        never = asyncio.Event()
        calls = 0
        def responder(request):
            nonlocal calls
            calls += 1
            if name == "provider-error":
                raise RuntimeError("offline fail")
            if name == "refusal":
                return [], "refusal"
            if name == "unknown-stop":
                return [text("complete")], "future_stop"
            if name == "pause" and calls == 1:
                return [text("partial")], "pause_turn"
            if name in ("completed", "denied", "cancel-tool", "failed-tool") and calls == 1:
                return [text("Inspect files. More context."), tool("bash", _id="u1", command="rg main .")], "tool_use"
            return [text("complete")], "end_turn"
        async def handler(ctx, command, **kwargs):
            if name == "cancel-tool":
                started.set()
                await never.wait()
            if name == "failed-tool":
                raise ValueError("tool failed")
            return "same"
        registry = default_registry()
        registry.register(dataclasses.replace(registry.get("bash"), handler=handler), replace=True)
        client = FakeAsyncAnthropic(responder=responder, thinking=False)
        if name == "cancel-model":
            async def blocked_create(**kwargs):
                started.set()
                await never.wait()
            client.messages.create = blocked_create
        session = AgentSession(name, root)
        session.permission_mode = "auto"
        session.workspace_bound = True
        settings = Settings(fake_llm=True, workspace_root=root, skills_dir=root / "empty-skills")
        agent = Agent(client=client, settings=settings, workspace=root, tools=registry,
                      hooks=Deny() if name == "denied" else Hooks(), system="stable",
                      transport=DirectTransport(), emit=session.emit, label=name,
                      state={"session": session, "permission_mode": "auto"})
        session.agent = agent
        live = session.subscribe(replay=False)
        if name.startswith("cancel-"):
            task = asyncio.create_task(session.run("go"))
            await started.wait()
            before = {k: session.info()[k] for k in ("status", "activity", "busy", "run_count")}
            cancelled = await session.cancel("stop now")
            try:
                await task
            except asyncio.CancelledError:
                pass
            output = None
        else:
            before = None
            cancelled = False
            output = await session.run("go")
        events = []
        identities = {}
        counters = {}
        def stable_id(value):
            if value is None:
                return None
            if value not in identities:
                kind = value.split("_", 1)[0]
                if kind == "act":
                    kind = "action" if len(value) > 20 else "activity"
                counters[kind] = counters.get(kind, 0) + 1
                identities[value] = f"{kind}_{counters[kind]}"
            return identities[value]
        while not live.empty():
            event = live.get_nowait()
            projected = {k: v for k, v in event.items()
                           if k not in ("ts", "duration_ms", "trajectory_id", "trajectory_status",
                                        "trajectory_recording_error", "state_persisted", "persist_error")}
            for key in ("span_id", "parent_span_id", "action_id", "activity_id", "message_id", "parent_message_id"):
                if key in projected:
                    projected[key] = stable_id(projected[key])
            events.append(projected)
        session.unsubscribe(live)
        info = {k: session.info()[k] for k in ("status", "activity", "busy", "run_count", "message_count", "subscribers")}
        cases.append({"name": name, "output": output, "before": before, "cancelled": cancelled,
                      "info": info, "events": events, "messages": agent.messages})
    async def all_cases():
        for name in ("completed", "denied", "failed-tool", "pause", "refusal", "unknown-stop",
                     "provider-error", "cancel-model", "cancel-tool"):
            await run_case(name)
        bus = AgentSession("bus", scratch)
        live = bus.subscribe(replay=False)
        for _ in range(SUBSCRIBER_QUEUE_MAX + 305):
            await bus.emit({"type": "status", "status": "idle"})
        for _ in range(5):
            await bus.emit({"type": "assistant_delta", "text": "piece", "_ephemeral": True})
        queued = []
        while not live.empty():
            queued.append(live.get_nowait())
        replay = bus.subscribe()
        replayed = []
        while not replay.empty():
            replayed.append(replay.get_nowait())
        bus.unsubscribe(live)
        bus.unsubscribe(replay)
        return {"backlog": BACKLOG, "queue": SUBSCRIBER_QUEUE_MAX,
                "live_count": len(queued), "live_first": queued[0]["seq"], "live_last": queued[-1]["seq"],
                "replay_count": len(replayed), "replay_first": replayed[0]["seq"], "replay_last": replayed[-1]["seq"],
                "ephemeral_live": sum(bool(e.get("ephemeral")) for e in queued),
                "ephemeral_replay": sum(bool(e.get("ephemeral")) for e in replayed)}
    bus = asyncio.run(all_cases())
    titles = [None, "", "  # Inspect files. Then write.", "你好。继续", "> Work! Continue", "x" * 100, "   \n   ", "***"]
    labels = [("read_file", {"path": "a\nb"}), ("write_file", {"path": "out"}), ("edit_file", {"path": "out"}),
              ("glob", {"pattern": "*.go"}), ("bash", {"command": "rg needle ."}), ("bash", {"command": "ls"}),
              ("bash", {"command": "cat a | wc"}), ("bash", {"command": "echo $(whoami)"}), ("bash", {"command": "cat file"}),
              ("compress", {})]
    return {"cases": cases, "bus": bus, "titles": [{"input": v, "title": activity_title(v)} for v in titles],
            "labels": [{"tool": n, "input": v, "display": tool_label(n, v)} for n, v in labels]}


def _scheduling_contracts(scratch: Path) -> dict[str, object]:
    """Real Python prompt chain, classified batches and per-session Todo nag."""
    import asyncio
    from types import SimpleNamespace
    from mini_loop.agent import Agent
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, tool
    from mini_loop.registry import Hook, Hooks, Tool, ToolCall, ToolRegistry
    from mini_loop.stuck import NullStuckDetector

    schema = {"type": "object", "properties": {"command": {"type": "string"}}, "required": ["command"]}
    modes = []
    def broken(call):
        raise ValueError("classifier failed")
    for name, static, classifier in [
        ("static-exclusive", False, None), ("static-parallel", True, None),
        ("override-exclusive", True, lambda call: "exclusive"),
        ("override-parallel", False, lambda call: "parallel"),
        ("invalid", True, lambda call: "invalid"), ("error", True, broken),
    ]:
        definition = Tool("bash", "shell", schema, lambda ctx, command: command,
                          parallel_safe=static, mode_for=classifier, risk="read")
        modes.append({"name": name, "static": static, "mode": definition.execution_mode(ToolCall("bash", {"command": "x"}, "x"))})

    async def exercise():
        seen = []
        class Keep(Hook):
            async def on_user_prompt(self, agent, text):
                seen.append(text)
        class Empty(Hook):
            async def on_user_prompt(self, agent, text):
                seen.append(text)
                return ""
        rewritten = await Hooks([Keep(), Empty(), Keep()]).user_prompt(SimpleNamespace(), "start")
        scratch.mkdir(parents=True)
        settings = Settings(fake_llm=True, workspace_root=scratch, skills_dir=scratch / "empty-skills", max_turns=2)
        first, tail = asyncio.Event(), asyncio.Event()
        log = []
        async def handler(ctx, command):
            if command == "a":
                await first.wait()
            elif command == "d":
                await tail.wait()
            log.append(command)
            if command == "b":
                first.set()
            elif command == "e":
                tail.set()
            return command
        registry = ToolRegistry()
        registry.register(Tool("bash", "shell", schema, handler, parallel_safe=True, risk="read",
                               mode_for=lambda call: "exclusive" if call.input["command"] == "c" else "parallel"))
        batch_agent = Agent(client=FakeAsyncAnthropic(), settings=settings, workspace=scratch,
                            tools=registry, tool_semaphore=asyncio.Semaphore(2), stuck_detector=NullStuckDetector())
        results = await batch_agent._exec_tool_batch([tool("bash", _id=name, command=name) for name in "abcde"])
        steps = [step.output_hash for step in batch_agent._recent_steps]

        async def echo(ctx, command):
            return "handled: go"
        registry = ToolRegistry()
        registry.register(Tool("bash", "shell", schema, echo, risk="read"))
        nag_agent = Agent(client=FakeAsyncAnthropic(), settings=settings, workspace=scratch,
                          tools=registry, stuck_detector=NullStuckDetector(), hooks=Hooks())
        nag_agent.todo.update([{"content": "open", "status": "pending", "activeForm": "Working"}])
        turns = []
        for index in range(4):
            before = len(nag_agent.messages)
            await nag_agent.run("go")
            reminders = [block["text"] for message in nag_agent.messages[before:]
                         if isinstance(message["content"], list)
                         for block in message["content"]
                         if block.get("type") == "text" and block.get("text") == "<reminder>Update your todos.</reminder>"]
            turns.append({"counter": nag_agent._rounds_without_todo, "reminders": reminders})
        return {"modes": modes, "prompt_seen": seen, "prompt": rewritten,
                "completion_order": log, "results": results, "step_outputs": steps,
                "todo_turns": turns, "default_tool_limit": settings.max_concurrent_tools}
    return asyncio.run(exercise())


def _manager_contracts(scratch: Path) -> dict[str, object]:
    """Capture actual manager creation/binding, deletion and shared defaults."""
    import asyncio
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager, WorkspaceBindingError, MAX_REMEMBERED_OWNERS
    from mini_loop.skills import SkillLoader

    scratch.mkdir(parents=True)
    scratch = scratch.resolve()
    allowed = scratch / "allowed"
    checkout = allowed / "repo"
    outside = scratch / "outside"
    checkout.mkdir(parents=True)
    outside.mkdir()
    (checkout / "keep").write_text("source")
    (allowed / "alias").symlink_to(checkout, target_is_directory=True)
    (allowed / "escape").symlink_to(outside, target_is_directory=True)
    managers = []
    def manager(name, roots=()):
        settings = Settings(fake_llm=True, workspace_root=scratch / name,
                            skills_dir=scratch / "empty-skills", bindable_roots=roots)
        value = SessionManager(settings, FakeAsyncAnthropic(), skills=SkillLoader(settings.skills_dir))
        managers.append(value)
        return value
    narrow, broad, off = manager("narrow", (allowed,)), manager("broad", (scratch,)), manager("off")
    cases = []
    for name, target, requested in [
        ("disabled", off, checkout), ("checkout", narrow, checkout),
        ("alias", narrow, allowed / "alias"), ("escape", narrow, allowed / "escape"),
        ("outside", narrow, outside), ("outside-missing", narrow, outside / "absent"),
        ("inside-missing", narrow, allowed / "absent"), ("file", narrow, checkout / "keep"),
        ("own-root", broad, broad.settings.workspace_root),
        ("own-child", broad, broad.settings.workspace_root / "absent"),
    ]:
        try:
            session = target.create(owner="owner", workspace=requested)
        except WorkspaceBindingError as error:
            detail = str(error)
            reason = ("disabled" if "binding is disabled" in detail else "own-root" if "manager's own" in detail
                      else "outside" if "outside every" in detail else "not-directory")
            cases.append({"name": name, "status": error.status, "reason": reason, "workspace": None, "bound": False})
        else:
            cases.append({"name": name, "status": 200, "reason": "", "workspace": str(session.workspace.relative_to(scratch)), "bound": session.workspace_bound})
            target.delete(session.id)
    core = manager("core")
    first, second = core.create(owner="first"), core.create(owner="second")
    fields = ["status", "activity", "busy", "run_count", "permission_mode", "workspace_bound", "model", "message_count", "todos", "subscribers", "sink_error"]
    initial = {key: first.info()[key] for key in fields}
    shared = {"model": first.agent.semaphore is second.agent.semaphore,
              "tools": first.agent.tool_semaphore is second.agent.tool_semaphore,
              "approvals": core.approvals is first.agent.state["manager"].approvals,
              "actions": first.agent.state["action_journal"] is second.agent.state["action_journal"]}
    before = [session.owner for session in core.list()]
    first_path = first.workspace
    deleted = core.delete(first.id)
    owners = {"remembered": core.session_owners.get(first.id), "listing": [session.owner for session in core.list()],
              "deleted": deleted, "workspace_removed": not first_path.exists(), "unknown_delete": core.delete("missing")}
    asyncio.run(core.stop())
    try:
        core.create(owner="first")
    except RuntimeError as error:
        stopped_create = str(error)
    else:
        raise AssertionError("stopped manager created a session")
    for target in managers:
        asyncio.run(target.stop())
    return {"binding_cases": cases, "initial": initial, "shared": shared, "owners_before": before,
            "deleted": owners, "bound_marker": (checkout / "keep").read_text(),
            "scratch_distinct": first.workspace != second.workspace, "id_length": len(first.id),
            "max_owners": MAX_REMEMBERED_OWNERS, "default_model_limit": core.settings.max_concurrent_llm,
            "default_tool_limit": core.settings.max_concurrent_tools, "default_rounds": core.settings.max_turns,
            "stopped_create": stopped_create, "stop_keeps_scratch": second.workspace.is_dir()}


def _control_contracts(scratch: Path) -> dict[str, object]:
    """Actual bounded steering, live posture changes and HTTP wakeup."""
    from mini_loop import SessionManager, Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
    from mini_loop.server import create_app
    from fastapi.testclient import TestClient
    import threading
    import time
    import asyncio

    scratch.mkdir(parents=True)
    def settings(name):
        return Settings(fake_llm=True, trajectory_enabled=False, enable_features=False,
                        workspace_root=scratch / name, skills_dir=scratch / "empty-skills")
    def wrappers(messages, tag):
        return [m["content"] for m in messages if isinstance(m.get("content"), str)
                and m["content"].startswith("<" + tag + ">")]
    async def scenario(name):
        calls = []
        box = {}
        def responder(kwargs):
            calls.append(kwargs["messages"])
            if name == "mid-round" and len(calls) == 1:
                box["session"].steer("actually, use staging")
                box["session"].change_permission_mode("readonly")
                return [tool("write_file", path="landed.txt", content="landed", _id="write")], "tool_use"
            return [text("done")], "end_turn"
        manager = SessionManager(settings(name), FakeAsyncAnthropic(responder=responder, thinking=False))
        session = box["session"] = manager.create()
        if name == "ordered":
            session.steer("first"); session.steer("second")
        elif name == "unicode":
            session.steer("🙂" * 16001)
        elif name == "overflow":
            for i in range(102): session.steer("input-" + str(i))
        elif name == "pre-first":
            session.change_permission_mode("auto")
        before = session.info()["pending_steering"]
        await session.run("go")
        if name == "posture-batch":
            for mode in ("auto", "readonly", "interactive", "interactive"):
                session.change_permission_mode(mode)
        await session.run("again")
        events = [{"type": event["type"], "count": event["count"], "text": event["text"]}
                  for event in session._backlog if event["type"] in ("steering_delivered", "posture_update")]
        result = {"name": name, "queued_before": before,
                  "pending_after": session.info()["pending_steering"], "mode": session.permission_mode,
                  "interjections": wrappers(session.agent.messages, "user_interjection"),
                  "postures": wrappers(session.agent.messages, "posture_update"), "events": events,
                  "request_interjections": [len(wrappers(c, "user_interjection")) for c in calls],
                  "request_postures": [len(wrappers(c, "posture_update")) for c in calls],
                  "capability_modes": [event["permission_mode"] for event in session._backlog
                                       if event["type"] == "capability_plan"],
                  "file_exists": (session.workspace / "landed.txt").exists()}
        await manager.stop()
        return result
    scenarios = asyncio.run(_gather_controls(scenario))
    saved = {key: os.environ.get(key) for key in ("MINILOOP_API_TOKENS", "MINILOOP_API_TOKEN")}
    try:
        os.environ["MINILOOP_API_TOKENS"] = "alice:token-a,bob:token-b"
        os.environ.pop("MINILOOP_API_TOKEN", None)
        entered = threading.Event()
        release = asyncio.Event()
        count = 0
        def responder(kwargs):
            nonlocal count
            count += 1
            if count == 1: return [tool("TodoWrite", items=[], _id="todo")], "tool_use"
            return [text("done")], "end_turn"
        fake = FakeAsyncAnthropic(responder=responder, thinking=False)
        original = fake.messages.create
        async def create(**kwargs):
            if count == 0:
                entered.set()
                await release.wait()
            return await original(**kwargs)
        fake.messages.create = create
        manager = SessionManager(settings("http"), fake)
        cases = []
        with TestClient(create_app(settings=settings("http"), manager=manager)) as http:
            headers = {"Authorization": "Bearer token-a"}
            sid = http.post("/sessions", json={}, headers=headers).json()["id"]
            session = manager._sessions[sid]
            def call(name, path, body, token="token-a"):
                r = http.post("/sessions/"+sid+"/"+path, json=body,
                              headers={"Authorization": "Bearer "+token})
                payload = r.json()
                if isinstance(payload, dict) and "session" in payload: payload["session"] = "session"
                if isinstance(payload, dict) and isinstance(payload.get("detail"), str):
                    payload["detail"] = payload["detail"].replace(sid, "session")
                cases.append({"name":name,"path":path,"body":body,"token":token,
                              "status":r.status_code,"response":payload})
            call("mode-pre-first", "mode", {"mode":"auto"})
            call("foreign-mode", "mode", {"mode":"readonly"}, "token-b")
            future = http.portal.start_task_soon(session.run, "go")
            if not entered.wait(5): raise RuntimeError("control fixture provider did not enter")
            try:
                call("busy-steer", "steer", {"message":"use staging"})
                call("foreign-steer", "steer", {"message":"foreign"}, "token-b")
                call("mode-live", "mode", {"mode":"readonly"})
                pending_busy = session.info()["pending_steering"]
            finally:
                http.portal.call(release.set)
            future.result(timeout=5)
            call("idle-steer", "steer", {"message":"check deploy"})
            deadline = time.monotonic() + 5
            while not (session.run_count == 2 and not session.busy):
                if time.monotonic() > deadline: raise RuntimeError("idle steer never completed")
                time.sleep(.001)
            http_result = {"cases":cases,"busy_pending":pending_busy,
                           "run_count":session.run_count,"pending_after":session.info()["pending_steering"],
                           "interjections":wrappers(session.agent.messages,"user_interjection"),
                           "postures":wrappers(session.agent.messages,"posture_update")}
        return {"max_chars":16000,"max_queue":100,"scenarios":scenarios,"http":http_result}
    finally:
        for key,value in saved.items():
            if value is None: os.environ.pop(key,None)
            else: os.environ[key]=value


async def _gather_controls(scenario):
    # Sequential because each scenario owns real workspace/session lifecycle.
    return [await scenario(name) for name in
            ("ordered", "unicode", "overflow", "pre-first", "posture-batch", "mid-round")]


def _fork_contracts(scratch: Path) -> dict[str, object]:
    """Actual completed-boundary fork, fresh state and owner-scoped HTTP."""
    import asyncio
    import copy
    import threading
    from fastapi.testclient import TestClient
    from mini_loop import SessionManager, Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
    from mini_loop.server import create_app

    def settings(name):
        return Settings(fake_llm=True, model="default-model", trajectory_enabled=False,
                        enable_features=False, workspace_root=scratch / name,
                        skills_dir=scratch / "empty-skills")
    async def probe():
        seen = []
        def responder(kwargs):
            seen.append(copy.deepcopy(kwargs))
            if len(seen) == 1:
                return [tool("TodoWrite", items=[{"content":"source task", "status":"pending",
                                                  "activeForm":"working"}], _id="todo")], "tool_use"
            return [text("noted")], "end_turn"
        manager = SessionManager(settings("runtime"), FakeAsyncAnthropic(responder=responder, thinking=False))
        source = manager.create(system="fixed source system", model="source-model", permission_mode="auto", owner="alice")
        (source.workspace / "source-only.txt").write_text("source")
        await source.run("the codeword is xyzzy")
        source.steer("parked source input")
        source.change_permission_mode("readonly")
        child = await manager.fork_session(source.id)
        initial = child.info()
        lineage = {**initial["forked_from"], "session":"source"}
        history = copy.deepcopy(child.agent.messages)
        initial_state = {key:initial[key] for key in ("status", "activity", "busy", "cancel_reason",
                         "run_count", "permission_mode", "pending_steering", "workspace_bound",
                         "model", "message_count", "todos", "subscribers", "forked_from")}
        initial_state["forked_from"] = lineage
        await child.run("what was the codeword?")
        source_events = [{"type":e["type"], "child":"child", "message_count":e["message_count"]}
                         for e in source._backlog if e["type"] == "session_forked"]
        result = {"initial":initial_state, "history":history,
                  "child_request_model":seen[-1]["model"], "child_request_system":seen[-1]["system"],
                  "child_request_messages":seen[-1]["messages"],
                  "source_unchanged":source.agent.messages == history,
                  "workspace_distinct":source.workspace != child.workspace,
                  "child_marker_exists":(child.workspace / "source-only.txt").exists(),
                  "source_pending":source.info()["pending_steering"],
                  "source_events":source_events}
        child.agent.messages[0]["content"] = "EDITED-IN-CHILD"
        result["rows_independent"] = source.agent.messages[0]["content"] == history[0]["content"]
        empty = manager.create(owner="alice")
        empty_child = await manager.fork_session(empty.id)
        result["empty"] = {"message_count":empty_child.info()["message_count"],
                           "forked_from":{"session":"empty", "message_count":0}}
        await manager.stop()
        return result
    result = asyncio.run(probe())
    saved = {key:os.environ.get(key) for key in ("MINILOOP_API_TOKENS", "MINILOOP_API_TOKEN")}
    try:
        os.environ["MINILOOP_API_TOKENS"] = "alice:token-a,bob:token-b"
        os.environ.pop("MINILOOP_API_TOKEN", None)
        entered = threading.Event()
        release = asyncio.Event()
        fake = FakeAsyncAnthropic(responder=lambda _:([text("noted")], "end_turn"), thinking=False)
        original = fake.messages.create
        async def delayed(**kwargs):
            entered.set()
            await release.wait()
            return await original(**kwargs)
        fake.messages.create = delayed
        manager = SessionManager(settings("http"), fake)
        cases = []
        with TestClient(create_app(settings=settings("http"), manager=manager)) as http:
            headers = {"Authorization":"Bearer token-a"}
            sid = http.post("/sessions", json={}, headers=headers).json()["id"]
            session = manager._sessions[sid]
            def call(name, session_id=sid, token="token-a"):
                response = http.post("/sessions/" + session_id + "/fork", headers={"Authorization":"Bearer " + token})
                payload = response.json()
                if "id" in payload:
                    payload["id"] = "child"
                    payload["created_at"] = 0
                    payload["workspace"] = "child-workspace"
                    payload["forked_from"]["session"] = "source"
                if "detail" in payload: payload["detail"] = payload["detail"].replace(sid,"source")
                cases.append({"name":name, "token":token, "status":response.status_code, "response":payload})
            call("foreign", token="token-b")
            call("missing", session_id="missing")
            call("empty")
            future = http.portal.start_task_soon(session.run, "go")
            if not entered.wait(5): raise RuntimeError("fork fixture provider did not enter")
            try: call("busy")
            finally: http.portal.call(release.set)
            future.result(timeout=5)
            call("completed")
        result["http"] = cases
    finally:
        for key,value in saved.items():
            if value is None: os.environ.pop(key,None)
            else: os.environ[key] = value
    return result


def _http_contracts(scratch: Path) -> dict[str, object]:
    """Actual FastAPI HTTP admission, ownership, CRUD, replay and SSE framing."""
    from fastapi.testclient import TestClient
    from mini_loop.auth import TokenAuth, NullAuth, refuse_open_bind
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app, _authenticated_message_context
    from mini_loop.auth import Principal
    import re

    scratch.mkdir(parents=True)
    saved = {key: os.environ.get(key) for key in ("MINILOOP_API_TOKENS", "MINILOOP_API_TOKEN")}
    ids = {}
    roots = {}
    def normalize(value):
        if isinstance(value, dict):
            return {key: (0 if key in ("created_at", "pid", "started_at", "uptime_s") else
                          "test-build" if key == "build" else normalize(item))
                    for key, item in value.items() if key != "posture"}
        if isinstance(value, list):
            return [normalize(item) for item in value]
        if isinstance(value, str):
            for original, symbol in roots.items():
                value = value.replace(original, symbol)
            for original, symbol in ids.items():
                value = value.replace(original, symbol)
            return value
        return value
    def run(name, authenticated):
        os.environ.pop("MINILOOP_API_TOKEN", None)
        if authenticated:
            os.environ["MINILOOP_API_TOKENS"] = "alice:token-a,bob:token-b"
        else:
            os.environ.pop("MINILOOP_API_TOKENS", None)
        cfg = Settings(fake_llm=True, trajectory_enabled=False, enable_features=False,
                       workspace_root=scratch / name, skills_dir=scratch / "empty-skills")
        client = FakeAsyncAnthropic(responder=lambda request: ([text("done")], "end_turn"), thinking=False)
        manager = SessionManager(cfg, client)
        cases = []
        with TestClient(create_app(settings=cfg, manager=manager)) as http:
            def call(label, method, path, body=None, token=None, key=None, created=None):
                headers = {}
                if token is not None:
                    headers["Authorization"] = "Bearer " + token
                if key is not None:
                    headers["Idempotency-Key"] = key
                response = http.request(method, path, json=body, headers=headers)
                payload = response.json()
                if created:
                    ids[payload["id"]] = created
                    roots[payload["workspace"]] = "workspace/" + created
                cases.append({"name": label, "method": method, "path": normalize(path),
                              "body": body, "headers": headers, "status": response.status_code,
                              "response": normalize(payload),
                              "challenge": response.headers.get("www-authenticate")})
                return payload
            call("health", "GET", "/healthz")
            if authenticated:
                call("missing-auth", "GET", "/sessions")
                call("query-not-for-data", "GET", "/sessions?access_token=token-a")
                call("unknown-route-gated", "GET", "/unknown")
            token = "token-a" if authenticated else None
            a = call("create", "POST", "/sessions", {}, token=token, created=name+"-a")
            b = call("create-second", "POST", "/sessions", {"mode": "auto", "system": ""}, token=token, created=name+"-b")
            sid = a["id"]
            call("listing", "GET", "/sessions", token=token)
            call("limit-one", "GET", "/sessions?limit=1", token=token)
            call("limit-zero", "GET", "/sessions?limit=0", token=token)
            call("detail", "GET", f"/sessions/{sid}", token=token)
            if authenticated:
                call("foreign", "GET", f"/sessions/{sid}", token="token-b")
            call("unknown", "GET", "/sessions/missing", token=token)
            call("approvals-empty", "GET", f"/sessions/{sid}/approvals", token=token)
            call("approval-missing", "POST", f"/sessions/{sid}/approvals/missing", {"decision": "allow"}, token=token)
            call("message", "POST", f"/sessions/{sid}/messages", {"message": "go"}, token=token, key="retry")
            call("idempotent", "POST", f"/sessions/{sid}/messages", {"message": "different"}, token=token, key="retry")
            call("cancel-idle", "POST", f"/sessions/{sid}/cancel", token=token)
            call("transcript-null", "GET", f"/sessions/{sid}/transcript", token=token)
            call("method", "POST", "/healthz")
            call("delete", "DELETE", f"/sessions/{sid}", token=token)
            call("deleted-cache-not-readable", "POST", f"/sessions/{sid}/messages", {"message": "go"}, token=token, key="retry")
            call("listing-after-delete", "GET", "/sessions", token=token)
            # Completed message streams are finite, so TestClient can inspect the
            # actual EventSourceResponse framing without a hanging observe request.
            headers = {"Authorization": "Bearer " + token} if token else {}
            response = http.post(f"/sessions/{b['id']}/messages/stream", json={"message": "go"}, headers=headers)
            frames = []
            for block in re.split(r"\r?\n\r?\n", response.text):
                fields = {}
                for line in block.splitlines():
                    key, _, value = line.partition(":")
                    fields[key] = value.strip()
                if "data" in fields:
                    event = json.loads(fields["data"])
                    frames.append({"id": fields["id"], "event": fields["event"],
                                   "seq": event["seq"], "type": event["type"], "session": normalize(event["session"])})
            stream = {"status": response.status_code, "content_type": response.headers["content-type"],
                      "cache_control": response.headers["cache-control"], "frames": frames}
        return {"cases": cases, "stream": stream}
    try:
        token_auth = TokenAuth({"token-a": "alice", "token-b": "bob"})
        auth_cases = []
        for header in (None, "", "Bearer", "Bearer ", "Basic token-a", "Bearer wrong", "bEaReR token-a", "Bearer token-b"):
            principal = token_auth.authenticate(header)
            auth_cases.append({"authorization": header, "principal": principal.id if principal else None,
                               "anonymous": principal.anonymous if principal else False})
        context = _authenticated_message_context(Principal("alice")).as_dict()
        context.pop("message_id")
        return {"auth": auth_cases, "principals": list(token_auth.principals()),
                "bind": [{"host": host, "refused": refuse_open_bind(host, NullAuth()) is not None}
                         for host in ("", "localhost", "127.0.0.1", "::1", "0.0.0.0", "remote")],
                "http_context": context, "authenticated": run("token", True), "anonymous": run("anon", False),
                "deferred_health_fields": ["posture"]}
    finally:
        for key, value in saved.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value


def _provider_contracts() -> dict[str, object]:
    """Real pinned SDK requests/replies and HTTP retry behavior, offline."""
    import asyncio
    import anthropic
    import httpx
    from unittest.mock import patch
    from anthropic._constants import DEFAULT_TIMEOUT, MODEL_NONSTREAMING_TOKENS
    from mini_loop.providers import AnthropicCompatibleProvider
    from mini_loop.builtins import default_registry

    request = {"model":"requested-model", "max_tokens":8000,
               "system":[{"type":"text", "text":"fixed system", "cache_control":{"type":"ephemeral"}}],
               "messages":[{"role":"user", "content":[{"type":"text", "text":"go",
                                                           "cache_control":{"type":"ephemeral"}}]}],
               "tools":[default_registry().get("bash").schema]}
    response = {"id":"msg_actual", "type":"message", "role":"assistant", "model":"served-alias",
                "content":[{"type":"thinking", "thinking":"private thought", "signature":"signed"},
                           {"type":"text", "text":"working", "citations":None},
                           {"type":"tool_use", "id":"tool-1", "name":"bash", "input":{"command":"echo handled"}}],
                "stop_reason":"tool_use", "stop_sequence":None, "stop_details":None, "container":None,
                "usage":{"input_tokens":11,"output_tokens":7,"cache_read_input_tokens":3,
                         "cache_creation_input_tokens":5,"service_tier":"standard",
                         "cache_creation":{"ephemeral_5m_input_tokens":5,"ephemeral_1h_input_tokens":0},
                         "server_tool_use":None}}
    class BrokenBody(httpx.AsyncByteStream):
        async def __aiter__(self):
            yield b'{"id":"partial'
            raise httpx.ReadError("body interrupted")
    async def scenario(spec):
        frames, waits = [], []
        sequence = spec.get("statuses",[200])
        async def responder(req):
            frames.append({"path":str(req.url), "method":req.method,
                           "headers":{k:req.headers[k] for k in ("x-api-key","anthropic-version","x-stainless-retry-count","content-type")},
                           "body":__import__("json").loads(req.content)})
            index = len(frames)-1
            status = sequence[min(index,len(sequence)-1)]
            if status == "connection": raise httpx.ConnectError("broken socket", request=req)
            if status == "timeout": raise httpx.ReadTimeout("socket timed out", request=req)
            if status == "body": return httpx.Response(200, stream=BrokenBody(), headers={"content-type":"application/json"})
            body = response if status==200 else {"type":"error", "error":{"type":"fixture_error","message":"fixture failure"}}
            return httpx.Response(status, json=body, headers={"request-id":"request-fixture", **spec.get("headers",{})})
        async def wait(seconds): waits.append(seconds)
        client = anthropic.AsyncAnthropic(api_key="fixture-key", base_url="https://provider.invalid/proxy/",
                                         http_client=httpx.AsyncClient(transport=httpx.MockTransport(responder), timeout=DEFAULT_TIMEOUT),
                                         **({"timeout":3} if spec.get("custom_timeout") else {}))
        kwargs = {**request, "model":spec.get("model",request["model"]), "max_tokens":spec.get("max_tokens",8000)}
        with patch("anthropic._base_client.random", return_value=0), patch("anthropic._base_client.anyio.sleep", wait), patch("anthropic._base_client.time.time", return_value=1700000000):
            try:
                reply = await client.messages.create(**kwargs)
                # Normalize only consumed domain fields; nullable citations are
                # an explicit unported SDK metadata field, not fake evidence.
                content = []
                for block in reply.content:
                    row = block.model_dump()
                    if row["type"]=="text": row.pop("citations",None)
                    content.append(row)
                payload = {k:getattr(reply,k) for k in ("id","type","role","model","stop_reason","stop_sequence")}
                payload["content"] = content
                payload["usage"] = {k:getattr(reply.usage,k) for k in ("input_tokens","output_tokens","cache_read_input_tokens","cache_creation_input_tokens","service_tier")}
                outcome = {"reply":payload, "error_class":None, "status":0}
            except Exception as error:
                outcome = {"reply":None,"error_class":type(error).__name__,"status":getattr(error,"status_code",0)}
        await client.close()
        return {**spec, "frames":frames, "waits":waits, **outcome}
    cases = [
        {"name":"success"},
        *[{"name":"retry-"+str(code),"statuses":[code,code,200]} for code in (408,409,429,500,529)],
        {"name":"exhausted","statuses":[503]},
        {"name":"bad-request","statuses":[400]},
        {"name":"unauthorized","statuses":[401]},
        {"name":"forbidden","statuses":[403]},
        {"name":"missing","statuses":[404]},
        {"name":"invalid","statuses":[422]},
        {"name":"retry-forced","statuses":[400],"headers":{"x-should-retry":"true"}},
        {"name":"retry-denied","statuses":[503],"headers":{"x-should-retry":"false"}},
        {"name":"seconds","statuses":[429,200],"headers":{"retry-after":"15"}},
        {"name":"date","statuses":[429,200],"headers":{"retry-after":"Tue, 14 Nov 2023 22:13:40 GMT"}},
        {"name":"milliseconds","statuses":[429,200],"headers":{"retry-after-ms":"1250","retry-after":"15"}},
        {"name":"ms-invalid","statuses":[429,200],"headers":{"retry-after-ms":"bad","retry-after":"15"}},
        {"name":"ms-nan","statuses":[429,200],"headers":{"retry-after-ms":"nan","retry-after":"15"}},
        *[{"name":"header-"+value,"statuses":[429,200],"headers":{"retry-after":value}} for value in ("0","-1","300","inf","nan","bad")],
        {"name":"connection","statuses":["connection","connection",200]},
        {"name":"timeout","statuses":["timeout"]},
        {"name":"interrupted-body","statuses":["body",200]},
        {"name":"generic-ceiling","max_tokens":21333},
        {"name":"generic-preflight","max_tokens":21334},
        {"name":"opus-ceiling","model":"claude-opus-4-1-20250805","max_tokens":8192},
        {"name":"opus-preflight","model":"claude-opus-4-1-20250805","max_tokens":8193},
        {"name":"custom-timeout","max_tokens":64000,"custom_timeout":True},
    ]
    async def gather():
        return [await scenario(spec) for spec in cases]
    return {"sdk_version":anthropic.__version__,"sdk_max_retries":2,
            "sdk_source_sha256":{name:hashlib.sha256((Path(anthropic.__file__).parent / name).read_bytes()).hexdigest()
                                 for name in ("_base_client.py", "_constants.py", "resources/messages/messages.py")},
            "sdk_model_ceilings":MODEL_NONSTREAMING_TOKENS,
            "omitted_nullable_sdk_fields":["text.citations"], "request":request,
            "descriptions":[AnthropicCompatibleProvider(api_key="fixture-key").describe(),
                            AnthropicCompatibleProvider(base_url="https://provider.invalid/proxy/",api_key="fixture-key").describe()],
            "cases":asyncio.run(gather())}


def _stream_contracts() -> dict:
    import asyncio
    import anthropic
    import httpx
    import json
    import hashlib
    from types import SimpleNamespace
    from unittest.mock import patch
    from anthropic.lib.streaming._messages import AsyncMessageStream
    from mini_loop.transport import StreamingTransport, DELTA_COALESCE_SECONDS, DELTA_COALESCE_CHARS

    start = {"id":"msg_stream", "type":"message", "role":"assistant", "model":"served-stream",
             "content":[],"stop_reason":None,"stop_sequence":None,
             "usage":{"input_tokens":12,"output_tokens":0,"cache_read_input_tokens":3,"cache_creation_input_tokens":5,"service_tier":"standard"}}
    def event(kind, **fields): return {"type":kind, **fields}
    events = [event("message_start",message=start),
              event("content_block_start",index=0,content_block={"type":"thinking","thinking":"","signature":""}),
              event("content_block_delta",index=0,delta={"type":"thinking_delta","thinking":"秘密"*105}),
              event("content_block_delta",index=0,delta={"type":"signature_delta","signature":"signed-proof"}),
              event("content_block_stop",index=0),
              event("content_block_start",index=1,content_block={"type":"text","text":"","citations":None}),
              event("content_block_delta",index=1,delta={"type":"text_delta","text":"你好 world"*30}),
              event("content_block_delta",index=1,delta={"type":"text_delta","text":"!"}),
              event("content_block_stop",index=1),
              event("content_block_start",index=2,content_block={"type":"tool_use","id":"tool-stream","name":"write_file","input":{},"caller":{"type":"direct"}}),
              event("content_block_delta",index=2,delta={"type":"input_json_delta","partial_json":'{"path":"artifact.txt",'}),
              event("content_block_delta",index=2,delta={"type":"input_json_delta","partial_json":'"content":"你好\\n"}'}),
              event("content_block_stop",index=2),
              event("message_delta",delta={"stop_reason":"tool_use","stop_sequence":None},usage={"output_tokens":9,"input_tokens":20,"cache_read_input_tokens":7,"cache_creation_input_tokens":0}),
              event("message_stop")]
    request={"model":"requested-stream","max_tokens":64000,"messages":[{"role":"user","content":"go"}]}
    class Body(httpx.AsyncByteStream):
        def __init__(self,data,drop): self.data,self.drop,self.closed=data,drop,False
        async def __aiter__(self):
            # Fragment UTF-8, line endings and every JSON token across reads.
            for offset in range(0,len(self.data),3): yield self.data[offset:offset+3]
            if self.drop: raise httpx.ReadError("stream dropped")
        async def aclose(self): self.closed=True
    async def scenario(spec):
        selected=events
        if spec.get("drop"):
            selected=events[:8] # both a flushed delta and a pending tail
        if spec.get("error"):
            selected=events[:8]+[event("error",error={"type":"overloaded_error","message":"overloaded"})]
        if spec.get("empty"):
            selected=[events[0],event("message_delta",delta={"stop_reason":"refusal","stop_sequence":None},usage={"output_tokens":0}),events[-1]]
        ending=spec.get("ending","\n")
        lines=[]
        for item in selected:
            data=json.dumps(item,ensure_ascii=False)
            if spec.get("no_type"): data=json.dumps({k:v for k,v in item.items() if k!="type"},ensure_ascii=False)
            # multiline data only splits at whitespace-safe JSON punctuation
            if spec.get("multiline"): data=data.replace(', "index"',',\n"index"')
            lines.extend([": comment", "event: "+item["type"], *["data: "+part for part in data.split("\n")], "", "event: ping", 'data: {"type":"ping"}', ""])
        payload=(ending.join(lines)+ending).encode()
        frames,waits,bodies,progress,captured=[],[],[],[],[]
        async def respond(req):
            frames.append({"body":json.loads(req.content),"retry":req.headers["x-stainless-retry-count"],"accept":req.headers["accept"]})
            if spec.get("retry") and len(frames)==1: return httpx.Response(429,json={"error":{"message":"retry"}},headers={"retry-after":"1"})
            body=Body(payload,spec.get("drop",False));bodies.append(body)
            return httpx.Response(200,stream=body,headers={"content-type":"text/event-stream"})
        async def wait(delay): waits.append(delay)
        async def send(kind, **fields):
            ephemeral=fields.pop("_ephemeral",False)
            captured.append({"type":kind,**fields,"ephemeral":ephemeral})
        class Secrets:
            def mask(self,text): return text.replace("秘密","[REDACTED]")
        agent=SimpleNamespace(client=anthropic.AsyncAnthropic(api_key="fixture-key",base_url="https://provider.invalid/",http_client=httpx.AsyncClient(transport=httpx.MockTransport(respond))),secrets=Secrets(),_send=send,streamed_text="stale",_last_stream_id=None)
        reply=None;error=None
        real_iter = AsyncMessageStream.__aiter__
        async def watch(stream):
            async for item in real_iter(stream):
                if item.type == "content_block_delta":
                    delta=item.delta
                    if delta.type=="text_delta": progress.append({"kind":"text","text":delta.text})
                    if delta.type=="thinking_delta": progress.append({"kind":"thinking","text":delta.thinking})
                yield item
        class StableUUID: hex="0123456789abcdef"*2
        with patch.object(AsyncMessageStream,"__aiter__",watch),patch("mini_loop.transport.uuid.uuid4",return_value=StableUUID()),patch("mini_loop.transport.time",SimpleNamespace(monotonic=lambda:0)),patch("anthropic._base_client.anyio.sleep",wait),patch("anthropic._base_client.random",return_value=0):
            try:
                reply=await StreamingTransport().send(agent,request)
                content=[]
                for block in reply.content:
                    row=block.model_dump()
                    if row["type"]=="text":
                        row.pop("citations",None)
                        row.pop("parsed_output",None)
                    if row["type"]=="tool_use": row={k:row[k] for k in ("type","id","name","input","caller")}
                    content.append(row)
                reply={"id":reply.id,"type":reply.type,"role":reply.role,"model":reply.model,"content":content,"stop_reason":reply.stop_reason,"stop_sequence":reply.stop_sequence,
                       "usage":{k:getattr(reply.usage,k) for k in ("input_tokens","output_tokens","cache_read_input_tokens","cache_creation_input_tokens","service_tier")}}
            except Exception as exc: error=type(exc).__name__
        await agent.client.close()
        return {"name":spec["name"],"wire":payload.decode(),"frames":frames,"waits":waits,"reply":reply,"error_class":error,"events":captured,"raw_deltas":progress,"partial":agent.streamed_text,"closed":all(b.closed for b in bodies)}
    async def collect():
        return [await scenario(spec) for spec in [{"name":"complete"},{"name":"crlf","ending":"\r\n"},{"name":"cr","ending":"\r"},{"name":"multiline","multiline":True},{"name":"no-type","no_type":True},{"name":"retry","retry":True},{"name":"refusal","empty":True},{"name":"drop","drop":True},{"name":"error","error":True}]]
    return {"sdk_version":anthropic.__version__,"request":request,"coalesce_chars":DELTA_COALESCE_CHARS,"coalesce_seconds":DELTA_COALESCE_SECONDS,"default_coalesce_chars":DELTA_COALESCE_CHARS,"default_coalesce_seconds":DELTA_COALESCE_SECONDS,
            "source_sha256":{name:hashlib.sha256((Path(anthropic.__file__).parent/name).read_bytes()).hexdigest() for name in ("lib/streaming/_messages.py","_streaming.py")},
            "transport_sha256":hashlib.sha256((REPO_ROOT/"python/mini_loop/transport.py").read_bytes()).hexdigest(),"cases":asyncio.run(collect())}


def _recovery_contracts() -> dict:
    import asyncio
    import copy
    from types import SimpleNamespace
    from unittest.mock import patch
    from mini_loop.recovery import DefaultRecovery, DirectRecovery
    from mini_loop.fake_llm import FakeMessage, FakeUsage, TextBlock, ToolUseBlock
    from mini_loop.agent import _content_payload
    from mini_loop.recovery import reactive_compact

    base={"model":"unknown-model","max_tokens":8000,"messages":[{"role":"user","content":"write"}]}
    def text(body,stop="end_turn"):
        return {"text":body,"stop":stop}
    def problem(name="ConnectionError",message="dropped",status=0,after=None):
        return {"error":name,"message":message,"status":status,"after":after}
    pair=[{"role":"assistant","content":[{"type":"tool_use","id":"u","name":"bash","input":{"command":"pwd"}}]},
          {"role":"user","content":[{"type":"tool_result","tool_use_id":"u","content":"cwd"}]}]
    history=[{"role":"user","content":"old "*500}]+pair+[{"role":"assistant" if i%2==0 else "user","content":"turn"} for i in range(5)]
    def reply(step):
        blocks=[TextBlock(step.get("text","done"))]
        if step.get("tool"): blocks.append(ToolUseBlock("bash",{"command":"pwd"},"u"))
        return FakeMessage(blocks,step.get("stop","end_turn"),FakeUsage(21,4),model="served",message_id="message")
    async def scenario(spec):
        kw=copy.deepcopy(base);kw["model"]=spec.get("model",kw["model"]);kw["max_tokens"]=spec.get("budget",8000)
        if spec.get("long"):kw["messages"]=copy.deepcopy(history)
        live=copy.deepcopy(kw["messages"])
        calls,events,waits=[],[],[]
        async def emit(kind,**fields):events.append({"type":kind,**fields})
        async def wait(seconds):waits.append(seconds)
        agent=SimpleNamespace(_send=emit,state={},transport=SimpleNamespace(streaming=spec.get("streaming",False)))
        async def call(kwargs):
            calls.append(copy.deepcopy(kwargs))
            steps=spec["steps"];step=steps[min(len(calls)-1,len(steps)-1)]
            if step.get("error"):
                exc=type(step["error"],(Exception,),{})(step["message"])
                if step.get("status"):exc.status_code=step["status"]
                if step.get("after") is not None:exc.response=SimpleNamespace(headers={"retry-after":str(step["after"])})
                raise exc
            return reply(step)
        recovery=DirectRecovery() if spec.get("direct") else DefaultRecovery(fallback_model=spec.get("fallback"),max_retries=spec.get("retries",10),escalate=spec.get("escalate",True),max_continuations=spec.get("continuations",3))
        final=None;error=None
        with patch("mini_loop.recovery.random.random",return_value=0),patch("mini_loop.recovery.asyncio.sleep",wait):
            try:
                result=await recovery.run(agent,kw,call,live_history=live)
                final={"id":result.id,"type":result.type,"role":result.role,"model":result.model,"content":_content_payload(result.content),"stop_reason":result.stop_reason,"stop_sequence":result.stop_sequence,"usage":vars(result.usage)}
            except Exception as exc:error=type(exc).__name__
        return {**spec,"calls":calls,"events":events,"waits":waits,"final":final,"error_class":error,"live":live,"model_override":agent.state.get("recovery_model")}
    cases=[
        {"name":"direct","direct":True,"steps":[text("partial","max_tokens")]},
        {"name":"success","steps":[text("done")]},
        {"name":"connection","steps":[problem(),text("done")]},
        {"name":"timeout","steps":[problem("TimeoutError"),text("done")]},
        {"name":"rate-prose","steps":[problem("Exception","rate limit exceeded"),text("done")]},
        {"name":"overload-fallback","fallback":"backup","steps":[problem("Exception","overloaded",529)]*3+[text("done")]},
        {"name":"exhausted","retries":2,"steps":[problem()]},
        {"name":"wait-limit","steps":[problem("Exception","rate limited",429,300)]},
        {"name":"after-zero","steps":[problem("Exception","rate limited",429,0),text("done")]},
        {"name":"after-big","steps":[problem("Exception","rate limited",429,10000),text("done")]},
        {"name":"after-nan","steps":[problem("Exception","rate limited",429,"nan"),text("done")]},
        {"name":"after-negative","steps":[problem("Exception","rate limited",429,-1),text("done")]},
        {"name":"fatal","steps":[problem("ValueError","bad request")]},
        {"name":"small-headroom","model":"claude-opus-4-1-20250805","steps":[text("front","max_tokens"),text("tail")]},
        {"name":"known-escalation","model":"claude-opus-4-1-20250805","budget":2000,"steps":[text("discarded","max_tokens"),text("regenerated")]},
        {"name":"unknown-refusal","steps":[text("front","max_tokens"),problem("ValueError","Streaming is required"),text("tail")]},
        {"name":"stream-escalation","streaming":True,"steps":[text("discarded","max_tokens"),text("regenerated")]},
        {"name":"continue","escalate":False,"steps":[text("first","max_tokens"),text("second","max_tokens"),text("last")]},
        {"name":"continuation-bound","escalate":False,"steps":[text("chunk","max_tokens")]},
        {"name":"truncated-tool","escalate":False,"steps":[{**text("call","max_tokens"),"tool":True}]},
        {"name":"continued-tool","escalate":False,"steps":[text("front","max_tokens"),{**text("call","max_tokens"),"tool":True}]},
        {"name":"reactive-paired","long":True,"steps":[problem("ValueError","prompt is too long"),text("done")]},
        {"name":"reactive-no-shrink","steps":[problem("ValueError","context_length_exceeded")]},
        {"name":"reactive-once","long":True,"steps":[problem("ValueError","prompt is too long")]},
    ]
    async def collect(): return [await scenario(spec) for spec in cases]
    return {"cases":asyncio.run(collect()),"pair_compaction":reactive_compact(history),"source_sha256":hashlib.sha256((PYTHON_ROOT/"mini_loop/recovery.py").read_bytes()).hexdigest()}


def _progress_contracts() -> dict:
    """Actual configurable StreamingTransport and stateful fake-client calls."""
    import asyncio
    import hashlib
    from types import SimpleNamespace
    from unittest.mock import patch
    from mini_loop.transport import StreamingTransport
    from mini_loop.fake_llm import FakeAsyncAnthropic, FakeMessage, FakeUsage, _FakeStreamEvent
    from mini_loop.builtins import default_registry

    async def progress(spec):
        clock = [0.0]
        events = []
        class Stream:
            async def __aenter__(self): return self
            async def __aexit__(self, *exc): return False
            async def __aiter__(self):
                for piece in spec["pieces"]:
                    clock[0] = piece["seconds"]
                    yield _FakeStreamEvent(piece["text"], 0, piece["kind"])
                if spec.get("fail"): raise ConnectionError("script stream dropped")
            async def get_final_message(self): return FakeMessage([], "end_turn")
        async def send(kind, **fields):
            ephemeral = fields.pop("_ephemeral", False)
            fields["stream_id"] = "<stream-id>"
            events.append({"type": kind, **fields, "ephemeral": ephemeral})
        class Secrets:
            def mask(self, value): return value.replace("秘密", "[REDACTED]")
        agent = SimpleNamespace(client=SimpleNamespace(messages=SimpleNamespace(stream=lambda **kwargs: Stream())),
                                secrets=Secrets(), _send=send, streamed_text="stale", _last_stream_id=None)
        options = {}
        if "chars" in spec: options["coalesce_chars"] = spec["chars"]
        if "duration" in spec: options["coalesce_seconds"] = spec["duration"]
        failure = None
        with patch("mini_loop.transport.time", SimpleNamespace(monotonic=lambda: clock[0])):
            try: await StreamingTransport(**options).send(agent, {})
            except ConnectionError as exc: failure = str(exc)
        return {**spec, "events": events, "partial": agent.streamed_text, "failure": failure}

    def piece(text, seconds=0, kind="text"): return {"text": text, "seconds": seconds, "kind": kind}
    specs = [
        {"name":"defaults", "pieces":[piece("界a"),piece("b"),piece("c")]},
        {"name":"characters", "chars":4, "duration":999, "pieces":[piece("界a"),piece("bc"),piece("d")]},
        {"name":"elapsed", "chars":999, "duration":0.1, "pieces":[piece("A",0.09),piece("B",0.1),piece("C",0.11),piece("D",0.3)]},
        {"name":"zero-characters", "chars":0, "duration":999, "pieces":[piece(""),piece("a"),piece("b")]},
        {"name":"zero-duration", "chars":999, "duration":0, "pieces":[piece(""),piece("a"),piece("b")]},
        {"name":"negative-characters", "chars":-1, "duration":999, "pieces":[piece("a"),piece("b")]},
        {"name":"negative-duration", "chars":999, "duration":-1, "pieces":[piece("a"),piece("b")]},
        {"name":"mixed-interrupted", "chars":3, "duration":999, "pieces":[piece("秘密",kind="thinking"),piece("A"),piece("B")], "fail":True},
        {"name":"unshown-interrupted", "chars":999, "duration":999, "pieces":[piece("hidden")], "fail":True},
    ]
    def dump(reply):
        return {"id":reply.id,"type":reply.type,"role":reply.role,"model":reply.model,
                "content":[{"type":block.type, **vars(block)} for block in reply.content],
                "stop_reason":reply.stop_reason,"stop_sequence":reply.stop_sequence,"usage":vars(reply.usage)}
    async def fake():
        schema = default_registry().schemas()[0]
        # The selected tool is bash; pin the name instead of relying on catalogue order.
        schema = next(s for s in default_registry().schemas() if s["name"] == "bash")
        request = {"model":"fake-requested", "max_tokens":8000, "messages":[{"role":"user","content":"go"}], "tools":[schema]}
        rows=[]
        for enabled in (True,False):
            client=FakeAsyncAnthropic(thinking=enabled,delay=0)
            for method in ("direct","direct","stream","direct","stream"):
                if method=="direct": reply=await client.messages.create(**request); deltas=[]
                else:
                    async with client.messages.stream(**request) as stream:
                        deltas=[]
                        async for event in stream:
                            value=event.delta
                            if hasattr(value,"thinking"): deltas.append({"kind":"thinking","text":value.thinking})
                            else: deltas.append({"kind":"text","text":value.text})
                        reply=await stream.get_final_message()
                rows.append({"thinking":enabled,"method":method,"calls":client.calls,"reply":dump(reply),"deltas":deltas})
        return {"request":request,"rows":rows}
    async def collect(): return {"progress":[await progress(s) for s in specs],"fake":await fake()}
    return {"source_sha256":{name:hashlib.sha256((REPO_ROOT/"python/mini_loop"/name).read_bytes()).hexdigest() for name in ("transport.py","fake_llm.py")}, **asyncio.run(collect())}


def _trajectory_contracts(scratch: Path) -> dict:
    """Execute the real source JSONL writer/reader and managed recording lifecycle."""
    import asyncio
    import hashlib
    import json
    from types import SimpleNamespace
    from unittest.mock import patch
    from mini_loop.trajectory import TrajectoryStore
    from mini_loop import Settings, SessionManager
    from mini_loop.fake_llm import FakeAsyncAnthropic, tool, text

    scratch.mkdir(parents=True, exist_ok=True)
    scratch = scratch.resolve()
    cases = []
    for name in ("open", "restarted", "completed", "cancelled", "error", "malformed-tail", "last-end-wins", "redacted"):
        store = TrajectoryStore(scratch / name, capture_content=name != "redacted")
        with patch("mini_loop.trajectory.uuid.uuid4", return_value=SimpleNamespace(hex="a" * 32)), patch("mini_loop.trajectory.time.time", return_value=1000.25):
            trajectory_id = store.start(session_id="session-a", owner="alice", run_index=2, input_text="你好🙂" * 55, metadata={"model":"fixture-model", "workspace":"<workspace>", "build":"fixture-build", "system":"private system", "custom":{"content":"private nested"}})
        events = [
            {"type":"model_start", "model_input":{"messages":[{"role":"user", "content":"private prompt"}], "system":"private system"}},
            {"type":"tool_use", "name":"bash", "input":{"command":"private command"}},
            {"type":"tool_result", "output":"private output", "error":False, "denied":False},
            {"type":"tool_result", "output":"denied", "error":False, "denied":True},
            {"type":"error", "error":"private exception"},
            {"type":"assistant_text", "text":"private final"},
        ]
        for index, event in enumerate(events, 1):
            store.append(trajectory_id, {**event, "seq":index, "ts":10.0 + index, "session":"session-a"})
        if name not in ("open", "restarted"):
            with patch("mini_loop.trajectory.time.time", return_value=1001.125):
                store.finish(trajectory_id, status=name if name in ("cancelled", "error") else "completed", output="private final", error="private error" if name == "error" else None, duration_ms=25.12355)
        if name == "last-end-wins":
            with patch("mini_loop.trajectory.time.time", return_value=1002.5):
                store.finish(trajectory_id, status="cancelled", duration_ms=30.25)
        if name == "malformed-tail":
            with store._path(trajectory_id).open("a") as handle:
                handle.write('{"truncated":\n')
        reader = TrajectoryStore(store.root, capture_content=store.capture_content) if name == "restarted" else store
        raw = store.raw(trajectory_id)
        cases.append({"name":name, "capture_content":store.capture_content, "raw":raw, "summary":reader.summary(trajectory_id), "document":reader.get(trajectory_id), "count":reader.count("session-a"), "root_mode":store.root.stat().st_mode & 0o777, "file_mode":store._path(trajectory_id).stat().st_mode & 0o777})

    class FailingStore(TrajectoryStore):
        def __init__(self, root, stage):
            super().__init__(root)
            self.stage = stage
        def start(self, **kwargs):
            if self.stage == "start-failure":
                raise OSError("fixture recording failure")
            return super().start(**kwargs)
        def append(self, trajectory_id, event):
            if self.stage == "append-failure":
                raise OSError("fixture recording failure")
            return super().append(trajectory_id, event)
        def finish(self, trajectory_id, **kwargs):
            if self.stage == "finish-failure":
                raise OSError("fixture recording failure")
            return super().finish(trajectory_id, **kwargs)

    async def managed(name):
        long_command = "awk 'BEGIN {for(i=0;i<6000;i++)printf \"A\"}'"
        def responder(request):
            if name == "provider-error":
                raise RuntimeError("fixture provider failure")
            if request["messages"][-1]["role"] == "user" and isinstance(request["messages"][-1]["content"], str):
                return [tool("bash", _id="trajectory-tool", command=long_command)], "tool_use"
            return [text("done")], "end_turn"
        settings = Settings(model="fixture-model", fake_llm=True, trajectory_enabled=name != "disabled", trajectory_root=scratch / ("runtime-" + name), workspace_root=scratch / ("workspaces-" + name))
        injected = FailingStore(settings.trajectory_root, name) if name.endswith("failure") else None
        client = FakeAsyncAnthropic(responder=responder, delay=3600 if name == "cancelled" else 0)
        manager = SessionManager(settings, client, trajectory_store=injected)
        session = manager.create(permission_mode="auto", owner="alice")
        if name == "cancelled":
            task = asyncio.create_task(session.run("record this"))
            while not any(event.get("type") == "model_start" for event in session._backlog):
                await asyncio.sleep(0)
            await session.cancel("stop now")
            try:
                await task
            except asyncio.CancelledError:
                pass
            final = None
        else:
            final = await session.run("record this")
        rows = manager.trajectories.list(session_id=session.id) if manager.trajectories is not None else []
        document = manager.trajectories.get(rows[0]["id"]) if rows else None
        live = list(session._backlog)
        terminal = next((event for event in reversed(live) if event.get("type") in ("done", "status", "error") and (event.get("type") != "status" or event.get("cancelled"))), {})
        recorded_events = document["events"] if document else []
        inputs = [event["model_input"] for event in recorded_events if "model_input" in event]
        recorded_output = next((event["output"] for event in recorded_events if event.get("type") == "tool_result"), None)
        live_output = next((event["output"] for event in live if event.get("type") == "tool_result"), None)
        return {"name":name, "final":final, "status":document["status"] if document else None, "owner":document["owner"] if document else None, "count":session.info()["trajectory_count"], "active":session.info()["active_trajectory_id"] is not None, "recording_error":session.info()["trajectory_recording_error"] is not None, "terminal_status":terminal.get("trajectory_status"), "terminal_persisted":terminal.get("trajectory_persisted"), "stored_terminal_status":next((event.get("trajectory_status") for event in reversed(recorded_events) if event.get("type") in ("done", "status", "error")), None), "recorded_types":[event["type"] for event in recorded_events], "live_output_chars":len(live_output) if live_output is not None else None, "stored_output_chars":len(recorded_output) if recorded_output is not None else None, "request_count":len(inputs), "request_has_full_result":any(part.get("type") == "tool_result" and len(part.get("content", "")) == 6000 for model_input in inputs for message in model_input["messages"] if isinstance(message.get("content"), list) for part in message["content"]), "ephemeral_recorded":any(event.get("ephemeral") for event in recorded_events)}
    async def collect():
        return [await managed(name) for name in ("completed", "provider-error", "cancelled", "disabled", "start-failure", "append-failure", "finish-failure")]
    from fastapi.testclient import TestClient
    from mini_loop.server import create_app
    cfg = Settings(fake_llm=True, trajectory_enabled=True, workspace_root=scratch / "http-workspaces", trajectory_root=scratch / "http-traces")
    manager = SessionManager(cfg, FakeAsyncAnthropic())
    session = manager.create(owner="alice")
    with patch("mini_loop.trajectory.uuid.uuid4", return_value=SimpleNamespace(hex="c" * 32)), patch("mini_loop.trajectory.time.time", return_value=1000.25):
        http_id = manager.trajectories.start(session_id=session.id, owner="alice", run_index=1, input_text="inspect", metadata={"model":"fixture-model", "workspace":"<workspace>", "build":"fixture-build"})
    with patch("mini_loop.trajectory.time.time", return_value=1001.125):
        manager.trajectories.finish(http_id, status="completed", output="done", duration_ms=25.5)
    seed = manager.trajectories.raw(http_id)
    def normalize(value):
        if isinstance(value, dict):
            return {key:normalize(item) for key,item in value.items()}
        if isinstance(value, list):
            return [normalize(item) for item in value]
        if isinstance(value, str):
            return value.replace(session.id, "<session>").replace(http_id, "<trajectory>")
        return value
    routes = []
    with patch.dict(os.environ, {"MINILOOP_API_TOKENS":"alice:token-a,bob:token-b", "MINILOOP_API_TOKEN":""}):
        with TestClient(create_app(settings=cfg, manager=manager)) as http:
            def call(name, path, token="token-a"):
                response = http.get(path, headers={"Authorization":"Bearer "+token})
                if response.headers.get("content-type", "").startswith("application/x-ndjson"):
                    body = [json.loads(line) for line in response.text.splitlines()]
                else:
                    body = response.json()
                routes.append({"name":name, "path":normalize(path), "token":token, "status":response.status_code, "body":normalize(body), "content_type":response.headers.get("content-type"), "disposition":normalize(response.headers.get("content-disposition"))})
            call("owner-list", "/trajectories")
            call("foreign-list", "/trajectories", "token-b")
            call("session-list", f"/sessions/{session.id}/trajectories")
            call("foreign-session-list", f"/sessions/{session.id}/trajectories", "token-b")
            call("session-filter", f"/trajectories?session_id={session.id}&limit=0")
            call("inspect", f"/trajectories/{http_id}")
            call("foreign-inspect", f"/trajectories/{http_id}", "token-b")
            call("export-json", f"/trajectories/{http_id}/export")
            call("export-jsonl", f"/trajectories/{http_id}/export?format=jsonl")
            call("foreign-export", f"/trajectories/{http_id}/export?format=jsonl", "token-b")
            call("invalid-format", f"/trajectories/{http_id}/export?format=csv")
            call("invalid-id", "/trajectories/traj_bad")
            http.delete(f"/sessions/{session.id}", headers={"Authorization":"Bearer token-a"})
            call("retained-after-delete", f"/trajectories/{http_id}")
            call("retained-list", "/trajectories")
        restarted = SessionManager(cfg, FakeAsyncAnthropic())
        with TestClient(create_app(settings=cfg, manager=restarted)) as http:
            call("retained-after-restart", f"/trajectories/{http_id}")
            call("foreign-after-restart", f"/trajectories/{http_id}", "token-b")

    return {"source_sha256":{name:hashlib.sha256((REPO_ROOT / "python/mini_loop" / name).read_bytes()).hexdigest() for name in ("trajectory.py", "session.py", "agent.py", "server.py")}, "stores":cases, "rounding":[{"input":value, "output":round(value, 3)} for value in (25.12355, -25.12355, 1.2345, -1.2345, 0.0005, -0.0005, 2.675, 1.0625, -1.0625, 0.00001)], "managed":asyncio.run(collect()), "http":{"seed":normalize(seed), "routes":routes}}



def _cron_contracts(scratch: Path) -> dict:
    """Actual cron parsing, operator state, claims, loss and dispatch authority."""
    import asyncio
    import uuid
    from dataclasses import asdict
    from datetime import datetime
    from unittest.mock import patch
    from mini_loop.cron import CronJob, CronScheduler, cron_matches, validate_cron

    scratch.mkdir(parents=True)
    dates = [datetime(2026, 10, 5, 12, 30), datetime(2026, 11, 13),
             datetime(2026, 11, 6), datetime(2026, 10, 4),
             datetime(2026, 10, 5, 2, 10), datetime(2026, 10, 5, 9, 15)]
    expressions = ["* * * * *", "*/15 9-17 * * 1-5", "0 0 13 * 5",
                   "0 0 */1 * 0", "0 0 * * 0", "0 0 1,13 * *",
                   "+0 0 * * *", "١_٠ ٢ * * *", "0\x1c0 * * *",
                   "*/99999999999999999999999 * * * *", "1-10/3 * * * *",
                   "* * * *", "*/0 * * * *", "0/2 * * * *", "1,,2 * * * *",
                   "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 7",
                   "10-1 * * * *", "-1 * * * *", "foo * * * *", "1.0 * * * *",
                   "*/1/2 * * * *", "1__0 * * * *", "* * * * * *"]
    parser = [{"expression":e, "error":validate_cron(e), "matches":[cron_matches(e,d) for d in dates]} for e in expressions]
    fixed = uuid.UUID("aabbccdd-0000-0000-0000-000000000000")
    now = dates[0]
    later = now.replace(minute=31)
    class Recording(CronScheduler):
        def __init__(self, path, secrets=None):
            self.fires=[]
            super().__init__(None, durable_path=path, secrets=secrets)
        def _fire(self, job):
            self.fires.append({"job":asdict(job), "disk":json.loads(self.durable_path.read_text()) if self.durable_path else []})
    path=scratch / "lifecycle.json"
    s=Recording(path)
    operations=[]
    def record(name, text=""):
        operations.append({"name":name,"text":text,"jobs":[asdict(j) for j in s.jobs.values()],
                           "armed":[j for j in s.jobs if s.armed(j)],"problems":s.problems.summary(),
                           "fires":s.fires.copy(),"claims":len(list(s._claims_dir.iterdir())) if s._claims_dir.exists() else 0})
    with patch("mini_loop.cron.uuid.uuid4",return_value=fixed):
        record("schedule", s.schedule("session-a", "* * * * *", "中文 run"))
    record("list",s.list_for("session-a"))
    s._tick_once(now);record("tick")
    s._tick_once(now);record("same-minute")
    s._tick_once(later);record("next-minute")
    s=Recording(path);record("restore",s.list_for("session-a"))
    s._tick_once(later);record("disarmed-tick")
    record("foreign-arm",s.arm("aabbccdd","other"))
    record("arm",s.arm("aabbccdd","session-a"))
    s._tick_once(later);record("restored-same-minute")
    s._tick_once(later.replace(minute=32));record("restored-next-minute")
    record("foreign-cancel",s.cancel("aabbccdd","other"))
    record("cancel",s.cancel("aabbccdd","session-a"))
    record("empty",s.list_for("session-a"))

    one_path=scratch / "one-shot.json"
    one=Recording(one_path)
    with patch("mini_loop.cron.uuid.uuid4",return_value=fixed):
        one.schedule("session-a","* * * * *","once",recurring=False)
    one._tick_once(now)
    one_shot={"jobs":[asdict(j) for j in one.jobs.values()],"fires":one.fires,
              "disk":json.loads(one_path.read_text())}
    pair_path=scratch / "pair.json"
    seed=CronJob("shared","* * * * *","claim","session-a",True,True)
    pair_path.write_text(json.dumps([asdict(seed)]))
    left,right=Recording(pair_path),Recording(pair_path)
    left.arm_all();right.arm_all()
    left._tick_once(now);right._tick_once(now)
    right._tick_once(later);left._tick_once(later)
    pair={"left_fires":len(left.fires),"right_fires":len(right.fires),
          "left_marker":left.jobs['shared'].last_fired,"right_marker":right.jobs['shared'].last_fired,
          "claims":len(list(left._claims_dir.iterdir())),"problems":left.problems.summary()+right.problems.summary()}
    losses=[]
    for kind in ("claim","save","one-shot-save"):
        loss_path=scratch / (kind+".json")
        loss_seed=CronJob("lost","* * * * *","lost","session-a",kind!="one-shot-save",True)
        loss_path.write_text(json.dumps([asdict(loss_seed)]))
        loss=Recording(loss_path);loss.arm_all()
        if kind=="claim": loss._claims_dir.write_text("blocked")
        else: loss_path.unlink();loss_path.mkdir()
        loss._tick_once(now)
        losses.append({"kind":kind,"job_count":len(loss.jobs),"marker":loss.jobs['lost'].last_fired if 'lost' in loss.jobs else None,
                       "fire_count":len(loss.fires),"problem_total":loss.problems.total()})
    class Mask:
        def mask(self,value):return value.replace("secret","[MASK]")
    mask_path=scratch / "mask.json"
    masked=Recording(mask_path,Mask())
    with patch("mini_loop.cron.uuid.uuid4",return_value=fixed):
        masked.schedule("session-a","* * * * *","secret")
    masking={"live":masked.jobs['aabbccdd'].prompt,"stored":json.loads(mask_path.read_text())[0]['prompt'],
             "restored":Recording(mask_path).jobs['aabbccdd'].prompt,"problems":masked.problems.summary()}
    bounded=Recording(None)
    prompt_error=bounded.schedule("s","* * * * *","你"*8001,durable=False)
    for _ in range(200): bounded.schedule("s","* * * * *","ok",durable=False)
    job_error=bounded.schedule("s","* * * * *","ok",durable=False)
    async def authority_probe():
        seen=[]
        class Runner:
            async def run(self,prompt,*,run_context):
                seen.append({"prompt":prompt,"authority":run_context.authority})
        class Manager:
            def get(self,sid):return Runner() if sid=="session-a" else None
        actual=CronScheduler(Manager())
        actual._fire(CronJob("invoke","* * * * *","go","session-a"))
        await asyncio.gather(*actual._running)
        actual._fire(CronJob("missing","* * * * *","go","absent"))
        await actual.stop()
        return {"runs":seen,"problems":actual.problems.summary()}
    return {"dates":[d.isoformat() for d in dates],"parser":parser,"operations":operations,
            "one_shot":one_shot,"pair":pair,"losses":losses,"masking":masking,
            "bounds":{"prompt_error":prompt_error,"job_error":job_error},"authority":asyncio.run(authority_probe()),
            "source_sha256":{name:hashlib.sha256((PYTHON_ROOT/'mini_loop'/name).read_bytes()).hexdigest()
                             for name in ('cron.py','durable.py','problems.py','run_context.py')}}


def _child_background_contracts(scratch: Path) -> dict:
    """Actual selected children and the shared-ledger live-parent source gap."""
    import asyncio
    from mini_loop import Settings
    from mini_loop.agent import Agent
    from mini_loop.background import background_injector
    from mini_loop.builtins import full_registry
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
    from mini_loop.harness import Harness
    from mini_loop.registry import ToolCall
    from mini_loop.run_context import RunContext
    from mini_loop.tool_policy import DEFAULT_ROLE_TOOL_POLICY

    class Selected:
        def __init__(self, check_only): self.check_only = check_only
        def select(self, role, parent):
            return parent.subset(["check_background"] if self.check_only else parent.names())

    async def scenario(name, action, role, live_parent, default=False):
        root = (scratch / name).resolve()
        root.mkdir(parents=True)
        children, requests = [], []
        async def capture(agent):
            if agent.depth and agent not in children: children.append(agent)
            mgr = agent.state.get("background")
            if agent.depth and mgr is not None:
                own = [v["handle"] for v in mgr._tasks.values() if v.get("handle")]
                if own and not live_parent:
                    await asyncio.gather(*own, return_exceptions=True)
                elif own:
                    for _ in range(1000):
                        if (root / "child-started").exists(): break
                        await asyncio.sleep(.005)
                    else: raise AssertionError("source child task never started")
            return []
        def responder(kwargs):
            requests.append(kwargs)
            if len(requests) == 1:
                if action == "check": call = tool("check_background", _id="child")
                elif action == "bash": call = tool("bash", _id="child", command="printf child", run_in_background=True)
                else: call = tool("background_run", _id="child", command="touch child-started; sleep 30" if live_parent else "printf child")
                return [call], "tool_use"
            return [text("child done")], "end_turn"
        registry = full_registry(background=True, tasks=False, memory=False, cron=False,
                                 plan=False, goals=False, diagnostics=False,
                                 session_query=False, teams=False, worktrees=False,
                                 mcp=False, self_audit=False)
        parent = Agent(client=FakeAsyncAnthropic(responder=responder, thinking=False),
                       settings=Settings(fake_llm=True, workspace_root=root, subagent_max_rounds=4),
                       workspace=root, label="main", tools=registry,
                       role_tool_policy=DEFAULT_ROLE_TOOL_POLICY if default else Selected(action == "check"),
                       harness=Harness(injectors=(capture, background_injector)),
                       state={"permission_mode":"auto"})
        parent_service = None
        try:
            if live_parent:
                await parent._exec_tool(ToolCall("background_run", {"command":"touch parent-started; sleep 30"}, "parent"))
                parent_service = parent.state["background"]
                for _ in range(1000):
                    if (root / "parent-started").exists(): break
                    await asyncio.sleep(.005)
                else: raise AssertionError("source parent task never started")
            summary = await parent.subagents.run(parent, prompt="child", agent_type=role, run_context=RunContext.default())
            child = children[0]
            child_service = child.state.get("background")
            task_result = next(block["content"] for message in child.messages
                               if isinstance(message["content"], list) for block in message["content"]
                               if block.get("type") == "tool_result" and block.get("tool_use_id") == "child")
            own_ids = [] if child_service is None else [key for key, value in child_service._tasks.items() if value.get("handle")]
            own_id = own_ids[0] if own_ids else None
            normalized = task_result.replace(own_id, "<child-id>") if own_id else task_result
            result = {"name":name, "action":action, "role":role, "live_parent":live_parent,
                      "default_role":default, "summary":summary, "tools":child.tools.names(),
                      "output":normalized, "child_has_service":child_service is not None,
                      "child_distinct_service":child_service is None or child_service is not parent_service,
                      "child_live_after_return":0 if child_service is None else child_service.live_count(),
                      "own_status":None if own_id is None else child_service._tasks[own_id]["status"],
                      "parent_still_running":parent_service is not None and parent_service.live_count() == 1,
                      "parent_ledger_exists":(root / ".background/bg_0001.json").exists() if live_parent else None,
                      "parent_reported_orphan":child_service is not None and child_service._tasks.get("bg_0001",{}).get("status") == "orphaned"}
            return result
        finally:
            for agent in [parent, *children]:
                if (mgr := agent.state.get("background")) is not None: await mgr.close()

    async def collect():
        return [await scenario(*case) for case in (
            ("selected-run", "run", "worker", False),
            ("selected-bash", "bash", "worker", False),
            ("explore-denied", "run", "Explore", False),
            ("check-only", "check", "worker", False),
            ("live-parent-selected", "run", "worker", True),
            ("live-parent-default", "bash", "worker", True, True),
        )]
    return {"cases":asyncio.run(collect()), "source_sha256":{
        name:hashlib.sha256((PYTHON_ROOT / "mini_loop" / name).read_bytes()).hexdigest()
        for name in ("subagents.py", "background.py", "builtins.py", "harness.py", "tool_policy.py")}}


def _managed_background_contracts(scratch: Path) -> dict:
    """Actual source manager deletion, stop and fresh-fork ownership."""
    import asyncio
    from mini_loop import SessionManager, Settings
    from mini_loop.builtins import full_registry, default_injectors
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool

    def registry():
        return full_registry(background=True, tasks=False, memory=False, cron=False,
                             plan=False, goals=False, diagnostics=False,
                             session_query=False, teams=False, worktrees=False,
                             mcp=False, self_audit=False)

    async def wait_started(session):
        for _ in range(1000):
            if (session.workspace / "started").exists():
                return
            await asyncio.sleep(.005)
        raise AssertionError("managed source background did not start")

    async def scenario(name, action, bound):
        root = (scratch / name).resolve()
        root.mkdir(parents=True)
        checkout = root / "checkout"
        checkout.mkdir()
        def responder(kwargs):
            if isinstance(kwargs["messages"][-1]["content"], str):
                return [tool("background_run", _id="managed-bg",
                             command="touch started; sleep 30 & wait")], "tool_use"
            return [text("started")], "end_turn"
        manager = SessionManager(
            Settings(fake_llm=True, workspace_root=root / "ws",
                     bindable_roots=(checkout,)),
            FakeAsyncAnthropic(responder=responder, thinking=False),
            tool_registry=registry(),
            injectors=default_injectors(background=True, teams=False),
        )
        session = manager.create(owner="alice", permission_mode="auto",
                                 workspace=checkout if bound else None)
        try:
            output = await session.run("start")
            await wait_started(session)
            service = session.agent.state["background"]
            child = await manager.fork_session(session.id) if action == "fork" else None
            fresh = child is None or "background" not in child.agent.state
            different_root = child is None or child.workspace != session.workspace
            if action == "stop":
                removed = None
                await manager.stop()
            else:
                removed = manager.delete(session.id, remove_workspace=action != "preserve")
                if manager._cleanup_tasks:
                    await asyncio.gather(*tuple(manager._cleanup_tasks))
            return {"name": name, "action": action, "bound": session.workspace_bound,
                    "output": output, "removed": removed,
                    "workspace_exists": session.workspace.is_dir(),
                    "status": service._tasks["bg_0001"]["status"],
                    "ledger_exists": (session.workspace / ".background/bg_0001.json").exists(),
                    "fresh_fork": fresh, "different_fork_root": different_root,
                    "tools": session.agent.tools.names(),
                    "cleanup_errors": list(manager.cleanup_errors)}
        finally:
            await manager.stop()

    async def collect():
        return [await scenario(*case) for case in (
            ("scratch-delete", "delete", False),
            ("scratch-preserve", "preserve", False),
            ("bound-delete", "delete", True),
            ("scratch-stop", "stop", False),
            ("fresh-fork", "fork", False),
        )]
    return {"cases": asyncio.run(collect()), "source_sha256": {
        name: hashlib.sha256((PYTHON_ROOT / "mini_loop" / name).read_bytes()).hexdigest()
        for name in ("manager.py", "background.py", "session.py", "builtins.py")}}


def _background_tool_contracts(scratch: Path) -> dict:
    """Actual optional schemas, gate calls, classifier and interruption marker."""
    import asyncio
    from mini_loop import Settings
    from mini_loop.agent import Agent
    from mini_loop.background import install_background
    from mini_loop.builtins import full_registry
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.registry import ToolRegistry, ToolCall
    from mini_loop.approvals import grant_candidate, proposed_candidate
    from mini_loop.session import AgentSession
    registry=install_background(ToolRegistry())
    metadata=[{"name":tool.name,"risk":tool.risk,"readonly":tool.readonly,
               "parallel_safe":tool.parallel_safe,"capabilities":sorted(tool.capabilities)}
              for name in registry.names() if (tool:=registry.get(name)) is not None]
    variants=[
        {"name":"background_run","input":{"command":"printf é"}},
        {"name":"background_run","input":{"command":"printf é","timeout":None,"approval_prefix":None}},
        {"name":"background_run","input":{"command":"printf é","timeout":0}},
        {"name":"background_run","input":{"command":"printf é","timeout":-1}},
        {"name":"background_run","input":{"command":"git status --short","timeout":12,"approval_prefix":["git","status"]}},
        {"name":"background_run","input":{"command":"rm -rf stuff","approval_prefix":["rm","-rf"]}},
        {"name":"background_run","input":{"command":"git status --short","approval_prefix":["git","pull"]}},
        {"name":"check_background","input":{}},
        {"name":"check_background","input":{"bg_id":None}},
        {"name":"check_background","input":{"bg_id":""}},
        {"name":"check_background","input":{"bg_id":"bg_0001"}},
    ]
    for row in variants:
        row["canonical"]=json.dumps(row["input"],sort_keys=True,ensure_ascii=False,separators=(",",":"))
        row["candidate"]=list(grant_candidate(row["name"],row["input"]) or [])
        row["proposed"]=list(proposed_candidate(row["name"],row["input"]) or [])
    def tools(enabled):
        return full_registry(background=enabled,tasks=False,memory=False,cron=False,plan=False,goals=False,
                             diagnostics=False,session_query=False,teams=False,worktrees=False,mcp=False,self_audit=False)
    mode_rows=[]
    for enabled in (False,True):
        reg=tools(enabled)
        for explicit in (None,False,True):
            value={"command":"printf test"}
            if explicit is not None:value["run_in_background"]=explicit
            mode_rows.append({"enabled":enabled,"input":value,"mode":reg.get("bash").execution_mode(ToolCall("bash",value,"mode"))})
    async def collect():
        root=scratch/"enabled";root.mkdir(parents=True)
        agent=Agent(client=FakeAsyncAnthropic(),settings=Settings(fake_llm=True,workspace_root=root/"ws"),
                    workspace=root,tools=tools(True),state={"permission_mode":"auto"})
        specs=[("check_background",{}),
               ("background_run",{"command":"printf hello","timeout":2}),
               ("check_background",{"bg_id":"bg_0001"}),
               ("background_run",{"command":":","timeout":0,"approval_prefix":None}),
               ("check_background",{"bg_id":"bg_0002"}),
               ("bash",{"command":"printf explicit","run_in_background":True}),
               ("check_background",{"bg_id":"bg_0003"}),
               ("bash",{"command":"printf ' build'","run_in_background":False}),
               ("check_background",{"bg_id":"bg_0004"}),
               ("bash",{"command":"printf foreground; exit 3"}),
               ("background_run",{"command":"sleep 30","timeout":-1}),
               ("check_background",{"bg_id":"bg_0005"}),
               ("background_run",{"command":"rm  -rf  /"}),
               ("check_background",{"bg_id":"missing"}),
               ("check_background",{})]
        rows=[]
        for index,(name,value) in enumerate(specs):
            out=await agent._exec_tool(ToolCall(name,value,f"bg-tool-{index}"))
            rows.append({"name":name,"input":value,"output":str(out)})
            if str(out).startswith("Started background task"):
                bg_id=str(out).split()[3].removesuffix(":")
                await asyncio.wait_for(asyncio.shield(agent.state["background"]._tasks[bg_id]["handle"]),5)
        await agent.state["background"].close()
        # Actual source interruption method with one observed, running native PID.
        await agent._exec_tool(ToolCall("background_run",{"command":"sleep 30"},"survivor"))
        manager=agent.state["background"]
        for _ in range(1000):
            path=root/".background/bg_0006.json"
            if path.exists() and json.loads(path.read_text()).get("pid") is not None:break
            await asyncio.sleep(.005)
        else:raise AssertionError("source survivor never started")
        agent.messages=[];agent.streamed_text="partial source"
        outer=AgentSession("bg-source",root);outer.agent=agent
        marked=outer._record_interruption("source bg cancel",[])
        marker=agent.messages[-1]["content"][0]["text"]
        repaired=outer._record_interruption("source bg cancel",["pending"])
        await manager.close()
        root=scratch/"disabled";root.mkdir(parents=True)
        disabled=Agent(client=FakeAsyncAnthropic(),settings=Settings(fake_llm=True,workspace_root=root/"ws"),
                       workspace=root,tools=tools(False))
        value={"command":"printf disabled","run_in_background":True}
        output=await disabled._exec_tool(ToolCall("bash",value,"disabled"))
        return {"steps":rows,"disabled":{"input":value,"output":str(output),"has_manager":"background" in disabled.state},
                "interruption":{"marked":marked,"text":marker,"repaired":repaired}}
    return {"schemas":registry.schemas(),"metadata":metadata,"variants":variants,"modes":mode_rows,
            **asyncio.run(collect()),"source_sha256":{name:hashlib.sha256((PYTHON_ROOT/"mini_loop"/name).read_bytes()).hexdigest()
            for name in ("background.py","builtins.py","permissions.py","approvals.py","session.py")}}


def _background_contracts(scratch: Path) -> dict:
    """Execute actual background commands, ledger adoption and bounded injection."""
    import asyncio
    from unittest.mock import patch
    from mini_loop.background import BackgroundManager, background_injector, should_run_background
    from mini_loop.agent import Agent
    from mini_loop import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic

    commands = [
        ("merged", "printf one; printf two >&2; printf three", 5_000_000, None),
        ("empty", ":", 5_000_000, None),
        ("exit", "printf fail; exit 7", 5_000_000, None),
        ("empty-exit", "exit 3", 5_000_000, None),
        ("raw-newlines", r"printf 'a\r\nb\rc\n'", 5_000_000, None),
        ("invalid-utf8", r"printf '\377\342\202'", 5_000_000, None),
        ("exact-bytes", "printf 中", 3, None),
        ("byte-overflow", "printf 中中", 3, None),
        ("partial-utf8", "printf 中x", 2, None),
        ("tail", "i=0; while [ $i -lt 60000 ]; do printf x; i=$((i+1)); done; printf FINAL; exit 4", 5_000_000, 30),
        ("timeout", "sleep 30", 5_000_000, 1),
        ("negative-timeout", "sleep 30", 5_000_000, -1),
    ]
    async def joined(manager, bg_id):
        await asyncio.wait_for(asyncio.shield(manager._tasks[bg_id]["handle"]), 40)
    def probe(manager):
        return {"listing": manager.check(), "live": manager.live_count(),
                "checks": [{"id": key, "output": manager.check(key)} for key in manager._tasks],
                "ledger_after": sorted(path.name for path in manager._ledger_dir.glob("bg_*.json"))}
    async def one(name, command, limit, timeout):
        root=scratch/name; root.mkdir(parents=True)
        manager=BackgroundManager(root, default_timeout=3)
        with patch("mini_loop.background.MAX_BASH_CAPTURE", limit):
            started=manager.run(command, timeout)
            await joined(manager,"bg_0001")
        result={"name":name,"command":command,"limit":limit,"timeout":timeout,
                "started":started, **probe(manager), "done":manager.drain()}
        await manager.close()
        return result
    async def collect():
        rows=[await one(*spec) for spec in commands]
        root=scratch/"unrecorded";root.mkdir(parents=True);(root/".background").write_text("blocked")
        manager=BackgroundManager(root)
        started=manager.run("printf ok")
        await joined(manager,"bg_0001")
        unrecorded={"started":started,**probe(manager),"done":manager.drain()}
        await manager.close()
        root=scratch/"retention";root.mkdir(parents=True)
        manager=BackgroundManager(root,max_results_retained=2)
        starts=[]
        for i in range(5):
            starts.append(manager.run(f"printf result-{i}"))
            await joined(manager,f"bg_{i+1:04d}")
        retained={"starts":starts,**probe(manager),"done":manager.drain()}
        await manager.close()
        root=scratch/"cancel";root.mkdir(parents=True)
        manager=BackgroundManager(root)
        started=manager.run("sleep 30")
        for _ in range(1000):
            path=root/".background/bg_0001.json"
            if path.exists() and json.loads(path.read_text()).get("pid") is not None: break
            await asyncio.sleep(.005)
        else: raise AssertionError("source background PID never landed")
        await manager.close()
        cancelled={"started":started,**probe(manager),"done":manager.drain()}
        root=scratch/"prestart-cancel";root.mkdir(parents=True)
        manager=BackgroundManager(root)
        manager.run("sleep 30")
        await manager.close()
        prestart=probe(manager)
        seeds=[("bg_0002",'{"command":"old work","pid":null}'),
               ("bg_0005",'{"command":"live work","pid":<PID>}'),
               ("bg_0007",'broken'), ("bg_old",'{}'),
               ("bg_００１０",'{"command":"unicode counter","pid":null}')]
        root=scratch/"orphans[source-scope]";ledger=root/".background";ledger.mkdir(parents=True)
        for key,raw in seeds: (ledger/(key+".json")).write_text(raw.replace("<PID>",str(os.getpid())))
        manager=BackgroundManager(root)
        adopted={**probe(manager),"done":manager.drain()}
        next_started=manager.run("printf fresh")
        await joined(manager,"bg_0011")
        await manager.close()
        # Normalize only the currently live process identity.
        adopted=json.loads(json.dumps(adopted).replace(str(os.getpid()),"<PID>"))
        root=scratch/"notifications";ledger=root/".background";ledger.mkdir(parents=True)
        for i in range(53):
            (ledger/f"bg_{i+1:04d}.json").write_text(json.dumps({"command":f"cmd-{i}","pid":None}))
        manager=BackgroundManager(root)
        listing=manager.check()
        events=[]
        async def emit(event): events.append(event)
        agent=Agent(client=FakeAsyncAnthropic(),settings=Settings(fake_llm=True,workspace_root=root/"ws"),
                    workspace=root,state={"background":manager},emit=emit)
        messages=await background_injector(agent)
        batch={"listing":listing,"messages":messages,"events":events,"drained":manager.drain()}
        await manager.close()
        return {"commands":rows,"unrecorded":unrecorded,"retained":retained,"cancelled":cancelled,
                "prestart_cancel_source_gap":prestart,"orphan_seeds":[{"id":key,"raw":raw} for key,raw in seeds],
                "adopted":adopted,"next_started":next_started,"batch":batch}
    result=asyncio.run(collect())
    result["heuristics"]=[{"command":command,"explicit":explicit,"result":should_run_background(command,explicit)}
                          for command,explicit in [("echo ok",False),("echo ok",True),("npm INSTALL",False),
                           ("cargo build",False),("pytest",False),("echo TEST",False),("echo compile",False),
                           ("echo docker build",False),("echo make",False),("testing",False)]]
    # Pin the primitive used by source orphan-ID adoption to Python's Unicode version.
    import unicodedata
    zeros=[chr(value) for value in range(0x110000) if unicodedata.decimal(chr(value), -1) == 0]
    samples=["1"+zero+chr(ord(zero)+9) for zero in zeros]
    samples += ["", "+1", "-1", "1.0", "²", "①", "\U00011f50", "9"*80]
    counters=[]
    for digits in samples:
        try: value=str(int(digits)) if digits.isdigit() else None
        except ValueError: value=None
        counters.append({"digits":digits,"value":value})
    result["counter_digits"]={"unicode_version":unicodedata.unidata_version,"cases":counters}
    result["source_sha256"]={name:hashlib.sha256((PYTHON_ROOT/"mini_loop"/name).read_bytes()).hexdigest()
                             for name in ("background.py","tools.py")}
    return result


def _managed_worktree_contracts(scratch: Path) -> dict:
    """Actual managed factory allocation, ordinary delete and stop semantics."""
    import asyncio
    import subprocess
    from mini_loop import SessionManager, Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
    from mini_loop.worktrees import worktree_workspace_factory

    def git(repo, *args, required=True):
        result = subprocess.run(["git", *args], cwd=repo, capture_output=True, text=True, timeout=30)
        if required and result.returncode:
            raise RuntimeError(result.stderr)
        return result.returncode == 0, result.stdout

    cases = [
        ("clean-delete", "repo", "delete", False),
        ("dirty-delete", "repo", "delete", True),
        ("dirty-preserve", "repo", "preserve", True),
        ("dirty-stop", "repo", "stop", True),
        ("nonrepo-fallback", "nonrepo", "delete", False),
        ("unborn-fallback", "unborn", "delete", False),
        ("branch-conflict-fallback", "conflict", "delete", False),
    ]
    async def scenario(name, kind, action, dirty):
        root = (scratch / name).resolve()
        repo = root / "repo"
        repo.mkdir(parents=True)
        if kind != "nonrepo":
            git(repo, "init", "-b", "main")
            for key, value in {"user.name": "Go parity", "user.email": "parity@example.invalid",
                               "commit.gpgsign": "false", "core.hooksPath": "/dev/null", "core.autocrlf": "false"}.items():
                git(repo, "config", key, value)
            if kind != "unborn":
                (repo / ".gitignore").write_text(".worktrees/\n")
                git(repo, "add", ".gitignore"); git(repo, "commit", "-m", "base")
        source_factory = worktree_workspace_factory(repo)
        def factory(session_id):
            if kind == "conflict":
                git(repo, "branch", "wt/" + session_id)
            return source_factory(session_id)
        def responder(kwargs):
            if isinstance(kwargs["messages"][-1]["content"], str):
                return [tool("bash", _id="write", command="printf proof > proof.txt")], "tool_use"
            return [text("done")], "end_turn"
        manager = SessionManager(Settings(fake_llm=True, workspace_root=root / "ws"),
                                 FakeAsyncAnthropic(responder=responder, thinking=False), workspace_factory=factory)
        session = manager.create(owner="alice", permission_mode="auto")
        def state():
            ok, listing = git(repo, "worktree", "list", "--porcelain", required=False)
            branch, _ = git(repo, "show-ref", "--verify", "--quiet", "refs/heads/wt/" + session.id, required=False)
            marker = session.workspace / "proof.txt"
            return {"directory": session.workspace.is_dir(), "linked": (session.workspace / ".git").is_file(),
                    "registered": ok and "worktree " + str(session.workspace) + "\n" in listing,
                    "branch": branch, "proof": marker.read_text() if marker.exists() else None}
        initial = state()
        output = await session.run("write") if dirty else None
        before = state()
        if action == "stop":
            await manager.stop()
            removed = None
        else:
            removed = manager.delete(session.id, remove_workspace=action != "preserve")
            if manager._cleanup_tasks:
                await asyncio.gather(*tuple(manager._cleanup_tasks))
        after = state()
        await manager.stop()
        return {"name": name, "kind": kind, "action": action, "dirty": dirty,
                "workspace": str(session.workspace).replace(str(repo), "<REPO>").replace(session.id, "<SESSION>"),
                "bound": session.workspace_bound, "owner": session.owner,
                "initial": initial, "before": before, "after": after,
                "output": output, "removed": removed, "cleanup_errors": list(manager.cleanup_errors)}
    async def run(): return [await scenario(*case) for case in cases]
    return {"cases": asyncio.run(run())}


def _worktree_tool_contracts(scratch: Path) -> dict:
    """Installed source tools over an actual Agent, including execution rebind."""
    import asyncio
    import re
    import subprocess
    from mini_loop import SessionManager, Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.builtins import default_registry
    from mini_loop.registry import ToolContext, ToolRegistry
    from mini_loop.tasks import Task, TaskStore, install_tasks
    from mini_loop.worktrees import install_worktrees

    def git(repo, *args):
        proc = subprocess.run(["git", *args], cwd=repo, capture_output=True, text=True, timeout=30)
        if proc.returncode: raise RuntimeError(proc.stderr)

    schema_registry = install_worktrees(ToolRegistry())
    schemas = schema_registry.schemas()
    metadata = [{"name": t.name, "risk": t.risk, "readonly": t.readonly,
                 "parallel_safe": t.parallel_safe, "capabilities": sorted(t.capabilities)}
                for name in schema_registry.names() if (t := schema_registry.get(name)) is not None]
    variants = [
        {"name": "create_worktree", "input": {"name": "one", "task_id": "task_link"}},
        {"name": "create_worktree", "input": {"name": "one", "task_id": None}},
        {"name": "remove_worktree", "input": {"name": "one", "discard_changes": None}},
        {"name": "remove_worktree", "input": {"name": "one", "discard_changes": False}},
        {"name": "remove_worktree", "input": {"name": "é", "discard_changes": True}},
        {"name": "keep_worktree", "input": {"name": "one"}},
        {"name": "enter_worktree", "input": {"name": "one"}},
        {"name": "list_worktrees", "input": {}},
    ]
    for variant in variants:
        variant["canonical"] = json.dumps(variant["input"], sort_keys=True, ensure_ascii=False, separators=(",", ":"))
    available = [
        {"name": "create_worktree", "input": {"name": "one", "task_id": "task_link"}},
        {"name": "keep_worktree", "input": {"name": "one"}},
        {"name": "list_worktrees", "input": {}},
        {"name": "enter_worktree", "input": {"name": "missing"}},
        {"name": "enter_worktree", "input": {"name": "one"}},
        {"name": "write_file", "input": {"path": "proof.txt", "content": "entered\n"}},
        {"name": "read_file", "input": {"path": "proof.txt"}},
        {"name": "bash", "input": {"command": "pwd"}},
        {"name": "get_task", "input": {"task_id": "task_link"}},
        {"name": "remove_worktree", "input": {"name": "one"}},
        {"name": "create_worktree", "input": {"name": "two", "task_id": None}},
        {"name": "enter_worktree", "input": {"name": "two"}},
        {"name": "write_file", "input": {"path": "proof.txt", "content": "second 雪\n"}},
        {"name": "get_task", "input": {"task_id": "task_link"}},
        {"name": "keep_worktree", "input": {"name": "two"}},
    ]
    async def scenario(configured, late_board=False):
        root = scratch / ("late-board" if late_board else ("configured" if configured else "unconfigured"))
        repo = root / "repo"
        repo.mkdir(parents=True)
        git(repo, "init", "-b", "main")
        for key, value in {"user.name": "Go parity", "user.email": "parity@example.invalid",
                           "commit.gpgsign": "false", "core.hooksPath": "/dev/null", "core.autocrlf": "false"}.items():
            git(repo, "config", key, value)
        (repo / ".gitignore").write_text(".worktrees/\n.tasks/\n")
        git(repo, "add", ".gitignore"); git(repo, "commit", "-m", "base")
        registry = install_worktrees(install_tasks(default_registry()))
        from mini_loop.fake_llm import text as fake_text, tool, system_text
        child_results = []
        def responder(kwargs):
            child = system_text(kwargs).startswith("You are a worker subagent")
            last = kwargs["messages"][-1]["content"]
            if child and isinstance(last, str):
                return [tool("enter_worktree", _id="enter-child", name="one"),
                        tool("write_file", _id="write-child", path="child.txt", content="child"),
                        tool("bash", _id="pwd-child", command="pwd")], "tool_use"
            if child:
                child_results.extend(part["content"] for part in last if isinstance(part, dict) and part.get("type") == "tool_result")
                return [fake_text("child done")], "end_turn"
            if isinstance(last, str):
                return [tool("task", _id="delegate", prompt="child", agent_type="worker")], "tool_use"
            return [fake_text("parent done")], "end_turn"
        manager = SessionManager(Settings(fake_llm=True, workspace_root=root / "ws", repo_root=repo if configured else None),
                                 FakeAsyncAnthropic(responder=responder, thinking=False), tool_registry=registry)
        session = manager.create()
        board = TaskStore(session.workspace)
        board.save(Task("task_link", "linked task"))
        # Leave state["tasks"] absent: create_worktree must perform lazy admission.
        def norm(text):
            text = text.replace(str(session.workspace), "<WORKSPACE>").replace(str(repo.resolve()), "<REPO>").replace(str(repo), "<REPO>")
            return re.sub(r"(?m)(\s)[0-9a-f]{7,40}(\s)", r"\1<HEAD>\2", text)
        steps = available if configured else [{"name": name, "input": {} if name == "list_worktrees" else {"name": "one"}}
                                                for name in schema_registry.names()]
        if late_board:
            session.agent.state["worktrees"].create("one")
            steps = [
                {"name": "enter_worktree", "input": {"name": "one"}},
                {"name": "create_worktree", "input": {"name": "two"}},
                {"name": "get_task", "input": {"task_id": "task_link"}},
                {"name": "write_file", "input": {"path": "proof.txt", "content": "late board\n"}},
                {"name": "keep_worktree", "input": {"name": "two"}},
            ]
        outputs = []
        for step in steps:
            ctx = ToolContext(session.agent, session.agent.workspace, session.agent.state)
            text = await registry.get(step["name"]).run(ctx, **step["input"])
            text = str(text)  # Structured Bash results use the actual source rendering.
            if step["name"] == "list_worktrees": text = "\n".join(" ".join(line.split()) for line in norm(text).splitlines())
            outputs.append({**step, "output": norm(text), "execution": norm(str(session.agent.workspace)),
                            "lifecycle": norm(str(session.workspace)), "binding": board.load("task_link").worktree,
                            "board_initialized": "tasks" in session.agent.state})
        proof = {name: (repo / ".worktrees" / name / "proof.txt").read_text()
                 for name in ("one", "two") if (repo / ".worktrees" / name / "proof.txt").exists()}
        child = None
        if configured and not late_board:
            class AllTools:
                def select(self, role, registry): return registry
            session.agent.role_tool_policy = AllTools()
            output = await session.agent.run("delegate child")
            child = {"output": output, "results": [norm(str(value)) for value in child_results],
                     "execution": norm(str(session.agent.workspace)),
                     "summary": next(part["content"] for message in session.agent.messages
                                     if isinstance(message["content"], list) for part in message["content"]
                                     if isinstance(part, dict) and part.get("type") == "tool_result" and part.get("tool_use_id") == "delegate"),
                     "proof": (session.agent.workspace / "child.txt").read_text()}
        await manager.stop()
        return {"configured": configured, "steps": outputs, "proof": proof, "child": child}
    async def run(): return [await scenario(True), await scenario(False), await scenario(True, late_board=True)]
    return {"schemas": schemas, "metadata": metadata, "variants": variants, "cases": asyncio.run(run())}


def _worktree_contracts(scratch: Path) -> dict:
    """Actual Git lifecycle, task binding, audited effects and factory fallbacks."""
    import re
    import subprocess
    from mini_loop.tasks import Task, TaskStore
    from mini_loop.worktrees import WorktreeManager, worktree_workspace_factory

    def git(repo, *args):
        proc = subprocess.run(["git", *args], cwd=repo, capture_output=True, text=True, timeout=30)
        if proc.returncode:
            raise RuntimeError(proc.stderr)
        return proc.stdout.strip()

    def normalize(text, repo):
        text = text.replace(str(repo), "<REPO>")
        return re.sub(r"(?m)(\s)[0-9a-f]{7,40}(\s)", r"\1<HEAD>\2", text)

    specs = [
        {"name": "names", "git": True, "steps": [
            {"op": "create", "name": name} for name in ("", ".", "..", "../escape", "a/b", "é", "x" * 65)
        ] + [{"op": "create", "name": "A._-9"}, {"op": "remove", "name": "A._-9"}]},
        {"name": "not-repository", "git": False, "steps": [
            {"op": "create", "name": "one"}, {"op": "list"}, {"op": "keep", "name": "missing"},
            {"op": "remove", "name": "missing"}, {"op": "factory", "name": "../é/s1"},
            {"op": "factory", "name": ".."}, {"op": "factory", "name": ""},
        ]},
        {"name": "lifecycle-binding", "git": True, "steps": [
            {"op": "create", "name": "bad-link", "task_id": "task_missing"},
            {"op": "create", "name": "alpha", "task_id": "task_link"},
            {"op": "create", "name": "alpha"}, {"op": "changes", "name": "alpha"},
            {"op": "keep", "name": "alpha"}, {"op": "list"},
            {"op": "remove", "name": "alpha"}, {"op": "remove", "name": "alpha"},
        ]},
        {"name": "dirty-and-ahead", "git": True, "steps": [
            {"op": "create", "name": "dirty"},
            {"op": "write", "name": "dirty", "file": "new.txt", "text": "untracked\n"},
            {"op": "remove", "name": "dirty"}, {"op": "keep", "name": "dirty"},
            {"op": "commit", "name": "dirty"}, {"op": "changes", "name": "dirty"},
            {"op": "remove", "name": "dirty"},
            {"op": "write", "name": "dirty", "file": "tracked.txt", "text": "modified\n"},
            {"op": "changes", "name": "dirty"}, {"op": "remove", "name": "dirty"},
            {"op": "remove", "name": "dirty", "discard": True},
        ]},
        {"name": "unverified-status", "git": False, "steps": [
            {"op": "mkdir", "name": "plain"}, {"op": "changes", "name": "plain"},
            {"op": "remove", "name": "plain"}, {"op": "remove", "name": "plain", "discard": True},
        ]},
        {"name": "factory", "git": True, "steps": [
            {"op": "factory", "name": "s1"}, {"op": "factory", "name": "s1"},
            {"op": "factory", "name": ".."}, {"op": "factory", "name": "../é/s2"},
            {"op": "factory", "name": "x" * 70}, {"op": "list"},
            {"op": "branch", "name": "collision"}, {"op": "factory", "name": "collision"},
            {"op": "remove", "name": "collision"},
        ]},
        {"name": "custom-location", "git": True, "base": "trees", "prefix": "review/", "steps": [
            {"op": "create", "name": "custom", "task_id": "task_link"},
            {"op": "keep", "name": "custom"}, {"op": "list"}, {"op": "remove", "name": "custom"},
        ]},
        {"name": "unborn", "git": True, "unborn": True, "steps": [
            {"op": "factory", "name": "empty"}, {"op": "create", "name": "fresh"},
        ]},
        {"name": "empty-prefix-base", "git": True, "base": "", "prefix": "", "steps": [
            {"op": "create", "name": "direct"}, {"op": "keep", "name": "direct"},
            {"op": "list"}, {"op": "remove", "name": "direct"},
        ]},
    ]
    cases = []
    for spec in specs:
        repo = scratch / spec["name"]
        repo.mkdir(parents=True)
        if spec["git"]:
            git(repo, "init", "-b", "main")
            for key, value in {"user.name": "Go parity", "user.email": "parity@example.invalid",
                               "commit.gpgsign": "false", "core.hooksPath": "/dev/null",
                               "core.autocrlf": "false"}.items():
                git(repo, "config", key, value)
            (repo / ".gitignore").write_text(".worktrees/\ntrees/\n.tasks/\n")
            (repo / "tracked.txt").write_text("base\n")
            if not spec.get("unborn"):
                git(repo, "add", ".gitignore", "tracked.txt")
                git(repo, "commit", "-m", "base")
        manager = WorktreeManager(repo, base=spec.get("base", ".worktrees"), branch_prefix=spec.get("prefix", "wt/"))
        board = TaskStore(repo)
        board.save(Task("task_link", "linked task"))
        factory = worktree_workspace_factory(repo, base=manager.base, branch_prefix=manager.branch_prefix)
        steps = []
        for step in spec["steps"]:
            name, op = step.get("name", ""), step["op"]
            path = manager.root / name
            value = None
            if op == "create": value = manager.create(name, task_id=step.get("task_id", ""), task_store=board)
            elif op == "remove": value = manager.remove(name, discard_changes=step.get("discard", False))
            elif op == "keep": value = manager.keep(name)
            elif op == "list":
                # Git list column alignment depends on the random root length.
                value = [" ".join(normalize(line, repo).split()) for line in manager.list().splitlines()]
            elif op == "changes": value = list(manager._changes(name))
            elif op == "factory": value = str(factory(name))
            elif op == "mkdir": path.mkdir(parents=True)
            elif op == "write": (path / step["file"]).write_text(step["text"])
            elif op == "commit": git(path, "add", "."); git(path, "commit", "-m", "work")
            elif op == "branch": git(repo, "branch", manager.branch_prefix + name)
            else: raise ValueError(op)
            if isinstance(value, str): value = normalize(value, repo)
            events = []
            if manager.events_path.exists():
                for line in manager.events_path.read_text().splitlines():
                    event = json.loads(line)
                    event.pop("ts")  # Nondeterministic clock, not lifecycle semantics.
                    events.append(event)
            linked = board.load("task_link")
            branch = subprocess.run(["git", "rev-parse", "--verify", "refs/heads/" + manager.branch_prefix + name],
                                    cwd=repo, capture_output=True, timeout=30).returncode == 0
            steps.append({"input": step, "value": value, "events": events,
                          "path_exists": path.exists(), "branch_exists": branch, "binding": linked.worktree})
        cases.append({**{key: value for key, value in spec.items() if key != "steps"}, "steps": steps})
    return {"cases": cases, "clock_normalized": True, "git_heads_normalized": True}


def _task_contracts(scratch: Path) -> dict:
    """Actual persistent task transitions, privacy, bounded views and owned HTTP."""
    import asyncio
    from dataclasses import asdict
    from unittest.mock import patch
    from mini_loop.tasks import TaskStore, Task, install_tasks
    from mini_loop.registry import ToolRegistry
    from mini_loop.secrets import SecretRegistry
    from fastapi.testclient import TestClient
    from mini_loop.auth import TokenAuth
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app
    secret = 'clé-secrète-"café"\\Ω-0123456789'
    specifications = [
        ('flow', [
            {'op':'render'}, {'op':'create','subject':'write the report'},
            {'op':'create','subject':'review it','dependencies':['task_000000000001']},
            {'op':'claim','id':'task_000000000002','owner':'alice'},
            {'op':'complete','id':'task_000000000001','owner':'alice'},
            {'op':'claim','id':'task_000000000001','owner':'alice'},
            {'op':'claim','id':'task_000000000001','owner':'bob'},
            {'op':'complete','id':'task_000000000001','owner':'bob'},
            {'op':'bind','id':'task_000000000002','name':'review-branch'},
            {'op':'complete','id':'task_000000000001','owner':'alice'},
            {'op':'can_start','id':'task_000000000002'}, {'op':'runnable'},
            {'op':'claim','id':'task_000000000002','owner':'alice'},
            {'op':'complete','id':'task_000000000002'}, {'op':'render'},
            {'op':'list'}, {'op':'load','id':'task_000000000002'},
            {'op':'claim','id':'task_000000000001','owner':'alice'},
            {'op':'load','id':'task_missing'}, {'op':'can_start','id':'task_missing'},
            {'op':'complete','id':'task_missing'}, {'op':'claim','id':'task_missing','owner':'a'},
            {'op':'bind','id':'task_missing','name':'valid'}]),
        ('missing', [ {'op':'create','subject':'forward','dependencies':['task_later']},
            {'op':'render'}, {'op':'runnable'}, {'op':'render'}]),
        ('validation', [ {'op':'create','subject':'bad','dependencies':['../escape']},
            {'op':'create','subject':'bad','worktree':'..'},
            {'op':'create','subject':'bad','worktree':'bad/name'},
            {'op':'load','id':'../escape'}, {'op':'create','subject':'empty tree','worktree':''},
            {'op':'bind','id':'task_000000000001','name':'.'},
            {'op':'bind','id':'task_000000000001','name':'x'*65}, {'op':'list'}]),
        ('crash', [ {'op':'create','subject':'work'},
            {'op':'marker','id':'task_000000000001','text':'ghost'},
            {'op':'claim','id':'task_000000000001','owner':'bob'}, {'op':'load','id':'task_000000000001'}]),
        ('corrupt', [ {'op':'file','name':'task_broken.json','text':'{unfinished'},
            {'op':'file','name':'task_wrong.json','text':'{"id":"task_wrong"}'},
            {'op':'file','name':'other.json','text':'{unfinished'}, {'op':'list'}, {'op':'render'}]),
        ('bounded', [ {'op':'batch','count':55,'subject':'plain'},
            {'op':'create','subject':'界'*17000,'description':'D'*17000},
            {'op':'render'}, {'op':'load','id':'task_000000000056'}]),
        ('masked', [ {'op':'create','subject':'use '+secret,'description':secret},
            {'op':'load','id':'task_000000000001'}, {'op':'claim','id':'task_000000000001','owner':secret},
            {'op':'load','id':'task_000000000001'}, {'op':'render'}]),
    ]
    cases=[]
    for name, specs in specifications:
        counter=[0]
        def new_id(self):
            counter[0]+=1
            return f'task_{counter[0]:012d}'
        secrets=SecretRegistry() if name=='masked' else None
        if secrets is not None: secrets.register('KEY',secret)
        store=TaskStore(scratch/name,secrets=secrets)
        steps=[]
        with patch.object(TaskStore,'_new_id',new_id):
            for spec in specs:
                value=None;error=None
                try:
                    op=spec['op'];tid=spec.get('id')
                    if op=='create':value=asdict(store.create(spec['subject'],spec.get('description',''),spec.get('dependencies'),spec.get('worktree')))
                    elif op=='claim':value=store.claim(tid,spec['owner'])
                    elif op=='complete':value=store.complete(tid,spec.get('owner'))
                    elif op=='bind':value=store.bind_worktree(tid,spec['name'])
                    elif op=='load':
                        task=store.load(tid);value=asdict(task) if task else None
                    elif op=='list':value=[asdict(task) for task in store.list()]
                    elif op=='runnable':value=[asdict(task) for task in store.runnable()]
                    elif op=='can_start':value=store.can_start(tid)
                    elif op=='render':value=store.render()
                    elif op=='file':(store.dir/spec['name']).write_text(spec['text'])
                    elif op=='marker':store._marker(tid).write_text(spec['text'])
                    elif op=='batch':
                        for _ in range(spec['count']):store.create(spec['subject'])
                except (ValueError, OSError) as exc:error=str(exc)
                steps.append({'input':spec,'value':value,'error':error,'problems':store.problems.summary(),
                              'total':store.problems.total(),'dropped':store.problems.dropped})
        cases.append({'name':name,'secret':secret if secrets is not None else None,'steps':steps})
    registry=install_tasks(ToolRegistry())
    from mini_loop.registry import ToolContext
    from types import SimpleNamespace
    tool_steps=[]
    context=ToolContext(SimpleNamespace(label='main',secrets=None),scratch/'tools',{})
    with patch.object(TaskStore,'_new_id',lambda self:'task_000000000001'):
        for name,arguments in [('create_task',{'subject':'tool work','description':'details','blockedBy':None,'worktree':None}),
                               ('list_tasks',{}),('get_task',{'task_id':'task_000000000001'}),
                               ('claim_task',{'task_id':'task_000000000001'}),
                               ('complete_task',{'task_id':'task_000000000001'}),
                               ('get_task',{'task_id':'task_000000000001'})]:
            tool_steps.append({'name':name,'input':arguments,'output':asyncio.run(registry.get(name).run(context,**arguments))})
    schemas=registry.schemas()
    settings=Settings(fake_llm=True,enable_features=False,workspace_root=scratch/'http',skills_dir=scratch/'skills',spill_dir=None)
    manager=SessionManager(settings,FakeAsyncAnthropic())
    app=create_app(settings=settings,manager=manager)
    http_cases=[];seed=[]
    with TestClient(app) as client:
        app.state.auth=TokenAuth({'token-a':'alice','token-b':'bob'})
        sid=client.post('/sessions',json={},headers={'Authorization':'Bearer token-a'}).json()['id']
        def request(name,session,token):
            response=client.get(f'/sessions/{session}/tasks',headers={'Authorization':'Bearer '+token} if token else {})
            value=response.json()
            if 'session' in value:value['session']='session-fixture'
            if isinstance(value,dict) and isinstance(value.get('detail'),str):value['detail']=value['detail'].replace(sid,'session-fixture')
            http_cases.append({'name':name,'session':'session-fixture' if session==sid else session,'token':token,'status':response.status_code,'body':value})
        request('empty',sid,'token-a')
        board=TaskStore(manager._sessions[sid].workspace)
        first=Task('task_first','write report');second=Task('task_second','review',blockedBy=[first.id])
        board.save(first);board.save(second);board.claim(first.id,'alice')
        seed=[asdict(task) for task in board.list()]
        request('owned',sid,'token-a');request('foreign',sid,'token-b');request('missing','missing','token-a');request('unauthenticated',sid,'')
    return {'cases':cases,'tools':tool_steps,'schemas':schemas,'metadata':[{'name':name,'readonly':registry.get(name).readonly,'risk':registry.get(name).risk,'capabilities':sorted(registry.get(name).capabilities)} for name in registry.names()],
            'http':{'seed':seed,'cases':http_cases}}


def _webui_contracts(scratch: Path) -> dict:
    """Actual public browser shells, protected data and immutable asset identities."""
    import hashlib
    from fastapi.testclient import TestClient
    from mini_loop.auth import TokenAuth
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app, CONSOLE_HTML, SECURITY_HEADERS
    from mini_loop.webui import render_page

    settings = Settings(fake_llm=True, trajectory_enabled=False, enable_features=False,
                        workspace_root=scratch, skills_dir=scratch / "empty-skills",
                        spill_dir=None)
    manager = SessionManager(settings, FakeAsyncAnthropic())
    app = create_app(settings=settings, manager=manager)
    cases = []
    with TestClient(app) as client:
        app.state.auth = TokenAuth({"token-a": "alice", "token-b": "bob"})
        for method, path, token in (
            ("GET", "/", ""), ("GET", "/ui", ""),
            ("GET", "/", "wrong"), ("GET", "/ui", "wrong"),
            ("GET", "/", "token-a"), ("GET", "/ui", "token-b"),
            ("POST", "/", ""), ("POST", "/ui", ""),
            ("HEAD", "/", ""), ("HEAD", "/ui", ""),
            ("GET", "/sessions", ""), ("GET", "/sessions", "wrong"),
            ("GET", "/sessions?access_token=token-a", ""),
            ("GET", "/ui/app.js", ""), ("GET", "/ui/app.js", "token-a"),
            ("GET", "/favicon.ico", ""),
        ):
            response = client.request(method, path,
                                      headers={"Authorization": "Bearer " + token} if token else {})
            cases.append({"method": method, "path": path, "token": token,
                          "status": response.status_code,
                          "content_type": response.headers.get("content-type", ""),
                          "body_sha256": hashlib.sha256(response.content).hexdigest(),
                          "body": response.text if response.status_code != 200 else None,
                          "allow": response.headers.get("allow", ""),
                          "headers": {name: response.headers.get(name, "") for name in SECURITY_HEADERS}})
    assets = {name: (PYTHON_ROOT / "mini_loop" / "webui" / name).read_bytes()
              for name in ("index.html", "app.css", "app.js")}
    assets["console.html"] = CONSOLE_HTML.encode()
    return {"assets": {name: {"sha256": hashlib.sha256(body).hexdigest(), "bytes": len(body)}
                       for name, body in assets.items()},
            "ui_sha256": hashlib.sha256(render_page().encode()).hexdigest(),
            "cases": cases}


def _trace_view_contracts(scratch: Path) -> dict:
    """Execute the existing ledger fold, HTML renderer, file CLI reader and iterator."""
    import hashlib
    from unittest.mock import patch
    from types import SimpleNamespace
    from mini_loop import trace_view
    from mini_loop.trajectory import TrajectoryStore
    scratch.mkdir(parents=True, exist_ok=True)
    injection = "<script>alert('unsafe')</script>&\"你好🙂"
    base = {"trajectory_id":"traj_fixture", "session":"session-fixture", "run_index":2,
            "status":"completed", "started_at":1000.0, "ended_at":1010.0,
            "duration_ms":10000.0, "input":"inspect " + injection, "output":"done", "error":None,
            "metrics":{"model_calls":1,"tool_calls":1,"tool_errors":0,"errors":0}, "partial":False}
    def start(span="m1", seq=1, **values):
        return {"type":"model_start", "seq":seq,"ts":1000.5,"span_id":span,
                "purpose":"agent_turn","model":"requested","message_count":2,"tool_count":10,"max_tokens":8000,**values}
    def end(span="m1", seq=2, **values):
        return {"type":"model_end","seq":seq,"ts":1001.0,"span_id":span,"status":"completed",
                "duration_ms":500.0,"stop_reason":"end_turn","usage":{"input_tokens":1234,"output_tokens":12},**values}
    def tool(span="t1", **values):
        return {"type":"tool_use","seq":3,"ts":1001.0,"span_id":span,"name":"bash","id":"call-1","input":{"command":"echo " + injection},**values}
    def result(span="t1", **values):
        return {"type":"tool_result","seq":4,"ts":1001.25,"span_id":span,"output":injection,"duration_ms":250.0,"error":False,"denied":False,**values}
    scenarios = [
        ("complete",[start(),end(served_model="actual",model_output=[{"type":"text","text":injection}]),tool(),result(command_result={"exit_code":0},replayed=True),{"type":"assistant_text","seq":5,"ts":1002.0,"text":injection},{"type":"done","text":"mirrored"},{"type":"trajectory_end"}],{}),
        ("open",[start(),tool()],{"status":"interrupted","ended_at":None,"duration_ms":None,"output":None,"partial":True}),
        ("denied",[tool(),result(denied=True)],{}),
        ("failed",[start(),end(status="error",error=injection),tool(),result(error=True)],{"status":"error","error":injection}),
        ("nested-requests",[start(),start("child",2,agent="main>explore",depth=1),tool(agent="main>explore",depth=1),start("compact",5,purpose="compaction_summary"),end("child",6),result(),{"type":"steering_delivered","seq":7,"ts":1003.0,"text":injection,"count":2}],{}),
        ("references",[{"type":"tool_catalog","seq":1,"ts":1000.2,"schemas":[{"name":f"tool_{i}","input_schema":{"type":"object"}} for i in range(40)],"fingerprint":"catalog-hash"},{"type":"system_prompt","seq":2,"ts":1000.3,"hash":"system-hash","text":"You are an agent. "*200},{"type":"capability_plan","seq":3,"ts":1000.4,"permission_mode":"auto","sandbox_confined":False,"fingerprint":"plan-hash"}],{}),
        ("unknown-and-orphans",[{"type":"future-event","seq":1,"ts":1000.5,"nested":{"secret":injection},"flag":True},end("missing"),result("missing"),{"type":"done","seq":5,"text":injection},{"type":"compact","seq":6,"kind":"failed","messages_removed":12}],{"output":None}),
        ("unicode-caps",[{"type":"assistant_text","seq":1,"ts":1001.0,"text":"你好🙂\x1c"*6000}],{}),
        ("tail-cap",[start(),end(),start("m2",3),end("m2",4),*[{"type":"assistant_text","seq":i+10,"ts":1002.0+i/1000,"text":f"row {i}"} for i in range(2010)]],{}),
        ("numeric-and-whitespace",[{"type":"future-number","seq":9007199254740993,"ts":1000.0,"values":[1000000.0,1e-5,1e16,-0.0,1.234567890123456],"text":"a\x1cb\x1dc\x1ed\x1ff"}],{}),
        ("empty-and-null",[{"type":"model_start","span_id":"m1"},{"type":"model_end","span_id":"m1"},{"type":"tool_use","span_id":"t1"},{"type":"tool_result","span_id":"t1"},{"type":None}],{"input":None,"output":None,"metrics":{"input_tokens":999,"output_tokens":999}}),
    ]
    cases=[]
    for name,events,over in scenarios:
        # The identical serialized input preserves object key order for both renderers.
        document=json.loads(json.dumps({**base,"events":events,**over},ensure_ascii=False,sort_keys=True))
        ledger=trace_view.build_ledger(document)
        with patch("mini_loop.trace_view.time.strftime",return_value="2000-01-02 03:04:05"):
            page=trace_view.render_html([ledger],title="mini-loop trace · fixture")
        cases.append({"name":name,"document":document,"page":page,
                      "row_count":len(ledger["rows"]),"omitted":ledger["omitted"],"metrics":ledger["metrics"],
                      "kinds":[row["kind"] for row in ledger["rows"]],"ended_at":ledger["ended_at"]})
    raw='\n'.join(json.dumps(record,ensure_ascii=False) for record in [
        {"record_type":"trajectory_start","trajectory_id":"traj_fixture","session":"session-fixture","run_index":1,"started_at":1000.0,"input":injection},
        {"record_type":"event",**start()}, {"record_type":"event",**end()},
        {"record_type":"event","type":"assistant_text","seq":3,"ts":1001.5,"text":injection},
        {"record_type":"trajectory_end","status":"completed","ended_at":1002.0,"output":"final","duration_ms":2000.0,"metrics":{"model_calls":1}},
        {"record_type":"trajectory_end","status":"cancelled","ended_at":1003.0,"output":"last-end","duration_ms":3000.0,"metrics":{"model_calls":1}},
    ])+'\n\n{"broken":\n'
    exported=scratch/"copied.jsonl";exported.write_text(raw)
    assembled=trace_view.assemble_file(exported)
    with patch("mini_loop.trace_view.time.strftime",return_value="2000-01-02 03:04:05"):
        file_page=trace_view.render_html([trace_view.build_ledger(assembled)],title="mini-loop trace · fixture")
    store=TrajectoryStore(scratch/"iterator")
    with patch("mini_loop.trajectory.uuid.uuid4",return_value=SimpleNamespace(hex="a"*32)), patch("mini_loop.trajectory.time.time",return_value=1000.0):
        tid=store.start(session_id="session-fixture",run_index=1,input_text="input")
    for event in ({"type":"model_start","seq":1},{"type":"tool_use","seq":2},{"type":"assistant_text","seq":3}):store.append(tid,event)
    with store._path(tid).open("a") as handle:handle.write('{"broken":\n')
    iterator=[]
    for types,limit in ((None,10), (None,2), ([],10), (["model_start","assistant_text"],10), (["tool_use"],1), (["missing"],1), ([""],10), (None,0), (None,-1)):
        iterator.append({"types":types,"limit":limit,"records":list(store.iter_events(tid,types=None if types is None else set(types),limit=limit))})
    from fastapi.testclient import TestClient
    from mini_loop import Settings, SessionManager
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.server import create_app
    cfg=Settings(fake_llm=True,trajectory_enabled=True,workspace_root=scratch/"http-ws",trajectory_root=scratch/"http-traces")
    manager=SessionManager(cfg,FakeAsyncAnthropic())
    with patch("mini_loop.trajectory.uuid.uuid4",return_value=SimpleNamespace(hex="c"*32)), patch("mini_loop.trajectory.time.time",return_value=1000.0):
        http_id=manager.trajectories.start(session_id="session-fixture",run_index=1,owner="alice",input_text=injection)
    for event in scenarios[0][1]: manager.trajectories.append(http_id,event)
    with patch("mini_loop.trajectory.time.time",return_value=1010.0): manager.trajectories.finish(http_id,status="completed",output="done",duration_ms=10000.0)
    http_seed=manager.trajectories.raw(http_id)
    http_cases=[]
    with patch.dict(os.environ,{"MINILOOP_API_TOKENS":"alice:token-a,bob:token-b","MINILOOP_API_TOKEN":""}), patch("mini_loop.trace_view.time.strftime",return_value="2000-01-02 03:04:05"):
        with TestClient(create_app(settings=cfg,manager=manager)) as client:
            for name,path,token in (("owner",f"/trajectories/{http_id}/view","token-a"),("foreign",f"/trajectories/{http_id}/view","token-b"),("invalid","/trajectories/traj_bad/view","token-a")):
                response=client.get(path,headers={"Authorization":"Bearer "+token})
                http_cases.append({"name":name,"path":path,"token":token,"status":response.status_code,"body":response.text,"content_type":response.headers.get("content-type"),"csp":response.headers.get("content-security-policy")})
            with manager.trajectories._path(http_id).open("a") as handle: handle.write("x"*8388608+"\n")
            response=client.get(f"/trajectories/{http_id}/view",headers={"Authorization":"Bearer token-a"})
            http_cases.append({"name":"oversized","path":f"/trajectories/{http_id}/view","token":"token-a","status":response.status_code,"body":response.text,"content_type":response.headers.get("content-type"),"csp":response.headers.get("content-security-policy")})
    return {"source_sha256":{name:hashlib.sha256((REPO_ROOT/"python/mini_loop"/name).read_bytes()).hexdigest() for name in ("trace_view.py","trajectory.py","server.py")},"css_sha256":hashlib.sha256(trace_view._CSS.encode()).hexdigest(),"js_sha256":hashlib.sha256(trace_view._FILTER_JS.encode()).hexdigest(),"cases":cases,"file":{"raw":raw,"page":file_page},"iterator":{"id":tid,"raw":store.raw(tid),"cases":iterator},"http":{"id":http_id,"seed":http_seed,"cases":http_cases}}


def _spill_contracts(scratch: Path) -> dict:
    """Pin actual private-store behavior and both Bash projection entry points."""
    import asyncio
    import hashlib
    import stat
    from unittest.mock import patch
    from mini_loop.spill import LocalSpillStore, MAX_SPILL_BYTES
    from mini_loop.tools import Toolset, CommandResult
    from mini_loop.secrets import SecretRegistry
    from mini_loop import Settings, SessionManager
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool

    scratch.mkdir(parents=True, exist_ok=True)
    scratch = scratch.resolve()
    token = "feedfacedeadbeef"
    stores = []
    recipes = [
        ("plain", "s1", "bash.txt", "already masked", 1),
        ("empty", "", "", "", 1),
        ("unicode", "会话🙂", "世界/🙂.txt", "你好🙂\nline\r\n", 7),
        ("traversal", "../../session", "../../evil/../result.txt", "safe", 1),
        ("absolute-hint", "/absolute/session", "/tmp/evil.txt", "safe", 1),
        ("hidden", "s1", ".hidden", "safe", 1),
        ("dots", "s1", "...", "safe", 1),
        ("embedded-dots", "s1", "a...b..c.txt", "safe", 1),
        ("long-hint", "s1", "A" * 100 + ".txt", "safe", 1),
        ("shell-hint", "s1", "$(command)\n;quoted'name.txt", "safe", 1),
        ("byte-ceiling", "s1", "bash.txt", "x", MAX_SPILL_BYTES),
        ("byte-overflow", "s1", "bash.txt", "x", MAX_SPILL_BYTES + 1),
        ("unicode-byte-ceiling", "s1", "bash.txt", "🙂", MAX_SPILL_BYTES // 4),
        ("unicode-byte-overflow", "s1", "bash.txt", "🙂", MAX_SPILL_BYTES // 4 + 1),
    ]
    for name, namespace, suggestion, pattern, repeat in recipes:
        root = scratch / name
        root.mkdir()
        root.chmod(0o777)  # constructor must tighten an inherited root
        store = LocalSpillStore(root)
        content = pattern * repeat
        result = None
        error = None
        with patch("mini_loop.spill._secrets.token_hex", return_value=token):
            try:
                ref = store.save_text(session_id=namespace, tool_name="bash", label="output", suggested_name=suggestion, content=content)
                artifact = Path(ref.locator)
                saved = artifact.read_bytes()
                result = {"ref": {"locator": ref.locator.replace(str(root), "<store>"), "bytes": ref.bytes, "retrieval_hint": ref.retrieval_hint.replace(str(root), "<store>")}, "sha256": hashlib.sha256(saved).hexdigest(), "root_mode": stat.S_IMODE(root.stat().st_mode), "namespace_mode": stat.S_IMODE(artifact.parent.stat().st_mode), "file_mode": stat.S_IMODE(artifact.stat().st_mode)}
            except (ValueError, OSError) as exc:
                error = type(exc).__name__
        stores.append({"name":name, "namespace":namespace, "suggestion":suggestion, "pattern":pattern, "repeat":repeat, "result":result, "error":error})

    collision_store = LocalSpillStore(scratch / "collision")
    with patch("mini_loop.spill._secrets.token_hex", return_value=token):
        first = collision_store.save_text(session_id="s1", tool_name="bash", label="output", suggested_name="bash.txt", content="first")
        try:
            collision_store.save_text(session_id="s1", tool_name="bash", label="output", suggested_name="bash.txt", content="second")
        except FileExistsError:
            collision = {"refused":True, "original":Path(first.locator).read_text()}
        victim = scratch / "victim.txt"
        victim.write_text("original")
        Path(first.locator).unlink()
        Path(first.locator).symlink_to(victim)
        try:
            collision_store.save_text(session_id="s1", tool_name="bash", label="output", suggested_name="bash.txt", content="attacker")
        except FileExistsError:
            symlink = {"refused":True, "victim":victim.read_text()}

    class BrokenStore:
        def save_text(self, **kwargs):
            raise OSError("fixture store failure")

    bash = []
    long_command = "awk 'BEGIN {for(i=0;i<60000;i++)printf \"A\";printf \"TAIL\"}'"
    for name, command, enabled, broken, secret in [
        ("short", "printf 'short'", True, False, False),
        ("exact-cap", "awk 'BEGIN {for(i=0;i<50000;i++)printf \"A\"}'", True, False, False),
        ("oversized", long_command, True, False, False),
        ("unicode", "awk 'BEGIN {for(i=0;i<15001;i++)printf \"你好🙂é\";printf \"TAIL\"}'", True, False, False),
        ("nonzero", long_command + "; exit 7", True, False, False),
        ("without-store", long_command, False, False, False),
        ("failing-store", long_command, True, True, False),
        ("split-secret", long_command + "; printf 'fixture-'; printf 'secret' >&2", True, False, True),
    ]:
        root = scratch / ("bash-" + name)
        store = LocalSpillStore(root) if enabled and not broken else (BrokenStore() if broken else None)
        registry = SecretRegistry()
        if secret:
            registry.register("DEMO", "fixture-secret")
        toolset = Toolset(scratch / ("ws-" + name), spill=store, secrets=registry)
        rendered = toolset.run_bash(command)
        artifacts = list(root.rglob("*.txt")) if root.exists() else []
        preserved = []
        for path in artifacts:
            raw = path.read_bytes()
            rendered = rendered.replace(str(path), "<artifact>")
            preserved.append({"sha256":hashlib.sha256(raw).hexdigest(), "bytes":len(raw)})
        bash.append({"name":name, "command":command, "enabled":enabled, "broken":broken, "secret":secret, "render_sha256":hashlib.sha256(rendered.encode()).hexdigest(), "render_chars":len(rendered), "preserved":preserved})

    # This executes the actual default tool adapter inside a managed turn. The
    # structured path currently bypasses run_bash's preservation policy.
    def responder(request):
        if request["messages"][-1]["role"] == "user" and isinstance(request["messages"][-1]["content"], str):
            return [tool("bash", _id="spill-tool", command=long_command)], "tool_use"
        return [text("done")], "end_turn"
    manager = SessionManager(Settings(fake_llm=True, trajectory_enabled=False, workspace_root=scratch / "managed-ws", spill_dir=scratch / "managed-spill"), FakeAsyncAnthropic(responder=responder))
    session = manager.create(permission_mode="auto")
    final = asyncio.run(session.run("spill probe"))
    result_text = session.agent.messages[-2]["content"][0]["content"]
    default_adapter = {"command":long_command, "final":final, "render_sha256":hashlib.sha256(result_text.encode()).hexdigest(), "render_chars":len(result_text), "preserved":len(list(manager.spill.root.rglob("*.txt")))}

    projections = []
    for name, stdout, stderr, error, projection in [
        ("timeout-projection", "B" * 60000, "tail", "Error: Timeout (1s)", "B" * 60000 + "tail"),
        ("error-without-projection", "B" * 60000, "", "Error: failed", None),
        ("strip-before-save", "\x1c " + "B" * 60000 + " \x1f", "", None, None),
    ]:
        root = scratch / ("projection-" + name)
        store = LocalSpillStore(root)
        toolset = Toolset(scratch / ("projection-ws-" + name), spill=store)
        result = CommandResult(stdout, stderr, 0, error is not None, False, 0, error, projection)
        with patch.object(toolset, "run_bash_result", return_value=result):
            rendered = toolset.run_bash("ignored fixture command")
        artifacts = list(root.rglob("*.txt"))
        hashes = []
        for path in artifacts:
            raw = path.read_bytes()
            rendered = rendered.replace(str(path), "<artifact>")
            hashes.append({"bytes":len(raw), "sha256":hashlib.sha256(raw).hexdigest()})
        projections.append({"name":name, "stdout_prefix":"\x1c " if name == "strip-before-save" else "", "stdout_pattern":"B", "stdout_repeat":60000, "stdout_suffix":" \x1f" if name == "strip-before-save" else "", "stderr":stderr, "error":error, "projection":projection is not None, "render_sha256":hashlib.sha256(rendered.encode()).hexdigest(), "render_chars":len(rendered), "preserved":hashes})

    manager_cases = []
    for name in ("enabled", "disabled", "root-is-file"):
        root = scratch / ("manager-" + name)
        if name == "root-is-file":
            root.write_text("not a directory")
        manager = SessionManager(Settings(fake_llm=True, trajectory_enabled=False, workspace_root=scratch / ("manager-ws-" + name), spill_dir=None if name == "disabled" else root), FakeAsyncAnthropic())
        manager_cases.append({"name":name, "available":manager.spill is not None})

    return {"source_sha256":{name:hashlib.sha256((REPO_ROOT / "python/mini_loop" / name).read_bytes()).hexdigest() for name in ("spill.py", "tools.py", "builtins.py", "manager.py")}, "max_bytes":MAX_SPILL_BYTES, "token":token, "stores":stores, "collision":collision, "leaf_symlink":symlink, "bash":bash, "projections":projections, "default_adapter":default_adapter, "managers":manager_cases}


def _configuration_contracts(scratch: Path) -> dict:
    """Execute actual Settings environment factories, validation and builtin skills."""
    import dataclasses
    import hashlib
    from unittest.mock import patch
    from mini_loop.config import Settings
    from mini_loop.skills import SkillLoader

    scratch.mkdir(parents=True, exist_ok=True)
    scratch = scratch.resolve()
    cases = [
        ("defaults", {}),
        ("core-overrides", {"MODEL_ID":"source-model","ANTHROPIC_API_KEY":"fixture-secret-key","ANTHROPIC_BASE_URL":"https://provider.invalid/anthropic", "MINILOOP_MAX_TOKENS":"1_024","MINILOOP_TOKEN_THRESHOLD":"5000","MINILOOP_MAX_CONCURRENT_LLM":"2","MINILOOP_MAX_CONCURRENT_TOOLS":"3","MINILOOP_MAX_TURNS":"4","MINILOOP_SUBAGENT_MAX_ROUNDS":"5","MINILOOP_SUBAGENT_MAX_DEPTH":"1","MINILOOP_BASH_TIMEOUT":"30","MINILOOP_APPROVAL_TIMEOUT":"45","MINILOOP_RATE_LIMIT_PER_MINUTE":"6","MINILOOP_FALLBACK_MODEL":"backup","MINILOOP_WORKSPACE_ROOT":"nested/work","MINILOOP_BINDABLE_ROOTS":"one: :../two","MINILOOP_SPILL_DIR":"", "MINILOOP_TRAJECTORIES":"off", "MINILOOP_FAKE_LLM":"1"}),
        ("optional-settings", {"MINILOOP_TOKEN_EFFICIENCY_MODE":" Enforce ","MINILOOP_TOKEN_EFFICIENCY_RESPONSE_STYLE":"CONCISE","MINILOOP_TOKEN_EFFICIENCY_PERSIST_RAW":"no","MINILOOP_TOKEN_EFFICIENCY_RAW_MIN_BYTES":"20","MINILOOP_TOKEN_EFFICIENCY_ARTIFACT_TTL_SECONDS":"12.5","MINILOOP_TOKEN_EFFICIENCY_MAX_ARTIFACT_BYTES":"30","MINILOOP_TOKEN_EFFICIENCY_MAX_TOTAL_BYTES":"40","MINILOOP_AST_OUTLINE_ENABLED":"yes","MINILOOP_AST_OUTLINE_BINARY":"/operator/ast-outline","MINILOOP_AST_OUTLINE_SHA256":"A"*64,"MINILOOP_AST_OUTLINE_TIMEOUT":"2.5","MINILOOP_AST_OUTLINE_MAX_OUTPUT_BYTES":"100","MINILOOP_FEATURES":"all","MINILOOP_GUARDIAN":"on","MINILOOP_DECISIONS":"JEV","TYPESAFE_API_KEY":"fixture-typesafe-secret","MINILOOP_EXPERIMENTAL_WORKFLOWS":"true","MINILOOP_WORKFLOW_MAX_CONCURRENT_AGENTS":"2","MINILOOP_WORKFLOW_MAX_AGENTS":"3","MINILOOP_WORKFLOW_MAX_ROUNDS":"1","MINILOOP_WORKFLOW_WALL_TIME_SECONDS":"25.5"}),
        ("paths", {"MINILOOP_SKILLS_DIR":"skills", "MINILOOP_USER_RESOURCES_ROOT":"users", "MINILOOP_MEMORY_ROOT":"memory", "MINILOOP_REPO_ROOT":"repo", "MINILOOP_TRAJECTORY_ROOT":"traces"}),
        ("empty-paths", {"MINILOOP_WORKSPACE_ROOT":"","MINILOOP_SKILLS_DIR":"", "MINILOOP_SPILL_DIR":"  ","MINILOOP_USER_RESOURCES_ROOT":"", "MINILOOP_MEMORY_ROOT":""}),
        ("empty-numeric", {"MINILOOP_MAX_TOKENS":" ","MINILOOP_TEAM_IDLE_POLL":"", "MINILOOP_TRAJECTORY_CAPTURE_CONTENT":""}),
        ("unicode-numbers", {"MINILOOP_MAX_TOKENS":"+１２_３", "MINILOOP_TEAM_IDLE_POLL":"١.٢_٥"}),
        ("legacy-flags", {"MINILOOP_FAKE_LLM":"False", "MINILOOP_FEATURES":"FALSE"}),
        ("boolean-case", {"MINILOOP_TRAJECTORIES":"FaLsE", "MINILOOP_TRAJECTORY_CAPTURE_CONTENT":" YeS "}),
        ("integer-typo", {"MINILOOP_MAX_TOKENS":"8k"}),
        ("float-typo", {"MINILOOP_TEAM_IDLE_POLL":"soon"}),
        ("float-hex-rejected", {"MINILOOP_TEAM_IDLE_POLL":"0x1p0"}),
        ("boolean-typo", {"MINILOOP_TRAJECTORY_CAPTURE_CONTENT":"flase"}),
        ("bad-mode", {"MINILOOP_TOKEN_EFFICIENCY_MODE":"auto"}),
        ("bad-style", {"MINILOOP_TOKEN_EFFICIENCY_RESPONSE_STYLE":"terse"}),
        ("bad-decision", {"MINILOOP_DECISIONS":"auto"}),
        ("jev-no-key", {"MINILOOP_DECISIONS":"jev"}),
        ("ast-unpinned", {"MINILOOP_AST_OUTLINE_ENABLED":"true"}),
        ("ast-bad-digest", {"MINILOOP_AST_OUTLINE_SHA256":"wrong"}),
        ("raw-limit-crossed", {"MINILOOP_TOKEN_EFFICIENCY_RAW_MIN_BYTES":"3000000"}),
        ("artifact-limit-crossed", {"MINILOOP_TOKEN_EFFICIENCY_MAX_ARTIFACT_BYTES":"30000000"}),
        ("workflow-cap", {"MINILOOP_WORKFLOW_MAX_CONCURRENT_AGENTS":"5"}),
        ("workflow-shortage", {"MINILOOP_WORKFLOW_MAX_AGENTS":"3"}),
        ("rate-negative", {"MINILOOP_RATE_LIMIT_PER_MINUTE":"-1"}),
    ]
    positive = ["MAX_TOKENS","TOKEN_THRESHOLD","MAX_CONCURRENT_LLM","MAX_CONCURRENT_TOOLS","MAX_TURNS","SUBAGENT_MAX_ROUNDS","SUBAGENT_MAX_DEPTH","BASH_TIMEOUT","APPROVAL_TIMEOUT","TEAM_IDLE_POLL","TEAM_IDLE_TIMEOUT","TOKEN_EFFICIENCY_RAW_MIN_BYTES","TOKEN_EFFICIENCY_ARTIFACT_TTL_SECONDS","TOKEN_EFFICIENCY_MAX_ARTIFACT_BYTES","TOKEN_EFFICIENCY_MAX_TOTAL_BYTES","AST_OUTLINE_TIMEOUT","AST_OUTLINE_MAX_OUTPUT_BYTES","WORKFLOW_MAX_CONCURRENT_AGENTS","WORKFLOW_MAX_ROUNDS","WORKFLOW_WALL_TIME_SECONDS"]
    cases.extend((f"nonpositive-{name.lower()}-{v}", {f"MINILOOP_{name}":str(v)}) for name in positive for v in (0,-1))
    rows=[]
    cwd=Path.cwd()
    try:
        os.chdir(scratch)
        for name, env in cases:
            error=None; result=None
            with patch.dict(os.environ, env, clear=True):
                try:
                    cfg=Settings()
                    result=dataclasses.asdict(cfg)
                    for key,value in list(result.items()):
                        if key.endswith("_key"): result[key]="<set>" if value else None
                        elif isinstance(value,Path): result[key]=str(value).replace(str(scratch),"<cwd>").replace(str(scratch.parent),"<parent>")
                        elif isinstance(value,tuple): result[key]=[str(v).replace(str(scratch),"<cwd>").replace(str(scratch.parent),"<parent>") for v in value]
                    if "MINILOOP_SKILLS_DIR" not in env: result["skills_dir"]="<builtin>"
                except ValueError as exc: error=str(exc)
            rows.append({"name":name,"env":env,"settings":result,"error":error})
    finally: os.chdir(cwd)
    loader=SkillLoader(REPO_ROOT/"python/skills")
    return {"source_sha256":hashlib.sha256((REPO_ROOT/"python/mini_loop/config.py").read_bytes()).hexdigest(),
            "cases":rows,"builtin":{"descriptions":loader.descriptions(),"loaded":loader.load("code_review"),"source_sha256":hashlib.sha256((REPO_ROOT/"python/skills/code_review/SKILL.md").read_bytes()).hexdigest()}}


def _snapshot() -> dict[str, bytes]:
    with tempfile.TemporaryDirectory(prefix="mini-loop-go-contract-") as scratch:
        # server.py constructs its default app at import time. Isolate that
        # composition root too, before importing the module.
        os.environ["MINILOOP_FAKE_LLM"] = "1"
        os.environ["MINILOOP_WORKSPACE_ROOT"] = scratch

        from mini_loop.builtins import default_registry
        from mini_loop.agent import KNOWN_STOP_REASONS, MAX_RESUMPTIONS, REFUSAL_NOTICE
        from mini_loop.actions import UNKNOWN_RESULT
        from mini_loop.config import Settings
        from mini_loop.fake_llm import FakeMessage, FakeUsage, TextBlock, ToolUseBlock
        from mini_loop.server import create_app
        from mini_loop.storage import SCHEMA_VERSION, _SCHEMA

        registry = default_registry()
        tools = registry.schemas()
        tool_metadata = [
            {
                "name": tool.name,
                "risk": tool.risk,
                "readonly": tool.readonly,
                "parallel_safe": tool.parallel_safe,
                "capabilities": sorted(tool.capabilities),
            }
            for name in registry.names()
            if (tool := registry.get(name)) is not None
        ]
        openapi = create_app(
            settings=Settings(fake_llm=True, workspace_root=Path(scratch))
        ).openapi()

        fake_replies = [
            FakeMessage(
                [
                    TextBlock("Working on it."),
                    ToolUseBlock("bash", {"command": "echo handled: go"}, "toolu_1"),
                ],
                "tool_use", FakeUsage(13, 2), message_id="msg_fake_000001",
            ),
            FakeMessage(
                [TextBlock("Done. Tool said: handled: go")],
                "end_turn", FakeUsage(20, 1), message_id="msg_fake_000002",
            ),
            FakeMessage([], "refusal", FakeUsage(7, 0), message_id="msg_fake_000003"),
        ]

        # Keep only the fields the Python agent reads. The SDK-specific
        # stop_details/container extensions belong to the later G4 adapter.
        reply_snapshot = [
            {
                "id": reply.id,
                "type": reply.type,
                "role": reply.role,
                "model": reply.model,
                "content": [{"type": block.type, **vars(block)} for block in reply.content],
                "stop_reason": reply.stop_reason,
                "stop_sequence": reply.stop_sequence,
                "usage": vars(reply.usage),
            }
            for reply in fake_replies
        ]
        file_contracts = _file_contracts(Path(scratch) / "files")
        glob_contracts = _glob_contracts(Path(scratch) / "globs")
        runtime_contracts = _runtime_contracts()
        skill_contracts = _skill_contracts(Path(scratch) / "skills")
        context_contracts = _context_contracts(Path(scratch) / "context")
        subagent_contracts = _subagent_contracts(Path(scratch) / "subagents")
        action_contracts = _action_contracts(Path(scratch) / "actions")
        approval_contracts = _approval_contracts()
        secret_contracts = _secret_contracts()
        command_contracts = _command_contracts(Path(scratch) / "commands")
        loop_contracts = _loop_contracts(Path(scratch) / "loops")
        lifecycle_contracts = _lifecycle_contracts(Path(scratch) / "lifecycle")
        scheduling_contracts = _scheduling_contracts(Path(scratch) / "scheduling")
        manager_contracts = _manager_contracts(Path(scratch) / "manager")
        http_contracts = _http_contracts(Path(scratch) / "http")
        control_contracts = _control_contracts(Path(scratch) / "controls")
        fork_contracts = _fork_contracts(Path(scratch) / "forks")
        provider_contracts = _provider_contracts()
        stream_contracts = _stream_contracts()
        recovery_contracts = _recovery_contracts()
        progress_contracts = _progress_contracts()
        configuration_contracts = _configuration_contracts(Path(scratch) / "config")
        spill_contracts = _spill_contracts(Path(scratch) / "spill")
        trajectory_contracts = _trajectory_contracts(Path(scratch) / "trajectory")
        trace_view_contracts = _trace_view_contracts(Path(scratch) / "trace-view")
        webui_contracts = _webui_contracts(Path(scratch) / "webui")
        task_contracts = _task_contracts(Path(scratch) / "tasks")
        worktree_contracts = _worktree_contracts(Path(scratch) / "worktrees")
        worktree_tool_contracts = _worktree_tool_contracts(Path(scratch) / "worktree-tools")
        managed_worktree_contracts = _managed_worktree_contracts(Path(scratch) / "managed-worktrees")
        background_contracts = _background_contracts(Path(scratch) / "background")
        background_tool_contracts = _background_tool_contracts(Path(scratch) / "background-tools")
        managed_background_contracts = _managed_background_contracts(Path(scratch) / "managed-background")
        child_background_contracts = _child_background_contracts(Path(scratch) / "child-background")
        cron_contracts = _cron_contracts(Path(scratch) / "cron")

    methods = {"get", "post", "put", "patch", "delete"}
    operations = sum(
        method in methods
        for path_item in openapi["paths"].values()
        for method in path_item
    )
    manifest = {
        "default_tool_names": [tool["name"] for tool in tools],
        "http_operation_count": operations,
        "sqlite_schema_version": SCHEMA_VERSION,
        "known_stop_reasons": sorted(KNOWN_STOP_REASONS),
        "max_resumptions": MAX_RESUMPTIONS,
        "refusal_notice": REFUSAL_NOTICE,
        "unknown_tool_result": UNKNOWN_RESULT,
    }
    return {
        "python-contract-manifest.json": _json_bytes(manifest),
        "python-default-tools.json": _json_bytes(tools),
        "python-default-tool-metadata.json": _json_bytes(tool_metadata),
        "python-fake-replies.json": _json_bytes(reply_snapshot),
        "python-file-tools.json": _json_bytes(file_contracts),
        "python-glob-tools.json": _json_bytes(glob_contracts),
        "python-runtime-tools.json": _json_bytes(runtime_contracts),
        "python-skills.json": _json_bytes(skill_contracts),
        "python-context.json": _json_bytes(context_contracts),
        "python-subagents.json": _json_bytes(subagent_contracts),
        "python-actions.json": _json_bytes(action_contracts),
        "python-approvals.json": _json_bytes(approval_contracts),
        "python-secrets.json": _json_bytes(secret_contracts),
        "python-commands.json": _json_bytes(command_contracts),
        "python-loops.json": _json_bytes(loop_contracts),
        "python-lifecycle.json": _json_bytes(lifecycle_contracts),
        "python-scheduling.json": _json_bytes(scheduling_contracts),
        "python-manager.json": _json_bytes(manager_contracts),
        "python-http.json": _json_bytes(http_contracts),
        "python-controls.json": _json_bytes(control_contracts),
        "python-forks.json": _json_bytes(fork_contracts),
        "python-provider.json": _json_bytes(provider_contracts),
        "python-streams.json": _json_bytes(stream_contracts),
        "python-recovery.json": _json_bytes(recovery_contracts),
        "python-progress.json": _json_bytes(progress_contracts),
        "python-configuration.json": _json_bytes(configuration_contracts),
        "python-spill.json": _json_bytes(spill_contracts),
        "python-trajectory.json": _json_bytes(trajectory_contracts),
        "python-trace-view.json": _json_bytes(trace_view_contracts),
        "python-webui.json": _json_bytes(webui_contracts),
        "python-tasks.json": _json_bytes(task_contracts),
        "python-worktrees.json": _json_bytes(worktree_contracts),
        "python-worktree-tools.json": _json_bytes(worktree_tool_contracts),
        "python-managed-worktrees.json": _json_bytes(managed_worktree_contracts),
        "python-background.json": _json_bytes(background_contracts),
        "python-background-tools.json": _json_bytes(background_tool_contracts),
        "python-managed-background.json": _json_bytes(managed_background_contracts),
        "python-child-background.json": _json_bytes(child_background_contracts),
        "python-cron.json": _json_bytes(cron_contracts),
        "python-openapi.json": _json_bytes(openapi),
        "python-sqlite-schema.sql": (_SCHEMA.strip() + "\n").encode(),
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="reject stale exports")
    args = parser.parse_args()

    snapshot = _snapshot()
    if args.check:
        stale = [name for name, body in snapshot.items()
                 if not (TARGET / name).is_file() or (TARGET / name).read_bytes() != body]
        if stale:
            print("Stale Go contract exports: " + ", ".join(stale), file=sys.stderr)
            return 1
        print(f"Python→Go contracts current: {len(snapshot)} files")
        return 0

    TARGET.mkdir(parents=True, exist_ok=True)
    for name, body in snapshot.items():
        (TARGET / name).write_bytes(body)
        print(f"wrote {TARGET / name} ({len(body)} bytes)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
