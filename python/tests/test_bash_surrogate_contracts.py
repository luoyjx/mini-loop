"""Verify actual subprocess effects at the selected-argv filesystem boundary."""
import json
from pathlib import Path

from tools.bash_surrogate_contracts import bash_surrogate_contracts


def test_actual_bash_surrogate_contracts(tmp_path):
    actual = bash_surrogate_contracts(tmp_path)
    root = Path(__file__).resolve().parents[2]
    assert actual == json.loads((root / "go/testdata/python-bash-surrogates.json").read_text())
    assert actual["filesystem_encoding"] == "utf-8"
    assert actual["filesystem_errors"] == "surrogateescape"
    rows = {row["name"]: row for row in actual["cases"]}
    assert len(rows) == 12
    assert all(row["sandbox_calls"] == 1 and row["sandbox_received_original"] for row in rows.values())
    failures = {"high-d800", "low-dfff", "low-dc7f-before-escape", "escape-before-high", "sandbox-extra-high", "sandbox-program-high"}
    for name in failures:
        row = rows[name]
        assert row["result"] is None
        assert row["exception"]["exception_class"] == "UnicodeEncodeError"
        assert row["exception"]["encoding"] == "utf-8"
        assert row["exception"]["reason"] == "surrogates not allowed"
        assert row["marker_hex"] is None and row["argument_hex"] is None
    for name, expected in {"escape-dc80": "80", "escape-dcff": "ff",
                           "mixed-escape-unicode": "80c3a9ff", "ordinary-unicode": "用户-é-Ω".encode().hex(),
                           "sandbox-repairs-high": b"repaired".hex(), "sandbox-extra-escape": "80"}.items():
        row = rows[name]
        assert row["exception"] is None
        assert row["result"]["stdout"] == "done"
        assert row["result"]["exit_code"] == 0
        assert row["result"]["error"] is None
        assert row["marker_hex"] == b"effect".hex()
        assert row["argument_hex"] == expected
    assert rows["sandbox-extra-high"]["exception"]["argument_index"] == 4
    assert rows["sandbox-program-high"]["exception"]["argument_index"] == 0
