"""Pin actual Source fault phases and effect ordering, not native acceptance."""
import json
from pathlib import Path

from tools.mcp_mapping_key_contracts import mcp_mapping_key_contracts, mcp_mapping_key_journal_contracts
from tools.mcp_model_root_contracts import mcp_model_root_contracts


def test_source_mapping_key_phases_and_prior_effects():
    rows = mcp_mapping_key_contracts()["cases"]
    assert len(rows) == 116
    cases = {row["name"]: row for row in rows}
    scalar = cases["auto/integer-key/batch"]
    assert scalar["error"] is None
    assert scalar["calls"] == [{"first": 1}]
    assert scalar["trace"] == ["before", "handler", "after", "result", "before", "after", "result"]
    assert scalar["results"][-1] == {
        "output": "Error: keywords must be strings", "denied": False, "failed": True,
    }
    readonly = cases["readonly/integer-key/batch"]
    assert readonly["error"] is None and readonly["calls"] == []
    assert readonly["trace"] == ["before", "result", "before", "result"]
    assert all(result["denied"] and not result["failed"] for result in readonly["results"])
    for name, kind in [("array-key", "list"), ("object-key", "dict")]:
        row = cases[f"auto/{name}/batch"]
        assert row["error"] == {"kind": "TypeError", "message": f"unhashable type: '{kind}'"}
        assert row["trace"] == row["calls"] == row["results"] == []
    mixed = cases["auto/text-then-integer/batch"]
    assert mixed["calls"] == [{"first": 1}]
    assert mixed["trace"][-2:] == ["before", "after"]
    assert len(mixed["results"]) == 1
    assert mixed["error"] == {"kind": "TypeError", "message": "'<' not supported between instances of 'int' and 'str'"}
    readonly_mixed = cases["readonly/text-then-integer/batch"]
    assert readonly_mixed["trace"] == ["before", "result", "before"]
    assert readonly_mixed["error"] == mixed["error"]
    assert readonly_mixed["calls"] == []
    later = cases["auto/later-short-row/batch"]
    assert later["error"] == {
        "kind": "ValueError", "message": "dictionary update sequence element #1 has length 1; 2 is required",
    }
    assert later["trace"] == []
    for name in ["nan-key", "positive-infinity-key", "negative-infinity-key"]:
        assert cases[f"auto/{name}"]["results"][-1]["output"] == "Error: keywords must be strings"
    assert all(row["model_input_unchanged"] for row in rows)
    for name in ["nan-root", "positive-infinity-root", "negative-infinity-root"]:
        row = cases[f"auto/{name}/batch"]
        assert row["error"] == {"kind": "TypeError", "message": "'float' object is not iterable"}
        assert row["trace"] == row["calls"] == []


def test_shared_capture_preserves_existing_fixture_and_new_finite_envelope():
    root = Path(__file__).resolve().parents[2]
    old = json.loads((root / "go/testdata/python-mcp-model-roots.json").read_text())
    assert mcp_model_root_contracts() == old
    current = mcp_mapping_key_contracts()
    assert current == json.loads((root / "go/testdata/python-mcp-mapping-keys.json").read_text())
    json.dumps(current, allow_nan=False)


def test_source_journal_hash_and_begin_fault_stages():
    snapshot = mcp_mapping_key_journal_contracts()
    root = Path(__file__).resolve().parents[2]
    assert snapshot == json.loads((root / "go/testdata/python-mcp-mapping-key-journal.json").read_text())
    assert len(snapshot["cases"]) == 116
    cases = {row["name"]: row for row in snapshot["cases"]}
    integer = cases["auto/integer-key/batch"]
    assert integer["actions"][-1]["status"] == "failed"
    assert integer["actions"][-1]["input_hash"] == "12b0137f6e4f4cc6df162a759f9ae58d020baa561b9e0fc5d34cf682ccef2699"
    nan = cases["auto/nan-key/batch"]
    assert nan["trace"][-2:] == ["before", "result"]
    assert nan["results"][-1]["output"] == "Error: Out of range float values are not JSON compliant"
    assert len(nan["actions"]) == 1
    mixed = cases["auto/text-then-integer/batch"]
    assert mixed["trace"][-1] == "before" and len(mixed["actions"]) == 1
    assert mixed["error"]["kind"] == "TypeError"
