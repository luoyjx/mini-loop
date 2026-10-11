"""Actual comprehensive registry and Manager composition, without model calls."""


def feature_bundle_contracts(scratch) -> dict:
    import asyncio
    import json
    from pathlib import Path
    from types import SimpleNamespace
    from mini_loop.builtins import default_registry, full_registry, default_injectors
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic
    from mini_loop.manager import SessionManager

    def catalogue(registry):
        return [dict(
            name=name, schema=registry.get(name).schema,
            readonly=registry.get(name).readonly,
            parallel_safe=registry.get(name).parallel_safe,
            risk=registry.get(name).risk,
            capabilities=sorted(registry.get(name).capabilities),
            modes=[registry.get(name).execution_mode(SimpleNamespace(
                input={"run_in_background": value})) for value in (False, True)],
        ) for name in registry.names()]

    groups = ("tasks", "background", "memory", "cron", "plan", "goals",
              "diagnostics", "session_query", "teams", "worktrees", "mcp",
              "self_audit")
    registry_cases = [dict(name="default", tools=catalogue(default_registry())),
                      dict(name="full", tools=catalogue(full_registry()))]
    registry_cases.extend(dict(name=f"without-{group}",
        tools=catalogue(full_registry(**{group: False}))) for group in groups)

    async def manager_case(name, *, enabled=False, custom_tools=False,
                           custom_injectors=False, workflows=False, ast=False,
                           decisions="off"):
        root = Path(scratch) / "feature-bundle" / name
        root.mkdir(parents=True, exist_ok=True)
        cfg = Settings(fake_llm=True, workspace_root=root / "workspaces",
            skills_dir=root / "skills", trajectory_enabled=False,
            ast_outline_enabled=ast,
            ast_outline_binary=str((root / "inventory-only-ast").resolve()),
            ast_outline_sha256="0" * 64,
            decision_mode=decisions)
        options = dict(enable_features=enabled, enable_workflows=workflows)
        if custom_tools:
            options["tool_registry"] = default_registry()
        if custom_injectors:
            options["injectors"] = []
        manager = SessionManager(cfg, FakeAsyncAnthropic(), **options)
        try:
            session = manager.create(owner="fixture")
            return dict(name=name, tools=catalogue(session.agent.tools),
                manager_injectors=[f.__name__ for f in manager.injectors],
                agent_injectors=[f.__name__ for f in session.agent.injectors],
                state_keys=sorted(session.agent.state),
                workflows=manager.enable_workflows,
                owner=session.owner, mode=session.permission_mode)
        finally:
            await manager.stop()

    async def managers():
        return [
            await manager_case("off"),
            await manager_case("full", enabled=True),
            await manager_case("full-custom-tools", enabled=True, custom_tools=True),
            await manager_case("full-custom-injectors", enabled=True, custom_injectors=True),
            await manager_case("full-workflows", enabled=True, workflows=True),
            await manager_case("full-ast", enabled=True, ast=True),
            await manager_case("full-decisions", enabled=True, decisions="llm"),
            await manager_case("full-independent", enabled=True, workflows=True,
                               ast=True, decisions="llm"),
        ]

    manager_cases = asyncio.run(managers())
    # Keep one concrete definition per distinct schema/traits/classifier profile.
    # Cases retain ordered references; repeated full schemas need not inflate the
    # fixture. A tool's default and background-aware variants stay distinct.
    definitions, identities, variants = [], {}, {}
    for case in [*registry_cases, *manager_cases]:
        references = []
        for tool in case["tools"]:
            key = json.dumps(tool, ensure_ascii=False, sort_keys=True)
            if key not in identities:
                name = tool["name"]
                variants[name] = variants.get(name, 0) + 1
                identity = name if variants[name] == 1 else f"{name}:variant-{variants[name]}"
                identities[key] = identity
                definitions.append(dict(definition_id=identity, **tool))
            references.append(identities[key])
        case["tools"] = references
    return dict(definitions=definitions, registries=registry_cases, managers=manager_cases,
        default_injectors=[f.__name__ for f in default_injectors()],
        no_default_injectors=[f.__name__ for f in default_injectors(background=False, teams=False)])
