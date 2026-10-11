"""How full the context actually is, according to the provider.

Compaction decided when to fire from ``estimate_tokens`` -- ``len(json.dumps(
messages)) // 4``. Measured against the provider's own tokenizer, that estimate
is off by a factor of **0.36x to 2.64x** depending on what the transcript holds:

    english prose      1,328 est /  1,085 actual   1.22x  fires too early
    chinese prose      2,708 est /  1,024 actual   2.64x  fires too early
    source code        1,468 est /  1,524 actual   0.96x  about right
    json blob          2,953 est /  4,205 actual   0.70x  fires too late
    base64 payload       908 est /  2,505 actual   0.36x  fires too late

Both directions cost something and they are not symmetric. Firing early throws
away context and invalidates the cached prefix for no reason. Firing late means
the request that finally goes out is over the model's limit, which is not a
degradation -- it is a hard error, and the session cannot make progress until
something else shrinks it. A base64-heavy transcript reaches 278k real tokens
while this estimator still reads 100k.

The estimator also never saw the system prompt or the tool schemas, which are
input too: a realistic system prompt plus eight tools measured **3,395 tokens
that the estimate reported as 8**.

None of this needs estimating. The provider returns the exact count in every
response, and the harness was already capturing it -- into an event, where
nothing read it. This module feeds it back into the decision.

**Anchor and delta, not a scale factor.** Each response gives an exact reading
for the prompt as it was. Later in the same turn the transcript has grown and
there is no new reading, so growth is extrapolated -- but the ratio used is
learned from how *consecutive* readings moved, which cancels the fixed
system-and-tools overhead instead of scaling it up with the transcript.

**Cached tokens still occupy the window.** With prompt caching on,
``input_tokens`` counts only the part that was not read from cache. Taking it as
the prompt size would report a 190k-token request as 4k -- under-counting, the
direction that ends in a hard failure, and it would get *worse* the better
caching works. :func:`prompt_tokens` sums all three fields.
"""

from __future__ import annotations

from typing import Any

from .compaction import estimate_tokens

__all__ = ["TokenMeter", "prompt_tokens", "PROMPT_TOKEN_FIELDS"]

#: Every field the provider counts toward the prompt. Cache hits are cheaper,
#: not smaller -- they occupy the context window like anything else.
PROMPT_TOKEN_FIELDS = (
    "input_tokens",
    "cache_read_input_tokens",
    "cache_creation_input_tokens",
)

#: A single strange reading must not be able to wreck the calibration.
MIN_CALIBRATION = 0.2
MAX_CALIBRATION = 6.0


def _field(usage: Any, name: str) -> int:
    value = usage.get(name) if isinstance(usage, dict) else getattr(usage, name, None)
    return int(value) if isinstance(value, (int, float)) else 0


def prompt_tokens(usage: Any) -> int | None:
    """Total prompt size the provider counted, or `None` when it said nothing.

    Sums the cached fields as well: they are billed differently but they take up
    the same context window, and omitting them under-counts exactly when the
    cache is working well.
    """

    if usage is None:
        return None
    total = sum(_field(usage, name) for name in PROMPT_TOKEN_FIELDS)
    return total or None


class TokenMeter:
    """Real prompt size, anchored on the provider's count.

    `used(messages)` answers "how full is the context right now" with the last
    exact reading plus calibrated growth since. Before any response has been
    seen it returns the raw estimate -- which is what the harness did for every
    request, not just the first.
    """

    def __init__(self, *, calibration: float = 1.0, smoothing: float = 0.5) -> None:
        self._anchor_actual: int | None = None
        self._anchor_estimate: int = 0
        #: Fingerprint of the request envelope (system prompt + tool catalog)
        #: the anchor was read under. The anchor models the prompt as
        #: `overhead + transcript * calibration` with the overhead FIXED --
        #: an assumption the session breaks whenever tools are registered
        #: (MCP connect), unregistered (teammate spawn), or the system prompt
        #: is rebuilt. DeepSeek Harness's token meter reuses provider usage
        #: only while "the canonical request envelope matches"; same rule here.
        self._anchor_envelope: str | None = None
        self._calibration = calibration
        self.smoothing = smoothing
        self.observations = 0

    @property
    def calibrated(self) -> bool:
        """True once a real reading has been seen."""
        return self._anchor_actual is not None

    @property
    def calibration(self) -> float:
        """Learned tokens-per-estimated-token, for growth since the anchor."""
        return self._calibration

    @property
    def anchor(self) -> int | None:
        """The last exact prompt size the provider reported."""
        return self._anchor_actual

    def observe(
        self, usage: Any, messages: list, *, envelope: str | None = None
    ) -> int | None:
        """Record what the provider counted for `messages`. Returns that count.

        The calibration is learned from the *difference* between consecutive
        readings. A ratio taken from a single absolute reading would fold the
        system prompt and tool schemas into a multiplier and then inflate them
        again as the transcript grows; a difference contains only what was added.

        `envelope` identifies the system prompt + tool catalog this reading was
        taken under. When it differs from the previous reading's, the delta
        between the two readings contains the envelope change as well as
        transcript growth, so the calibration update is skipped -- the reading
        still re-anchors.
        """

        actual = prompt_tokens(usage)
        if actual is None:
            return None
        estimate = estimate_tokens(messages)

        previous_actual, previous_estimate = self._anchor_actual, self._anchor_estimate
        same_envelope = (
            envelope is None
            or self._anchor_envelope is None
            or envelope == self._anchor_envelope
        )
        grew = estimate - previous_estimate
        if (
            previous_actual is not None
            and same_envelope
            and grew > 0
            and actual > previous_actual
        ):
            ratio = (actual - previous_actual) / grew
            blended = (1 - self.smoothing) * self._calibration + self.smoothing * ratio
            self._calibration = min(MAX_CALIBRATION, max(MIN_CALIBRATION, blended))

        self._anchor_actual = actual
        self._anchor_estimate = estimate
        self._anchor_envelope = envelope
        self.observations += 1
        return actual

    def used(self, messages: list) -> int:
        """Best available answer to "how many prompt tokens is this now".

        The anchor models the prompt as `overhead + anchor_estimate *
        calibration`, where the overhead -- the system prompt and tool schemas,
        which `estimate_tokens` never sees -- is fixed across turns. The size at
        any other estimate is therefore `overhead + estimate * calibration`,
        which reduces to `anchor_actual + delta * calibration` with a *signed*
        delta. Clamping the delta to growth-only assumed the transcript can only
        grow between provider readings -- but compaction shrinks it, and the
        meter re-anchors only on the next response. Until then a growth-clamped
        meter keeps reporting the pre-compaction size, so the `context_used() >
        threshold` gate runs the expensive LLM-summary layer against a
        transcript the cheap layers (snip, micro) already cut below threshold. A
        signed delta lets the shrink be seen; the result is floored at zero, and
        a shrink large enough to floor it means the transcript is genuinely
        small, so reporting "near empty" is correct rather than an under-count.
        """

        estimate = estimate_tokens(messages)
        if self._anchor_actual is None:
            return estimate
        delta = estimate - self._anchor_estimate
        return max(0, int(self._anchor_actual + delta * self._calibration))

    def used_for(self, messages: list, *, envelope: str | None = None) -> int:
        """`used`, but honest about a changed request envelope.

        An anchor read under one envelope misprices another: connecting an MCP
        server can add tens of thousands of schema tokens that neither the
        estimator nor the stale anchor sees -- under-counting, the direction
        that ends in a hard overflow. On mismatch the anchored model is set
        aside and the raw estimate answers, until the next response re-anchors
        under the new envelope. Callers that cannot name their envelope get
        `used`'s behavior unchanged.
        """

        if (
            envelope is not None
            and self._anchor_envelope is not None
            and envelope != self._anchor_envelope
        ):
            return estimate_tokens(messages)
        return self.used(messages)

    def snapshot(self) -> dict[str, Any]:
        """What the meter knows, for events and the console."""

        return {
            "calibrated": self.calibrated,
            "anchor_tokens": self._anchor_actual,
            "anchor_envelope": self._anchor_envelope,
            "calibration": round(self._calibration, 3),
            "observations": self.observations,
        }

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: the meter is advisory measurement; a wrong reading degrades compaction timing, and accuracy is characterized by tests, not assertable at runtime."
)
