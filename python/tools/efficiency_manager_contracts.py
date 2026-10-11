"""Capture real Manager efficiency defaults, session stores and lifecycle order."""

from __future__ import annotations

import asyncio
from dataclasses import asdict
from pathlib import Path

from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, tool
from mini_loop.manager import SessionManager
from mini_loop.registry import ToolRegistry
from mini_loop.token_efficiency import (
    ComponentDescriptor, MaskedRawArtifactStore, ObservationReduction,
    TokenEfficiencyRegistry,
)


def efficiency_manager_contracts(scratch: Path | str) -> dict:
    scratch = Path(scratch)

    def report(value):
        if value is None:
            return None
        result = value.as_dict()
        for item in result["components"]:
            item["elapsed_ms"] = 0
        return result

    def components(runtime):
        return dict(default_mode=runtime.default_mode.value,
                    observation=[dict(descriptor=asdict(b.descriptor), mode=None if b.mode is None else b.mode.value)
                                 for b in runtime.components.observation_reducers],
                    request=[dict(descriptor=asdict(b.descriptor), mode=None if b.mode is None else b.mode.value)
                             for b in runtime.components.request_optimizers],
                    response=[dict(descriptor=asdict(b.descriptor), mode=None if b.mode is None else b.mode.value)
                              for b in runtime.components.response_policies])

    async def scenario(name, *, mode="off", style="normal", persist=True,
                       supplied_catalog=False, injected=False, template=False,
                       shared=False, fail_initialize=False, fail_close=False, live=False):
        path = scratch / name
        stores, events = [], []
        template_store = None
        reducing = asyncio.Event()

        class Probe:
            descriptor = ComponentDescriptor(id="manager-efficiency-probe", version="1", stage="observation")

            async def reduce(self, observation, **_kwargs):
                if live:
                    events.append(dict(hook="reduce", content=observation.content))
                    reducing.set()
                    try:
                        await asyncio.Event().wait()
                    except asyncio.CancelledError:
                        events.append(dict(hook="reduce_cancelled"))
                        raise
                return ObservationReduction(observation.content)

            async def initialize(self, services=None):
                events.append(dict(hook="initialize", services_absent=services is None))
                if fail_initialize:
                    raise ValueError("private initializer diagnostic")

            async def close(self, deadline_seconds=None):
                events.append(dict(hook="close", stores_closed=all(store.closed for store in stores),
                                   deadline_absent=deadline_seconds is None))
                if fail_close:
                    raise RuntimeError("private close diagnostic")

        runtime = None
        shared_path = path / "shared"
        if injected:
            registry = TokenEfficiencyRegistry()
            registry.register_observation(Probe())
            if template:
                template_path = shared_path if shared else path / "foreign"
                template_path.mkdir(parents=True, exist_ok=True)
                template_store = MaskedRawArtifactStore(template_path)
                stores.append(template_store)
            runtime = registry.runtime(default_mode="enforce", raw_store=template_store)
        caller_catalog = ToolRegistry() if supplied_catalog else None
        manager = SessionManager(
            Settings(fake_llm=True, workspace_root=path / "workspaces", skills_dir=path / "empty-skills",
                     token_efficiency_mode=mode, token_efficiency_response_style=style,
                     token_efficiency_persist_raw=persist),
            FakeAsyncAnthropic(responder=(lambda _kwargs: ([tool("bash", command="printf 'masked authority'")], "tool_use")) if live else None),
            token_efficiency=runtime, tool_registry=caller_catalog,
            workspace_factory=(lambda _id: shared_path) if shared else None,
        )
        initial = components(manager.token_efficiency)
        caller_names = None if caller_catalog is None else caller_catalog.names()
        await manager.start()
        await manager.start()
        initialized = report(manager._token_efficiency_initialize_report)
        sessions = [manager.create(permission_mode=permission) for permission in ("interactive", "readonly", "interactive")]
        session_rows = []
        for session in sessions:
            agent = session.agent
            store = agent.token_efficiency.raw_store
            if store is not None and all(store is not existing for existing in stores):
                stores.append(store)
            pointer = None if store is None else store.put_masked("masked authority")
            session_rows.append(dict(permission=session.permission_mode,
                                     recovery_tool="read_token_artifact" in agent.tools,
                                     raw_store=store is not None,
                                     workspace_matches=store is not None and store.workspace == session.workspace.resolve(),
                                     template_store=store is not None and store is template_store,
                                     raw_content=None if pointer is None else store.get_masked(pointer.ref)))
        first_store = sessions[0].agent.token_efficiency.raw_store
        last_store = sessions[2].agent.token_efficiency.raw_store
        deleted = manager.delete(sessions[0].id, remove_workspace=False)
        if manager._cleanup_tasks:
            await asyncio.gather(*tuple(manager._cleanup_tasks))
        first_closed_after_delete = None if first_store is None else first_store.closed
        live_closed_after_delete = None if last_store is None else last_store.closed
        live_task = None
        if live:
            live_task = asyncio.create_task(sessions[2].run("diagnose"))
            try:
                await asyncio.wait_for(reducing.wait(), timeout=5)
            except BaseException:
                await manager.stop()
                await asyncio.gather(live_task, return_exceptions=True)
                raise
        await manager.stop()
        live_cancelled = None
        if live_task is not None:
            result = (await asyncio.gather(live_task, return_exceptions=True))[0]
            live_cancelled = isinstance(result, asyncio.CancelledError)
        await manager.stop()
        await manager.start()
        stopped_create = None
        try:
            manager.create()
        except RuntimeError as error:
            stopped_create = str(error)
        return dict(name=name, mode=mode, style=style, persist=persist, supplied_catalog=supplied_catalog,
                    injected=injected, template=template, shared=shared,
                    fail_initialize=fail_initialize, fail_close=fail_close, live=live,
                    live_drained=None if live_task is None else live_task.done(), live_cancelled=live_cancelled,
                    components=initial, caller_names=caller_names,
                    caller_catalog_preserved=caller_catalog is None or manager.tool_registry is caller_catalog,
                    sessions=session_rows, deleted=deleted,
                    stores_shared=first_store is not None and first_store is last_store,
                    first_closed_after_delete=first_closed_after_delete,
                    live_closed_after_delete=live_closed_after_delete,
                    all_stores_closed=all(store.closed for store in stores), events=events,
                    initialize=initialized, close=report(manager._token_efficiency_close_report),
                    initialize_error=manager._token_efficiency_initialize_error,
                    close_error=manager._token_efficiency_close_error,
                    store_close_errors=manager._token_efficiency_store_close_errors,
                    stopped_create=stopped_create)

    async def capture():
        rows = [await scenario(f"{mode}-{style}", mode=mode, style=style)
                for mode in ("off", "shadow", "enforce") for style in ("normal", "concise")]
        rows.extend([
            await scenario("caller-catalog", mode="enforce", supplied_catalog=True),
            await scenario("no-persistence", mode="enforce", persist=False),
            await scenario("injected", injected=True),
            await scenario("foreign-template", injected=True, template=True),
            await scenario("shared-template", injected=True, template=True, shared=True),
            await scenario("failed-initialize", injected=True, fail_initialize=True),
            await scenario("failed-close", injected=True, fail_close=True),
            await scenario("live-consumer-stop", injected=True, live=True),
        ])
        return rows

    return dict(rows=asyncio.run(capture()))
