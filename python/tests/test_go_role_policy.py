"""Public role policies retain Unicode isolation through actual children."""

import json
from pathlib import Path

from tools.role_policy_contracts import role_policy_contracts


def test_role_policy_source_profiles():
    actual = role_policy_contracts()
    expected = json.loads((Path(__file__).resolve().parents[2] /
                           "go/testdata/python-role-policy.json").read_text())
    assert actual == expected
    assert len(actual["cases"]) == 34
    assert len(actual["children"]) == 7
    assert len(actual["invalid_tools"]) == 6
    for row in actual["invalid_tools"]:
        assert row["error"] == f"ValueError: Tool '{row['name']}' capabilities must be non-empty strings"
    rows = {row["name"]: row for row in actual["cases"]}
    for order in ["forward", "reverse"]:
        assert "read_probe" in rows[f"dotted-{order}-dotted"]["names"]
        assert "write_probe" not in rows[f"dotted-{order}-dotted"]["names"]
        assert rows[f"dotted-{order}-ascii"]["names"] == ["write_probe"]
        assert rows[f"dotted-{order}-expanded"]["names"] == rows[f"dotted-{order}-dotted"]["names"]
        assert rows[f"sigma-{order}-upper"]["names"] == rows[f"sigma-{order}-final"]["names"]
        assert rows[f"sigma-{order}-medial"]["names"] == ["write_probe"]
    assert rows["raw-duplicate-before-normalization"]["names"] == ["write_probe"]
    assert "read_probe" in rows["raw-duplicate-reversed-position"]["names"]
    assert rows["unicode-capability-dotted"]["names"] == ["dotted_cap"]
    assert rows["unicode-capability-ascii"]["names"] == ["ascii_cap"]
    assert rows["unicode-capability-sigma"]["names"] == ["sigma_cap"]
    assert rows["capability-whitespace-retained"]["names"] == ["spaced_cap"]
    assert rows["empty-capability-set"]["names"] == []
    assert rows["empty-capability-text"]["names"] == []
    assert rows["empty-profile-mapping"]["failed"]
    for row in actual["cases"]:
        assert "unclassified_probe" not in row["names"]
        assert row["names"] == [tool["name"] for tool in row["selected"]]
    for row in actual["children"]:
        assert row["request_count"] == 2 and row["summary"] == "child done"
        assert [part["tool_use_id"] for part in row["tool_results"]] == [
            attempt["id"] for attempt in row["attempts"]]
        assert all(part["type"] == "tool_result" and "is_error" not in part
                   for part in row["tool_results"])
        assert row["parent_history_unchanged"] and row["parent_marker_unchanged"]
        assert row["parent_mode"] == "auto"
        assert row["child_label"] == "main>" + row["role"].lower()
        assert row["parent_context"]["approved_capabilities"]
        assert row["peer_context"]["authority"] == "peer_agent"
        assert row["peer_context"]["parent_message_id"] == "parent"
        assert row["peer_context"]["approved_capabilities"] == []
        assert all(not observation["parent_marker_present"] for observation in row["observations"])
    children = {row["name"]: row for row in actual["children"]}
    assert children["dotted-read-isolation"]["effects"] == ["read_probe"]
    assert children["ascii-write-isolation"]["effects"] == ["write_probe"]
    assert children["sigma-read-isolation"]["effects"] == ["read_probe"]
    explore = children["explore-alias-readonly"]
    assert explore["effects"] == ["read_probe"]
    assert all(observation["mode"] == "readonly" for observation in explore["observations"])
    assert next(output for output in explore["outputs"] if output["id"] == "write_attempt")["denied"]
