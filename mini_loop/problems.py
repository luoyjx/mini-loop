"""A bounded, deduplicating place for a subsystem to say "that did not work".

Rounds 45 to 50 gave six subsystems a `problems` list, on the reasoning that a
surface with nowhere to report will eventually fail silently. Each one was a
plain list, appended to on every occurrence, and never trimmed. Asking the
round-49 checklist about them — *is the content bounded* — answers no:

    cron   : one dead job for 24h -> 1,440 problem entries, 138,240 chars
    teams  : 10,000 bad reads      -> 10,000 entries
    memory : 2,000 oversized writes-> 2,000 entries
    tasks  : 2,000 oversized tasks -> 2,000 entries

Two failures, not one. The obvious one is that a long-running process leaks
memory through its own error channel. The subtler one matters more: a *single*
recurring fault produces thousands of identical entries, so the count stops
meaning "how many things are wrong", and a different, rarer problem — the one
nobody has seen yet — is buried under a repeat of the one they already know
about.

So this deduplicates and counts rather than accumulating, and keeps the newest
distinct problems when full. It subclasses `list` deliberately: every existing
call site does `self.problems.append(...)`, and the audit iterates and takes
`len`, so nothing else has to change to gain the behaviour.
"""

from __future__ import annotations

__all__ = ["ProblemLog", "MAX_DISTINCT_PROBLEMS"]

#: Distinct problems retained. Past this, the oldest distinct one is dropped --
#: a subsystem with a hundred *different* faults has a bigger issue than the
#: hundred-and-first going unrecorded.
MAX_DISTINCT_PROBLEMS = 50


class ProblemLog(list):
    """Deduplicating, bounded list of problem messages.

    Reads as a list of distinct messages. `counts` says how often each was
    reported, and `summary()` renders them together for display.
    """

    def __init__(self, *, limit: int = MAX_DISTINCT_PROBLEMS) -> None:
        super().__init__()
        self.limit = limit
        self.counts: dict[str, int] = {}
        #: Every occurrence ever appended, including ones whose message has
        #: since been evicted. Kept separately because `counts` is per *retained*
        #: message: with more distinct problems than the limit, entries churn and
        #: their counts restart, so summing `counts` reported 400 appends as 3.
        self._total = 0
        #: Evictions, not distinct problems lost. A churning set of four
        #: messages against a limit of three evicts hundreds of times while
        #: losing four distinct problems, so this is a churn signal rather than
        #: a count of anything.
        self.dropped = 0

    def append(self, message: object) -> None:
        text = str(message)
        self._total += 1
        if text in self.counts:
            self.counts[text] += 1
            return
        if len(self) >= self.limit:
            evicted = super().pop(0)
            self.counts.pop(evicted, None)
            self.dropped += 1
        self.counts[text] = 1
        super().append(text)

    def extend(self, messages) -> None:
        for message in messages:
            self.append(message)

    def total(self) -> int:
        """Every occurrence ever appended, retained or evicted.

        Not `sum(self.counts.values())`, which only sees retained messages and
        restarts at one whenever an evicted message comes back. With more
        distinct problems than the limit that under-reported 400 occurrences as
        3 -- the same "the count stopped meaning anything" failure this class was
        written to fix, reappearing at the eviction boundary.
        """

        return self._total

    def summary(self) -> list[str]:
        """Distinct problems, each with its occurrence count when above one."""
        return [
            f"{text} (x{self.counts[text]})" if self.counts[text] > 1 else text
            for text in self
        ]

    def churning(self) -> bool:
        """True when eviction is cycling rather than retiring old problems.

        More evictions than distinct problems retained means the log is too
        small for what this subsystem is reporting, and the per-message counts
        it shows are therefore lower bounds.
        """

        return self.dropped > self.limit

    def clear(self) -> None:
        super().clear()
        self.counts.clear()
        self._total = 0
        self.dropped = 0
