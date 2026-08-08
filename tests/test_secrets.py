"""Secret handling -- offline, deterministic, no API key.

The regression that motivated this module: an agent runs `printenv`, and the
host's API key lands in four places at once -- the model transcript, the event
stream the console renders, the trajectory, and the durable state store. The
end-to-end test at the bottom asserts all four are clean.
"""

import asyncio
from pathlib import Path

from mini_loop.agent import Agent
from mini_loop.config import Settings
from mini_loop.fake_llm import FakeAsyncAnthropic, text, tool
from mini_loop.manager import SessionManager
from mini_loop.secrets import (
    MASK,
    MIN_MASKABLE_LENGTH,
    NullSecretRegistry,
    SecretRegistry,
)
from mini_loop.skills import SkillLoader
from mini_loop.storage import SQLiteStateStore

SKILLS_DIR = Path(__file__).resolve().parent.parent / "skills"
LONG = "sk-canary-0123456789abcdef"


def _settings(tmp_path, **over) -> Settings:
    base = dict(fake_llm=True, workspace_root=tmp_path / "ws", skills_dir=SKILLS_DIR)
    base.update(over)
    return Settings(**base)


# --- registration ----------------------------------------------------------

def test_from_environ_picks_credential_shaped_names():
    env = {
        "ANTHROPIC_API_KEY": LONG,
        "GITHUB_TOKEN": LONG + "b",
        "AWS_SECRET_ACCESS_KEY": LONG + "c",
        "PATH": "/usr/bin",
        "HOME": "/home/dev",
        "EDITOR": "vim",
    }
    registry = SecretRegistry.from_environ(environ=env)
    assert set(registry.names()) == {
        "ANTHROPIC_API_KEY", "GITHUB_TOKEN", "AWS_SECRET_ACCESS_KEY"
    }


def test_empty_values_are_not_registered():
    registry = SecretRegistry.from_environ(environ={"FOO_TOKEN": ""})
    assert registry.names() == ()


def test_extra_names_can_be_forced():
    registry = SecretRegistry.from_environ(
        environ={"DATABASE_URL": LONG}, extra_names=["DATABASE_URL"]
    )
    assert registry.names() == ("DATABASE_URL",)


# --- injection is narrow ---------------------------------------------------

def test_only_named_secrets_reach_a_command():
    registry = SecretRegistry.from_environ(
        environ={"ANTHROPIC_API_KEY": LONG, "GITHUB_TOKEN": LONG + "b"}
    )
    assert registry.env_for_command("curl -H \"$GITHUB_TOKEN\" x") == {
        "GITHUB_TOKEN": LONG + "b"
    }
    assert registry.env_for_command("printenv") == {}
    assert registry.env_for_command("ls -la") == {}


def test_name_matching_is_case_insensitive():
    registry = SecretRegistry.from_environ(environ={"GITHUB_TOKEN": LONG})
    assert registry.find_in_text("echo $github_token") == {"GITHUB_TOKEN"}


def test_scrub_env_removes_every_registered_name():
    env = {"ANTHROPIC_API_KEY": LONG, "PATH": "/usr/bin"}
    registry = SecretRegistry.from_environ(environ=env)
    scrubbed = registry.scrub_env(env)
    assert "ANTHROPIC_API_KEY" not in scrubbed
    assert scrubbed["PATH"] == "/usr/bin"


# --- masking is wide -------------------------------------------------------

def test_masking_covers_values_the_command_never_named():
    """Upstream's lesson: a token can arrive embedded in a URL."""
    registry = SecretRegistry.from_environ(environ={"GITHUB_TOKEN": LONG})
    out = registry.mask(f"remote: https://x:{LONG}@github.com/o/r.git")
    assert LONG not in out
    assert MASK in out


def test_rotation_keeps_masking_the_previously_handed_out_value():
    """Re-resolving would let the old value start leaking again."""
    values = [LONG, "sk-rotated-fedcba9876543210"]
    registry = SecretRegistry()
    registry.register("API_KEY", lambda: values[0])

    assert registry.mask(f"leak {LONG}") == f"leak {MASK}"
    values[0] = values[1]  # secret rotates underneath us
    assert registry.mask(f"leak {LONG}") == f"leak {MASK}", "old value stopped masking"


def test_short_values_are_reported_not_masked():
    """Masking a 3-char value would shred unrelated output."""
    registry = SecretRegistry()
    registry.register("TINY", "abc")
    text_out = registry.mask("abc appears in abcdef and fabric")
    assert text_out == "abc appears in abcdef and fabric"
    assert registry.short_values() == ("TINY",)


def test_longest_value_is_masked_first():
    """A secret containing another must not be left half-masked."""
    inner = "0123456789abcdef"
    outer = "sk-" + inner + "-tail"
    registry = SecretRegistry()
    registry.register("INNER", inner)
    registry.register("OUTER", outer)
    assert registry.mask(f"value={outer}") == f"value={MASK}"


def test_failed_resolution_does_not_raise():
    registry = SecretRegistry()

    def boom():
        raise RuntimeError("vault unreachable")

    registry.register("BROKEN", boom)
    assert registry.mask("nothing to do here") == "nothing to do here"
    assert registry.env_for_command("echo $BROKEN") == {}


def test_non_string_payloads_pass_through():
    registry = SecretRegistry.from_environ(environ={"API_KEY": LONG})
    assert registry.mask(None) is None
    assert registry.mask(42) == 42


def test_null_registry_is_inert():
    registry = NullSecretRegistry()
    assert registry.mask(LONG) == LONG
    assert registry.names() == ()
    assert registry.env_for_command("printenv") == {}


def test_min_length_constant_is_enforced():
    registry = SecretRegistry()
    registry.register("EDGE", "x" * (MIN_MASKABLE_LENGTH - 1))
    assert MASK not in registry.mask("x" * (MIN_MASKABLE_LENGTH - 1))
    registry.register("OK", "y" * MIN_MASKABLE_LENGTH)
    assert MASK in registry.mask("y" * MIN_MASKABLE_LENGTH)


# --- the bash boundary -----------------------------------------------------

def test_bash_cannot_read_an_unreferenced_secret(tmp_path):
    from mini_loop.tools import Toolset

    env_name = "MINILOOP_TEST_SECRET"
    registry = SecretRegistry.from_environ(
        environ={env_name: LONG}, extra_names=[env_name]
    )
    toolset = Toolset(tmp_path / "ws", secrets=registry)

    import os

    os.environ[env_name] = LONG
    try:
        # A command that does not name the secret never sees it. Note the grep
        # pattern deliberately avoids the variable's *name*: spelling it would
        # itself trigger narrow injection, which is exactly the contract.
        blind = toolset.run_bash("printenv | grep -c 'sk-canary' || true")
        assert LONG not in blind
        assert blind.strip().startswith("0")

        # Naming it *does* hand it over -- that is the point, so legitimate use
        # works. The second layer is that the value is masked on the way out,
        # so it still never reaches the transcript.
        named = toolset.run_bash(f'echo "${env_name}"')
        assert LONG not in named
        assert MASK in named
    finally:
        os.environ.pop(env_name, None)


# --- end to end: all four sinks -------------------------------------------

def test_a_leaked_key_reaches_no_sink(tmp_path):
    canary = "sk-CANARY-9f3a2b1c-do-not-log"
    env_name = "MINILOOP_CANARY_KEY"

    def responder(kwargs: dict):
        if not kwargs.get("tools"):
            return [text("[summary]")], "end_turn"
        last = kwargs["messages"][-1]
        if isinstance(last.get("content"), str):
            return [tool("bash", _id="t1", command="printenv")], "tool_use"
        return [text("done")], "end_turn"

    import os

    os.environ[env_name] = canary
    events: list[dict] = []
    store = SQLiteStateStore(tmp_path / "state.db")
    try:
        manager = SessionManager(
            _settings(tmp_path),
            FakeAsyncAnthropic(responder=responder),
            state_store=store,
            event_sink=lambda e: events.append(e),
            secrets=SecretRegistry.from_environ(extra_names=[env_name]),
        )
        session = manager.create()
        asyncio.run(session.run("dump the environment"))
        asyncio.run(manager.stop())

        sinks = {
            "transcript": str(session.agent.messages),
            "events": str(events),
            "stored messages": str(store.load_messages(session.id)),
            "stored events": str(store.load_events(session.id)),
        }
        leaked = [name for name, blob in sinks.items() if canary in blob]
        assert not leaked, f"canary leaked into: {leaked}"
    finally:
        os.environ.pop(env_name, None)
        store.close()


def test_masking_is_off_unless_asked_for(tmp_path):
    """Default stays inert: turning this on is a deployment decision."""
    agent = Agent(
        client=FakeAsyncAnthropic(),
        settings=_settings(tmp_path),
        workspace=tmp_path / "ws",
        skills=SkillLoader(SKILLS_DIR),
    )
    assert isinstance(agent.secrets, NullSecretRegistry)
    assert agent.toolset.secrets is agent.secrets


# --- the other direction: secrets the *model* writes into arguments --------

def test_a_secret_in_a_tool_argument_is_masked_in_every_record(tmp_path):
    """Masking outputs covered one direction only.

    Tool arguments are model-generated: a model that read a credential can put
    it straight into a command. Before this, that reached the event stream, the
    console, the trajectory and the durable tables -- four sinks out of five,
    with only `tool_result` masked.
    """
    canary = "sk-argcanary-0123456789abcdef"
    env_name = "MINILOOP_ARG_CANARY"

    def responder(kwargs: dict):
        if not kwargs.get("tools"):
            return [text("[summary]")], "end_turn"
        last = kwargs["messages"][-1]
        if isinstance(last.get("content"), str):
            return (
                [tool("bash", _id="t1", command=f'echo "auth {canary}" > /dev/null')],
                "tool_use",
            )
        return [text("done")], "end_turn"

    import os

    os.environ[env_name] = canary
    events: list[dict] = []
    store = SQLiteStateStore(tmp_path / "state.db")
    try:
        manager = SessionManager(
            _settings(tmp_path),
            FakeAsyncAnthropic(responder=responder),
            state_store=store,
            event_sink=lambda e: events.append(e),
            secrets=SecretRegistry.from_environ(extra_names=[env_name]),
        )
        session = manager.create()
        asyncio.run(session.run("call the api"))
        asyncio.run(manager.stop())

        recorded = {
            "tool_use events": str([e for e in events if e.get("type") == "tool_use"]),
            "tool_result events": str([e for e in events if e.get("type") == "tool_result"]),
            "durable messages": str(store.load_messages(session.id)),
            "durable events": str(store.load_events(session.id)),
        }
        leaked = [name for name, blob in recorded.items() if canary in blob]
        assert not leaked, f"argument canary reached: {leaked}"
        assert MASK in recorded["tool_use events"]
    finally:
        os.environ.pop(env_name, None)
        store.close()


def test_the_executed_command_keeps_the_real_value(tmp_path):
    """Masking the recorded copy must not break the command."""
    from mini_loop.registry import ToolCall

    registry = SecretRegistry()
    registry.register("TOK", LONG)
    seen = {}

    async def capture(_ctx, value=""):
        seen["value"] = value
        return "ok"

    from mini_loop.registry import Hooks, Tool, ToolRegistry

    tools = ToolRegistry()
    tools.register(
        Tool("capture", "capture", {"type": "object", "properties": {"value": {"type": "string"}}}, capture)
    )
    agent = Agent(
        client=FakeAsyncAnthropic(),
        settings=_settings(tmp_path),
        workspace=tmp_path / "ws",
        skills=SkillLoader(SKILLS_DIR),
        tools=tools,
        hooks=Hooks(),
        secrets=registry,
    )
    asyncio.run(agent._exec_tool(ToolCall("capture", {"value": LONG}, "t1")))
    assert seen["value"] == LONG, "the tool received a masked argument"


def test_mask_payload_walks_nested_structures():
    registry = SecretRegistry()
    registry.register("TOK", LONG)
    payload = {"headers": {"Authorization": f"Bearer {LONG}"}, "args": [LONG, "safe"]}
    masked = registry.mask_payload(payload)
    assert LONG not in str(masked)
    assert masked["args"][1] == "safe"
    assert masked["headers"]["Authorization"] == f"Bearer {MASK}"


def test_mask_payload_is_inert_on_the_null_registry():
    payload = {"command": LONG}
    assert NullSecretRegistry().mask_payload(payload) == payload


def test_mask_payload_masks_dict_keys_not_only_values():
    """A credential-listing tool that keys a map *by* the credential
    (`{"<token>": {...}}` -- a real API shape) otherwise wrote the secret into
    the trajectory and the durable tables as a key, which the value-only mask
    walked straight past."""
    registry = SecretRegistry()
    registry.register("TOK", LONG)

    keyed = {LONG: {"scopes": ["read"]}, f"prefix-{LONG}-suffix": 1}
    masked = registry.mask_payload(keyed)
    assert LONG not in str(masked)
    assert MASK in masked                       # the bare-secret key is masked
    assert f"prefix-{MASK}-suffix" in masked    # an embedded secret keeps its surround

    # Non-string keys are untouched, and a payload with no secret is unchanged.
    mixed = registry.mask_payload({1: "a", ("t",): "b"})
    assert 1 in mixed and ("t",) in mixed
    assert registry.mask_payload({"plain": {"nested": [1, "ok"]}}) == {
        "plain": {"nested": [1, "ok"]}
    }
