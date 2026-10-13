"""Actual MCP model-batch mapping-key stages; Source prerequisite only."""
from tools.mcp_model_root_contracts import capture_mcp_model_root_cases


def mcp_mapping_key_contracts() -> dict:
    recipes = [
        ("integer-key", [[1, "value"]]),
        ("negative-key", [[-1, "value"]]),
        ("float-key", [[1.5, "value"]]),
        ("true-key", [[True, "value"]]),
        ("false-key", [[False, "value"]]),
        ("null-key", [[None, "value"]]),
        ("nan-key", [[float("nan"), "value"]]),
        ("positive-infinity-key", [[float("inf"), "value"]]),
        ("negative-infinity-key", [[float("-inf"), "value"]]),
        ("array-key", [[["x"], "value"]]),
        ("empty-array-key", [[[], "value"]]),
        ("object-key", [[{"x": 1}, "value"]]),
        ("empty-object-key", [[{}, "value"]]),
        ("text-then-integer", [["x", "first"], [1, "second"]]),
        ("integer-then-text", [[1, "first"], ["x", "second"]]),
        ("null-then-text", [[None, "first"], ["x", "second"]]),
        ("true-then-text", [[True, "first"], ["x", "second"]]),
        ("float-then-text", [[1.5, "first"], ["x", "second"]]),
        ("equal-number-keys", [[1, "first"], [1.0, "second"], [True, "third"]]),
        ("integer-and-text-keys", [[1, "first"], ["1", "second"]]),
        ("empty-text-key", [["", "value"]]),
        ("keyword-text", [["class", "value"]]),
        ("spaced-text-key", [["has space", "value"]]),
        ("later-short-row", [[1, "first"], ["x"]]),
        ("later-unhashable-key", [[1, "first"], [["x"], "second"]]),
    ]
    return {
        "cases": capture_mcp_model_root_cases(recipes, batch_all=True),
        "limits": [
            "Actual Agent.run, FakeAsyncAnthropic, register_mcp and default PermissionHook; not SDK/HTTP/Stdio or arbitrary callback parity.",
            "Scalar non-text keys reach keyword binding after permission; unhashable keys fail whole-batch dict preparation before effects.",
            "Mixed-key ordering faults can happen after after_tool but before on_result. Auto batches may retain the earlier successful effect.",
            "NaN/infinity observations are encoded only inside input_json text; fixture envelope stays finite JSON.",
            "Native required comparison is pending; current native differences must remain retained, not relabelled as accepted adaptations.",
        ],
    }
