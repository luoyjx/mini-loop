"""Actual Source profile/federation discovery with isolated files and HTTP."""

import hashlib
import inspect
import json
import os
from unittest.mock import patch

import anthropic
import httpx
from anthropic.lib.credentials import _chain, _providers, _workload, _cache, _auth
from mini_loop.config import Settings
from mini_loop.providers import provider_for


async def provider_discovery_cases(scratch):
    real_sdk, real_http = anthropic.AsyncAnthropic, httpx.Client
    oauth = {"authentication": {"type": "user_oauth"}}
    credentials = {"access_token": "profile-token", "expires_at": 2000000000}
    federation = {"ANTHROPIC_IDENTITY_TOKEN": "fixture.jwt.assertion",
        "ANTHROPIC_FEDERATION_RULE_ID": "fixture-rule", "ANTHROPIC_ORGANIZATION_ID": "fixture-org"}
    profiles = [
        dict(name="default-profile", config=oauth, credentials=credentials),
        dict(name="active-pointer", profile="dev", pointer="dev", config=oauth, credentials=credentials),
        dict(name="explicit-profile", profile="dev", environment={"ANTHROPIC_PROFILE": "dev"}, config=oauth, credentials=credentials),
        dict(name="explicit-config-dir-missing", explicit_dir=True),
        dict(name="missing-explicit-profile", environment={"ANTHROPIC_PROFILE": "missing"}),
        dict(name="missing-active-profile", pointer="missing"),
        dict(name="invalid-profile-name", environment={"ANTHROPIC_PROFILE": "../escape"}),
        dict(name="corrupt-fallback-profile", config="{"),
        dict(name="corrupt-explicit-profile", explicit_dir=True, config="{"),
        dict(name="unknown-auth-kind", config={"authentication": {"type": "future"}}, credentials=credentials),
        dict(name="missing-profile-token", config=oauth, credentials={}),
        dict(name="world-readable-profile", config=oauth, credentials=credentials, mode=0o644),
        dict(name="symlink-profile-secret", config=oauth, credentials=credentials, symlink=True),
        dict(name="profile-endpoint-workspace", config={**oauth, "base_url": "https://profile.invalid", "workspace_id": "profile-workspace"}, credentials=credentials),
        dict(name="environment-endpoint-wins", environment={"ANTHROPIC_BASE_URL": "https://environment.invalid"}, config={**oauth, "base_url": "https://profile.invalid"}, credentials=credentials),
        dict(name="static-key-shadows-profile", environment={"ANTHROPIC_API_KEY": "env-key", "ANTHROPIC_PROFILE": "missing"}),
        dict(name="federation-environment", environment=federation),
        dict(name="federation-before-fallback", environment=federation, config=oauth, credentials=credentials),
        dict(name="explicit-profile-before-federation", profile="dev", environment={**federation, "ANTHROPIC_PROFILE": "dev"}, config=oauth, credentials=credentials),
        dict(name="federation-incomplete", environment={"ANTHROPIC_IDENTITY_TOKEN": "fixture.jwt.assertion"}),
        dict(name="federation-file-before-literal", environment=federation, identity_file="file.jwt.assertion\n"),
        dict(name="federation-refuses-http", environment={**federation, "ANTHROPIC_BASE_URL": "http://remote.invalid"}),
        dict(name="federation-exchange-failure", environment=federation, token_status=401),
        dict(name="oauth-refresh", config={"authentication": {"type": "user_oauth", "client_id": "fixture-client"}}, credentials={"access_token": "expired-token", "refresh_token": "fixture-refresh", "expires_at": 1699990000}),
        dict(name="federation-model-401", environment=federation, model_statuses=[401, 200]),
        dict(name="federation-file-rotation", environment=federation, identity_file="file.jwt.assertion", advance_clock=3600, rotate_identity="rotated.jwt.assertion"),
    ]
    rows = []
    for spec in profiles:
        root = scratch / spec["name"]
        home = root / "home"
        directory = home / ".config" / "anthropic"
        (directory / "configs").mkdir(parents=True)
        (directory / "credentials").mkdir()
        profile = spec.get("profile", "default")
        if "config" in spec:
            value = spec["config"]
            (directory / "configs" / f"{profile}.json").write_text(value if isinstance(value, str) else json.dumps(value))
        if "credentials" in spec:
            path = directory / "credentials" / f"{profile}.json"
            if spec.get("symlink"):
                target = directory / "credentials" / "secret-target.json"
                target.write_text(json.dumps(spec["credentials"]))
                target.chmod(0o600)
                path.symlink_to(target)
            else:
                path.write_text(json.dumps(spec["credentials"]))
                path.chmod(spec.get("mode", 0o600))
        if "pointer" in spec:
            (directory / "active_config").write_text(spec["pointer"])
        environment = dict(spec.get("environment", {}))
        if spec.get("explicit_dir"):
            environment["ANTHROPIC_CONFIG_DIR"] = str(directory)
        if "identity_file" in spec:
            identity = root / "identity.jwt"
            identity.write_text(spec["identity_file"])
            environment["ANTHROPIC_IDENTITY_TOKEN_FILE"] = str(identity)
        requests, exchanges, constructors, texts = [], [], [], []
        fault, constructed = None, False
        clock = [1700000000]
        credentials_client = real_http(transport=httpx.MockTransport(lambda request: exchange(request)))

        def exchange(request):
            exchanges.append(dict(url=str(request.url), body=json.loads(request.content),
                beta=request.headers.get("anthropic-beta")))
            if spec.get("token_status"):
                return httpx.Response(spec["token_status"], headers={"request-id": "fixture-exchange"},
                    json={"error": "invalid_grant", "access_token": "must-redact"})
            return httpx.Response(200, json=dict(access_token="exchanged-token", token_type="Bearer", expires_in=3600))

        async def transport(request):
            requests.append(dict(url=str(request.url), headers={name: request.headers[name] for name in
                ("x-api-key", "authorization", "anthropic-workspace-id", "anthropic-beta") if name in request.headers}))
            statuses = spec.get("model_statuses", [])
            if len(requests) <= len(statuses) and statuses[len(requests)-1] != 200:
                if spec.get("rotate_identity"):
                    (root / "identity.jwt").write_text(spec["rotate_identity"])
                return httpx.Response(statuses[len(requests)-1], json={"error": {"type": "authentication_error", "message": "revoked fixture token"}})
            return httpx.Response(200, json=dict(id="fixture", type="message", role="assistant", model="fixture-model",
                content=[dict(type="text", text="ok")], stop_reason="end_turn", stop_sequence=None,
                usage=dict(input_tokens=1, output_tokens=1)))

        def construct(**kwargs):
            constructors.append(kwargs)
            return real_sdk(**kwargs, max_retries=0, http_client=httpx.AsyncClient(transport=httpx.MockTransport(transport)))

        client = None
        with patch.dict(os.environ, environment, clear=True), patch("pathlib.Path.home", return_value=home), \
                patch("time.time", side_effect=lambda: clock[0]), \
                patch("anthropic.AsyncAnthropic", side_effect=construct), \
                patch("anthropic._client.TokenCache", side_effect=lambda provider: _cache.TokenCache(provider, time_source=lambda: clock[0])), \
                patch("httpx.Client", return_value=credentials_client):
            try:
                source = provider_for(Settings())
                client = source.create_client()
                constructed = True
                for _ in range(2):
                    reply = await client.messages.create(model="fixture-model", max_tokens=8,
                        messages=[dict(role="user", content="hello")])
                    texts.append(reply.content[0].text)
                    if len(texts) == 1 and spec.get("advance_clock"):
                        clock[0] += spec["advance_clock"]
                        if spec.get("rotate_identity"):
                            (root / "identity.jwt").write_text(spec["rotate_identity"])
            except Exception as exc:
                fault = dict(type=type(exc).__name__, message=str(exc).replace(str(scratch), "<scratch>"))
            finally:
                if client is not None:
                    await client.close()
                credentials_client.close()
        credentials_after = None
        credentials_mode = None
        if spec["name"] == "oauth-refresh":
            path = directory / "credentials" / f"{profile}.json"
            credentials_after = json.loads(path.read_text())
            credentials_mode = path.stat().st_mode & 0o777
        # Normalize only the isolated absolute origin for deterministic recipes.
        normalized = json.loads(json.dumps(dict(spec=spec, environment=environment, constructed=constructed,
            constructors=constructors, requests=requests, exchanges=exchanges, texts=texts, fault=fault,
            credentials_after=credentials_after, credentials_mode=credentials_mode)).replace(str(scratch), "<scratch>"))
        rows.append(normalized)
    return rows


def discovery_source_hashes():
    symbols = [_chain.default_credentials, _chain._build_federation_result,
        _providers.CredentialsFile._load_config, _providers.CredentialsFile._read_credentials,
        _providers.CredentialsFile._call_user_oauth, _workload.WorkloadIdentityCredentials.__call__,
        _cache.TokenCache.get_token, _cache.TokenCache.invalidate,
        _auth.AccessTokenAuth._apply, _auth.AccessTokenAuth.async_auth_flow]
    return {symbol.__qualname__: hashlib.sha256(inspect.getsource(symbol).encode()).hexdigest() for symbol in symbols}
