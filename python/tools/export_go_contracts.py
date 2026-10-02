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
