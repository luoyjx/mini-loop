import json
import socket
from pathlib import Path

from tools.permission_event_contracts import permission_event_contracts


def test_managed_permission_events_match_source_contract(monkeypatch):
    def deny_network(*args, **kwargs):
        raise AssertionError("permission contracts must not open a network connection")

    monkeypatch.setattr(socket.socket, "connect", deny_network)
    monkeypatch.setattr(socket.socket, "connect_ex", deny_network)
    observed = permission_event_contracts()
    root = Path(__file__).resolve().parents[2]
    assert observed == json.loads((root / "go/testdata/python-permission-events.json").read_text())
    assert len(observed["source_sha256"]) == 9
    rows = {row["name"]: row for row in observed["cases"]}
    assert len(rows) == 14
    assert all(row["pending_approvals"] == 0 for row in rows.values())
    for row in rows.values():
        if row["approval_fault"] != "cancel":
            assert row["error"] is None
            assert (row["output"], row["status"]) == ("done", "idle")
        for event in row["permission_events"]:
            assert len(event) == 9
            assert (event["type"], event["session"], event["message_id"],
                    event["agent"], event["depth"]) == ("permission", "session", "turn", "main", 0)
    unknown = rows["readonly-unclassified"]
    assert unknown["permission_events"][0]["reason"] == "risk=None"
    assert "tool risk: unclassified" in unknown["tool_results"][0]["output"]
    assert unknown["handler_calls"] == []
    assert rows["immutable-auto"]["permission_events"][0]["rule"] == "immutable-deny-list"
    assert rows["immutable-auto"]["handler_calls"] == []
    assert rows["readonly-read"]["permission_events"] == []
    for name in ("readonly-read", "auto-external", "interactive-allow", "masked-interactive"):
        assert len(rows[name]["handler_calls"]) == 1
    assert rows["interactive-deny"]["handler_calls"] == []
    for name in ("auto-two-rules", "interactive-two-allow"):
        assert [event["decision"] for event in rows[name]["permission_events"]] == ["allow", "allow"]
        assert len(rows[name]["handler_calls"]) == 1
    assert [event["decision"] for event in rows["interactive-later-deny"]["permission_events"]] == ["allow", "deny"]
    for name in ("approval-later-error", "approval-later-cancel"):
        row = rows[name]
        assert [event["decision"] for event in row["permission_events"]] == ["allow"]
        assert row["handler_calls"] == []
        trace = [item["type"] for item in row["ordered_trace"]]
        assert trace[:4] == ["tool_use", "approval_callback", "permission", "approval_callback"]
    assert rows["approval-later-error"]["tool_results"][0]["error"] is True
    assert rows["approval-later-cancel"]["error"]["type"] == "CancelledError"
    assert rows["approval-later-cancel"]["tool_results"] == []
    masked = rows["masked-interactive"]
    assert masked["permission_events"][0]["tool"] == "probe_<hidden>"
    assert "secret_token" not in json.dumps(masked["permission_events"] + masked["tool_results"])
    assert masked["handler_calls"] == [{"tool": "probe_secret_token", "command": "echo secret_token"}]
