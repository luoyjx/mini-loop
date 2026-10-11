"""Display-only projections for the activity view (docs/WEBUI_PLAN.md R8).

Two pure helpers: a short phase title extracted from a COMPLETE public
commentary, and a deterministic semantic label for a tool call. Both are
presentation metadata and nothing else -- they never touch `ToolCall.input`,
never decide permissions, and never execute anything they parsed. The
conservative fallbacks are the design: any command shape this module does
not positively recognize projects to a neutral "run <preview>", so being
wrong is impossible by construction, only being vague.

Tense is the CONSUMER's job. `tool_label` returns a verb stem plus object;
`tool_use` only means Requested, so the UI conjugates by real lifecycle
(Requested/Running/Read/Ran) and never claims completion this module could
not know about.
"""

from __future__ import annotations

__all__ = ["ACTIVITY_TITLE_CAP", "activity_title", "tool_label"]

#: One line, bounded: a title is a heading, not a paragraph. Overflow is
#: cut with an ellipsis rather than rejected -- a long first sentence still
#: names the phase better than no title.
ACTIVITY_TITLE_CAP = 80

#: Sentence stops the title extractor recognizes, ASCII and CJK.
_SENTENCE_STOPS = (". ", "。", "! ", "！", "? ", "？", "; ", "；")

#: Shell metacharacters that end any attempt at semantic classification.
#: A pipe, substitution, redirect, or chain means the head token no longer
#: describes what the command does; classifying past one would label a
#: command by its first word and lie about the rest.
_OPAQUE_SHELL = ("|", "$(", "`", ">", "<", ";", "&&", "||", "\n")

#: Head token -> verb stem for the simple, single-purpose command shapes.
_SHELL_VERBS = {
    "rg": "search", "grep": "search", "egrep": "search", "fgrep": "search",
    "ls": "list", "tree": "list",
    "cat": "read", "head": "read", "tail": "read", "wc": "read",
}

_PREVIEW_CAP = 60


def _one_line(text: str, cap: int) -> str:
    line = " ".join(str(text).split())
    return line if len(line) <= cap else line[: cap - 1] + "…"


def activity_title(text: str) -> str | None:
    """First sentence of the first line of a complete commentary, or None.

    Conservative on purpose (R8-1): only already-public text goes in, only
    a single bounded line comes out, and anything unusable answers None so
    the caller falls back to tool labels instead of inventing a heading.
    Never raises -- a malformed commentary must not block the tool batch
    that follows it.
    """

    if not text or not str(text).strip():
        return None
    line = str(text).strip().splitlines()[0].strip()
    # Markdown furniture is not a title: strip heading/list/quote markers.
    line = line.lstrip("#*->• ").rstrip()
    if not line:
        return None
    cut = len(line)
    for stop in _SENTENCE_STOPS:
        idx = line.find(stop)
        if idx != -1:
            cut = min(cut, idx + 1)  # keep the stop character itself
    line = line[:cut].strip()
    if not line:
        return None
    return _one_line(line, ACTIVITY_TITLE_CAP)


def tool_label(name: str, tool_input: dict) -> dict:
    """Deterministic display projection: {"verb": stem, "object": text}.

    Known file tools map by their own schema; bash maps only the simple
    single-purpose shapes (search/list/read heads, no metacharacters).
    Everything else -- pipes, substitutions, redirects, chains, unknown
    tools -- projects to run/call with a bounded preview. The projection is
    computed from a masked COPY of the arguments by the caller; this
    function never mutates its input.
    """

    tool_input = tool_input if isinstance(tool_input, dict) else {}
    if name == "read_file":
        return {"verb": "read", "object": _one_line(tool_input.get("path", ""), _PREVIEW_CAP)}
    if name == "glob":
        return {"verb": "search", "object": _one_line(tool_input.get("pattern", ""), _PREVIEW_CAP)}
    if name == "write_file":
        return {"verb": "write", "object": _one_line(tool_input.get("path", ""), _PREVIEW_CAP)}
    if name == "edit_file":
        return {"verb": "edit", "object": _one_line(tool_input.get("path", ""), _PREVIEW_CAP)}
    if name in ("bash", "background_run"):
        command = str(tool_input.get("command", ""))
        preview = _one_line(command, _PREVIEW_CAP)
        if any(marker in command for marker in _OPAQUE_SHELL):
            return {"verb": "run", "object": preview}
        tokens = command.split()
        verb = _SHELL_VERBS.get(tokens[0]) if tokens else None
        if verb is None:
            return {"verb": "run", "object": preview}
        rest = _one_line(" ".join(tokens[1:]), _PREVIEW_CAP) or "."
        return {"verb": verb, "object": rest}
    return {"verb": "call", "object": name}


#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: two pure display projections with no state; their conservatism is pinned by tests, and nothing at runtime consumes them for decisions."
)
