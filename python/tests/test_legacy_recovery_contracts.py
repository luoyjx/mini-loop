import asyncio
import json
import os
import random
import socket
from pathlib import Path

from tools.legacy_recovery_contracts import legacy_recovery_contracts


def test_actual_legacy_recovery_construction_and_runs_match_contract(monkeypatch):
    def deny_network(*args, **kwargs):
        raise AssertionError("legacy recovery contracts must use the offline fake client")

    monkeypatch.setattr(socket.socket, "connect", deny_network)
    monkeypatch.setattr(socket.socket, "connect_ex", deny_network)
    environment, sleep, jitter = dict(os.environ), asyncio.sleep, random.random
    observed = legacy_recovery_contracts()
    assert dict(os.environ) == environment
    assert asyncio.sleep is sleep and random.random is jitter
    root = Path(__file__).resolve().parents[2]
    assert observed == json.loads((root / "go/testdata/python-legacy-recovery.json").read_text())
    assert len(observed["source_sha256"]) == 12

    constructors = {row["name"]: row for row in observed["constructors"]}
    assert len(constructors) == 9
    assert constructors["absent"]["selected"] is None
    assert constructors["empty"]["selected"] is None
    assert constructors["whitespace"]["selected"] == " \t "
    assert constructors["explicit-priority"]["selected"] == "explicit"
    assert constructors["explicit-empty"]["selected"] == "legacy"
    assert constructors["modern-ignored"]["selected"] is None
    assert constructors["capture-stable"]["after_selected"] == "before"
    assert constructors["explicit-512"]["selected"] == "m" * 512
    assert constructors["legacy-513"]["selected"] == "m" * 513
    assert all(row["selected"] == row["after_selected"] for row in constructors.values())

    agents = {row["name"]: row for row in observed["agents"]}
    assert len(agents) == 11
    assert agents["legacy-only"]["settings_fallback"] is None
    assert agents["legacy-only"]["selected"] == "legacy"
    assert agents["modern-priority"]["selected"] == "modern"
    assert agents["modern-empty"]["settings_fallback"] is None
    assert agents["modern-empty"]["selected"] == "legacy"
    assert agents["modern-whitespace"]["selected"] == " \t "
    assert agents["legacy-between-settings-agent"]["selected"] == "after"
    assert agents["modern-between-settings-agent"]["selected"] == "before"
    assert agents["legacy-after-agent"]["after_selected"] == "before"
    assert agents["explicit-direct"]["recovery_class"] == "DirectRecovery"
    assert agents["explicit-direct"]["selected"] is None
    assert agents["explicit-recovery"]["selected"] == "explicit"
    assert agents["explicit-recovery-empty"]["selected"] == "legacy"
    assert all(row["selected"] == row["after_selected"] for row in agents.values())

    runs = {row["name"]: row for row in observed["runs"]}
    assert len(runs) == 6
    for name, target in (("legacy-only", "legacy"), ("modern-priority", "modern"),
                         ("captured-before-turn", "before"), ("no-fallback", None),
                         ("legacy-long-513", "m" * 513)):
        row = runs[name]
        assert row["outputs"] == ["done", "done"]
        assert row["errors"] == [None, None]
        assert row["error_events"] == []
        assert row["waits"] == [0.5, 1.0, 2.0]
        assert [request["model"] for request in row["requests"]] == ["primary"] * 3 + [target or "primary"] * 2
        assert [request["turn"] for request in row["requests"]] == [1, 1, 1, 1, 2]
        assert row["model_after_turns"] == [target, target]
        actions = [event["action"] for event in row["events"]]
        assert actions == (["retry", "retry", "fallback_model", "retry"] if target else ["retry"] * 3)
        assert all((event["type"], event["agent"], event["depth"], event["message_id"]) ==
                   ("recovery", "main", 0, "turn-1") for event in row["events"])
    direct = runs["explicit-direct"]
    assert direct["requests"] == [{"turn": 1, "model": "primary"}]
    assert direct["events"] == direct["waits"] == []
    assert direct["outputs"] == ["[Error] Overloaded: fixture overloaded"]
    assert direct["errors"] == [None]
    assert direct["error_events"] == [{"type": "error", "agent": "main", "depth": 0,
                                       "message_id": "turn-1", "error": "Overloaded: fixture overloaded"}]
    assert direct["model_after_turns"] == [None]

    managers = {row["name"]: row for row in observed["managers"]}
    assert len(managers) == 5
    for name, first, second, injected, recovery_class in (
        ("default-changing", "first", "second", False, "DefaultRecovery"),
        ("modern-fixed", "fixed-modern", "fixed-modern", False, "DefaultRecovery"),
        ("explicit-shared", "captured-policy", "captured-policy", True, "DefaultRecovery"),
        ("direct-shared", None, None, True, "DirectRecovery"),
        ("empty-then-value", None, "later", False, "DefaultRecovery"),
    ):
        row = managers[name]
        assert row["first"] == {"selected": first, "recovery_class": recovery_class}
        assert row["second"] == {"selected": second, "recovery_class": recovery_class}
        assert row["first_after_second"] == row["first"]
        assert row["shared_recovery"] is injected
        assert row["first_uses_injected"] is injected
        assert row["second_uses_injected"] is injected
        assert row["manager_recovery_class"] == (recovery_class if injected else None)
    assert managers["default-changing"]["settings_fallback"] is None
    assert managers["modern-fixed"]["settings_fallback"] == "fixed-modern"
    assert managers["direct-shared"]["settings_fallback"] == "modern"
    assert managers["empty-then-value"]["settings_fallback"] is None
