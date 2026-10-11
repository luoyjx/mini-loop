"""Observe actual benchmark construction, setup, filtering and raw-store ownership."""
from pathlib import Path


def benchmark_efficiency_filter_contracts(scratch: Path | str) -> dict:
    import asyncio
    import os
    from unittest.mock import patch
    from mini_loop.benchmark import BenchTask, run_arm
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, text
    from mini_loop.manager import SessionManager as SourceManager

    async def collect():
        rows = []
        recipes = [("off", "off", None), ("shadow", "shadow", ("read_file",)),
                   ("unfiltered", "enforce", None), ("empty", "enforce", ()),
                   ("readonly-tools", "enforce", ("glob", "read_file", "glob", "missing")),
                   ("recovery-only", "enforce", ("read_token_artifact",)),
                   ("unknown-only", "enforce", ("missing",))]
        for name, mode, selection in recipes:
            manager = None
            session = None
            before_names = None
            before_store = None
            setup_names = None
            setup_store = None
            observed_names = None

            class ObservedManager(SourceManager):
                def __init__(self, *args, **kwargs):
                    nonlocal manager
                    super().__init__(*args, **kwargs)
                    manager = self
                def create(self, **kwargs):
                    nonlocal session, before_names, before_store
                    session = super().create(**kwargs)
                    before_names = session.agent.tools.names()
                    before_store = session.agent.token_efficiency.raw_store
                    return session

            def setup(workspace):
                nonlocal setup_names, setup_store
                assert workspace == session.workspace
                setup_names = session.agent.tools.names()
                setup_store = session.agent.token_efficiency.raw_store

            def responder(request):
                nonlocal observed_names
                observed_names = [tool["name"] for tool in request["tools"]]
                return [text("done")], "end_turn"

            with patch.dict(os.environ, {}, clear=True), patch("mini_loop.manager.SessionManager", ObservedManager):
                settings = Settings(fake_llm=True, workspace_root=Path(scratch)/name,
                    skills_dir=Path(scratch)/"empty-skills", trajectory_enabled=False,
                    spill_dir=None, token_efficiency_mode=mode)
                result = await run_arm("test", settings, FakeAsyncAnthropic(responder=responder, thinking=False),
                    (BenchTask(name, "finish", lambda workspace, final: final == "done", setup=setup,
                               tool_names=selection),))
            after_store = session.agent.token_efficiency.raw_store
            rows.append(dict(name=name, mode=mode, selection=selection,
                before_names=before_names, setup_names=setup_names,
                after_names=session.agent.tools.names(), model_names=observed_names,
                before_store=before_store is not None, setup_same_store=setup_store is before_store,
                after_same_store=after_store is before_store, passed=result[0]["passed"]))
            await manager.stop()
            rows[-1]["closed"] = before_store is None or before_store.closed
        return dict(rows=rows)
    return asyncio.run(collect())
