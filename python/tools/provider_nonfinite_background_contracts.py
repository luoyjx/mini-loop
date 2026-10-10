"""Actual SDK background_run observations and their Source consumer stages."""
import hashlib
import inspect
import json
from pathlib import Path

from mini_loop.storage import SQLiteStateStore, SessionRecord

import anthropic

from tools.provider_nonfinite_bash_contracts import _provider_nonfinite_shell_contracts


def provider_nonfinite_background_contracts(scratch: Path | str) -> dict:
    if anthropic.__version__ != "0.107.1":
        raise RuntimeError("Background observation contracts require anthropic 0.107.1")
    inputs = []
    for name, number in (("nan", float("nan")), ("infinity", float("inf")), ("negative-infinity", -float("inf"))):
        for shape, value in (("root", number), ("root-array", [number]),
                ("command", {"command": number}), ("command-array", {"command": [number]}),
                ("command-object", {"command": {"keep": number}}),
                ("extra", {"command": None, "extra": number})):
            inputs.append((shape + "-" + name, value))
    result = _provider_nonfinite_shell_contracts(Path(scratch), inputs, tool_name="background_run",
        rewrite_names={"root-nan", "command-nan", "extra-nan"},
        rewrite_command="printf background-input-rewritten", background=True)

    for row in result["cases"]:
        if row["mode"] != "keep":
            continue
        path = Path(scratch) / ("nonfinite-background-" + row["name"] + ".sqlite3")
        store = SQLiteStateStore(path)
        try:
            store.upsert_session(SessionRecord("session", str(scratch), None, 1.0, 0, "idle", 0))
            store.append_messages("session", [{"role": "assistant", "content": json.loads(row["content_json"])}])
        finally:
            store.close()
        reopened = SQLiteStateStore(path)
        try:
            row["reopened_messages_json"] = json.dumps(reopened.load_messages("session"), sort_keys=True)
        finally:
            reopened.close()
    result["source_sha256"]["SQLiteStateStore.append_messages"] = hashlib.sha256(inspect.getsource(SQLiteStateStore.append_messages).encode()).hexdigest()
    return result
