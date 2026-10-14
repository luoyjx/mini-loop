import json
from pathlib import Path

from tools.public_identity_audit_contracts import public_identity_audit_contracts


def test_actual_public_identity_audit_contracts(tmp_path):
    actual = public_identity_audit_contracts(tmp_path)
    root = Path(__file__).resolve().parents[2]
    assert actual == json.loads((root / "go/testdata/python-public-identity-audit.json").read_text())
    assert all(actual["public_aliases"].values())
    assert actual["build"] == dict(
        lowercase_hex=True, length=12, matches_package_content=True,
        stable_within_process=True, corpus_cached_after_edit=True,
        corpus_changed_after_cache_clear=True, corpus_ignores_non_source=True,
        cache_maxsize=1,
    )
    assert actual["build_failure"] == dict(
        exception_class="IsADirectoryError", failure_cache_size=0,
        recovered_after_repair=True, success_cached_after_edit=True,
        recovery_cache_misses=2, recovery_cache_hits=1, recovery_cache_size=1,
    )
    assert len(actual["wall_clock"]) == 8
    assert all(row["build_matches_public"] and row["pid_matches_process"] for row in actual["wall_clock"])
    clocks = {row["name"]: row for row in actual["wall_clock"]}
    assert clocks["backward"]["uptime_s"] == -0.25
    assert clocks["forward"]["uptime_s"] == 0.125
    assert clocks["positive-tie-even"]["uptime_s"] == 0.062
    assert clocks["negative-tie-even"]["uptime_s"] == -0.062
    assert clocks["negative-zero"]["uptime_negative_zero"] is True
    assert len(actual["runtime"]) == 3
    for row in actual["runtime"]:
        assert row["pid_matches_process"] and row["build_matches_public"]
        assert row["started_at_stable"] and row["uptime_nonnegative"] and row["uptime_nondecreasing"]
    assert actual["runtime"][0]["posture"] is None
    assert actual["runtime"][1]["posture"]["authenticated"] is False
    assert actual["runtime"][2]["posture"]["authenticated"] is True
    assert actual["manager_side_effects"] == dict(sessions=0, model_calls=0)
    assert len(actual["renderer"]) == 6
    assert all(row["unchanged_after_render"] for row in actual["renderer"])
    assert actual["renderer"][0]["rendered"] == "audit: no findings."
    assert actual["renderer"][1]["rendered"].endswith("4 finding(s): 1 critical, 1 high, 1 medium, 1 info")
    assert len(actual["invalid_findings"]) == 5
    assert all(row["exception_class"] == "ValueError" for row in actual["invalid_findings"])
