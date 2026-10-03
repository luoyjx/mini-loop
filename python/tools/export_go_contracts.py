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
