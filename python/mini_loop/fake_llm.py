"""A deterministic, offline stand-in for `anthropic.AsyncAnthropic`.

It mimics exactly the surface the agent loop touches:

    resp = await client.messages.create(model=, messages=, tools=, ...)
    resp.content       # list of blocks with .type / .text  or  .type/.name/.id/.input
    resp.stop_reason   # "tool_use" | "end_turn"

Two uses:
  * the FastAPI server boots with it when MINILOOP_FAKE_LLM=1, so the whole
    thing can be curled with no API key;
  * tests inject a custom `responder` to script precise tool-call sequences.

The default responder runs one `bash echo` then summarizes -- enough to drive
the loop, tool dispatch, sandboxing, and event stream end to end.
"""

from __future__ import annotations

import asyncio
import json

from .blocks import block_field
import os
from collections.abc import Callable


class TextBlock:
    type = "text"

    def __init__(self, text: str) -> None:
        self.text = text

    def __repr__(self) -> str:
        return f"TextBlock({self.text!r})"


class ToolUseBlock:
    type = "tool_use"

    def __init__(self, name: str, input: dict, id: str) -> None:
        self.name = name
        self.input = input
        self.id = id
        self.caller = None

    def __repr__(self) -> str:
        return f"ToolUseBlock({self.name!r}, {self.input!r})"


class InvalidTranscript(ValueError):
    """What the provider answers a malformed conversation with (a 400)."""


#: Anthropic's documented ceiling on `cache_control` breakpoints.
#:
#: Deliberately *not* imported from `caching.MAX_BREAKPOINTS`. This is the
#: provider's limit; that is the policy's budget. They must agree, and a double
#: that borrowed the harness's number could never disagree -- the test comparing
#: them would be a tautology. Two copies plus one assertion is the only shape
#: where the drift is detectable.
MAX_CACHE_BREAKPOINTS = 4


def validate_transcript(messages) -> None:
    """Reject what the real API rejects about the *shape* of a conversation.

    Checked against the live endpoint. It refuses three things the double used
    to accept in silence:

        unanswered tool_use          400 `tool_use` ids were found without
                                         `tool_result` blocks immediately after
        tool_result with no tool_use 400 unexpected `tool_result`
        tool_result id mismatch      400 unexpected `tool_result`

    That matters more here than a missing field would. Several subsystems exist
    *only* to keep this shape legal -- `_close_unanswered_tools` after a cancel
    or a crash, the pair-preserving logic in `snip_compact`, the tool-batch
    ordering -- and with a double that accepts anything, every one of them could
    have been broken with the suite still green.

    Deliberately no stricter than the observed behaviour: results must follow
    *immediately*, and an empty content string is accepted because the real
    endpoint accepts it.
    """

    if not messages:
        raise InvalidTranscript("messages: at least one message is required")

    def blocks(message):
        content = message.get("content") if isinstance(message, dict) else None
        return content if isinstance(content, list) else []

    def ids(message, kind, key):
        return [
            block_field(block, key)
            for block in blocks(message)
            if block_field(block, "type") == kind
        ]

    breakpoints = 0
    for index, message in enumerate(messages):
        content = message.get("content") if isinstance(message, dict) else None
        if isinstance(content, list) and not content:
            raise InvalidTranscript(
                f"messages.{index}: all messages must have non-empty content"
            )
        for block in blocks(message):
            if isinstance(block, dict) and "cache_control" in block:
                breakpoints += 1
            if block_field(block, "type") == "thinking" and not block_field(
                block, "signature"
            ):
                raise InvalidTranscript(
                    f"messages.{index}: a `thinking` block requires its "
                    "`signature` to be sent back unmodified"
                )
        expected = ids(message, "tool_use", "id")
        following = messages[index + 1] if index + 1 < len(messages) else None
        answered = ids(following, "tool_result", "tool_use_id") if following else []
        missing = [i for i in expected if i not in answered]
        if missing:
            raise InvalidTranscript(
                f"messages.{index}: `tool_use` ids were found without "
                f"`tool_result` blocks immediately after: {missing}"
            )
        previous = messages[index - 1] if index else None
        offered = ids(previous, "tool_use", "id") if previous else []
        for result_id in ids(message, "tool_result", "tool_use_id"):
            if result_id not in offered:
                raise InvalidTranscript(
                    f"messages.{index}: unexpected `tool_result` for "
                    f"{result_id!r}; no matching `tool_use` immediately before"
                )

    if breakpoints > MAX_CACHE_BREAKPOINTS:
        raise InvalidTranscript(
            f"a maximum of {MAX_CACHE_BREAKPOINTS} cache_control blocks may be "
            f"supplied; {breakpoints} were"
        )


def validate_request(kwargs) -> None:
    """Everything the provider checks before it will answer at all."""

    max_tokens = kwargs.get("max_tokens")
    if not isinstance(max_tokens, int) or max_tokens < 1:
        raise InvalidTranscript(f"Invalid max_tokens value: {max_tokens!r}")
    validate_transcript(kwargs.get("messages"))


class FakeUsage:
    """What a provider reports back about the request it just answered.

    The offline model used to report nothing here, which meant every code path
    that reads `usage` was untestable -- and the token meter, which drives
    compaction off the provider's count, would have been silently inert in the
    entire suite. A stand-in for a provider has to stand in for the parts the
    harness reads, not only the parts it renders.
    """

    def __init__(self, input_tokens: int, output_tokens: int = 0) -> None:
        self.input_tokens = input_tokens
        self.output_tokens = output_tokens
        self.cache_read_input_tokens = 0
        self.cache_creation_input_tokens = 0
        self.service_tier = "standard"


def count_tokens(kwargs: dict) -> int:
    """A deliberately *different* approximation from `estimate_tokens`.

    Two properties matter, and both are things the estimator gets wrong:
    it counts the system prompt and tool schemas, which are input as much as the
    messages are; and it charges non-ASCII text far more per character, which is
    where `len(json.dumps(...)) // 4` diverges worst (JSON escapes each CJK
    character to six ASCII ones). A fake that agreed with the estimator would
    make every metering test pass for the wrong reason.
    """

    payload = json.dumps(
        [kwargs.get("messages"), kwargs.get("system"), kwargs.get("tools")],
        default=str,
        ensure_ascii=False,
    )
    # `sum(1 for c in payload if ord(c) < 128)` is the obvious spelling and it
    # ran per request over the whole payload -- 72% of a profiled 40-turn
    # session, three million generator steps, inside the *test double*. The
    # suite is what executes every rule in this repo, so a double that dominates
    # it is a real cost. Encoding with errors="ignore" drops exactly the
    # non-ASCII characters and counts the rest in C: 28x faster, identical
    # output on ASCII, CJK and emoji alike.
    ascii_chars = len(payload.encode("ascii", "ignore"))
    wide_chars = len(payload) - ascii_chars
    return int(ascii_chars / 4 + wide_chars) + 8


class ThinkingBlock:
    """Extended-thinking output, which the real reasoner returns constantly.

    Carried here because the harness has to *round-trip* it: a continued
    tool-use conversation sends the assistant's thinking blocks back up, and the
    API rejects one whose `signature` does not survive intact. The offline model
    emitted only text and tool_use, so every transform the harness applies to a
    thinking block -- normalization, masking, cache annotation, compaction,
    the storage round-trip -- ran untested against real traffic's most common
    assistant block.
    """

    type = "thinking"

    def __init__(self, thinking: str, signature: str = "sig_fake_00000000") -> None:
        self.thinking = thinking
        self.signature = signature

    def __repr__(self) -> str:
        return f"ThinkingBlock({self.thinking!r})"


class FakeMessage:
    """Shaped like a real response, including the fields the harness reads.

    `id`, `model`, `role` and `type` are not decoration: they are what a
    trajectory, an event payload or a debugging session shows, and a stand-in
    that omits them makes those paths look fine offline.
    """

    def __init__(
        self,
        content: list,
        stop_reason: str,
        usage=None,
        *,
        model: str = "fake-model",
        message_id: str = "msg_fake",
    ) -> None:
        self.id = message_id
        self.type = "message"
        self.role = "assistant"
        self.model = model
        self.content = content
        self.stop_reason = stop_reason
        self.stop_sequence = None
        self.stop_details = None
        self.container = None
        self.usage = usage or FakeUsage(0)


# --- block helpers (handy when scripting a responder in tests) -------------

def text(s: str) -> TextBlock:
    return TextBlock(s)


def thinking(s: str, signature: str = "sig_fake_00000000") -> ThinkingBlock:
    return ThinkingBlock(s, signature)


def tool(name: str, /, _id: str = "toolu_x", **input) -> ToolUseBlock:
    # `name` is positional-only so a tool's own `name` input field can still be
    # passed as a kwarg, e.g. tool("greet", name="World").
    return ToolUseBlock(name, input, _id)


def system_text(source) -> str:
    """Return the system prompt as text, whatever wire shape it arrived in.

    The cache policy renders `system` as a list of text blocks so it can carry a
    `cache_control` breakpoint. Responders and tests that only care about the
    prompt's words should go through this instead of assuming a bare string.
    """

    system = source.get("system") if isinstance(source, dict) else source
    if system is None:
        return ""
    if isinstance(system, str):
        return system
    if isinstance(system, list):
        return "\n".join(
            str(block.get("text", "")) if isinstance(block, dict) else str(block)
            for block in system
        )
    return str(system)


def _last_result_text(content: list) -> str:
    for part in content:
        if isinstance(part, dict) and part.get("type") == "tool_result":
            return str(part.get("content", ""))[:200]
    return ""


def default_responder(kwargs: dict) -> tuple[list, str]:
    """One bash echo, then a summary. Stateless: keyed off the last message."""
    tools = kwargs.get("tools")
    messages = kwargs["messages"]
    last = messages[-1]

    # No tools => this is the auto-compaction summarization call.
    if not tools:
        return [text(f"[summary of {len(messages)} message(s)]")], "end_turn"

    # Fresh user prompt (a plain string) => take one action.
    if isinstance(last.get("content"), str):
        prompt = last["content"].replace("\n", " ")[:60]
        return [
            text("Working on it."),
            tool("bash", _id="toolu_1", command=f'echo handled: {prompt}'),
        ], "tool_use"

    # Tool results came back (a list) => wrap up.
    return [text(f"Done. Tool said: {_last_result_text(last['content'])}")], "end_turn"


def scripted(turns: list[tuple[list, str]]) -> Callable[[dict], tuple[list, str]]:
    """Build a responder that returns each (blocks, stop_reason) turn in order,
    falling back to a plain end_turn once the script is exhausted."""
    state = {"i": 0}

    def responder(kwargs: dict) -> tuple[list, str]:
        if not kwargs.get("tools"):
            return [text("[summary]")], "end_turn"
        i = state["i"]
        state["i"] += 1
        if i < len(turns):
            return turns[i]
        return [text("Done.")], "end_turn"

    return responder


class _Messages:
    def __init__(self, parent: "FakeAsyncAnthropic") -> None:
        self._parent = parent

    def stream(self, **kwargs):
        return _FakeStream(self._parent, kwargs)

    async def create(self, **kwargs) -> FakeMessage:
        # The real SDK refuses a non-streaming call whose `max_tokens` implies a
        # request longer than ten minutes, and raises *before* sending. A fake
        # that accepts any budget hid a recovery path that could never work: it
        # escalated to 64000 and turned a recoverable truncation into a failed
        # turn, and every test passed.
        validate_request(kwargs)
        if self._parent.nonstreaming_ceiling is not None:
            if int(kwargs.get("max_tokens") or 0) > self._parent.nonstreaming_ceiling:
                raise ValueError(
                    "Streaming is required for operations that may take longer "
                    "than 10 minutes. See https://github.com/anthropics/"
                    "anthropic-sdk-python#long-requests for more details"
                )
        self._parent.calls += 1
        if self._parent.delay:
            await asyncio.sleep(self._parent.delay)
        content, stop = self._parent.responder(kwargs)
        if self._parent.thinking:
            content = [
                ThinkingBlock(
                    "considering the request", f"sig_fake_{self._parent.calls:08d}"
                ),
                *content,
            ]
        return FakeMessage(
            content,
            stop,
            usage=FakeUsage(count_tokens(kwargs), output_tokens=len(content)),
            model=kwargs.get("model", "fake-model"),
            message_id=f"msg_fake_{self._parent.calls:06d}",
        )


class _FakeStream:
    """What `client.messages.stream(...)` yields, closely enough to matter.

    An async context manager that is also an async iterator of events and
    answers `get_final_message()`. Reproduced because a stand-in that only
    supports the call shape the harness used *before* a feature lands is how a
    broken path stays green -- `usage` and the non-streaming ceiling were both
    found that way.
    """

    def __init__(self, parent: "FakeAsyncAnthropic", kwargs: dict) -> None:
        self._parent = parent
        self._kwargs = kwargs
        self._final: FakeMessage | None = None

    async def __aenter__(self) -> "_FakeStream":
        validate_request(self._kwargs)
        content, stop = self._parent.responder(self._kwargs)
        if self._parent.thinking:
            content = [
                ThinkingBlock(
                    "considering the request", f"sig_fake_{self._parent.calls:08d}"
                ),
                *content,
            ]
        self._parent.calls += 1
        self._final = FakeMessage(
            content, stop,
            usage=FakeUsage(count_tokens(self._kwargs), output_tokens=len(content)),
            model=self._kwargs.get("model", "fake-model"),
            message_id=f"msg_fake_{self._parent.calls:06d}",
        )
        return self

    async def __aexit__(self, *exc) -> bool:
        return False

    async def __aiter__(self):
        for index, block in enumerate(self._final.content):
            kind = block_field(block, "type")
            body = block_field(block, "text") or block_field(block, "thinking")
            if not body:
                continue
            # Thinking arrives as `thinking_delta` carrying `.thinking`, text as
            # `text_delta` carrying `.text`. Collapsing both into `.text` -- as
            # this did -- made every consumer look correct while it could not
            # actually tell them apart.
            field = "thinking" if kind == "thinking" else "text"
            # Split into several deltas, so coalescing is actually exercised.
            step = max(1, len(body) // 3)
            for start in range(0, len(body), step):
                yield _FakeStreamEvent(body[start:start + step], index, field)

    async def get_final_message(self) -> FakeMessage:
        return self._final


class _FakeStreamEvent:
    type = "content_block_delta"

    def __init__(self, body: str, index: int, field: str = "text") -> None:
        self.index = index
        self.delta = _FakeDelta(body, field)


class _FakeDelta:
    """Carries exactly one of `.text` or `.thinking`, as the real one does."""

    def __init__(self, body: str, field: str = "text") -> None:
        self.type = f"{field}_delta"
        setattr(self, field, body)


class FakeAsyncAnthropic:
    def __init__(self, responder: Callable[[dict], tuple[list, str]] | None = None,
                 delay: float | None = None, thinking: bool = True,
                 nonstreaming_ceiling: int | None = 8192) -> None:
        self.responder = responder or default_responder
        #: Prepend a thinking block to every response, as a reasoning model does.
        self.thinking = thinking
        #: Largest `max_tokens` accepted without streaming, as the SDK enforces.
        #: `None` disables the check for tests that do not care.
        self.nonstreaming_ceiling: int | None = nonstreaming_ceiling
        self.delay = delay if delay is not None else float(os.getenv("MINILOOP_FAKE_DELAY", "0") or 0)
        self.calls = 0
        self.messages = _Messages(self)

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: a test double asserts on test traffic (validate_transcript) and must never observe production state."
)
