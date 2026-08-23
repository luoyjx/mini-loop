"""One contract, every provider (Pi P0-1: conformance before adoption).

The same checks run against each provider: the client exposes
`.messages.create`, a reply carries content, a stop reason, usage counts,
and the response's own model claim. The fake runs always; the real
endpoint runs only under MINILOOP_REAL_PROVIDER_TESTS=1 (network and
credits are operator decisions, never CI defaults) -- when it runs, it is
the round-206 measurement as a repeatable test: the served model is
whatever the endpoint answers with, not necessarily what was asked for.

`describe()` is the audit surface and must never carry a credential.
"""

import asyncio
import os

import pytest

from mini_loop.config import Settings, build_client
from mini_loop.providers import (
    AnthropicCompatibleProvider,
    FakeProvider,
    ModelProvider,
    provider_for,
)


async def _conformance(client, model: str) -> None:
    reply = await client.messages.create(
        model=model, max_tokens=32,
        messages=[{"role": "user", "content": "Reply with exactly: PROVIDER-OK"}],
    )
    assert reply.content, "a reply carries content blocks"
    assert getattr(reply, "stop_reason", None), "a reply names its stop reason"
    usage = getattr(reply, "usage", None)
    assert usage is not None and usage.input_tokens > 0
    assert getattr(reply, "model", None), (
        "the response names the model that served it -- the identity "
        "authority served_model records"
    )


def test_the_fake_provider_conforms():
    provider = FakeProvider()
    assert isinstance(provider, ModelProvider)
    asyncio.run(_conformance(provider.create_client(), "claude-sonnet-4-6"))


@pytest.mark.skipif(
    not os.getenv("MINILOOP_REAL_PROVIDER_TESTS"),
    reason="real-endpoint conformance is operator-gated "
           "(MINILOOP_REAL_PROVIDER_TESTS=1): network + credits",
)
def test_the_configured_real_provider_conforms():
    settings = Settings()  # .env supplies base_url/key; may be any endpoint
    provider = provider_for(settings)
    assert isinstance(provider, ModelProvider)
    asyncio.run(_conformance(provider.create_client(), settings.model))


def test_provider_for_respects_the_fake_flag(tmp_path):
    fake = provider_for(Settings(fake_llm=True, workspace_root=tmp_path))
    assert isinstance(fake, FakeProvider)
    real = provider_for(Settings(fake_llm=False, workspace_root=tmp_path,
                                 api_key="k", base_url=None))
    assert isinstance(real, AnthropicCompatibleProvider)


def test_build_client_delegates_to_the_seam(tmp_path):
    client = build_client(Settings(fake_llm=True, workspace_root=tmp_path))
    from mini_loop.fake_llm import FakeAsyncAnthropic

    assert isinstance(client, FakeAsyncAnthropic)


def test_describe_never_carries_a_credential():
    secret = "sk-super-secret-value-1234567890"
    provider = AnthropicCompatibleProvider(
        base_url="https://api.deepseek.com/anthropic", api_key=secret,
    )
    rendered = str(provider.describe())
    assert secret not in rendered
    assert provider.describe()["credential"] == "<set>"
    assert "deepseek" in provider.describe()["endpoint"]


@pytest.mark.skipif(
    not os.getenv("MINILOOP_REAL_PROVIDER_TESTS"),
    reason="real-endpoint streaming conformance is operator-gated",
)
def test_the_streaming_path_conforms_against_the_real_endpoint():
    """207 validated the non-streaming path; StreamingTransport is a
    separate code path (content_block_delta events, get_final_message,
    partial-text bookkeeping) with its own assumptions. Probed against
    DeepSeek (round 218): no divergence -- served model, stop reason,
    usage, and the completed-stream partial-clear all hold. Kept as a
    gated test so the claim is executable, not a one-off."""
    import asyncio

    from mini_loop.config import Settings, build_client
    from mini_loop.transport import StreamingTransport

    settings = Settings()
    client = build_client(settings)

    class _Stub:
        def __init__(self):
            self.client = client
            self.streamed_text = ""
            self._last_stream_id = None
            self.semaphore = asyncio.Semaphore(1)
            self.secrets = type("S", (), {"mask": staticmethod(lambda t: t)})()

        async def _send(self, *a, **k):
            pass

    async def run():
        stub = _Stub()
        final = await StreamingTransport().send(stub, {
            "model": settings.model, "max_tokens": 64,
            "messages": [{"role": "user",
                          "content": "Reply with exactly: STREAM-OK"}],
        })
        assert final.content and getattr(final, "stop_reason", None)
        assert getattr(final, "model", None), "the stream names its served model"
        assert getattr(final.usage, "output_tokens", None) is not None
        # A completed stream leaves no partial to re-record (round-and-a-half
        # of interrupted-turn bookkeeping depends on this).
        assert stub.streamed_text == ""

    asyncio.run(run())


@pytest.mark.skipif(
    not os.getenv("MINILOOP_REAL_PROVIDER_TESTS"),
    reason="real-endpoint served-model end-to-end is operator-gated",
)
def test_served_model_survives_a_real_streaming_turn():
    """Round 206 recorded served_model (identity honesty) with the fake;
    round 218 checked the streaming transport surfaces .model. This pins
    the END TO END against a real aliasing endpoint: a full streaming
    agent turn's model_end event carries the model that actually served
    (measured: deepseek-v4-flash for a claude-sonnet request), not the
    name that was asked for. If a compatible endpoint's streamed final
    message dropped .model, every streaming turn would record None."""
    import asyncio
    import pathlib
    import tempfile

    from mini_loop import SessionManager
    from mini_loop.builtins import full_registry
    from mini_loop.config import build_client
    from mini_loop.transport import StreamingTransport

    settings = Settings(
        workspace_root=pathlib.Path(tempfile.mkdtemp()) / "ws",
        skills_dir=pathlib.Path(__file__).resolve().parent.parent / "skills",
    )
    manager = SessionManager(settings, build_client(settings),
                             transport=StreamingTransport(),
                             tool_registry=full_registry())
    session = manager.create()
    ends = []
    original = session._capture_event

    async def spy(event):
        result = await original(event)
        if result.get("type") == "model_end" and result.get("purpose") == "agent_turn":
            ends.append(result)
        return result

    session._capture_event = spy
    asyncio.run(session.run("Reply with exactly: STREAM-SERVED"))
    assert ends, "no agent-turn model_end recorded"
    assert all(e.get("served_model") for e in ends), (
        "a streaming turn recorded no served model -- identity honesty lost "
        "on the streaming path"
    )
