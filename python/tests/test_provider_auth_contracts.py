import json
from pathlib import Path
from tools.provider_auth_contracts import provider_auth_contracts


def test_missing_authentication_is_deferred_and_never_sent(tmp_path):
    expected = json.loads((Path(__file__).resolve().parents[2] /
                           "go/testdata/python-provider-auth.json").read_text())
    assert provider_auth_contracts(tmp_path) == expected
    assert all(row["constructed"] and not row["calls"] for row in expected["cases"])
    assert [row["fault"]["type"] for row in expected["cases"]] == ["TypeError"] * 3 + ["ValueError"]


def test_source_authentication_precedence_and_headers(tmp_path):
    captured = provider_auth_contracts(tmp_path)
    rows = {row["name"]: row for row in captured["source_cases"]}
    assert len(rows) == 10
    assert all(len(row["calls"]) == 1 and row["text"] == "ok" for row in rows.values())
    headers = lambda name: rows[name]["calls"][0]["headers"]
    assert headers("explicit-suppresses-env") == {"x-api-key": "explicit-key"}
    assert headers("environment-both") == {"x-api-key": "env-key", "authorization": "Bearer env-token"}
    assert headers("empty-falls-through") == {"authorization": "Bearer env-token"}
    assert headers("custom-overrides-static") == {
        "x-api-key": "custom-key", "authorization": "Bearer custom-token", "x-fixture": "last"}
    assert rows["explicit-endpoint"]["calls"][0]["url"] == "https://explicit.invalid/v1/messages"
    assert rows["environment-endpoint"]["calls"][0]["url"] == "https://environment.invalid/v1/messages"
    assert rows["environment-bearer"]["description"]["credential"] == "<absent>"
