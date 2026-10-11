"""Actual observation/reduction constructors and both builtin reducer algorithms."""
import asyncio
from copy import deepcopy
from dataclasses import asdict
from mini_loop.token_efficiency import (
    MaskedObservation, ObservationReduction,
    PassthroughObservationReducer, DeterministicLosslessReducer,
)


def efficiency_observation_contracts() -> dict:
    base = dict(content="masked text", content_type="text/plain", reduced_by=[],
                raw_ref=None, raw_digest=None, metadata=[])
    observation_specs = [
        ("default", {}), ("empty", dict(content="")),
        ("missing-type", dict(content_type="")), ("bad-type", dict(content_type="text")),
        ("slash-type", dict(content_type="/")),
        ("dedup-components", dict(reduced_by=["first", "second", "first"])),
        ("unvalidated-component", dict(reduced_by=["free text", ""])),
        ("ref-only", dict(raw_ref="raw")), ("digest-only", dict(raw_digest="digest")),
        ("paired-opaque", dict(raw_ref="not-a-valid-ref", raw_digest="not-a-valid-digest")),
        ("metadata", dict(metadata=[["exit_code", 7], ["timed_out", False],
                                   ["overflowed", True], ["duration_ms", 3.25]])),
        ("metadata-sequence", dict(metadata=[["same", None], ["same", [1, "文"]],
                                            ["", {"nested": True}]])),
    ]
    reduction_specs = [("default", {}), ("empty", dict(content="")),
        ("warnings", dict(warnings=["trimmed:1", "trimmed:1"])),
        ("too-many", dict(warnings=["trimmed:1"] * 33)),
        ("too-long", dict(warnings=["w" * 257])),
        ("unsafe", dict(warnings=["free text"])),
        ("suffix", dict(warnings=["folded:123456789012"])),
        ("suffix-overflow", dict(warnings=["folded:1234567890123"]))]
    def constructors(constructor, base, specs):
        rows = []
        for name, changes in specs:
            value = deepcopy(base | changes)
            args = deepcopy(value)
            for field in ("reduced_by", "metadata", "warnings"):
                if field in args: args[field] = tuple(args[field])
            try:
                output, error = asdict(constructor(**args)), None
            except (TypeError, ValueError) as fault:
                output, error = None, dict(kind=type(fault).__name__, message=str(fault))
            rows.append(dict(name=name, input=value, output=output, error=error))
        return rows
    specs = [("empty", ""), ("plain", "single line"),
             ("duplicate-two", "repeat\nrepeat"), ("duplicate-ten", "building\n" * 10),
             ("blank", "\n\n\n"), ("carriage-blank", "\r\n\r\r\n\n"),
             ("spaces-preserved", " \n \n\t\n\t"),
             ("ansi-colour", "\x1b[32mbuilding\x1b[0m\n" * 10),
             ("ansi-osc-bel", "before\x1b]0;title\x07after"),
             ("ansi-osc-st", "before\x1b]0;title\x1b\\after"),
             ("ansi-osc-greedy", "a\x1b]first\x1b\\middle\x1b]second\x1b\\z"),
             ("ansi-incomplete", "a\x1b[32\n\x1b]unfinished"),
             ("ansi-controls", "a\x1b[?25l\x1b[1;2 H\x1b[0mb"),
             ("mixed", "\x1b[32mbuilding\x1b[0m\n" * 10 + "\n\n\nERROR /tmp/app.py:42 failed\n" * 2)]
    critical = ["ERROR", "error", "fail", "faile", "failed", "failure", "exception",
                "TRACEBACK", "panic", "fatal", "warn", "warning", "denied", "timeout", "exit",
                "errors", "preerror", "error_suffix", "éerror", "erroré", "文error",
                "İXİT", "faıled", "faİled", "faſe", "exıt", "EXIT", "waRnIng",
                "1 item", "١ item", "１ item", "² item", "𞓰 item", "\U0001e4f0 item",
                "/path", "\\path", "C:\\path", "C:/path", "K:/path", "before /path", "before\x1c/path",
                "x/path", "unit.py", "unit.a-b", "unit.++++", "unit.+a", "unit.K", "unit.ſ", "unit.İ", "unit.ı",
                "unit.abcdefghijklmnop", "unit.pyé", "unit.py文", "a./path", "README"]
    specs += [("critical-" + str(index), line + "\n" + line) for index, line in enumerate(critical)]
    async def reducers():
        rows = []
        for name, text in specs:
            observation = MaskedObservation(text)
            outputs = []
            for reducer in (PassthroughObservationReducer(), DeterministicLosslessReducer()):
                result = await reducer.reduce(observation, query="unused query", budget_tokens=1)
                outputs.append(dict(component=reducer.descriptor.id, output=asdict(result)))
            rows.append(dict(name=name, content=text, outputs=outputs))
        return rows
    return dict(observations=constructors(MaskedObservation, base, observation_specs),
                reductions=constructors(ObservationReduction, dict(content="text", warnings=[]), reduction_specs),
                descriptors=[asdict(r.descriptor) for r in (PassthroughObservationReducer(), DeterministicLosslessReducer())],
                reducers=asyncio.run(reducers()))
