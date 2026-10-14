"""Source no-team requests retain owner/auth and non-consuming boundaries."""
import json
from pathlib import Path

from tools.teamless_http_contracts import teamless_http_contracts


def test_actual_teamless_http_contracts(tmp_path):
    actual = teamless_http_contracts(tmp_path)
    root = Path(__file__).resolve().parents[2]
    assert actual == json.loads((root / "go/testdata/python-teamless-http.json").read_text())
    rows = {row["name"]: row for row in actual["cases"]}
    assert len(rows) == 12
    assert actual["sessions_created"] == 2
    assert actual["model_calls"] == 0
    assert all(row["mailbox_unchanged"] and row["model_calls"] == 0 for row in rows.values())
    for name in ("missing-team-key", "null-team-key", "detached-agent", "teamless-corrupt-mailbox"):
        row = rows[name]
        assert row["status"] == 200
        assert row["response"] == {"session": "session-alice", "team": None}
        assert row["actual_peek_calls"] == 0
    assert rows["default-lead"]["response"] == rows["restored-lead"]["response"]
    assert rows["default-lead"]["response"]["team"] == "session-alice"
    assert len(rows["default-lead"]["response"]["inbox"]) == 2
    assert rows["default-lead"]["actual_peek_calls"] == rows["restored-lead"]["actual_peek_calls"] == 1
    policy = rows["default-lead"]["headers"]["content-security-policy"]
    assert policy.startswith("default-src 'none';")
    for row in rows.values():
        if row["status"] < 500:
            assert row["headers"]["content-security-policy"] == policy
            assert row["headers"]["x-content-type-options"] == "nosniff"
            assert row["headers"]["x-frame-options"] == "DENY"
            assert row["headers"]["referrer-policy"] == "no-referrer"
    for name, status in (("foreign-teamless", 404), ("unknown-teamless", 404),
                         ("no-auth-teamless", 401), ("bad-auth-teamless", 401),
                         ("wrong-method-teamless", 405)):
        assert rows[name]["status"] == status
        assert rows[name]["actual_peek_calls"] == 0
    assert rows["restored-corrupt-mailbox"]["status"] == 500
    assert rows["restored-corrupt-mailbox"]["actual_peek_calls"] == 1
    assert rows["restored-corrupt-mailbox"]["headers"] == {"content-type": "text/plain; charset=utf-8"}
