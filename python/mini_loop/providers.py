"""The model-provider seam: whoever builds the client owns the model boundary.

Pi's provider contract (PI_RESEARCH.md P0-1/P1-1): `ModelProvider` owns the
complete model-request boundary -- client construction, credentials, and the
identity of what actually serves. mini-loop had that boundary split across
`config.build_client` (construction), `Settings` (credentials), and nothing
at all for identity -- which is how round 206 could measure a deployment
whose every record named `claude-sonnet-4-6` while the wire served
`deepseek-v4-flash`.

This is the first slice: construction and audit identity move behind one
protocol, `build_client` delegates (no caller changes), and one conformance
suite runs the same contract against every provider -- the fake always, a
real endpoint behind an explicit env gate so CI never needs network or
credentials.

`describe()` is the audit surface and carries NO credentials, ever: the
posture report and `--dump-config` may quote it verbatim.
"""

from __future__ import annotations

from typing import Protocol, runtime_checkable

__all__ = [
    "ModelProvider",
    "FakeProvider",
    "AnthropicCompatibleProvider",
    "provider_for",
]


@runtime_checkable
class ModelProvider(Protocol):
    """One model backend: construction plus honest audit identity."""

    name: str

    def create_client(self):
        """Return an async client exposing `.messages.create(...)`."""
        ...

    def describe(self) -> dict:
        """Credential-free audit identity (safe to print verbatim)."""
        ...


class FakeProvider:
    """The deterministic offline stand-in, behind the same contract."""

    name = "fake"

    def create_client(self):
        from .fake_llm import FakeAsyncAnthropic

        return FakeAsyncAnthropic()

    def describe(self) -> dict:
        return {"name": self.name, "wire": "anthropic", "endpoint": "in-process"}


class AnthropicCompatibleProvider:
    """The Anthropic wire shape at any endpoint.

    That includes api.anthropic.com and every compatible surface (measured:
    api.deepseek.com/anthropic). A compatible endpoint may alias model
    names, so the response's `model` field -- recorded as `served_model`
    since round 206 -- is the identity authority, never the request's.
    """

    def __init__(self, *, base_url: str | None = None,
                 api_key: str | None = None) -> None:
        self.base_url = base_url
        self._api_key = api_key
        self.name = "anthropic" if not base_url else "anthropic-compatible"

    def create_client(self):
        from anthropic import AsyncAnthropic

        kwargs: dict = {}
        if self.base_url:
            kwargs["base_url"] = self.base_url
        if self._api_key:
            kwargs["api_key"] = self._api_key
        return AsyncAnthropic(**kwargs)

    def describe(self) -> dict:
        # Identity without credentials: the endpoint is audit-relevant (it
        # decides which fleet actually answers); the key never is.
        return {
            "name": self.name,
            "wire": "anthropic",
            "endpoint": self.base_url or "https://api.anthropic.com",
            "credential": "<set>" if self._api_key else "<absent>",
        }


def provider_for(settings) -> ModelProvider:
    """Resolve the provider the way `build_client` always has."""

    if settings.fake_llm:
        return FakeProvider()
    return AnthropicCompatibleProvider(
        base_url=settings.base_url, api_key=settings.api_key
    )


#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: providers are pure construction values; the "
    "credential-free describe contract is pinned by conformance tests, and "
    "identity honesty lives on the model_end event (served_model)."
)
