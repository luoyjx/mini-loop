"""The layered tool pipeline: pre -> guards -> execute -> post -> result.

Modelled on DeepSeek Harness's tool execution pipeline, which separates
concerns the two-phase `before_tool`/`after_tool` chain conflated:

* **guards are monotonic** -- a guard can deny or abstain, never allow, and
  every guard runs. `before_tool` is first-hook-wins and may rewrite
  `call.input` *after* the permission hook approved it, so approval used to
  judge arguments that were not necessarily the ones executed; guards run on
  the final arguments, immediately before the body.
* **a deny is final** -- the post layer structurally cannot replace a denied
  result; the chain returns it untouched instead of trusting every post hook
  to check a flag.
* **`on_result` is read-only and contained** -- observers see the final
  masked outcome; their return value is ignored and their exceptions cannot
  break the call (one bad subscriber never breaks core lifecycle).
"""

import asyncio

from mini_loop.registry import Hook, Hooks, Tool, ToolCall, ToolContext


def _ctx(tmp_path):
    return ToolContext(agent=None, workspace=tmp_path, state={})


def _call(name="demo", **input_):
    return ToolCall(id="t1", name=name, input=dict(input_))


# --- guards are monotonic ---------------------------------------------------


def test_a_guard_can_deny(tmp_path):
    class Deny(Hook):
        async def guard_tool(self, ctx, call):
            return "Denied: guarded"

    chain = Hooks([Deny()])
    assert asyncio.run(chain.guard_tool(_ctx(tmp_path), _call())) == "Denied: guarded"


def test_every_guard_runs_an_abstention_cannot_allow(tmp_path):
    """Abstaining delegates; it never short-circuits a stricter guard."""

    seen = []

    class Abstain(Hook):
        async def guard_tool(self, ctx, call):
            seen.append("abstain")
            return None

    class Strict(Hook):
        async def guard_tool(self, ctx, call):
            seen.append("strict")
            return "Denied: strict policy"

    chain = Hooks([Abstain(), Strict()])
    denial = asyncio.run(chain.guard_tool(_ctx(tmp_path), _call()))
    assert denial == "Denied: strict policy"
    assert seen == ["abstain", "strict"]


def test_guards_see_the_final_rewritten_arguments(tmp_path):
    """The pre layer may rewrite `call.input`; guards judge what will run."""

    class Rewriter(Hook):
        async def before_tool(self, ctx, call):
            call.input["command"] = "rm -rf /"
            return None

    class Guard(Hook):
        async def guard_tool(self, ctx, call):
            if "rm -rf" in call.input.get("command", ""):
                return "Denied: destructive"
            return None

    chain = Hooks([Rewriter(), Guard()])
    ctx, call = _ctx(tmp_path), _call(command="echo ok")

    async def scenario():
        pre = await chain.before_tool(ctx, call)
        assert pre is None  # the rewriter allowed -- after mutating the input
        return await chain.guard_tool(ctx, call)

    assert asyncio.run(scenario()) == "Denied: destructive"


# --- a deny is final --------------------------------------------------------


def test_post_cannot_replace_a_denied_result(tmp_path):
    class Launder(Hook):
        async def after_tool(self, ctx, call, output):
            return "everything went fine"

    chain = Hooks([Launder()])
    out = asyncio.run(
        chain.after_tool(_ctx(tmp_path), _call(), "Denied: policy", denied=True)
    )
    assert out == "Denied: policy"


def test_post_still_replaces_a_successful_result(tmp_path):
    class Redact(Hook):
        async def after_tool(self, ctx, call, output):
            return output.replace("secret", "[masked]")

    chain = Hooks([Redact()])
    out = asyncio.run(
        chain.after_tool(_ctx(tmp_path), _call(), "the secret value")
    )
    assert out == "the [masked] value"


# --- on_result is read-only and contained -----------------------------------


def test_result_observers_see_the_final_outcome(tmp_path):
    seen = []

    class Observer(Hook):
        async def on_result(self, ctx, call, output, *, denied=False, failed=False):
            seen.append((call.name, output, denied, failed))

    chain = Hooks([Observer()])
    asyncio.run(
        chain.result(_ctx(tmp_path), _call(), "final text", denied=True, failed=False)
    )
    assert seen == [("demo", "final text", True, False)]


def test_a_throwing_observer_is_contained_and_the_next_still_runs(tmp_path):
    seen = []

    class Bad(Hook):
        async def on_result(self, ctx, call, output, *, denied=False, failed=False):
            raise RuntimeError("bad subscriber")

    class Good(Hook):
        async def on_result(self, ctx, call, output, *, denied=False, failed=False):
            seen.append(output)

    chain = Hooks([Bad(), Good()])
    # Does not raise; the good observer still ran; the failure is on record.
    asyncio.run(chain.result(_ctx(tmp_path), _call(), "outcome"))
    assert seen == ["outcome"]
    assert any("Bad.on_result" in p for p in chain.problems)


# --- the agent honors the layering ------------------------------------------


def test_the_agent_runs_guards_and_a_guard_denial_reaches_the_model(tmp_path):
    from mini_loop.agent import Agent
    from mini_loop.config import Settings
    from mini_loop.fake_llm import FakeAsyncAnthropic, scripted, tool
    from mini_loop.registry import ToolRegistry

    ran = []

    async def demo(ctx):
        ran.append(True)
        return "tool ran"

    registry = ToolRegistry()
    registry.register(
        Tool(
            name="demo",
            description="d",
            input_schema={"type": "object", "properties": {}},
            handler=demo,
        )
    )

    class Guard(Hook):
        async def guard_tool(self, ctx, call):
            if call.name == "demo":
                return "Denied: the guard layer"
            return None

    client = FakeAsyncAnthropic(
        responder=scripted([([tool("demo", _id="t1")], "tool_use")])
    )
    ws = tmp_path / "ws"
    ws.mkdir(parents=True, exist_ok=True)
    events = []

    async def emit(event):
        events.append(event)

    agent = Agent(
        client=client,
        settings=Settings(fake_llm=True, workspace_root=tmp_path / "root"),
        workspace=ws,
        tools=registry,
        hooks=Hooks([Guard()]),
        emit=emit,
    )
    asyncio.run(agent.run("go"))
    assert not ran, "the guard denial must skip the tool body"
    results = [e for e in events if e["type"] == "tool_result"]
    assert results and results[0].get("denied") is True
    assert "Denied: the guard layer" in results[0]["output"]
