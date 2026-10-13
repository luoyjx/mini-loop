"""Pin actual Source fault phases and effect ordering, not native acceptance."""
import json
from pathlib import Path

from tools.mcp_mapping_key_contracts import mcp_mapping_key_contracts
from tools.mcp_model_root_contracts import mcp_model_root_contracts


def test_source_mapping_key_phases_and_prior_effects():
    rows = mcp_mapping_key_contracts()["cases"]
    assert len(rows) == 100
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


def test_shared_capture_preserves_existing_fixture_and_new_finite_envelope():
    root = Path(__file__).resolve().parents[2]
    old = json.loads((root / "go/testdata/python-mcp-model-roots.json").read_text())
    assert mcp_model_root_contracts() == old
    current = mcp_mapping_key_contracts()
    assert current == json.loads((root / "go/testdata/python-mcp-mapping-keys.json").read_text())
    json.dumps(current, allow_nan=False)
