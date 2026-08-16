"""dsh-style message-trace viewer: a recorded run as one readable page.

The trajectory store (`trajectory.py`) keeps the authoritative account of a
run -- model spans, tool spans, assistant text, compaction, errors -- as
append-only JSONL. Reading one today means paging raw JSON. dsh renders the
same information as a turn-aware ledger (its `ui-trajectory` package): index /
kind / content rows, thick rules at turn boundaries, a local inspector for
input, output, timing and token usage, and an overview strip that projects
real start/duration onto a timeline. This module is that ledger for
mini-loop, produced as one self-contained HTML file: no external assets,
no script beyond a local text filter, nothing fetched.

Three rules carried over from the rest of the harness:

- **Everything is escaped.** The ledger renders transcript content -- tool
  output, model text, user input -- which is untrusted by definition. Every
  string passes through `_esc` before it reaches the page, so a tool result
  containing `<script>` renders as text, never as markup.
- **No fabricated duration.** A span with no end event (crash, still
  running) says `in flight`; the overview draws a start marker, not an
  invented bar. Copied deliberately from dsh, whose viewer renders partial
  rows "without a fabricated duration".
- **No silent caps.** A ledger longer than `MAX_ROWS` keeps the tail (the
  end of a run is where the answer is) and states exactly how many earlier
  records were dropped and how to get them; totals are computed over the
  full event list before the cap, so the summary never shrinks with the page.
"""

from __future__ import annotations

import html
import json
import os
import time
from pathlib import Path

#: Ledger rows kept per turn; the tail survives, the omission is stated.
MAX_ROWS = 2_000
#: Characters of a row's content shown inline in the ledger.
PREVIEW_CHARS = 240
#: Characters of one field shown inside the inspector.
INSPECTOR_CHARS = 20_000

#: Envelope keys every event carries; the generic inspector hides them so the
#: payload -- the part that differs -- is what the reader sees.
_ENVELOPE_KEYS = frozenset({
    "type", "seq", "ts", "session", "trajectory_id", "trace_id", "group_id",
    "agent", "depth", "record_type",
})


def _esc(value) -> str:
    """The single escaping chokepoint: nothing reaches the page around it."""
    return html.escape(str(value), quote=True)


def _capped(text: str, limit: int) -> str:
    if len(text) <= limit:
        return text
    omitted = len(text) - limit
    return (
        text[:limit]
        + f"\n[... {omitted:,} more characters -- export the trajectory for the full record]"
    )


def _preview(text: str) -> str:
    collapsed = " ".join(str(text).split())
    if len(collapsed) > PREVIEW_CHARS:
        collapsed = collapsed[:PREVIEW_CHARS] + "..."
    return collapsed


def _fmt_ms(duration_ms) -> str:
    if duration_ms is None:
        return "in flight"
    ms = float(duration_ms)
    if ms >= 60_000:
        return f"{ms / 60_000:.1f} min"
    if ms >= 1_000:
        return f"{ms / 1_000:.2f} s"
    return f"{ms:.0f} ms"


def _fmt_offset(ts, base) -> str:
    if ts is None or base is None:
        return ""
    return f"+{max(0.0, float(ts) - float(base)):.1f}s"


def _pretty(value) -> str:
    if isinstance(value, str):
        return value
    try:
        return json.dumps(value, ensure_ascii=False, indent=2, default=str)
    except (TypeError, ValueError):
        return str(value)


def _payload(event: dict) -> dict:
    return {k: v for k, v in event.items() if k not in _ENVELOPE_KEYS}


# -- ledger assembly ---------------------------------------------------------

def build_ledger(trajectory: dict) -> dict:
    """Fold one assembled trajectory into a turn ledger.

    Rows correlate span pairs (`model_start`/`model_end`, `tool_use`/
    `tool_result`) by `span_id`, the way dsh's ledger assembles business
    records from raw events. An event type this module has never seen still
    produces a row -- a new path inherits the need to be visible, so unknown
    kinds render generically rather than disappearing.
    """
    events = trajectory.get("events") or []
    base = trajectory.get("started_at")
    rows: list[dict] = []
    open_spans: dict[str, dict] = {}
    step = 0
    request_no = 0

    user_input = trajectory.get("input")
    if user_input is not None:
        rows.append({
            "kind": "user", "label": "user", "seq": 0, "ts": base,
            "content": str(user_input),
            "detail": {"Input": str(user_input)},
        })

    for event in events:
        etype = str(event.get("type", "event"))
        seq, ts = event.get("seq"), event.get("ts")
        span_id = event.get("span_id")
        # Subagent activity arrives in the same event stream, tagged with the
        # child's label and delegation depth. Carried onto every row so the
        # renderer can nest it -- flattened, a child's bash rows are
        # indistinguishable from the parent's (dsh indents nested subtools
        # for the same reason).
        depth = int(event.get("depth") or 0)
        nest = {"depth": depth, "agent": event.get("agent")} if depth else {}
        if etype == "model_start":
            # One chronological numbering space across every purpose --
            # ordinary turns and compaction summaries alike -- as in dsh's
            # Request projection.
            request_no += 1
            # Steps are the *parent* loop's structure; a child's model calls
            # advance its own story, not the turn's step count.
            if event.get("purpose") == "agent_turn" and depth == 0:
                step += 1
                rows.append({"kind": "step", "label": f"step {step}", "seq": seq})
            row = {
                "kind": "model",
                "label": f"#{request_no} {event.get('model', 'model')}",
                "seq": seq, "ts": ts, "duration_ms": None, "status": "in flight",
                "content": f"purpose={event.get('purpose')} "
                           f"messages={event.get('message_count')}",
                "detail": {
                    "Request": request_no,
                    "Purpose": event.get("purpose"),
                    "Model": event.get("model"),
                    "Messages": event.get("message_count"),
                    "Tool catalog": event.get("tool_count"),
                    "Max tokens": event.get("max_tokens"),
                },
                **nest,
            }
            rows.append(row)
            if span_id:
                open_spans[span_id] = row
        elif etype == "model_end":
            row = open_spans.pop(span_id, None)
            if row is None:
                continue
            row["status"] = str(event.get("status", "completed"))
            # A span closes with the duration its end event measured; a span
            # that never closes keeps `None` and renders as `in flight`.
            row["duration_ms"] = event.get("duration_ms")
            usage = event.get("usage") or {}
            row["usage"] = usage
            row["detail"]["Stop reason"] = event.get("stop_reason")
            row["detail"]["Status"] = row["status"]
            row["detail"]["Duration"] = _fmt_ms(row["duration_ms"])
            if event.get("error"):
                row["error"] = True
                row["detail"]["Error"] = event.get("error")
            if usage:
                row["detail"]["Usage"] = usage
            if isinstance(event.get("model_output"), (list, dict)):
                row["detail"]["Output"] = event["model_output"]
        elif etype == "tool_use":
            row = {
                "kind": "tool", "label": str(event.get("name", "tool")),
                "seq": seq, "ts": ts, "duration_ms": None, "status": "in flight",
                "content": _pretty(event.get("input", {})),
                "detail": {
                    "Tool": event.get("name"),
                    "Input": event.get("input"),
                    "Call id": event.get("id"),
                    "Action id": event.get("action_id"),
                },
                **nest,
            }
            rows.append(row)
            if span_id:
                open_spans[span_id] = row
        elif etype == "tool_result":
            row = open_spans.pop(span_id, None)
            if row is None:
                continue
            row["duration_ms"] = event.get("duration_ms")
            denied = bool(event.get("denied"))
            failed = bool(event.get("error"))
            row["status"] = "denied" if denied else ("error" if failed else "completed")
            row["error"] = denied or failed
            output = event.get("output", "")
            row["content"] = f"{row['label']} -> {output}"
            row["detail"]["Output"] = output
            row["detail"]["Status"] = row["status"]
            row["detail"]["Duration"] = _fmt_ms(row["duration_ms"])
            if event.get("replayed"):
                row["detail"]["Replayed"] = "reused a recorded outcome"
            command = event.get("command_result")
            if isinstance(command, dict):
                row["detail"]["Command"] = command
        elif etype == "assistant_text":
            text = str(event.get("text", ""))
            rows.append({
                "kind": "assistant", "label": "assistant", "seq": seq, "ts": ts,
                "content": text, "detail": {"Text": text}, **nest,
            })
        elif etype == "tool_catalog":
            # Reference data, not conversation: one compact line in the
            # ledger, the full schemas behind the inspector. Dumped through
            # the generic branch these drowned the rows around them.
            schemas = event.get("schemas") or []
            rows.append({
                "kind": "reference", "label": "catalog",
                "seq": seq, "ts": ts,
                "content": f"{len(schemas)} tools · fingerprint "
                           f"{event.get('fingerprint', '?')}",
                "detail": {"Fingerprint": event.get("fingerprint"),
                           "Schemas": schemas},
                **nest,
            })
        elif etype == "system_prompt":
            text = str(event.get("text", ""))
            rows.append({
                "kind": "reference", "label": "system",
                "seq": seq, "ts": ts,
                "content": f"{len(text):,} chars · hash "
                           f"{event.get('hash', '?')}",
                "detail": {"Hash": event.get("hash"),
                           "System prompt": event.get("text")},
                **nest,
            })
        elif etype == "steering_delivered":
            # A first-class row, as in dsh's ledger: the reader sees what was
            # steered at the position it entered the turn, not a bare count.
            text = str(event.get("text", ""))
            rows.append({
                "kind": "steer", "label": f"steer x{event.get('count', 1)}",
                "seq": seq, "ts": ts, "content": text,
                "detail": {"Interjection": text,
                           "Steers delivered": event.get("count")},
                **nest,
            })
        elif etype == "compact":
            rows.append({
                "kind": "compaction", "label": f"compact/{event.get('kind')}",
                "seq": seq, "ts": ts, "content": _pretty(_payload(event)),
                "detail": {"Compaction": _payload(event)},
                "error": event.get("kind") == "failed", **nest,
            })
        elif etype == "error":
            rows.append({
                "kind": "error", "label": "error", "seq": seq, "ts": ts,
                "content": _pretty(_payload(event)), "error": True,
                "detail": {"Error": _payload(event)}, **nest,
            })
        elif etype in ("trajectory_start", "trajectory_end") or (
            etype == "done" and trajectory.get("output") is not None
        ):
            # Collapsed, not suppressed: these session events mirror what the
            # turn header and final row already render from the assembled
            # trajectory (dsh collapses the same trace-only events). `done`
            # keeps its row when there is no assembled output to mirror it,
            # so the answer can never disappear from the page.
            continue
        else:
            rows.append({
                "kind": "event", "label": etype, "seq": seq, "ts": ts,
                "content": _pretty(_payload(event)),
                "detail": {"Payload": _payload(event)}, **nest,
            })

    status = str(trajectory.get("status", "completed"))
    if trajectory.get("output") is not None or trajectory.get("error") is not None:
        final = trajectory.get("error") or trajectory.get("output") or ""
        rows.append({
            "kind": "final", "label": f"final/{status}",
            "seq": None, "ts": trajectory.get("ended_at"),
            "content": str(final), "detail": {"Final": final},
            "error": trajectory.get("error") is not None,
        })

    metrics = dict(trajectory.get("metrics") or {})
    tokens_in = tokens_out = 0
    for row in rows:
        usage = row.get("usage") or {}
        tokens_in += int(usage.get("input_tokens") or 0)
        tokens_out += int(usage.get("output_tokens") or 0)
    metrics["input_tokens"], metrics["output_tokens"] = tokens_in, tokens_out

    # Totals above are folded over every row; only now does the cap apply.
    omitted = 0
    if len(rows) > MAX_ROWS:
        omitted = len(rows) - MAX_ROWS
        rows = rows[-MAX_ROWS:]

    end = trajectory.get("ended_at")
    if end is None:
        stamps = [r["ts"] for r in rows if r.get("ts") is not None]
        end = max(stamps) if stamps else base
    return {
        "trajectory_id": trajectory.get("trajectory_id"),
        "session": trajectory.get("session"),
        "run_index": trajectory.get("run_index"),
        "status": status,
        "partial": bool(trajectory.get("partial")),
        "started_at": base,
        "ended_at": end,
        "duration_ms": trajectory.get("duration_ms"),
        "input": trajectory.get("input"),
        "metrics": metrics,
        "rows": rows,
        "omitted": omitted,
    }


# -- rendering ---------------------------------------------------------------

_CSS = """
:root { color-scheme: light dark;
  --bg:#ffffff; --fg:#1a1c22; --dim:#69707d; --line:#e2e5ea; --panel:#f5f6f8;
  --user:#7c5cd6; --model:#3d6fd6; --tool:#2e8f57; --assist:#b07f2e;
  --err:#c4433c; --mark:#9aa2af; }
@media (prefers-color-scheme: dark) { :root {
  --bg:#14161b; --fg:#dde1e8; --dim:#8b93a1; --line:#2a2e37; --panel:#1c1f26; } }
body { background:var(--bg); color:var(--fg);
  font:13px/1.45 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;
  margin:0; padding:1.2rem 1.6rem 4rem; }
h1 { font-size:1.05rem; margin:0 0 .2rem; }
.meta,.totals { color:var(--dim); margin:.15rem 0; }
.totals b { color:var(--fg); font-weight:600; }
#q { width:100%; max-width:34rem; margin:.8rem 0; padding:.35rem .55rem;
  background:var(--panel); color:var(--fg); border:1px solid var(--line);
  border-radius:5px; font:inherit; }
.turn { border-top:4px solid var(--line); margin-top:1.4rem; padding-top:.5rem; }
.turn-head { display:flex; gap:.8rem; flex-wrap:wrap; align-items:baseline; }
.turn-head .status { color:var(--dim); }
.turn-head .status.err { color:var(--err); }
.overview { position:relative; height:26px; margin:.6rem 0 .9rem;
  background:var(--panel); border:1px solid var(--line); border-radius:4px;
  overflow:hidden; }
.overview .sp { position:absolute; height:10px; border-radius:2px;
  min-width:3px; opacity:.85; }
.overview .sp.model { top:3px;  background:var(--model); }
.overview .sp.tool  { top:14px; background:var(--tool); }
.overview .sp.err   { background:var(--err); }
.overview .sp.open  { width:3px; background:var(--mark); }
.omitted { color:var(--dim); font-style:italic; padding:.3rem 0; }
details.row { border-bottom:1px solid var(--line); }
details.row > summary { display:flex; gap:.7rem; padding:.28rem .2rem;
  cursor:pointer; list-style:none; align-items:baseline; }
details.row > summary::-webkit-details-marker { display:none; }
details.row[open] { background:var(--panel); }
.idx { color:var(--dim); min-width:3.2rem; text-align:right; }
.off { color:var(--dim); min-width:4.2rem; }
.badge { min-width:6.2rem; font-weight:600; }
.kind-user .badge,.kind-steer .badge{color:var(--user)} .kind-model .badge{color:var(--model)}
.kind-tool .badge{color:var(--tool)} .kind-assistant .badge{color:var(--assist)}
.kind-error .badge,.row.err .badge{color:var(--err)}
.kind-compaction .badge,.kind-event .badge,.kind-final .badge,
.kind-reference .badge{color:var(--dim)}
.kind-reference summary{opacity:.75}
.prev { flex:1; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;
  color:var(--fg); }
.dur { color:var(--dim); white-space:nowrap; }
.row.err .dur { color:var(--err); }
.step-rule { border:0; border-top:1px dashed var(--line); margin:.15rem 0; }
.step-label { color:var(--dim); font-size:.85em; padding-left:3.9rem; }
.agent-chip { color:var(--dim); font-size:.85em; border:1px solid var(--line);
  border-radius:3px; padding:0 .3rem; white-space:nowrap; }
.overview .sp.nested { opacity:.45; }
.inspector { padding:.4rem .6rem .8rem 3.9rem; }
.inspector h4 { margin:.7rem 0 .2rem; font-size:.85em; color:var(--dim);
  text-transform:uppercase; letter-spacing:.05em; }
.inspector pre { margin:0; padding:.5rem .6rem; background:var(--bg);
  border:1px solid var(--line); border-radius:4px; overflow-x:auto;
  white-space:pre-wrap; word-break:break-word; }
"""

_FILTER_JS = """
document.getElementById('q').addEventListener('input', function () {
  var needle = this.value.toLowerCase();
  document.querySelectorAll('details.row').forEach(function (row) {
    row.style.display = !needle ||
      row.textContent.toLowerCase().indexOf(needle) !== -1 ? '' : 'none';
  });
});
"""


def _overview(ledger: dict) -> list[str]:
    base, end = ledger.get("started_at"), ledger.get("ended_at")
    if base is None or end is None:
        return []
    total = max(float(end) - float(base), 1e-9)
    parts = ['<div class="overview">']
    for row in ledger["rows"]:
        if row.get("kind") not in ("model", "tool") or row.get("ts") is None:
            continue
        left = max(0.0, (float(row["ts"]) - float(base)) / total * 100)
        classes = f"sp {row['kind']}"
        if row.get("depth"):
            classes += " nested"
        title = _esc(f"{row['label']} · {_fmt_ms(row.get('duration_ms'))}")
        if row.get("duration_ms") is None:
            # An open span gets a start marker; a bar would be an invented
            # duration for work whose end was never recorded.
            parts.append(
                f'<div class="{classes} open" style="left:{left:.2f}%" '
                f'title="{title}"></div>'
            )
            continue
        if row.get("error"):
            classes += " err"
        width = max(float(row["duration_ms"]) / 1000 / total * 100, 0.25)
        parts.append(
            f'<div class="{classes}" style="left:{left:.2f}%;'
            f'width:{min(width, 100 - left):.2f}%" title="{title}"></div>'
        )
    parts.append("</div>")
    return parts


def _inspector(row: dict) -> list[str]:
    parts = ['<div class="inspector">']
    if row.get("depth"):
        parts.append(
            f"<h4>Agent</h4><pre>{_esc(row.get('agent'))} "
            f"(delegation depth {row['depth']})</pre>"
        )
    for name, value in (row.get("detail") or {}).items():
        if value is None:
            continue
        parts.append(f"<h4>{_esc(name)}</h4>")
        parts.append(f"<pre>{_esc(_capped(_pretty(value), INSPECTOR_CHARS))}</pre>")
    parts.append("</div>")
    return parts


def _row_html(row: dict, base) -> list[str]:
    if row.get("kind") == "step":
        return [
            '<hr class="step-rule">',
            f'<div class="step-label">{_esc(row["label"])}</div>',
        ]
    classes = f"row kind-{row['kind']}" + (" err" if row.get("error") else "")
    seq = row.get("seq")
    dur = ""
    if "duration_ms" in row:
        dur = _fmt_ms(row.get("duration_ms"))
    elif row.get("status"):
        dur = str(row["status"])
    depth = int(row.get("depth") or 0)
    indent = f' style="padding-left:{depth * 1.4:.1f}rem"' if depth else ""
    parts = [f'<details class="{classes}"{indent}>', "<summary>"]
    parts.append(f'<span class="idx">{seq if seq is not None else ""}</span>')
    parts.append(f'<span class="off">{_esc(_fmt_offset(row.get("ts"), base))}</span>')
    parts.append(f'<span class="badge">{_esc(row["label"])}</span>')
    if depth and row.get("agent"):
        parts.append(f'<span class="agent-chip">{_esc(row["agent"])}</span>')
    parts.append(f'<span class="prev">{_esc(_preview(row.get("content", "")))}</span>')
    if dur:
        parts.append(f'<span class="dur">{_esc(dur)}</span>')
    parts.append("</summary>")
    parts.extend(_inspector(row))
    parts.append("</details>")
    return parts


def render_html(ledgers: list[dict], *, title: str = "mini-loop trace") -> str:
    """Render one page: a `.turn` section per ledger, dsh-style."""
    parts = [
        "<!doctype html><html><head><meta charset='utf-8'>",
        f"<title>{_esc(title)}</title>",
        "<meta name='viewport' content='width=device-width,initial-scale=1'>",
        f"<style>{_CSS}</style></head><body>",
        f"<h1>{_esc(title)}</h1>",
    ]
    total_metrics = {"model_calls": 0, "tool_calls": 0, "tool_errors": 0,
                     "errors": 0, "input_tokens": 0, "output_tokens": 0}
    for ledger in ledgers:
        for key in total_metrics:
            total_metrics[key] += int((ledger.get("metrics") or {}).get(key) or 0)
    session = next((l.get("session") for l in ledgers if l.get("session")), "")
    parts.append(
        f'<div class="meta">session {_esc(session)} · {len(ledgers)} turn(s) · '
        f'generated {time.strftime("%Y-%m-%d %H:%M:%S")}</div>'
    )
    parts.append(
        '<div class="totals">'
        + " · ".join(f"{k.replace('_', ' ')} <b>{v:,}</b>"
                     for k, v in total_metrics.items())
        + "</div>"
    )
    parts.append('<input id="q" type="search" placeholder="filter records">')

    for turn_no, ledger in enumerate(ledgers, start=1):
        status = ledger.get("status", "")
        status_cls = "status err" if status not in ("completed", "running") else "status"
        duration = (
            _fmt_ms(ledger.get("duration_ms"))
            if ledger.get("duration_ms") is not None else str(status)
        )
        parts.append('<section class="turn">')
        parts.append(
            '<div class="turn-head">'
            f'<b>turn {turn_no}</b>'
            f'<span>{_esc(ledger.get("trajectory_id") or "")}</span>'
            f'<span class="{status_cls}">{_esc(status)}'
            f'{" · partial" if ledger.get("partial") else ""}</span>'
            f'<span class="status">{_esc(duration)}</span>'
            "</div>"
        )
        parts.extend(_overview(ledger))
        if ledger.get("omitted"):
            parts.append(
                f'<div class="omitted">{ledger["omitted"]:,} earlier records '
                "omitted from this page -- export the trajectory as JSONL for "
                "the full log</div>"
            )
        base = ledger.get("started_at")
        for row in ledger["rows"]:
            parts.extend(_row_html(row, base))
        parts.append("</section>")

    parts.append(f"<script>{_FILTER_JS}</script></body></html>")
    return "".join(parts)


# -- CLI ---------------------------------------------------------------------

def assemble_file(path: Path) -> dict:
    """Assemble a raw trajectory JSONL file without a store.

    Mirrors `TrajectoryStore.get()` for a file that may live outside any
    store root (an export, a copy pulled off another machine). Liveness is
    unknowable from a bare file, so a missing end record reads as
    `interrupted` -- the same value the store reports for a dead process.
    """
    records, partial = [], False
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line.strip():
            continue
        try:
            records.append(json.loads(line))
        except json.JSONDecodeError:
            partial = True
    if not records or records[0].get("record_type") != "trajectory_start":
        raise ValueError(f"{path} is not a trajectory JSONL file")
    start = records[0]
    events = [r for r in records if r.get("record_type") == "event"]
    end = next(
        (r for r in reversed(records) if r.get("record_type") == "trajectory_end"),
        None,
    )
    return {
        "trajectory_id": start.get("trajectory_id", path.stem),
        "session": start.get("session"),
        "run_index": start.get("run_index"),
        "status": end.get("status", "completed") if end else "interrupted",
        "started_at": start.get("started_at"),
        "ended_at": end.get("ended_at") if end else None,
        "duration_ms": end.get("duration_ms") if end else None,
        "input": start.get("input"),
        "output": end.get("output") if end else None,
        "error": end.get("error") if end else None,
        "metrics": end.get("metrics") if end else {},
        "events": events,
        "partial": partial or end is None,
    }


def _resolve_ledgers(target: str, root: Path | None) -> list[dict]:
    as_path = Path(target)
    if as_path.is_file():
        return [build_ledger(assemble_file(as_path))]
    if root is None:
        from .config import Settings

        settings = Settings()
        root = settings.trajectory_root or (
            settings.workspace_root / ".trajectories"
        )
    from .trajectory import TrajectoryStore

    store = TrajectoryStore(root)
    if target.startswith("traj_"):
        return [build_ledger(store.get(target))]
    summaries = store.list(session_id=target, limit=500)
    if not summaries:
        raise SystemExit(
            f"no trajectory file and no recorded session '{target}' under {root}"
        )
    summaries.sort(key=lambda s: (s.get("started_at") or 0))
    return [build_ledger(store.get(s["trajectory_id"])) for s in summaries]


def main(argv: list[str] | None = None) -> int:
    import argparse

    parser = argparse.ArgumentParser(
        prog="python -m mini_loop.trace_view",
        description="Render a recorded trajectory (or a session's runs) as a "
                    "self-contained HTML ledger.",
    )
    parser.add_argument(
        "target", help="a trajectory .jsonl file, a traj_* id, or a session id"
    )
    parser.add_argument("-o", "--output", type=Path, default=None)
    parser.add_argument(
        "--root", type=Path, default=None,
        help="trajectory store root (default: the configured one)",
    )
    args = parser.parse_args(argv)

    ledgers = _resolve_ledgers(args.target, args.root)
    page = render_html(ledgers, title=f"mini-loop trace · {args.target}")
    out = args.output or Path(f"{Path(args.target).stem}.trace.html")
    # 0600 like the trajectory files themselves: the page carries the same
    # prompts and tool output the recording does, so it inherits their mode.
    fd = os.open(out, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as handle:
        handle.write(page)
    print(out)
    return 0


if __name__ == "__main__":  # pragma: no cover
    raise SystemExit(main())

#: The module's runtime-invariant posture (tools/verify_invariants.py).
NO_RUNTIME_INVARIANT = (
    "No runtime invariant: the viewer is a read-side projection of the "
    "recorded trajectory log; it renders already-masked store rows into "
    "static HTML and feeds nothing back into the agent loop."
)
