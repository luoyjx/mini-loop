"""Pinned Source behavior for the Go runtime-posture port."""

import json
from pathlib import Path

from tools.posture_contracts import posture_contracts


def test_posture_contracts_match_export_across_roots(tmp_path):
    target = Path(__file__).resolve().parents[2] / "go/testdata/python-posture.json"
    expected = json.loads(target.read_text())
    assert posture_contracts(tmp_path / "first") == expected
    assert posture_contracts(tmp_path / "second") == expected


def test_posture_uses_runtime_state_without_creating_sessions(tmp_path):
    cases = {row["name"]: row for row in posture_contracts(tmp_path)["cases"]}
    for row in cases.values():
        assert row["status"] == 200
        assert row["health"]["posture"] == row["direct"] == row["after"]
        assert row["sessions_before"] == row["sessions_after"]
        assert row["resources_before"] == row["resources_after"]
    assert cases["default-empty"]["sessions_after"] == 0
    default = cases["default-empty"]["direct"]
    assert default["cache_policy"] == "DefaultCachePolicy"
    assert default["stuck_detector"] == "DefaultStuckDetector"
    live = cases["live-overrides"]
    assert live["before"] == default
    assert live["direct"]["cache_policy"] == "NullCachePolicy"
    assert live["direct"]["stuck_detector"] == "NullStuckDetector"
    assert live["direct"]["sandbox_reason"] == "fixture live agent unavailable"
    assert cases["unregistered"]["direct"]["secrets_unregistered"] == 2
    assert cases["registered"]["direct"]["secrets_unregistered"] == 0
    assert cases["registered-case-sensitive"]["direct"]["secrets_unregistered"] == 2
    assert cases["null-registry"]["direct"]["secrets_unregistered"] == 0
    failed = cases["broken-store"]
    assert failed["before"]["state_store_error"] is None
    assert failed["direct"]["state_store_error"] == "RuntimeError: fixture disk unavailable"
    assert cases["sqlite-auth"]["direct"]["authenticated"]
    assert cases["sqlite-auth"]["direct"]["state_store"] == "SQLiteStateStore"
    assert cases["sqlite-auth"]["direct"]["action_journal"] == "DurableActionJournal"
    # The public features field describes deployment settings, not inferred tools.
    assert cases["full-settings-custom-manager"]["health"]["features"]
    assert cases["workflows"]["direct"]["workflows"]
    assert not cases["workflows"]["health"]["features"]

    assert cases["owner-resources"]["direct"]["user_resources"]
    assert cases["owner-resources"]["resources_after"] == 0
    assert cases["trajectories"]["direct"]["trajectories"]
