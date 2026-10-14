import json
from pathlib import Path

from tools.null_boundary_contracts import null_boundary_contracts


def test_actual_default_null_secret_and_state_boundaries():
    observed = null_boundary_contracts()
    fixture = Path(__file__).resolve().parents[2] / "go/testdata/python-null-boundaries.json"
    assert observed == json.loads(fixture.read_text())
    assert {(row["layer"], row["construction"]) for row in observed["rows"]} == {
        (layer, construction) for layer in ("agent", "session", "manager") for construction in ("omitted", "none")}
    assert len(observed["rows"]) == 6
    for row in observed["rows"]:
        assert row["secret_class"] == "NullSecretRegistry"
        assert row["mask_text"] == row["output"] == observed["sample"]
        assert row["mask_payload"] == observed["payload"]
        assert row["model_calls"] == 1
        assert row["live_messages"] == 2
        assert row["sqlite_connect_calls"] == 0
        assert row["sqlite_files_after_stop"] == []
        if row["layer"] != "agent":
            assert row["state_class"] == "NullStateStore"
            assert row["stored_sessions"] == row["stored_messages"] == row["stored_events"] == 0
            assert row["session_status"] == "idle" and row["busy"] is False and row["run_count"] == 1
            assert row["persist_error"] is None
