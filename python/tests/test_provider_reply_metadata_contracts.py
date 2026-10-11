import json
from pathlib import Path

from tools.provider_reply_metadata_contracts import provider_reply_metadata_contracts


def test_actual_sdk_reply_metadata_defaults_and_stream_updates():
    fixture = provider_reply_metadata_contracts()
    assert fixture["sdk_version"] == "0.107.1"
    assert len(fixture["cases"]) == 44
    rows = {(row["name"], row["mode"]): row["expected_metadata"] for row in fixture["cases"]}
    for mode in ("direct", "stream"):
        assert rows["absent", mode] == {"container": None, "stop_details": None}
        assert rows["container-epoch", mode]["container"]["expires_at"] == "2030-01-02T03:04:05Z"
        assert rows["container-naive", mode]["container"]["expires_at"] == "2030-01-02T03:04:05"
        assert rows["container-missing-expiry", mode]["container"]["expires_at"] is None
        assert rows["container-invalid-expiry", mode]["container"]["expires_at"] == "not-a-date"
        assert rows["container-fractional", mode]["container"]["expires_at"] == "2030-01-02T03:04:05.123400Z"
        assert rows["container-offset", mode]["container"]["expires_at"] == "2030-01-02T03:04:05+08:00"
        assert rows["container-empty", mode]["container"] == {"id": None, "expires_at": None}
        assert rows["refusal-empty", mode]["stop_details"] == {"type": None, "category": None, "explanation": None}
        assert rows["refusal-field-types", mode]["stop_details"] == {"type": 0, "category": ["future"], "explanation": False}
        assert rows["container-scalar", mode]["container"] == "opaque"
        assert rows["refusal-defaults", mode]["stop_details"] == {"type": "refusal", "category": None, "explanation": None}
        assert rows["refusal-extra", mode]["stop_details"]["future"] == [False, "x"]
        assert rows["refusal-scalar", mode]["stop_details"] == ["opaque"]
    assert rows["delta-container", "direct"]["container"]["id"] == "replacement"
    assert rows["delta-container", "stream"]["container"]["id"] == "container_α"
    assert rows["delta-refusal", "stream"]["stop_details"]["category"] == "cyber"
    assert rows["delta-refusal-null", "direct"]["stop_details"] is None
    assert rows["delta-refusal-null", "stream"]["stop_details"]["type"] == "refusal"
    expected = Path(__file__).resolve().parents[2] / "go/testdata/python-provider-reply-metadata.json"
    assert json.loads(expected.read_text()) == fixture
