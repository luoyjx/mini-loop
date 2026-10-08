"""Freeze Python's current public contracts for the independent Go port.

From the repository root, run ``.venv/bin/python python/tools/export_go_contracts.py``
or add ``--check`` to reject drift without writing. The export uses the real
default registry, FastAPI OpenAPI builder, temporary SQLite databases and
offline model probes. It does not call a paid endpoint or include credentials.
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
    fields = ["status", "activity", "busy", "run_count", "permission_mode", "workspace_bound", "model", "message_count", "todos", "subscribers", "sink_error", "workflows"]
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
                         "model", "message_count", "todos", "subscribers", "forked_from", "workflows")}
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



def _cron_surface_contracts(scratch: Path) -> dict:
    """Actual model gates and authenticated operator HTTP, with fixed future jobs."""
    import asyncio
    from mini_loop import Settings, SessionManager
    from mini_loop.agent import Agent
    from mini_loop.builtins import default_registry
    from mini_loop.cron import install_cron
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.registry import ToolRegistry, ToolCall
    from mini_loop.approvals import grant_candidate, proposed_candidate
    from mini_loop.server import create_app
    from mini_loop.auth import TokenAuth
    from fastapi.testclient import TestClient
    registry = install_cron(ToolRegistry())
    future = '0 0 31 2 *'
    variants = [
        {'name':'schedule_cron','input':{'cron':future,'prompt':'é'}},
        {'name':'schedule_cron','input':{'cron':future,'prompt':'é','recurring':False,'durable':False}},
        {'name':'schedule_cron','input':{'cron':future,'prompt':'é','recurring':True}},
        {'name':'schedule_cron','input':{'cron':future,'prompt':'é','durable':False}},
        {'name':'list_crons','input':{}},
        {'name':'cancel_cron','input':{'job_id':'missing'}},
    ]
    for row in variants:
        row['canonical'] = json.dumps(row['input'], sort_keys=True, ensure_ascii=False, separators=(',',':'))
        row['candidate'] = list(grant_candidate(row['name'],row['input']) or [])
        row['proposed'] = list(proposed_candidate(row['name'],row['input']) or [])
    def settings(root):
        return Settings(fake_llm=True,enable_features=False,trajectory_enabled=False,
                        workspace_root=root,skills_dir=root/'empty',spill_dir=None)
    async def tools():
        cases=[]
        for name in ('unconfigured','enabled','readonly'):
            root=scratch/name; root.mkdir(parents=True)
            m=SessionManager(settings(root),FakeAsyncAnthropic()) if name!='unconfigured' else None
            a=m.create(owner='alice',permission_mode='readonly' if name=='readonly' else 'auto').agent if m else Agent(
                client=FakeAsyncAnthropic(),settings=settings(root),workspace=root,tools=default_registry())
            install_cron(a.tools)
            aliases={}
            def normalize(value):
                for actual,alias in aliases.items():value=value.replace(actual,alias)
                return value
            specs=[('list_crons',{}),('schedule_cron',{'cron':future,'prompt':'é'*65}),
                   ('list_crons',{}),('schedule_cron',{'cron':future,'prompt':'once','recurring':False,'durable':False}),
                   ('schedule_cron',{'cron':'bad','prompt':'invalid'}),
                   ('cancel_cron',{'job_id':'missing'}),('cancel_cron',{'job_id':'<job1>'}),('list_crons',{})]
            steps=[]
            try:
                for index,(tool,value) in enumerate(specs):
                    original=dict(value)
                    for actual,alias in aliases.items():
                        if value.get('job_id')==alias:value={**value,'job_id':actual}
                    output=str(await a._exec_tool(ToolCall(tool,value,f'cron-tool-{index}')))
                    if m:
                        for jid in m.cron.jobs:
                            if jid not in aliases:aliases[jid]=f'<job{len(aliases)+1}>'
                    steps.append({'name':tool,'input':original,'output':normalize(output),
                                  'jobs':len(m.cron.jobs) if m else 0})
                cases.append({'name':name,'steps':steps})
            finally:
                if m:await m.stop()
        return cases
    tool_cases=asyncio.run(tools())
    root=scratch/'http';root.mkdir(parents=True)
    m=SessionManager(settings(root),FakeAsyncAnthropic())
    app=create_app(settings=settings(root),manager=m)
    rows=[];aliases={}
    with TestClient(app) as client:
        app.state.auth=TokenAuth({'token-a':'alice','token-b':'bob'})
        ids={}
        for token,label in [('token-a','session-fixture'),('token-b','other-session')]:
            sid=client.post('/sessions',json={},headers={'Authorization':'Bearer '+token}).json()['id']
            ids[label]=sid
        def normalize(value):
            if isinstance(value,str):
                for alias,actual in ids.items():value=value.replace(actual,alias)
                for actual,alias in aliases.items():value=value.replace(actual,alias)
                return value
            if isinstance(value,list):return [normalize(v) for v in value]
            if isinstance(value,dict):return {k:normalize(v) for k,v in value.items()}
            return value
        base='/sessions/session-fixture/cron'
        specs=[('empty','GET',base,'token-a',None),
               ('schedule','POST',base,'token-a',{'cron':future,'prompt':'é'}),
               ('owned','GET',base,'token-a',None),('foreign-session','GET',base,'token-b',None),
               ('missing-session','GET','/sessions/missing/cron','token-a',None),
               ('unauthenticated','GET',base,'',None),('bad-token','GET',base,'wrong',None),
               ('foreign-cancel','DELETE','/sessions/other-session/cron/<job1>','token-b',None),
               ('foreign-arm','POST','/sessions/other-session/cron/<job1>/arm','token-b',None),
               ('foreign-session-cancel','DELETE',base+'/<job1>','token-b',None),
               ('foreign-session-arm','POST',base+'/<job1>/arm','token-b',None),
               ('arm','POST',base+'/<job1>/arm','token-a',None),
               ('unknown-arm','POST',base+'/missing/arm','token-a',None),
               ('invalid-expression','POST',base,'token-a',{'cron':'bad','prompt':'x'}),
               ('missing-field','POST',base,'token-a',{'cron':future}),
               ('empty-prompt','POST',base,'token-a',{'cron':future,'prompt':''}),
               ('empty-cron','POST',base,'token-a',{'cron':'','prompt':'x'}),
               ('long-cron','POST',base,'token-a',{'cron':'x'*101,'prompt':'x'}),
               ('null-bool','POST',base,'token-a',{'cron':future,'prompt':'x','recurring':None}),
               ('invalid-bool','POST',base,'token-a',{'cron':future,'prompt':'x','durable':'maybe'}),
               ('coerce-bools','POST',base,'token-a',{'cron':future,'prompt':'coerced','recurring':'no','durable':'yes'}),
               ('numeric-bools','POST',base,'token-a',{'cron':future,'prompt':'numbers','recurring':0.0,'durable':1}),
               ('bad-numeric-bool','POST',base,'token-a',{'cron':future,'prompt':'x','recurring':2}),
               ('foreign-schedule','POST',base,'token-b',{'cron':future,'prompt':'foreign'}),
               ('validation-before-owner','POST',base,'token-b',{'cron':future,'prompt':''}),
               ('list-coerced','GET',base,'token-a',None),
               ('cancel','DELETE',base+'/<job1>','token-a',None),
               ('cancel-again','DELETE',base+'/<job1>','token-a',None),
               ('remaining','GET',base,'token-a',None)]
        for name,method,path,token,value in specs:
            actual=path
            for alias,sid in ids.items():actual=actual.replace(alias,sid)
            for jid,alias in aliases.items():actual=actual.replace(alias,jid)
            response=client.request(method,actual,json=value,headers={'Authorization':'Bearer '+token} if token else {})
            for jid in m.cron.jobs:
                if jid not in aliases:aliases[jid]=f'<job{len(aliases)+1}>'
            rows.append({'name':name,'method':method,'path':path,'token':token,'input':value,
                         'status':response.status_code,'body':normalize(response.json())})
    return {'schemas':registry.schemas(),'metadata':[{'name':t.name,'risk':t.risk,'readonly':t.readonly,
            'parallel_safe':t.parallel_safe,'capabilities':sorted(t.capabilities)} for n in registry.names() if (t:=registry.get(n))],
            'variants':variants,'tools':tool_cases,'http':rows,
            'source_sha256':{n:hashlib.sha256((PYTHON_ROOT/'mini_loop'/n).read_bytes()).hexdigest()
                             for n in ('cron.py','registry.py','permissions.py','server.py')}}


def _managed_cron_contracts(scratch: Path) -> dict:
    """Actual default manager ownership, managed authority and shutdown."""
    import asyncio
    from mini_loop import Settings, SessionManager
    from mini_loop.fake_llm import FakeAsyncAnthropic, text
    from mini_loop.run_context import RunContext
    from mini_loop.cron import CronJob
    from datetime import datetime
    scratch.mkdir(parents=True)
    when=datetime(2026,10,5,12,30)
    def client():return FakeAsyncAnthropic(responder=lambda _:([text('done')],'end_turn'),thinking=False)
    def manager(root,**kwargs):return SessionManager(Settings(fake_llm=True,workspace_root=root),client(),**kwargs)
    seen=[]
    async def capture(agent):
        seen.append(agent.current_run_context.as_dict())
        return []
    actual=manager(scratch/'authority',injectors=[capture])
    session=actual.create(owner='alice',permission_mode='auto')
    # Outside an event loop: source schedule does not implicitly start ticking.
    actual.cron.schedule(session.id,'30 12 5 10 *','scheduled')
    job=next(iter(actual.cron.jobs.values()))
    async def authority():
        try:
            await session.run('human',run_context=RunContext.explicit_human(actor_id='human',approved_capabilities=('personal_skill.capture_source',)))
            actual.cron._tick_once(when)
            await asyncio.gather(*tuple(actual.cron._running))
            ids=[c['message_id'] for c in seen]
            contexts=[{**c,'message_id':'<message>'} for c in seen]
            return {'contexts':contexts,'distinct_ids':len(ids)==len(set(ids)),
                    'run_count':session.info()['run_count'],'status':session.info()['status'],
                    'history_count':len(session.agent.messages),
                    'scheduled_prompt':session.agent.messages[-2]['content'].replace(job.id,'<job>'),
                    'default_tools':session.agent.tools.names(),'default_scheduler':actual.cron is not None}
        finally:await actual.stop()
    async def ownership(name,action,bound):
        root=(scratch/name).resolve();root.mkdir();checkout=root/'checkout';checkout.mkdir()
        m=SessionManager(Settings(fake_llm=True,workspace_root=root/'ws',bindable_roots=(checkout,)),client())
        a=m.create(owner='alice',permission_mode='auto',workspace=checkout if bound else None)
        b=m.create(owner='bob',permission_mode='auto')
        try:
            m.cron.schedule(a.id,'0 0 31 2 *','alice')
            m.cron.schedule(b.id,'0 0 31 2 *','bob')
            aj=next(j for j in m.cron.jobs.values() if j.session_id==a.id)
            foreign_cancel=m.cron.cancel(aj.id,session_id=b.id).replace(aj.id,'<job>')
            foreign_arm=m.cron.arm(aj.id,session_id=b.id).replace(aj.id,'<job>')
            child=await m.fork_session(a.id) if action=='fork' else None
            child_jobs=0 if child is None else sum(j.session_id==child.id for j in m.cron.jobs.values())
            shared=child is None or child.agent.state['cron'] is m.cron
            if action=='stop':removed=None;await m.stop()
            else:removed=m.delete(a.id,remove_workspace=action!='preserve')
            if m._cleanup_tasks:await asyncio.gather(*tuple(m._cleanup_tasks))
            stored=json.loads(m.cron.durable_path.read_text())
            return {'name':name,'action':action,'bound':bound,'removed':removed,
                    'workspace_exists':a.workspace.exists(),
                    'alice_jobs':sum(j.session_id==a.id for j in m.cron.jobs.values()),
                    'bob_jobs':sum(j.session_id==b.id for j in m.cron.jobs.values()),
                    'stored_jobs':len(stored),'foreign_cancel':foreign_cancel,'foreign_arm':foreign_arm,
                    'child_jobs':child_jobs,'shared_service':shared,'problems':m.cron.problems.summary()}
        finally:await m.stop()
    async def stopped_run():
        entered,cancelled=asyncio.Event(),asyncio.Event()
        async def block(agent):
            entered.set()
            try:await asyncio.Event().wait()
            except asyncio.CancelledError:cancelled.set();raise
            return []
        m=manager(scratch/'stop-live',injectors=[block])
        s=m.create(owner='alice',permission_mode='auto')
        m.cron.jobs['running']=CronJob('running','* * * * *','wait',s.id)
        m.cron._armed.add('running')
        try:
            m.cron._tick_once(when)
            await asyncio.wait_for(entered.wait(),5)
            await m.stop()
            return {'cancelled':cancelled.is_set(),'busy':s.busy,'status':s.info()['status'],
                    'run_count':s.info()['run_count'],'running_tasks':len(m.cron._running),
                    'workspace_exists':s.workspace.exists()}
        finally:await m.stop()
    async def collect():
        ownership_rows=[await ownership(*row) for row in (
            ('delete','delete',False),('preserve','preserve',False),('bound','delete',True),
            ('fork','fork',False),('stop','stop',False))]
        return ownership_rows,await stopped_run()
    authority_row=asyncio.run(authority())
    ownership_rows,stop_row=asyncio.run(collect())
    return {'authority':authority_row,'ownership':ownership_rows,'stop_live':stop_row,
            'source_sha256':{name:hashlib.sha256((PYTHON_ROOT/'mini_loop'/name).read_bytes()).hexdigest()
                             for name in ('manager.py','session.py','cron.py','run_context.py')}}


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


def _state_store_contracts(scratch: Path) -> dict:
    """Actual schema-v7 transactions, leases, audit retention and epoch storage."""
    from concurrent.futures import ThreadPoolExecutor
    from dataclasses import asdict, replace
    from unittest.mock import patch
    import sqlite3
    from mini_loop.storage import SQLiteStateStore, SessionRecord, StorageSchemaError
    from mini_loop.fake_llm import TextBlock, ThinkingBlock, ToolUseBlock

    scratch.mkdir(parents=True)
    path = scratch / "state.db"
    store = SQLiteStateStore(path)
    second = SQLiteStateStore(path)
    initial = SessionRecord("s", "$WORKSPACE", None, 10.0, 0, "idle", 900,
                            owner="tenant-a", workspace_bound=True)
    sibling = SessionRecord("other", "$OTHER", "system", 20.0, 1, "error", 0,
                            owner="tenant-b")
    store.upsert_session(initial)
    store.upsert_session(sibling)
    sessions = [{"name": "initial", "records": [asdict(r) for r in store.load_sessions()]}]
    updated = replace(initial, created_at=999.0, run_count=3, status="running",
                      todos=({"content": "check", "status": "pending", "activeForm": "checking"},),
                      pending_steering=("汉字😀 queued",))
    store.upsert_session(updated)
    store.append_event("s", {"type": "status", "status": "running", "seq": 1})
    store.append_event("s", {"type": "done", "text": "done", "seq": 2})
    sessions.append({"name": "updated", "records": [asdict(r) for r in second.load_sessions()]})

    provider_rows = [
        {"role": "user", "content": "begin 汉字😀"},
        {"role": "assistant", "content": [ThinkingBlock("reason", "opaque-signature"),
            TextBlock("working"), ToolUseBlock("bash", {"command": "echo safe"}, "u")]},
        {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "u", "content": "safe"}]},
        {"role": "assistant", "content": "done"},
    ]
    counts = [store.append_messages("s", provider_rows[:2]),
              second.append_messages("s", provider_rows[2:])]
    original = store.load_messages("s")
    rewritten = [{"role": "user", "content": "summary"}]
    counts.append(store.append_messages("s", rewritten, epoch=2))
    counts.append(store.append_messages("s", [], epoch=1))
    counts.append(store.append_messages("s", [], epoch=3))
    messages = {"append_counts": counts, "original": original,
                "current": second.load_messages("s"),
                "old": second.load_messages("s", epoch=1),
                "absent": second.load_messages("s", epoch=3),
                "epoch": second.transcript_epoch("s"),
                "ordinals": [list(r) for r in store._db.execute(
                    "SELECT ordinal,epoch FROM messages WHERE session_id='s' ORDER BY ordinal")]}
    # A second insert aborts the complete batch, including its first row.
    store._db.execute("CREATE TRIGGER reject_row BEFORE INSERT ON messages "
                      "WHEN instr(NEW.payload, 'reject') > 0 "
                      "BEGIN SELECT RAISE(ABORT, 'rejected probe row'); END")
    failed = False
    try:
        store.append_messages("s", [{"role": "user", "content": "first"},
                                    {"role": "assistant", "content": "reject"}], epoch=2)
    except sqlite3.IntegrityError:
        failed = True
    messages["rollback"] = {"failed": failed, "count": second.message_count("s"),
                            "current": second.load_messages("s")}
    store._db.execute("DROP TRIGGER reject_row")
    events = {"all": store.load_events("s"), "tail": store.load_events("s", after=1, limit=1),
              "zero_limit": store.load_events("s", limit=0), "cursor": store.event_cursor("s"),
              "missing_cursor": store.event_cursor("missing")}
    # Query times are supplied explicitly; no wall-clock outcomes are normalized.
    lease_steps = []
    with patch("mini_loop.storage.time.time", return_value=100.0) as clock:
        def lease(name, method, owner, now, ttl=10.0):
            clock.return_value = now
            target = second if owner == "process-b" else store
            if method == "release":
                target.release_lease("s", owner)
                result = None
            else:
                result = getattr(target, method + "_lease")("s", owner, ttl=ttl)
            lease_steps.append({"name": name, "operation": method, "owner": owner,
                                "now": now, "ttl": ttl, "result": result,
                                "holder": store.lease_holder("s")})
        lease("free", "acquire", "process-a", 100.0)
        lease("foreign-active", "acquire", "process-b", 100.0)
        lease("foreign-release", "release", "process-b", 100.0)
        lease("renew", "renew", "process-a", 105.0)
        lease("foreign-at-equality", "acquire", "process-b", 115.0)
        lease("own-renew-at-equality", "renew", "process-a", 115.0)
        lease("expired-cannot-renew", "renew", "process-a", 125.25)
        lease("foreign-after-expiry", "acquire", "process-b", 125.25)
        lease("stale-renew", "renew", "process-a", 126.0)
        lease("stale-release", "release", "process-a", 126.0)
        # Upsert cannot release or transfer the independently held lease.
        store.upsert_session(updated)
        holder_after_upsert = second.lease_holder("s")
        lease("holder-release", "release", "process-b", 126.0)
        lease("same-owner-reacquire", "acquire", "process-a", 130.0)
        lease("same-owner-expired-reacquire", "acquire", "process-a", 141.0)
        missing_claim = store.acquire_lease("missing", "process-a", ttl=10)

    action = {"action_id": "a", "session_id": "s", "message_id": "m", "tool_use_id": "u",
              "tool_name": "bash", "input_hash": "hash", "status": "started", "result": None,
              "workflow_run_id": None, "created_at": 10.0, "completed_at": None}
    store.write_action(action)
    store.write_action({**action, "action_id": "other-action", "session_id": "other"})
    approval = {"approval_id": "approval", "session_id": "s", "tool_use_id": "u",
                "tool_name": "bash", "rule": "exec", "message": "allow?", "input_preview": "echo safe",
                "status": "pending", "created_at": 10.0, "resolved_at": None}
    store.write_approval(approval)
    store.write_approval({**approval, "status": "cancelled", "resolved_at": 15.0,
                          "message": "identity must stay", "kind": "question", "answer": "no"})
    approvals = store.read_approvals("s")
    store.close()
    store = SQLiteStateStore(path)
    actions = {"after_open": store.read_action("a"),
               "scoped_unknown": sorted(store.mark_inflight_unknown("s")),
               "after_scoped": [store.read_action("a"), store.read_action("other-action")],
               "global_unknown": sorted(store.mark_inflight_unknown()),
               "repeated_unknown": store.mark_inflight_unknown()}
    store.write_action({**action, "status": "completed", "result": "done", "completed_at": 16.0,
                        "session_id": "cannot-transfer", "input_hash": "cannot-change", "created_at": 999.0})
    actions["updated"] = store.read_action("a")
    store.delete_session("s")
    deletion = {"sessions": [asdict(r) for r in second.load_sessions()],
                "messages": second.load_messages("s"), "event_cursor": second.event_cursor("s"),
                "events": second.load_events("s"), "holder": second.lease_holder("s"),
                "action": second.read_action("a"), "approvals": second.read_approvals("s")}
    store.close()
    second.close()

    # Two real connections allocate from MAX(ordinal) inside writing transactions.
    concurrent_path = scratch / "concurrent.db"
    left, right = SQLiteStateStore(concurrent_path), SQLiteStateStore(concurrent_path)
    def append_rows(args):
        backing, label = args
        for i in range(12):
            backing.append_messages("concurrent", [{"role": "user", "content": f"{label}-{i}"}])
    with ThreadPoolExecutor(max_workers=2) as executor:
        list(executor.map(append_rows, [(left, "left"), (right, "right")]))
    concurrent = {"ordinals": [r[0] for r in left._db.execute(
                    "SELECT ordinal FROM messages ORDER BY ordinal")],
                  "contents": sorted(r["content"] for r in left.load_messages("concurrent"))}
    left.close()
    right.close()

    # Original v1 shape, including rows, is upgraded in place by the source.
    legacy_path = scratch / "legacy.db"
    raw = sqlite3.connect(legacy_path)
    raw.executescript("""
      CREATE TABLE schema_version (version INTEGER NOT NULL);
      INSERT INTO schema_version VALUES (1);
      CREATE TABLE sessions (session_id TEXT PRIMARY KEY, workspace TEXT NOT NULL,
        system TEXT, created_at REAL NOT NULL, run_count INTEGER NOT NULL DEFAULT 0,
        status TEXT NOT NULL DEFAULT 'idle');
      CREATE TABLE messages (session_id TEXT NOT NULL, ordinal INTEGER NOT NULL,
        payload TEXT NOT NULL, PRIMARY KEY(session_id,ordinal));
      CREATE TABLE events (session_id TEXT NOT NULL, ordinal INTEGER NOT NULL,
        payload TEXT NOT NULL, PRIMARY KEY(session_id,ordinal));
      INSERT INTO sessions VALUES ('legacy', '$LEGACY', NULL, 1.0, 0, 'idle');
      INSERT INTO messages VALUES ('legacy', 1, '{"role":"user","content":"legacy"}');
    """)
    raw.close()
    legacy_store = SQLiteStateStore(legacy_path)
    legacy = {"version": legacy_store._db.execute("SELECT version FROM schema_version").fetchone()[0],
              "sessions": [asdict(r) for r in legacy_store.load_sessions()],
              "messages": legacy_store.load_messages("legacy"),
              "columns": {table: [r["name"] for r in legacy_store._db.execute(f"PRAGMA table_info({table})")]
                          for table in ("sessions", "messages", "actions", "approvals")}}
    legacy_store._db.execute("UPDATE schema_version SET version=8")
    legacy_store.close()
    refused_future = False
    try:
        rejected = SQLiteStateStore(legacy_path)
    except StorageSchemaError:
        refused_future = True
    else:
        rejected.close()
    corrupt_path = scratch / "corrupt.db"
    corrupt_path.write_bytes(b"not a database")
    refused_corrupt = False
    try:
        rejected = SQLiteStateStore(corrupt_path)
    except StorageSchemaError:
        refused_corrupt = True
    else:
        rejected.close()
    return {"schema_version": 7, "sessions": sessions, "messages": messages, "events": events,
            "leases": {"steps": lease_steps, "holder_after_upsert": holder_after_upsert,
                       "missing_claim": missing_claim}, "actions": actions, "approvals": approvals,
            "deletion": deletion, "concurrent": concurrent, "legacy": legacy,
            "refused_future": refused_future, "refused_corrupt": refused_corrupt,
            "source_sha256": {name: hashlib.sha256((PYTHON_ROOT / "mini_loop" / name).read_bytes()).hexdigest()
                              for name in ("storage.py", "session.py", "manager.py")}}


def _state_session_contracts(scratch: Path) -> dict:
    """Run actual AgentSession capture/guard against SQLite; no paid model calls."""
    import asyncio
    from unittest.mock import patch
    from mini_loop.agent import Agent
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, TextBlock, ThinkingBlock
    from mini_loop.secrets import SecretRegistry
    from mini_loop.session import AgentSession, LeaseLost
    from mini_loop.storage import SQLiteStateStore

    canary = 'state-secret-"汉字"\\0123456789'
    scratch.mkdir(parents=True)

    def attach(name, *, masked=False):
        root = scratch / name
        root.mkdir()
        store = SQLiteStateStore(root / "state.db")
        session = AgentSession(name, root, state_store=store, system="system")
        session.created_at = 10.0
        session.owner, session.workspace_bound = "tenant", True
        registry = SecretRegistry() if masked else None
        if registry is not None:
            registry.register("KEY", canary)
        session.agent = Agent(client=FakeAsyncAnthropic(),
                              settings=Settings(fake_llm=True, workspace_root=root),
                              workspace=root, secrets=registry)
        session._persist_session_record()
        return session, store

    async def collect():
        session, store = attach("state", masked=True)
        session.lease_owner = "process"
        session._require_lease()
        session.steer("queued " + canary)
        queued = list(store.load_sessions()[0].pending_steering)
        session.status, session.run_count = "running", 1
        session.agent.messages = [{"role": "user", "content": canary}]
        session._transcript_guard(session.agent.messages)
        count_before_provider = store.message_count("state")
        await session._capture_event({"type": "assistant_delta", "text": "piece", "_ephemeral": True})
        cursor_after_ephemeral = store.event_cursor("state")
        await session._capture_event({"type": "status", "status": "running"})
        session.agent.messages.append({"role": "assistant", "content": [
            ThinkingBlock("reason " + canary, "opaque-signature"), TextBlock(canary)]})
        await session._capture_event({"type": "assistant_text", "text": canary, "phase": "final_answer"})
        session.status = "idle"
        await session._finish_trajectory("completed", terminal_event={"type": "done", "text": "done"})
        terminal = store.load_events("state")[-1]
        final_status_without_growth = store.load_sessions()[0].status
        stored_raw = "0123456789" in json.dumps(store.load_messages("state"), ensure_ascii=False)
        live_raw = session.agent.messages[0]["content"] == canary
        session.agent.messages = [{"role": "user", "content": "summary"}]
        session._steering.clear()
        await session._capture_event({"type": "status", "status": "idle"})
        await session._capture_event({"type": "status", "status": "idle"})
        events = store.load_events("state")
        rewrite = {"epoch": store.transcript_epoch("state"),
                   "old_count": store.message_count("state", epoch=1),
                   "current_count": store.message_count("state"),
                   "event_epochs": [event["transcript_epoch"] for event in events],
                   "event_seqs": [event["seq"] for event in events],
                   "physical_cursor": store.event_cursor("state")}
        store.close()

        confirmed, confirmed_store = attach("confirmed")
        confirmed.lease_owner = "process"
        confirmed._require_lease()
        confirmed.agent.messages = [{"role": "user", "content": "start"}]
        lost = False
        with patch.object(confirmed_store, "renew_lease", return_value=False):
            try:
                await confirmed._capture_event({"type": "status", "status": "running"})
            except LeaseLost:
                lost = True
        confirmed_store.close()
        unconfirmed, unconfirmed_store = attach("unconfirmed")
        unconfirmed.lease_owner = "process"
        unconfirmed.agent.messages = [{"role": "user", "content": "start"}]
        unconfirmed_lost = False
        with patch.object(unconfirmed_store, "renew_lease", return_value=False):
            try:
                await unconfirmed._capture_event({"type": "status", "status": "running"})
            except LeaseLost:
                unconfirmed_lost = True
        unconfirmed_store.close()

        failures = []
        for name, operation in (("append", "append_messages"), ("event", "append_event"),
                                ("count", "message_count")):
            faulty, faulty_store = attach(name)
            faulty.agent.messages = [{"role": "user", "content": "start"}]
            if name == "count":
                # append_messages also queries message_count; isolate the guard
                # query after a successful append, rather than failing a write.
                faulty._flush_messages()
            stopped = False
            with patch.object(faulty_store, operation, side_effect=OSError("fixture write fault")):
                try:
                    if name == "event":
                        await faulty._capture_event({"type": "status", "status": "running"})
                    else:
                        faulty._transcript_guard(faulty.agent.messages)
                except OSError:
                    stopped = True
            failures.append({"name": name, "stopped": stopped,
                             "reported": faulty.persist_error is not None})
            faulty_store.close()
        return {"count_before_provider": count_before_provider,
                "cursor_after_ephemeral": cursor_after_ephemeral,
                "queued": queued, "live_raw": live_raw, "stored_raw": stored_raw,
                "final_status_without_growth": final_status_without_growth,
                "terminal_state_persisted": terminal["state_persisted"],
                "rewrite": rewrite, "confirmed_loss_stopped": lost,
                "unconfirmed_loss_stopped": unconfirmed_lost, "failures": failures}

    result = asyncio.run(collect())
    result["source_sha256"] = {name: hashlib.sha256((PYTHON_ROOT / "mini_loop" / name).read_bytes()).hexdigest()
                               for name in ("session.py", "storage.py", "agent.py")}
    return result


def _state_restore_contracts(scratch: Path) -> dict:
    """Execute real manager rehydration and SQLite crash-tail repair."""
    import asyncio
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.storage import SQLiteStateStore, SessionRecord
    from mini_loop.actions import UNKNOWN_RESULT, NOT_RUN_RESULT

    scratch.mkdir(parents=True)
    results = []
    recipes = [
        ("clean", [{"role": "user", "content": "start"},
                   {"role": "assistant", "content": [{"type": "text", "text": "done"}]}]),
        ("bare-user", [{"role": "user", "content": "interrupted"}]),
        ("bare-blocks", [{"role": "user", "content": [{"type": "text", "text": "interrupted"}]}]),
        ("tools", [{"role": "user", "content": "start"}, {"role": "assistant", "content": [
            {"type": "tool_use", "id": "unknown", "name": "bash", "input": {"command": "echo effect"}},
            {"type": "tool_use", "id": "parked", "name": "bash", "input": {"command": "echo pending"}}]}]),
        ("paired", [{"role": "user", "content": "start"}, {"role": "assistant", "content": [
            {"type": "tool_use", "id": "done", "name": "bash", "input": {"command": "echo paired"}}]},
            {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "done", "content": "paired"}]}]),
        ("empty", []),
        ("foreign", [{"role": "user", "content": "other writer prompt"}]),
    ]
    for name, messages in recipes:
        root = scratch / name
        root.mkdir()
        store = SQLiteStateStore(root / "state.db")
        workspace = root / "missing-workspace"
        todo = ({"content": "remember", "status": "pending", "activeForm": "remembering"},)
        record = SessionRecord("saved", str(workspace), "recorded system", 10.0, 4,
                               "running", 800, todos=todo, owner="tenant",
                               pending_steering=("queued request",), workspace_bound=True)
        store.upsert_session(record)
        store.append_messages("saved", messages, epoch=2)
        # A payload sequence ahead of the physical ordinal, as after ephemeral
        # progress. Capture source reset behavior without normalizing it away.
        store.append_event("saved", {"type": "status", "status": "running", "seq": 9,
                                     "session": "saved", "transcript_epoch": 2, "ts": 10.0})
        if name == "tools":
            store.write_approval({"approval_id": "pending", "session_id": "saved", "tool_use_id": "parked",
                                  "tool_name": "bash", "rule": "risk", "message": "pending", "input_preview": "{}",
                                  "status": "pending", "created_at": 11.0, "resolved_at": None,
                                  "kind": "permission", "answer": None})
        if name == "foreign":
            store.acquire_lease("saved", "foreign", ttl=3600)
        settings = Settings(fake_llm=True, workspace_root=root / "fleet", trajectory_root=root / "trajectories")
        manager = SessionManager(settings, FakeAsyncAnthropic(), state_store=store)
        handles = manager.restore_sessions()
        session = handles[0]
        saved = store.load_sessions()[0]
        approvals = store.read_approvals("saved")
        results.append({"name": name, "initial": messages,
                        "restored": store.load_messages("saved"),
                        "message_count": len(session.agent.messages), "epoch": store.transcript_epoch("saved"),
                        "seq_after_restore": session._seq, "physical_cursor": store.event_cursor("saved"),
                        "status": session.status, "busy": session.busy, "mode": session.permission_mode,
                        "owner": session.owner, "created_at": session.created_at, "run_count": session.run_count,
                        "bound": session.workspace_bound, "workspace_recreated": workspace.is_dir(),
                        "live_todos": session.agent.todo.snapshot(), "saved_todos": list(saved.todos),
                        "live_steering": list(session._steering), "saved_steering": list(saved.pending_steering),
                        "repaired": list(session._unknown_tool_uses), "omitted": session.agent.state["personal_skill_turns_omitted"],
                        "confirmed": session.lease_confirmed, "goal_armed": session.agent.state["goal_armed"],
                        "approvals": [{"id": a["approval_id"], "status": a["status"],
                                       "resolved": a["resolved_at"] is not None} for a in approvals],
                        "duplicate_restore_count": len(manager.restore_sessions())})
        # Capture the durable second-restore state before a model turn can save
        # the live metadata. This is a second real manager, not a reset mock.
        store.release_lease("saved", manager.instance_id)
        second = SessionManager(settings, FakeAsyncAnthropic(), state_store=store)
        again = second.restore_sessions()[0]
        results[-1]["second_steering"] = list(again._steering)
        results[-1]["second_todos"] = again.agent.todo.snapshot()
        results[-1]["second_messages"] = len(again.agent.messages)
        asyncio.run(second.stop())
        asyncio.run(manager.stop())
        store.close()
    return {"cases": results, "unknown_result": UNKNOWN_RESULT, "not_run_result": NOT_RUN_RESULT,
            "source_sha256": {name: hashlib.sha256((PYTHON_ROOT / "mini_loop" / name).read_bytes()).hexdigest()
                              for name in ("session.py", "manager.py", "agent.py", "storage.py")}}


def _scheduled_restore_contracts(scratch: Path) -> dict:
    """Run real scheduled restoration and its next model request, offline."""
    import asyncio
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text
    from mini_loop.manager import SessionManager
    from mini_loop.storage import SQLiteStateStore, NullStateStore, SessionRecord, _json_safe

    scratch.mkdir(parents=True)
    results = []
    clean = [{"role": "user", "content": "remember prior"},
             {"role": "assistant", "content": [{"type": "text", "text": "remembered"}]}]
    crash = [{"role": "user", "content": "prior request"},
             {"role": "assistant", "content": [{"type": "tool_use", "id": "effect",
              "name": "bash", "input": {"command": "echo old-effect"}}]}]
    recipes = [("bound-clean", True, clean), ("scratch-clean", False, clean),
               ("bound-crash", True, crash), ("scratch-crash", False, crash),
               ("missing-sql", False, None), ("missing-null", False, None),
               ("foreign-bound", True, clean)]
    async def scenario(name, bound, initial):
        root = scratch / name
        root.mkdir()
        store = NullStateStore() if name == "missing-null" else SQLiteStateStore(root / "state.db")
        recorded = root / "recorded"
        fresh = root / "factory"
        factory_calls = []
        requests = []
        def factory(session_id):
            factory_calls.append(session_id)
            return fresh
        def responder(kwargs):
            requests.append(kwargs)
            return [text("scheduled complete")], "end_turn"
        if initial is not None:
            store.upsert_session(SessionRecord("stable", str(recorded), "saved custom system", 20.0, 3,
                "running", 0, todos=({"content": "todo", "status": "pending", "activeForm": "doing"},),
                owner="alice", pending_steering=("queued",), workspace_bound=bound))
            store.append_messages("stable", initial, epoch=3)
        if name == "foreign-bound":
            store.acquire_lease("stable", "foreign", ttl=3600)
        settings = Settings(fake_llm=True, workspace_root=root / "fleet", trajectory_root=root / "trajectories")
        manager = SessionManager(settings, FakeAsyncAnthropic(responder=responder, thinking=False),
                                 state_store=store, workspace_factory=factory)
        session = manager.restore_scheduled_session("stable")
        again = manager.restore_scheduled_session("stable")
        before = store.load_sessions()
        result = {"name": name, "initial": initial or [], "messages": _json_safe(session.agent.messages),
                  "owner": session.owner, "bound": session.workspace_bound,
                  "workspace_kind": "recorded" if session.workspace == recorded else "factory",
                  "workspace_created": session.workspace.is_dir(), "factory_calls": list(factory_calls),
                  "same_handle": session is again, "system": session.system, "mode": session.permission_mode,
                  "run_count": session.run_count, "status": session.status, "busy": session.busy,
                  "confirmed": session.lease_confirmed,
                  "saved_before_count": len(before), "saved_before_system": before[0].system if before else None,
                  "saved_before_workspace_kind": ("recorded" if Path(before[0].workspace) == recorded else "factory") if before else None,
                  "repaired": list(session._unknown_tool_uses)}
        try:
            result["run_result"] = await session.run("[Scheduled cron fixture] continue")
            result["run_error"] = None
        except Exception as error:
            result["run_result"] = None
            result["run_error"] = type(error).__name__
        result["model_calls"] = len(requests)
        result["history_persisted"] = bool(requests) and store.message_count("stable") >= len(session.agent.messages)
        if name == "missing-null":
            result["history_persisted"] = False
        await manager.stop()
        store.close()
        return result
    async def run_all():
        return [await scenario(*recipe) for recipe in recipes]
    results = asyncio.run(run_all())
    return {"cases": results,
            "source_sha256": {name: hashlib.sha256((PYTHON_ROOT / "mini_loop" / name).read_bytes()).hexdigest()
                              for name in ("session.py", "manager.py", "cron.py", "storage.py")}}


def _event_catchup_contracts(scratch: Path) -> dict:
    """Exercise the actual SSE endpoint iterator over real SQLite and Null stores."""
    import asyncio
    from starlette.requests import Request
    from mini_loop.auth import NullAuth
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app
    from mini_loop.storage import SQLiteStateStore, NullStateStore

    scratch.mkdir(parents=True)
    recipes = [("past-backlog", 250, "5", 0, False, False),
               ("bounded-tail", 2210, "5", 0, False, False),
               ("ephemeral-gap", 250, "5", 10, False, False),
               ("boundary", 250, "5", 0, False, True),
               ("null", 250, "5", 0, True, False)]
    recipes += [("header-" + str(i), 250, header, 0, False, False)
                for i, header in enumerate(("", "0", "-5", "bad", "+5", " 5 ", "1_0", "1__0",
                                           "999999999999999999999999999999999999", "\x1c5", "\x1f5", "0" * 4301))]
    async def scenario(name, count, header, ephemeral, null, boundary):
        root = scratch / name
        root.mkdir()
        store = NullStateStore() if null else SQLiteStateStore(root / "state.db")
        settings = Settings(fake_llm=True, workspace_root=root / "fleet", trajectory_root=root / "trace")
        manager = SessionManager(settings, FakeAsyncAnthropic(thinking=False), state_store=store)
        session = manager.create()
        for _ in range(count):
            for _ in range(ephemeral):
                await session.emit({"type": "assistant_delta", "text": "progress", "_ephemeral": True})
            await session.emit({"type": "status", "status": "idle"})
        head = store.event_cursor(session.id)
        reads = []
        loop = asyncio.get_running_loop()
        original = store.load_events
        def load(session_id, *, after=0, limit=None):
            reads.append({"after": str(after), "limit": limit})
            if boundary:
                asyncio.run_coroutine_threadsafe(session.emit({"type": "status", "status": "idle"}), loop).result()
            return original(session_id, after=after, limit=limit)
        store.load_events = load
        app = create_app(manager=manager, settings=settings)
        app.state.manager = manager
        app.state.auth = NullAuth()
        endpoint = next(route.endpoint for route in app.routes
                        if getattr(route, "path", "") == "/sessions/{session_id}/events")
        request = Request({"type": "http", "app": app, "headers": [(b"last-event-id", header.encode("latin1"))],
                           "method": "GET", "path": "/sessions/" + session.id + "/events",
                           "query_string": b""})
        response = await endpoint(request, session.id, True)
        ids, names = [], []
        stream_error = None
        try:
            while True:
                try:
                    frame = await asyncio.wait_for(response.body_iterator.__anext__(), 0.1)
                except asyncio.TimeoutError:
                    break
                except Exception as error:
                    stream_error = type(error).__name__
                    break
                ids.append(int(frame["id"]))
                names.append(frame["event"])
        finally:
            await response.body_iterator.aclose()
        subscribers = len(session._subscribers)
        seq = session._seq
        await manager.stop()
        store.close()
        return {"name": name, "count": count, "header": header, "ephemeral": ephemeral,
                "null": null, "boundary": boundary, "physical_head": head, "live_sequence": seq,
                "ids": ids, "envelope_names": sorted(set(names)), "reads": reads,
                "subscribers_after": subscribers, "stream_error": stream_error}
    async def run_all():
        return [await scenario(*recipe) for recipe in recipes]
    return {"cases": asyncio.run(run_all()),
            "source_sha256": {name: hashlib.sha256((PYTHON_ROOT / "mini_loop" / name).read_bytes()).hexdigest()
                              for name in ("server.py", "session.py", "storage.py")}}


def _transcript_contracts(scratch: Path) -> dict:
    """Actual owned HTTP transcript reads over SQLite, rewrites and Null storage."""
    from fastapi.testclient import TestClient
    from importlib.metadata import version
    from mini_loop.auth import TokenAuth
    from mini_loop.compaction import microcompact
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.secrets import SecretRegistry
    from mini_loop.server import create_app
    from mini_loop.storage import SQLiteStateStore, NullStateStore
    from urllib.parse import urlencode

    scratch.mkdir(parents=True)
    secrets = SecretRegistry()
    secrets.register("KEY", "transcript-secret-0123456789")
    initial = [{"role": "user", "content": "transcript-secret-0123456789"}]
    for i in range(8):
        initial += [{"role": "assistant", "content": [{"type": "tool_use", "id": "t" + str(i),
                    "name": "bash", "input": {"command": "echo hi"}}]},
                    {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "t" + str(i),
                    "content": "ORIGINAL-" * 60}]}]
    cases = []
    def scenario(name, null):
        root = scratch / name
        root.mkdir()
        store = NullStateStore() if null else SQLiteStateStore(root / "state.db")
        settings = Settings(fake_llm=True, trajectory_enabled=False, workspace_root=root / "fleet")
        manager = SessionManager(settings, FakeAsyncAnthropic(thinking=False), state_store=store, secrets=secrets)
        app = create_app(manager=manager, settings=settings)
        with TestClient(app) as client:
            app.state.auth = TokenAuth({"token-a": "alice", "token-b": "bob"})
            owner = {"Authorization": "Bearer token-a"}
            session = manager.create(owner="alice")
            sid = session.id
            def call(label, query=None, token="token-a", missing=False):
                suffix = "" if query is None else "?" + urlencode(query)
                path = "/sessions/" + ("missing" if missing else sid) + "/transcript" + suffix
                headers = {} if token is None else {"Authorization": "Bearer " + token}
                response = client.get(path, headers=headers)
                value = response.json()
                if isinstance(value, dict) and value.get("session") == sid:
                    value["session"] = "fixture"
                if isinstance(value, dict) and isinstance(value.get("detail"), str):
                    value["detail"] = value["detail"].replace(sid, "fixture")
                cases.append({"name": name + "-" + label, "null": null, "query": query or [],
                              "token": token, "missing": missing, "status": response.status_code,
                              "response": value, "challenge": response.headers.get("www-authenticate")})
            call("empty")
            session.agent.messages.extend(initial)
            session._flush_messages()
            call("original")
            cleared = microcompact(session.agent.messages)
            assert cleared > 0
            session._flush_messages()
            # The same immutable pointer list is not a snapshot: microcompact
            # changed the source messages; capture the stored epochs explicitly.
            snapshots = [store.load_messages(sid, epoch=epoch) for epoch in (1, 2)]
            call("latest")
            for label, value in (("old", "1"), ("current", "2"), ("zero", "0"), ("future", "9"),
                                 ("negative", "-1"), ("empty-query", ""), ("bad", "no"),
                                 ("whole-float", "1.0"), ("fraction", "1.1"), ("plus", "+1"),
                                 ("strip", " 1 "), ("underscore", "0_1"), ("unicode", "١"),
                                 ("huge", "999999999999999999999999999999999999"),
                                 ("leading-zeroes", "0" * 4500 + "1"), ("too-large", "1" * 4500)):
                call(label, [("epoch", value)])
            call("repeated", [("epoch", "1"), ("epoch", "2")])
            call("foreign", token="token-b")
            call("foreign-invalid", [("epoch", "bad")], token="token-b")
            call("missing", missing=True)
            call("missing-invalid", [("epoch", "bad")], missing=True)
            call("unauth", token=None)
            call("unauth-invalid", [("epoch", "bad")], token=None)
            call("query-token", [("access_token", "token-a")], token=None)
            # A missing intermediate epoch is allowed inside the highest bound.
            if not null:
                store.append_messages(sid, [{"role": "assistant", "content": [{"type": "tool_use",
                    "id": "crash", "name": "bash", "input": {"command": "echo partial"}}]}], epoch=4)
                call("crash-tail")
                call("gap-epoch", [("epoch", "3")])
        store.close()
        return {"name": name, "epochs": snapshots, "cleared": cleared}
    seeds = [scenario("sql", False), scenario("null", True)]
    return {"cases": cases, "seeds": seeds,
            "validation_versions": {name: version(name) for name in ("fastapi", "pydantic", "pydantic_core")},
            "source_sha256": {name: hashlib.sha256((PYTHON_ROOT / "mini_loop" / name).read_bytes()).hexdigest()
                              for name in ("server.py", "session.py", "storage.py", "compaction.py")}}


def _plan_mode_contracts(scratch: Path) -> dict:
    """Actual source gate, prompt transitions and SQL restoration, offline."""
    import asyncio
    from mini_loop.agent import Agent
    from mini_loop.builtins import default_registry
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text
    from mini_loop.manager import SessionManager
    from mini_loop.plan_mode import install_plan_mode, PLAN_SECTION, fold_plan_mode
    from mini_loop.registry import ToolCall, ToolRegistry
    from mini_loop.storage import SQLiteStateStore, SessionRecord
    from mini_loop.approvals import grant_candidate, proposed_candidate
    scratch.mkdir(parents=True)
    registry = ToolRegistry()
    install_plan_mode(registry)
    variants = [{"name": "enter_plan_mode", "input": {}},
                {"name": "exit_plan_mode", "input": {"plan": "# é\n1. do it"}}]
    for row in variants:
        row["canonical"] = json.dumps(row["input"], sort_keys=True, ensure_ascii=False, separators=(",", ":"))
        row["candidate"] = list(grant_candidate(row["name"], row["input"]) or [])
        row["proposed"] = list(proposed_candidate(row["name"], row["input"]) or [])
    async def scenario(name):
        root = scratch / name
        root.mkdir()
        calls, events, results = [], [], []
        async def reviewer(ctx, plan):
            calls.append(plan)
            return name == "approve", "split step 2 into smaller pieces"
        async def emit(event):
            if event.get("type") == "plan_mode": events.append(bool(event["active"]))
            if event.get("type") == "tool_result": results.append(event)
        tools = default_registry()
        install_plan_mode(tools, approval=reviewer if name in ("approve", "reject") else None)
        a = Agent(client=FakeAsyncAnthropic(), workspace=root, tools=tools, emit=emit,
                  system="fixed" if name == "fixed" else None,
                  state={"permission_mode": "readonly" if name == "readonly" else "auto"},
                  settings=Settings(fake_llm=True,
                                    workspace_root=root, skills_dir=root / "empty", spill_dir=None))
        fingerprint = a.tools.snapshot().fingerprint
        steps = []
        for i, (tool, value) in enumerate([
            ("exit_plan_mode", {"plan": "# Before"}), ("enter_plan_mode", {}),
            ("enter_plan_mode", {}), ("exit_plan_mode", {"plan": ""}),
            ("exit_plan_mode", {"plan": "prose"}),
            ("exit_plan_mode", {"plan": "\u001c\u00a0# é\n1. do it\u001f"}),
            ("exit_plan_mode", {"plan": "# Again"})]):
            output = str(await a._exec_tool(ToolCall(tool, value, f"plan-{i}")))
            steps.append({"name": tool, "input": value, "output": output,
                          "active": bool(a.state.get("plan_mode")), "section": PLAN_SECTION in a.system,
                          "failed": results[-1]["error"], "denied": bool(results[-1].get("denied")),
                          "events": list(events), "catalog_stable": a.tools.snapshot().fingerprint == fingerprint})
        return {"name": name, "steps": steps, "reviews": calls}
    async def restore(active):
        root = scratch / ("restore-on" if active else "restore-off")
        root.mkdir()
        store = SQLiteStateStore(root / "state.db")
        store.upsert_session(SessionRecord("saved", str(root / "workspace"), None, 1.0, 2, "idle", 0, owner="alice"))
        store.append_messages("saved", [{"role": "user", "content": "prior"},
            {"role": "assistant", "content": [{"type": "text", "text": "remembered"}]}], epoch=1)
        for i, value in enumerate((False, True, active)):
            store.append_event("saved", {"type": "plan_mode", "active": value, "seq": i+1,
                "ts": 1.0, "session": "saved", "transcript_epoch": 1, "agent": "main", "depth": 0})
        requests = []
        def responder(kwargs):
            system = kwargs.get("system", "")
            if not isinstance(system, str): system = "\n".join(x.get("text", "") for x in system)
            requests.append(PLAN_SECTION in system)
            return [text("restored")], "end_turn"
        tools = default_registry()
        install_plan_mode(tools)
        manager = SessionManager(Settings(fake_llm=True, workspace_root=root / "fleet", trajectory_enabled=False),
            FakeAsyncAnthropic(responder=responder, thinking=False), state_store=store, tool_registry=tools)
        session = next(x for x in manager.restore_sessions() if x.id == "saved")
        folded = bool(session.agent.state.get("plan_mode"))
        await session.run("continue")
        await manager.stop()
        store.close()
        return {"active": active, "folded": folded, "request_sections": requests}
    async def run():
        return [await scenario(n) for n in ("headless", "readonly", "approve", "reject", "fixed")], [await restore(v) for v in (True, False)]
    cases, restored = asyncio.run(run())
    return {"section": PLAN_SECTION, "schemas": registry.schemas(),
        "metadata": [{"name": t.name, "risk": t.risk, "readonly": t.readonly, "parallel_safe": t.parallel_safe,
                      "capabilities": sorted(t.capabilities)} for t in registry._tools.values()],
        "variants": variants, "cases": cases, "restored": restored,
        "folds": [fold_plan_mode([]), fold_plan_mode([{"type": "plan_mode", "active": True}, {"type": "other"}])],
        "source_sha256": {n: hashlib.sha256((PYTHON_ROOT / "mini_loop" / n).read_bytes()).hexdigest()
                          for n in ("plan_mode.py", "prompts.py", "session.py", "registry.py")}}


def _goal_contracts(scratch: Path) -> dict:
    """Actual five source tools, stop consumer, SQLite restore and owned view."""
    import asyncio
    import copy
    from mini_loop.agent import Agent
    from mini_loop.builtins import default_registry
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text
    from mini_loop.goals import install_goals, GoalContinuation, fold_goal
    from mini_loop.registry import ToolRegistry, ToolCall, Hooks
    from mini_loop.run_context import RunContext
    from mini_loop.manager import SessionManager
    from mini_loop.storage import SQLiteStateStore, SessionRecord
    from mini_loop.server import create_app
    from mini_loop.auth import TokenAuth
    from mini_loop.approvals import grant_candidate, proposed_candidate
    from fastapi.testclient import TestClient
    scratch.mkdir(parents=True)
    registry = ToolRegistry()
    install_goals(registry)
    variants = [{"name": "goal_create", "input": {"objective": "é"}},
        {"name": "goal_create", "input": {"objective": "", "max_rounds": None}},
        {"name": "goal_create", "input": {"objective": "é", "max_rounds": 0}},
        {"name": "goal_status", "input": {}},
        {"name": "goal_complete", "input": {"revision": -1}},
        {"name": "goal_resume", "input": {"revision": 3}},
        {"name": "goal_block", "input": {"revision": 2, "code": "needs--input-", "message": "é"}}]
    for row in variants:
        row["canonical"] = json.dumps(row["input"], sort_keys=True, ensure_ascii=False, separators=(",", ":"))
        row["candidate"] = list(grant_candidate(row["name"], row["input"]) or [])
        row["proposed"] = list(proposed_candidate(row["name"], row["input"]) or [])
    contexts = {"human": RunContext.explicit_human(actor_id="alice"),
        "untrusted": RunContext.default(), "peer": RunContext.peer_agent(delegated_by="parent")}
    def settings(root):
        return Settings(fake_llm=True, workspace_root=root, skills_dir=root / "empty",
                        trajectory_enabled=False, spill_dir=None)
    async def scenario(mode):
        root = scratch / mode
        root.mkdir()
        events, results, aliases = [], [], {}
        def normalize(value):
            if isinstance(value, dict): return {k: normalize(v) for k, v in value.items()}
            if isinstance(value, list): return [normalize(v) for v in value]
            if isinstance(value, str):
                for ident, alias in aliases.items(): value = value.replace(ident, alias)
            return value
        async def emit(event):
            if event["type"] == "goal_change": events.append(copy.deepcopy({k: event[k] for k in ("operation", "goal")}))
            if event["type"] == "tool_result": results.append(event)
        tools = default_registry()
        install_goals(tools)
        a = Agent(client=FakeAsyncAnthropic(), tools=tools, workspace=root, settings=settings(root),
                  state={"permission_mode": mode}, emit=emit)
        recipes = [("untrusted", "goal_status", {}),
            ("untrusted", "goal_create", {"objective": "é"}),
            ("peer", "goal_create", {"objective": "é"}),
            ("human", "goal_complete", {"revision": 1}),
            ("human", "goal_create", {"objective": "bad", "max_rounds": -1}),
            ("human", "goal_create", {"objective": "bad", "max_rounds": 101}),
            ("human", "goal_create", {"objective": "finish é", "max_rounds": 2}),
            ("human", "goal_create", {"objective": "second"}),
            ("human", "goal_complete", {"revision": -1}),
            ("human", "goal_block", {"revision": 1, "code": "Bad", "message": "reason"}),
            ("human", "goal_block", {"revision": 1, "code": "needs-input", "message": "\u001c\u00a0"}),
            ("human", "goal_block", {"revision": 1, "code": "needs--input-\n", "message": "\u001c need é \u001f"}),
            ("untrusted", "goal_resume", {"revision": 2}),
            ("human", "goal_resume", {"revision": 1}),
            ("human", "goal_resume", {"revision": 2}),
            ("human", "stop", {}), ("human", "stop", {}), ("human", "stop", {}),
            ("human", "goal_resume", {"revision": 6}),
            ("untrusted", "goal_complete", {"revision": 6}),
            ("human", "goal_complete", {"revision": 7}),
            ("human", "goal_resume", {"revision": 7}),
            ("human", "goal_block", {"revision": 7, "code": "needs-input", "message": "complete can be blocked"}),
            ("human", "goal_complete", {"revision": 8}),
            ("human", "goal_create", {"objective": "", "max_rounds": 0}),
            ("human", "goal_status", {})]
        fingerprint = a.tools.snapshot().fingerprint
        steps = []
        for i, (authority, tool, value) in enumerate(recipes):
            if tool == "stop":
                output = await GoalContinuation().on_stop(a, a.messages, "done")
                failed = denied = False
            else:
                output = str(await a._exec_tool(ToolCall(tool, value, f"goal-{i}"), run_context=contexts[authority]))
                failed, denied = results[-1]["error"], bool(results[-1].get("denied"))
            goal = a.state.get("goal")
            if goal is not None and goal["id"] not in aliases: aliases[goal["id"]] = f"<goal{len(aliases)+1}>"
            steps.append(normalize({"authority": authority, "name": tool, "input": value, "output": output,
                "failed": failed, "denied": denied, "goal": copy.deepcopy(goal),
                "armed": bool(a.state.get("goal_armed")), "events": copy.deepcopy(events),
                "catalog_stable": a.tools.snapshot().fingerprint == fingerprint}))
        return {"mode": mode, "steps": steps}
    async def loop(custom):
        root = scratch / ("custom" if custom else "default")
        root.mkdir()
        requests = []
        def responder(kwargs):
            requests.append(copy.deepcopy(kwargs["messages"]))
            return [text("done")], "end_turn"
        a = Agent(client=FakeAsyncAnthropic(responder=responder, thinking=False), workspace=root,
            settings=settings(root), hooks=Hooks([]) if custom else None,
            state={"goal": {"id": "goal_saved", "revision": 1, "objective": "finish", "phase": "active",
                    "rounds_started": 0, "max_rounds": 2, "blocked": None}, "goal_armed": True})
        await a.run("begin", run_context=contexts["human"])
        return {"custom": custom, "requests": len(requests), "goal": copy.deepcopy(a.state["goal"]),
                "armed": bool(a.state["goal_armed"])}
    async def restore():
        root = scratch / "restore"
        root.mkdir()
        store = SQLiteStateStore(root / "state.db")
        store.upsert_session(SessionRecord("saved", str(root / "workspace"), None, 1.0, 2, "idle", 0, owner="alice"))
        goal = {"id": "goal_saved", "revision": 5, "objective": "finish", "phase": "active",
                "rounds_started": 1, "max_rounds": 2, "blocked": None}
        store.append_event("saved", {"type": "goal_change", "operation": "resume", "goal": goal,
            "seq": 1, "ts": 1.0, "session": "saved", "transcript_epoch": 0, "agent": "main", "depth": 0})
        count = 0
        def responder(kwargs):
            nonlocal count
            count += 1
            return [text("done")], "end_turn"
        tools = default_registry()
        install_goals(tools)
        m = SessionManager(settings(root / "fleet"), FakeAsyncAnthropic(responder=responder, thinking=False),
            state_store=store, tool_registry=tools)
        session = next(x for x in m.restore_sessions() if x.id == "saved")
        before = {"goal": copy.deepcopy(session.agent.state["goal"]), "armed": bool(session.agent.state["goal_armed"])}
        await session.run("continue")
        first = count
        await session.agent._exec_tool(ToolCall("goal_resume", {"revision": 5}, "resume"), run_context=contexts["human"])
        await session.run("continue", run_context=contexts["human"])
        after = {"goal": copy.deepcopy(session.agent.state["goal"]), "armed": bool(session.agent.state["goal_armed"])}
        await m.stop()
        store.close()
        return {"before": before, "after": after, "untrusted_requests": first, "human_requests": count-first}
    async def run():
        return [await scenario(mode) for mode in ("auto", "readonly")], [await loop(v) for v in (False, True)], await restore()
    cases, loops, restored = asyncio.run(run())
    root = scratch / "http"
    root.mkdir()
    m = SessionManager(settings(root), FakeAsyncAnthropic())
    session = m.create(owner="alice")
    app = create_app(settings=settings(root), manager=m)
    http = []
    with TestClient(app) as client:
        app.state.auth = TokenAuth({"token-a": "alice", "token-b": "bob"})
        for name, token, sid in [("owned", "token-a", session.id), ("foreign", "token-b", session.id),
                                 ("missing", "token-a", "missing"), ("unauthenticated", "", session.id)]:
            response = client.get(f"/sessions/{sid}/goal", headers={"Authorization": f"Bearer {token}"} if token else {})
            body = response.json()
            if "session" in body: body["session"] = "session-fixture"
            if isinstance(body.get("detail"), str): body["detail"] = body["detail"].replace(session.id, "session-fixture")
            http.append({"name": name, "token": token, "session": "missing" if sid == "missing" else "session-fixture",
                         "status": response.status_code, "body": body})
    return {"schemas": registry.schemas(),
        "metadata": [{"name": t.name, "risk": t.risk, "readonly": t.readonly, "parallel_safe": t.parallel_safe,
                      "capabilities": sorted(t.capabilities)} for t in registry._tools.values()],
        "variants": variants, "cases": cases, "loops": loops, "restored": restored, "http": http,
        "clear_fold": fold_goal([{"type": "goal_change", "operation": "create", "goal": restored["before"]["goal"]},
                                 {"type": "goal_change", "operation": "clear"}]),
        "source_sha256": {n: hashlib.sha256((PYTHON_ROOT / "mini_loop" / n).read_bytes()).hexdigest()
                          for n in ("goals.py", "permissions.py", "agent.py", "session.py", "server.py")}}


def _plan_outcome_contracts(scratch: Path) -> dict:
    """Audit actual gate/observer/journal/loop/recording outcomes together."""
    import asyncio
    import copy
    from mini_loop import Settings, SessionManager
    from mini_loop.builtins import default_registry
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
    from mini_loop.permissions import default_hooks
    from mini_loop.plan_mode import install_plan_mode
    from mini_loop.registry import Hook, ToolCall
    from mini_loop.run_context import RunContext
    scratch.mkdir(parents=True)
    async def scenario(name):
        root = scratch / name
        root.mkdir()
        reviews, observers, requests = [], [], []
        async def reviewer(ctx, plan):
            reviews.append(plan)
            if name == "reviewer-fault": raise RuntimeError("reviewer unavailable")
            return name == "approved", "revise step 2"
        class OutcomeHook(Hook):
            async def before_tool(self, ctx, call):
                if name == "before-deny" and call.name == "exit_plan_mode": return "DENIED: plan policy"
                return None
            async def after_tool(self, ctx, call, output):
                if name == "after-fault" and call.name == "exit_plan_mode": raise RuntimeError("post hook unavailable")
                return None
            async def on_result(self, ctx, call, output, *, denied=False, failed=False):
                record = ctx.state["action_journal"].get(ctx.action_id)
                observers.append({"name": call.name, "output": output, "failed": failed, "denied": denied,
                    "status": record.status if record else None, "result": record.result if record else None})
        hooks = default_hooks().add(OutcomeHook())
        tools = default_registry()
        install_plan_mode(tools, approval=reviewer)
        recipes = [("enter_plan_mode", {}), ("exit_plan_mode", {"plan": "prose"}),
                   ("exit_plan_mode", {"plan": "# Execute\n1. do it"})]
        def responder(kwargs):
            requests.append(copy.deepcopy(kwargs["messages"]))
            if len(requests) <= len(recipes):
                n, value = recipes[len(requests)-1]
                return [tool(n, _id=f"plan-{len(requests)}", **value)], "tool_use"
            return [text("done")], "end_turn"
        manager = SessionManager(Settings(fake_llm=True, workspace_root=root / "workspaces",
            trajectory_enabled=True, trajectory_root=root / "traces", skills_dir=root / "empty", spill_dir=None),
            FakeAsyncAnthropic(responder=responder, thinking=False), tool_registry=tools, hooks=hooks)
        session = manager.create(owner="alice", permission_mode="auto")
        run = RunContext.default()
        final = await session.run("plan", run_context=run)
        def event_projection(event):
            return {"name": event["name"], "output": event["output"], "failed": event["error"],
                    "denied": bool(event.get("denied")), "replayed": bool(event.get("replayed"))}
        live = [event_projection(e) for e in session._backlog if e["type"] == "tool_result"]
        rows = manager.trajectories.list(session_id=session.id)
        document = manager.trajectories.get(rows[0]["id"])
        stored = [event_projection(e) for e in document["events"] if e["type"] == "tool_result"]
        model_results = [copy.deepcopy(message["content"]) for message in requests[-1]
                         if message["role"] == "user" and isinstance(message["content"], list)
                         and any(p.get("type") == "tool_result" for p in message["content"])]
        steps = [{"name": v.name, "failed": v.failed, "denied": v.denied} for v in session.agent.recent_steps]
        before = bool(session.agent.state.get("plan_mode"))
        await session.agent._exec_tool(ToolCall("exit_plan_mode", recipes[-1][1], "plan-3"), run_context=run)
        replay = event_projection(next(e for e in reversed(session._backlog) if e["type"] == "tool_result"))
        result = {"name": name, "final": final, "requests": len(requests), "active": before,
            "live": live, "stored": stored, "observers": observers[:3], "replay_observer": observers[-1],
            "replay": replay, "reviews": reviews, "steps": steps, "model_results": model_results,
            "tool_errors": document["metrics"]["tool_errors"], "replay_active": bool(session.agent.state.get("plan_mode"))}
        await manager.stop()
        return result
    async def run():
        return [await scenario(n) for n in ("rejected", "approved", "reviewer-fault", "before-deny", "after-fault")]
    return {"cases": asyncio.run(run()), "source_sha256": {
        n: hashlib.sha256((PYTHON_ROOT / "mini_loop" / n).read_bytes()).hexdigest()
        for n in ("plan_mode.py", "agent.py", "actions.py", "registry.py", "trajectory.py")}}


def _decision_contracts(scratch: Path) -> dict:
    """Run typed decision contracts and isolated HTTP mocks against actual source."""
    import asyncio
    import copy
    import httpx
    import mini_loop.decisions as domain
    from mini_loop.decisions import DecisionRequest, DecisionResult, JevDecisionProvider, validate_result
    base = {"state": {"ticket": "I was billed twice", "charges": [12, 12]}, "questions": {
        "department": {"type": "choice", "instructions": "Which department?", "criteria": {"billing": {"about": "Charges"}, "technical": None}},
        "urgency": {"type": "score", "instructions": {"question": "How urgent?"}, "criteria": ["Can wait", {"description": "Needs attention"}, ["Urgent"]]},
        "refund": {"type": "noul", "instructions": "Is a refund requested?"}}}
    result = {"provider": "typesafe", "model": "jev-1.13.0", "probability_source": "jev", "usage": {"input_tokens": 125, "output_tokens": 17}, "answers": {
        "department": {"type": "choice", "choice": "billing", "confidence": .7, "probabilities": {"billing": .85, "technical": .15}},
        "urgency": {"type": "score", "score": 1.1, "confidence": .4, "probabilities": {"0": .1, "1": .7, "2": .2}, "legend": {"0": "Can wait", "1": {"description": "Needs attention"}, "2": ["Urgent"]}},
        "refund": {"type": "noul", "noul": .65}}}
    requests, results, bounds = [], [], []
    def capture(name, value, rows, evaluate):
        row = {"name": name, "input": copy.deepcopy(value)}
        try: row["accepted"] = evaluate(value)
        except Exception as error: row["error"] = str(error)
        rows.append(row)
    capture("mixed", base, requests, lambda v: DecisionRequest(**v).to_dict())
    for n, state in (("empty-string", ""), ("empty-object", {}), ("empty-array", []), ("numbers", {"int": 10**80, "float": 1.0, "small": 1e-7, "zero": -0.0}), ("null-state", None), ("bool-state", True), ("number-state", 0)):
        v = copy.deepcopy(base); v["state"] = state
        capture(n, v, requests, lambda v: DecisionRequest(**v).to_dict())
    for name, question in (
        ("unknown-type", {"type": "text", "instructions": "What next?"}),
        ("empty-instructions", {"type": "noul", "instructions": " \u001c"}),
        ("extra-field", {"type": "noul", "instructions": "Yes?", "execute": "anything"}),
        ("choice-empty", {"type": "choice", "instructions": "Which?", "criteria": {}}),
        ("choice-empty-name", {"type": "choice", "instructions": "Which?", "criteria": {"": None}}),
        ("choice-boolean", {"type": "choice", "instructions": "Which?", "criteria": {"x": True}}),
        ("score-one", {"type": "score", "instructions": "How?", "criteria": ["one"]}),
        ("score-null", {"type": "score", "instructions": "How?", "criteria": [None, "one"]}),
        ("noul-key", {"type": "noul", "instructions": "Yes?", "criteria": {"yes": "true"}}),
        ("noul-null", {"type": "noul", "instructions": "Yes?", "criteria": None}),
        ("noul-criteria", {"type": "noul", "instructions": ["Yes?"], "criteria": {"true": {}, "false": None}}),
        ("structured-instructions", {"type": "noul", "instructions": {"nested": [True, None, 1.0]}})):
        capture(name, {"state": "", "questions": {"q": question}}, requests, lambda v: DecisionRequest(**v).to_dict())
    for name, value in (("empty-questions", {}), ("empty-id", {"": {"type": "noul", "instructions": "Yes?"}}), ("object-question", {"q": []})):
        capture(name, {"state": "", "questions": value}, requests, lambda v: DecisionRequest(**v).to_dict())
    q = {"type": "noul", "instructions": "True?"}
    for n in (32, 33):
        capture(f"questions-{n}", {"state": "", "questions": {str(i): q for i in range(n)}}, requests, lambda v: DecisionRequest(**v).to_dict())
    for n in (255, 256):
        capture(f"choices-{n}", {"state": "", "questions": {"q": {"type": "choice", "instructions": "Which?", "criteria": {str(i): None for i in range(n)}}}}, requests, lambda v: DecisionRequest(**v).to_dict())
    request = DecisionRequest(**base)
    capture("mixed", result, results, lambda v: validate_result(request, DecisionResult(**v)).to_dict())
    mutations = {
        "answer-missing": lambda v: v["answers"].pop("refund"),
        "answer-extra": lambda v: v["answers"].update(extra={"type": "noul", "noul": .5}),
        "type-mismatch": lambda v: v["answers"]["refund"].update(type="choice"),
        "noul-bool": lambda v: v["answers"]["refund"].update(noul=True),
        "noul-extra": lambda v: v["answers"]["refund"].update(confidence=.3),
        "choice-not-max": lambda v: v["answers"]["department"].update(choice="technical"),
        "choice-unknown": lambda v: v["answers"]["department"].update(choice="unknown"),
        "confidence-bool": lambda v: v["answers"]["department"].update(confidence=True),
        "probability-bool": lambda v: v["answers"]["department"]["probabilities"].update(billing=True),
        "probability-sum": lambda v: v["answers"]["department"]["probabilities"].update(billing=.8),
        "probability-missing": lambda v: v["answers"]["department"]["probabilities"].pop("technical"),
        "wrong-score": lambda v: v["answers"]["urgency"].update(score=1),
        "score-bool": lambda v: v["answers"]["urgency"].update(score=True),
        "wrong-legend": lambda v: v["answers"]["urgency"]["legend"].update({"1": "changed"}),
        "empty-model": lambda v: v.update(model=" \u001c"),
        "bad-source": lambda v: v.update(probability_source="unknown"),
        "empty-usage": lambda v: v.update(usage={}),
        "partial-usage": lambda v: v.update(usage={"input_tokens": 2}),
        "bool-usage": lambda v: v.update(usage={"input_tokens": True, "output_tokens": 2}),
        "negative-usage": lambda v: v.update(usage={"input_tokens": -1, "output_tokens": 2})}
    for name, mutate in mutations.items():
        v = copy.deepcopy(result); mutate(v)
        capture(name, v, results, lambda v: validate_result(request, DecisionResult(**v)).to_dict())
    integer_result = copy.deepcopy(result)
    integer_result["answers"]["department"].update(confidence=1, probabilities={"billing": 1, "technical": 0})
    integer_result["answers"]["urgency"].update(score=1, confidence=0, probabilities={"0": 0, "1": 1, "2": 0})
    integer_result["answers"]["refund"]["noul"] = 1
    capture("integer-probabilities", integer_result, results, lambda v: validate_result(request, DecisionResult(**v)).to_dict())
    empty_size = len(domain._encoded({"state": "", "questions": {"q": q}}, domain.MAX_DECISION_BYTES))
    for char in ("x", "界"):
        count = (domain.MAX_DECISION_BYTES - empty_size) // len(char.encode())
        for n in (count, count + 1):
            v = {"state": char*n, "questions": {"q": q}}
            row = {"char": char, "count": n}
            try: row["bytes"] = len(domain._encoded(DecisionRequest(**v).to_dict(), domain.MAX_DECISION_BYTES))
            except Exception as error: row["error"] = str(error)
            bounds.append(row)
    deep = "value"
    for _ in range(40): deep = [deep]
    capture("deep", {"state": deep, "questions": {"q": q}}, requests, lambda v: DecisionRequest(**v).to_dict())
    payload = {k: result[k] for k in ("model", "answers", "usage")}
    async def http_case(name, statuses, headers=None, body=None, exception=None):
        calls, delays = [], []
        async def respond(r):
            calls.append({"method": r.method, "url": str(r.url), "authorization": r.headers["authorization"], "content_type": r.headers["content-type"], "body": json.loads(r.content)})
            if exception: raise exception("private-response-secret")
            status = statuses[min(len(calls)-1, len(statuses)-1)]
            return httpx.Response(status, headers={"retry-after": headers or "0"}, content=body if body is not None else json.dumps(payload).encode())
        async def sleep(delay): delays.append(delay)
        previous = domain.asyncio.sleep
        domain.asyncio.sleep = sleep
        try:
            async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
                row = {"name": name, "statuses": statuses, "header": headers or "0", "body": (body.decode() if body is not None else None), "exception": exception.__name__ if exception else None}
                try: row["result"] = (await JevDecisionProvider("test-private-key", client=client).evaluate(request)).to_dict()
                except Exception as error: row["error"] = str(error)
                row.update(calls=calls, delays=delays, borrowed_open=not client.is_closed)
                return row
        finally: domain.asyncio.sleep = previous
    async def run():
        rows=[]
        for status in (200, 401, 422, 302, 500): rows.append(await http_case(str(status), [status]))
        for header in ("0", "999", "nan", "inf", "-1", "invalid", "0_0", "0x1p1"):
            rows.append(await http_case("retry-"+header, [429,529,200],header))
        rows.append(await http_case("exhausted", [429]))
        for name, body in (("invalid-json", b"private-response-secret"), ("duplicate", b'{"model":"first","model":"second"}'), ("array", b"[]"), ("usage-missing", json.dumps({**payload,"usage":{}}).encode()), ("answer-invalid", json.dumps({**payload,"answers":{}}).encode())):
            rows.append(await http_case(name,[200],body=body))
        for exception in (httpx.ReadTimeout,httpx.ConnectError): rows.append(await http_case(exception.__name__,[200],exception=exception))
        return rows
    return {"requests": requests, "results": results, "bounds": bounds, "http": asyncio.run(run()), "source_sha256": {"decisions.py": hashlib.sha256((PYTHON_ROOT / "mini_loop" / "decisions.py").read_bytes()).hexdigest()}}


def _decision_runtime_contracts(scratch: Path) -> dict:
    """Execute real decision LLM calls and the common tool gate, without remote I/O."""
    import asyncio
    import copy
    from types import SimpleNamespace
    from mini_loop.agent import Agent, _CURRENT_RUN_CONTEXT
    from mini_loop.config import Settings
    from mini_loop.decision_llm import LLMDecisionProvider, _SYSTEM
    from mini_loop.decision_tools import install_decisions
    from mini_loop.decisions import DecisionRequest, DecisionResult
    from mini_loop.fake_llm import FakeMessage, FakeAsyncAnthropic
    from mini_loop.registry import ToolRegistry, ToolCall
    from mini_loop.run_context import RunContext
    from mini_loop.secrets import SecretRegistry
    scratch.mkdir(parents=True)
    request = {"state": {"ticket": "duplicate charge", "nested": [None, True, 1]}, "questions": {
        "route": {"type": "choice", "instructions": "Select a team", "criteria": {"billing": "Charges", "support": None}},
        "quality": {"type": "score", "instructions": {"question": "Completeness?"}, "criteria": ["missing", {"partial": True}, ["complete"]]},
        "ready": {"type": "noul", "instructions": "Enough detail?"}}}
    distributions = {"route": {"billing": .8, "support": .2}, "quality": {"0": .1, "1": .2, "2": .7}, "ready": {"true": .6, "false": .4}}
    body = json.dumps({"distributions": distributions}, ensure_ascii=False)
    recipes = []
    def add(name, output=body, stop="end_turn", value=None, content=None):
        recipes.append({"name": name, "input": copy.deepcopy(value or request), "reply": {
            "id": "msg_decision", "type": "message", "role": "assistant", "model": "served-decision-v1",
            "content": content if content is not None else [{"type": "text", "text": output}],
            "stop_reason": stop, "usage": {"input_tokens": 17, "output_tokens": 5}}})
    add("mixed")
    tied = copy.deepcopy(distributions); tied["route"] = {"support": .5, "billing": .5}
    add("response-order-tie", json.dumps({"distributions": tied}))
    integers = copy.deepcopy(distributions); integers["route"] = {"support": 0, "billing": 1}; integers["ready"] = {"true": 1, "false": 0}; integers["quality"] = {"2": 1, "0": 0, "1": 0}
    add("integer-probabilities", json.dumps({"distributions": integers}))
    add("single-choice", '{"distributions":{"q":{"only":1}}}', value={"state": [], "questions": {"q": {"type": "choice", "instructions": "Only?", "criteria": {"only": None}}}})
    add("thinking-and-split-text", content=[{"type": "thinking", "thinking": "private reasoning", "signature": "signed"}, {"type": "text", "text": body[:20]}, {"type": "text", "text": body[20:]}])
    add("redacted-thinking", content=[{"type": "redacted_thinking", "data": "opaque encrypted reasoning"}, {"type": "text", "text": body}])
    for stop in ("max_tokens", "tool_use", "pause_turn", "refusal", "stop_sequence"):
        add("stop-" + stop, stop=stop)
    add("empty-content", content=[])
    add("tool-content", content=[{"type": "tool_use", "id": "extra", "name": "compress", "input": {}}])
    for name, output in (("malformed", "private payload"), ("fence", "```json\n{}\n```"), ("array", "[]"),
            ("empty-object", "{}"), ("duplicate", '{"distributions":{},"distributions":{}}'),
            ("nonfinite", '{"distributions":{"route":{"billing":NaN}}}'),
            ("oversize", "x" * (128 * 1024 + 1)), ("surrogate", '"\\ud800"')):
        add(name, output)
    for name, probabilities in (("sum", {"billing": .2, "support": .2}), ("negative", {"billing": 1.1, "support": -.1}),
            ("boolean", {"billing": True, "support": 0}), ("string", {"billing": "0.8", "support": .2}),
            ("missing", {"billing": 1}), ("extra", {"billing": .8, "support": .2, "extra": 0}),
            ("huge-integer", {"billing": 10 ** 1000, "support": 0}),
            ("sum-tolerance", {"billing": .8, "support": .200001})):
        d = copy.deepcopy(distributions); d["route"] = probabilities
        add("probability-" + name, json.dumps({"distributions": d}))
    add("wrong-ids", '{"distributions":{"unknown":{"true":1,"false":0}}}')
    add("provider-fault")
    async def llm(row):
        requests, events = [], []
        async def create(**kwargs):
            requests.append(copy.deepcopy(kwargs))
            if row["name"] == "provider-fault": raise RuntimeError("private upstream credential")
            wire = row["reply"]
            return FakeMessage(copy.deepcopy(wire["content"]), wire["stop_reason"],
                usage=SimpleNamespace(**wire["usage"]), model=wire["model"], message_id=wire["id"])
        async def emit(event): events.append(copy.deepcopy(event))
        parent = Agent(client=SimpleNamespace(messages=SimpleNamespace(create=create)), emit=emit,
            workspace=scratch, settings=Settings(fake_llm=True, model="configured-model", skills_dir=scratch / "empty", spill_dir=None), label="parent", depth=2)
        parent.messages = [{"role": "user", "content": "unrelated private history"}]
        parent.state["recovery_model"] = "previous-parent-model"
        parent.last_text, parent.streamed_text, parent._last_model_span_id = "answer", "stream", "parent-span"
        before = (copy.deepcopy(parent.messages), copy.deepcopy(parent.state), parent.token_meter.snapshot())
        run = RunContext(message_id="parent-message")
        token = _CURRENT_RUN_CONTEXT.set(run)
        result = copy.deepcopy(row)
        try:
            result["result"] = (await LLMDecisionProvider(parent).evaluate(DecisionRequest(**row["input"]))).to_dict()
        except Exception as error:
            result["error"] = str(error); result["error_type"] = type(error).__name__
        finally:
            result["parent_context_restored"] = _CURRENT_RUN_CONTEXT.get() is run
            _CURRENT_RUN_CONTEXT.reset(token)
        result["requests"] = requests
        result["parent_unchanged"] = before == (parent.messages, parent.state, parent.token_meter.snapshot()) and (parent.last_text, parent.streamed_text, parent._last_model_span_id) == ("answer", "stream", "parent-span")
        result["model_scopes"] = [{"agent": e.get("agent"), "depth": e.get("depth"), "purpose": e.get("purpose"), "parent_message_id": e.get("parent_message_id"), "has_child_message": e.get("message_id") != run.message_id} for e in events if e["type"] == "model_start"]
        result["event_types"] = [e["type"] for e in events]
        return result
    secret = 'clé-"private"\\Ω-token'
    async def tool_case(name, mode):
        calls, events = [], []
        class Backend:
            model = "configured-decision"
            async def evaluate(self, value):
                calls.append(value.to_dict())
                if name == "provider-fault": raise RuntimeError("private upstream credential")
                probability = 9 if name == "bad-answer" else .9
                return DecisionResult(provider="custom", model="served-custom", probability_source="llm_estimate",
                    answers={"q": {"type": "noul", "noul": probability}}, usage={} if name == "unavailable-usage" else {"input_tokens": 13, "output_tokens": 3})
        registry = ToolRegistry(); install_decisions(registry, Backend())
        secrets = SecretRegistry(min_length=1); secrets.register("canary", secret)
        if name == "mask-invalid": secrets.register("type", "noul")
        async def emit(event): events.append(copy.deepcopy(event))
        parent = Agent(client=FakeAsyncAnthropic(), workspace=scratch, tools=registry, secrets=secrets, emit=emit,
            state={"permission_mode": mode}, settings=Settings(fake_llm=True, skills_dir=scratch / "empty", spill_dir=None))
        value = {"state": {secret: [secret]}, "questions": {"q": {"type": "noul", "instructions": "Ready?"}}} if name == "masked" else {"state": "urgent", "questions": {"q": {"type": "noul", "instructions": "Ready?"}}}
        output = await parent._exec_tool(ToolCall("decision", value, "decision-1"))
        projected = []
        for e in events:
            if e["type"] not in {"model_start", "model_end", "decision_completed", "decision_failed"}: continue
            projected.append({k: v for k, v in e.items() if k in {"type", "purpose", "model", "tool_count", "message_count", "status", "served_model", "usage", "prompt_tokens", "provider", "probability_source", "question_count", "error_type"}})
        final = next(e for e in reversed(events) if e["type"] == "tool_result")
        return {"name": name, "mode": mode, "input": value, "calls": calls, "events": projected,
                "output": output, "failed": final["error"], "denied": bool(final.get("denied"))}
    async def run():
        return [await llm(row) for row in recipes], [await tool_case(name, mode) for name, mode in (
            ("success", "auto"), ("readonly", "readonly"), ("headless", "interactive"), ("provider-fault", "auto"),
            ("bad-answer", "auto"), ("unavailable-usage", "auto"), ("masked", "auto"), ("mask-invalid", "auto"))]
    llm_cases, tool_cases = asyncio.run(run())
    registry = ToolRegistry(); install_decisions(registry)
    tool = registry.get("decision")
    return {"system": _SYSTEM, "schemas": registry.schemas(), "metadata": {"risk": tool.risk, "readonly": tool.readonly,
        "parallel_safe": tool.parallel_safe, "capabilities": sorted(tool.capabilities)}, "secret": secret,
        "llm": llm_cases, "tools": tool_cases, "source_sha256": {
            n: hashlib.sha256((PYTHON_ROOT / "mini_loop" / n).read_bytes()).hexdigest()
            for n in ("decision_llm.py", "decision_tools.py", "decisions.py", "agent.py", "registry.py", "permissions.py", "secrets.py")}}


def _decision_replay_contracts(scratch: Path) -> dict:
    """Run actual source large-result gates, SQL reopen and retention budgets."""
    import asyncio
    import runpy
    from mini_loop.actions import _bounded_result
    helpers = runpy.run_path(str(PYTHON_ROOT / "tests" / "test_decision_replay.py"))
    scratch.mkdir(parents=True)
    request = helpers["_request"]()
    baseline = asyncio.run(helpers["CountingProvider"]().evaluate(request)).to_dict()
    canonical = lambda value: json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    digest = lambda text: hashlib.sha256(text.encode()).hexdigest()
    cases = []
    for durable in (False, True):
        for maximum in (False, True):
            root = scratch / f"{durable}-{maximum}"
            root.mkdir()
            journal = (helpers["DurableActionJournal"](helpers["SQLiteStateStore"](root / "actions.db"))
                       if durable else helpers["InMemoryActionJournal"]())
            provider = helpers["CountingProvider"](at_limit=maximum)
            run = helpers["RunContext"].default()
            call = helpers["ToolCall"]("decision", request.to_dict(), "same-decision")
            first = asyncio.run(helpers["_agent"](root, journal, provider)._exec_tool(call, run_context=run))
            if durable:
                journal.store.close()
                journal = helpers["DurableActionJournal"](helpers["SQLiteStateStore"](root / "actions.db"))
            replay = asyncio.run(helpers["_agent"](root, journal, provider)._exec_tool(call, run_context=run))
            cases.append({"backing": "sqlite-reopen" if durable else "memory", "maximum": maximum,
                          "bytes": len(first.encode()), "canonical_sha256": digest(canonical(json.loads(first))),
                          "replay_exact": replay == first, "calls": provider.calls})
            if durable:
                journal.store.close()
    journal = helpers["InMemoryActionJournal"]()
    provider = helpers["CountingProvider"](at_limit=True)
    agent = helpers["_agent"](scratch, journal, provider)
    run = helpers["RunContext"].default()
    calls = [helpers["ToolCall"]("decision", request.to_dict(), f"decision-{i}") for i in range(5)]
    for call in calls:
        asyncio.run(agent._exec_tool(call, run_context=run))
    replay = asyncio.run(agent._exec_tool(calls[0], run_context=run))
    retained = [r for r in journal._records.values() if r.result != helpers["SHED_RESULT"]]
    bounds = []
    for tool, limit in (("decision", helpers["MAX_DECISION_ACTION_RESULT_CHARS"]),
                        ("ordinary", helpers["MAX_ACTION_RESULT_CHARS"])):
        for extra in (0, 1):
            value = _bounded_result("é" * (limit + extra), tool_name=tool)
            stored = helpers["DurableActionJournal"](helpers["SQLiteStateStore"](scratch / f"bound-{tool}-{extra}.db"))
            try:
                helpers["_begin"](stored, tool_name=tool)
                stored.finish("a1", status="unknown")
                reconciled = stored.reconcile("a1", status="completed", result="é" * (limit + extra))
                bounds.append({"tool": tool, "input_chars": limit + extra, "chars": len(value),
                               "sha256": digest(value), "reconciled_sha256": digest(reconciled.result)})
            finally:
                stored.store.close()
    return {"request": request.to_dict(), "baseline": baseline, "cases": cases, "bounds": bounds,
            "aggregate": {"records": len(journal._records), "retained": len(retained),
                          "retained_chars": journal._retained_result_chars,
                          "calls": provider.calls, "replay": replay, "problems": journal.problems},
            "source_sha256": {str(p.relative_to(PYTHON_ROOT)): hashlib.sha256(p.read_bytes()).hexdigest()
                              for p in (PYTHON_ROOT / "tests" / "test_decision_replay.py",
                                        *(PYTHON_ROOT / "mini_loop" / n for n in ("actions.py", "decisions.py", "decision_tools.py", "agent.py", "storage.py")))}}


def _decision_sink_contracts(scratch: Path) -> dict:
    """Actual escaped-result masking and cooperative gate cancellation."""
    import asyncio
    from mini_loop.agent import Agent
    from mini_loop.actions import InMemoryActionJournal
    from mini_loop.config import Settings
    from mini_loop.decision_tools import install_decisions
    from mini_loop.decisions import DecisionResult
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.registry import ToolRegistry, ToolCall
    from mini_loop.secrets import SecretRegistry
    scratch.mkdir(parents=True)
    secret = 'clé-secret-"中文"-123'
    request = {"state": {secret: [secret]}, "questions": {"q": {"type": "noul", "instructions": "Ready?"}}}
    async def run():
        events, requests = [], []
        secrets = SecretRegistry(); secrets.register("canary", secret)
        class Backend:
            async def evaluate(self, request):
                requests.append(request.to_dict())
                return DecisionResult(provider="custom", model=secret, usage={}, probability_source="llm_estimate",
                                      answers={"q": {"type": "noul", "noul": .9}})
        registry = ToolRegistry(); install_decisions(registry, Backend())
        async def emit(e): events.append(e)
        journal = InMemoryActionJournal()
        agent = Agent(client=FakeAsyncAnthropic(), workspace=scratch, tools=registry, secrets=secrets, emit=emit, llm_semaphore=asyncio.Semaphore(1),
                      settings=Settings(fake_llm=True, skills_dir=scratch / "skills", spill_dir=None),
                      state={"session_id": "sink", "action_journal": journal, "permission_mode": "auto"})
        output = await agent._exec_tool(ToolCall("decision", request, "mask"))
        if not output.startswith("{"):
            raise RuntimeError("Decision sink recipe did not return structured output")
        escaped = {"backend_request": requests[0], "output_model": json.loads(output)["model"],
                   "metadata_models": [e["model"] for e in events if e["type"] == "decision_completed"]}
        events.clear()
        entered = asyncio.Event()
        class Waiting:
            async def evaluate(self, request):
                entered.set()
                await asyncio.Event().wait()
        registry = ToolRegistry(); install_decisions(registry, Waiting()); agent.tools = registry
        task = asyncio.create_task(agent._exec_tool(ToolCall("decision", request, "cancel")))
        await entered.wait(); task.cancel()
        cancelled = False
        try:
            await task
        except asyncio.CancelledError:
            cancelled = True
        records = [r for r in journal._records.values() if r.tool_use_id == "cancel"]
        return escaped, {"propagated": cancelled, "decision_completed": any(e["type"] == "decision_completed" for e in events),
                         "decision_failed": any(e["type"] == "decision_failed" for e in events),
                         "model_status": [e["status"] for e in events if e["type"] == "model_end"],
                         "action_status": records[0].status, "permit": agent.semaphore._value}
    escaped, cancellation = asyncio.run(run())
    return {"secret": secret, "request": request, "escaped": escaped, "cancellation": cancellation,
            "source_sha256": {n: hashlib.sha256((PYTHON_ROOT / "mini_loop" / n).read_bytes()).hexdigest()
                              for n in ("agent.py", "decision_tools.py", "decisions.py", "secrets.py", "actions.py")}}


def _user_skill_contracts() -> dict:
    """Actual owner hashing and pure user-skill canonicalization, without writes."""
    from mini_loop.user_resources import _canonical_user_skill_parts, UserResourceResolver, UserSkillValidationError
    recipes = [
        {"name": "note", "description": " Useful ", "body": "\r\n  Read this.\rSecond line. \r\n"},
        {"name": "123", "description": "中文", "body": "\x1cBody\x1f"},
        *({"name": n, "description": "d", "body": "b"} for n in ("", "Upper", "a_b", "a--b", "../escape", "a" * 64, "a" * 65)),
        *({"name": "note", "description": d, "body": "b"} for d in ("", " ", "a\nb", "a\rb", "nul\x00", "<skill>", "< / ſkill >", "\x1f")),
        {"name": "note", "description": "中", "description_repeat": 200, "body": "b"},
        {"name": "note", "description": "中", "description_repeat": 201, "body": "b"},
        *({"name": "note", "description": "d", "body": b} for b in ("", " ", "\x00", "<SKILL/>", "<\u2028/skill>", "<sKİll>", "<sKıll>", "<skillish>", "front\r\nbody\rback")),
        *({"name": "note", "description": "d", "body": "中", "body_repeat": n} for n in (50000, 50001)),
        *({"name": "note", "description": "d", "body": "x" + sep, "body_repeat": n}
          for sep in ("\n", "\r\n", "\x85", "\u2028", "\x1c", "\x1f") for n in (500, 501)),
    ]
    cases = []
    for recipe in recipes:
        description = recipe["description"] * recipe.get("description_repeat", 1)
        body = recipe["body"] * recipe.get("body_repeat", 1)
        row = {"input": recipe}
        try:
            canonical, description, body = _canonical_user_skill_parts(recipe["name"], description, body)
            row.update(text_sha256=hashlib.sha256(canonical.encode()).hexdigest(),
                       description=description, body_sha256=hashlib.sha256(body.encode()).hexdigest())
        except UserSkillValidationError as error:
            row.update(error_code=error.code, error=str(error))
        cases.append(row)
    owners = [{"owner": owner, "key": UserResourceResolver._owner_key(owner)}
              for owner in ("alice", "Alice", "alice ", " ", "../../escape", "é", "e\u0301", "anonymous", "共享owner", "nul\x00owner")]
    return {"cases": cases, "owners": owners, "source_sha256": {
            n: hashlib.sha256((PYTHON_ROOT / "mini_loop" / n).read_bytes()).hexdigest()
            for n in ("user_resources.py", "skills.py")}}


def _owner_directory_contracts(scratch: Path) -> dict:
    """Actual resolver directory policy; store/catalogue behavior is not exported."""
    from mini_loop.user_resources import UserResourceResolver
    from mini_loop.skills import SkillLoader
    scratch.mkdir(parents=True)
    cases = []
    for recipe in ("fresh", "lax", "root-link", "dangling-root", "link-parent", "owner-link", "skills-link", "memory-link", "owner-file", "cycle-root"):
        base = scratch / recipe; base.mkdir()
        root, outside = base / "configured", base / "outside"
        outside.mkdir(mode=0o755); outside.chmod(0o755)
        if recipe == "lax": root.mkdir(mode=0o755)
        if recipe in {"root-link", "dangling-root"}:
            target = outside if recipe == "root-link" else base / "missing-target"
            root.symlink_to(target, target_is_directory=True)
        if recipe == "link-parent":
            nested = outside / "nested"; nested.mkdir()
            (base / "jump").symlink_to(nested, target_is_directory=True)
            root = base / "jump" / ".." / "configured"
        if recipe == "cycle-root": root.symlink_to(root)
        row = {"recipe": recipe, "owner": "alice"}
        try:
            resolver = UserResourceResolver(root, SkillLoader(base / "agent"))
            owner = resolver.root / resolver._owner_key("alice")
            if recipe in {"owner-link", "skills-link", "memory-link"}:
                target = owner
                if recipe != "owner-link": owner.mkdir(); target = owner / recipe.split("-")[0]
                target.symlink_to(outside, target_is_directory=True)
            if recipe == "owner-file": owner.write_text("keep")
            resources = resolver.for_owner("alice")
            row.update(root=str(resolver.root.relative_to(base.resolve())), owner_root=str(resources.root.relative_to(base.resolve())),
                       modes=[p.stat().st_mode & 0o777 for p in (resolver.root,resources.root,resources.root/"skills",resources.root/"memory")],
                       cached=resolver.for_owner("alice") is resources)
        except Exception as error:
            row.update(error=True, symlink_refusal="must not be a symlink" in str(error))
        row["outside_mode"] = outside.stat().st_mode & 0o777
        cases.append(row)
    return {"cases": cases, "source_sha256": hashlib.sha256((PYTHON_ROOT / "mini_loop" / "user_resources.py").read_bytes()).hexdigest()}


def _layered_skill_contracts(scratch: Path) -> dict:
    """Actual two-source catalogue, scope selection and source verification."""
    from mini_loop.skills import SkillLoader, LayeredSkillLoader
    cases = []
    def recipe(source, name, description="", body="body", count=1):
        return dict(source=source, name=name, description=description, body=body, count=count)
    def add(name, recipes, loads, mutations=()):
        base = scratch / name
        roots = {source: base / source for source in ("agent", "user")}
        for root in roots.values(): root.mkdir(parents=True)
        for row in recipes:
            for i in range(row["count"]):
                skill_name = row["name"] if row["count"] == 1 else f'{row["name"]}-{i:03d}'
                path = roots[row["source"]] / skill_name / "SKILL.md"
                path.parent.mkdir()
                path.write_text(f'---\nname: {skill_name}\ndescription: {row["description"]}\n---\n{row["body"]}', encoding="utf-8")
        agent, user = (SkillLoader(roots[source]) for source in ("agent", "user"))
        layered = LayeredSkillLoader(agent, user)
        descriptions = layered.descriptions()
        assert layered.descriptions() == descriptions
        for row in mutations:
            path = roots[row["source"]] / row["name"] / "SKILL.md"
            if row.get("remove"): path.unlink()
            else: path.write_text(row["text"], encoding="utf-8")
        results = []
        for request in loads:
            output = layered.load(**request)
            results.append(dict(input=request, sha256=hashlib.sha256(output.encode()).hexdigest(), failed=output.startswith("Error:")))
        normalize = lambda text: text.replace(str(roots["agent"]), "$AGENT").replace(str(roots["user"]), "$USER")
        problems = lambda loader: [dict(message=normalize(str(p)), count=loader.problems.counts[str(p)]) for p in loader.problems]
        cases.append(dict(name=name, recipes=recipes, mutations=list(mutations), descriptions=descriptions,
                          loads=results, problems=problems(layered), agent_problems=problems(agent), user_problems=problems(user)))
    requests = [dict(name=n) for n in ("shared", "agent:shared", "user:shared", "agent-only", "user-only", "missing", "", "bad/name", "other:shared", "user:")]
    requests += [dict(name="shared", scope=s) for s in ("agent", "user", " USER ", "\x1cAGENT\x1f", "invalid", "")]
    requests += [dict(name="agent:shared", scope="user"), dict(name="bad/name", scope="invalid")]
    add("collision", [recipe("agent","shared", "Agent", "a"), recipe("user","shared", "User", "u"), recipe("agent","agent-only"), recipe("user","user-only")], requests)
    add("empty", [], [dict(name="missing"),dict(name="missing",scope="user")])
    add("agent-only", [recipe("agent","one", "中文", "🌱")], [dict(name="one"),dict(name="user:one")])
    add("user-only", [recipe("user","one")], [dict(name="one"),dict(name="agent:one")])
    for source in ("agent", "user"):
        for mode in ("changed", "removed", "identical", "newlines"):
            text = "new" if mode == "changed" else "---\nname: shared\ndescription: Desc\n---\nbody"
            if mode == "newlines": text = text.replace("\n", "\r\n")
            add(source+"-"+mode, [recipe("agent","shared","Desc"),recipe("user","shared","Desc")],
                [dict(name=source+":shared"),dict(name=source+":shared")],
                [dict(source=source,name="shared",remove=mode=="removed",text=text)])
    for source in ("agent", "user"):
        add(source+"-flood", [recipe(source,"note","中"*201,"body",100)], [dict(name="note-099"),dict(name="missing")])
    add("both-flood", [recipe("agent","policy","🌱"*200,"a",40),recipe("user","note","中"*200,"u",80)], [dict(name="user:note-079")])
    add("available-cap", [recipe("agent","a"*55,"","a",100),recipe("user","u"*55,"","u",100)], [dict(name="missing"),dict(name="missing",scope="user")])
    return dict(cases=cases, source_sha256=hashlib.sha256((PYTHON_ROOT / "mini_loop" / "skills.py").read_bytes()).hexdigest())


def _memory_store_contracts(scratch: Path) -> dict:
    """Actual file memory operations, exact owner identity and scoped replacement."""
    from mini_loop.memory import MemoryStore, ScopedMemory, MAX_BODY
    from mini_loop.secrets import SecretRegistry
    cases = []
    def write(name, owner="anonymous", **fields):
        return dict(op="write", owner=owner, name=name, **fields)
    def add(name, steps, files=(), secret=""):
        root = scratch / name; root.mkdir(parents=True)
        for row in files:
            path = root / row["path"]
            if row.get("directory"): path.mkdir()
            else: path.write_bytes(bytes.fromhex(row["hex"]) if row.get("hex") else row.get("text", "").encode())
        secrets = None
        if secret:
            secrets = SecretRegistry(); secrets.register("TEST_SECRET", secret)
        store = MemoryStore(root, secrets=secrets)
        results = []
        for step in steps:
            row = dict(step)
            owner = step.get("owner")
            bound = ScopedMemory(store, owner) if step.get("scoped") else store
            kwargs = {} if step.get("scoped") else dict(owner=owner)
            op = step["op"]
            output, records = "", []
            if op == "write":
                output = bound.write(step["name"], step.get("type", "project"), step.get("description", ""),
                    step.get("body", "") * step.get("repeat", 1), origin=step.get("origin", "explicit"), **kwargs)
            elif op == "replace": bound.replace_all(step.get("memories", []), origin=step.get("origin", "imported"), **kwargs)
            elif op == "list": records = bound.list(**kwargs)
            elif op == "index": output = bound.index(**kwargs)
            elif op == "search": records = bound.search(step.get("query"), step.get("limit", 5), **kwargs)
            elif op == "flush": store.flush()
            elif op == "mutate":
                target = root / step["path"]
                if step.get("remove"): target.unlink()
                else: target.write_bytes(bytes.fromhex(step["hex"]) if step.get("hex") else step.get("text", "").encode())
            else: raise AssertionError(op)
            row.update(output=output, records=[dict(**{k:v for k,v in m.items() if k!="body"}, body_sha256=hashlib.sha256(m["body"].encode()).hexdigest(), body_characters=len(m["body"])) for m in records])
            row["index_exists"] = store.index_path.exists()
            results.append(row)
        final = [dict(path=path.name, sha256=hashlib.sha256(path.read_bytes()).hexdigest()) for path in sorted(root.iterdir()) if path.is_file()]
        cases.append(dict(name=name, files=list(files), secret=secret, steps=results, final_files=final,
            problems=[dict(message=str(p),count=store.problems.counts[str(p)]) for p in store.problems]))
    add("exact-owners", [write("a b", "alice", body="red apple"),write("a-b", "alice",body="green apple"),
        write("a b", "bob",body="other"),write("a b", " alice ",body="spaced"),write("a b", "alice\n",body="newline"),
        dict(op="list"),*[dict(op="list",owner=o) for o in ("alice","bob"," alice ","alice\n")],dict(op="index",owner="alice"),
        dict(op="search",owner="alice",query="apple",limit=5)])
    add("names", [*[write(n,body="memo") for n in ("中文", "🌱", "memory", "MEMORY", "../out", "", "A"*120, "İß")],dict(op="list"),dict(op="index")])
    add("headers-origins", [write(" title\nsecond ","alice\x1c",type="invalid",description="A\u2028B",body=" whitespace ",origin="invalid"),
        write("auto","alice",type="feedback",origin="auto_extracted",body="remember"),dict(op="list"),dict(op="list",owner="alice\x1c")])
    add("body-cap",[write("huge",body="中",repeat=MAX_BODY+1),dict(op="list"),dict(op="search",query="中中",limit=1)])
    add("masked",[write("credential","alice",description="token-secret-12345",body="token-secret-12345"),dict(op="index",owner="alice"),dict(op="list",owner="alice")],secret="token-secret-12345")
    add("scoped-replace",[write("one","alice",body="alpha"),write("one","bob",body="beta"),
        dict(op="replace",owner="alice",scoped=True,origin="consolidated",memories=[dict(name="new",body="alpha new")]),
        dict(op="list",owner="alice",scoped=True),dict(op="list",owner="bob",scoped=True),dict(op="index",owner="bob",scoped=True)])
    add("all-replace",[write("one","alice",body="old"),dict(op="replace",memories=[dict(name="fresh",origin="bad",body="new")]),dict(op="list"),dict(op="search",limit=-1)])
    legacy=lambda name,owner,body: f'---\nname: {name}\nowner: {owner}\n---\n{body}'
    add("legacy-migrate",[dict(op="list",owner="alice"),write("a b","alice",body="new"),dict(op="list",owner="alice"),write("a-b","alice",body="distinct"),dict(op="list",owner="alice")],
        [dict(path="a-b.md",text=legacy("a b","alice","old"))])
    add("unreadable",[dict(op="list"),dict(op="index"),dict(op="replace",owner="alice",memories=[]),dict(op="list")],
        [dict(path="bad.md",hex="fffe"),dict(path="folder.md",directory=True),dict(path="plain.md",text="plain\r\ntext")])
    add("parse-cache",[dict(op="list"),dict(op="mutate",path="note.md",text=legacy("note","anonymous","changed long")),dict(op="list"),
        dict(op="mutate",path="note.md",remove=True),dict(op="list")],[dict(path="note.md",text=legacy("note","anonymous","initial"))])
    add("search",[write("Beta",body="apple apple "),write("Alpha",body="apple pear"),write("Greek",body="中文 短句 αβ αβ ½½"),
        dict(op="search",query="apple APPLE",limit=5),dict(op="search",query="中文 αβ ½½",limit=2),dict(op="search",query="a !",limit=5),dict(op="search",limit=0)])
    add("keyed-import",[dict(op="list",owner="alice"),dict(op="list",owner="bob"),dict(op="list")],
        [dict(path="key.md",text="---\nname: key\nowner: bob\nowner_key: "+hashlib.sha256(b"alice").hexdigest().upper()+"\norigin: nope\ntype: custom\n---\nbody"),
         dict(path="invalid.md",text="---\nowner: alice\nowner_key: bad\n---\nlegacy")])
    flood=[write(f"note-{i:03d}",description="中"*200) for i in range(50)]
    add("index-cap",flood+[dict(op="index"),dict(op="list")])
    return dict(cases=cases,source_sha256=hashlib.sha256((PYTHON_ROOT / "mini_loop" / "memory.py").read_bytes()).hexdigest())


def _owner_resource_contracts(scratch: Path) -> dict:
    """Actual immutable resource caches, owner-local logs and mutable memory."""
    from mini_loop.user_resources import UserResourceResolver
    from mini_loop.skills import SkillLoader
    from mini_loop.memory import ScopedMemory
    from mini_loop.secrets import SecretRegistry
    cases = []
    def file(source, name, text, owner=""):
        return dict(source=source, owner=owner, name=name, text=text)
    def add(name, files, steps, secret=""):
        base=scratch/name; agent_dir=base/"agent"; root=base/"users"
        agent_dir.mkdir(parents=True);root.mkdir()
        def write(row):
            directory=agent_dir if row["source"]=="agent" else root/UserResourceResolver._owner_key(row["owner"])/"skills"
            path=directory/row["name"]/"SKILL.md";path.parent.mkdir(parents=True,exist_ok=True);path.write_text(row["text"],encoding="utf-8")
        for row in files: write(row)
        agent=SkillLoader(agent_dir)
        secrets=None
        if secret: secrets=SecretRegistry();secrets.register("TEST_SECRET",secret)
        resolver=UserResourceResolver(root,agent,secrets)
        pinned={};results=[]
        for step in steps:
            row=dict(step);owner=step.get("owner","");op=step["op"]
            if op=="file":write(step["file"])
            elif op=="resolve":
                resource=resolver.for_owner(owner);old=pinned.get(owner);pinned.setdefault(owner,resource)
                row.update(cached=old is resource if old is not None else False,scope=resource.scope,
                           key=resource.root.name,descriptions=resource.skills.descriptions())
            elif op=="new-resolver":resolver=UserResourceResolver(root,agent,secrets)
            elif op=="load":
                resource=resolver.for_owner(owner);output=resource.skills.load(step["name"])
                row.update(output=output,failed=output.startswith("Error:"))
            elif op=="remember":
                row["output"]=ScopedMemory(resolver.for_owner(owner).memory,owner).write(step["name"],"project","",step["body"])
            elif op=="index":row["output"]=ScopedMemory(resolver.for_owner(owner).memory,owner).index()
            elif op=="problems":
                normalize=lambda text:text.replace(str(root),"$USERS").replace(str(agent_dir),"$AGENT")
                row["problems"]=[normalize(str(p)) for p in resolver.problems]
                row["local_problems"]=[normalize(str(p)) for p in resolver.for_owner(owner).skills.problems]
            else:raise AssertionError(op)
            results.append(row)
        cases.append(dict(name=name,files=files,steps=results,secret=secret))
    skill=lambda name,body: f'---\nname: {name}\ndescription: {name} description\n---\n{body}'
    add("owners",[file("agent","shared",skill("shared","agent")),
        file("user","shared",skill("shared","alice"),"alice"),file("user","only",skill("only","bob"),"bob")],
        [*[dict(op="resolve",owner=o) for o in ("alice","bob"," alice ","é","e\u0301","alice")],
         dict(op="load",owner="alice",name="shared"),dict(op="load",owner="alice",name="user:shared"),
         dict(op="load",owner="bob",name="shared"),dict(op="load",owner="bob",name="user:shared"),
         dict(op="remember",owner="alice",name="one",body="private-alice"),dict(op="index",owner="bob"),dict(op="index",owner="alice")])
    add("snapshot",[],[dict(op="resolve",owner="alice"),dict(op="file",file=file("user","later",skill("later","new"),"alice")),
        dict(op="resolve",owner="alice"),dict(op="load",owner="alice",name="later"),dict(op="new-resolver"),
        dict(op="resolve",owner="alice"),dict(op="load",owner="alice",name="later")])
    bad="---\nname: bad/name\n---\nrefused"
    add("problems",[file("agent","bad",bad),file("user","bad",bad,"alice"),file("user","other",bad,"bob")],
        [dict(op="resolve",owner="alice"),dict(op="problems",owner="alice"),dict(op="resolve",owner="bob"),dict(op="problems",owner="alice"),dict(op="problems",owner="bob")])
    add("masked",[],[dict(op="resolve",owner="alice"),dict(op="remember",owner="alice",name="one",body="token-secret-12345"),dict(op="index",owner="alice")],secret="token-secret-12345")
    return dict(cases=cases,source_sha256=hashlib.sha256((PYTHON_ROOT/"mini_loop"/"user_resources.py").read_bytes()).hexdigest())


def _durable_create_contracts(scratch: Path) -> dict:
    """Actual anchored no-replace create and bounded no-follow reads."""
    from mini_loop.durable import atomic_create_bytes, read_bytes_no_follow
    import errno
    cases=[]
    recipes=("fresh", "exists", "directory", "leaf-link", "parent-link", "link-parent", "read-exact", "read-over", "read-directory", "read-leaf-link", "read-parent-link", "read-empty")
    for recipe in recipes:
        base=(scratch/recipe).resolve();base.mkdir(parents=True)
        parent=base/"parent";outside=base/"outside";parent.mkdir();outside.mkdir()
        target=parent/"SKILL.md";payload=b"canonical\n";read=recipe.startswith("read-")
        if recipe in {"exists", "read-exact", "read-over"}:target.write_bytes(b"existing")
        if recipe=="read-empty":target.write_bytes(b"")
        if recipe in {"directory","read-directory"}:target.mkdir()
        if recipe in {"leaf-link","read-leaf-link"}:
            (outside/"SKILL.md").write_bytes(b"outside");target.symlink_to(outside/"SKILL.md")
        if recipe in {"parent-link","read-parent-link"}:
            (base/"alias").symlink_to(outside,target_is_directory=True)
            (outside/"SKILL.md").write_bytes(b"outside");target=base/"alias"/"SKILL.md"
        if recipe=="link-parent":
            (base/"alias").symlink_to(parent,target_is_directory=True);target=base/"alias"/".."/"SKILL.md"
        row=dict(recipe=recipe,read=read,limit=7 if recipe=="read-over" else 8)
        try:
            if read:row["hex"]=read_bytes_no_follow(target,max_bytes=row["limit"]).hex()
            else:
                identity=atomic_create_bytes(target,payload);stat=target.stat()
                row.update(identity_matches=identity==(stat.st_dev,stat.st_ino),mode=stat.st_mode&0o777)
        except Exception as error:
            row.update(failed=True,exists=isinstance(error,FileExistsError),too_large=isinstance(error,OverflowError),
                unsafe=isinstance(error,OSError) and error.errno in (errno.ELOOP,errno.ENOTDIR),not_regular="requires a regular file" in str(error))
        row["files"]=[dict(path=str(path.relative_to(base)),hex=path.read_bytes().hex()) for directory in (base,parent,outside) for path in sorted(directory.iterdir()) if path.is_file() and not path.is_symlink()]
        row["scratch_count"]=sum(1 for path in base.rglob("*.tmp"))
        cases.append(row)
    return dict(cases=cases,source_sha256=hashlib.sha256((PYTHON_ROOT/"mini_loop"/"durable.py").read_bytes()).hexdigest())


def _publication_contracts(scratch: Path) -> dict:
    """Actual create-only source publisher, safe receipts and future snapshots."""
    from mini_loop.user_resources import UserResourceResolver, UserSkillPublicationError, canonical_user_skill
    from mini_loop.skills import SkillLoader
    from mini_loop.secrets import SecretRegistry
    cases = []
    def fields(name="review", description="Review safely", body="Read first."):
        return dict(name=name, description=description, body=body)
    def step(value=None, owner="alice"):
        return dict(owner=owner, fields=value or fields())
    def add(name, steps, secret_mode="", collision=False, seed=""):
        base=scratch/name;agent_dir=base/"agent";agent_dir.mkdir(parents=True)
        if collision:
            path=agent_dir/"review"/"SKILL.md";path.parent.mkdir()
            path.write_text(canonical_user_skill(**fields(body="agent policy")),encoding="utf-8")
        registry=None
        if secret_mode:
            registry=SecretRegistry()
            registry.register("TEST_SECRET", (lambda: None) if secret_mode=="unresolved" else "1234" if secret_mode=="short" else " secret-token " if secret_mode=="raw" else "secret-token")
        root=base/"users";agent=SkillLoader(agent_dir);resolver=UserResourceResolver(root,agent,registry)
        previous={}
        for item in steps:
            owner=item["owner"]
            if owner and owner not in previous: previous[owner]=resolver.for_owner(owner)
        target=root/resolver._owner_key("alice")/"skills"/"review"/"SKILL.md"
        victim=base/"victim";victim.mkdir()
        if seed=="alternate":
            path=target.parent.parent/"aaa"/"SKILL.md";path.parent.mkdir()
            path.write_text(canonical_user_skill(**fields()),encoding="utf-8")
            path.chmod(0o644)
        elif seed=="directory-link":target.parent.symlink_to(victim,target_is_directory=True)
        elif seed=="file-link":
            target.parent.mkdir();(victim/"SKILL.md").write_text("untouched",encoding="utf-8");target.symlink_to(victim/"SKILL.md")
        elif seed=="file-directory":target.mkdir(parents=True)
        elif seed:raise AssertionError(seed)
        results=[]
        for item in steps:
            row=dict(item);owner=item["owner"]
            try:
                publication=resolver.publish_skill(owner,item["fields"])
                resource=resolver.for_owner(owner)
                row.update(receipt=publication.as_dict(),future_descriptions=resource.skills.descriptions(),
                           memory_reused=resource.memory is previous[owner].memory,
                           old_descriptions=previous[owner].skills.descriptions(),
                           load_sha256=hashlib.sha256(resource.skills.load("user:"+item["fields"]["name"]).encode()).hexdigest())
            except UserSkillPublicationError as error:row["error"]=error.as_dict()
            results.append(row)
        files=[]
        for owner in previous:
            skill_root=root/resolver._owner_key(owner)/"skills"
            for path in sorted(skill_root.rglob("SKILL.md")):
                if path.is_symlink() or not path.is_file():continue
                files.append(dict(owner=owner,path=str(path.relative_to(skill_root)),sha256=hashlib.sha256(path.read_bytes()).hexdigest(),mode=path.stat().st_mode&0o777))
        cases.append(dict(name=name,steps=results,secret_mode=secret_mode,collision=collision,seed=seed,files=files,
                          victim_unchanged=not (victim/"SKILL.md").exists() or (victim/"SKILL.md").read_text()=="untouched"))
    add("create-retry-conflict",[step(),step(),step(fields(description="different")),step(fields(body="different"))])
    add("normalized",[step(fields(description="  spaced  ",body="\r\n  Read first.\rSecond line. \r\n")),
                      step(fields(description="spaced",body="Read first.\nSecond line."))])
    add("ordering",[step(fields(name="zulu")),step(fields(name="alpha"))])
    add("owners",[step(),step(fields(body="bob"),"bob"),step(fields(body="spaced")," alice ")])
    add("agent-collision",[step(),step()],collision=True)
    add("secret-body",[step(fields(body="secret-token"))],secret_mode="long")
    add("secret-name",[step(fields(name="secret-token"))],secret_mode="long")
    add("secret-description",[step(fields(description="secret-token"))],secret_mode="long")
    add("secret-before-normalization",[step(fields(body=" secret-token "))],secret_mode="raw")
    add("short-secret",[step()],secret_mode="short")
    add("unresolved-secret",[step()],secret_mode="unresolved")
    for seed in ("alternate","directory-link","file-link","file-directory"):add(seed,[step()],seed=seed)
    add("invalid",[step(owner=""),step(fields(name="Upper")),step(fields(description="")),step(fields(body=""))])
    return dict(cases=cases,source_sha256=hashlib.sha256((PYTHON_ROOT/"mini_loop"/"user_resources.py").read_bytes()).hexdigest())


def _user_session_resource_contracts(scratch: Path) -> dict:
    """Actual manager create/fork/teammate/SQLite restoration resource binding."""
    import asyncio
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.registry import ToolCall
    from mini_loop.skills import SkillLoader
    from mini_loop.storage import SQLiteStateStore
    from mini_loop.user_resources import UserResourceResolver, canonical_user_skill
    async def scenario():
        scratch.mkdir(parents=True);agent_dir=scratch/"agent";agent_dir.mkdir()
        path=agent_dir/"first"/"SKILL.md";path.parent.mkdir()
        path.write_text(canonical_user_skill("first","First","agent"),encoding="utf-8")
        agent=SkillLoader(agent_dir);root=scratch/"users";resolver=UserResourceResolver(root,agent)
        resolver.publish_skill("alice",dict(name="first",description="First",body="alice"))
        resolver.publish_skill("bob",dict(name="first",description="First",body="bob"))
        store=SQLiteStateStore(scratch/"state.db")
        settings=Settings(fake_llm=True,workspace_root=scratch/"workspaces",skills_dir=agent_dir,user_resources_root=None)
        manager=SessionManager(settings,FakeAsyncAnthropic(),skills=agent,user_resources=resolver,state_store=store)
        frames=[]
        async def capture(name,session,bound=True):
            events=[];original=session.agent.emit
            async def emit(event):events.append(event)
            session.agent.emit=emit
            loads=[]
            try:
                for value in ("agent:first","user:first","user:later","first"):
                    output=await session.agent._exec_tool(ToolCall("load_skill",dict(name=value),"probe"))
                    final=next(event for event in reversed(events) if event.get("type")=="tool_result")
                    loads.append(dict(input=dict(name=value),sha256=hashlib.sha256(output.encode()).hexdigest(),failed=bool(final["error"])))
            finally:session.agent.emit=original
            frames.append(dict(name=name,owner=session.owner,bound=bound,descriptions=session.agent.skills.descriptions(),loads=loads))
        try:
            alice=manager.create(owner="alice");bob=manager.create(owner="bob");anonymous=manager.create(owner="anonymous")
            await capture("alice-old",alice);await capture("bob",bob);await capture("anonymous",anonymous)
            resolver.publish_skill("alice",dict(name="later",description="Later",body="new"))
            await capture("alice-live",alice)
            fork=await manager.fork_session(alice.id);await capture("fork",fork)
            await manager.spawn_teammate(alice.id,"worker","implementer","stand by")
            teammate=next(item for item in manager.list() if item.id not in (alice.id,bob.id,anonymous.id,fork.id))
            await capture("teammate",teammate)
            memory_reused=fork.agent.state["memory"] is alice.agent.state["memory"]
            teammate_memory_pinned=teammate.agent.state["memory"] is alice.agent.state["memory"]
            manager.delete(bob.id)
            resources_retained=(root/resolver._owner_key("bob")/"skills"/"first"/"SKILL.md").exists()
        finally:await manager.stop()
        refreshed=UserResourceResolver(root,agent)
        restored_manager=SessionManager(settings,FakeAsyncAnthropic(),skills=agent,user_resources=refreshed,state_store=store)
        try:
            restored=restored_manager.restore_sessions()
            saved=next(item for item in restored if item.id==alice.id)
            await capture("restored-alice",saved)
            scheduled=restored_manager.restore_scheduled_session("missing")
            await capture("scheduled-anonymous",scheduled)
        finally:await restored_manager.stop();store.close()
        disabled=SessionManager(settings,FakeAsyncAnthropic(),skills=agent)
        try:await capture("disabled",disabled.create(owner="alice"),False)
        finally:await disabled.stop()
        return dict(frames=frames,memory_reused=memory_reused,teammate_memory_pinned=teammate_memory_pinned,
                    resources_retained=resources_retained)
    return asyncio.run(scenario())


def _memory_tool_contracts(scratch: Path) -> dict:
    """Execute actual owner-scoped memory tools through the source Agent gate."""
    import asyncio
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.memory import install_memory
    from mini_loop.registry import ToolRegistry, ToolCall
    registry = install_memory(ToolRegistry())
    specs = [
        ("alice", "recall", {}),
        ("alice", "remember", dict(name="same", content="alice café")),
        ("bob", "recall", {}),
        ("bob", "remember", dict(name="same", content="bob only", type="user", description="")),
        ("alice", "recall", dict(query=None)),
        ("bob", "recall", dict(query="bob")),
        ("alice", "remember", dict(name="same", content="replaced", type="feedback", description="updated")),
        ("alice", "remember", dict(name="<name & \"quote\" 'apostrophe'>", content="<raw body>", type="unknown", description=None)),
        ("alice", "remember", dict(name="nullable", content="null type", type=None)),
        ("alice", "recall", dict(query="raw body")),
        ("alice", "recall", dict(query="missing")),
        ("alice", "recall", {}),
        ("readonly", "remember", dict(name="blocked", content="must not persist")),
        ("readonly", "recall", {}),
    ]
    async def scenario():
        settings = Settings(fake_llm=True, enable_features=False, trajectory_enabled=False,
                            spill_dir=None, user_resources_root=None, workspace_root=scratch/"workspaces")
        manager = SessionManager(settings, FakeAsyncAnthropic())
        sessions = {owner: manager.create(owner=owner, permission_mode="readonly" if owner == "readonly" else "auto")
                    for owner in ("alice", "bob", "readonly")}
        events = {}
        for owner, session in sessions.items():
            install_memory(session.agent.tools)
            events[owner] = []
            async def emit(event, owner=owner): events[owner].append(event)
            session.agent.emit = emit
        steps = []
        try:
            for index, (owner, name, value) in enumerate(specs):
                output = await sessions[owner].agent._exec_tool(ToolCall(name, value, f"memory-{index}"))
                event = next(e for e in reversed(events[owner]) if e.get("type") == "tool_result")
                steps.append(dict(owner=owner, name=name, input=value, output=output,
                                  canonical=json.dumps(value,sort_keys=True,ensure_ascii=False,separators=(',',':')),
                                  failed=bool(event["error"]), denied=bool(event.get("denied", False))))
        finally: await manager.stop()
        return steps
    return dict(schemas=registry.schemas(), metadata=[dict(name=t.name,risk=t.risk,readonly=t.readonly,
                parallel_safe=t.parallel_safe,capabilities=sorted(t.capabilities))
                for name in registry.names() if (t:=registry.get(name))], steps=asyncio.run(scenario()))


def _memory_context_contracts(scratch: Path) -> dict:
    """Actual selection side requests, source wrappers and change-only facts."""
    import asyncio
    import copy
    from types import SimpleNamespace
    from mini_loop.agent import Agent
    from mini_loop.caching import NullCachePolicy, runtime_facts_injector
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeMessage, FakeUsage, text
    from mini_loop.memory import MemoryStore, install_memory, prepare_memory_context
    from mini_loop.registry import ToolRegistry
    recipes = [
        dict(name="selected", reply="[2,0]", query="unrelated"),
        dict(name="duplicates-bools-and-floats", reply='prefix [true,false,1.0,"2",{},-1,99,2,2,0,1] suffix', query="unrelated"),
        dict(name="empty-selection-fallback", reply="[]", query="beta"),
        dict(name="malformed-fallback", reply="not JSON", query="gamma"),
        dict(name="fault-fallback", reply="", query="alpha", fault=True),
        dict(name="no-hits", reply="[]", query="no match"),
        dict(name="empty-store", reply="[0]", query="alpha", empty=True),
        dict(name="automatic-disabled", reply="[0]", query="alpha", auto=False),
        dict(name="recall-missing", reply="[0]", query="alpha", recall=False),
        dict(name="remember-missing", reply="[0]", query="alpha", remember=False),
        dict(name="unicode-query-tail", reply="[1]", query="前"*4001+"tail"),
        dict(name="non-array-brackets", reply='[0] [2]', query="alpha"),
    ]
    async def scenario(row):
        root=scratch/row["name"];root.mkdir(parents=True)
        store=MemoryStore(root/"memory")
        store.write("foreign","project","foreign private","foreign memory",owner="foreign")
        if not row.get("empty",False):
            for name,kind,origin in (("alpha","project","explicit"),("beta","feedback","imported"),("gamma","reference","auto_extracted")):
                store.write(name,kind,name+" facts",name+" body",owner="owner",origin=origin)
        calls,events=[],[]
        async def create(**kwargs):
            calls.append(copy.deepcopy(kwargs))
            if row.get("fault",False):raise RuntimeError("selection failed")
            return FakeMessage([text(row["reply"])],"end_turn",FakeUsage(777,3),model="served-memory")
        async def emit(event):events.append(copy.deepcopy(event))
        registry=install_memory(ToolRegistry())
        if not row.get("recall",True):registry.unregister("recall")
        if not row.get("remember",True):registry.unregister("remember")
        agent=Agent(client=SimpleNamespace(messages=SimpleNamespace(create=create)),workspace=root,tools=registry,emit=emit,
                    cache_policy=NullCachePolicy(),state=dict(memory=store,resource_owner="owner",memory_auto=row.get("auto",True)),
                    settings=Settings(fake_llm=True,spill_dir=None,skills_dir=root/"empty"))
        meter=agent.token_meter.snapshot()
        prepared=await prepare_memory_context(agent,row["query"])
        assert agent.token_meter.snapshot()==meter
        facts=[]
        for _ in range(2):facts.extend(await runtime_facts_injector(agent) or [])
        return {**row,"fault":row.get("fault",False),"empty":row.get("empty",False),"auto":row.get("auto",True),
                "recall":row.get("recall",True),"remember":row.get("remember",True),"prepared":prepared,"calls":calls,
                "events":[{k:e[k] for k in ("action","count")} for e in events if e["type"]=="memory"],"facts":facts}
    async def run():return [await scenario(row) for row in recipes]
    return dict(cases=asyncio.run(run()))


def _memory_extraction_contracts(scratch: Path) -> dict:
    """Actual extraction through Agent._create, with incremental owner writes."""
    import asyncio
    import copy
    from types import SimpleNamespace
    from mini_loop.agent import Agent
    from mini_loop.caching import NullCachePolicy
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeMessage, FakeUsage, text
    from mini_loop.memory import MemoryStore, ScopedMemory, extract_memories
    base = [
        {"role":"user", "content":"<memory_context>\nrecalled private\n</memory_context>\n\nRemember café 用户"},
        {"role":"user", "content":"<runtime-state>\nprivate index\n</runtime-state>"},
        {"role":"assistant", "content":[
            {"type":"thinking","thinking":"durable reasoning","signature":"signed"},
            {"type":"tool_use","id":"call","name":"bash","input":{"command":"echo relevant"}},
            {"type":"text","text":"<runtime-state>\nexclude\n</runtime-state>"},
            {"type":"text","text":"actual assistant fact"}]},
        {"role":"user", "content":[{"type":"tool_result","tool_use_id":"call","content":"private tool result"},
                                      {"type":"text","text":"actual user fact"}]},
        {"role":"user", "content":""},
    ]
    valid = dict(name="new", type="feedback", description="durable", body="learned")
    recipes = [
        dict(name="valid", reply=json.dumps([valid])),
        dict(name="ignored-authority-fields", reply=json.dumps([{**valid,"owner":"foreign","origin":"explicit","root":"outside"}])),
        dict(name="defaults", reply='[{"name":"minimal"}]'),
        dict(name="unknown-type", reply='[{"name":"unknown","type":"custom","body":"fact"}]'),
        dict(name="max-five", reply=json.dumps([{**valid,"name":f"fact-{i}"} for i in range(7)])),
        dict(name="partial-write-missing-name", reply=json.dumps([valid,dict(body="invalid"),dict(name="never")])),
        dict(name="partial-write-nonobject", reply=json.dumps([valid,7,dict(name="never")])),
        dict(name="empty", reply="[]"), dict(name="malformed",reply="not JSON"),
        dict(name="multiple-arrays",reply="[{}] [1]"),
        dict(name="provider-fault",reply="[]",fault=True),
        dict(name="unicode-tail",reply="[]",tail=True),
        dict(name="greedy-memory-prefix",reply="[]",greedy=True),
    ]
    async def scenario(row):
        root=scratch/row["name"]; root.mkdir(parents=True)
        raw=MemoryStore(root/"memory"); store=ScopedMemory(raw,"owner")
        raw.write("foreign","project","foreign description","foreign secret",owner="foreign")
        store.write("existing","project","known","seed",origin="explicit")
        calls, events = [], []
        async def create(**kwargs):
            calls.append(copy.deepcopy(kwargs))
            if row.get("fault",False): raise RuntimeError("extraction failed")
            # Concatenation of text blocks, rather than joining with newlines.
            middle=len(row["reply"])//2
            return FakeMessage([text("prefix "+row["reply"][:middle]),text(row["reply"][middle:]+" suffix")],
                               "end_turn",FakeUsage(777,3),model="served-memory")
        async def emit(event): events.append(copy.deepcopy(event))
        agent=Agent(client=SimpleNamespace(messages=SimpleNamespace(create=create)),workspace=root,emit=emit,
                    cache_policy=NullCachePolicy(),state=dict(memory=raw,resource_owner="owner"),
                    settings=Settings(fake_llm=True,spill_dir=None,skills_dir=root/"empty"))
        messages=copy.deepcopy(base)
        if row.get("tail",False): messages=[{"role":"user","content":"字"*7000+"tail"}]
        if row.get("greedy",False): messages=[{"role":"user","content":"<memory_context>\none\n</memory_context>\n\nbetween\n</memory_context>\n\nkeep"}]
        agent.messages=copy.deepcopy(messages)
        before=agent.token_meter.snapshot()
        count=await extract_memories(store,list(agent.messages),agent.client,agent.settings.model,create=agent._create)
        assert agent.token_meter.snapshot()==before
        assert agent.messages==messages
        return {**row,"fault":row.get("fault",False),"messages":messages,"calls":calls,"count":count,
                "records":store.list(),"foreign":ScopedMemory(raw,"foreign").list(),
                "purposes":[e["purpose"] for e in events if e["type"]=="model_start"]}
    async def run(): return [await scenario(row) for row in recipes]
    return dict(cases=asyncio.run(run()))


def _memory_consolidation_contracts(scratch: Path) -> dict:
    """Actual scoped consolidation threshold, request and origin identity."""
    import asyncio
    import copy
    from types import SimpleNamespace
    from mini_loop.agent import Agent
    from mini_loop.caching import NullCachePolicy
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeMessage, FakeUsage, text
    from mini_loop.memory import MemoryStore, ScopedMemory, consolidate_memories
    recipes=[dict(name="below-threshold",seed=9,mode="changed"),
             dict(name="threshold-and-origins",seed=10,mode="changed"),
             dict(name="all-unchanged",seed=12,mode="same"),
             dict(name="ignored-nonobjects",seed=10,mode="skip"),
             dict(name="no-valid-entries",seed=10,mode="invalid"),
             dict(name="empty-array",seed=10,mode="empty"),
             dict(name="malformed",seed=10,mode="malformed"),
             dict(name="provider-fault",seed=10,mode="changed",fault=True),
             dict(name="changed-type",seed=10,mode="type"),
             dict(name="absent-defaults",seed=10,mode="defaults")]
    async def scenario(row):
        root=scratch/row["name"];root.mkdir(parents=True)
        raw=MemoryStore(root/"memory");store=ScopedMemory(raw,"owner")
        raw.write("foreign","project","foreign description","foreign secret",owner="foreign")
        origins=("explicit","auto_extracted","imported","consolidated")
        for i in range(row["seed"]):store.write(f"fact-{i}","project",f"description-{i} 中文",f"body-{i} café",origin=origins[i%4])
        before=store.list(); indexed={r["name"]:r for r in before}
        mode=row["mode"]
        if mode=="same":items=copy.deepcopy(before)
        elif mode=="skip":items=[None,7,{},dict(body="missing"),copy.deepcopy(indexed["fact-0"])]
        elif mode=="invalid":items=[None,7,{},dict(body="missing")]
        elif mode=="empty":items=[]
        elif mode=="defaults":items=[dict(name="minimal")]
        elif mode=="type":items=[{**indexed["fact-0"],"type":"unknown"}]
        else:items=[copy.deepcopy(indexed["fact-0"]),{**indexed["fact-1"],"body":"changed"},dict(name="new",body="merged",owner="foreign",origin="explicit")]
        reply="not JSON" if mode=="malformed" else json.dumps(items,ensure_ascii=False)
        calls,events=[],[]
        async def create(**kwargs):
            calls.append(copy.deepcopy(kwargs))
            if row.get("fault",False):raise RuntimeError("consolidation failed")
            return FakeMessage([text(reply)],"end_turn",FakeUsage(888,4),model="served-memory")
        async def emit(event):events.append(copy.deepcopy(event))
        agent=Agent(client=SimpleNamespace(messages=SimpleNamespace(create=create)),workspace=root,emit=emit,
                    cache_policy=NullCachePolicy(),state=dict(memory=raw,resource_owner="owner"),
                    settings=Settings(fake_llm=True,spill_dir=None,skills_dir=root/"empty"))
        count=await consolidate_memories(store,agent)
        return {**row,"fault":row.get("fault",False),"reply":reply,"before":before,"calls":calls,"count":count,
                "records":store.list(),"foreign":ScopedMemory(raw,"foreign").list(),
                "purposes":[e["purpose"] for e in events if e["type"]=="model_start"]}
    async def run():return [await scenario(row) for row in recipes]
    return dict(cases=asyncio.run(run()))


def _memory_capture_contracts(scratch: Path) -> dict:
    """Actual Agent endpoint capture, including the source tool-halt omission."""
    import asyncio
    import copy
    from types import SimpleNamespace
    from mini_loop.agent import Agent
    from mini_loop.caching import NullCachePolicy
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeMessage, FakeUsage, text, tool
    from mini_loop.memory import MemoryStore, ScopedMemory, install_memory
    from mini_loop.registry import ToolRegistry, Tool, Hooks
    from mini_loop.stuck import DefaultStuckDetector, StuckThresholds, NullStuckDetector
    class Resume(Hooks):
        async def stop(self,*args):return "keep going"
    recipes=[dict(name=k) for k in ("normal","exhaustion","resume-halt","tool-halt","provider-error","readonly","disabled","no-pair","cancel","capture-error","threshold-after-extract")]
    async def scenario(row):
        root=scratch/row["name"];root.mkdir(parents=True)
        raw=MemoryStore(root/"memory");store=ScopedMemory(raw,"owner")
        raw.write("foreign","project","foreign description","foreign secret",owner="foreign")
        if row["name"]=="threshold-after-extract":
            for i in range(9):store.write(f"seed-{i}","project",f"seed-{i}","original",origin="explicit")
        registry=install_memory(ToolRegistry())
        async def same(ctx,command):return "same"
        registry.register(Tool("bash","shell",{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]},same,risk="exec"))
        if row["name"]=="no-pair":registry.unregister("remember")
        calls,events=[],[]
        original_list=raw.list
        async def create(**kwargs):
            calls.append(copy.deepcopy(kwargs));budget=kwargs["max_tokens"]
            if budget==200:content=[text("[0]")];reason="end_turn"
            elif budget==1500:content=[text('[{"name":"captured","body":"fact"}]')];reason="end_turn"
            elif budget==2500:content=[text('[{"name":"merged","body":"fact"}]')];reason="end_turn"
            else:
                if row["name"]=="provider-error":raise RuntimeError("main provider failed")
                if row["name"]=="cancel":raise asyncio.CancelledError()
                if row["name"]=="capture-error":
                    def broken(*args,**kw):raise RuntimeError("capture listing failed")
                    raw.list=broken
                if row["name"] in ("exhaustion","tool-halt"):content=[tool("bash",_id=f"u{len(calls)}",command="same")];reason="tool_use"
                else:content=[text("done")];reason="end_turn"
            return FakeMessage(content,reason,FakeUsage(1234,2),model="served-memory")
        async def emit(event):events.append(copy.deepcopy(event))
        detector=DefaultStuckDetector(StuckThresholds(monologue=2,repeat_action_result=2,max_nudges=0)) if row["name"] in ("resume-halt","tool-halt") else NullStuckDetector()
        agent=Agent(client=SimpleNamespace(messages=SimpleNamespace(create=create)),workspace=root,emit=emit,
                    tools=registry,hooks=Resume() if row["name"]=="resume-halt" else Hooks(),stuck_detector=detector,
                    cache_policy=NullCachePolicy(),max_rounds=2,
                    state=dict(memory=raw,resource_owner="owner",memory_auto=row["name"]!="disabled",permission_mode="readonly" if row["name"]=="readonly" else "auto"),
                    settings=Settings(fake_llm=True,spill_dir=None,skills_dir=root/"empty"))
        error=False
        try:await agent.run("learn")
        except (Exception,asyncio.CancelledError):error=True
        raw.list=original_list
        projected=[]
        for event in events:
            if event["type"]=="memory":projected.append({k:event[k] for k in ("action","count","consolidated") if k in event})
        return {**row,"budgets":[c["max_tokens"] for c in calls],"error":error,"records":store.list(),
                "foreign":ScopedMemory(raw,"foreign").list(),"memory":projected,
                "capture_errors":sum(e["type"]=="memory_capture_error" for e in events),
                "purposes":[e["purpose"] for e in events if e["type"]=="model_start"]}
    async def run():return [await scenario(row) for row in recipes]
    return dict(cases=asyncio.run(run()))


def _launcher_memory_contracts(scratch: Path) -> dict:
    """Actual source default roots, eager failures and default tool selection."""
    import asyncio
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager

    async def run():
        rows = []
        cases = [
            ("default-shared", False, False, ""),
            ("configured-shared", True, False, ""),
            ("owner-local", False, True, ""),
            ("owner-and-shared", True, True, ""),
            ("shared-file", True, False, "memory"),
            ("shared-file-with-owner", True, True, "memory"),
            ("owner-file", False, True, "users"),
        ]
        for name, configured, owner_local, blocked in cases:
            base = scratch / name
            base.mkdir(parents=True)
            memory_root = base / "memory" if configured else base / "workspaces" / ".memory"
            users_root = base / "users"
            if blocked:
                path = memory_root if blocked == "memory" else users_root
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text("root is a regular file", encoding="utf-8")
            settings = Settings(fake_llm=True, workspace_root=base / "workspaces",
                                trajectory_enabled=False, spill_dir=None,
                                memory_root=memory_root if configured else None,
                                user_resources_root=users_root if owner_local else None)
            manager = None
            row = dict(name=name, configured_shared=configured, owner_local=owner_local,
                       blocked=blocked)
            try:
                manager = SessionManager(settings, FakeAsyncAnthropic())
                row["started"] = True
                row["backend"] = "owner-local" if manager.user_resources is not None else "shared"
                row["shared_path"] = manager.memory.dir.relative_to(base).as_posix()
                session = manager.create(owner="alice")
                row["memory_tools"] = [n for n in session.agent.tools.names() if n in ("remember", "recall")]
                row["users_mode"] = (users_root.stat().st_mode & 0o777) if owner_local else None
            except OSError:
                row["started"] = False
            finally:
                if manager is not None:
                    await manager.stop()
            row["shared_directory"] = memory_root.is_dir()
            row["users_directory"] = users_root.is_dir()
            rows.append(row)
        return rows

    return dict(cases=asyncio.run(run()),
                source_sha256=hashlib.sha256((PYTHON_ROOT / "mini_loop" / "manager.py").read_bytes()).hexdigest())


def _draft_store_contracts() -> dict:
    """Actual bounded draft operations; UUIDs normalize to stable recipe labels."""
    from dataclasses import replace
    from mini_loop.skill_capture import PersonalSkillDraftStore, PersonalSkillError
    recipes = [
        dict(name="quotas-authority-expiry", ttl=10, max_items=4, max_per_owner=3, max_per_session=2, start=100,
             operations=[
                 ("add","a1","alice","s1"),("add","a2","alice","s1"),("get","a1","alice","s1"),
                 ("add","a3","alice","s1"),("get","a1","alice","s1"),
                 ("get","a2","bob","s1"),("get","a2","alice","s2"),("wrong-digest","a2","alice","s1"),
                 ("add","a4","alice","s2"),("add","a5","alice","s3"),("get","a2","alice","s1"),
                 ("add","b1","bob","s1"),("add","c1","carol","s1"),("add","b2","bob","s1"),
                 ("get","b1","bob","s1"),("wrong-consume","a3","alice","s1"),("get","a3","alice","s1"),
                 ("consume","a3","alice","s1"),("peek","a3","alice","s1"),
                 ("advance","", "", ""),("get","b2","alice","s1"),("peek","b2","bob","s1"),
                 ("peek","b2","bob","s1"),("add","c2","carol","s1"),("discard","a4","","")]),
        dict(name="identity-and-normalization", ttl=5, max_items=4, max_per_owner=None, max_per_session=4, start=0,
             operations=[("add","first"," alice ","s"),("get","first","alice","s"),
                         ("advance","","",""),("discard","first","",""),("get","first"," alice ","s"),
                         ("add","second","alice","s"),("discard-clone","second","",""),("peek","second","alice","s"),
                         ("consume","second","alice","s"),("discard","second","",""),
                         ("add","no-owner","","s"),("add","no-session","alice",""),
                         ("invalid-name","bad","alice","s"),("invalid-coverage","bad","alice","s"),
                         ("negative-omitted","bad","alice","s")]),
    ]
    result = []
    for recipe in recipes:
        clock = [float(recipe["start"])]
        store = PersonalSkillDraftStore(ttl_seconds=recipe["ttl"], max_items=recipe["max_items"],
                                        max_per_owner=recipe["max_per_owner"], max_per_session=recipe["max_per_session"], clock=lambda: clock[0])
        handles = {}; steps = []
        for action, ref, owner, session in recipe["operations"]:
            step = dict(action=action, ref=ref, owner=owner, session=session)
            try:
                draft = None
                if action in ("add", "invalid-name", "invalid-coverage", "negative-omitted"):
                    fields = dict(name="reviewed-skill" if action != "invalid-name" else "Bad Name", description="  Review workflow  ",
                                  body="  One.\r\nTwo.\rThree.  ", evidence_indexes=[-1,0,0,2],
                                  coverage="unknown" if action == "invalid-coverage" else "authenticated_turns_tail",
                                  omitted=-1 if action == "negative-omitted" else 3, compacted_history_excluded=True)
                    step["input"] = fields
                    draft = store.add(owner=owner, session_id=session, **fields)
                    handles[ref] = draft
                elif action == "advance":
                    clock[0] += recipe["ttl"]
                elif action in ("discard", "discard-clone"):
                    draft = handles[ref]
                    step["discarded"] = store.discard_committed(replace(draft) if action == "discard-clone" else draft)
                    draft = None
                else:
                    draft_id = handles[ref].draft_id if ref in handles else ref
                    digest = "0" * 64 if action in ("wrong-digest","wrong-consume") else handles[ref].digest if action == "consume" else None
                    if action in ("consume", "wrong-consume"):
                        draft = store.consume(draft_id, owner=owner, session_id=session, digest=digest)
                    else:
                        method = store.peek if action == "peek" else store.get
                        draft = method(draft_id, owner=owner, session_id=session, digest=digest)
                if draft is not None:
                    public = draft.public_dict(); public["draft_id"] = ref
                    step["preview"] = public
            except PersonalSkillError as error:
                step["error"] = dict(code=error.code, status=error.status_code, message=str(error))
            steps.append(step)
        result.append(dict(config={key:recipe[key] for key in ("name","ttl","max_items","max_per_owner","max_per_session","start")}, steps=steps))
    defaults = PersonalSkillDraftStore()
    return dict(cases=result, defaults=dict(ttl=defaults.ttl_seconds,max_items=defaults.max_items,max_per_owner=defaults.max_per_owner,max_per_session=defaults.max_per_session),
                source_sha256=hashlib.sha256((PYTHON_ROOT / "mini_loop" / "skill_capture.py").read_bytes()).hexdigest())


def _skill_projection_contracts() -> dict:
    """Actual legacy/provenance projections with compact Unicode JSON budgets."""
    import copy
    from mini_loop.skill_capture import project_session_messages, project_authenticated_turns
    from mini_loop.secrets import SecretRegistry
    cases = []
    def add(name, mode, messages, *, limit=40000, repeat=1, prior=0, compacted=False, values=(), minimum=8):
        registry = SecretRegistry(min_length=minimum)
        for index,value in enumerate(values): registry.register(f"VALUE{index}",value)
        expanded = copy.deepcopy(messages)
        for row in expanded:
            if isinstance(row["content"],str): row["content"] *= repeat
            else:
                for block in row["content"]:
                    if block["type"] == "text": block["text"] *= repeat
        if mode == "authenticated":
            result = project_authenticated_turns(expanded,registry,max_chars=limit,prior_omitted=prior,compacted_history_excluded=compacted)
        else:
            result = project_session_messages(expanded,registry,max_chars=limit)
        encoded = json.dumps(result,ensure_ascii=False,separators=(",",":"),sort_keys=True)
        cases.append(dict(name=name,mode=mode,messages=messages,max_chars=limit,repeat=repeat,prior_omitted=prior,compacted=compacted,values=list(values),minimum=minimum,
                          sha256=hashlib.sha256(encoded.encode()).hexdigest(),count=len(result["messages"]),coverage=result["coverage"],omitted=result["omitted"],compacted_result=result["compacted_history_excluded"]))
    def row(role,text): return dict(role=role,content=text)
    add("ordinary","legacy",[row("user","  Human  "),row("assistant"," Answer "),row("system","[context compressed hidden]")])
    wrappers=["<runtime-state x='1'>hidden", "<task_notification>hidden", "<team_inbox>hidden", "<workflow_next>hidden", "[cron task]hidden", "[scheduled cron task]hidden", "[goal round 1]hidden", "[turn interrupted]hidden", "[error]hidden", "[stopped]hidden", "[context compressed]hidden", "[snipped]hidden"]
    for index,text in enumerate(wrappers): add(f"injected-{index}","legacy",[row("user",text),row("assistant","kept")])
    add("greedy-memory","legacy",[row("user","<memory_context>\nsecret\n</memory_context>\nforged close secret\n</memory_context>\nHuman")])
    add("malformed-memory","legacy",[row("user","<memory_context>inline</memory_context>Human"),row("assistant","<memory_context>\nmissing")])
    add("interjection","legacy",[row("user"," <USER_INTERJECTION>\n  Human  \n</USER_INTERJECTION> "),row("assistant","<user_interjection>\n[stopped]internal\n</user_interjection>")])
    add("unicode-i-and-space","legacy",[row("user","\x1c<USER_İNTERJECTİON>\nHuman\n</USER_İNTERJECTİON>\u0085"),row("assistant","\u2003<team_ınbox>internal")])
    add("unicode-word-boundaries","legacy",[row("user","[cron你]human"),row("user","[snippedé]human"),row("user","[stopped_]human"),row("user","[snipped\U0001e4d0]gap"),row("assistant","[context compressedly]human")])
    add("user-arrays-and-assistant-blocks","legacy",[row("user",[dict(type="text",text="user protocol"),dict(type="tool_result",tool_use_id="x",content="private tool")]),row("assistant",[dict(type="text",text="one"),dict(type="thinking",thinking="private reasoning",signature="sig"),dict(type="text",text="[snipped]hidden"),dict(type="text",text="two")])])
    add("masked-body","legacy",[row("user","projection-private-secret"),row("assistant","projection-private-secret and safe")],values=["projection-private-secret"])
    add("authenticated-verbatim","authenticated",[row("user","  <runtime-state>human  "),row("assistant","<memory_context>human"),row("user",""),row("user","   "),row("system","ignored")])
    add("authenticated-metadata","authenticated",[row("user","human"),row("assistant","answer")],prior=7,compacted=True)
    add("masked-role-label","authenticated",[row("user","human"),row("assistant","safe")],values=["assistant"])
    add("masked-fixed-keys","authenticated",[row("user","content is data"),row("assistant","safe")],values=["role","content","assistant"],minimum=1)
    for mode in ("legacy","authenticated"):
        add(mode+"-unicode-budget",mode,[row("user","界🌱"),row("assistant","界🌱"),row("user","界🌱")],repeat=9000)
        add(mode+"-escaped-budget",mode,[row("user","\x01")],repeat=9000)
        add(mode+"-mask-before-budget",mode,[row("user","projection-private-secret")],limit=60,values=["projection-private-secret"])
        exact=len(json.dumps([row("user","界🌱<>&\u2028\u2029")],ensure_ascii=False,separators=(",",":"),sort_keys=True))
        add(mode+"-exact",mode,[row("user","界🌱<>&\u2028\u2029")],limit=exact)
        add(mode+"-one-short",mode,[row("user","界🌱<>&\u2028\u2029")],limit=exact-1)
        add(mode+"-cap",mode,[row("user","界🌱"),row("assistant","界🌱"),row("user","界🌱")],limit=99999,repeat=9000)
        add(mode+"-tiny",mode,[row("user","a")],limit=1)
    return dict(cases=cases,source_sha256=hashlib.sha256((PYTHON_ROOT/"mini_loop"/"skill_capture.py").read_bytes()).hexdigest())


def _skill_capture_contracts() -> dict:
    """Real source recorder transitions and their authenticated projections."""
    from types import SimpleNamespace
    from mini_loop.skill_capture import record_personal_skill_turn, project_authenticated_turns
    from mini_loop.secrets import SecretRegistry
    cases = []

    def add(name, steps, *, values=(), minimum=8, flavor="registry"):
        registry = SecretRegistry(min_length=minimum)
        for index, value in enumerate(values):
            registry.register(f"VALUE{index}", value)
        if flavor == "unresolved":
            registry.register("missing", lambda: None)
        elif flavor in ("missing_reports", "crashing_reports"):
            class Reports:
                def names(self): return ("registered",)
                def mask_payload(self, value): return value
            if flavor == "crashing_reports":
                def fail(): raise RuntimeError("private screening failure")
                Reports.unresolved = staticmethod(fail)
                Reports.short_values = staticmethod(lambda: ())
            registry = Reports()
        agent = SimpleNamespace(state={}, secrets=registry, messages=[])
        results = []
        for step in steps:
            if step.get("recover"):
                registry.register("VALUE0", "healthy-replacement-secret")
            agent.messages = step.get("history", [])
            for _ in range(step.get("turns", 1)):
                record_personal_skill_turn(agent, step["user"] * step.get("repeat", 1), step["final"] * step.get("repeat", 1))
            state = dict(established="personal_skill_turns" in agent.state,
                         messages=agent.state.get("personal_skill_turns", []),
                         omitted=agent.state.get("personal_skill_turns_omitted", 0),
                         compacted_history_excluded=agent.state.get("personal_skill_compacted_history_excluded", False),
                         error=agent.state.get("personal_skill_capture_error", ""))
            digest = lambda value: hashlib.sha256(json.dumps(value, ensure_ascii=False, separators=(",", ":"), sort_keys=True).encode()).hexdigest()
            result = dict(sha256=digest(state), count=len(state["messages"]), omitted=state["omitted"], error=state["error"])
            if not state["error"]:
                projected = project_authenticated_turns(state["messages"], registry, prior_omitted=state["omitted"], compacted_history_excluded=state["compacted_history_excluded"])
                result["projection_sha256"] = digest(projected)
            results.append(result)
        cases.append(dict(name=name, steps=steps, values=list(values), minimum=minimum, flavor=flavor, results=results))

    def step(user="human", final="answer", **extra): return dict(user=user, final=final, **extra)
    add("ordinary-and-trim", [step("\x1c Human \u0085", "\u2003 Answer \u2029"), step("next", "next answer")])
    add("empty-pairs", [step(" ", "answer"), step("human", "\u2003"), step()])
    add("message-count", [step(turns=40)])
    add("unicode-budget", [step("界🌱", "界🌱", repeat=9000)])
    add("escaped-budget", [step("\x01", "answer", repeat=9000)])
    add("oversized-pair", [step("界🌱", "界🌱", repeat=20001)])
    add("one-message-eviction", [step("界🌱", "a", repeat=15000)])
    add("memory-looking-human", [step("<memory_context>human", "<runtime-state>answer")])
    add("sticky-gap", [step(history=[dict(role="system", content="[Context compressed here]")]), step()])
    add("unicode-gap", [step(history=[dict(role="user", content="[snippedé]human")]), step(history=[dict(role="user", content="[snipped\U0001e4d0]gap")])])
    add("block-gap-ignored", [step(history=[dict(role="assistant", content=[dict(type="text", text="[snipped]gap")])])])
    add("body-masking", [step("capture-private-secret", "capture-private-secret and answer")], values=["capture-private-secret"])
    add("role-label-masking", [step()], values=["assistant"])
    add("role-key-masking", [step()], values=["role"], minimum=1)
    add("content-key-masking", [step()], values=["content"], minimum=1)
    add("key-collision", [step()], values=["role", "content"], minimum=1)
    add("short-sticky-recovery", [step(), step(recover=True)], values=["tiny"])
    add("unresolved", [step()], flavor="unresolved")
    add("missing-reports", [step()], flavor="missing_reports")
    add("crashing-reports", [step()], flavor="crashing_reports")
    return dict(cases=cases, source_sha256=hashlib.sha256((PYTHON_ROOT / "mini_loop" / "skill_capture.py").read_bytes()).hexdigest())


def _skill_candidate_contracts() -> dict:
    """Actual parser outcomes, including source validation order and JSON lexemes."""
    import sys
    from mini_loop.skill_capture import _parse_candidate
    from mini_loop.secrets import SecretRegistry
    cases = []
    base = dict(schema="mini-loop.personal-skill-draft/v1", decision="create", description=" Useful recipe ", body=" Do this.\r\nThen that. ", evidence_indexes=[1, 0])

    def add(name, fields=None, *, raw=None, requested_name="recipe", count=3, values=(), minimum=8, repeat_text="", repeat_count=0):
        if raw is None:
            raw = json.dumps(base if fields is None else fields, ensure_ascii=False, separators=(",", ":"))
        expanded = raw.replace("__REPEAT__", repeat_text * repeat_count) if repeat_count else raw
        registry = SecretRegistry(min_length=minimum)
        for index, value in enumerate(values):
            registry.register(f"VALUE{index}", value)
        expected = dict(error="", decision="", sha256="")
        try:
            parsed = _parse_candidate(expanded, name=requested_name, message_count=count, secrets=registry)
        except ValueError as error:
            expected["error"] = str(error)
        else:
            if parsed == "skip":
                result = dict(decision="skip", description="", body="", evidence_indexes=[])
            else:
                description, body, evidence = parsed
                result = dict(decision="create", description=description, body=body, evidence_indexes=list(evidence))
            expected["decision"] = result["decision"]
            expected["sha256"] = hashlib.sha256(json.dumps(result, ensure_ascii=False, separators=(",", ":"), sort_keys=True).encode()).hexdigest()
        cases.append(dict(name=name, raw=raw, requested_name=requested_name, message_count=count, values=list(values), minimum=minimum, repeat_text=repeat_text, repeat_count=repeat_count, expected=expected))

    add("create-original-fields")
    add("unicode-outer-trim", raw="\x1c" + json.dumps(base) + "\u0085")
    add("skip", {**base, "decision":"skip", "description":"", "body":"", "evidence_indexes":[]}, requested_name="../invalid")
    for name, raw in (("malformed", "{private malformed"), ("fenced", "```json\n{}\n```"), ("trailing", "{} {}"), ("root-list", "[]"), ("root-null", "null"), ("root-string", '"private"'), ("root-nan", "NaN")):
        add(name, raw=raw)
    add("missing-field", {key:value for key,value in base.items() if key != "body"})
    add("extra-field", {**base, "extra":"candidate-private-secret"}, values=["candidate-private-secret"])
    add("sensitive-body", {**base, "body":"candidate-private-secret"}, values=["candidate-private-secret"])
    add("sensitive-nested-before-schema", {**base, "description":{"nested":"candidate-private-secret"}}, values=["candidate-private-secret"])
    add("sensitive-nested-key-before-evidence", {**base, "evidence_indexes":[{"candidate-private-secret":"value"}]}, values=["candidate-private-secret"])
    add("sensitive-fixed-key", values=["body"], minimum=1)
    add("sensitive-decision-before-schema", {**base, "schema":"wrong"}, values=["create"], minimum=1)
    add("schema-wrong", {**base, "schema":"wrong"})
    add("schema-null", {**base, "schema":None})
    add("decision-wrong", {**base, "decision":"publish"})
    add("decision-list", {**base, "decision":[]})
    add("description-null", {**base, "description":None})
    add("body-object", {**base, "body":{"private":"value"}})
    for name, evidence in (("null",None), ("object",{}), ("empty",[]), ("bool",[True]), ("float",[0.0]), ("exponent",[1e1]), ("string",["0"]), ("negative",[-1]), ("out-of-range",[3]), ("duplicate",[0,0]), ("big-int",[10**100])):
        add("evidence-"+name, {**base, "evidence_indexes":evidence})
    for name, over in (("description",dict(description=" ")), ("body",dict(body="x")), ("evidence",dict(evidence_indexes=[0])), ("big-int",dict(evidence_indexes=[10**100])), ("bool",dict(evidence_indexes=[False]))):
        add("skip-"+name, {**base, "decision":"skip", "description":"", "body":"", "evidence_indexes":[], **over})
    for name, over in (("name",{}), ("description-line",dict(description="two\nlines")), ("empty-description",dict(description=" ")), ("empty-body",dict(body=" ")), ("nul",dict(body="\0")), ("wrapper",dict(body="<SKİLL>private</SKİLL>"))):
        add("invalid-skill-"+name, {**base, **over}, requested_name="../bad" if name == "name" else "recipe")
    add("description-limit", {**base,"description":"__REPEAT__"}, repeat_text="x", repeat_count=201)
    add("body-limit", {**base,"body":"__REPEAT__"}, repeat_text="x", repeat_count=50001)
    add("body-boundary", {**base,"body":"__REPEAT__"}, repeat_text="x", repeat_count=50000)
    add("line-limit", {**base,"body":"__REPEAT__"}, repeat_text="x\\n", repeat_count=501)
    add("short-registry-not-parser-health", values=["tiny"])
    ordinary = json.dumps(base, separators=(",", ":"))
    add("duplicate-last-wins", raw=ordinary[:-1]+',"body":"final body"}')
    add("duplicate-discarded-secret", raw='{"body":"candidate-private-secret",'+ordinary[1:], values=["candidate-private-secret"])
    add("negative-zero", raw=ordinary.replace('[1,0]', '[-0]'))
    for name, key, value in (("nan-description","description",float("nan")), ("infinite-body","body",float("inf")), ("nan-evidence","evidence_indexes",[float("nan")]), ("negative-inf-evidence","evidence_indexes",[float("-inf")]), ("schema-nan","schema",float("nan"))):
        add(name, {**base,key:value})
    add("overflow-float-evidence", raw=ordinary.replace('[1,0]', '[1e999]'))
    add("quoted-nonfinite", {**base,"body":"NaN Infinity -Infinity are strings"})
    add("integer-digit-limit", raw=ordinary.replace('[1,0]', '[__REPEAT__]'), repeat_text="9", repeat_count=sys.get_int_max_str_digits()+1)
    add("paired-surrogates", raw=ordinary.replace('Useful recipe', r'Useful \ud83c\udf31'))
    add("escaped-backslash-u", {**base,"body":r"literal \ud800"})
    return dict(cases=cases, integer_digit_limit=sys.get_int_max_str_digits(), source_sha256=hashlib.sha256((PYTHON_ROOT / "mini_loop" / "skill_capture.py").read_bytes()).hexdigest())


def _skill_preview_contracts() -> dict:
    """Actual complete preview business flow, using a recorded model boundary."""
    import asyncio
    from types import SimpleNamespace
    from mini_loop.skill_capture import preview_personal_skill, record_personal_skill_turn, PersonalSkillError
    from mini_loop.secrets import SecretRegistry
    cases = []
    candidate = dict(schema="mini-loop.personal-skill-draft/v1", decision="create", description=" Useful recipe ", body=" First.\r\nThen. ", evidence_indexes=[0])
    accepted = json.dumps(candidate, separators=(",", ":"))
    skipped = json.dumps({**candidate,"decision":"skip","description":"","body":"","evidence_indexes":[]}, separators=(",", ":"))

    async def add(name, *, requested_name="recipe", owner="owner", session="session", focus="", ledger=False, turns=1, user="human", final="answer", history=None, values=(), minimum=8, unresolved=False, responses=None, short_after=False):
        history = history if history is not None else [dict(role="user",content="human"),dict(role="assistant",content="answer")]
        responses = responses if responses is not None else [accepted]
        registry = SecretRegistry(min_length=minimum)
        for index, value in enumerate(values): registry.register(f"VALUE{index}",value)
        if unresolved: registry.register("missing", lambda: None)
        state = dict(session_id=session)
        if ledger: state["personal_skill_turns"] = []
        calls = []
        async def create(messages, **kwargs):
            calls.append(dict(messages=messages, **kwargs))
            response = responses[len(calls)-1]
            if response == "provider-error": raise RuntimeError("private-provider-error")
            if response == "cancelled": raise asyncio.CancelledError()
            if short_after: registry.register("new-short", "tiny")
            return SimpleNamespace(content=[dict(type="thinking", thinking="private ignored"),dict(type="text",text=response)])
        agent = SimpleNamespace(state=state, secrets=registry, messages=history, _create=create)
        if ledger:
            for _ in range(turns): record_personal_skill_turn(agent,user,final)
        expected = dict(error="",status=0,preview=None)
        try:
            draft = await preview_personal_skill(agent, owner, requested_name, focus)
        except asyncio.CancelledError:
            expected["error"] = "cancelled"
        except PersonalSkillError as error:
            expected.update(error=error.code,status=error.status_code)
        else:
            public = draft.public_dict()
            expected["preview"] = {key:value for key,value in public.items() if key not in ("draft_id","created_at","expires_at")}
        expected["calls"] = [hashlib.sha256(json.dumps(call,ensure_ascii=False,separators=(",", ":"),sort_keys=True).encode()).hexdigest() for call in calls]
        cases.append(dict(name=name,requested_name=requested_name,owner=owner,session=session,focus=focus,ledger=ledger,turns=turns,user=user,final=final,history=history,values=list(values),minimum=minimum,unresolved=unresolved,responses=responses,short_after=short_after,expected=expected))

    async def run():
        await add("legacy-create")
        await add("authenticated-create",ledger=True)
        await add("authenticated-tail",ledger=True,turns=40)
        await add("empty-ledger-no-history-fallback",ledger=True,turns=0)
        await add("authenticated-gap",ledger=True,history=[dict(role="user",content="[Context compressed here]")])
        await add("legacy-gap",history=[dict(role="user",content="[snipped]gap"),dict(role="assistant",content="answer")])
        await add("legacy-only-internal",history=[dict(role="user",content="<runtime-state>internal")])
        await add("invalid-name",requested_name="../bad",owner="")
        await add("invalid-owner",owner="")
        await add("sensitive-name",values=["recipe"],minimum=1)
        await add("short-health",values=["tiny"])
        await add("unresolved-health",unresolved=True)
        await add("latched-capture",ledger=True,values=["tiny"])
        await add("focus-mask",focus="preview-private-secret then safe",values=["preview-private-secret"])
        await add("focus-unicode-bound",focus="界🌱"*1200)
        await add("root-key-mask",values=["requested_name"],minimum=1)
        await add("legacy-role-key-mask",values=["role"],minimum=1)
        await add("authenticated-role-key-mask",ledger=True,values=["role"],minimum=1)
        await add("repair-malformed",responses=["private malformed candidate",accepted])
        await add("repair-sensitive",responses=[json.dumps({**candidate,"body":"preview-private-secret"}),accepted],values=["preview-private-secret"])
        await add("repair-evidence",responses=[json.dumps({**candidate,"evidence_indexes":[999]}),accepted])
        await add("two-invalid",responses=["private malformed one","private malformed two"])
        await add("skip",responses=[skipped])
        await add("post-create-health",short_after=True)
        await add("post-skip-health",responses=[skipped],short_after=True)
        await add("provider-error",responses=["provider-error"])
        await add("repair-provider-error",responses=["bad","provider-error"])
        await add("cancelled",responses=["cancelled"])
        await add("invalid-session-after-model",session="")
    asyncio.run(run())
    return dict(cases=cases,source_sha256=hashlib.sha256((PYTHON_ROOT / "mini_loop" / "skill_capture.py").read_bytes()).hexdigest())


def _native_skill_preview_contracts(scratch: Path) -> dict:
    """Preview through the actual Agent._create recovery/cache/event path."""
    import asyncio
    import copy
    from types import SimpleNamespace
    from mini_loop.agent import Agent
    from mini_loop.caching import NullCachePolicy
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeMessage, FakeUsage, text, thinking, tool
    from mini_loop.skill_capture import preview_personal_skill, PersonalSkillError

    def candidate(decision: str, description: str, body: str, evidence: list[int]) -> str:
        return json.dumps(
            dict(schema="mini-loop.personal-skill-draft/v1", decision=decision,
                 description=description, body=body, evidence_indexes=evidence),
            separators=(",", ":"),
        )

    valid = candidate("create", "recipe", "procedure", [0])
    skip = candidate("skip", "", "", [])
    recipes = [
        dict(name="valid", responses=[valid]),
        dict(name="repair", responses=["malformed", valid]),
        dict(name="skip", responses=[skip]),
        dict(name="tool-block-ignored", responses=[valid], tool=True),
        dict(name="empty-refusal", responses=["", ""]),
        dict(name="provider-error", responses=[valid], fault=True),
    ]

    async def scenario(row):
        root = scratch / row["name"]
        root.mkdir(parents=True)
        calls, events = [], []

        async def create(**kwargs):
            calls.append(copy.deepcopy(kwargs))
            if row.get("fault"):
                raise RuntimeError("private preview provider failure")
            raw = row["responses"][len(calls) - 1]
            # Split inside a JSON string to detect inserted text separators.
            middle = raw.index("procedure") + 4 if "procedure" in raw else len(raw) // 2
            blocks = [thinking("private ignored thinking"), text(raw[:middle]), text(raw[middle:])]
            if row.get("tool"):
                blocks.append(tool("bash", command="touch NEVER"))
            return FakeMessage(
                blocks, "tool_use" if row.get("tool") else "end_turn",
                FakeUsage(777, 3), model="served-preview",
            )

        async def emit(event):
            events.append(copy.deepcopy(event))

        agent = Agent(
            client=SimpleNamespace(messages=SimpleNamespace(create=create)),
            workspace=root, emit=emit, cache_policy=NullCachePolicy(),
            state=dict(session_id="session"),
            settings=Settings(fake_llm=True, spill_dir=None, skills_dir=root / "empty"),
        )
        history = [
            dict(role="user", content="human evidence"),
            dict(role="assistant", content="assistant evidence"),
        ]
        agent.messages = copy.deepcopy(history)
        before = agent.token_meter.snapshot()
        expected = dict(error="", status=0, preview=None)
        try:
            draft = await preview_personal_skill(agent, "owner", "recipe")
        except PersonalSkillError as error:
            expected.update(error=error.code, status=error.status_code)
        else:
            expected["preview"] = {
                key: value for key, value in draft.public_dict().items()
                if key not in ("draft_id", "created_at", "expires_at")
            }
        assert agent.messages == history and agent.token_meter.snapshot() == before
        return {
            **row, "fault": row.get("fault", False), "tool": row.get("tool", False),
            "messages": history, "calls": calls, "expected": expected,
            "purposes": [e["purpose"] for e in events if e["type"] == "model_start"],
            "ends": [e["status"] for e in events if e["type"] == "model_end"],
        }

    async def run():
        return [await scenario(row) for row in recipes]

    return dict(cases=asyncio.run(run()))


def _manager_skill_draft_contracts(scratch: Path) -> dict:
    """Actual manager pool injection across create/fork and SQL restoration."""
    import asyncio
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.skill_capture import _draft_store, PersonalSkillError
    from mini_loop.storage import SQLiteStateStore

    async def scenario():
        scratch.mkdir(parents=True)
        settings = Settings(fake_llm=True, workspace_root=scratch / "workspaces",
                            skills_dir=scratch / "empty", user_resources_root=None)
        store = SQLiteStateStore(scratch / "state.db")
        frames = []
        manager = SessionManager(settings, FakeAsyncAnthropic(), state_store=store)

        def capture(name, fleet, session):
            frames.append(dict(name=name, shared=_draft_store(session.agent) is fleet.personal_skill_drafts))

        try:
            alice = manager.create(owner="alice")
            bob = manager.create(owner="bob")
            anonymous = manager.create()
            capture("alice", manager, alice)
            capture("bob", manager, bob)
            capture("anonymous", manager, anonymous)
            fork = await manager.fork_session(alice.id)
            capture("fork", manager, fork)
            draft = manager.personal_skill_drafts.add(
                owner="alice", session_id=alice.id, name="recipe", description="recipe",
                body="procedure", evidence_indexes=[0], coverage="current_epoch", omitted=0,
            )
            old_pool = manager.personal_skill_drafts
        finally:
            await manager.stop()

        for scheduled in (False, True):
            fleet = SessionManager(settings, FakeAsyncAnthropic(), state_store=store)
            try:
                if scheduled:
                    restored = fleet.restore_scheduled_session(alice.id)
                    capture("scheduled-alice", fleet, restored)
                    capture("scheduled-missing", fleet, fleet.restore_scheduled_session("missing"))
                else:
                    restored = next(row for row in fleet.restore_sessions() if row.id == alice.id)
                    capture("restored-alice", fleet, restored)
                assert fleet.personal_skill_drafts is not old_pool
                try:
                    fleet.personal_skill_drafts.peek(draft.draft_id, owner="alice", session_id=alice.id)
                except PersonalSkillError as error:
                    assert error.code == "draft_not_found"
                else:
                    raise AssertionError("process-local draft survived manager restart")
            finally:
                await fleet.stop()
        store.close()
        return dict(frames=frames, restart_discards_drafts=True, resources_disabled=True)

    return asyncio.run(scenario())


def _manager_skill_preview_contracts(scratch: Path) -> dict:
    """Actual manager policy ordering, ledger refusal and successful previews."""
    import asyncio
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, FakeMessage, FakeUsage, text
    from mini_loop.manager import SessionManager
    from mini_loop.session import LeaseLost
    from mini_loop.skills import SkillLoader
    from mini_loop.skill_capture import PersonalSkillError, record_personal_skill_turn
    from mini_loop.user_resources import UserResourceResolver

    async def scenario(name):
        root = scratch / name
        root.mkdir(parents=True)
        skills = SkillLoader(root / "empty")
        resolver = None if name == "disabled" else UserResourceResolver(root / "users", skills)
        settings = Settings(fake_llm=True, workspace_root=root / "workspaces",
                            skills_dir=root / "empty", user_resources_root=None)
        manager = SessionManager(settings, FakeAsyncAnthropic(), user_resources=resolver)
        session = manager.create(owner="anonymous" if name == "anonymous" else "alice",
                                 permission_mode="readonly" if name == "readonly" else "interactive")
        calls = []
        async def create(messages, **kwargs):
            calls.append(kwargs["purpose"])
            return FakeMessage([text(json.dumps(dict(schema="mini-loop.personal-skill-draft/v1",
                decision="create", description="recipe", body="procedure", evidence_indexes=[0])))],
                "end_turn", FakeUsage(777, 3))
        session.agent._create = create
        session.agent.messages = [dict(role="user", content="legacy evidence")]
        if name != "empty":
            record_personal_skill_turn(session.agent, "human evidence", "assistant evidence")
        if name == "closed":
            session._accepting_runs = False
        if name == "lease-lost":
            def lost():
                raise LeaseLost("private holder")
            session._require_lease = lost
        owner = "foreign" if name == "foreign" else session.owner
        expected = dict(error="", status=0, coverage="")
        try:
            result = await manager.preview_personal_skill(session.id, owner, "recipe")
        except PersonalSkillError as error:
            expected.update(error=error.code, status=error.status_code)
        else:
            expected["coverage"] = result["coverage"]
        finally:
            await manager.stop()
        return dict(name=name, expected=expected, calls=calls)

    async def run():
        return [await scenario(name) for name in
                ("foreign", "anonymous", "disabled", "empty", "valid", "readonly", "closed", "lease-lost")]
    return dict(cases=asyncio.run(run()))


def _manager_skill_commit_contracts(scratch: Path) -> dict:
    """Actual manager reviewed-draft publication and retention ordering."""
    import asyncio
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.skills import SkillLoader
    from mini_loop.skill_capture import PersonalSkillError
    from mini_loop.user_resources import UserResourceResolver

    async def scenario(name):
        root = scratch / name
        root.mkdir(parents=True)
        skills = SkillLoader(root / "empty")
        resolver = UserResourceResolver(root / "users", skills)
        settings = Settings(fake_llm=True, workspace_root=root / "workspaces",
                            skills_dir=root / "empty", user_resources_root=None)
        manager = SessionManager(settings, FakeAsyncAnthropic(), user_resources=resolver)
        session = manager.create(owner="alice", permission_mode="readonly" if name == "readonly" else "interactive")
        fields = dict(name="recipe", description="recipe", body="procedure")
        if name in ("conflict", "idempotent"):
            resolver.publish_skill("alice", {**fields, "body": "different" if name == "conflict" else "procedure"})
        draft = manager.personal_skill_drafts.add(owner="alice", session_id=session.id,
            **fields, evidence_indexes=[0], coverage="authenticated_turns", omitted=0)
        owner = "foreign" if name == "foreign" else "alice"
        digest = "wrong" if name == "wrong-digest" else draft.digest
        expected = dict(error="", status=0, idempotent=False, activation="", source="")
        try:
            result = await manager.commit_personal_skill(session.id, owner, draft.draft_id, digest)
        except PersonalSkillError as error:
            expected.update(error=error.code, status=error.status_code)
        else:
            expected.update(idempotent=result["idempotent"], activation=result["activation"], source=result["source"])
        try:
            manager.personal_skill_drafts.peek(draft.draft_id, owner="alice", session_id=session.id)
        except PersonalSkillError:
            retained = False
        else:
            retained = True
        finally:
            await manager.stop()
        return dict(name=name, expected=expected, retained=retained)

    async def run():
        return [await scenario(name) for name in
                ("foreign", "readonly", "wrong-digest", "valid", "conflict", "idempotent")]
    return dict(cases=asyncio.run(run()))


def _snapshot() -> dict[str, bytes]:
    from tools.workflow_tool_contracts import workflow_bound_tool_contracts
    from tools.manager_workflow_contracts import manager_workflow_contracts
    from tools.workflow_session_contracts import workflow_session_contracts
    from tools.workflow_inbox_contracts import workflow_inbox_contracts
    from tools.workflow_http_contracts import workflow_http_contracts
    from tools.workflow_launcher_contracts import workflow_launcher_contracts
    from tools.workflow_archive_contracts import workflow_archive_contracts
    from tools.workflow_legacy_archive_contracts import workflow_legacy_archive_contracts
    from tools.workflow_trajectory_contracts import workflow_trajectory_contracts
    from tools.workflow_trace_view_contracts import workflow_trace_view_contracts
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
        managed_cron_contracts = _managed_cron_contracts(Path(scratch) / "managed-cron")
        cron_surface_contracts = _cron_surface_contracts(Path(scratch) / "cron-surfaces")
        state_store_contracts = _state_store_contracts(Path(scratch) / "state-store")
        state_session_contracts = _state_session_contracts(Path(scratch) / "state-session")
        state_restore_contracts = _state_restore_contracts(Path(scratch) / "state-restore")
        scheduled_restore_contracts = _scheduled_restore_contracts(Path(scratch) / "scheduled-restore")
        event_catchup_contracts = _event_catchup_contracts(Path(scratch) / "event-catchup")
        plan_mode_contracts = _plan_mode_contracts(Path(scratch) / "plan-mode")
        goal_contracts = _goal_contracts(Path(scratch) / "goals")
        plan_outcome_contracts = _plan_outcome_contracts(Path(scratch) / "plan-outcomes")
        decision_contracts = _decision_contracts(Path(scratch) / "decisions")
        decision_runtime_contracts = _decision_runtime_contracts(Path(scratch) / "decision-runtime")
        decision_replay_contracts = _decision_replay_contracts(Path(scratch) / "decision-replay")
        decision_sink_contracts = _decision_sink_contracts(Path(scratch) / "decision-sinks")
        user_skill_contracts = _user_skill_contracts()
        owner_directory_contracts = _owner_directory_contracts(Path(scratch) / "owner-directories")
        layered_skill_contracts = _layered_skill_contracts(Path(scratch) / "layered-skills")
        memory_store_contracts = _memory_store_contracts(Path(scratch) / "memory-store")
        owner_resource_contracts = _owner_resource_contracts(Path(scratch) / "owner-resources")
        durable_create_contracts = _durable_create_contracts(Path(scratch) / "durable-create")
        publication_contracts = _publication_contracts(Path(scratch) / "publication")
        user_session_resource_contracts = _user_session_resource_contracts(Path(scratch) / "user-session-resources")
        memory_tool_contracts = _memory_tool_contracts(Path(scratch) / "memory-tools")
        memory_context_contracts = _memory_context_contracts(Path(scratch) / "memory-context")
        memory_extraction_contracts = _memory_extraction_contracts(Path(scratch) / "memory-extraction")
        memory_consolidation_contracts = _memory_consolidation_contracts(Path(scratch) / "memory-consolidation")
        memory_capture_contracts = _memory_capture_contracts(Path(scratch) / "memory-capture")
        launcher_memory_contracts = _launcher_memory_contracts(Path(scratch) / "launcher-memory")
        draft_store_contracts = _draft_store_contracts()
        skill_projection_contracts = _skill_projection_contracts()
        skill_capture_contracts = _skill_capture_contracts()
        skill_candidate_contracts = _skill_candidate_contracts()
        skill_preview_contracts = _skill_preview_contracts()
        native_skill_preview_contracts = _native_skill_preview_contracts(Path(scratch) / "native-skill-preview")
        manager_skill_draft_contracts = _manager_skill_draft_contracts(Path(scratch) / "manager-skill-drafts")
        manager_skill_preview_contracts = _manager_skill_preview_contracts(Path(scratch) / "manager-skill-preview")
        manager_skill_commit_contracts = _manager_skill_commit_contracts(Path(scratch) / "manager-skill-commit")
        transcript_contracts = _transcript_contracts(Path(scratch) / "transcript")

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
        "python-managed-cron.json": _json_bytes(managed_cron_contracts),
        "python-cron-surfaces.json": _json_bytes(cron_surface_contracts),
        "python-state-store.json": _json_bytes(state_store_contracts),
        "python-state-session.json": _json_bytes(state_session_contracts),
        "python-state-restore.json": _json_bytes(state_restore_contracts),
        "python-scheduled-restore.json": _json_bytes(scheduled_restore_contracts),
        "python-event-catchup.json": _json_bytes(event_catchup_contracts),
        "python-transcript.json": _json_bytes(transcript_contracts),
        "python-plan-mode.json": _json_bytes(plan_mode_contracts),
        "python-plan-outcomes.json": _json_bytes(plan_outcome_contracts),
        "python-decisions.json": _json_bytes(decision_contracts),
        "python-decision-runtime.json": _json_bytes(decision_runtime_contracts),
        "python-decision-replay.json": _json_bytes(decision_replay_contracts),
        "python-decision-sinks.json": _json_bytes(decision_sink_contracts),
        "python-user-skills.json": _json_bytes(user_skill_contracts),
        "python-owner-directories.json": _json_bytes(owner_directory_contracts),
        "python-layered-skills.json": _json_bytes(layered_skill_contracts),
        "python-memory-store.json": _json_bytes(memory_store_contracts),
        "python-owner-resources.json": _json_bytes(owner_resource_contracts),
        "python-durable-create.json": _json_bytes(durable_create_contracts),
        "python-user-publication.json": _json_bytes(publication_contracts),
        "python-user-session-resources.json": _json_bytes(user_session_resource_contracts),
        "python-memory-tools.json": _json_bytes(memory_tool_contracts),
        "python-memory-context.json": _json_bytes(memory_context_contracts),
        "python-memory-extraction.json": _json_bytes(memory_extraction_contracts),
        "python-memory-consolidation.json": _json_bytes(memory_consolidation_contracts),
        "python-memory-capture.json": _json_bytes(memory_capture_contracts),
        "python-launcher-memory.json": _json_bytes(launcher_memory_contracts),
        "python-skill-drafts.json": _json_bytes(draft_store_contracts),
        "python-skill-projection.json": _json_bytes(skill_projection_contracts),
        "python-skill-capture.json": _json_bytes(skill_capture_contracts),
        "python-skill-candidate.json": _json_bytes(skill_candidate_contracts),
        "python-skill-preview.json": _json_bytes(skill_preview_contracts),
        "python-native-skill-preview.json": _json_bytes(native_skill_preview_contracts),
        "python-manager-skill-drafts.json": _json_bytes(manager_skill_draft_contracts),
        "python-manager-skill-preview.json": _json_bytes(manager_skill_preview_contracts),
        "python-manager-skill-commit.json": _json_bytes(manager_skill_commit_contracts),
        "python-personal-skill-requests.json": _json_bytes(_personal_skill_request_contracts()),
        "python-personal-skill-http.json": _json_bytes(_personal_skill_http_contracts()),
        "python-personal-skill-http-validation.json": _json_bytes(_personal_skill_http_validation_contracts()),
        "python-personal-skill-http-parsing.json": _json_bytes(_personal_skill_http_parsing_contracts()),
        "python-personal-skill-http-encoding.json": _json_bytes(_personal_skill_http_encoding_contracts()),
        "python-personal-skill-http-surrogates.json": _json_bytes(_personal_skill_http_surrogate_contracts()),
        "python-personal-skill-http-numbers.json": _json_bytes(_personal_skill_http_number_contracts()),
        "python-personal-skill-http-depth.json": _json_bytes(_personal_skill_http_depth_contracts()),
        "python-skill-catalogue-http.json": _json_bytes(_skill_catalogue_http_contracts()),
        "python-memory-http.json": _json_bytes(_memory_http_contracts()),
        "python-benchmark-statistics.json": _json_bytes(_benchmark_statistics_contracts()),
        "python-benchmark-tasks.json": _json_bytes(_benchmark_task_contracts()),
        "python-benchmark-arms.json": _json_bytes(_benchmark_arm_contracts()),
        "python-benchmark-http.json": _json_bytes(_benchmark_http_contracts()),
        "python-self-audit.json": _json_bytes(_self_audit_contracts()),
        "python-problem-log.json": _json_bytes(_problem_log_contracts()),
        "python-self-audit-manager.json": _json_bytes(_self_audit_manager_contracts()),
        "python-self-audit-http.json": _json_bytes(_self_audit_http_contracts()),
        "python-self-audit-tool.json": _json_bytes(_self_audit_tool_contracts()),
        "python-improvement-instruments.json": _json_bytes(_improvement_instrument_contracts()),
        "python-improvement-archive-append.json": _json_bytes(_improvement_archive_append_contracts()),
        "python-improvement-archive-read.json": _json_bytes(_improvement_archive_read_contracts()),
        "python-improvement-http-read.json": _json_bytes(_improvement_http_read_contracts()),
        "python-verified-fold.json": _json_bytes(_verified_fold_contracts()),
        "python-verified-service.json": _json_bytes(_verified_service_contracts()),
        "python-improvement-proposal.json": _json_bytes(_improvement_proposal_contracts()),
        "python-improvement-proposal-http.json": _json_bytes(_improvement_proposal_http_contracts()),
        "python-team-bus.json": _json_bytes(_team_bus_contracts()),
        "python-team-http.json": _json_bytes(_team_http_contracts()),
        "python-team-tools.json": _json_bytes(_team_tool_contracts()),
        "python-team-protocols.json": _json_bytes(_team_protocol_contracts()),
        "python-team-effects.json": _json_bytes(_team_effect_contracts()),
        "python-team-lifecycle.json": _json_bytes(_team_lifecycle_contracts()),

        "python-workflow-models.json": _json_bytes(_workflow_model_contracts()),

        "python-workflow-validation.json": _json_bytes(_workflow_validation_contracts()),
        "python-workflow-records.json": _json_bytes(_workflow_record_contracts()),
        "python-workflow-store-core.json": _json_bytes(_workflow_store_contracts()),
        "python-workflow-attempts.json": _json_bytes(_workflow_attempt_contracts()),
        "python-workflow-completion.json": _json_bytes(_workflow_completion_contracts()),
        "python-workflow-outbox.json": _json_bytes(_workflow_outbox_contracts()),
        "python-workflow-retention.json": _json_bytes(_workflow_retention_contracts()),
        "python-workflow-engine.json": _json_bytes(_workflow_engine_contracts()),
        "python-workflow-runner.json": _json_bytes(_workflow_runner_contracts(scratch)),
        "python-workflow-views.json": _json_bytes(_workflow_views_contracts()),
        "python-workflow-admission.json": _json_bytes(_workflow_admission_contracts()),
        "python-workflow-tools.json": _json_bytes(_workflow_tool_contracts(Path(scratch))),
        "python-workflow-service.json": _json_bytes(_workflow_service_contracts()),
        "python-workflow-bound-tools.json": _json_bytes(workflow_bound_tool_contracts()),
        "python-manager-workflows.json": _json_bytes(manager_workflow_contracts()),
        "python-workflow-session-events.json": _json_bytes(workflow_session_contracts()),
        "python-workflow-inbox.json": _json_bytes(workflow_inbox_contracts()),
        "python-workflow-http.json": _json_bytes(workflow_http_contracts()),
        "python-workflow-launcher.json": _json_bytes(workflow_launcher_contracts()),
        "python-workflow-archive.json": _json_bytes(workflow_archive_contracts()),
        "python-workflow-legacy-archive.json": _json_bytes(workflow_legacy_archive_contracts()),
        "python-workflow-trajectory.json": _json_bytes(workflow_trajectory_contracts()),
        "python-workflow-trace-view.json": _json_bytes(workflow_trace_view_contracts()),
        "python-goals.json": _json_bytes(goal_contracts),
        "python-openapi.json": _json_bytes(openapi),
        "python-sqlite-schema.sql": (_SCHEMA.strip() + "\n").encode(),
    }


def _personal_skill_request_contracts() -> dict:
    """Actual Pydantic request acceptance and normalized concrete values."""
    from pydantic import ValidationError
    from mini_loop.server import PersonalSkillPreviewReq, PersonalSkillCommitReq

    cases = []
    preview = [
        ("default-focus", {"name": "safe-name"}),
        ("explicit-focus", {"name": "safe-name", "focus": "reviewed"}),
        ("digit-name", {"name": "123"}),
        ("name-limit", {"name": "a" * 64}),
        ("long-name", {"name": "a" * 65}),
        ("empty-name", {"name": ""}),
        ("uppercase-name", {"name": "Safe"}),
        ("trailing-newline", {"name": "safe\n"}),
        ("double-hyphen", {"name": "safe--name"}),
        ("leading-hyphen", {"name": "-safe"}),
        ("trailing-hyphen", {"name": "safe-"}),
        ("path-name", {"name": "../safe"}),
        ("missing-name", {}),
        ("null-name", {"name": None}),
        ("number-name", {"name": 42}),
        ("bool-name", {"name": True}),
        ("null-focus", {"name": "safe", "focus": None}),
        ("number-focus", {"name": "safe", "focus": 42}),
        ("focus-limit", {"name": "safe", "focus": "界" * 2000}),
        ("astral-focus-limit", {"name": "safe", "focus": "😀" * 2000}),
        ("long-focus", {"name": "safe", "focus": "😀" * 2001}),
        ("combining-focus-limit", {"name": "safe", "focus": "e\u0301" * 1000}),
        ("owner-injection", {"name": "safe", "owner": "bob"}),
        ("path-injection", {"name": "safe", "path": "../escape"}),
        ("body-injection", {"name": "safe", "body": "procedure"}),
        ("case-sensitive-field", {"Name": "safe"}),
        ("array-root", []),
        ("null-root", None),
    ]
    commit = [
        ("valid", {"digest": "0123456789abcdef" * 4}),
        ("short", {"digest": "a" * 63}),
        ("long", {"digest": "a" * 65}),
        ("uppercase", {"digest": "A" * 64}),
        ("not-hex", {"digest": "g" * 64}),
        ("trailing-newline", {"digest": "a" * 63 + "\n"}),
        ("null", {"digest": None}),
        ("number", {"digest": 42}),
        ("missing", {}),
        ("body-injection", {"digest": "a" * 64, "body": "changed"}),
        ("owner-injection", {"digest": "a" * 64, "owner": "bob"}),
        ("array-root", []),
        ("null-root", None),
    ]
    for kind, model, inputs in (("preview", PersonalSkillPreviewReq, preview),
                                 ("commit", PersonalSkillCommitReq, commit)):
        for name, payload in inputs:
            raw = json.dumps(payload, ensure_ascii=False)
            try:
                result = model.model_validate_json(raw).model_dump()
            except ValidationError:
                accepted, result = False, None
            else:
                accepted = True
            cases.append(dict(kind=kind, name=name, raw=raw, accepted=accepted, result=result))
        # JSON duplicate-key behavior is last-value-wins in the source boundary.
        raw = '{"name":"bad/first","name":"last"}' if kind == "preview" else '{"digest":"bad","digest":"' + "a" * 64 + '"}'
        cases.append(dict(kind=kind, name="duplicate-last-wins", raw=raw,
                          accepted=True, result=model.model_validate_json(raw).model_dump()))
    return dict(cases=cases)


def _personal_skill_http_contracts() -> dict:
    """Actual authenticated HTTP capture, reviewed preview and publication."""
    from fastapi.testclient import TestClient
    from mini_loop.auth import TokenAuth
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, FakeMessage, FakeUsage, text
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app
    from mini_loop.skills import SkillLoader
    from mini_loop.user_resources import UserResourceResolver

    with tempfile.TemporaryDirectory() as scratch:
        root = Path(scratch)
        resolver = UserResourceResolver(root / "users", SkillLoader(root / "empty"))
        settings = Settings(fake_llm=True, workspace_root=root / "workspaces",
                            skills_dir=root / "empty", user_resources_root=None,
                            trajectory_enabled=False)
        manager = SessionManager(settings, FakeAsyncAnthropic(), user_resources=resolver)
        app = create_app(manager=manager)
        with TestClient(app) as client:
            app.state.auth = TokenAuth({"token-a": "alice", "token-b": "bob"})
            headers = {"Authorization": "Bearer token-a"}
            sid = client.post("/sessions", headers=headers, json={}).json()["id"]
            second = client.post("/sessions", headers=headers, json={}).json()["id"]
            async def create(messages, **kwargs):
                value = json.dumps(dict(schema="mini-loop.personal-skill-draft/v1",
                    decision="create", description="recipe", body="procedure", evidence_indexes=[0])) if kwargs.get("purpose") == "personal_skill_preview" else "done"
                return FakeMessage([text(value)], "end_turn", FakeUsage(1, 1))
            manager.get(sid).agent._create = create
            rows = []
            def record(name, path, body, token="token-a"):
                response = client.post(path, json=body,
                    headers={"Authorization": "Bearer " + token} if token else {})
                value = response.json()
                if response.status_code == 200:
                    for key in ("draft_id", "session", "created_at", "expires_at"):
                        value.pop(key, None)
                else:
                    value = json.loads(json.dumps(value).replace(sid, "<session>"))
                rows.append(dict(name=name, status=response.status_code, response=value))
                return response.json()
            preview_path = f"/sessions/{sid}/personal-skills/preview"
            record("unauthenticated", preview_path, {"name": "recipe"}, token="")
            record("query-token-refused", preview_path + "?access_token=token-a", {"name": "recipe"}, token="")
            record("foreign-preview", preview_path, {"name": "recipe"}, token="token-b")
            record("empty-evidence", preview_path, {"name": "recipe"})
            assert client.post(f"/sessions/{sid}/messages", headers=headers,
                               json={"message": "human evidence"}).status_code == 200
            draft = record("preview", preview_path, {"name": "recipe"})
            commit_path = f"/sessions/{sid}/personal-skills/{draft['draft_id']}/commit"
            reviewed = {"digest": draft["digest"]}
            assert client.post(f"/sessions/{sid}/mode", headers=headers,
                               json={"mode": "readonly"}).status_code == 200
            record("readonly", commit_path, reviewed)
            assert client.post(f"/sessions/{sid}/mode", headers=headers,
                               json={"mode": "interactive"}).status_code == 200
            record("wrong-digest", commit_path, {"digest": "0" * 64})
            record("cross-session", f"/sessions/{second}/personal-skills/{draft['draft_id']}/commit", reviewed)
            record("foreign-commit", commit_path, reviewed, token="token-b")
            record("commit", commit_path, reviewed)
            record("consumed", commit_path, reviewed)
    return dict(cases=rows)


def _personal_skill_http_validation_contracts() -> dict:
    """Actual FastAPI validation lists, including ordering and echoed input."""
    from fastapi.testclient import TestClient
    from mini_loop.auth import TokenAuth
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app

    inputs = [dict(kind=c["kind"], name=c["name"], raw=c["raw"])
              for c in _personal_skill_request_contracts()["cases"] if not c["accepted"]]
    inputs.extend([
        dict(kind="preview", name="absent-body", raw=""),
        dict(kind="commit", name="absent-body", raw=""),
        dict(kind="preview", name="multiple-errors", raw='{"owner":"bob","focus":false,"name":""}'),
        dict(kind="preview", name="extra-order", raw='{"name":"safe","z":{"nested":[1,true,null,"界"]},"a":["x"]}'),
        dict(kind="preview", name="duplicate-final-invalid", raw='{"name":"safe","name":"../bad"}'),
        dict(kind="commit", name="root-number", raw="42"),
        dict(kind="preview", name="root-string", raw='"not an object"'),
        dict(kind="preview", name="root-bool", raw="true"),
    ])
    with tempfile.TemporaryDirectory() as scratch:
        root = Path(scratch)
        settings = Settings(fake_llm=True, workspace_root=root / "workspaces",
                            skills_dir=root / "empty", user_resources_root=None,
                            trajectory_enabled=False)
        manager = SessionManager(settings, FakeAsyncAnthropic())
        with TestClient(create_app(manager=manager)) as client:
            client.app.state.auth = TokenAuth({"token-a": "alice"})
            for case in inputs:
                suffix = "preview" if case["kind"] == "preview" else "draft/commit"
                response = client.post("/sessions/missing/personal-skills/" + suffix,
                    content=case["raw"], headers={"Authorization": "Bearer token-a",
                                                 "Content-Type": "application/json"})
                assert response.status_code == 422
                case.update(status=response.status_code, response=response.json())
    return dict(cases=inputs)


def _personal_skill_http_parsing_contracts() -> dict:
    """Actual FastAPI syntax offsets and media/encoding admission results."""
    import base64
    from fastapi.testclient import TestClient
    from mini_loop.auth import TokenAuth
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app

    malformed = [" ", "{", "[", "[1,]", "[1", '{"x" 1}', '{"x":}',
                 '{"x":1', '{"x":1,}', "true false", "tru", "-", "01",
                 "1.", "1e", '"unclosed', '"bad\\q"', '"bad\\u12x4"',
                 '"bad\n"', '{"界":1,}', '{"😀": [1,]}', '{} {}',
                 '{"x":[true false]}', '"trailing\\', '\ufeff{}']
    inputs = [(kind, f"syntax-{index}", raw.encode(), "application/json")
              for kind in ("preview", "commit") for index, raw in enumerate(malformed)]
    # A BOM on bytes is accepted by json.loads; the string-BOM example above also
    # arrives as bytes, so this is a successful parse with a missing field.
    inputs.extend(("preview", "media-" + str(index), raw, media)
        for index, (raw, media) in enumerate([
            (b'{}', "text/plain"), (b'[]', "text/plain"),
            (b'{}', "application/x-www-form-urlencoded"),
            (b'{}', "text/json"), (b'{}', "application/vnd.api+json"),
            (b'{"name":"safe"}', "application/json; charset=utf-8"),
            (b'{"name":"safe"}', "APPLICATION/JSON"),
            (b'{"name":"safe"}', None),
            (b'\xff', "application/json"), (b'\xff', "text/plain"),
            (b'\xef\xbb\xbf{}', "application/json"),
            (b' ', "text/plain"), (b'', "text/plain"),
        ]))
    rows = []
    with tempfile.TemporaryDirectory() as scratch:
        root = Path(scratch)
        settings = Settings(fake_llm=True, workspace_root=root / "workspaces",
                            skills_dir=root / "empty", user_resources_root=None,
                            trajectory_enabled=False)
        with TestClient(create_app(manager=SessionManager(settings, FakeAsyncAnthropic())),
                        raise_server_exceptions=False) as client:
            client.app.state.auth = TokenAuth({"token-a": "alice"})
            for kind, name, raw, media in inputs:
                headers = {"Authorization": "Bearer token-a"}
                if media is not None:
                    headers["Content-Type"] = media
                suffix = "preview" if kind == "preview" else "draft/commit"
                response = client.post("/sessions/missing/personal-skills/" + suffix,
                                       content=raw, headers=headers)
                json_response = "application/json" in response.headers.get("content-type", "")
                rows.append(dict(kind=kind, name=name, body=base64.b64encode(raw).decode(),
                    media=media, status=response.status_code,
                    response=response.json() if json_response else None,
                    text=None if json_response else response.text))
    return dict(cases=rows)


def _personal_skill_http_encoding_contracts() -> dict:
    """Actual byte detection/transcoding, plus explicit unresolved surrogates."""
    import base64
    from fastapi.testclient import TestClient
    from mini_loop.auth import TokenAuth
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app

    encodings = [("utf8", "utf-8", b""), ("utf8-bom", "utf-8-sig", b""),
        ("utf16le", "utf-16-le", b""), ("utf16be", "utf-16-be", b""),
        ("utf16le-bom", "utf-16-le", b"\xff\xfe"),
        ("utf16be-bom", "utf-16-be", b"\xfe\xff"),
        ("utf32le", "utf-32-le", b""), ("utf32be", "utf-32-be", b""),
        ("utf32le-bom", "utf-32-le", b"\xff\xfe\x00\x00"),
        ("utf32be-bom", "utf-32-be", b"\x00\x00\xfe\xff")]
    inputs = []
    for kind in ("preview", "commit"):
        valid = '{"name":"safe","focus":"界😀"}' if kind == "preview" else '{"digest":"' + "a" * 64 + '"}'
        invalid = '{"name":"界😀"}' if kind == "preview" else '{"digest":"界😀"}'
        for label, encoding, bom in encodings:
            for name, payload in (("empty", "{}"), ("valid", valid),
                                  ("validation", invalid), ("syntax", '{"界😀":1,}')):
                inputs.append((kind, label + "-" + name, bom + payload.encode(encoding)))
    inputs.extend(("preview", "invalid-" + str(index), raw)
        for index, raw in enumerate([
            b"\xff\xfe\x00", b"\xfe\xff\x00", b"\xff\xfe\x00\x00\x00",
            b"\x00\x00\xfe\xff\x00", b"\xff\xfe\x00\x00\x00\x00\x11\x00",
            b"\x00\x00\xfe\xff\x00\x11\x00\x00", b"\xc0\xaf", b"\xf4\x90\x80\x80",
        ]))
    pending = [("preview", "surrogate-" + label,
                bom + '{"name":"safe","focus":"\ud800"}'.encode(encoding, "surrogatepass"))
               for label, encoding, bom in encodings if label.endswith("-bom") or label == "utf8"]
    rows, unresolved = [], []
    with tempfile.TemporaryDirectory() as scratch:
        root = Path(scratch)
        settings = Settings(fake_llm=True, workspace_root=root / "workspaces",
                            skills_dir=root / "empty", user_resources_root=None,
                            trajectory_enabled=False)
        with TestClient(create_app(manager=SessionManager(settings, FakeAsyncAnthropic())),
                        raise_server_exceptions=False) as client:
            client.app.state.auth = TokenAuth({"token-a": "alice"})
            for destination, cases in ((rows, inputs), (unresolved, pending)):
                for kind, name, raw in cases:
                    suffix = "preview" if kind == "preview" else "draft/commit"
                    response = client.post("/sessions/missing/personal-skills/" + suffix,
                        content=raw, headers={"Authorization": "Bearer token-a",
                                             "Content-Type": "application/json; charset=utf-8"})
                    json_response = "application/json" in response.headers.get("content-type", "")
                    destination.append(dict(kind=kind, name=name,
                        body=base64.b64encode(raw).decode(), status=response.status_code,
                        response=response.json() if json_response else None,
                        text=None if json_response else response.text))
    return dict(cases=rows, pending_surrogates=unresolved)


def _personal_skill_http_surrogate_contracts() -> dict:
    """Actual surrogatepass, JSON escape pairing and final-key retention."""
    import base64
    from fastapi.testclient import TestClient
    from mini_loop.auth import TokenAuth
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app

    rows = _personal_skill_http_encoding_contracts()["pending_surrogates"]
    payloads = [
        ("escaped-focus", '{"name":"safe","focus":"\\ud800"}'),
        ("escaped-name", '{"name":"\\ud800"}'),
        ("escaped-root", '"\\udc00"'),
        ("escaped-pair", '{"name":"safe","focus":"\\ud800\\udc00"}'),
        ("escaped-overwritten", '{"name":"safe","focus":"\\ud800","focus":"okay"}'),
        ("nested-overwritten", '{"name":"safe","extra":{"x":"\\ud800","x":"okay"}}'),
        ("key", '{"name":"safe","\\ud800":0}'),
        ("different-keys", '{"name":"safe","\\ud800":0,"\ufffd":1}'),
        ("raw-focus", '{"name":"safe","focus":"\ud800"}'),
        ("raw-pair", '{"name":"safe","focus":"\ud800\udc00"}'),
        ("raw-overwritten", '{"name":"safe","focus":"\ud800","focus":"okay"}'),
        ("raw-syntax", '{"name":"safe","focus":"\ud800",}'),
        ("raw-outside-string", '[ \ud800 ]'),
        ("raw-unterminated", '"\ud800'),
    ]
    with tempfile.TemporaryDirectory() as scratch:
        root = Path(scratch)
        settings = Settings(fake_llm=True, workspace_root=root / "workspaces",
                            skills_dir=root / "empty", user_resources_root=None,
                            trajectory_enabled=False)
        with TestClient(create_app(manager=SessionManager(settings, FakeAsyncAnthropic())),
                        raise_server_exceptions=False) as client:
            client.app.state.auth = TokenAuth({"token-a": "alice"})
            for kind in ("preview", "commit"):
                for encoding in ("utf-8", "utf-16", "utf-32"):
                    for name, payload in payloads:
                        raw = payload.encode(encoding, "surrogatepass")
                        suffix = "preview" if kind == "preview" else "draft/commit"
                        response = client.post("/sessions/missing/personal-skills/" + suffix,
                            content=raw, headers={"Authorization": "Bearer token-a",
                                                 "Content-Type": "application/json"})
                        json_response = "application/json" in response.headers.get("content-type", "")
                        rows.append(dict(kind=kind, name=encoding + "-" + name,
                            body=base64.b64encode(raw).decode(), status=response.status_code,
                            response=response.json() if json_response else None,
                            text=None if json_response else response.text))
    return dict(cases=rows)


def _personal_skill_http_number_contracts() -> dict:
    """Actual float rounding, nonfinite retention and decimal-integer refusal."""
    from fastapi.testclient import TestClient
    from mini_loop.auth import TokenAuth
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app

    numbers = ["NaN", "Infinity", "-Infinity", "1e999", "-1e999", "0", "-0",
               "-0.0", "1.0", "1e-999", "-1e-999", "1.0000000000000001",
               "9007199254740993", "9007199254740993.0", "1e15", "1e16",
               "1e-4", "1e-5", "5e-324", "9" * 4300, "9" * 4301,
               "9" * 4301 + ".0", "1e" + "9" * 1000, "1e-" + "9" * 1000]
    rows = []
    with tempfile.TemporaryDirectory() as scratch:
        root = Path(scratch)
        settings = Settings(fake_llm=True, workspace_root=root / "workspaces",
                            skills_dir=root / "empty", user_resources_root=None,
                            trajectory_enabled=False)
        with TestClient(create_app(manager=SessionManager(settings, FakeAsyncAnthropic())),
                        raise_server_exceptions=False) as client:
            client.app.state.auth = TokenAuth({"token-a": "alice"})
            for kind in ("preview", "commit"):
                field = "name" if kind == "preview" else "digest"
                valid = '"safe"' if kind == "preview" else '"' + "a" * 64 + '"'
                suffix = "preview" if kind == "preview" else "draft/commit"
                for index, number in enumerate(numbers):
                    payloads = [
                        ("root", number),
                        ("field", '{"' + field + '":' + number + '}'),
                        ("extra", '{"' + field + '":' + valid + ',"extra":' + number + '}'),
                        ("nested", '{"' + field + '":' + valid + ',"extra":[' + number + ']}'),
                        ("overwritten", '{"' + field + '":' + number + ',"' + field + '":' + valid + '}'),
                        ("syntax", '[' + number + ',]'),
                    ]
                    for shape, raw in payloads:
                        response = client.post("/sessions/missing/personal-skills/" + suffix,
                            content=raw, headers={"Authorization": "Bearer token-a",
                                                 "Content-Type": "application/json"})
                        is_json = "application/json" in response.headers.get("content-type", "")
                        rows.append(dict(kind=kind, name=str(index) + "-" + shape, raw=raw,
                            status=response.status_code, response=response.json() if is_json else None,
                            text=None if is_json else response.text))
    return dict(integer_digit_limit=sys.get_int_max_str_digits(), cases=rows)


def _personal_skill_http_depth_contracts() -> dict:
    """Real Uvicorn HTTP, not TestClient's different recursion-budget stack."""
    import logging
    import socket
    import threading
    import time
    import httpx
    import uvicorn
    from mini_loop.auth import TokenAuth
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app

    rows = []
    depths = [1, 255, 256, 257, 512, 972, 978, 979, 984, 985, 986, 1000]
    with tempfile.TemporaryDirectory() as scratch:
        root = Path(scratch)
        settings = Settings(fake_llm=True, workspace_root=root / "workspaces",
                            skills_dir=root / "empty", user_resources_root=None,
                            trajectory_enabled=False)
        app = create_app(manager=SessionManager(settings, FakeAsyncAnthropic()))
        sock = socket.socket()
        sock.bind(("127.0.0.1", 0))
        endpoint = "http://127.0.0.1:" + str(sock.getsockname()[1])
        server = uvicorn.Server(uvicorn.Config(app, log_config=None,
                                log_level="critical", access_log=False))
        worker = threading.Thread(target=server.run, kwargs={"sockets": [sock]}, daemon=True)
        logger = logging.getLogger("uvicorn.error")
        previous_level = logger.level
        try:
            # Expected source recursion exceptions return safe 500. Their responses
            # are recorded below; do not dump hundreds of identical ASGI tracebacks.
            logger.setLevel(logging.CRITICAL)
            worker.start()
            deadline = time.monotonic() + 5
            while not server.started:
                if not worker.is_alive() or time.monotonic() >= deadline:
                    raise RuntimeError("source depth HTTP server did not start")
                time.sleep(0.01)
            app.state.auth = TokenAuth({"token-a": "alice"})
            with httpx.Client(base_url=endpoint, timeout=5, headers={
                    "Authorization": "Bearer token-a", "Content-Type": "application/json",
                    "Connection": "close"}) as client:
                for kind in ("preview", "commit"):
                    field = "name" if kind == "preview" else "digest"
                    valid = '"safe"' if kind == "preview" else '"' + "a" * 64 + '"'
                    suffix = "preview" if kind == "preview" else "draft/commit"
                    for depth in depths:
                        leaf = '"depth-canary-12345"'
                        nested = "[" * depth + leaf + "]" * depth
                        payloads = [
                            ("root-array", nested),
                            ("root-object", '{"x":' * depth + leaf + "}" * depth),
                            ("extra", '{"' + field + '":' + valid + ',"extra":' + nested + '}'),
                            ("overwritten", '{"' + field + '":' + nested + ',"' + field + '":' + valid + '}'),
                            ("empty", "[" * depth + "]" * depth),
                            ("syntax", nested + ","),
                        ]
                        for shape, raw in payloads:
                            response = client.post("/sessions/missing/personal-skills/" + suffix,
                                                   content=raw)
                            is_json = "application/json" in response.headers.get("content-type", "")
                            # Retain boundary bytes: client-side json.loads at depth
                            # ~980 would consume a different recursive call budget.
                            rows.append(dict(kind=kind, name=str(depth) + "-" + shape, raw=raw,
                                status=response.status_code,
                                json=response.text if is_json else None,
                                text=None if is_json else response.text))
        finally:
            server.should_exit = True
            if worker.ident is not None:
                worker.join(timeout=5)
            sock.close()
            logger.setLevel(previous_level)
            if worker.is_alive():
                raise RuntimeError("source depth HTTP server did not stop")
    return dict(transport="uvicorn-default-http", uvicorn=uvicorn.__version__,
                recursion_limit=sys.getrecursionlimit(), cases=rows)


def _skill_catalogue_http_contracts() -> dict:
    """Actual owner-admitted descriptions and future-only publication visibility."""
    from fastapi.testclient import TestClient
    from mini_loop.auth import TokenAuth, NullAuth
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app
    from mini_loop.skills import SkillLoader
    from mini_loop.user_resources import UserResourceResolver, canonical_user_skill

    scenarios = []
    with tempfile.TemporaryDirectory() as scratch:
        for mode in ("empty", "legacy", "layered", "anonymous"):
            root = Path(scratch) / mode
            agent_dir = root / "agent"
            agent_dir.mkdir(parents=True)
            if mode != "empty":
                path = agent_dir / "first" / "SKILL.md"
                path.parent.mkdir()
                path.write_text(canonical_user_skill("first", "Agent first 界", "private agent body"), encoding="utf-8")
            loader = SkillLoader(agent_dir)
            resolver = UserResourceResolver(root / "users", loader) if mode == "layered" else None
            if resolver is not None:
                for owner in ("alice", "bob"):
                    resolver.publish_skill(owner, dict(name="first", description=owner + " private catalogue", body=owner + " private body"))
            settings = Settings(fake_llm=True, workspace_root=root / "workspaces",
                                skills_dir=agent_dir, trajectory_enabled=False, user_resources_root=None)
            manager = SessionManager(settings, FakeAsyncAnthropic(), skills=loader, user_resources=resolver)
            app = create_app(manager=manager)
            with TestClient(app, raise_server_exceptions=False) as client:
                app.state.auth = NullAuth() if mode == "anonymous" else TokenAuth({"token-a": "alice", "token-b": "bob"})
                ids = {}
                def create(label, token):
                    response = client.post("/sessions", json={}, headers={"Authorization": "Bearer " + token} if token else {})
                    assert response.status_code == 200, response.text
                    ids[label] = response.json()["id"]
                actor = "" if mode == "anonymous" else "token-a"
                create("alice", actor)
                create("second", actor)
                create("bob", "" if mode == "anonymous" else "token-b")
                ids["missing"] = "missing"
                rows = []
                def capture(name, target, token, query=""):
                    response = client.get("/sessions/" + ids[target] + "/skills" + query,
                        headers={"Authorization": "Bearer " + token} if token else {})
                    value = response.text
                    for label, sid in ids.items():
                        if label != "missing":
                            value = value.replace(sid, "<" + label + ">")
                    rows.append(dict(name=name, target=target, token=token, query=query,
                                     status=response.status_code, response=json.loads(value)))
                capture("missing-credentials", "alice", "")
                capture("query-token", "alice", "", "?access_token=token-a")
                capture("other-owner", "alice", "" if mode == "anonymous" else "token-b")
                capture("missing-session", "missing", actor)
                capture("alice-before", "alice", actor)
                capture("second-before", "second", actor)
                capture("bob-before", "bob", "" if mode == "anonymous" else "token-b")
                if resolver is not None:
                    resolver.publish_skill("alice", dict(name="later", description="Later publication", body="not a live replacement"))
                capture("alice-after", "alice", actor)
                capture("second-after", "second", actor)
                create("fresh", actor)
                capture("fresh-after", "fresh", actor)
                capture("bob-after", "bob", "" if mode == "anonymous" else "token-b")
                fork = client.post("/sessions/" + ids["alice"] + "/fork", json={},
                    headers={"Authorization": "Bearer " + actor} if actor else {})
                assert fork.status_code == 200, fork.text
                ids["fork"] = fork.json()["id"]
                capture("fork-after", "fork", actor)
            scenarios.append(dict(mode=mode, cases=rows))
    return dict(scenarios=scenarios)


def _memory_http_contracts() -> dict:
    """Actual owned metadata/body reads, mutable views and route decoding."""
    from fastapi.testclient import TestClient
    from mini_loop.auth import TokenAuth, NullAuth
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.memory import memory_store_for
    from mini_loop.server import create_app
    from mini_loop.skills import SkillLoader
    from mini_loop.user_resources import UserResourceResolver

    scenarios = []
    with tempfile.TemporaryDirectory() as scratch:
        for mode in ("shared", "local", "anonymous"):
            root = Path(scratch) / mode
            loader = SkillLoader(root / "skills")
            resolver = UserResourceResolver(root / "users", loader) if mode == "local" else None
            settings = Settings(fake_llm=True, workspace_root=root / "workspaces",
                                trajectory_enabled=False, user_resources_root=None)
            manager = SessionManager(settings, FakeAsyncAnthropic(), skills=loader, user_resources=resolver)
            app = create_app(manager=manager)
            with TestClient(app, raise_server_exceptions=False) as client:
                app.state.auth = NullAuth() if mode == "anonymous" else TokenAuth({"token-a": "alice", "token-b": "bob"})
                actor, other = ("", "") if mode == "anonymous" else ("token-a", "token-b")
                ids = {"missing": "missing"}
                for label, token in (("alice", actor), ("second", actor), ("bob", other)):
                    response = client.post("/sessions", json={}, headers={"Authorization": "Bearer " + token} if token else {})
                    assert response.status_code == 200, response.text
                    ids[label] = response.json()["id"]
                rows = []
                def capture(name, target, suffix, token, update=False):
                    response = client.get("/sessions/" + ids[target] + "/memory" + suffix,
                        headers={"Authorization": "Bearer " + token} if token else {})
                    value = response.text
                    for label, sid in ids.items():
                        if label != "missing":
                            value = value.replace(sid, "<" + label + ">")
                    rows.append(dict(name=name, target=target, suffix=suffix, token=token,
                                     update=update, status=response.status_code, response=json.loads(value)))
                capture("empty", "alice", "", actor)
                for label in ("alice", "bob"):
                    owner = "anonymous" if mode == "anonymous" else label
                    memory_store_for(manager.get(ids[label]).agent).write("界", "project", owner + " description", owner + " private body", origin="imported")
                if resolver is not None:
                    manager.memory.write("fallback", "project", "shared only", "must not leak", owner="alice")
                for suffix in ("", "/%E7%95%8C"):
                    capture("alice" + suffix, "alice", suffix, actor)
                    capture("same-owner" + suffix, "second", suffix, actor)
                    capture("bob" + suffix, "bob", suffix, other)
                    capture("foreign" + suffix, "alice", suffix, other)
                capture("no-auth", "alice", "", "")
                capture("query-auth", "alice", "?access_token=token-a", "")
                capture("missing-session", "missing", "/%E7%95%8C", actor)
                capture("missing-name", "alice", "/quote%27%22%5C%E7%95%8C", actor)
                capture("encoded-slash", "alice", "/a%2Fb", actor)
                memory_store_for(manager.get(ids["alice"]).agent).write("界", "feedback", "updated", "latest body")
                capture("updated", "second", "/%E7%95%8C", actor, update=True)
            scenarios.append(dict(mode=mode, cases=rows))
    return dict(scenarios=scenarios)


def _benchmark_statistics_contracts() -> dict:
    """Actual aggregation/verdict/behavior functions, without running model arms."""
    from mini_loop.benchmark import aggregate_runs, compare, _behavioral_metrics

    def row(task="task", passed=True, arm="arm", **dimensions):
        return dict(arm=arm, task=task, passed=passed, error=None, **dimensions)
    aggregates = []
    recipes = {
        "empty": [],
        "single": [[row(duration_ms=1.0, rounds=2)]],
        "even-tie": [[row(passed=True, rounds=1)], [row(passed=False, rounds=4)]],
        "majority-first-error": [[row(passed=True, rounds=10)],
            [dict(row(passed=False, rounds=2), error="first fault")],
            [dict(row(passed=True, rounds=7), error="later fault")]],
        "first-seen-order": [[row("z", arm="first"), row("界")],
            [row("z", arm="second"), row("a"), row("z", passed=False)]],
        "sparse": [[row(rounds=None, duration_ms=-0.0)], [row(rounds=3, duration_ms=2.5)], [row()]],
        "large-integer-even": [[row(rounds=9223372036854775807)], [row(rounds=9223372036854775806)]],
        "mixed-exact-order": [[row(rounds=9007199254740993)], [row(rounds=9007199254740992.0)], [row(rounds=9007199254740992)]],
        "boolean-measurements": [[row(rounds=True, tool_calls=False)], [row(rounds=False, tool_calls=True)], [row(rounds=True)]],
        "beyond-int64": [[row(rounds=10**40+1)], [row(rounds=10**40+3)]],
    }
    for name, runs in recipes.items():
        aggregates.append(dict(name=name, runs=runs, result=aggregate_runs(runs)))
    comparisons = []
    pairs = {
        "empty": ([], []),
        "equal": ([row("b"), row("a")], [row("a"), row("b")]),
        "regression-beats-win": ([row("loss"), row("win", False)], [row("win"), row("loss", False)]),
        "win": ([row(passed=False)], [row()]),
        "different-tasks": ([row("a")], [row("b")]),
        "different-count": ([row("a")], []),
        "duplicates": ([row(passed=False), row()], [row(passed=False), row(passed=False)]),
        "zero-base": ([row(rounds=0)], [row(rounds=5)]),
        "nonpositive": ([row(rounds=-1)], [row(rounds=0)]),
        "strict-threshold": ([row(rounds=100)], [row(rounds=125)]),
        "rounded-threshold": ([row(duration_ms=100.0)], [row(duration_ms=125.049)]),
        "all-dimensions": ([row(duration_ms=10.0, context_tokens_estimate=100,
             rounds=2, tool_calls=4, tool_errors=1, repeated_reads=1)],
            [row(duration_ms=15.0, context_tokens_estimate=130, rounds=3,
             tool_calls=5, tool_errors=2, repeated_reads=0)]),
        "signed-zero": ([row(duration_ms=-0.0)], [row(duration_ms=0.0)]),
        "mixed-sums": ([row(rounds=1), row(rounds=2.0)], [row(rounds=4.0), row(rounds=-1)]),
        "large-sum-and-boolean": ([row(rounds=9223372036854775807), row(rounds=1, tool_errors=True)],
            [row(rounds=9223372036854775807), row(rounds=2, tool_errors=False)]),
    }
    for name, (baseline, candidate) in pairs.items():
        try:
            result, error = compare(baseline, candidate), None
        except ValueError as exc:
            result, error = None, str(exc)
        comparisons.append(dict(name=name, baseline=baseline, candidate=candidate, result=result, error=error))
    def read(id, **window):
        return dict(type="tool_use", id=id, name="read_file", input=dict(path="log", **window))
    def output(id, text, **extra):
        return dict(type="tool_result", tool_use_id=id, content=text, **extra)
    transcripts = {
        "empty": [],
        "plain": [dict(role="user", content="question"), dict(role="assistant", content="answer")],
        "windows": [dict(role="assistant", content=[read("1"), read("2", offset=None, limit=None),
            read("3", offset=0), read("4", offset=1), read("5", offset=1),
            read("6", offset=1, limit=2), read("7", offset=1, limit=3)])],
        "rendered-errors": [dict(role="user", content=[output(str(i), text)
            for i, text in enumerate(("Error: failure", "\u3000Unknown tool x", "command (exit 2)\n",
                "command (exit 1٢)\u3000", "command (exit 0)", "command (exit 01)",
                "command (exit 2) more", "no failure", "command (exit ٢)"))])],
        "flag-is-not-judge": [dict(role="user", content=[output("1", "okay", is_error=True)])],
        "roles": [dict(role="user", content=[read("1")]), dict(role="assistant", content=[
            dict(type="tool_use", id="2", name="bash", input=dict(command="echo yes")), output("3", "Error happened")])],
    }
    behaviors = [dict(name=name, messages=messages, result=_behavioral_metrics(messages))
                 for name, messages in transcripts.items()]
    rounding = [dict(value=value, digits=digits, result=round(value, digits))
        for value in (2.675, 1.225, 0.0005, -0.0005, 0.0015, 25.05, 25.15, -25.05, 1e16, 1e-300)
        for digits in (1, 3)]
    return dict(aggregates=aggregates, comparisons=comparisons, behaviors=behaviors, rounding=rounding)


def _benchmark_task_contracts() -> dict:
    """Actual admitted task specs, seeded bytes and filesystem/text judges."""
    from mini_loop.benchmark import DEFAULT_TASKS, HELDOUT_TASKS

    tasks = DEFAULT_TASKS + HELDOUT_TASKS
    specs = [dict(name=t.name, prompt=t.prompt, setup=t.setup is not None,
                  tool_names=t.tool_names) for t in tasks]
    by_name = {t.name: t for t in tasks}
    cases = []
    with tempfile.TemporaryDirectory(prefix="go-benchmark-tasks-") as directory:
        scratch = Path(directory)
        def add(task, kind="missing", path="", text="", final="", hex_bytes=None):
            root = scratch / str(len(cases))
            root.mkdir()
            if path:
                target = root / path
                target.parent.mkdir(parents=True, exist_ok=True)
                if kind == "file":
                    target.write_bytes(bytes.fromhex(hex_bytes) if hex_bytes is not None else text.encode())
                elif kind == "directory":
                    target.mkdir()
                elif kind == "broken":
                    target.symlink_to("absent")
                elif kind == "loop":
                    target.symlink_to(target.name)
                elif kind == "symlink":
                    (target.parent / "actual").write_bytes(text.encode())
                    target.symlink_to("actual")
            try:
                passed = bool(by_name[task].expect(root, final))
                error = None
            except Exception as exc:
                passed, error = False, type(exc).__name__
            cases.append(dict(task=task, kind=kind, path=path,
                              hex_bytes=hex_bytes if hex_bytes is not None else text.encode().hex(),
                              final=final, passed=passed, error=error))
        for task, path in (("write-file", "greeting.txt"), ("edit-config", "config.ini"),
                           ("append-log", "notes.log"), ("nested-file", "src/app/main.txt")):
            for kind in ("missing", "directory", "broken", "loop"):
                add(task, kind, path)
        for text in ("HELLO", "shelloworld", "goodbye", "hÉllo"):
            add("write-file", "file", "greeting.txt", text)
        add("write-file", "file", "greeting.txt", hex_bytes="68656c6c6fff")
        for text in ("retries = 3", "retries = 3x", "retries=3"):
            add("edit-config", "file", "config.ini", text)
        add("edit-config", "file", "config.ini", hex_bytes="ff")
        for text in ("", "a\nb\n", "\n\n\n", "a\r\nb\rc", "a\vb\fc",
                     "a\x1cb\x1dc", "a\x1eb\x85c", "a\u2028b\u2029c", "a\x1fb\x1fc"):
            add("append-log", "file", "notes.log", text)
        add("append-log", "file", "notes.log", hex_bytes="ff")
        add("nested-file", "file", "src/app/main.txt", "wrong content")
        for task, path, text in (("write-file", "greeting.txt", "HELLO"),
                                 ("edit-config", "config.ini", "retries = 3"),
                                 ("append-log", "notes.log", "\n\n\n"),
                                 ("nested-file", "src/app/main.txt", "wrong")):
            add(task, "symlink", path, text)
        for task, values in (("arithmetic", ("12", "1120", "twelve")),
                             ("word-count", ("5", "15", "five")),
                             ("page-long-log", ("prefix token-04321 suffix", "token-04320")),
                             ("page-long-log-readonly", ("token-04321", ""))):
            for final in values:
                add(task, final=final)
        root = scratch / "seed"
        root.mkdir()
        DEFAULT_TASKS[3].setup(root)
        data = (root / "data.log").read_bytes()
        lines = data.decode().splitlines()
        seed = dict(size=len(data), sha256=hashlib.sha256(data).hexdigest(),
                    first=lines[0], deep=lines[4320], last=lines[-1], lines=len(lines))
    return dict(specs=specs, cases=cases, seed=seed)


def _benchmark_arm_contracts() -> dict:
    """Actual source fresh-session runner, default fake effects and fault stages."""
    import asyncio
    from contextlib import nullcontext
    from unittest.mock import patch
    from mini_loop.benchmark import DEFAULT_TASKS, HELDOUT_TASKS, BenchTask, run_arm
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text
    from mini_loop.manager import SessionManager as SourceManager

    async def collect():
        cases = []
        with tempfile.TemporaryDirectory(prefix="go-benchmark-arms-") as directory:
            root = Path(directory)
            managers = []
            class ObservedManager(SourceManager):
                def __init__(self, *args, **kwargs):
                    super().__init__(*args, **kwargs)
                    self.observed = []
                    managers.append(self)
                def create(self, **kwargs):
                    session = super().create(**kwargs)
                    self.observed.append(session)
                    return session
            def fail_setup(workspace): raise ValueError("setup fault")
            def fail_judge(workspace, final): raise ValueError("judge fault")
            def fail_run(kwargs): raise ValueError("run fault")
            def cancel_run(kwargs): raise asyncio.CancelledError()
            recipes = [
                ("visible", DEFAULT_TASKS, FakeAsyncAnthropic()),
                ("heldout", HELDOUT_TASKS, FakeAsyncAnthropic()),
                ("empty", (), FakeAsyncAnthropic()),
                ("judge-fault", (BenchTask("bad-judge", "judge", fail_judge),),
                 FakeAsyncAnthropic(responder=lambda kwargs: ([text("done")], "end_turn"))),
                ("setup-fault", (BenchTask("bad-setup", "setup", lambda w,f: True, setup=fail_setup),), FakeAsyncAnthropic()),
                ("provider-fault-recovered", (BenchTask("bad-run", "run", lambda w,f: True),), FakeAsyncAnthropic(responder=fail_run)),
                ("run-fault", (BenchTask("bad-run", "run", lambda w,f: True),), FakeAsyncAnthropic()),
                ("cancelled-provider", (BenchTask("cancelled", "cancel", lambda w,f: True),), FakeAsyncAnthropic(responder=cancel_run)),
            ]
            with patch.dict(os.environ, {}, clear=True), patch("mini_loop.manager.SessionManager", ObservedManager):
                for name, tasks, client in recipes:
                    settings = Settings(fake_llm=True, workspace_root=root/name,
                                        skills_dir=root/"skills", spill_dir=None)
                    try:
                        async def fail_session_run(session, prompt):
                            raise RuntimeError("run fault")
                        with patch("mini_loop.session.AgentSession.run", fail_session_run) if name == "run-fault" else nullcontext():
                            rows = await run_arm("test", settings, client, tasks)
                        # Wall time is checked natively, not treated as a stable
                        # fake/model measurement. Preserve every other source field.
                        for row in rows: row.pop("duration_ms")
                        error = None
                    except Exception as exc:
                        rows, error = None, type(exc).__name__ + ": " + str(exc)
                    except asyncio.CancelledError:
                        rows, error = None, "CancelledError"
                    manager = managers[-1]
                    sessions = [dict(owner=s.owner, mode=s.permission_mode,
                                     messages=s.agent.messages,
                                     files=sorted(str(p.relative_to(s.workspace)) for p in s.workspace.rglob("*") if p.is_file()),
                                     tools=s.agent.tools.names()) for s in manager.observed]
                    cases.append(dict(name=name, rows=rows, error=error, sessions=sessions))
                    # The real runner has returned; this probe joins its captured
                    # manager to avoid leaving source cleanup work live.
                    await manager.stop()
        return dict(cases=cases)
    return asyncio.run(collect())


def _benchmark_http_contracts() -> dict:
    """Actual fake-only HTTP arms; normalize wall timing alone."""
    from unittest.mock import patch
    from fastapi.testclient import TestClient
    from mini_loop.auth import TokenAuth
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager as SourceManager
    from mini_loop.server import create_app

    cases = []
    with tempfile.TemporaryDirectory(prefix="go-benchmark-http-") as scratch:
        root = Path(scratch)
        for name, raw, environment, main_fake in [
            ("default", "", {}, True),
            ("ignored-body", '{"real":true,"tasks":[],"model":"paid"}', {}, True),
            ("malformed-body", "{broken", {}, True),
            ("real-main", "", {}, False),
            ("one-round", "", {"MINILOOP_MAX_TURNS": "1"}, True),
        ]:
            managers = []
            class ObservedManager(SourceManager):
                def __init__(self, *args, **kwargs):
                    super().__init__(*args, **kwargs)
                    self.observed = []
                    managers.append(self)
                def create(self, **kwargs):
                    session = super().create(**kwargs)
                    self.observed.append(session)
                    return session
            with patch.dict(os.environ, environment, clear=True):
                settings = Settings(fake_llm=main_fake, workspace_root=root/name,
                                    skills_dir=root/"skills", trajectory_enabled=False,
                                    user_resources_root=None)
                main_client = FakeAsyncAnthropic(responder=lambda kwargs: (_ for _ in ()).throw(AssertionError("main provider invoked")))
                app = create_app(manager=SourceManager(settings, main_client))
                with TestClient(app, raise_server_exceptions=False) as client:
                    app.state.auth = TokenAuth({"token-a": "alice"})
                    unauthorized = client.post("/benchmark")
                    assert unauthorized.status_code == 401
                    with patch("mini_loop.manager.SessionManager", ObservedManager):
                        response = client.post("/benchmark?real=true", content=raw,
                            headers={"Authorization": "Bearer token-a", "Content-Type": "application/json"})
                    assert response.status_code == 200, response.text
                    body = response.json()
                    for row in body["baseline"] + body["candidate"]:
                        row.pop("duration_ms")
                    for key in ("comparison", "heldout_comparison"):
                        body[key]["dimensions"].pop("duration_ms", None)
                        body[key]["dimension_warnings"] = [w for w in body[key]["dimension_warnings"] if not w.startswith("duration_ms ")]
                    assert len(managers) == 4
                    workspace_roots = [m.settings.workspace_root for m in managers]
                    assert all(not path.exists() for path in workspace_roots)
                    assert len({id(m.client) for m in managers}) == 4
                    cases.append(dict(name=name, raw=raw, environment=environment,
                                      main_fake=main_fake, status=response.status_code,
                                      response=body, arms=[len(m.observed) for m in managers],
                                      cleaned=True, clients=4))
                    for manager in managers:
                        client.portal.call(manager.stop)
    return dict(cases=cases)


def _self_audit_contracts() -> dict:
    """Actual source report/suggestions over explicit observable runtime seams."""
    from types import SimpleNamespace as NS
    from mini_loop.self_audit import build_report, suggest_objectives, suggest_bench_tasks
    from mini_loop.problems import ProblemLog

    def ledger(entries, limit=50):
        log = ProblemLog(limit=limit)
        log.extend(entries)
        return dict(entries=list(log), summary=log.summary(), total=log.total(),
                    churning=log.churning())
    def session(sid, owner, created, activity="idle", problems=None):
        return dict(id=sid, owner=owner, created_at=created, activity=activity,
                    agent_present=True, problems=problems or {})
    records = [dict(id="slow-a", status="complete", partial=False, duration_ms=1250.0,
                    tool_uses=[dict(name="load_skill", skill="界"), dict(name="read_file")]),
               dict(id="slow-z", status="error", partial=True, duration_ms=1250.0,
                    tool_uses=[dict(name="load_skill", skill="界"), dict(name="load_skill")]),
               dict(id="fast", status="interrupted", partial=False, duration_ms=50.0,
                    tool_uses=[dict(name="load_skill", skill="safe")])]
    mixed = dict(sessions=[session("old", "alice", 1, "idle", dict(memory=ledger(["same", "old friction"]))),
                          session("new", "bob", 2, "running", dict(tasks=ledger(["bob private", "same"]))),
                          session("mid", "alice", 1.5, "waiting", dict(registry=ledger(["same", "last", "repeat", "repeat"])))],
                 problems=dict(cron=ledger(["global private", "same"]),
                               actions=ledger(["a", "b", "c", "a", "b", "c"],2)),
                 trajectories={}, cron=dict(jobs=["z", "a", "c"], armed=["c"]))
    mixed["trajectories"] = {"global": records, "by_session": {"old": [records[0]], "mid": [records[2]], "new": [records[1]]}, "has_events": True}
    cases=[]
    def capture(name, observation, owner=None, include_global=True, limit=8):
        def fail(class_name):
            def raise_error(*args, **kwargs):
                raise {"ValueError": ValueError, "RuntimeError": RuntimeError, "OSError": OSError}[class_name]("private failure content")
            return raise_error
        def log(data):
            class ObservedLog(list):
                def summary(self):
                    if data.get("failure"): return fail(data["failure"]["class"])()
                    return data.get("summary", list(self))
                def total(self): return data.get("total", len(self.summary()))
                def churning(self): return data.get("churning", False)
            return ObservedLog(data["entries"])
        manager=NS()
        live=[]
        for row in observation.get("sessions", []):
            state={}; agent=NS(tools=NS()) if row.get("agent_present") else None
            if agent is not None:
                for key, data in row.get("problems", {}).items():
                    if key=="registry": agent.tools.problems=log(data)
                    else: state[key]=NS(problems=log(data))
                agent.state=state
            info=fail(row["inspection_failure"]["class"]) if row.get("inspection_failure") else lambda row=row: dict(activity=row["activity"]) if "activity" in row else {}
            live.append(NS(id=row["id"], owner=row["owner"], created_at=row["created_at"], agent=agent, info=info))
        manager.list=fail(observation["sessions_failure"]["class"]) if observation.get("sessions_failure") else lambda: live
        for key, data in observation.get("problems", {}).items(): setattr(manager,key,NS(problems=log(data)))
        store=observation.get("trajectories")
        if store is not None:
            def summaries(*, session_id=None, limit=50):
                if store.get("trends_failure"): return fail(store["trends_failure"]["class"])()
                rows=store.get("global", []) if session_id is None else store.get("by_session", {}).get(session_id, [])
                return [dict(id=r["id"], status=r.get("status", "unknown"), partial=r.get("partial",False), **({"duration_ms":r["duration_ms"]} if "duration_ms" in r else {})) for r in rows[:limit]]
            def events(tid, *, types, limit):
                if store.get("usage_failure"): return fail(store["usage_failure"]["class"])()
                all_rows=store.get("global", [])+sum(store.get("by_session", {}).values(), [])
                row=next(r for r in all_rows if r["id"]==tid)
                if row.get("event_failure"): return fail(row["event_failure"]["class"])()
                return [dict(type="tool_use", name=e["name"], input=dict(name=e["skill"]) if "skill" in e else {}) for e in row.get("tool_uses", [])[:limit]]
            manager.trajectories=NS(list=summaries)
            if store.get("has_events"): manager.trajectories.iter_events=events
            if "trajectories" in observation.get("problems", {}): manager.trajectories.problems=log(observation["problems"]["trajectories"])
        cron=observation.get("cron", {})
        if cron.get("failure"):
            class BrokenCron:
                @property
                def jobs(self): return fail(cron["failure"]["class"])()
            manager.cron=BrokenCron()
            if "cron" in observation.get("problems", {}): manager.cron.problems=log(observation["problems"]["cron"])
        else:
            old=getattr(manager,"cron",NS())
            old.jobs=dict.fromkeys(cron.get("jobs", [])); old._armed=set(cron.get("armed", [])); manager.cron=old
        if observation.get("problems_failure"):
            class BrokenProblemSource:
                @property
                def problems(self): return fail(observation["problems_failure"]["class"])()
            manager.skills=BrokenProblemSource()
        report=build_report(manager, owner=owner, include_global=include_global)
        def suggestions(function):
            try: return function(manager,owner=owner,limit=limit), None
            except Exception as error: return None,type(error).__name__
        objectives,objectives_error=suggestions(suggest_objectives)
        drafts,drafts_error=suggestions(suggest_bench_tasks)
        cases.append(dict(name=name, observation=observation, owner=owner,
                          include_global=include_global, limit=limit, report=report,
                          objectives=objectives, objectives_error=objectives_error,
                          drafts=drafts, drafts_error=drafts_error))
    import copy
    capture("empty",dict(sessions=[],problems={},cron={}))
    capture("mixed",mixed)
    capture("unscoped-no-globals",mixed,include_global=False)
    capture("alice",mixed,"alice",False)
    capture("bob",mixed,"bob",False)
    capture("unknown-owner",mixed,"nobody",False)
    capture("explicit-owner-global",mixed,"alice",True)
    for limit in (-3,0,1,2): capture("limit-"+str(limit),mixed,limit=limit)
    chars=dict(sessions=[],problems=dict(skills=ledger(["\x1c ", "\x1f界  "+"界"*350,"界"*300,"é","界"*300])),cron={})
    capture("unicode-dedup-after-cap",chars)
    capped=dict(sessions=[],problems=dict(actions=ledger(["界"*9000])),cron={})
    capture("report-cap",capped)
    many=dict(sessions=[session("s"+str(i),"alice" if i%2 else "bob",i,"odd" if i%2 else "even") for i in range(240)],problems={},cron={})
    capture("session-cap-global",many)
    capture("session-cap-owner",many,"alice",False)
    fault=copy.deepcopy(mixed);fault["sessions"][0]["inspection_failure"]=dict(**{"class":"RuntimeError"});fault["problems"]["cron"]["failure"]=dict(**{"class":"ValueError"});fault["cron"]["failure"]=dict(**{"class":"OSError"})
    capture("independent-failures",fault)
    capture("sessions-unreadable",dict(sessions=[],sessions_failure=dict(**{"class":"RuntimeError"}),problems={},cron={}))
    fault=copy.deepcopy(mixed);fault["trajectories"]["trends_failure"]=dict(**{"class":"OSError"});fault["trajectories"]["usage_failure"]=dict(**{"class":"OSError"});capture("trajectory-failure",fault)
    fault=copy.deepcopy(mixed);fault["trajectories"]["global"][0]["event_failure"]=dict(**{"class":"ValueError"});capture("event-failure",fault)
    many=copy.deepcopy(mixed);rows=[dict(id="r%03d"%i,status="complete",tool_uses=[]) for i in range(60)];many["trajectories"]["global"]=rows;many["trajectories"]["by_session"]={"old":rows,"mid":rows,"new":rows};capture("trajectory-event-caps",many);capture("trajectory-owner-caps",many,"alice",False)
    shape=dict(sessions=[dict(id="no-agent",owner="anonymous",created_at=1,agent_present=False,problems=dict(registry=ledger(["excluded"]))),dict(id="unknown-activity",owner="anonymous",created_at=2,agent_present=True,problems={})],problems={key:ledger([key]) for key in ("cron","trajectories","approvals","skills","actions")},trajectories={"global":[],"by_session":{},"has_events":False},cron={})
    shape["problems"]["skills"]["summary"]=[]
    capture("all-ledgers-missing-seams",shape)
    capture("problem-source-failure",dict(sessions=[],problems={},problems_failure={"class":"OSError"},cron={}))
    capture("owner-collection-failure",dict(sessions=[],sessions_failure={"class":"RuntimeError"},problems={},trajectories={"global":records,"by_session":{},"has_events":True},cron={}),"alice",False)
    owner_cap=dict(sessions=[session("s"+str(i),"alice",i) for i in range(21)],problems={},trajectories={"global":[],"by_session":{"s"+str(i):[dict(id="r"+str(i),status="complete",tool_uses=[dict(name="load_skill",skill="excluded" if i==0 else "visible")])] for i in range(21)},"has_events":True},cron={})
    capture("recent-owned-session-cap",owner_cap,"alice",False)
    budget=copy.deepcopy(mixed);budget["trajectories"]["global"][0]["tool_uses"]=[dict(name="read_file")]*200+[dict(name="load_skill",skill="beyond-budget")];capture("event-scan-budget",budget)
    return dict(cases=cases)


def _problem_log_contracts() -> dict:
    """Actual source append/extend/clear/FIFO/count/churn state transitions."""
    from mini_loop.problems import ProblemLog
    cases = []
    recipes = [
        ("default", 50, []),
        ("duplicate-order", 3, [("append", [text]) for text in "abacda"]),
        ("churn-lifetime", 3, [("extend", list("abcd") * 100)]),
        ("clear", 2, [("extend", list("abca")), ("clear", []), ("extend", ["界", "界"])]),
        ("limit-one", 1, [("extend", ["a", "a", "b", "a", "a"])]),
        ("zero", 0, [("append", ["a"]), ("extend", ["b", "c"]), ("clear", [])]),
        ("negative", -1, [("append", ["a"]), ("clear", [])]),
        ("exact-text", 50, [("extend", ["", "", "A", "a", " a ", "界", "界", "é", "é"])]),
        ("capacity", 50, [
            ("extend", ["p" + str(i) for i in range(51)]),
            ("append", ["p1"]),
            ("append", ["p0"]),
        ]),
    ]
    for name, limit, operations in recipes:
        log = ProblemLog(limit=limit)

        def state():
            return dict(
                entries=[dict(text=text, count=log.counts[text]) for text in log],
                total=log.total(), dropped=log.dropped, limit=log.limit,
                churning=log.churning(),
            )

        steps = [dict(operation="snapshot", messages=[], state=state(), summary=log.summary(), error=None)]
        for operation, messages in operations:
            error = None
            try:
                if operation == "append":
                    log.append(messages[0])
                elif operation == "extend":
                    log.extend(messages)
                else:
                    log.clear()
            except Exception as exc:
                error = type(exc).__name__
            steps.append(dict(
                operation=operation, messages=messages, state=state(),
                summary=log.summary(), error=error,
            ))
        cases.append(dict(name=name, limit=limit, steps=steps))
    return dict(cases=cases)


def _self_audit_manager_contracts() -> dict:
    """Actual manager/session construction and activity/owner scan projections."""
    import asyncio
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.self_audit import build_report

    mixed = [dict(owner="alice", created=1.0, status="idle"),
             dict(owner="bob", created=3.0, status="error"),
             dict(owner="alice", created=2.0, status="running")]
    capped = [dict(owner="alice", created=float(i), status="idle") for i in range(105)]
    cases = []

    async def capture(root, name, rows, owner=None, include_global=True, bus_fault=False):
        settings = Settings(workspace_root=root / name, trajectory_enabled=False)
        manager = SessionManager(settings, FakeAsyncAnthropic())
        try:
            for row in rows:
                session = manager.create(owner=row["owner"], permission_mode="auto")
                session.created_at = row["created"]
                session.status = row["status"]
            if bus_fault:
                # Actual manager binds the shared mailbox as state['bus'], not
                # the state['teams'] slot scanned by _problem_sources. Retain
                # a real unread message while a bad key records a bus problem.
                mailbox = f"{session.id}/lead"
                manager.bus.send(mailbox, mailbox, "pending private mail")
                manager.bus.read("invalid")
                assert manager.bus.problems
                assert "teams" not in session.agent.state
            report = build_report(manager, owner=owner, include_global=include_global)
            cases.append(dict(name=name, sessions=rows, owner=owner,
                              include_global=include_global,
                              bus_fault=bus_fault,
                              report=report))
            if bus_fault:
                assert len(manager.bus.peek(mailbox)) == 1
                assert "teams[" not in report
                assert "pending private mail" not in report
        finally:
            await manager.stop()

    async def run(root):
        await capture(root, "empty", [])
        await capture(root, "fleet", mixed)
        await capture(root, "owned", mixed, "alice", False)
        await capture(root, "unknown-owner", mixed, "nobody", False)
        await capture(root, "fleet-cap", capped)
        await capture(root, "owner-cap", capped, "alice", False)
        await capture(root, "fleet-shared-bus-fault", mixed, bus_fault=True)
        await capture(root, "owned-shared-bus-fault", mixed, "alice", False, bus_fault=True)

    with tempfile.TemporaryDirectory(prefix="go-self-audit-manager-") as scratch:
        asyncio.run(run(Path(scratch)))
    return dict(cases=cases)


def _self_audit_http_contracts() -> dict:
    """Actual authenticated/open report and inert curation HTTP responses."""
    from fastapi.testclient import TestClient
    from mini_loop.auth import TokenAuth, NullAuth
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app
    from mini_loop.skills import SkillLoader
    from mini_loop.user_resources import UserResourceResolver

    scenarios = []
    entries = ["fleet private", "  ", "repeat", "fleet private", "unicode 界", "repeat"]
    with tempfile.TemporaryDirectory(prefix="go-self-audit-http-") as scratch:
        for mode in ("open", "authenticated", "local"):
            root = Path(scratch) / mode
            directory = root / "skills"
            directory.mkdir(parents=True)
            loader = SkillLoader(directory)
            resolver = UserResourceResolver(root / "users", loader) if mode == "local" else None
            settings = Settings(fake_llm=True, workspace_root=root / "workspaces",
                                trajectory_enabled=False, rate_limit_per_minute=1)
            manager = SessionManager(settings, FakeAsyncAnthropic(), skills=loader,
                                     user_resources=resolver)
            manager.skills.problems.extend(entries)
            app = create_app(manager=manager, settings=settings)
            with TestClient(app, raise_server_exceptions=False) as client:
                app.state.auth = NullAuth() if mode == "open" else TokenAuth({"token-a": "alice", "token-b": "bob"})
                ids = {}
                for index, (label, owner) in enumerate((("alice", "alice"), ("second", "alice"), ("bob", "bob"))):
                    session = manager.create(owner=owner, permission_mode="auto")
                    session.created_at = float(index)
                    ids[label] = session.id
                if mode == "local":
                    for label in ("alice", "bob"):
                        binding = manager.get(ids[label]).agent.state["memory"]
                        for _ in range(2):
                            binding.write(label + "-friction", "project", "",
                                          "界" * 32001)
                rows = []
                def capture(name, path, token="token-a", method="GET", body=""):
                    response = client.request(method, path, content=body,
                        headers={"Authorization": "Bearer " + token} if token else {})
                    text = response.text
                    for label, sid in ids.items():
                        text = text.replace(sid, "<" + label + ">")
                    value = json.loads(text) if response.headers.get("content-type") == "application/json" else text
                    rows.append(dict(name=name, method=method, path=path, token=token,
                                     body=body, status=response.status_code,
                                     content_type=response.headers.get("content-type"),
                                     challenge=response.headers.get("www-authenticate"),
                                     allow=response.headers.get("allow"), response=value))
                for suffix in ("", "/suggestions", "/bench-task-drafts"):
                    path = "/self-audit" + suffix
                    capture("alice" + suffix, path)
                    capture("bob" + suffix, path, "token-b")
                    capture("query-ignored" + suffix, path + "?owner=bob&include_global=true&limit=1")
                    capture("body-ignored" + suffix, path, body="{broken")
                    capture("no-auth" + suffix, path, "")
                    capture("query-auth" + suffix, path + "?access_token=token-a", "")
                    capture("bad-auth" + suffix, path, "bad")
                    capture("wrong-method" + suffix, path, method="POST")
                scenarios.append(dict(mode=mode, global_entries=entries, cases=rows))
    return dict(scenarios=scenarios)


def _self_audit_tool_contracts() -> dict:
    """Capture the real installed tool's schema, traits and keyword boundary."""
    import asyncio
    from mini_loop.builtins import default_registry
    from mini_loop.registry import ToolCall, ToolContext
    from mini_loop.self_audit import install_self_audit

    registry = default_registry()
    absent_from_default = registry.get("self_audit") is None
    install_self_audit(registry)
    tool = registry.get("self_audit")
    assert tool is not None
    cases = []
    no_manager_output = None
    with tempfile.TemporaryDirectory(prefix="mini-loop-self-audit-tool-") as scratch:
        ctx = ToolContext(agent=None, workspace=Path(scratch), state={})
        for value in ({}, {"owner": "other"}, {"include_global": True},
                      {"limit": 1}, {"unused": None}):
            try:
                output = asyncio.run(tool.run(ctx, **value))
                if not value:
                    no_manager_output = output
            except TypeError:
                accepted = False
            else:
                accepted = True
            cases.append(dict(input=value, accepted=accepted))
    return dict(schema=tool.schema, readonly=tool.readonly, risk=tool.risk,
                parallel_safe=tool.parallel_safe,
                execution_mode=tool.execution_mode(ToolCall("self_audit", {}, "audit")),
                absent_from_default=absent_from_default, cases=cases,
                no_manager_output=no_manager_output)


def _improvement_instrument_contracts() -> dict:
    """Observe the actual proposal acceptance-instrument classifier/digest."""
    from unittest.mock import patch
    from mini_loop.self_improve import verifier_touches, verifier_fingerprint

    path_sets = [[], ["mini_loop/agent.py", "tools/verify_guards.py",
                      ".github/workflows/ci.yml", "tests/conftest.py", "docs/README.md"],
                 ["prefix/tools/verify_any", "tools/VERIFY_x", "CONFTEst.py",
                  "my_conftest.py.bak", "x.github/workflows/y", "tools/verify_any",
                  "tools/verify_any", "tests\\conftest.py"]]
    touches = [dict(paths=paths, touched=verifier_touches(paths)) for paths in path_sets]
    scenarios = [
        ("empty", []),
        ("single", [dict(path="tools/verify_one.py", text="print('ok')\n")]),
        ("changed-bytes", [dict(path="tools/verify_one.py", text="print('changed')\n")]),
        ("renamed", [dict(path="tools/verify_other.py", text="print('ok')\n")]),
        ("all-patterns", [dict(path="tools/verify_z", text="z"), dict(path="tools/verify_a", text="a"),
                          dict(path=".github/workflows/ci.yml", text="ci"),
                          dict(path=".github/workflows/.hidden", text="hidden"),
                          dict(path="conftest.py", text="root"),
                          dict(path="tests/conftest.py", text="nested"),
                          dict(path="other/conftest.py", text="ignored"),
                          dict(path="tools/verify_dir/child", text="ignored"),
                          dict(path=".github/workflows/nested/child", text="ignored")]),
        ("unicode-and-binary", [dict(path="tools/verify_界", text="界\n"),
                                dict(path="tools/verify_é", hex="00ff800a"),
                                dict(path=".github/workflows/Ω", text="Ω")]),
        ("symlinks", [dict(path="notes.txt", text="target"),
                      dict(path="tools/verify_link", target="../notes.txt"),
                      dict(path="tools/verify_broken", target="../missing"),
                      dict(path="tools/verify_loop", target="verify_loop"),
                      dict(path="conftest.py/child", text="directory ignored")]),
        ("read-fault", [dict(path="tools/verify_one.py", text="private", unreadable=True)]),
    ]
    rows = []
    with tempfile.TemporaryDirectory(prefix="go-improvement-instruments-") as scratch:
        for name, files in scenarios:
            root = Path(scratch) / (name + "[literal]*?")
            root.mkdir()
            for item in files:
                path = root / item["path"]
                path.parent.mkdir(parents=True, exist_ok=True)
                if "target" in item:
                    path.symlink_to(item["target"])
                else:
                    path.write_bytes(bytes.fromhex(item["hex"]) if "hex" in item else item["text"].encode())
            read_bytes = Path.read_bytes
            unreadable = {str(root / item["path"]) for item in files if item.get("unreadable")}
            def read(path):
                if str(path) in unreadable:
                    raise PermissionError("private filesystem error")
                return read_bytes(path)
            with patch.object(Path, "read_bytes", read):
                fingerprint = verifier_fingerprint(root)
            rows.append(dict(name=name, files=files, fingerprint=fingerprint))
        missing = Path(scratch) / "missing"
        rows.append(dict(name="missing-root", files=[], missing=True,
                         fingerprint=verifier_fingerprint(missing)))
        rows.append(dict(name="nul-root", files=[], nul=True,
                         fingerprint=verifier_fingerprint(Path(scratch) / "\0")))
    return dict(touches=touches, fingerprints=rows)


def _improvement_archive_append_contracts() -> dict:
    """Observe actual source append, masking and best-effort filesystem behavior."""
    from unittest.mock import patch
    from types import SimpleNamespace
    from mini_loop.improvement_archive import ImprovementArchive
    from mini_loop.secrets import SecretRegistry

    cases = []
    recipes = [
        dict(name="missing", proposal={}),
        dict(name="filled", proposal=dict(objective="修复\n<&>", verified=True,
             rounds=3, branch="proposal/main", workspace="/workspace", diff_stat="a | 2 +",
             touches_verifiers=["tools/verify_guard.py"], integrity="clean",
             summary="omitted", next="omitted", future="omitted"), owner="alice", parent_id="imp_parent"),
        dict(name="empty", proposal=dict(objective="", verified=False, rounds=0, branch="",
             workspace="", diff_stat="", touches_verifiers=[], integrity="suspect"), owner=""),
        dict(name="masked-values", proposal=dict(objective="token-secret", touches_verifiers=["token-secret"]),
             owner="token-secret", parent_id="token-secret", secrets=["token-secret", "001122334455"]),
        dict(name="masked-key-collision", proposal=dict(objective="later-value"), owner="earlier-value",
             secrets=["owner", "objective"]),
        dict(name="root-file", proposal={}, fault="root-file"),
        dict(name="archive-directory", proposal={}, fault="archive-directory"),
    ]
    with tempfile.TemporaryDirectory(prefix="go-improvement-archive-") as scratch:
        for recipe in recipes:
            root = Path(scratch) / recipe["name"]
            if recipe.get("fault") == "root-file":
                root.write_text("occupied", encoding="utf-8")
            elif recipe.get("fault") == "archive-directory":
                (root / "archive.jsonl").mkdir(parents=True)
            registry = None
            if recipe.get("secrets"):
                registry = SecretRegistry(mask_with="[MASK]", min_length=1)
                for index, secret in enumerate(recipe["secrets"]):
                    registry.register(f"KEY_{index}", secret)
            archive = ImprovementArchive(root, secrets=registry)
            options = {key: recipe[key] for key in ("owner", "parent_id") if key in recipe}
            with patch("mini_loop.improvement_archive.uuid.uuid4", return_value=SimpleNamespace(hex="00112233445566778899aabbccddeeff")), \
                 patch("mini_loop.improvement_archive.time.time", return_value=1700000000.125):
                proposal_id = archive.record(recipe["proposal"], **options)
            rows = []
            if archive.path.is_file():
                rows = [json.loads(line) for line in archive.path.read_text(encoding="utf-8").splitlines()]
            cases.append({**recipe, "proposal_id": proposal_id, "rows": rows})
    return {"cases": cases}


def _improvement_archive_read_contracts() -> dict:
    """Actual source newest-first, arbitrary legacy JSON and owner-filter failures."""
    import hashlib
    from mini_loop.improvement_archive import ImprovementArchive
    cases = []
    recipes = [
        dict(name="unknown-fields", content='{"owner":"alice","future":{"k":[true,null,2.0]},"proposal_id":"old"}\n{"owner":"bob","rounds":"legacy"}\n{"owner":"alice","proposal_id":"new"}\n'),
        dict(name="owner-filter", content='{"owner":"alice","id":1}\n{"owner":"bob","id":2}\n{"owner":"alice","id":3}', owner="alice", limit=1),
        dict(name="empty-owner", content='{"owner":""}\n{}\n{"owner":null}\n{"owner":0}', owner=""),
        dict(name="malformed", content='{"owner":"alice","id":1}\n{broken\n\n{"owner":"bob","id":2}\n{"owner":"alice","id":3}trailing'),
        dict(name="zero-limit", content='1\n2\n3', limit=0),
        dict(name="negative-limit", content='1\n2\n3', limit=-10),
        dict(name="legacy-scalars", content='null\nfalse\n42\n["legacy",{"extra":true}]\n"text"'),
        dict(name="owner-shape", content='{"owner":"alice"}\n[]', owner="alice"),
        dict(name="owner-shape-after-match", content='[]\n{"owner":"alice"}', owner="alice"),
        dict(name="limit-stops-before-shape", content='[]\n{"owner":"alice"}', owner="alice", limit=1),
        dict(name="last-key-wins", content='{"owner":"bob","future":1,"owner":"alice","future":[2]}', owner="alice"),
        dict(name="python-lines", content='1\r\n2\r3\v4\f5\x1c6\x1d7\x1e8\x859\u202810\u202911'),
        dict(name="split-inside-string", content='{"owner":"alice","text":"line\u2028break"}'),
        dict(name="bom-line", content='1\n\ufeff{"owner":"alice"}'),
        dict(name="numbers", content='[-0,-0.0,9007199254740993,1e15,1e16,1e-5,1e-999,1.0000000000000001]'),
        dict(name="nonfinite", content='[NaN,Infinity,-Infinity,1e999]'),
        dict(name="escapes", content=r'{"text":"\u754c\ud83c\udf31\ud800x\udfff\b\f\n\r\t\u0000\u007f\\\"\/","\ud800":true}'),
        dict(name="missing", content="", fault="missing"),
        dict(name="archive-directory", content="", fault="archive-directory"),
        dict(name="invalid-utf8", content="", fault="invalid-utf8", limit=1),
        dict(name="large-row", width=30000),
        dict(name="integer-limit", integer_digits=4301),
        dict(name="integer-boundary", integer_digits=4300),
        dict(name="deep-valid", depth=500),
        dict(name="deep-error", depth=1005),
        dict(name="default-limit", records=205),
        dict(name="explicit-large-limit", records=205, limit=210),
    ]
    with tempfile.TemporaryDirectory(prefix="go-improvement-read-") as scratch:
        for recipe in recipes:
            root = Path(scratch) / recipe["name"]
            archive = ImprovementArchive(root)
            content = recipe.get("content", "")
            if "width" in recipe:
                content = '{"owner":"alice","body":"' + "界"*recipe["width"] + '"}'
            if "integer_digits" in recipe:
                content = "9"*recipe["integer_digits"]
            if "depth" in recipe:
                content = "["*recipe["depth"] + "0" + "]"*recipe["depth"]
            if "records" in recipe:
                content = "\n".join(str(index) for index in range(recipe["records"]))
            if recipe.get("fault") != "missing":
                root.mkdir()
                if recipe.get("fault") == "archive-directory":
                    archive.path.mkdir()
                elif recipe.get("fault") == "invalid-utf8":
                    archive.path.write_bytes(b"\xff\n1")
                else:
                    archive.path.write_text(content, encoding="utf-8")
            options = {key: recipe[key] for key in ("owner", "limit") if key in recipe}
            error = None
            digests = []
            try:
                rows = archive.list(**options)
                digests = [hashlib.sha256(json.dumps(row, ensure_ascii=True, separators=(",", ":")).encode()).hexdigest() for row in rows]
            except (AttributeError, UnicodeDecodeError, ValueError, RecursionError) as exc:
                error = type(exc).__name__
            cases.append({**recipe, "error": error, "row_digests": digests})
    return {"cases": cases}


def _improvement_http_read_contracts() -> dict:
    """Actual source owner admission and complete legacy lineage HTTP responses."""
    from fastapi.testclient import TestClient
    from mini_loop.auth import TokenAuth, NullAuth
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app
    from mini_loop.secrets import SecretRegistry

    ordinary = '{"owner":"alice","proposal_id":"first","future":{"unicode":"界🌱","token":"archive-secret-123"}}\n{"owner":"bob","proposal_id":"second"}\n{"owner":"alice","proposal_id":"third"}\n'
    recipes = [
        dict(name="alice", content=ordinary),
        dict(name="bob", content=ordinary, token="token-b"),
        dict(name="query-ignored", content=ordinary, path="/improvements?owner=bob&limit=1&include_global=true"),
        dict(name="body-ignored", content=ordinary, body="{broken"),
        dict(name="no-auth", content=ordinary, token=""),
        dict(name="bad-auth", content=ordinary, token="bad"),
        dict(name="query-auth", content=ordinary, token="", path="/improvements?access_token=token-a"),
        dict(name="wrong-method", content=ordinary, method="POST"),
        dict(name="malformed", content=ordinary+'{broken\n'),
        dict(name="empty", content=""),
        dict(name="missing", fault="missing"),
        dict(name="directory", fault="directory"),
        dict(name="invalid-utf8", fault="invalid-utf8"),
        dict(name="legacy-scalars", content='null\n[1,"legacy"]\nfalse'),
        dict(name="owner-type", content='{"owner":true}\n{"owner":null}\n{}'),
        dict(name="duplicate-owner", content='{"owner":"bob","owner":"alice","objective":"last"}'),
        dict(name="nonfinite", content='{"owner":"alice","future":[NaN,Infinity,1e999]}'),
        dict(name="foreign-nonfinite", content='{"owner":"bob","future":NaN}'),
        dict(name="surrogate-value", content=r'{"owner":"alice","future":"\ud800"}'),
        dict(name="surrogate-key", content=r'{"owner":"alice","\udfff":"legacy"}'),
        dict(name="foreign-surrogate", content=r'{"owner":"bob","future":"\ud800"}'),
        dict(name="finite-and-bigint", content='{"owner":"alice","future":[-0.0,9007199254740993,1e-5,1e15]}'),
        dict(name="default-cap", records=205),
    ]
    scenarios = []
    with tempfile.TemporaryDirectory(prefix="go-improvement-http-") as scratch:
        for mode in ("open", "authenticated"):
            root = Path(scratch)/mode
            settings = Settings(fake_llm=True, workspace_root=root, trajectory_enabled=False,
                                rate_limit_per_minute=1)
            secrets = SecretRegistry()
            secrets.register("ARCHIVE_TOKEN", "archive-secret-123")
            manager = SessionManager(settings, FakeAsyncAnthropic(), secrets=secrets)
            startup_archive_exists = manager.improvements.root.exists()
            app = create_app(manager=manager, settings=settings)
            captured = []
            with TestClient(app, raise_server_exceptions=False) as client:
                app.state.auth = NullAuth() if mode=="open" else TokenAuth({"token-a":"alice","token-b":"bob"})
                for recipe in recipes:
                    path = manager.improvements.path
                    if path.is_file():
                        path.unlink()
                    elif path.is_dir():
                        path.rmdir()
                    path.parent.mkdir(exist_ok=True)
                    content = recipe.get("content", "")
                    if "records" in recipe:
                        content = "\n".join(json.dumps({"owner":"alice", "index":index}) for index in range(recipe["records"]))
                    fault = recipe.get("fault")
                    if fault=="directory":
                        path.mkdir()
                    elif fault=="invalid-utf8":
                        path.write_bytes(b"\xff")
                    elif fault!="missing":
                        path.write_text(content, encoding="utf-8")
                    token = recipe.get("token", "token-a")
                    response = client.request(recipe.get("method", "GET"), recipe.get("path", "/improvements"),
                        content=recipe.get("body", ""), headers={"Authorization":"Bearer "+token} if token else {})
                    value = response.json() if response.headers.get("content-type")=="application/json" else response.text
                    captured.append({**recipe, "status":response.status_code,
                        "content_type":response.headers.get("content-type"),
                        "challenge":response.headers.get("www-authenticate"),
                        "allow":response.headers.get("allow"), "response":value})
                    if "records" in recipe:
                        import hashlib
                        captured[-1].pop("response")
                        captured[-1]["response_digest"] = hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
                        captured[-1]["response_rows"] = len(value["proposals"])
            scenarios.append(dict(mode=mode, startup_archive_exists=startup_archive_exists, cases=captured))
    return dict(scenarios=scenarios)


def _verified_fold_contracts() -> dict:
    """Actual pure receipt-authorized folds and byte-for-byte canonical identities."""
    from dataclasses import asdict
    from mini_loop.verified_loop import (RequirementV1, TaskContractV1, VerifiedCheckpointV1,
        ArtifactV1, FactV1, AuditReceiptV1, StatePatchV1, apply_patch)
    contract = TaskContractV1(run_id="run-界", revision=1, original_request_hash="original",
        requirements=(RequirementV1("tests", "the suite <&> is green"),
                      RequirementV1("docs", "docs cover the flag", blocking=False)),
        allowed_surfaces=("README",), persistence_boundary="workspace", contamination_rules=("clean",))
    checkpoint = VerifiedCheckpointV1(contract_revision=1, state_revision=0,
        requirements=(("tests", "pending"), ("docs", "pending")), blockers=("duplicate", "duplicate"))
    recipes = [dict(name="empty", operations=[]),
        dict(name="nonverified-statuses", operations=[dict(kind="status", id="tests", status="blocked"),dict(kind="status", id="docs", status="untrusted")]),
        dict(name="bare-verified", operations=[dict(kind="status", id="tests", status="verified")]),
        dict(name="stale", base=-1, operations=[]),
        dict(name="revision-mismatch", contract_revision=2, operations=[]),
        dict(name="unknown-status", operations=[dict(kind="status", id="tests", status="done")]),
        dict(name="missing-requirement", operations=[dict(kind="status", id="missing", status="pending")]),
        dict(name="missing-blocker", operations=[dict(kind="clear_blocker", blocker="missing")]),
        dict(name="atomic-refusal", operations=[dict(kind="add_blocker", blocker="new"),dict(kind="status", id="tests", status="verified")]),
        dict(name="typed-artifacts-facts", operations=[dict(kind="artifact", artifact=dict(digest="digest", producer="worker", evidence_refs=["e1","界"])),dict(kind="fact", fact=dict(source="command", content="facts <&>", freshness="current", trust="untrusted")),dict(kind="clear_blocker",blocker="duplicate"),dict(kind="add_blocker",blocker="new")]),
        dict(name="foreign-unused-receipt", operations=[], receipts=[dict(contract_hash="foreign", verdict="complete", integrity="clean", coverage=["tests"])]),
        dict(name="duplicate-state", duplicate=True, operations=[dict(kind="status",id="docs",status="blocked")]),
        dict(name="checkpoint-extra-id", extra=True, operations=[dict(kind="status",id="extra",status="verified")],receipts=[dict(verdict="complete",integrity="clean",coverage=["extra"])]),
    ]
    for verdict in ("complete", "incomplete", "blocked"):
        for integrity in ("clean", "suspect", "violation"):
            for coverage in (["tests"], ["docs"]):
                recipes.append(dict(name=f"receipt-{verdict}-{integrity}-"+coverage[0],
                    operations=[dict(kind="status", id="tests", status="verified")],
                    receipts=[dict(verdict=verdict, integrity=integrity, coverage=coverage)]))
    recipes.append(dict(name="one-covering-receipt", operations=[dict(kind="status",id="tests",status="verified")],receipts=[dict(verdict="incomplete",integrity="suspect",coverage=["tests"]),dict(verdict="complete",integrity="clean",coverage=["tests"])]))
    cases = []
    for recipe in recipes:
        pairs = list(checkpoint.requirements)
        if recipe.get("duplicate"):
            pairs = [("tests","pending"),("docs","pending"),("tests","blocked")]
        if recipe.get("extra"):
            pairs.append(("extra","pending"))
        state = VerifiedCheckpointV1(contract_revision=recipe.get("contract_revision",1), state_revision=0,
            requirements=tuple(pairs), blockers=checkpoint.blockers)
        operations=[]
        for op in recipe["operations"]:
            if op["kind"]=="status":operations.append(("set_requirement_status",op["id"],op["status"]))
            elif op["kind"]=="artifact":operations.append(("add_artifact",ArtifactV1(**op["artifact"])))
            elif op["kind"]=="fact":operations.append(("add_fact",FactV1(**op["fact"])))
            else:operations.append((op["kind"],op["blocker"]))
        receipts=[AuditReceiptV1(contract_hash=row.get("contract_hash",contract.contract_hash), round_id=f"round-{i}",
            verdict=row["verdict"],integrity=row["integrity"],coverage=tuple(row["coverage"]), evidence_refs=("exit:0",),verifier_ids=("command",)) for i,row in enumerate(recipe.get("receipts",[]))]
        outcome=None;error=None
        try:
            outcome=apply_patch(contract,state,StatePatchV1(recipe.get("base",0),tuple(operations),tuple(receipts))).canonical()
        except ValueError as exc:
            error=str(exc)
        state_spec=dict(contract_revision=state.contract_revision,state_revision=state.state_revision,
            requirements=[dict(id=rid,status=status) for rid,status in state.requirements],artifacts=[],facts=[],blockers=list(state.blockers))
        cases.append(dict(name=recipe["name"],checkpoint=state_spec,base=recipe.get("base",0),
            operations=recipe["operations"],receipts=[asdict(r) for r in receipts],canonical=outcome,error=error,
            initial_canonical=state.canonical(),statuses=[state.status_of("tests"),state.status_of("missing")]))
    return dict(contract=asdict(contract),contract_hash=contract.contract_hash,cases=cases)


def _verified_service_contracts() -> dict:
    """Run the actual coordinator over explicit worker/command/probe seams."""
    import asyncio
    from dataclasses import asdict
    from types import SimpleNamespace
    from mini_loop.verified_loop_service import VerifiedLoopService
    from mini_loop.tools import CommandResult
    def result(exit_code=0, **kwargs):
        return {**dict(stdout="",stderr="",exit_code=exit_code,timed_out=False,
            overflowed=False,duration_ms=0), **kwargs}
    cases = [
        dict(name="first-pass", results=[result()], summaries=["done"]),
        dict(name="feedback-repair", results=[result(1),result()], summaries=["wrong","fixed"]),
        dict(name="prose-cannot-complete", maximum=2, results=[result(7),result(7)], summaries=["complete","complete"]),
        dict(name="nil-exit", maximum=1, results=[result(None)]),
        dict(name="timeout", maximum=1, results=[{**result(),"timed_out":True,"error":"deadline"}]),
        dict(name="overflow-zero-source", maximum=1, results=[{**result(),"overflowed":True}]),
        dict(name="error-zero-source", maximum=1, results=[result(error="harness diagnostic")]),
        dict(name="tampered", maximum=1, results=[result()], probes=["a","b"]),
        dict(name="restored-next-round", results=[result(),result()], probes=["a","b","a"]),
        dict(name="baseline-none", results=[result()], probes=[None,"b"]),
        dict(name="current-none", maximum=1, results=[result()], probes=["a",None]),
        dict(name="zero-rounds", maximum=0, results=[], probes=["a"]),
        dict(name="negative-rounds", maximum=-2, results=[]),
        dict(name="unicode-limits", request="界🌱"*1400, command="command:"+"界"*140, results=[result()]),
        dict(name="feedback-tail", maximum=2, results=[result(2,stdout="prefix"+"🌱"*2100),result(3,stderr="last")]),
        dict(name="empty-request-command", request="", command="", results=[result()]),
        dict(name="worker-error", failure="worker", results=[result()]),
        dict(name="acceptance-error", failure="acceptance", results=[result()]),
        dict(name="initial-probe-error", failure="probe", probes=["a"], results=[result()]),
        dict(name="round-event-error", failure="verified_round", results=[result()]),
        dict(name="receipt-event-error", failure="verified_receipt", results=[result()]),
        dict(name="checkpoint-event-error", failure="verified_checkpoint", results=[result()]),
    ]
    async def capture(recipe):
        events=[];objectives=[];trace=[];probe_calls=0;command_calls=0;summary_calls=0
        failure=recipe.get("failure")
        async def emit(event):
            trace.append(event["type"])
            if failure==event["type"]:
                raise RuntimeError(failure+" failed")
            events.append(event)
        async def worker(objective,role):
            nonlocal summary_calls
            assert role=="worker"
            trace.append("worker");objectives.append(objective)
            if failure=="worker":
                raise RuntimeError("worker failed")
            summary_calls+=1
            summaries=recipe.get("summaries",["worker-summary"])
            return summaries[min(summary_calls-1,len(summaries)-1)]
        def command(text):
            nonlocal command_calls
            assert text==recipe.get("command","accept")
            trace.append("acceptance")
            if failure=="acceptance":
                raise RuntimeError("acceptance failed")
            row=recipe["results"][command_calls];command_calls+=1
            return CommandResult(**row)
        def probe():
            nonlocal probe_calls
            trace.append("probe")
            if failure=="probe":
                raise RuntimeError("probe failed")
            value=recipe["probes"][probe_calls];probe_calls+=1
            return value
        session=SimpleNamespace(id="run-service",agent=SimpleNamespace(_run_subagent=worker,
            toolset=SimpleNamespace(run_bash_result=command)),emit=emit)
        kwargs=dict(acceptance_command=recipe.get("command","accept"))
        if "maximum" in recipe:
            kwargs["max_rounds"]=recipe["maximum"]
        if "probes" in recipe:
            kwargs["integrity_probe"]=probe
        outcome=None;error=None
        try:
            raw=await VerifiedLoopService(session).run_task(recipe.get("request","repair the workspace"),**kwargs)
            outcome={**raw,"checkpoint":raw["checkpoint"].canonical(),"receipts":[asdict(r) for r in raw["receipts"]]}
        except RuntimeError as exc:
            error=str(exc)
        return {**recipe,"events":events,"objectives":objectives,"trace":trace,"outcome":outcome,"error":error}
    return dict(cases=asyncio.run(_gather_verified_services(cases,capture)))


async def _gather_verified_services(cases, capture):
    return [await capture(case) for case in cases]


def _improvement_proposal_contracts() -> dict:
    """Actual source proposal composition, including its real verified coordinator."""
    import asyncio
    from types import SimpleNamespace
    from unittest.mock import patch
    from mini_loop.self_improve import propose_improvement
    from mini_loop.tools import CommandResult
    cases = [
        dict(name="commit", status=" M code.py\n"),
        dict(name="no-change", status=""),
        dict(name="unverified-still-commits", status="?? new.py\n", acceptance_exit=1),
        dict(name="add-failed-still-attempts-commit", status=" M code.py\n", add_exit=3),
        dict(name="commit-failed", status="?? new.py\n", commit_exit=1),
        dict(name="nil-commit-exit", status=" M code.py\n", commit_exit=None),
        dict(name="touches-renames-and-order", status=" M tools/verify_a.py\nR  old -> .github/workflows/new.yml\n?? conftest.py\n?? conftest.py\n", archive=True, parent="imp_parent", owner="tenant"),
        dict(name="unicode-lines", status="?? tools/verify_界.py\u0085?? conftest.py\r\nX\n", objective="界🌱"*300, archive=True),
        dict(name="archive-null-parent", status="", archive=True),
        dict(name="parent-without-archive", status="", parent="imp_parent"),
        dict(name="zero-rounds", status="", maximum=0),
        dict(name="blank-command", command="\u2003\t"),
        dict(name="not-git", repository=False),
        dict(name="status-error", failure="git status --porcelain -uall"),
        dict(name="commit-error", status=" M code.py\n", failure="git commit -m 'self-improvement proposal' --no-verify"),
        dict(name="archive-error", status="", archive=True, failure="archive"),
        dict(name="event-error", status="", failure="event"),
    ]
    async def capture(recipe):
        calls=[]; events=[]; archive_rows=[]
        failure=recipe.get("failure")
        async def emit(event):
            calls.append(event["type"])
        async def worker(objective, role):
            assert role=="worker"
            calls.append("worker")
            return "worker-summary"
        def command(text):
            calls.append(text)
            if failure==text:
                raise RuntimeError(text+" failed")
            rows={
                recipe.get("command","accept"):dict(exit_code=recipe.get("acceptance_exit",0)),
                "git status --porcelain -uall":dict(stdout=recipe.get("status","")),
                "git add -A":dict(exit_code=recipe.get("add_exit",0)),
                "git commit -m 'self-improvement proposal' --no-verify":dict(exit_code=recipe.get("commit_exit",0)),
                "git diff --stat HEAD~1 HEAD":dict(stdout=" code.py | 1 +\n"),
                "git rev-parse --abbrev-ref HEAD":dict(stdout=" proposal-branch\n"),
            }
            return CommandResult(**{**dict(stdout="",stderr="",exit_code=0,timed_out=False,overflowed=False,duration_ms=0),**rows[text]})
        def record(proposal, *, owner, parent_id):
            calls.append("archive")
            if failure=="archive":
                raise RuntimeError("archive failed")
            row={key:proposal[key] for key in ("objective","verified","rounds","branch","workspace","diff_stat","touches_verifiers","integrity")}
            archive_rows.append(dict(fields=row,owner=owner,parent_id=parent_id))
            return "imp_fixture"
        async def send(kind, **payload):
            calls.append(kind)
            if failure=="event":
                raise RuntimeError("event failed")
            events.append(payload)
        session=SimpleNamespace(id="proposal-run",emit=emit,agent=SimpleNamespace(workspace="<workspace>",_run_subagent=worker,_send=send,toolset=SimpleNamespace(run_bash_result=command)))
        proposal=None;error=None
        with patch("mini_loop.self_improve.is_git_repo",return_value=recipe.get("repository",True)), patch("mini_loop.self_improve.verifier_fingerprint",return_value="unchanged"):
            try:
                proposal=await propose_improvement(session,recipe.get("objective","improve code"),acceptance_command=recipe.get("command","accept"),max_rounds=recipe.get("maximum",1),archive=SimpleNamespace(record=record) if recipe.get("archive") else None,owner=recipe.get("owner","anonymous"),parent_id=recipe.get("parent"))
            except (RuntimeError,ValueError) as exc:
                error=str(exc)
        return {**recipe,"calls":calls,"events":events,"archive_rows":archive_rows,"proposal":proposal,"error":error}
    return dict(cases=asyncio.run(_gather_verified_services(cases,capture)))


def _improvement_proposal_http_contracts() -> dict:
    """Actual FastAPI proposal admission; effects have an explicit observed seam."""
    from unittest.mock import patch
    from types import SimpleNamespace
    from fastapi.testclient import TestClient
    from mini_loop.auth import TokenAuth
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app
    from mini_loop.self_improve import propose_improvement
    cases=[]
    good=dict(objective="improve",acceptance_command="accept")
    recipes=[dict(name="default"),dict(name="again-no-rate"),dict(name="extra-ignored",body={**good,"owner":"bob","workspace":"/untrusted","unknown":float("nan")}),
        dict(name="no-content-type",content_type=None),dict(name="text-content",content_type="text/plain"),dict(name="empty-body",raw=""),
        dict(name="malformed",raw="{broken"),dict(name="array",body=[]),dict(name="null",body=None),dict(name="missing-fields",body={}),
        dict(name="empty-fields",body=dict(objective="",acceptance_command="")),dict(name="long-objective",body={**good,"objective":"界"*4001}),
        dict(name="long-command",body={**good,"acceptance_command":"界"*1001}),dict(name="parent-number",body={**good,"parent_id":4}),
        dict(name="parent-null",body={**good,"parent_id":None}),dict(name="parent-empty",body={**good,"parent_id":""}),
        dict(name="foreign",target="bob"),dict(name="unknown",target="missing"),dict(name="foreign-invalid",target="bob",body={}),
        dict(name="no-auth",token=""),dict(name="bad-auth",token="bad"),dict(name="busy",busy=True),
        dict(name="blank-acceptance",body={**good,"acceptance_command":" \t"},action="real"),dict(name="non-git",action="real"),
        dict(name="runtime-error",action="error"),dict(name="wrong-method",method="GET")]
    for value in (0,11,True,False,3.0,2.5,None,"  +03  ","1_0","2.00","2e0","bad","9"*100,float("inf"),float("nan")):
        recipes.append(dict(name="rounds-"+str(value)[:12],body={**good,"max_rounds":value}))
    with tempfile.TemporaryDirectory(prefix="go-proposal-http-") as scratch:
        settings=Settings(fake_llm=True,workspace_root=Path(scratch)/"workspaces",trajectory_enabled=False,rate_limit_per_minute=1)
        manager=SessionManager(settings,FakeAsyncAnthropic())
        ids={owner:manager.create(owner=owner).id for owner in ("alice","bob")}
        app=create_app(manager=manager,settings=settings)
        with TestClient(app,raise_server_exceptions=False) as client:
            app.state.auth=TokenAuth({"token-a":"alice","token-b":"bob"})
            for recipe in recipes:
                target=recipe.get("target","alice");sid=ids.get(target,"missing")
                session=manager.get(sid);calls=[]
                async def observed(session,objective,*,acceptance_command,max_rounds,archive,owner,parent_id):
                    calls.append(dict(owner=owner,objective=objective,command=acceptance_command,maximum=max_rounds,parent_id=parent_id))
                    if recipe.get("action")=="real":
                        return await propose_improvement(session,objective,acceptance_command=acceptance_command,max_rounds=max_rounds,archive=archive,owner=owner,parent_id=parent_id)
                    if recipe.get("action")=="error":
                        raise RuntimeError("credential")
                    return dict(objective=objective,verified=True,rounds=1,summary="done",workspace="<workspace>",branch="proposal",diff_stat="(no changes)",touches_verifiers=[],integrity="clean",next="review the diff on the branch; merge only after the paired benchmark and your own read agree it is an improvement",proposal_id="<proposal>",parent_id=parent_id)
                if recipe.get("busy"):
                    session._running=SimpleNamespace(done=lambda:False)
                raw=recipe.get("raw",json.dumps(recipe.get("body",good),ensure_ascii=False))
                token=recipe.get("token","token-a");headers={"Authorization":"Bearer "+token} if token else {}
                content_type=recipe.get("content_type","application/json")
                if content_type is not None: headers["Content-Type"]=content_type
                with patch("mini_loop.self_improve.propose_improvement",observed):
                    response=client.request(recipe.get("method","POST"),f"/sessions/{sid}/propose-improvement",content=raw,headers=headers)
                if recipe.get("busy"): session._running=None
                text=response.text
                for owner,value in ids.items(): text=text.replace(value,"<"+owner+">")
                body=json.loads(text) if response.headers.get("content-type")=="application/json" else text
                cases.append(dict(name=recipe["name"],raw=raw,token=token,target=target,method=recipe.get("method","POST"),content_type=content_type,busy=recipe.get("busy",False),action=recipe.get("action","success"),status=response.status_code,response=body,calls=calls))
    return dict(cases=cases)


def _team_bus_contracts() -> dict:
    """Actual MessageBus send/peek/read, including historical JSONL records."""
    from unittest.mock import patch
    from mini_loop.teams import MessageBus
    from mini_loop.secrets import SecretRegistry

    secret = 'credential-界-"-\\-value'
    normal = dict(action="send", to="team/bob", content="hello")
    recipes = [
        dict(name="memory-default", memory=True, operations=[normal, dict(action="peek"), dict(action="peek"), dict(action="read"), dict(action="read")]),
        dict(name="disk-default", operations=[normal, dict(action="peek"), dict(action="peek"), dict(action="read"), dict(action="read")]),
        dict(name="memory-cap", memory=True, count=150),
        dict(name="memory-injected-overflow", memory=True, injected=500),
        dict(name="disk-overflow", count=150),
        dict(name="disk-exact-cap", count=100),
        dict(name="content-limit", operations=[dict(action="send", content="界", repeat=16000), dict(action="read")]),
        dict(name="content-over-limit", operations=[dict(action="send", content="界", repeat=16001), dict(action="read")]),
        dict(name="empty-content-type", operations=[dict(action="send", content="", type=""), dict(action="read")]),
        dict(name="memory-free-key", memory=True, operations=[dict(action="send", to="arbitrary", content="ok"), dict(action="read", to="arbitrary")]),
        dict(name="nested-masking", mask=True, operations=[dict(action="send", content=secret, metadata={secret:[secret, {"private":secret}, True, 3]}, extra={"extra":secret}), dict(action="peek"), dict(action="read")]),
        dict(name="memory-not-masked", memory=True, mask=True, operations=[dict(action="send", content=secret), dict(action="read")]),
        dict(name="extra-overrides", operations=[dict(action="send", content="checked", metadata={"request_id":"req_1","approve":True}, extra={"from":"team/other","ts":3,"type":"custom"}), dict(action="read")]),
        dict(name="malformed-and-nonobjects", preload='{"content":"first"}\nnot json\n\n[]\n4\nnull\n{"content":"last","metadata":null,"extra":{"a":1}}\n'),
        dict(name="unicode-line-breaks", preload='{"content":"a"}\r\n{"content":"b"}\r{"content":"c"}\u2028{"content":"d"}\n'),
        dict(name="duplicate-keys", preload='{"a":1,"b":2,"a":3,"from":"team/a","content":"dup"}\n'),
        dict(name="nonfinite-surrogates", preload='{"content":"\\ud800","metadata":{"nan":NaN,"inf":Infinity,"over":1e400,"minus":-Infinity}}\n'),
        dict(name="integer-limit", preload='{"n":'+"9"*4301+'}\n'),
        dict(name="bad-utf8", preload_hex="7bff7d0a"),
        dict(name="empty-file", preload=""),
        dict(name="tail-bounded", padding=MessageBus.MAX_READ_BYTES+100, preload='{"content":"last1"}\n{"content":"last2"}\n'),
        dict(name="tail-no-newline", padding=MessageBus.MAX_READ_BYTES+100, preload=""),
    ]
    for key in ("../lead", "./lead", "team/..", "team/.", "../../etc/passwd", "team/", "team/a/b", "界/bob", "x"*65+"/bob"):
        recipes.append(dict(name="key-"+key, operations=[dict(action="send", to=key, content="private"), dict(action="peek", to=key), dict(action="read", to=key)]))
    recipes.append(dict(name="dot-names-legal", operations=[dict(action="send",to=".team/.bob",content="ok"),dict(action="read",to=".team/.bob")]))
    cases = []
    for recipe in recipes:
        with tempfile.TemporaryDirectory(prefix="go-team-bus-") as scratch:
            root = Path(scratch)/"teams"
            bus = MessageBus(None if recipe.get("memory") else root,
                             secrets=SecretRegistry.from_environ(environ={"P_API_KEY":secret}) if recipe.get("mask") else None)
            path = root/"team/inboxes/bob.jsonl"
            if recipe.get("injected"):
                bus.inboxes["team/bob"] = [{"from":"s","to":"team/bob","content":f"m{i:03d}"} for i in range(recipe["injected"])]
            if "preload" in recipe or "preload_hex" in recipe:
                path.parent.mkdir(parents=True)
                data = bytes.fromhex(recipe["preload_hex"]) if "preload_hex" in recipe else recipe["preload"].encode()
                path.write_bytes(b"x"*recipe.get("padding",0)+data)
            operations = recipe.get("operations", [dict(action="send", content=f"m{i:03d}") for i in range(recipe.get("count",0))] + [dict(action="peek"), dict(action="read"), dict(action="read")])
            steps = []
            with patch("mini_loop.teams.time.time", return_value=1000.0):
                for operation in operations:
                    key = operation.get("to", "team/bob")
                    encoded_operation = dict(operation)
                    for field in ("metadata", "extra"):
                        if field in encoded_operation:
                            encoded_operation[field+"_json"] = json.dumps(encoded_operation.pop(field),ensure_ascii=True,separators=(",",":"))
                    step = dict(operation=encoded_operation)
                    try:
                        if operation["action"] == "send":
                            kwargs = dict(operation.get("extra",{}))
                            result = bus.send("team/lead", key, operation.get("content","")*operation.get("repeat",1), operation.get("type","message"), operation.get("metadata"), **kwargs)
                            step["text"] = result
                        else:
                            result = getattr(bus,operation["action"])(key)
                            step["rows"] = [json.dumps(row,ensure_ascii=True,separators=(",",":")) for row in result]
                    except Exception as error:
                        step["error"] = type(error).__name__
                    step["problems"] = [str(value).replace(str(root),"<root>") for value in bus.problems]
                    if not recipe.get("memory"):
                        step["exists"] = path.exists()
                        step["disk_hash"] = hashlib.sha256(path.read_bytes()).hexdigest() if path.exists() and path.stat().st_size < 10000 else None
                    steps.append(step)
            cases.append(dict(recipe={key:value for key,value in recipe.items() if key!="operations"},steps=steps))
    return dict(secret=secret,cases=cases)


def _team_http_contracts() -> dict:
    """Actual default team identity and non-consuming owned HTTP projection."""
    from fastapi.testclient import TestClient
    from mini_loop.auth import TokenAuth
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.server import create_app
    cases = []
    with tempfile.TemporaryDirectory(prefix="go-team-http-") as scratch:
        settings = Settings(fake_llm=True,workspace_root=Path(scratch)/"workspaces",trajectory_enabled=False,rate_limit_per_minute=1)
        manager = SessionManager(settings,FakeAsyncAnthropic())
        ids = {owner:manager.create(owner=owner).id for owner in ("alice","bob")}
        app = create_app(manager=manager,settings=settings)
        with TestClient(app,raise_server_exceptions=False) as client:
            app.state.auth=TokenAuth({"token-a":"alice","token-b":"bob"})
            sid = ids["alice"]
            path = manager.bus._path(sid+"/lead")
            for recipe in (dict(name="default"),dict(name="foreign",target="bob"),dict(name="unknown",target="missing"),dict(name="no-auth",token=""),dict(name="bad-auth",token="bad"),dict(name="peek-50",rows=75),dict(name="peek-again"),dict(name="nonfinite",preload='{"content":"bad","n":NaN}\n'),dict(name="surrogate",preload='{"content":"\\ud800"}\n'),dict(name="bad-encoding",preload_hex="ff"),dict(name="teamless",teamless=True),dict(name="wrong-method",method="POST")):
                if "rows" in recipe:
                    path.parent.mkdir(parents=True,exist_ok=True)
                    path.write_text("".join(json.dumps(dict(content=f"m{i:03d}",metadata={},extra=[1,True]))+"\n" for i in range(recipe["rows"])))
                if "preload" in recipe or "preload_hex" in recipe:
                    path.write_bytes(bytes.fromhex(recipe["preload_hex"]) if "preload_hex" in recipe else recipe["preload"].encode())
                agent = manager.get(sid).agent
                team = agent.state.pop("team_id",None) if recipe.get("teamless") else None
                token = recipe.get("token","token-a")
                target = recipe.get("target","alice")
                response = client.request(recipe.get("method","GET"),f"/sessions/{ids.get(target,'missing')}/team",headers={"Authorization":"Bearer "+token} if token else {})
                if recipe.get("teamless"): agent.state["team_id"]=team
                text = response.text
                for owner,value in ids.items(): text=text.replace(value,"<"+owner+">")
                body = json.loads(text) if response.headers.get("content-type")=="application/json" else text
                cases.append(dict(recipe=recipe,status=response.status_code,response=body,exists=path.exists(),messages=75 if recipe["name"] in {"peek-50","peek-again"} else None))
    return dict(cases=cases)


def _team_tool_contracts() -> dict:
    """Actual installed team schemas, keyword boundary and JSON identities.

    Well-typed inputs follow the advertised source schema. Python handlers do
    not enforce schema types; Go's additional type rejections are native tests.
    No-manager tool calls inspect only the real keyword binding, not scheduling.
    """
    import asyncio
    from mini_loop.builtins import default_registry
    from mini_loop.registry import ToolCall, ToolContext, ToolRegistry
    from mini_loop.secrets import SecretRegistry
    from mini_loop.teams import install_teams

    registry = install_teams(ToolRegistry())
    default = default_registry()
    secret = SecretRegistry(mask_with="hidden", min_length=1)
    secret.register("TEAM_TEST_SECRET", "secret")
    variants = {
        "spawn_teammate": [dict(name="secret", role="worker", prompt="secret\n界")],
        "send_message": [dict(to="bob",content="hello"), dict(to="",content=""),
                         dict(to="bob",content="secret",type=""),
                         dict(to="bob",content="secret",metadata=None),
                         dict(to="bob",content="secret",metadata={}),
                         dict(to="secret",content='"secret"',type="secret",metadata={
                             "z": [1,1.0,True,None,{"界":"secret"}],
                             "a": {"z":"last","a":"first"},
                             "large": 9007199254740993, "negative_zero": -0.0,
                             "tiny": 1e-7, "secret":"old", "hidden":"new"}),
                         dict(to="bob",content="duplicate metadata",metadata={"a":3,"b":2})],
        "read_inbox": [{}],
        "broadcast": [dict(content="secret\n界"),dict(content="")],
        "list_teammates": [{}],
        "request_shutdown": [dict(target="secret"),dict(target="secret",reason=""),dict(target="secret",reason="secret")],
        "request_plan": [dict(teammate="secret",task="secret")],
        "submit_plan": [dict(plan="secret"),dict(plan="")],
        "review_plan": [dict(request_id="secret",approve=False),dict(request_id="secret",approve=True,feedback=""),dict(request_id="secret",approve=False,feedback="secret")],
        "list_protocols": [{}],
    }
    tools, cases = [], []
    ctx = ToolContext(agent=None,workspace=Path("."),state={})
    for name in registry.names():
        tool = registry.get(name)
        assert tool is not None
        tools.append(dict(schema=tool.schema, readonly=tool.readonly,risk=tool.risk,
                          parallel_safe=tool.parallel_safe,
                          execution_mode=tool.execution_mode(ToolCall(name,{},"team")),
                          absent_from_default=default.get(name) is None))
        inputs = variants[name]
        rejected = [dict(**inputs[0],owner="foreign"),dict(**inputs[0],team_id="foreign"),
                    dict(**inputs[0],session_id="foreign"),dict(**inputs[0],root="foreign")]
        for required in tool.input_schema.get("required",[]):
            rejected.append({key:value for key,value in inputs[0].items() if key!=required})
        for value in inputs + rejected:
            try:
                asyncio.run(tool.run(ctx,**value))
            except TypeError:
                accepted=False
            else:
                accepted=True
            cases.append(dict(name=name,input_json=json.dumps(value),accepted=accepted,
                              canonical=json.dumps(value,sort_keys=True,ensure_ascii=False,separators=(",",":")),
                              sorted=json.dumps(value,sort_keys=True,ensure_ascii=False),
                              masked_json=json.dumps(secret.mask_payload(value))))
    return dict(tools=tools,cases=cases)


def _team_protocol_contracts() -> dict:
    """Actual manager handshakes/delivery over real sessions and fixed mailboxes.

    Roster construction is an explicit trusted fixture seam, not spawn evidence.
    Only request UUIDs and wall timestamps are fixed; methods and bus are real.
    """
    from dataclasses import asdict
    from types import SimpleNamespace
    from unittest.mock import patch
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.teams import ProtocolState, _render_messages
    import mini_loop.manager as manager_module

    recipes = [
        dict(name="plan-review",operations=[dict(action="submit",content="Plan 界"),dict(action="peek"),dict(action="consume"),dict(action="review",team="other",request=1,approve=True),dict(action="review",request=1,approve=True),dict(action="consume",member="bob"),dict(action="review",request=1,approve=False)]),
        dict(name="shutdown",operations=[dict(action="shutdown"),dict(action="peek",member="bob"),dict(action="consume",member="bob"),dict(action="consume"),dict(action="consume",member="bob")]),
        dict(name="without-agent",operations=[dict(action="shutdown",member="no-agent"),dict(action="consume",member="no-agent"),dict(action="consume")]),
        dict(name="missing-targets",operations=[dict(action="shutdown",member="missing"),dict(action="request_plan",member="missing"),dict(action="submit",member="lead"),dict(action="review",request=1,approve=True)]),
        dict(name="instruction-refusal",operations=[dict(action="request_plan",content="界",repeat=16000),dict(action="peek",member="bob")]),
        dict(name="plan-truncation",operations=[dict(action="submit",content="界",repeat=16001),dict(action="consume")]),
        dict(name="shutdown-truncation",operations=[dict(action="shutdown",content="😀",repeat=16001),dict(action="consume",member="bob"),dict(action="consume")]),
        dict(name="review-truncation",operations=[dict(action="submit",content="plan"),dict(action="review",request=1,approve=False,content="界",repeat=16001),dict(action="consume",member="bob")]),
        dict(name="request-plan",operations=[dict(action="request_plan",content="build it"),dict(action="consume",member="bob")]),
        dict(name="shutdown-not-plan",operations=[dict(action="shutdown"),dict(action="review",request=1,approve=True)]),
        dict(name="correlation-only",operations=[dict(action="submit",content="plan"),dict(action="inject",message={"from":"other/spoof","type":"plan_approval_response","content":{"z":[True,None,"界"],"a":1},"metadata":{"approve":"yes"}},request=1),dict(action="consume"),dict(action="review",request=1,approve=True)]),
        dict(name="wrong-type",operations=[dict(action="submit",content="plan"),dict(action="inject",message={"type":"shutdown_response","content":"wrong","metadata":{"approve":True}},request=1),dict(action="consume")]),
        dict(name="false-approval",operations=[dict(action="submit",content="plan"),dict(action="inject",message={"type":"plan_approval_response","content":None,"metadata":{"approve":[]}},request=1),dict(action="consume")]),
        dict(name="nan-approval",operations=[dict(action="submit",content="plan"),dict(action="inject",message={"type":"plan_approval_response","content":float("inf"),"metadata":{"approve":float("nan")}},request=1),dict(action="consume")]),
        dict(name="legacy-render",operations=[dict(action="inject",message={"from":"t/name","content":{"text":"\ud800","n":float("nan")},"metadata":{"extra":True}}),dict(action="consume")]),
        dict(name="partial-fault",operations=[dict(action="shutdown"),dict(action="inject",member="bob",message={"metadata":None}),dict(action="consume",member="bob"),dict(action="consume")]),
        dict(name="unhashable-id",operations=[dict(action="inject",message={"metadata":{"request_id":[]}}),dict(action="consume"),dict(action="consume")]),
        dict(name="render-fault",operations=[dict(action="inject",message={"from":None,"metadata":{}}),dict(action="consume")]),
        dict(name="bad-recipient",operations=[dict(action="shutdown",member="."),dict(action="consume",member=".")]),
        dict(name="unicode-recipient",operations=[dict(action="shutdown",member="\U0001fae8")]),
        dict(name="publication-before-fault",operations=[dict(action="block",member="bob"),dict(action="shutdown"),dict(action="peek",member="bob")]),
        dict(name="resolved-before-fault",operations=[dict(action="submit",content="plan"),dict(action="block",member="bob"),dict(action="review",request=1,approve=True,content="feedback")]),
        dict(name="prune-resolved-first",memory=True,operations=[dict(action="batch",count=5),dict(action="batch",count=205,resolve=True)]),
        dict(name="prune-all-pending",memory=True,operations=[dict(action="batch",count=220)]),
    ]
    values = [None, False, True, 0, 1, -1, -0.0, 1e-7, "", "no", [], [False], {},
              {"quote'": "\n", "surrogate": "\ud800", "unicode15": "\U0001fae8"}, float("nan"), float("inf")]
    operations=[]
    for index,value in enumerate(values,1):
        operations.extend([dict(action="submit",content="value plan"),
                           dict(action="inject",request=index,message={"type":"plan_approval_response","content":value,"metadata":{"approve":value}}),
                           dict(action="consume")])
    recipes.append(dict(name="closed-value-responses",operations=operations))
    def encoded(value):
        return json.dumps(value,ensure_ascii=True,separators=(",",":"))
    def digest(value):
        return hashlib.sha256(value.encode("utf-8",errors="surrogatepass")).hexdigest()
    cases=[]
    for recipe in recipes:
        with tempfile.TemporaryDirectory(prefix="go-team-protocol-") as scratch:
            manager=SessionManager(Settings(fake_llm=True,workspace_root=Path(scratch)/"ws",trajectory_enabled=False),FakeAsyncAnthropic())
            children={name:manager.create(owner="alice") for name in ("bob","no-agent",".","\U0001fae8")}
            children["no-agent"].agent=None
            manager._teammates["team"]={name:child.id for name,child in children.items()}
            if recipe.get("memory"):
                from mini_loop.teams import MessageBus
                manager.bus=MessageBus()
            counter=0
            def uuid():
                nonlocal counter
                counter+=1
                return SimpleNamespace(hex=f"{counter:010x}"+"0"*22)
            def state(*args,**kwargs):
                return ProtocolState(*args,created_at=1000.0,**kwargs)
            steps=[]
            with patch("mini_loop.manager.uuid.uuid4",side_effect=uuid),patch.object(manager_module,"ProtocolState",side_effect=state),patch("mini_loop.teams.time.time",return_value=1000.0):
                for operation in recipe["operations"]:
                    op=dict(operation)
                    if "message" in op: op["message_json"]=encoded(op.pop("message"))
                    step=dict(operation=op)
                    member=operation.get("member","bob" if operation["action"] in {"submit","shutdown","request_plan","block"} else "lead")
                    team=operation.get("team","team")
                    content=operation.get("content","")*operation.get("repeat",1)
                    rid=f"req_{operation.get('request',0):010x}"
                    child=children.get(member)
                    if child and child.agent: child.agent.state.pop("shutdown_requested",None)
                    try:
                        action=operation["action"]
                        if action=="submit": output=manager.submit_plan(team,member,content)
                        elif action=="shutdown": output=manager.request_shutdown(team,member,content)
                        elif action=="request_plan": output=manager.request_plan(team,member,content)
                        elif action=="review": output=manager.review_plan(team,rid,operation["approve"],content)
                        elif action in {"peek","consume"}:
                            rows=manager.peek_team_inbox(team,member) if action=="peek" else manager.consume_team_inbox(team,member)
                            output=dict(messages=rows,shutdown_requested=bool(child and child.agent and child.agent.state.get("shutdown_requested",False)))
                            step["render_hash"]=digest(_render_messages(rows))
                        elif action=="inject":
                            message=json.loads(op["message_json"])
                            if operation.get("request"): message.setdefault("metadata",{})["request_id"]=rid
                            key=team+"/"+member
                            if recipe.get("memory"): manager.bus.inboxes.setdefault(key,[]).append(message)
                            else:
                                path=manager.bus._path(key);path.parent.mkdir(parents=True,exist_ok=True)
                                with path.open("a") as file:file.write(encoded(message)+"\n")
                            output="injected"
                        elif action=="block":
                            path=manager.bus._path(team+"/"+member);path.parent.mkdir(parents=True,exist_ok=True);path.mkdir();output="blocked"
                        elif action=="batch":
                            output=[]
                            for i in range(operation["count"]):
                                request=manager.submit_plan(team,"bob",f"plan-{i}")
                                output.append(request)
                                if operation.get("resolve"):output.append(manager.review_plan(team,request,True,""))
                        step["result_hash"]=digest(encoded(output))
                    except Exception as error:
                        step["error"]=type(error).__name__
                    step["shutdown_requested"]=bool(child and child.agent and child.agent.state.get("shutdown_requested",False))
                    step["states_hash"]=digest(json.dumps([asdict(s) for s in manager.protocols.values()],indent=2))
                    step["state_count"]=len(manager.protocols)
                    step["problems"]=[str(value).replace(str(manager.bus.root),"<root>") if manager.bus.root else str(value) for value in manager.bus.problems]
                    steps.append(step)
            cases.append(dict(name=recipe["name"],memory=recipe.get("memory",False),steps=steps))
    return dict(cases=cases)


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



def _team_effect_contracts() -> dict:
    """Real installed team handlers over real manager sessions and bus IO.

    The trusted roster fixture is explicit; spawning/lifecycle is not exercised.
    Request IDs and timestamps are normalized only in displayed output.
    """
    import asyncio
    import re
    from types import SimpleNamespace
    from unittest.mock import patch
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager
    from mini_loop.registry import ToolContext, ToolRegistry
    from mini_loop.teams import install_teams

    def op(name, actor="lead", **value):
        return dict(name=name, actor=actor, input=value)

    tools = [op("send_message",to="lead",content="hi"),op("read_inbox"),
             op("broadcast",content="hi"),op("list_teammates"),
             op("request_shutdown",target="bob"),op("request_plan",teammate="bob",task="work"),
             op("submit_plan",plan="plan"),op("review_plan",request_id="missing",approve=True),
             op("list_protocols")]
    cases = [
        dict(name="unconfigured",unconfigured=True,operations=tools),
        dict(name="empty",members=[],operations=[op("list_teammates"),op("read_inbox"),op("list_protocols"),op("broadcast",content="hello"),op("request_shutdown",target="bob"),op("request_plan",teammate="bob",task="work"),op("submit_plan",plan="plan"),op("review_plan",request_id="missing",approve=True)]),
        dict(name="routing",operations=[op("list_teammates"),op("send_message",to="bob",content="hi 界",type="",metadata={"z":[1,1.0,None],"a":{"first":True}}),op("read_inbox",actor="bob"),op("read_inbox",actor="bob"),op("send_message",actor="bob",to="lead",content="answer"),op("read_inbox")]),
        dict(name="recipients",operations=[op("send_message",to="foreign/lead",content="no"),op("send_message",to="quote'\n",content="no"),op("send_message",to="\U0001fae8",content="no"),op("read_inbox")]),
        dict(name="broadcast",operations=[op("broadcast",content="hello"),op("read_inbox",actor="bob"),op("read_inbox",actor="charlie"),op("read_inbox"),op("broadcast",actor="bob",content="peer broadcast"),op("read_inbox",actor="bob"),op("read_inbox",actor="charlie"),op("read_inbox")]),
        dict(name="broadcast-refusal",members=["bob","charlie","d","e","f"],operations=[dict(op("broadcast",content="界"),repeat=16001),op("read_inbox",actor="bob"),dict(op("send_message",to="bob",content="界"),repeat=16001)]),
        dict(name="lead-only",operations=[op("request_shutdown",actor="bob",target="charlie"),op("request_plan",actor="bob",teammate="charlie",task="work"),op("review_plan",actor="bob",request_id="missing",approve=True),op("list_protocols",actor="bob")]),
        dict(name="plan",operations=[op("request_plan",teammate="bob",task="work"),op("read_inbox",actor="bob"),op("submit_plan",actor="bob",plan="Plan 界"),op("list_protocols"),op("read_inbox"),op("review_plan",request_id="<request1>",approve=False,feedback="revise"),op("read_inbox",actor="bob"),op("list_protocols",actor="bob"),op("review_plan",request_id="<request1>",approve=True)]),
        dict(name="shutdown",operations=[op("request_shutdown",target="bob"),op("read_inbox",actor="bob"),op("read_inbox"),op("list_protocols"),op("read_inbox",actor="bob")]),
        dict(name="shutdown-partial-fault",operations=[op("request_shutdown",target="bob"),dict(action="inject",actor="bob",row={"metadata":None}),op("read_inbox",actor="bob"),op("read_inbox"),op("list_protocols")]),
        dict(name="metadata-defaults",operations=[op("send_message",to="bob",content="absent"),op("send_message",to="bob",content="null",metadata=None),op("send_message",to="bob",content="empty",metadata={}),op("read_inbox",actor="bob")]),
    ]
    registry=install_teams(ToolRegistry())
    results=[]
    for recipe in cases:
        with tempfile.TemporaryDirectory(prefix="go-team-effects-") as scratch:
            manager=SessionManager(Settings(fake_llm=True,workspace_root=Path(scratch)/"ws",trajectory_enabled=False),FakeAsyncAnthropic())
            lead=manager.create(owner="alice")
            members=recipe.get("members",["bob","charlie"])
            sessions={"lead":lead,**{name:manager.create(owner="alice") for name in members}}
            manager._teammates["team"]={name:sessions[name].id for name in members}
            for name,session in sessions.items():
                session.agent.state.update(team_id="team",agent_name=name)
            counter=0
            def uuid():
                nonlocal counter
                counter+=1
                return SimpleNamespace(hex=f"{counter:010x}"+"0"*22)
            aliases={}
            def normalize(text):
                text=re.sub(r'("created_at": )[^,\n]+',r'\g<1>0.0',text)
                for rid,alias in aliases.items(): text=text.replace(rid,alias)
                return text
            rows=[]
            with patch("mini_loop.manager.uuid.uuid4",side_effect=uuid):
                for step in recipe["operations"]:
                    name=step.get("actor","lead")
                    if step.get("action")=="inject":
                        path=manager.bus._path("team/"+name)
                        with path.open("a") as file: file.write(json.dumps(step["row"])+"\n")
                        rows.append(dict(action="inject",actor=name,row_json=json.dumps(step["row"])))
                        continue
                    value=dict(step["input"])
                    if step.get("repeat"): value["content"]*=step["repeat"]
                    for key,text in list(value.items()):
                        if isinstance(text,str):
                            for rid,alias in aliases.items(): text=text.replace(alias,rid)
                            value[key]=text
                    state={} if recipe.get("unconfigured") else sessions[name].agent.state
                    ctx=ToolContext(agent=None,workspace=Path(scratch),state=state)
                    row=dict(name=step["name"],actor=name,input_json=json.dumps(step["input"]),repeat=step.get("repeat",1))
                    try:
                        output=asyncio.run(registry.get(step["name"]).run(ctx,**value))
                        for rid in manager.protocols:
                            if rid not in aliases: aliases[rid]=f"<request{len(aliases)+1}>"
                        row["output"]=normalize(output)
                    except Exception as error:
                        row["error"]=type(error).__name__
                    row["shutdown_requested"]=bool(sessions[name].agent.state.get("shutdown_requested",False))
                    row["state_count"]=len(manager.protocols)
                    rows.append(row)
            results.append(dict(name=recipe["name"],unconfigured=recipe.get("unconfigured",False),members=members,steps=rows))
    return dict(cases=results)


def _team_lifecycle_contracts() -> dict:
    """Actual spawn, injector and idle turns, with real task/mailbox effects.

    The initially created idle task is cancelled before each explicit probe. Idle
    probes use the real source loop and short real sleeps; no clock/run stub replaces
    scheduling or model execution. Worktree path selection uses a real manager with
    prepared directories, not newly created Git branches. IDs/workspaces are normalized.
    """
    import asyncio
    from dataclasses import asdict
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text
    from mini_loop.manager import SessionManager
    from mini_loop.tasks import Task
    from mini_loop.teams import team_injector
    from mini_loop.worktrees import WorktreeManager

    injectors = [
        dict(name="empty", rows=[]),
        dict(name="legacy", rows=[{"from":"team/peer","to":"team/bob","content":"quote \" 界", "type":"", "metadata":{"z":[1,1.0,None],"a":True},"ts":1.5,"unknown":"keep"}, {"content":"missing fields"}, {"from":"x/name","type":"message","metadata":{},"content":[float("nan"),"\ud800"]}]),
        dict(name="unconfigured", rows=[{"from":"peer","content":"retained"}], unconfigured=True),
        dict(name="teamless", rows=[{"from":"peer","content":"retained"}], teamless=True),
        dict(name="bad-sender", rows=[{"from":7,"content":"bad"}]),
        dict(name="shutdown", rows=[{"from":"team/lead","type":"shutdown_request","metadata":{"request_id":"request"},"content":"stop"}]),
        dict(name="partial-shutdown", rows=[{"from":"team/lead","type":"shutdown_request","metadata":{"request_id":"request"}}, {"type":"shutdown_request","metadata":[]}]),
        dict(name="overflow", rows=[{"from":"team/peer","content":str(i)} for i in range(101)]),
    ]
    idle = [
        dict(name="timeout", rows=[], tasks=[]),
        dict(name="inbox", rows=[{"from":"team/peer","to":"team/bob","content":"hello 界","type":"message","metadata":{"first":1,"next":[True,None]},"ts":1.5,"unknown":"kept"}], tasks=[]),
        dict(name="shutdown-priority", rows=[{"from":"team/lead","type":"shutdown_request","metadata":{"request_id":"request"}}], tasks=[dict(id="task_a",subject="should remain pending")]),
        dict(name="sticky-shutdown", rows=[{"from":"team/peer","content":"discarded"}], tasks=[], shutdown_before=True),
        dict(name="task", rows=[], tasks=[dict(id="task_a",subject="work 界",description="details")]),
        dict(name="inbox-before-task", rows=[{"from":"team/peer","content":"first"}], tasks=[dict(id="task_a",subject="second")]),
        dict(name="blocked", rows=[], tasks=[dict(id="task_a",subject="blocked",blockedBy=["missing"])]),
        dict(name="worktree", rows=[], tasks=[dict(id="task_a",subject="branch",worktree="checkout")], worktree_exists=True),
        dict(name="missing-worktree", rows=[], tasks=[dict(id="task_a",subject="fallback",worktree="checkout")]),
        dict(name="invalid-worktree", rows=[], tasks=[dict(id="task_a",subject="fallback",worktree="../escape")]),
        dict(name="poll-crosses-deadline", rows=[{"from":"team/peer","content":"delivered after sleep"}],tasks=[],poll_ms=20,timeout_ms=1),
    ]

    async def scenario(recipe, is_idle):
        with tempfile.TemporaryDirectory(prefix="go-team-lifecycle-") as scratch:
            settings=Settings(fake_llm=True, workspace_root=Path(scratch)/"ws", trajectory_enabled=False, team_idle_poll=recipe.get("poll_ms",2)/1000, team_idle_timeout=recipe.get("timeout_ms",100)/1000)
            manager=SessionManager(settings,FakeAsyncAnthropic(lambda _: ([text("done")],"end_turn"),thinking=False),injectors=[])
            lead=manager.create(owner="alice")
            result=await manager.spawn_teammate(lead.id,"bob","research","hello")
            child=manager._sessions[manager._teammates[lead.id]["bob"]]
            await child.spawn_task
            child.lifecycle_task.cancel()
            await asyncio.gather(child.lifecycle_task,return_exceptions=True)
            manager.bus.read(lead.id+"/lead")
            initial=dict(shared_workspace=child.workspace==lead.workspace, shared_skills=child.agent.skills is lead.agent.skills, shared_memory=child.agent.state["memory"] is lead.agent.state["memory"], owner=child.owner, mode=child.permission_mode, role=child.agent.state["role"], recursive_spawn=child.agent.tools.get("spawn_teammate") is not None, result=result.replace(child.id,"<session>"))
            rows=recipe["rows"]
            path=manager.bus._path(lead.id+"/bob")
            path.parent.mkdir(parents=True,exist_ok=True)
            path.write_text("".join(json.dumps(row)+"\n" for row in rows))
            def normalize(value):
                return value.replace(str(child.workspace.resolve()),"<workspace>").replace(str(child.workspace),"<workspace>").replace(str((Path(scratch)/"repo"/".worktrees"/"checkout").resolve()),"<worktree>").replace(str(Path(scratch)/"repo"/".worktrees"/"checkout"),"<worktree>").replace(lead.id,"team")
            observation=dict(name=recipe["name"],rows_json=[json.dumps(row) for row in rows],initial=initial)
            if not is_idle:
                if recipe.get("unconfigured"): child.agent.state["manager"]=None
                if recipe.get("teamless"): child.agent.state["team_id"]=None
                observation.update(unconfigured=recipe.get("unconfigured",False),teamless=recipe.get("teamless",False),output=[],error="")
                start=child._seq
                try:
                    observation["output"]=[normalize(message["content"]) for message in await team_injector(child.agent)]
                except Exception as error:
                    observation["error"]=type(error).__name__
                observation["counts"]=[event["count"] for event in child._backlog if event["seq"]>start and event["type"]=="team_inbox"]
                observation["shutdown"]=bool(child.agent.state.get("shutdown_requested",False))
                observation["remaining"]=len(manager.bus.peek(lead.id+"/bob"))
                observation["raw"]=json.dumps(rows,default=str)
            else:
                board=child.agent.state["tasks"]
                for row in recipe["tasks"]: board.save(Task(**row))
                manager.worktrees=WorktreeManager(Path(scratch)/"repo")
                if recipe.get("worktree_exists"): manager.worktrees.path_for("checkout").mkdir(parents=True)
                child.agent.state["shutdown_requested"]=recipe.get("shutdown_before",False)
                prompts,contexts,workspaces=[],[],[]
                original=child.run
                async def observe_run(prompt, **kwargs):
                    prompts.append(normalize(prompt))
                    context=kwargs["run_context"].as_dict()
                    context["message_id"]="<message>"
                    contexts.append(context)
                    workspaces.append(normalize(str(child.agent.workspace)))
                    return await original(prompt,**kwargs)
                child.run=observe_run
                await manager._teammate_idle_loop(child)
                observation.update(poll_ms=recipe.get("poll_ms",2),timeout_ms=recipe.get("timeout_ms",100),tasks=recipe["tasks"],worktree_exists=recipe.get("worktree_exists",False),shutdown_before=recipe.get("shutdown_before",False),prompts=prompts,contexts=contexts,workspaces=workspaces,final_tasks=[asdict(task) for task in board.list()],shutdown=bool(child.agent.state.get("shutdown_requested",False)))
            observation["outgoing"]=[dict(content=row["content"],type=row["type"],metadata=row["metadata"]) for row in manager.bus.read(lead.id+"/lead")]
            await manager.stop()
            return observation

    async def capture():
        return dict(injectors=[await scenario(case,False) for case in injectors],idle=[await scenario(case,True) for case in idle],restart=await _team_restart_contract())
    return asyncio.run(capture())


async def _team_restart_contract() -> dict:
    """Real source SQLite close/reopen after an owned initial teammate turn."""
    import asyncio
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text
    from mini_loop.manager import SessionManager
    from mini_loop.storage import SQLiteStateStore

    with tempfile.TemporaryDirectory(prefix="go-team-restart-") as scratch:
        root = Path(scratch)
        settings = Settings(workspace_root=root / "ws", trajectory_enabled=False,
                            team_idle_poll=3600, team_idle_timeout=7200)
        store = SQLiteStateStore(root / "state.db")
        client = FakeAsyncAnthropic(lambda _: ([text("done")], "end_turn"), thinking=False)
        manager = SessionManager(settings, client, state_store=store, injectors=[])
        try:
            lead = manager.create(owner="alice")
            await manager.spawn_teammate(lead.id, "bob", "research", "hello")
            child = manager.teammate_session(lead.id, "bob")
            await child.spawn_task
            manager.bus.send(lead.id + "/lead", lead.id + "/bob", "pending old-team mail")
            child_id, parent_id, workspace = child.id, lead.id, child.workspace
        finally:
            await manager.stop()
            store.close()
        reopened = SQLiteStateStore(root / "state.db")
        fresh = SessionManager(settings, client, state_store=reopened, injectors=[])
        try:
            restored = fresh.restore_sessions()
            child = fresh.get(child_id)
            state = child.agent.state
            return dict(
                restored_count=len(restored), owner=child.owner,
                same_workspace=child.workspace == workspace,
                own_team=state["team_id"] == child_id, name=state["agent_name"],
                label=child.agent.label, mode=child.permission_mode,
                role_present="role" in state, tasks_present="tasks" in state,
                recursive_spawn=child.agent.tools.get("spawn_teammate") is not None,
                runner_present=hasattr(child, "lifecycle_task"),
                old_roster_present=fresh.teammate_session(parent_id, "bob") is not None,
                old_inbox_count=len(fresh.bus.peek(parent_id + "/bob")),
                new_inbox_count=len(fresh.peek_team_inbox(child_id, "lead")),
                status=child.status, run_count=child.run_count,
                history=child.agent.messages,
            )
        finally:
            await fresh.stop()
            reopened.close()


def _workflow_model_contracts() -> dict:
    """Actual source model normalization, identity and all finite status predicates."""
    import dataclasses
    from mini_loop.workflows.models import (
        WorkflowDefinition, NodeKind, RunStatus, NodeStatus, AttemptStatus,
        VerificationStatus, DefinitionSource, Artifact, canonical_json, content_hash,
    )
    definitions = [
        dict(name="minimal", return_from="finish"),
        dict(name="研究", return_from="a", nodes=[dict(id="a", kind="agent")]),
        dict(name="typed", return_from="b", source="project", source_version="v2",
             input_schema={"type":"object","properties":{"x":{"type":"number"}}},
             budget={"wall_time_seconds":900,"max_rounds":0,"token_budget":0},
             policy={"allowed_tools":[]},
             nodes=[dict(id="a",kind="map",items_from="input.items",needs=[],max_rounds=2),
                    dict(id="b",kind="return",needs=["a"],output_schema={"type":"array"})]),
        dict(name="identity",return_from="a",nodes=[dict(id="a",kind="verify")],
             definition_id="explicit",revision="pinned",parent_revision="parent",
             definition_hash={"forged":True}),
        dict(name="float",return_from="a",budget={"wall_time_seconds":0.25},
             nodes=[dict(id="a",kind="barrier",output_schema={})]),
    ]
    cases=[]
    for recipe in definitions:
        model=WorkflowDefinition.from_dict(recipe)
        cases.append(dict(input=recipe, semantic=model.semantic_dict(), output=model.to_dict(),
                          canonical=canonical_json(model.semantic_dict())))
    values=[None,False,123,1.0,-0.0,1e-7,"中文<&\n",{"z":1,"a":[2.0,"é"]}]
    hashes=[dict(input_json=json.dumps(value),canonical=canonical_json(value),hash=content_hash(value)) for value in values]
    errors=[]
    for raw in ["NaN","Infinity","-Infinity",'"\\ud800"']:
        value=json.loads(raw)
        try: content_hash(value)
        except Exception as error: errors.append(dict(input_json=raw,error=type(error).__name__))
    artifacts=[]
    for valid in [True,False]:
        artifact=Artifact.create(run_id="run",node_id="node",attempt_id="attempt",
                                 value={"answer":[1.0,"中文"]},schema={"type":"object"},
                                 verification_status=VerificationStatus.UNVERIFIED,schema_valid=valid)
        row=dataclasses.asdict(artifact)
        row["artifact_id"]="<artifact>";row["created_at"]=0
        artifacts.append(row)
    return dict(definitions=cases,hashes=hashes,errors=errors,artifacts=artifacts,
                kinds=[state.value for state in NodeKind],
                sources=[state.value for state in DefinitionSource],
                verification=[state.value for state in VerificationStatus],
                runs=[dict(value=state.value,terminal=state.is_terminal) for state in RunStatus],
                nodes=[dict(value=state.value,terminal=state.is_terminal,satisfies=state.satisfies_dependency) for state in NodeStatus],
                attempts=[dict(value=state.value,terminal=state.is_terminal) for state in AttemptStatus])


def _workflow_validation_contracts() -> dict:
    """Actual definition/schema/value validators, including refusal order."""
    import copy
    import dataclasses
    from mini_loop.workflows.models import WorkflowDefinition, WorkflowNode, NodeAttempt
    from mini_loop.workflows.validation import validate_definition, validate_schema_definition, validate_json_value
    from mini_loop.workflows.artifacts import ArtifactSubmission, artifact_from_submission, verification_status_from_value

    def result(call):
        try: call(); return dict(error="",detail="")
        except Exception as error: return dict(error=type(error).__name__,detail=str(error))
    base=dict(name="wf",return_from="a",nodes=[dict(id="a",kind="agent")])
    recipes=[("valid",{}),("version",dict(schema_version=2)),("name",dict(name="中文")),
             ("empty",dict(nodes=[])),("concurrency-low",dict(budget={"max_concurrent_agents":0})),
             ("concurrency-high",dict(budget={"max_concurrent_agents":5})),
             ("agents-low",dict(budget={"max_agents":0})),("agents-high",dict(budget={"max_agents":33})),
             ("rounds",dict(budget={"max_rounds":0})),("wall",dict(budget={"wall_time_seconds":0})),
             ("tokens",dict(budget={"token_budget":0})),("no-tools",dict(policy={"allowed_tools":[]})),
             ("duplicate-tools",dict(policy={"allowed_tools":["glob","glob"]})),
             ("mutating-tools",dict(policy={"allowed_tools":["bash","write_file"]})),
             ("tool-subset",dict(policy={"allowed_tools":["glob"]})),
             ("profile",dict(policy={"agent_profile":"writer"})),
             ("input-schema",dict(input_schema={"$ref":"other"})),
             ("output-schema",dict(output_schema={"type":"bogus"})),
             ("property-order",dict(input_schema={"properties":{"z":{"type":"bogus"},"a":{"type":"bogus"}}})),
             ("return-missing",dict(return_from="absent")),
             ("return-schema",dict(output_schema={"type":"array"})),
             ("order",dict(schema_version=0,name="!",nodes=[])),
             ("schema-equality",dict(output_schema={"const":1},nodes=[dict(id="a",kind="agent",output_schema={"const":True})])),
             ("dag",dict(return_from="c",nodes=[dict(id="a",kind="agent"),dict(id="b",kind="verify",needs=["a"]),dict(id="c",kind="reduce",needs=["a","b"])]))]
    for name,patch in [("invalid-id",dict(id="0bad")),("unsupported-kind",dict(kind="map")),
                       ("node-rounds",dict(max_rounds=0)),("node-budget",dict(max_rounds=5)),
                       ("items",dict(items_from="a.rows")),("node-schema",dict(output_schema={"minItems":1})),
                       ("duplicate-needs",dict(needs=["x","x"])),("unknown-needs",dict(needs=["x"])),
                       ("self-needs",dict(needs=["a"]))]:
        node=copy.deepcopy(base["nodes"][0]);node.update(patch);recipes.append((name,dict(nodes=[node])))
    recipes.extend([("duplicate-id",dict(nodes=[dict(id="a",kind="agent"),dict(id="a",kind="reduce")])),
                    ("too-many",dict(budget={"max_agents":1},nodes=[dict(id="a",kind="agent"),dict(id="b",kind="agent")])),
                    ("cycle",dict(nodes=[dict(id="a",kind="agent",needs=["b"]),dict(id="b",kind="agent",needs=["a"])]))])
    definitions=[]
    for name,patch in recipes:
        recipe=copy.deepcopy(base);recipe.update(patch)
        definitions.append(dict(name=name,input_json=json.dumps(recipe),**result(lambda:validate_definition(WorkflowDefinition.from_dict(recipe)))))
    schemas=[{}, {"type":["string","null"]},{"type":[]},{"type":"bad"},{"type":[{}]},
             {"required":None},{"required":[1]},{"properties":[]},{"properties":{"x":False}},
             {"items":None},{"enum":{}},{"additionalProperties":{}},{"title":0},{"description":False},
             {"z":0,"$ref":"x"},{"type":None},{"enum":[]},{"items":{"type":"integer"}}]
    schema_cases=[dict(schema_json=json.dumps(schema),**result(lambda:validate_schema_definition(schema))) for schema in schemas]
    values=[({},None),({"enum":[True]},1),({"const":1},True),({"type":"integer"},1.0),
            ({"type":"number"},False),({"type":["string","null"]},3),({},float("nan")),
            ({"type":"object","required":["x","x"]},{}),
            ({"additionalProperties":False,"properties":{"a":{}}},{"z":1,"b":2}),
            ({"properties":{"x":{"items":{"type":"integer"}}}},{"x":[1,False]}),
            ({"enum":[{"a":1.0}]},{"a":True}),({"const":9007199254740993},9007199254740992.0),
            ({"enum":[1,2]},3),({}, {"ignored":float("nan")}),
            ({"required":["x"]},"nonobject"),({"items":{"type":"boolean"}},[True,False]),
            ({"type":"number"},float("inf")),({"properties":{"x":{"type":"number"}}},{"x":float("nan")}),
            (json.loads('{"enum":[NaN]}'),json.loads('NaN')),
            (json.loads('{"const":NaN}'),json.loads('NaN')),
            (json.loads('{"const":{"x":NaN}}'),json.loads('{"x":NaN}'))]
    value_cases=[dict(schema_json=json.dumps(schema),value_json=json.dumps(value),**result(lambda:validate_json_value(schema,value))) for schema,value in values]
    submissions=[]
    attempt=NodeAttempt(attempt_id="attempt",run_id="run",node_id="bound",attempt=1,agent_id="worker",spawn_index=0)
    node=WorkflowNode(id="schema-source",kind="agent",output_schema={"type":"object","required":["ok"]})
    for kind,value in [("structured",{"ok":True}),("wrong-tool",{"ok":True}),("unstructured",{"ok":True}),("invalid-value",{})]:
        submission=ArtifactSubmission(value,tool_name="other" if kind=="wrong-tool" else "return_artifact") if kind!="unstructured" else value
        captured=[]
        def execute():
            artifact=artifact_from_submission(submission,attempt=attempt,node=node)
            row=dataclasses.asdict(artifact);row["artifact_id"]="<artifact>";row["created_at"]=0;captured.append(row)
        outcome=result(execute)
        submissions.append(dict(kind=kind,value_json=json.dumps(value),schema_json=json.dumps(node.output_schema),output=captured,**outcome))
    verification=[None,{}, {"status":"verified"},{"status":"refuted"},{"status":"not_applicable"},{"status":"unverified"},{"status":[]},{"status":False},{"status":"unknown"}]
    return dict(definitions=definitions,schemas=schema_cases,values=value_cases,submissions=submissions,
                verification=[dict(value_json=json.dumps(value),status=verification_status_from_value(value).value) for value in verification])


def _workflow_record_contracts() -> dict:
    """Actual source constructors and normalized record projections; no store effects."""
    import copy
    import dataclasses
    from mini_loop.run_context import RunContext
    from mini_loop.workflows.models import (
        WorkflowRun, NodeState, AttemptClaim, NodeAttempt, OutboxMessage,
        RunStatus, NodeStatus, AttemptStatus, VerificationStatus,
    )
    context = dataclasses.asdict(RunContext(message_id="msg_fixed", actor_id="owner",
        origin="explicit_human", channel="local", authority="explicit_human",
        stamped_by="trusted_local", approved_capabilities=("workflow.manage", "workflow.launch")))
    bases = {
        "run": dict(run_id="run", definition_revision="revision", session_id="session",
                    run_context=context, idempotency_key="key", args={"x":[1.0,"中文"]}),
        "node": dict(run_id="run", node_id="node"),
        "claim": dict(node_id="node", agent_id="worker", spawn_index=0),
        "attempt": dict(attempt_id="attempt", run_id="run", node_id="node", attempt=1,
                        agent_id="worker", spawn_index=0),
        "outbox": dict(message_id="outbox", run_id="run", session_id="session", kind="custom_notice",
                       payload={"ok":True,"nested":[1.0,"中文"]}),
    }
    constructors = {"run":WorkflowRun, "node":NodeState, "claim":AttemptClaim,
                    "attempt":NodeAttempt, "outbox":OutboxMessage}
    cases = []
    errors = []
    def execute(kind, recipe):
        args = copy.deepcopy(recipe)
        if kind == "run": args["run_context"] = RunContext(**args["run_context"])
        return constructors[kind](**args)
    def capture(kind, label, patch):
        recipe = copy.deepcopy(bases[kind]); recipe.update(patch)
        model = execute(kind, recipe)
        output = dataclasses.asdict(model)
        if "created_at" in output and "created_at" not in recipe: output["created_at"] = 0
        cases.append(dict(kind=kind, name=label, input=recipe, output=output,
                          terminal=model.is_terminal if kind == "run" else None))
    for kind in bases:
        capture(kind, "default", {})
        empty = {key:"" for key,value in bases[kind].items() if isinstance(value,str)}
        if kind == "run": empty.update(args={},created_at=0)
        if kind == "outbox": empty.update(payload={},created_at=0)
        if kind in ("claim","attempt"): empty["spawn_index"] = -2
        capture(kind, "empty-identities", empty)
    capture("run", "full", dict(status="COMPLETED",version=3,parent_run_id="parent",
        launch_action_id="launch",created_at=42.25,started_at=43.25,ended_at=44.25,
        active_node_ids=["b","a"],event_cursor=7,attempts_used=2,policy_snapshot_hash="policy",
        workspace_baseline="base",final_artifact_id="artifact",error="detail",cancel_reason="reason"))
    capture("node", "full", dict(status="UNVERIFIED",version=-1,attempt_ids=["a","b"],
                                  result_artifact_ids=["x","y"],error="detail"))
    capture("claim", "full", dict(parent_agent_id="parent",spawn_index=3))
    capture("attempt", "full", dict(status="UNKNOWN",version=5,parent_agent_id="parent",
        started_at=42.25,heartbeat_at=43.25,ended_at=44.25,result_artifact_id="artifact",
        verification_status="refuted",error="detail"))
    capture("outbox", "full", dict(created_at=42.25,claim_token="token",claimed_at=43.25,delivered_at=44.25))
    capture("run", "inert-provenance", dict(run_context={**context,"authority":"historical-unknown"}))
    for kind, enum in [("run",RunStatus),("node",NodeStatus),("attempt",AttemptStatus)]:
        for status in enum: capture(kind, "status-"+status.value, dict(status=status.value))
        for status in [None, "bad"]:
            recipe=copy.deepcopy(bases[kind]);recipe["status"]=status
            try: execute(kind, recipe)
            except Exception as error: errors.append(dict(kind=kind,input=recipe,error=type(error).__name__))
    for status in VerificationStatus: capture("attempt", "verification-"+status.value, dict(verification_status=status.value))
    for status in [None,"bad"]:
        recipe=copy.deepcopy(bases["attempt"]);recipe["verification_status"]=status
        try: execute("attempt",recipe)
        except Exception as error: errors.append(dict(kind="attempt",input=recipe,error=type(error).__name__))
    return dict(cases=cases,errors=errors)


def _workflow_store_contracts() -> dict:
    """Actual process-local registration/admission/CAS/claims; matrix seeds are explicit."""
    import copy
    import dataclasses
    from mini_loop.run_context import RunContext
    from mini_loop.workflows.models import WorkflowDefinition, AttemptClaim, RunStatus
    from mini_loop.workflows.store import InMemoryWorkflowStore
    context=dataclasses.asdict(RunContext(message_id="msg_fixed",actor_id="owner"))
    definition=dict(name="wf",revision="base",definition_id="stable",return_from="a",
        input_schema={"type":"object","required":["must"]},
        nodes=[dict(id="b",kind="agent",needs=["a"]),dict(id="a",kind="agent")],
        budget={"max_agents":3,"max_concurrent_agents":1})
    store=InMemoryWorkflowStore(); refs={}; ids={}; attempt_ids={}; rows=[]
    def normalized(value):
        if dataclasses.is_dataclass(value):
            value=dataclasses.asdict(value)
            for key in ("created_at","started_at","ended_at"):
                if key in value and value[key] is not None: value[key]=0
        if isinstance(value,dict): return {key:normalized(item) for key,item in value.items()}
        if isinstance(value,(list,tuple)): return [normalized(item) for item in value]
        if isinstance(value,str): return ids.get(value,attempt_ids.get(value,value))
        return value
    def ref(label): return refs.get(label,label)
    def step(op,**params):
        command=dict(op=op,**params); output=None; error="";detail="";launch_hash=""
        try:
            if op=="register": output=store.register_definition(WorkflowDefinition.from_dict(params["definition"])).to_dict()
            elif op=="definition": output=store.get_definition(params["revision"]).to_dict()
            elif op=="create":
                output=store.create_run(definition_revision=params.get("revision","base"),
                    session_id=params.get("session","session"),idempotency_key=params.get("key","key"),
                    args=json.loads(params.get("args_json",'{"choice":[true,1.0],"中文":"值"}')),
                    run_context=RunContext(**params.get("context",context)),
                    parent_run_id=params.get("parent"),launch_action_id=params.get("action"),
                    policy_snapshot_hash=params.get("policy",""))
                if output.run_id not in ids: ids[output.run_id]="<run-"+str(len(ids)+1)+">"
                if "save" in params: refs[params["save"]]=output.run_id
                launch_hash=store._launches[(output.session_id,output.idempotency_key)][0]
            elif op=="run": output=store.get_run(ref(params["run"]))
            elif op=="runs": output=store.list_runs(session_id=params.get("session"))
            elif op=="nodes": output=store.list_nodes(ref(params["run"]))
            elif op=="node": output=store.get_node(ref(params["run"]),params["node"])
            elif op=="transition": output=store.transition_run(ref(params["run"]),expected_version=params["expected"],to_status=params["status"],error=params.get("detail"))
            elif op=="claim":
                output=store.claim_nodes(ref(params["run"]),[AttemptClaim(**c) for c in params["claims"]],expected_version=params["expected"])
                for a in output: attempt_ids[a.attempt_id]="<attempt-"+str(len(attempt_ids)+1)+">"
            elif op=="attempts": output=store.list_attempts(ref(params["run"]))
            elif op=="attempt": output=store.get_attempt(next((a for a,label in attempt_ids.items() if label==params["attempt"]),params["attempt"]))
        except Exception as e: error=type(e).__name__;detail=str(e)
        for raw,label in {**ids,**attempt_ids}.items(): detail=detail.replace(raw,label)
        rows.append(dict(command_json=json.dumps(command),output_json=json.dumps(normalized(output)),error=error,detail=detail,launch_hash=launch_hash))
    step("register",definition=definition)
    step("register",definition={**definition,"revision":"alias","definition_id":"other","parent_revision":"parent"})
    step("definition",revision="alias");step("definition",revision="base")
    step("register",definition={**definition,"name":"different"})
    step("register",definition={**definition,"nodes":[dict(id="a",kind="map")]})
    step("create",save="r1");step("create",save="r1")
    for patch in [dict(args_json='{"changed":1}'),dict(context={**context,"actor_id":"other"}),dict(parent="parent"),dict(action="action"),dict(policy="policy")]: step("create",**patch)
    step("create",save="r2",session="other")
    step("create",save="r3",session="third",context={key:value for key,value in context.items() if key!="approved_capabilities"})
    step("create",session="",revision="missing");step("create",key="",revision="missing");step("create",revision="missing")
    step("runs");step("runs",session="other");step("runs",session="")
    step("run",run="missing");step("nodes",run="missing");step("node",run="missing",node="a")
    step("claim",run="r1",claims=[],expected=0)
    step("transition",run="missing",expected=0,status="bad")
    step("transition",run="r1",expected=99,status="RUNNING")
    step("transition",run="r1",expected=0,status="RUNNING",detail="first")
    step("transition",run="r1",expected=0,status="RUNNING")
    step("transition",run="r1",expected=1,status="RUNNING",detail="ignored")
    c=lambda node,index=0:dict(node_id=node,agent_id="worker",spawn_index=index,parent_agent_id="parent")
    step("claim",run="r1",claims=[c("a"),c("missing")],expected=1)
    step("nodes",run="r1");step("run",run="r1");step("attempts",run="r1")
    step("claim",run="r1",claims=[c("a"),c("a")],expected=1)
    step("claim",run="r1",claims=[],expected=1)
    step("claim",run="r1",claims=[c("a",2),c("b",1)],expected=2)
    step("nodes",run="r1");step("run",run="r1");step("attempts",run="r1")
    step("attempt",attempt="<attempt-1>");step("attempt",attempt="missing");step("attempts",run="missing")
    step("claim",run="r1",claims=[c("a")],expected=3)
    step("claim",run="r1",claims=[c("missing")],expected=3)
    step("claim",run="r1",claims=[c("missing"),c("missing")],expected=3)
    step("claim",run="r1",claims=[],expected=3)
    step("transition",run="r1",expected=4,status="COMPLETED")
    step("transition",run="r1",expected=5,status="RUNNING")
    step("run",run="r1");step("runs")
    step("create",save="r1");step("nodes",run="r1")
    matrix=[]
    for before in RunStatus:
        for target in RunStatus:
            probe=InMemoryWorkflowStore();probe.register_definition(WorkflowDefinition.from_dict(definition))
            run=probe.create_run(definition_revision="base",session_id="s",idempotency_key="k",args={},run_context=RunContext(message_id="msg"))
            # Explicit trusted state fixture, not a public initial-state transition.
            seeded=probe._runs[run.run_id];seeded.status=before;seeded.version=7;seeded.error="old"
            error="";detail=""
            try: probe.transition_run(run.run_id,expected_version=7,to_status=target,error="new")
            except Exception as e: error=type(e).__name__;detail=str(e)
            current=probe.get_run(run.run_id)
            matrix.append(dict(before=before.value,target=target.value,error=error,detail=detail,
                status=current.status.value,version=current.version,started=current.started_at is not None,
                ended=current.ended_at is not None,stored_error=current.error))
    canonical_errors=[]
    for args_json,actor_json in [("NaN",'"owner"'),('{"x":Infinity}','"owner"'),('{}','"\\ud800"')]:
        try: store.create_run(definition_revision="base",session_id="s",idempotency_key="bad",args=json.loads(args_json),run_context=RunContext(message_id="msg",actor_id=json.loads(actor_json)))
        except Exception as e: canonical_errors.append(dict(args_json=args_json,actor_json=actor_json,error=type(e).__name__))
    return dict(steps=rows,matrix=matrix,canonical_errors=canonical_errors)


def _workflow_attempt_contracts() -> dict:
    """Actual attempt start/settlement/artifact effects, including late invalid verification."""
    import dataclasses
    from mini_loop.run_context import RunContext
    from mini_loop.workflows.models import WorkflowDefinition, AttemptClaim, AttemptStatus, NodeStatus, Artifact
    from mini_loop.workflows.store import InMemoryWorkflowStore
    definition=dict(name="wf",revision="base",return_from="a",nodes=[dict(id="a",kind="agent")])
    def seed():
        store=InMemoryWorkflowStore();store.register_definition(WorkflowDefinition.from_dict(definition))
        run=store.create_run(definition_revision="base",session_id="s",idempotency_key="k",args={},run_context=RunContext(message_id="msg"))
        run=store.transition_run(run.run_id,expected_version=0,to_status="RUNNING")
        attempt=store.claim_nodes(run.run_id,[AttemptClaim(node_id="a",agent_id="worker",spawn_index=0)],expected_version=1)[0]
        return store,run,attempt
    def capture(call):
        try: call();return dict(error="",detail="")
        except Exception as error:return dict(error=type(error).__name__,detail=str(error))
    def projection(store,run,attempt,artifact):
        ids={run.run_id:"<run>",attempt.attempt_id:"<attempt>"}
        if artifact is not None:ids[artifact.artifact_id]="<artifact>"
        def clean(value):
            if dataclasses.is_dataclass(value):
                value=dataclasses.asdict(value)
                for key in ("created_at","started_at","heartbeat_at","ended_at"):
                    if key in value and value[key] is not None:value[key]=0
            if isinstance(value,dict):return {key:clean(child) for key,child in value.items()}
            if isinstance(value,(list,tuple)):return [clean(child) for child in value]
            if isinstance(value,str):return ids.get(value,value)
            return value
        data=dict(run=clean(store.get_run(run.run_id)),node=clean(store.get_node(run.run_id,"a")),
            attempt=clean(store.get_attempt(attempt.attempt_id)),artifacts=clean(store.artifacts_for_node(run.run_id,"a")))
        return data,ids
    starts=[]
    for status in AttemptStatus:
        for expected in (0,1):
            store,run,attempt=seed();store._attempts[attempt.attempt_id].status=status
            outcome=capture(lambda:store.start_attempt(attempt.attempt_id,expected_version=expected))
            data,ids=projection(store,run,attempt,None)
            for raw,label in ids.items():outcome["detail"]=outcome["detail"].replace(raw,label)
            starts.append(dict(status=status.value,expected=expected,output_json=json.dumps(data),**outcome))
    recipes=[]
    for a in AttemptStatus:
        for n in NodeStatus:
            if a.is_terminal and n.is_terminal:recipes.append(dict(name=a.value+"/"+n.value,attempt_status=a.value,node_status=n.value))
    recipes.extend([
        dict(name="artifact",artifact=True,verification="verified"),
        dict(name="schema-false",artifact=True,schema_valid=False,verification="refuted"),
        dict(name="foreign-run",artifact=True,foreign="run"),dict(name="foreign-node",artifact=True,foreign="node"),dict(name="foreign-attempt",artifact=True,foreign="attempt"),
        dict(name="stale",expected=0),dict(name="claimed",before="CLAIMED",expected=0),
        dict(name="node-not-running",node_before="FAILED"),
        dict(name="nonterminal-attempt",attempt_status="RUNNING"),dict(name="nonterminal-node",node_status="PENDING"),
        dict(name="bad-attempt",attempt_status="bad"),dict(name="bad-node",node_status="bad"),
        dict(name="late-verification",artifact=True,verification="bad"),dict(name="empty-verification",verification=""),
        dict(name="run-already-terminal",run_before="COMPLETED"),dict(name="node-unverified",node_status="UNVERIFIED",verification="unverified"),
    ])
    commits=[]
    for recipe in recipes:
        store,run,attempt=seed()
        if recipe.get("before")!="CLAIMED":store.start_attempt(attempt.attempt_id,expected_version=0)
        if "node_before" in recipe:store._nodes[(run.run_id,"a")].status=NodeStatus(recipe["node_before"])
        if "run_before" in recipe:store._runs[run.run_id].status=recipe["run_before"]
        artifact=None
        if recipe.get("artifact"):
            binding=dict(run_id=run.run_id,node_id="a",attempt_id=attempt.attempt_id)
            foreign=recipe.get("foreign")
            if foreign:binding[{"run":"run_id","node":"node_id","attempt":"attempt_id"}[foreign]]="foreign"
            artifact=Artifact.create(**binding,value={"ok":True},schema={"type":"object"},verification_status="unverified",schema_valid=recipe.get("schema_valid",True))
        kwargs=dict(expected_version=recipe.get("expected",1),attempt_status=recipe.get("attempt_status","SUCCEEDED"),node_status=recipe.get("node_status","SUCCEEDED"),artifact=artifact,error="detail")
        if "verification" in recipe:kwargs["verification_status"]=recipe["verification"]
        outcome=capture(lambda:store.commit_attempt(attempt.attempt_id,**kwargs))
        data,ids=projection(store,run,attempt,artifact)
        for raw,label in ids.items():outcome["detail"]=outcome["detail"].replace(raw,label)
        repeat=capture(lambda:store.commit_attempt(attempt.attempt_id,**kwargs))
        for raw,label in ids.items():repeat["detail"]=repeat["detail"].replace(raw,label)
        commits.append(dict(recipe=recipe,output_json=json.dumps(data),repeat=repeat,**outcome))
    return dict(starts=starts,commits=commits)


def _workflow_completion_contracts() -> dict:
    """Actual cancellation/finalization folds; initial states are explicit fixtures."""
    import dataclasses
    from mini_loop.run_context import RunContext
    from mini_loop.workflows.models import (
        Artifact, AttemptClaim, AttemptStatus, NodeStatus, RunStatus, WorkflowDefinition,
    )
    from mini_loop.workflows.store import InMemoryWorkflowStore

    recipes = []
    for status in RunStatus:
        for active in (False, True):
            for stale in (False, True):
                recipes.append(dict(op="request", status=status.value, active=active, stale=stale))
            recipes.append(dict(op="finish", status=status.value, active=active))
        recipes.append(dict(op="fail", status=status.value))
        recipes.append(dict(op="finalize", status=status.value, node_status="SUCCEEDED"))
    for status in NodeStatus:
        recipes.append(dict(op="finalize", status="RUNNING", node_status=status.value))
    recipes.extend([
        dict(op="finalize", status="RUNNING", node_status="SUCCEEDED", foreign=True),
        dict(op="finalize", status="RUNNING", node_status="SUCCEEDED", no_artifact=True),
        dict(op="finalize", status="RUNNING", node_status="SUCCEEDED", stale=True),
        dict(op="request", status="RUNNING", reason="", prior_reason=""),
        dict(op="request", status="RUNNING", active=True, reason="new", prior_reason="first"),
        dict(op="request", status="CREATED", reason="first"),
    ])
    for status in AttemptStatus:
        recipes.append(dict(op="cancel_claimed", status="CANCELLING", claims=True, attempt_status=status.value))
    recipes.extend([
        dict(op="cancel_claimed", status="CANCELLING", claims=True, bad_node="a"),
        dict(op="cancel_claimed", status="CANCELLING", claims=True, reason=""),
        dict(op="cancel_claimed", status="RUNNING"),
    ])
    for op in ("request", "finish", "fail", "finalize", "cancel_claimed"):
        recipes.append(dict(op=op, status="RUNNING", missing=True))

    rows = []
    for recipe in recipes:
        store = InMemoryWorkflowStore()
        definition = WorkflowDefinition.from_dict(dict(
            name="wf", revision="base", return_from="a",
            nodes=[dict(id="a", kind="agent"), dict(id="b", kind="agent")],
        ))
        store.register_definition(definition)
        run = store.create_run(
            definition_revision="base", session_id="s", idempotency_key="k", args={},
            run_context=RunContext(message_id="msg"),
        )
        ids = {run.run_id: "<run>"}
        stored = store._runs[run.run_id]
        if recipe.get("claims"):
            stored.status = RunStatus.RUNNING
            attempts = store.claim_nodes(run.run_id, [
                AttemptClaim(node_id="b", agent_id="worker-b", spawn_index=9),
                AttemptClaim(node_id="a", agent_id="worker-a", spawn_index=1),
            ], expected_version=0)
            for attempt in attempts:
                ids[attempt.attempt_id] = "<attempt-" + attempt.node_id + ">"
            store._attempts[attempts[1].attempt_id].status = AttemptStatus(recipe.get("attempt_status", "CLAIMED"))
            if recipe.get("bad_node"):
                store._nodes[(run.run_id, recipe["bad_node"])].status = NodeStatus.FAILED
        stored.status = RunStatus(recipe["status"])
        if "prior_reason" in recipe:
            stored.cancel_reason = recipe["prior_reason"]
        if recipe.get("active"):
            store._nodes[(run.run_id, "a")].status = NodeStatus.RUNNING
            stored.active_node_ids = ("a",)
        if recipe["op"] == "finalize":
            for node in store._nodes.values():
                node.status = NodeStatus(recipe.get("node_status", "SUCCEEDED"))
            artifact = Artifact.create(
                run_id="foreign" if recipe.get("foreign") else run.run_id,
                node_id="b", attempt_id="seed-attempt", value={"ok": True}, schema={},
                schema_valid=False, verification_status="refuted",
            )
            ids[artifact.artifact_id] = "<artifact>"
            if not recipe.get("no_artifact"):
                store._artifacts[artifact.artifact_id] = artifact
        target = "missing" if recipe.get("missing") else run.run_id

        def clean(value):
            if dataclasses.is_dataclass(value):
                value = dataclasses.asdict(value)
                for key in ("created_at", "started_at", "heartbeat_at", "ended_at"):
                    if key in value and value[key] is not None:
                        value[key] = 0
            if isinstance(value, dict):
                return {key: clean(child) for key, child in value.items()}
            if isinstance(value, (list, tuple)):
                return [clean(child) for child in value]
            if isinstance(value, str):
                return ids.get(value, value)
            return value

        def project():
            messages = store.list_outbox(run_id=run.run_id)
            for message in messages:
                ids[message.message_id] = "<outbox>"
            return clean(dict(
                run=store.get_run(run.run_id), nodes=store.list_nodes(run.run_id),
                attempts=store.list_attempts(run.run_id), outbox=messages,
            ))

        def invoke(repeated=False):
            version = store.get_run(run.run_id).version + int(recipe.get("stale", False) and not repeated)
            reason = "second" if repeated else recipe.get("reason")
            try:
                op = recipe["op"]
                if op == "request":
                    kwargs = {} if reason is None else dict(reason=reason)
                    result = store.request_cancel(target, expected_version=version, **kwargs)
                elif op == "finish":
                    result = store.finish_cancellation(target)
                elif op == "fail":
                    result = store.fail_run(target, error="failure")
                elif op == "finalize":
                    result = store.finalize_run(target, expected_version=version, final_artifact_id=artifact.artifact_id)
                else:
                    kwargs = {} if reason is None else dict(error=reason)
                    result = store.cancel_claimed_attempts(target, **kwargs)
                cancelled = clean([item.attempt_id for item in result]) if isinstance(result, list) else []
                return dict(error="", detail="", cancelled=cancelled)
            except Exception as error:
                detail = str(error)
                for raw, label in ids.items():
                    detail = detail.replace(raw, label)
                return dict(error=type(error).__name__, detail=detail, cancelled=[])

        outcome = invoke()
        state = project()
        repeated = invoke(repeated=True)
        rows.append(dict(recipe=recipe, output_json=json.dumps(state), repeat=repeated,
                         repeat_json=json.dumps(project()), **outcome))
    return dict(rows=rows)


def _workflow_outbox_contracts() -> dict:
    """Actual outbox enqueue/lease/settlement, including sequential refusal effects."""
    import dataclasses
    import time
    from mini_loop.run_context import RunContext
    from mini_loop.workflows.models import WorkflowDefinition, RunStatus
    from mini_loop.workflows.store import InMemoryWorkflowStore

    recipes = [
        dict(op="enqueue", kind="a"), dict(op="enqueue", kind="new"),
        dict(op="enqueue", kind="new", terminal=True),
        dict(op="enqueue", kind=""), dict(op="enqueue", kind="", missing=True),
        dict(op="enqueue", kind="new", missing=True), dict(op="enqueue", kind=" "),
    ]
    for state in ("fresh", "active", "expired", "no-time", "no-token", "delivered", "future", "empty-token"):
        for limit in (None, -1, 0, 1, 2):
            recipes.append(dict(op="claim", state=state, limit=limit))
    recipes.extend([
        dict(op="claim", run_ids=[]), dict(op="claim", run_ids=["b"]),
        dict(op="claim", run_ids=["foreign"]), dict(op="claim", session="foreign"),
        dict(op="claim", session=""), dict(op="claim", lease_seconds=0),
        dict(op="claim", lease_seconds=-1), dict(op="claim", state="active", lease_seconds=1),
        dict(op="claim", state="expired", lease_seconds=100),
        dict(op="claim", state="active", lease_kind="nan"),
        dict(op="claim", state="expired", lease_kind="inf"),
    ])
    for op in ("ack", "release"):
        for state in ("active", "expired", "delivered", "fresh", "empty-token"):
            for token in ("old", "wrong", ""):
                recipes.append(dict(op=op, state=state, token=token, messages=["a", "b"]))
        for messages in ([], ["a", "a"], ["a", "foreign"], ["a", "missing"], ["missing"], ["foreign"]):
            recipes.append(dict(op=op, state="active", messages=messages, token="old"))
        recipes.extend([
            dict(op=op, state="active", messages=["a", "b"], token="old", mismatch_b=True),
            dict(op=op, state="active", messages=[], token=""),
            dict(op=op, state="delivered", messages=["a"], token="old", session="foreign"),
        ])

    rows = []
    for recipe in recipes:
        store = InMemoryWorkflowStore()
        store.register_definition(WorkflowDefinition.from_dict(dict(
            name="wf", revision="base", return_from="a", nodes=[dict(id="a", kind="agent")],
        )))
        runs, messages, ids = {}, {}, {}
        for key, session, created in (("a", "s", 30), ("b", "s", 10), ("foreign", "foreign", 20)):
            run = store.create_run(
                definition_revision="base", session_id=session, idempotency_key=key, args={},
                run_context=RunContext(message_id="msg"),
            )
            runs[key] = run.run_id
            ids[run.run_id] = "<run-" + key + ">"
            message = store.enqueue_outbox(run.run_id, kind=key, payload={"value": key})
            messages[key] = message.message_id
            ids[message.message_id] = "<message-" + key + ">"
            store._outbox[message.message_id] = dataclasses.replace(message, created_at=created)
        if recipe.get("terminal"):
            store._runs[runs["a"]].status = RunStatus.COMPLETED
        now = time.time()
        for key in ("a", "b"):
            message = store._outbox[messages[key]]
            state = recipe.get("state", "fresh")
            token, claimed, delivered = None, None, None
            if state in ("active", "expired", "no-time", "future", "empty-token"):
                token = "" if state == "empty-token" else "old"
                if state != "no-time":
                    claimed = now + 60 if state == "future" else now - (60 if state == "expired" else 5)
            if state == "no-token":
                claimed = now - 5
            if state == "delivered":
                delivered = 1.0
            if key == "b" and recipe.get("mismatch_b"):
                token = "other"
            store._outbox[message.message_id] = dataclasses.replace(
                message, claim_token=token, claimed_at=claimed, delivered_at=delivered,
            )

        def clean(value):
            if dataclasses.is_dataclass(value):
                value = dataclasses.asdict(value)
                for stamp in ("created_at", "claimed_at", "delivered_at"):
                    if value.get(stamp) is not None:
                        value[stamp] = 0
            if isinstance(value, dict):
                return {key: clean(child) for key, child in value.items()}
            if isinstance(value, (list, tuple)):
                return [clean(child) for child in value]
            if isinstance(value, str):
                return ids.get(value, value)
            return value

        def invoke(repeated=False):
            try:
                op = recipe["op"]
                result, token = [], None
                if op == "enqueue":
                    target = "missing" if recipe.get("missing") else runs["a"]
                    message = store.enqueue_outbox(target, kind=recipe["kind"], payload={"changed": True})
                    ids.setdefault(message.message_id, "<message-new>")
                    result = [message]
                elif op == "claim":
                    kwargs = {}
                    if "run_ids" in recipe:
                        kwargs["run_ids"] = {runs[key] for key in recipe["run_ids"]}
                    if "lease_seconds" in recipe:
                        kwargs["lease_seconds"] = recipe["lease_seconds"]
                    if "lease_kind" in recipe:
                        kwargs["lease_seconds"] = float(recipe["lease_kind"])
                    token, result = store.claim_outbox(
                        session_id=recipe.get("session", "s"), limit=recipe.get("limit"), **kwargs,
                    )
                    ids[token] = "<claim-repeat>" if repeated else "<claim>"
                else:
                    kwargs = dict(session_id=recipe.get("session", "s"), claim_token=recipe["token"],
                                  message_ids=[messages.get(key, "missing") for key in recipe["messages"]])
                    if op == "ack":
                        result = store.acknowledge_outbox(**kwargs)
                    else:
                        store.release_outbox(**kwargs)
                return dict(error="", detail="", token=clean(token), result_json=json.dumps(clean(result)))
            except Exception as error:
                detail = str(error)
                for raw, label in ids.items():
                    detail = detail.replace(raw, label)
                return dict(error=type(error).__name__, detail=detail, token=None, result_json="[]")

        outcome = invoke()
        state = json.dumps(clean(store.list_outbox()))
        repeated = invoke(True)
        rows.append(dict(recipe=recipe, output_json=state, repeat=repeated,
                         repeat_json=json.dumps(clean(store.list_outbox())), **outcome))
    return dict(rows=rows)


def _workflow_retention_contracts() -> dict:
    """Actual whole graph retention and bounded launch deduplication."""
    import dataclasses
    from mini_loop.run_context import RunContext
    from mini_loop.workflows.models import Artifact, AttemptClaim, RunStatus, WorkflowDefinition, NodeStatus
    from mini_loop.workflows.store import InMemoryWorkflowStore, MAX_TERMINAL_RUNS

    definition = WorkflowDefinition.from_dict(dict(
        name="wf", revision="base", return_from="a", nodes=[dict(id="a", kind="agent")],
    ))
    recipes = [dict(status=status.value, notice=notice, keep=0)
               for status in RunStatus for notice in ("none", "pending", "claimed", "delivered")]
    recipes.extend(dict(status="COMPLETED", notice=notice, keep=keep)
                   for keep in (-1, 1, 2, 4, 100) for notice in ("delivered", "pending"))
    recipes.extend([
        dict(status="COMPLETED", notice="delivered", keep=0, node_status="RUNNING"),
        dict(status="FAILED", notice="delivered", keep=0, node_status="PENDING"),
        dict(status="COMPLETED", notice="expired", keep=0),
    ])
    rows = []
    for recipe in recipes:
        store = InMemoryWorkflowStore()
        store.register_definition(definition)
        runs, attempts, artifacts, ids = {}, {}, {}, {}
        inputs = {}
        for key, session, created in (("a", "s", 20), ("b", "s", 30), ("c", "s", 10), ("d", "foreign", 40)):
            inputs[key] = dict(definition_revision="base", session_id=session, idempotency_key=key,
                               args={}, run_context=RunContext(message_id="msg"))
            if key == "d":
                inputs[key]["parent_run_id"] = runs["a"]
            run = store.create_run(**inputs[key])
            runs[key] = run.run_id
            ids[run.run_id] = "<run-" + key + ">"
            store.transition_run(run.run_id, expected_version=0, to_status="RUNNING")
            attempt = store.claim_nodes(run.run_id, [AttemptClaim(node_id="a", agent_id="worker", spawn_index=0)], expected_version=1)[0]
            attempts[key] = attempt.attempt_id
            ids[attempt.attempt_id] = "<attempt-" + key + ">"
            store.start_attempt(attempt.attempt_id, expected_version=0)
            artifact = Artifact.create(run_id=run.run_id, node_id="a", attempt_id=attempt.attempt_id,
                                       value={"key": key}, schema={})
            artifacts[key] = artifact.artifact_id
            ids[artifact.artifact_id] = "<artifact-" + key + ">"
            store.commit_attempt(attempt.attempt_id, expected_version=1, attempt_status="SUCCEEDED",
                                 node_status="SUCCEEDED", artifact=artifact)
            store.finalize_run(run.run_id, expected_version=3, final_artifact_id=artifact.artifact_id)
            message = store.list_outbox(run_id=run.run_id)[0]
            ids[message.message_id] = "<outbox-" + key + ">"
            notice = recipe["notice"] if key == "a" else "delivered"
            if notice == "none":
                del store._outbox[message.message_id]
                del store._outbox_keys[(run.run_id, message.kind)]
            elif notice in ("claimed", "delivered", "expired"):
                token, leased = store.claim_outbox(session_id=session, run_ids={run.run_id})
                ids[token] = "<claim-" + key + ">"
                if notice == "delivered":
                    store.acknowledge_outbox(session_id=session, message_ids=[leased[0].message_id], claim_token=token)
                elif notice == "expired":
                    store._outbox[message.message_id] = dataclasses.replace(store._outbox[message.message_id], claimed_at=0)
            stored = store._runs[run.run_id]
            stored.created_at = created
            stored.status = RunStatus(recipe["status"] if key == "a" else "RUNNING" if key == "c" else "COMPLETED")
            store._outbox.update({mid: dataclasses.replace(m, created_at=created)
                                 for mid, m in store._outbox.items() if m.run_id == run.run_id})
        if "node_status" in recipe:
            store._nodes[(runs["a"], "a")].status = NodeStatus(recipe["node_status"])

        def clean(value):
            if dataclasses.is_dataclass(value):
                value = dataclasses.asdict(value)
                for stamp in ("created_at", "started_at", "ended_at", "heartbeat_at", "claimed_at", "delivered_at"):
                    if stamp in value and value[stamp] is not None:
                        value[stamp] = 0
            if isinstance(value, dict):
                return {key: clean(child) for key, child in value.items()}
            if isinstance(value, (list, tuple)):
                return [clean(child) for child in value]
            if isinstance(value, str):
                return ids.get(value, value)
            return value

        def project():
            return clean(dict(
                runs=store.list_runs(),
                nodes=[store.get_node(runs[key], "a") for key in runs if runs[key] in store._runs],
                attempts=[store.get_attempt(attempts[key]) for key in attempts if attempts[key] in store._attempts],
                artifacts=[store.get_artifact(artifacts[key]) for key in artifacts if artifacts[key] in store._artifacts],
                outbox=store.list_outbox(),
                launches=[[key, ids[store._launches[(inputs[key]["session_id"], key)][1]]]
                          for key in inputs if (inputs[key]["session_id"], key) in store._launches],
                outbox_keys=sorted([[ids[run], kind, ids[mid]] for (run, kind), mid in store._outbox_keys.items()]),
                definitions=sorted(store._definitions), hashes=sorted(store._definition_hashes),
            ))

        pruned = store.prune_terminal_runs(keep=recipe["keep"])
        state = project()
        repeat = store.prune_terminal_runs(keep=recipe["keep"])
        replay = store.create_run(**inputs["a"])
        fresh = replay.run_id != runs["a"]
        changed_error = ""
        try:
            store.create_run(**(inputs["a"] | {"args": {"changed": True}}))
        except Exception as error:
            changed_error = type(error).__name__
        rows.append(dict(recipe=recipe, pruned=clean(pruned), output_json=json.dumps(state),
                         repeat=clean(repeat), fresh_replay=fresh, replay_status=replay.status.value,
                         changed_error=changed_error))

    store = InMemoryWorkflowStore()
    store.register_definition(definition)
    first = None
    for index in range(MAX_TERMINAL_RUNS + 1):
        run = store.create_run(definition_revision="base", session_id="s", idempotency_key=str(index),
                               args={}, run_context=RunContext(message_id="msg"))
        stored = store._runs[run.run_id]
        stored.status, stored.created_at = RunStatus.FAILED, index
        if first is None:
            first = run.run_id
    pruned = store.prune_terminal_runs()
    default = dict(limit=MAX_TERMINAL_RUNS, removed_first=pruned == [first], remaining=len(store._runs),
                   repeat_count=len(store.prune_terminal_runs()), launch_first_retained=("s", "0") in store._launches)

    # Stable IDs make source's timestamp tie-break observable without UUID noise.
    store = InMemoryWorkflowStore()
    store._runs = {key: dataclasses.replace(run, run_id=key, status=RunStatus.FAILED, created_at=1)
                   for key in ("z", "a", "b")}
    ties = store.prune_terminal_runs(keep=1)
    return dict(rows=rows, default=default, ties=ties)


def _workflow_engine_contracts() -> dict:
    """Actual batch scheduler, inputs and structured/fallback attempt settlement."""
    import asyncio
    import dataclasses
    from mini_loop.run_context import RunContext
    from mini_loop.workflows.artifacts import return_artifact, ArtifactSubmission
    from mini_loop.workflows.engine import WorkflowEngine
    from mini_loop.workflows.models import WorkflowDefinition, RunStatus, NodeStatus
    from mini_loop.workflows.store import InMemoryWorkflowStore

    recipes = []
    for status in RunStatus:
        recipes.append(dict(name="entry-" + status.value, status=status.value))
    for behavior in ("ok", "runtime", "value-error", "type-error", "nil", "wrong-tool", "bad-value"):
        recipes.append(dict(name="agent-" + behavior, behavior=behavior))
        recipes.append(dict(name="verify-" + behavior, behavior=behavior, verify=True))
    for verification in ("verified", "refuted", "unverified", "invalid", ""):
        recipes.append(dict(name="verification-" + verification, verify=True, verification=verification))
    recipes.extend([
        dict(name="diamond", graph="diamond"),
        dict(name="diamond-serial", graph="diamond", concurrency=1),
        dict(name="parallel-failure", graph="parallel", behavior="runtime"),
        dict(name="budget-exhausted", attempts_used=32),
        dict(name="deadlocked", node_before="CANCELLED"),
        dict(name="empty-return", node_before="SUCCEEDED"),
        dict(name="failed-node", node_before="FAILED"),
        dict(name="dependency-args", graph="args"),
        dict(name="cancel-running", cancel=True),
        dict(name="cancel-permit-wait", cancel=True, blocked=True),
    ])

    async def scenario(recipe):
        store = InMemoryWorkflowStore()
        nodes = [dict(id="a", kind="verify" if recipe.get("verify") else "agent", output_schema={"type": "object"})]
        return_from = "a"
        if recipe.get("graph") in ("diamond", "parallel"):
            nodes = [dict(id="a", kind="agent"), dict(id="b", kind="agent")]
            if recipe["graph"] == "diamond":
                nodes += [dict(id="c", kind="reduce", needs=["a", "b"]), dict(id="v", kind="verify", needs=["c"])]
                return_from = "v"
        elif recipe.get("graph") == "args":
            nodes = [dict(id="args", kind="agent"), dict(id="a", kind="reduce", needs=["args"])]
        definition = WorkflowDefinition.from_dict(dict(name="wf", revision="base", return_from=return_from, nodes=nodes))
        store.register_definition(definition)
        run = store.create_run(definition_revision="base", session_id="s", idempotency_key="k",
                               args={"input": 1}, run_context=RunContext(message_id="msg"))
        stored = store._runs[run.run_id]
        stored.status = RunStatus(recipe.get("status", "QUEUED"))
        stored.attempts_used = recipe.get("attempts_used", 0)
        if "node_before" in recipe:
            node = store._nodes[(run.run_id, "a")]
            node.status = NodeStatus(recipe["node_before"])
            if node.status == NodeStatus.FAILED:
                node.error = "seeded failure"
        calls = []
        started = asyncio.Event()
        async def runner(attempt, node, inputs):
            calls.append(dict(node_id=node.id, spawn_index=attempt.spawn_index, inputs=inputs))
            if recipe.get("cancel"):
                started.set()
                await asyncio.Event().wait()
            behavior = recipe.get("behavior", "ok")
            if behavior == "runtime":
                raise RuntimeError("worker failed")
            if behavior == "value-error":
                raise ValueError("worker failed")
            if behavior == "type-error":
                raise TypeError("worker failed")
            if behavior == "nil":
                return None
            if behavior == "wrong-tool":
                return ArtifactSubmission(value={"ok": True}, tool_name="wrong")
            if behavior == "bad-value":
                return return_artifact("wrong")
            if node.kind.value == "verify":
                return return_artifact({"status": recipe.get("verification", "verified")})
            return return_artifact({"node": node.id})
        engine = WorkflowEngine(store, runner, max_concurrent_agents=recipe.get("concurrency", 4),
                                attempt_semaphore=asyncio.Semaphore(0) if recipe.get("blocked") else None)
        error, detail = "", ""
        try:
            if recipe.get("cancel"):
                task = asyncio.create_task(engine.execute(run.run_id))
                if recipe.get("blocked"):
                    while not any(attempt.status.value == "RUNNING" for attempt in store.list_attempts(run.run_id)):
                        await asyncio.sleep(0)
                else:
                    await started.wait()
                await engine.cancel(run.run_id)
                await task
            else:
                await engine.execute(run.run_id)
        except Exception as failure:
            error, detail = type(failure).__name__, str(failure)
        attempts = store.list_attempts(run.run_id)
        ids = {run.run_id: "<run>"}
        for attempt in attempts:
            ids[attempt.attempt_id] = "<attempt-" + attempt.node_id + ">"
            ids[attempt.agent_id] = "<agent-" + attempt.node_id + ">"
        artifacts = [artifact for node in definition.nodes for artifact in store.artifacts_for_node(run.run_id, node.id)]
        for artifact in artifacts:
            ids[artifact.artifact_id] = "<artifact-" + artifact.node_id + ">"
        messages = store.list_outbox(run_id=run.run_id)
        for message in messages:
            ids[message.message_id] = "<outbox>"
        def clean(value):
            if dataclasses.is_dataclass(value):
                value = dataclasses.asdict(value)
                for stamp in ("created_at", "started_at", "ended_at", "heartbeat_at"):
                    if stamp in value and value[stamp] is not None:
                        value[stamp] = 0
            if isinstance(value, dict):
                return {key: clean(child) for key, child in value.items()}
            if isinstance(value, (list, tuple)):
                return [clean(child) for child in value]
            if isinstance(value, str):
                return ids.get(value, value)
            return value
        data = clean(dict(run=store.get_run(run.run_id), nodes=store.list_nodes(run.run_id),
                          attempts=attempts, artifacts=artifacts, outbox=messages,
                          calls=sorted(calls, key=lambda call: call["spawn_index"])))
        for raw, label in ids.items():
            detail = detail.replace(raw, label)
        repeat_error = ""
        try:
            await engine.execute(run.run_id)
        except Exception as failure:
            repeat_error = type(failure).__name__
        return dict(recipe=recipe, output_json=json.dumps(data), error=error, detail=detail, repeat_error=repeat_error)

    async def collect():
        return [await scenario(recipe) for recipe in recipes]
    constructor = []
    for limit in (0, -1, 1, 4, 5):
        try:
            WorkflowEngine(InMemoryWorkflowStore(), lambda *_: None, max_concurrent_agents=limit)
            constructor.append(dict(limit=limit, error="", detail=""))
        except Exception as failure:
            constructor.append(dict(limit=limit, error=type(failure).__name__, detail=str(failure)))
    return dict(rows=asyncio.run(collect()), constructor=constructor)


def _workflow_runner_contracts(scratch: Path) -> dict:
    """Exercise real isolated Agents with scripted native tool calls."""
    import asyncio
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
    from mini_loop.run_context import RunContext
    from mini_loop.workflows.models import NodeAttempt, WorkflowNode, NodeKind
    from mini_loop.workflows import runner as module
    scratch = Path(scratch)
    base = module.Agent
    workers = []
    class CapturedAgent(base):
        def __init__(self, **kwargs):
            super().__init__(**kwargs)
            workers.append(self)
    recipes = [
        dict(name="object", schema={"type": "object"}, values=[{"value": "ok"}]),
        dict(name="repair", schema={"type": "string"}, values=[1, "repaired"]),
        dict(name="duplicate", schema={"type": "string"}, values=["first", "second"]),
        dict(name="numeric-enum", schema={"type": "integer", "enum": [1, 2], "const": 2}, values=[2]),
        dict(name="null", schema={"type": "null"}, values=[None]),
        dict(name="missing", schema={"type": "object"}, values=[]),
        dict(name="exhausted", schema={"type": "string"}, values=[1, "unreached"], node_rounds=1),
        dict(name="task-fallback", schema={"type": "object"}, values=[{}], task=""),
    ]
    async def scenario(recipe):
        root = scratch / ("workflow-worker-" + recipe["name"])
        root.mkdir(parents=True, exist_ok=True)
        settings = Settings(fake_llm=True, workspace_root=root / "workspaces", skills_dir=root / "skills")
        index = 0
        def responder(request):
            nonlocal index
            current = index
            index += 1
            if current < len(recipe["values"]):
                return [tool("return_artifact", _id="u" + str(current), value=recipe["values"][current])], "tool_use"
            return [text("done")], "end_turn"
        client = FakeAsyncAnthropic(responder=responder, thinking=False)
        launch = RunContext(message_id="launch", actor_id="human", authority="explicit_human", approved_capabilities=frozenset({"workflow.launch", "workflow.manage"}))
        runner = module.FreshAgentRunner(client=client, settings=settings, workspace=root, context_resolver=lambda _: launch, max_rounds=4)
        node = WorkflowNode("a", NodeKind.AGENT, output_schema=recipe["schema"], max_rounds=recipe.get("node_rounds"), prompt_template=recipe.get("task", "inspect"))
        attempt = NodeAttempt("attempt", "run", "a", 1, "worker", 0)
        submission, error, detail = None, "", ""
        try:
            submission = await runner(attempt, node, {"args": {"text": "你好"}})
        except Exception as failure:
            error, detail = type(failure).__name__, str(failure)
        worker = workers[-1]
        context = runner.last_run_context
        return dict(recipe=recipe, value=submission.value if submission else None, error=error, detail=detail,
                    calls=client.calls, tools=list(runner.last_tool_names), system=worker._system,
                    rounds=worker.max_rounds, mode=worker.state["permission_mode"],
                    schema=worker.tools.get("return_artifact").input_schema,
                    context=dict(authority=context.authority, actor_id=context.actor_id, parent_message_id=context.parent_message_id,
                                 delegated_by=context.delegated_by, approved_capabilities=sorted(context.approved_capabilities)))
    async def collect():
        return [await scenario(recipe) for recipe in recipes]
    module.Agent = CapturedAgent
    try:
        return dict(rows=asyncio.run(collect()))
    finally:
        module.Agent = base


def _workflow_views_contracts() -> dict:
    """Actual status/summary, bounded leases, UTF-8 preview and parent append/ack."""
    import asyncio
    import dataclasses
    from types import SimpleNamespace
    from mini_loop.actions import InMemoryActionJournal
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.run_context import RunContext
    from mini_loop.workflows.models import WorkflowDefinition, RunStatus, Artifact
    from mini_loop.workflows.store import InMemoryWorkflowStore
    from mini_loop.workflows.service import WorkflowService, workflow_injector
    recipes = [dict(name="status-"+status.value, status=status.value) for status in RunStatus]
    recipes += [
        dict(name="object-result", artifact=True, value={"z": 1.0, "a": "你好"}),
        dict(name="ascii-at-bound", artifact=True, text="x", repeat=7998),
        dict(name="ascii-over-bound", artifact=True, text="x", repeat=7999),
        dict(name="utf8-at-bound", artifact=True, text="界", repeat=2666),
        dict(name="utf8-over-bound", artifact=True, text="界", repeat=2667),
        dict(name="fallback", payload={"error": {"why": "fault"}, "cancel_reason": False}, run_error="", cancel_reason=""),
        dict(name="run-diagnostic-wins", payload={"error": "fallback", "cancel_reason": "fallback"}, run_error="run fault", cancel_reason="run reason"),
        dict(name="same-turn", launch_turn=1, turn=1),
        dict(name="future-turn", launch_turn=2, turn=1),
        dict(name="past-turn", launch_turn=1, turn=2),
        dict(name="negative-turn", launch_turn=-2, turn=-1),
        dict(name="no-turn-yet", turn=0),
        dict(name="foreign-session", session="foreign"),
        dict(name="foreign-status", status_session="foreign"),
        dict(name="missing-artifact", missing_artifact=True),
        dict(name="release", mode="release", artifact=True, value="retrievable"),
        dict(name="ack", mode="ack"),
        dict(name="append", mode="append", artifact=True, value="result"),
        dict(name="append-failure", mode="append-failure"),
        dict(name="count-bound", count=51),
    ]
    def scenario(recipe):
        store = InMemoryWorkflowStore()
        definition = WorkflowDefinition.from_dict(dict(name="wf", revision="base", return_from="a", nodes=[dict(id="a", kind="agent")]))
        store.register_definition(definition)
        service = WorkflowService(settings=Settings(fake_llm=True), client=FakeAsyncAnthropic(), action_journal=InMemoryActionJournal(), session_resolver=lambda _: None, store=store)
        ids, runs = {}, []
        for index in range(recipe.get("count", 1)):
            run = store.create_run(definition_revision="base", session_id="s", idempotency_key="k"+str(index), args={}, run_context=RunContext(message_id="msg"))
            ids[run.run_id] = "<run-"+str(index)+">"
            stored = store._runs[run.run_id]
            stored.status = RunStatus(recipe.get("status", "COMPLETED"))
            stored.error = recipe.get("run_error")
            stored.cancel_reason = recipe.get("cancel_reason")
            if recipe.get("artifact"):
                value = recipe.get("text", "")*recipe["repeat"] if "repeat" in recipe else recipe.get("value")
                artifact = Artifact.create(run_id=run.run_id, node_id="a", attempt_id="seed", value=value, schema={})
                store._artifacts[artifact.artifact_id] = artifact
                stored.final_artifact_id = artifact.artifact_id
                ids[artifact.artifact_id] = "<artifact-"+str(index)+">"
            if recipe.get("missing_artifact"):
                stored.final_artifact_id = "absent"
            if "launch_turn" in recipe:
                service._launch_turns.setdefault(run.run_id, recipe["launch_turn"])
            notice = store.enqueue_outbox(run_id=run.run_id, kind="notice", payload=recipe.get("payload", {}))
            ids[notice.message_id] = "<notice-"+str(index)+">"
            runs.append(run)
        def clean(value):
            if dataclasses.is_dataclass(value):
                value = dataclasses.asdict(value)
                for stamp in ("created_at", "claimed_at", "delivered_at"):
                    if stamp in value and value[stamp] is not None:
                        value[stamp] = 0
                if value.get("claim_token") is not None and "claim_token" in value:
                    value["claim_token"] = "<claim>"
            if isinstance(value, dict):
                return {key: clean(child) for key, child in value.items()}
            if isinstance(value, (list, tuple)):
                return [clean(child) for child in value]
            if isinstance(value, str):
                for raw, label in ids.items():
                    value = value.replace(raw, label)
                return value
            return value
        statuses, status_error = [], ""
        try:
            statuses = [service.status(run.run_id, session_id=recipe.get("status_session", "s")) for run in runs]
        except Exception as error:
            status_error = type(error).__name__
        notifications, message_ids, token = [], (), ""
        messages, append_observed_pending = [], False
        class Messages(list):
            def append(self, message):
                nonlocal append_observed_pending
                append_observed_pending = all(notice.delivered_at is None and notice.claim_token is not None for notice in store.list_outbox())
                if recipe.get("mode") == "append-failure":
                    raise RuntimeError("append failed")
                super().append(message)
        messages = Messages()
        error, detail = "", ""
        try:
            session = recipe.get("session", "s")
            turn = recipe.get("turn", 1)
            if recipe.get("mode") in ("append", "append-failure"):
                parent = SimpleNamespace(state={"workflow_service": service, "session": SimpleNamespace(id=session, run_count=turn)}, messages=messages)
                asyncio.run(workflow_injector(parent))
            else:
                notifications, message_ids, token = service.prepare_notifications(session_id=session, parent_turn=turn)
                if recipe.get("mode") == "release":
                    service.release_notifications(session_id=session, message_ids=message_ids, claim_token=token)
                if recipe.get("mode") == "ack":
                    service.acknowledge_notifications(session_id=session, message_ids=message_ids, claim_token=token)
        except Exception as failure:
            error, detail = type(failure).__name__, str(failure)
        output = clean(dict(statuses=statuses, status_error=status_error, summaries=service.summaries(recipe.get("session", "s")), notifications=notifications, message_ids=message_ids, has_token=bool(token), messages=messages, append_observed_pending=append_observed_pending, outbox=store.list_outbox()))
        return dict(recipe=recipe, error=error, detail=clean(detail), output_json=json.dumps(output, ensure_ascii=False))
    return dict(rows=[scenario(recipe) for recipe in recipes])



def _workflow_admission_contracts() -> dict:
    """Actual dynamic service admission and launch policy digest, without execution."""
    import copy
    from mini_loop.actions import InMemoryActionJournal
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.workflows.models import WorkflowDefinition, content_hash
    from mini_loop.workflows.service import WorkflowService
    base = dict(name="wf", return_from="a", nodes=[dict(id="a", kind="agent")])
    recipes = [("default", {}, {}, False), ("typed-default", {}, {}, True),
               ("forged-metadata", dict(definition_hash={"fake": True}, definition_id=[1], revision=42,
                                        parent_revision=False, source="forged", source_version={}), {}, False),
               ("tools-reversed", dict(policy=dict(allowed_tools=["glob", "read_file"])), {}, False),
               ("authority-retained", dict(policy=dict(origin_authority_required="untrusted")), {}, False),
               ("invalid-before-cap", dict(name="!"), dict(max_rounds=1), False),
               ("duplicate-tools", dict(policy=dict(allowed_tools=["glob", "glob"])), {}, False),
               ("subset-tools", dict(policy=dict(allowed_tools=["glob"])), {}, False),
               ("unknown-field", dict(unrecognized=True), {}, False),
               ("missing-name", {}, {}, False), ("missing-return", {}, {}, False),
               ("missing-both", {}, {}, False)]
    for key, lowered in (("max_concurrent_agents", 2), ("max_agents", 16), ("max_rounds", 2), ("wall_time_seconds", 100.5)):
        recipes.append(("cap-"+key, {}, {key: lowered}, False))
        recipes.append(("at-"+key, dict(budget={key: lowered}), {key: lowered}, False))
    recipes += [("cap-order", {}, dict(max_concurrent_agents=2, max_agents=16, max_rounds=2, wall_time_seconds=100.5), False),
                ("fractional-wall", dict(budget=dict(wall_time_seconds=0.125)), dict(wall_time_seconds=0.125), False),
                ("graph-before-cap", dict(nodes=[dict(id="a", kind="agent", needs=["a"])]), dict(max_rounds=1), False)]
    rows = []
    for name, patch, caps_patch, typed in recipes:
        payload = copy.deepcopy(base)
        payload.update(patch)
        if name in ("missing-name", "missing-both"):
            payload.pop("name")
        if name in ("missing-return", "missing-both"):
            payload.pop("return_from")
        caps = dict(max_concurrent_agents=4, max_agents=32, max_rounds=4, wall_time_seconds=900.0)
        caps.update(caps_patch)
        settings = Settings(fake_llm=True, **{"workflow_"+key: value for key, value in caps.items()})
        service = WorkflowService(settings=settings, client=FakeAsyncAnthropic(), action_journal=InMemoryActionJournal(), session_resolver=lambda _: None)
        supplied = WorkflowDefinition.from_dict(payload) if typed else payload
        input_payload = supplied.to_dict() if typed else payload
        error, detail, output, digest = "", "", None, ""
        try:
            definition = service._definition(supplied)
            output = definition.to_dict()
            digest = content_hash(dict(policy=definition.policy, **caps), prefix="wfpolicy")
        except Exception as failure:
            error, detail = type(failure).__name__, str(failure)
        rows.append(dict(name=name, input_json=json.dumps(input_payload, ensure_ascii=False), caps=caps,
                         error=error, detail=detail, output_json=json.dumps(output, ensure_ascii=False), policy_hash=digest))
    return dict(rows=rows)


def _workflow_tool_contracts(scratch: Path) -> dict:
    """Source tool schemas and actual memory/SQLite workflow journal effects."""
    import copy
    import dataclasses
    import tempfile
    from mini_loop.actions import InMemoryActionJournal, DurableActionJournal, _payload_hash
    from mini_loop.agent import _tool_action_id
    from mini_loop.registry import ToolRegistry, ToolCall
    from mini_loop.run_context import RunContext
    from mini_loop.storage import SQLiteStateStore
    from mini_loop.workflows.tools import install_workflows
    from mini_loop.workflows.validation import validate_json_value
    registry = install_workflows(ToolRegistry())
    base = dict(definition=dict(name="wf", nodes=[dict(id="a", kind="agent")], return_from="a"), args={})
    inputs = [("launch", "Workflow", base),
              ("empty-definition", "Workflow", dict(definition={}, args={})),
              ("original-metadata", "Workflow", dict(definition={**base["definition"], "revision":"original", "source":"plugin", "definition_hash":False}, args={"z":1.0,"a":"汉字😀\n\u2028<>&"})),
              ("nested", "Workflow", dict(definition=base["definition"], args={"z":[None,False,1,1.0,{"secret":"value"}],"a":2**80})),
              ("status", "WorkflowStatus", dict(run_id="wf_run")),
              ("cancel", "WorkflowCancel", dict(run_id="wf_run")),
              ("empty-status", "WorkflowStatus", dict(run_id="")),
              ("unicode-cancel", "WorkflowCancel", dict(run_id="汉字😀\n\u2028<>&"))]
    context = dataclasses.replace(RunContext.default(), message_id="m")
    rows = []
    for name, tool_name, value in inputs:
        validate_json_value(registry.get(tool_name).input_schema, value)
        rows.append(dict(name=name, tool=tool_name, input_json=json.dumps(value, ensure_ascii=False),
                         canonical=json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":")),
                         spaced=json.dumps(value, ensure_ascii=False, sort_keys=True), input_hash=_payload_hash(value),
                         action_id=_tool_action_id(session_id="s", run_context=context, call=ToolCall(tool_name,value,"u"))))
    schemas = [registry.get(name).schema for name in ("Workflow","WorkflowStatus","WorkflowCancel")]
    refusals=[]
    for tool_name in ("Workflow", "WorkflowStatus", "WorkflowCancel"):
        values = ([{}, dict(definition={}), dict(args={}), dict(definition=None,args={}), dict(definition=[],args={}),
                   dict(definition={},args=None),dict(definition={},args=[]),dict(definition={},args={},extra=True)]
                  if tool_name=="Workflow" else [{},dict(run_id=None),dict(run_id=1),dict(run_id=False),dict(run_id=[]),dict(run_id="r",extra=True)])
        for value in values:
            error=""
            try: validate_json_value(registry.get(tool_name).input_schema,value)
            except Exception as failure: error=type(failure).__name__
            assert error
            refusals.append(dict(tool=tool_name,input_json=json.dumps(value),error=error))
    def clean(record):
        value=dataclasses.asdict(record);value["created_at"]=0
        if value["completed_at"] is not None: value["completed_at"]=0
        return value
    runs=[]
    for backing in ("memory","sqlite"):
        for mode in ("attach", "finish-then-attach", "fallback-id", "changed-args", "changed-metadata", "normalized-defaults", "changed-tool-use", "changed-session", "changed-message"):
            with tempfile.TemporaryDirectory(prefix="workflow-action-",dir=str(scratch)) as temp:
                store=SQLiteStateStore(Path(temp)/"state.db") if backing=="sqlite" else None
                journal=DurableActionJournal(store) if store else InMemoryActionJournal()
                value=copy.deepcopy(base)
                request=dict(action_id="action",session_id="s",message_id="m",tool_use_id="action" if mode=="fallback-id" else "u",tool_name="Workflow",input_value=value)
                records=[clean(journal.begin(**request)),clean(journal.attach_workflow("action","run"))]
                replay=copy.deepcopy(request)
                if mode=="changed-args": replay["input_value"]["args"]={"x":1}
                if mode=="changed-metadata": replay["input_value"]["definition"]["revision"]="forged"
                if mode=="normalized-defaults":
                    from mini_loop.workflows.models import WorkflowDefinition
                    replay["input_value"]["definition"]=WorkflowDefinition.from_dict(value["definition"]).to_dict()
                if mode=="changed-tool-use": replay["tool_use_id"]="other"
                if mode=="changed-session": replay["session_id"]="other"
                if mode=="changed-message": replay["message_id"]="other"
                if mode=="finish-then-attach": records.append(clean(journal.finish("action",status="completed",result="async_launched")))
                errors=[]
                for operation in (lambda:journal.begin(**replay),lambda:journal.attach_workflow("action","run"),lambda:journal.attach_workflow("action","other"),lambda:journal.attach_workflow("missing","run")):
                    try: records.append(clean(operation()));errors.append("")
                    except Exception as failure: errors.append(type(failure).__name__)
                records.append(clean(journal.get("action")))
                runs.append(dict(backing=backing,mode=mode,input_json=json.dumps(value), replay_json=json.dumps(replay["input_value"]),records=records,errors=errors))
                if store: store.close()
    return dict(inputs=rows,schemas=schemas,refusals=refusals,runs=runs)


def _workflow_service_contracts() -> dict:
    """Actual owned service/engine/store/journal; replace only isolated worker body."""
    import asyncio
    import copy
    import dataclasses
    from types import SimpleNamespace
    from mini_loop.actions import InMemoryActionJournal
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.run_context import RunContext, WORKFLOW_LAUNCH, WORKFLOW_MANAGE
    from mini_loop.workflows.artifacts import ArtifactSubmission
    import mini_loop.workflows.service as module
    recipes = [dict(name=name) for name in ("complete","raw-action","serial","verify","worker-fault","factory-fault","missing-result","invalid-result","timeout","cancel","close","wait-shield","foreign-cancel","terminal-replay","payload-conflict","observer-fault")]
    recipes += [dict(name=name) for name in ("untrusted","no-capability","no-action","no-parent","authority-policy","args-schema","closed-first")]
    async def scenario(recipe):
        mode=recipe["name"]
        definition=dict(name="wf",return_from="a",nodes=[dict(id="a",kind="agent")])
        if mode=="serial": definition=dict(name="wf",return_from="b",nodes=[dict(id="a",kind="agent"),dict(id="b",kind="reduce",needs=["a"])])
        if mode=="verify": definition=dict(name="wf",return_from="b",nodes=[dict(id="a",kind="agent"),dict(id="b",kind="verify",needs=["a"])])
        if mode=="raw-action": definition.update(revision="forged",source="plugin",source_version="v",definition_hash=False)
        if mode=="timeout": definition["budget"]=dict(wall_time_seconds=0.02)
        if mode=="authority-policy":definition["policy"]=dict(origin_authority_required="untrusted")
        if mode=="args-schema":definition["input_schema"]=dict(type="object",required=["x"],properties=dict(x=dict(type="string")))
        started,release=asyncio.Event(),asyncio.Event()
        events,calls=[],[]
        async def emit(event):
            if mode=="observer-fault":raise RuntimeError("observer failed")
            events.append(event)
        parent=SimpleNamespace(workspace="<workspace>",emit=emit)
        class Worker:
            def __init__(self, **kwargs):
                if mode=="factory-fault":raise RuntimeError("factory fault")
                self.config=kwargs
            async def __call__(self,attempt,node,inputs):
                context=self.config["context_resolver"](attempt)
                calls.append(dict(node_id=node.id,spawn_index=attempt.spawn_index,inputs=inputs,context=context.as_dict(),max_rounds=self.config["max_rounds"],workspace=str(self.config["workspace"])))
                await self.config["emit"](dict(type="tool_use",name="compress",id="u",input=dict(secret="excluded"),output="excluded",text="excluded"))
                started.set()
                if mode in ("timeout","cancel","close","wait-shield"):await release.wait()
                if mode=="worker-fault":raise RuntimeError("worker fault")
                if mode=="missing-result":return None
                value=["bad"] if mode=="invalid-result" else (dict(status="verified") if node.kind.value=="verify" else dict(node=node.id))
                return ArtifactSubmission(value=value)
        original=module.FreshAgentRunner;module.FreshAgentRunner=Worker
        journal=InMemoryActionJournal()
        service=module.WorkflowService(settings=Settings(fake_llm=True),client=FakeAsyncAnthropic(),action_journal=journal,session_resolver=lambda _: None if mode=="no-parent" else parent)
        context=dataclasses.replace(RunContext.explicit_human(actor_id="human",approved_capabilities=(WORKFLOW_LAUNCH,WORKFLOW_MANAGE)),message_id="m")
        if mode in ("untrusted","closed-first"):context=dataclasses.replace(RunContext.default(),message_id="m")
        if mode=="no-capability":context=dataclasses.replace(context,approved_capabilities=())
        request=dict(session_id="s",definition=definition,args={},run_context=context,action_id="" if mode=="no-action" else "action",launch_turn=2,tool_use_id="u")
        if mode=="raw-action":request["action_input"]=dict(definition=copy.deepcopy(definition),args={})
        launches,error,detail,operation_error,operation_detail=[],"","","",""
        run=None
        try:
            if mode=="closed-first":await service.close()
            try:
                result=await service.launch(**request);launches.append(result.as_dict())
                if mode in ("cancel","close","wait-shield","timeout"):await started.wait()
                if mode=="cancel":await service.cancel(result.run_id)
                if mode=="close":await service.close()
                if mode=="wait-shield":
                    waiter=asyncio.create_task(service.wait(result.run_id));await asyncio.sleep(0);waiter.cancel()
                    try:await waiter
                    except asyncio.CancelledError:pass
                    release.set()
                run=await service.wait(result.run_id)
                if mode in ("terminal-replay","payload-conflict"):
                    replay=copy.copy(request)
                    if mode=="payload-conflict":replay["args"]={"changed":True}
                    try:launches.append((await service.launch(**replay)).as_dict())
                    except Exception as failure:operation_error,operation_detail=type(failure).__name__,str(failure)
                if mode=="foreign-cancel":
                    try:await service.cancel(result.run_id,session_id="other")
                    except Exception as failure:operation_error,operation_detail=type(failure).__name__,str(failure)
            except Exception as failure:error,detail=type(failure).__name__,str(failure)
            ids={}
            attempts=service.store.list_attempts(run.run_id) if run else []
            artifacts=[]
            if run:
                ids[run.run_id]="<run>"
                for attempt in attempts:
                    ids[attempt.attempt_id]="<attempt-"+attempt.node_id+">";ids[attempt.agent_id]="<agent-"+attempt.node_id+">"
                for node in service.store.list_nodes(run.run_id):artifacts+=service.store.artifacts_for_node(run.run_id,node.node_id)
                for artifact in artifacts:ids[artifact.artifact_id]="<artifact-"+artifact.node_id+">"
            outbox=service.store.list_outbox()
            for notice in outbox:ids[notice.message_id]="<outbox>"
            def clean(value):
                if dataclasses.is_dataclass(value):value=dataclasses.asdict(value)
                if isinstance(value,dict):
                    return {key:(0 if key in ("created_at","started_at","ended_at","heartbeat_at","completed_at","occurred_at") and child is not None else "<event>" if key=="event_id" else clean(child)) for key,child in value.items()}
                if isinstance(value,(list,tuple)):return [clean(child) for child in value]
                if isinstance(value,str):
                    for raw,label in ids.items():value=value.replace(raw,label)
                    return value
                return value
            data=clean(dict(launches=launches,run=run,nodes=service.store.list_nodes(run.run_id) if run else [],attempts=attempts,artifacts=artifacts,outbox=outbox,events=events,calls=calls,journal=journal.get("action"),observability=service.observability_errors,active=service.has_active("s")))
            return dict(recipe=recipe,definition_json=json.dumps(definition),error=error,detail=clean(detail),operation_error=operation_error,operation_detail=clean(operation_detail),output_json=json.dumps(data,ensure_ascii=False))
        finally:
            release.set();await service.close();module.FreshAgentRunner=original
    async def collect():return [await scenario(recipe) for recipe in recipes]
    return dict(rows=asyncio.run(collect()))


if __name__ == "__main__":
    raise SystemExit(main())
