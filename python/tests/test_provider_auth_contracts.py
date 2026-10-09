import json
from pathlib import Path
from tools.provider_auth_contracts import provider_auth_contracts


def test_missing_authentication_is_deferred_and_never_sent(tmp_path):
    expected = json.loads((Path(__file__).resolve().parents[2] /
                           "go/testdata/python-provider-auth.json").read_text())
    assert provider_auth_contracts(tmp_path) == expected
    assert all(row["constructed"] and not row["calls"] for row in expected["cases"])
    assert [row["fault"]["type"] for row in expected["cases"]] == ["TypeError"] * 3 + ["ValueError"]
