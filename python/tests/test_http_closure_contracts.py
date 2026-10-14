import json
from pathlib import Path

from tools.http_closure_contracts import http_closure_contracts


def test_http_closure_matches_actual_routes():
    observed = http_closure_contracts()
    fixture = Path(__file__).resolve().parents[2] / "go/testdata/python-http-closure.json"
    assert observed == json.loads(fixture.read_text())
    rows = {row["name"]: row for row in observed["cases"]}
    assert len(rows) == len(observed["cases"]) == 24
    assert all(row["model_calls"] == 0 for row in rows.values())
    assert rows["multiple-errors"]["status"] == 422
    assert [item["loc"] for item in rows["multiple-errors"]["response"]["detail"]] == [
        ["body", "cron"], ["body", "prompt"], ["body", "recurring"], ["body", "durable"]]
    assert rows["foreign-invalid"]["status"] == rows["missing-session-invalid"]["status"] == 422
    assert rows["bad-auth-invalid"]["status"] == 401
    assert rows["valid-string-bools"]["cron_jobs"] == 1
    assert rows["valid-numeric-bools"]["cron_jobs"] == 2
    assert rows["workflow-running-detail"]["response"]["status"] == "RUNNING"
    assert rows["workflow-cancelled-detail"]["response"]["status"] == "CANCELLED"
    assert rows["workflow-cancelled-detail"]["response"]["cancel_reason"] == "operator stop"
    assert rows["workflow-cancelled-detail"]["response"]["active_node_ids"] == []
