"""Capture real Source posture selection and public health projection."""


def posture_contracts(scratch) -> dict:
    from pathlib import Path
    from unittest.mock import patch
    import os
    import asyncio
    from fastapi.testclient import TestClient
    from mini_loop.auth import NullAuth, TokenAuth
    from mini_loop.caching import NullCachePolicy
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.identity import posture
    from mini_loop.manager import SessionManager
    from mini_loop.sandbox import UnavailableSandbox
    from mini_loop.secrets import SecretRegistry, NullSecretRegistry
    from mini_loop.server import create_app
    from mini_loop.storage import NullStateStore, SQLiteStateStore
    from mini_loop.stuck import NullStuckDetector

    class BrokenStore(NullStateStore):
        def append_event(self, session_id, event):
            raise RuntimeError("fixture disk unavailable")

    names = ("default-empty", "default-live", "null-registry", "unregistered",
             "registered", "registered-case-sensitive", "unavailable-sandbox",
             "sqlite-auth", "broken-store", "live-overrides", "workflows",
             "full-settings-custom-manager", "owner-resources", "trajectories")
    environment = {"FIXTURE_API_KEY": "fixture-value", "fixture_token": "fixture-value",
                   "EMPTY_SECRET": "", "ORDINARY": "fixture-value"}
    rows = []
    for name in names:
        root = Path(scratch) / "posture" / name
        root.mkdir(parents=True, exist_ok=True)
        authenticated = name == "sqlite-auth"
        env = dict(environment)
        if authenticated:
            env["MINILOOP_API_TOKEN"] = "fixture-auth-token"
        with patch.dict(os.environ, env, clear=True):
            cfg = Settings(fake_llm=True, workspace_root=root / "workspaces",
                skills_dir=root / "skills", trajectory_enabled=name == "trajectories",
                user_resources_root=root / "resources" if name == "owner-resources" else None,
                enable_features=name == "full-settings-custom-manager")
            options = {}
            if name == "null-registry":
                options["secrets"] = NullSecretRegistry()
            elif name in ("unregistered", "registered", "registered-case-sensitive"):
                registry = SecretRegistry()
                if name == "registered":
                    # Names count as registered even when lazy values are unresolved.
                    registry.register("FIXTURE_API_KEY", lambda: None)
                    registry.register("fixture_token", "short")
                elif name == "registered-case-sensitive":
                    registry.register("FIXTURE_TOKEN", "fixture-value")
                options["secrets"] = registry
            elif name == "unavailable-sandbox":
                options["sandbox"] = UnavailableSandbox("fixture platform unavailable")
            elif name == "sqlite-auth":
                options["state_store"] = SQLiteStateStore(root / "state.db")
            elif name == "broken-store":
                options["state_store"] = BrokenStore()
            elif name == "workflows":
                options["enable_workflows"] = True
            manager = SessionManager(cfg, FakeAsyncAnthropic(), **options)
            auth = TokenAuth({"fixture-auth-token": "fixture-owner"}) if authenticated else NullAuth()
            try:
                resources_before = len(list((root / "resources").iterdir())) if name == "owner-resources" else None
                before = posture(manager, auth)
                session = None
                if name in ("default-live", "broken-store", "live-overrides"):
                    session = manager.create(owner="fixture-owner")
                if name == "broken-store":
                    asyncio.run(session.run("hello"))
                if name == "live-overrides":
                    session.agent.cache_policy = NullCachePolicy()
                    session.agent.stuck_detector = NullStuckDetector()
                    session.agent.secrets = SecretRegistry()
                    session.agent.sandbox = UnavailableSandbox("fixture live agent unavailable")
                direct = posture(manager, auth)
                count = len(manager.list())
                with TestClient(create_app(settings=cfg, manager=manager)) as client:
                    response = client.get("/healthz")
                    health = response.json()
                    for key in ("build", "pid", "started_at", "uptime_s"):
                        health.pop(key)
                    after = posture(manager, auth)
                    rows.append(dict(name=name, authenticated=authenticated,
                        before=before, direct=direct, status=response.status_code,
                        health=health, after=after, sessions_before=count,
                        sessions_after=len(manager.list()), resources_before=resources_before,
                        resources_after=len(list((root / "resources").iterdir())) if name == "owner-resources" else None))
            finally:
                store = options.get("state_store")
                if isinstance(store, SQLiteStateStore):
                    store.close()
    return dict(cases=rows, volatile_health_fields=["build", "pid", "started_at", "uptime_s"])
