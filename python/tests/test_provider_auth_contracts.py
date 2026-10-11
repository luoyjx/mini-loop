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


def test_settings_provider_key_suppresses_environment_bearer(tmp_path):
    rows = {row["name"]: row for row in provider_auth_contracts(tmp_path)["settings_cases"]}
    headers = lambda name: rows[name]["calls"][0]["headers"]
    assert headers("settings-bearer") == {"authorization": "Bearer env-token"}
    assert headers("settings-empty-key") == {"x-api-key": "", "authorization": "Bearer env-token"}
    assert headers("settings-key-suppresses-bearer") == {"x-api-key": "env-key"}
    assert headers("settings-custom-overrides") == {"x-api-key": "custom-key", "authorization": "Bearer custom-token"}


def test_discovery_phases_caching_refresh_and_file_guards(tmp_path):
    rows = {row["spec"]["name"]: row for row in provider_auth_contracts(tmp_path)["discovery_cases"]}
    assert len(rows) == 51
    for name in ["explicit-config-dir-missing", "missing-explicit-profile", "missing-active-profile",
                 "invalid-profile-name", "corrupt-explicit-profile", "federation-refuses-http"]:
        assert not rows[name]["constructed"] and not rows[name]["requests"]
    assert rows["corrupt-fallback-profile"]["constructed"]
    assert rows["corrupt-fallback-profile"]["fault"]["type"] == "TypeError"
    for name in ["world-readable-profile", "symlink-profile-secret", "missing-profile-token"]:
        assert rows[name]["constructed"] and not rows[name]["requests"]
        assert rows[name]["fault"]["type"] == "AnthropicError"
    assert len(rows["federation-before-fallback"]["exchanges"]) == 1
    assert rows["federation-before-fallback"]["requests"][0]["headers"]["anthropic-beta"] == "oauth-2025-04-20"
    assert rows["explicit-profile-before-federation"]["exchanges"] == []
    assert rows["explicit-profile-before-federation"]["requests"][0]["headers"]["authorization"] == "Bearer profile-token"
    assert rows["profile-endpoint-workspace"]["requests"][0]["url"] == "https://profile.invalid/v1/messages"
    assert rows["profile-endpoint-workspace"]["requests"][0]["headers"]["anthropic-workspace-id"] == "profile-workspace"
    assert rows["environment-endpoint-wins"]["requests"][0]["url"] == "https://environment.invalid/v1/messages"
    failure = rows["federation-exchange-failure"]
    assert len(failure["exchanges"]) == 2 and not failure["requests"]
    assert "must-redact" not in failure["fault"]["message"]
    assert len(rows["federation-model-401"]["exchanges"]) == 1
    assert rows["federation-model-401"]["fault"]["type"] == "AuthenticationError"
    rotation = rows["federation-file-rotation"]
    assert [exchange["body"]["assertion"] for exchange in rotation["exchanges"]] == ["file.jwt.assertion", "rotated.jwt.assertion"]
    refresh = rows["oauth-refresh"]
    assert refresh["exchanges"][0]["body"] == {"grant_type": "refresh_token", "refresh_token": "fixture-refresh", "client_id": "fixture-client"}
    assert refresh["credentials_after"]["access_token"] == "exchanged-token"
    assert refresh["credentials_after"]["expires_at"] == 1700003600
    assert refresh["credentials_mode"] == 0o600


def test_federation_profile_disk_cache_refresh_and_reconstruction(tmp_path):
    rows = {row["spec"]["name"]: row for row in provider_auth_contracts(tmp_path)["discovery_cases"]}
    for name in ["fresh", "31", "fractional"]:
        row = rows[f"profile-federation-{name}"]
        assert row["constructed"] and row["exchanges"] == [] and row["texts"] == ["ok", "ok"]
        assert row["requests"][0]["headers"]["authorization"] == "Bearer disk-token"
        assert "anthropic-workspace-id" not in row["requests"][0]["headers"]
    for name in ["missing-cache", "30", "corrupt-expiry", "force", "reopen", "empty-cache", "env-fill", "write-failure"]:
        row = rows[f"profile-federation-{name}"]
        assert row["constructed"] and len(row["exchanges"]) == 1 and row["texts"] == ["ok", "ok"]
        assert "scope" not in row["exchanges"][0]["body"]
    for name in ["missing-rule", "unsupported-identity", "empty-identity-path", "invalid-cache-type", "world-readable", "symlink-cache"]:
        row = rows[f"profile-federation-{name}"]
        assert row["constructed"] and row["requests"] == [] and row["exchanges"] == []
        assert row["fault"]["type"] == ("WorkloadIdentityError" if name == "missing-rule" else "AnthropicError")
    updated = rows["profile-federation-corrupt-expiry"]["credentials_after"]
    assert updated["extra"] == {"generation": 7} and updated["refresh_token"] == "preserved-refresh"
    assert updated["access_token"] == "exchanged-token" and updated["expires_at"] == 1700003600
    assert rows["profile-federation-30"]["credentials_mode"] == 0o600
    assert len(rows["profile-federation-reopen"]["constructors"]) == 2
    env = rows["profile-federation-env-fill"]["exchanges"][0]["body"]
    assert env["federation_rule_id"] == "env-rule" and env["organization_id"] == "env-org"
    assert env["workspace_id"] == "env-workspace"
    assert rows["profile-federation-env-fill"]["credentials_mode"] == 0o600


def test_discovery_defined_empty_and_literal_reader_lifecycle(tmp_path):
    rows = {row["spec"]["name"]: row for row in provider_auth_contracts(tmp_path)["discovery_cases"]}
    key = rows["empty-key-shadows-explicit-profile"]
    assert key["constructed"] and key["fault"]["type"] == "TypeError"
    assert not key["requests"] and not key["exchanges"]
    for name, value in [("empty-auth-shadows-explicit-profile", "Bearer "),
                        ("static-auth-shadows-explicit-profile", "Bearer static-bearer")]:
        row = rows[name]
        assert row["constructed"] and row["fault"] is None and not row["exchanges"]
        assert [request["headers"]["authorization"] for request in row["requests"]] == [value, value]
    custom = rows["custom-auth-does-not-shadow-explicit-profile"]
    assert not custom["constructed"] and custom["fault"]["type"] == "AnthropicError"
    fallback = rows["empty-selectors-fallback-profile"]
    assert fallback["constructed"] and fallback["fault"] is None
    assert fallback["requests"][0]["headers"]["authorization"] == "Bearer profile-token"
    empty = rows["federation-empty-literal"]
    assert empty["fault"] is None and empty["exchanges"][0]["body"]["assertion"] == ""
    rotation = rows["federation-literal-rotation"]
    assert [item["body"]["assertion"] for item in rotation["exchanges"]] == ["fixture.jwt.assertion", "rotated.literal.assertion"]
    missing = rows["federation-literal-disappearance"]
    assert missing["constructed"] and missing["fault"]["type"] == "AnthropicError"
    assert len(missing["requests"]) == len(missing["exchanges"]) == 1
