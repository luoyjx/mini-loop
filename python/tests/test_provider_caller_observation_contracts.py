import copy
import json
import math

import pytest

from tools.provider_caller_observation_contracts import provider_caller_observation_contracts


@pytest.fixture(scope="module")
def caller_observations():
    return provider_caller_observation_contracts()


def _assert_observation(actual, expected):
    assert type(actual) is type(expected)
    if isinstance(expected, float) and math.isnan(expected):
        assert math.isnan(actual)
    elif isinstance(expected, dict):
        assert actual.keys() == expected.keys()
        for key in expected:
            _assert_observation(actual[key], expected[key])
    elif isinstance(expected, list):
        assert len(actual) == len(expected)
        for got, want in zip(actual, expected):
            _assert_observation(got, want)
    else:
        assert actual == expected


def _expected_caller(value):
    value = copy.deepcopy(value)
    if isinstance(value, dict):
        value.setdefault("type", None)
        if value["type"] in ("code_execution_20250825", "code_execution_20260120"):
            value.setdefault("tool_id", None)
    elif isinstance(value, list):
        # The first union variant constructs immediate mapping elements as
        # DirectCaller. It does not redispatch code tags or traverse nested lists.
        for item in value:
            if isinstance(item, dict):
                item.setdefault("type", None)
    return value


def test_actual_sdk_caller_defaults_and_complete_direct_stream_shapes(caller_observations):
    fixture = caller_observations
    assert fixture["sdk_version"] == "0.107.1"
    assert fixture["pydantic_version"] == "2.13.4"
    assert len(fixture["source_sha256"]) == 12
    assert all(len(value) == 64 for value in fixture["source_sha256"].values())
    assert len(fixture["sdk_schema_sha256"]) == 8
    assert all(len(value) == 64 for value in fixture["sdk_schema_sha256"].values())
    assert len(fixture["profile_names"]) == 28
    assert len(fixture["cases"]) == 224
    assert {(row["name"], row["mode"]) for row in fixture["cases"]} == {
        (kind + "/" + profile, mode)
        for kind in fixture["block_kinds"]
        for profile in fixture["profile_names"]
        for mode in fixture["modes"]
    }
    # The fixture itself stays standard JSON even when its observed wire data is not.
    json.dumps(fixture, allow_nan=False)
    classes = {"tool_use": "ToolUseBlock", "server_tool_use": "ServerToolUseBlock",
               "web_search_tool_result": "WebSearchToolResultBlock", "web_fetch_tool_result": "WebFetchToolResultBlock"}
    for row in fixture["cases"]:
        source = json.loads(row["response_json"])["content"]
        expected = copy.deepcopy(source)
        expected[0]["caller"] = _expected_caller(source[0]["caller"])
        expected[1]["citations"] = None
        kind = source[0]["type"]
        expected_class = classes[kind]
        if row["mode"] == "stream":
            expected[1]["parsed_output"] = None
            if kind == "web_fetch_tool_result":
                expected_class = "ParsedTextBlock"
                expected[0].update(text=None, citations=None, parsed_output=None)
        assert row["sdk_block_class"] == expected_class
        observed = json.loads(row["expected_content_json"])
        _assert_observation(observed, expected)
        if row["name"].endswith("/root-mixed-list"):
            # Only immediate list mappings become DirectCaller. Nested lists
            # retain sparse dictionaries without type/tool_id defaults.
            assert observed[0]["caller"][-1] == [{}, {"type": "code_execution_20250825"}]


def test_actual_sdk_forwarding_retains_finite_callers_and_rejects_nonfinite_before_transport(caller_observations):
    rows = caller_observations["forward_cases"]
    assert len(rows) == 8
    assert {(row["name"], row["mode"]) for row in rows} == {
        ("tool_use/" + profile, mode)
        for profile in ("unknown-type", "root-mixed-list", "code-2025-id-nan", "unknown-extension-nonfinite")
        for mode in ("direct", "stream")
    }
    for row in rows:
        finite = row["name"].endswith(("unknown-type", "root-mixed-list"))
        assert row["succeeded"] is finite
        assert row["transport_requests"] == (2 if finite else 1)
        if finite:
            assert row["error_class"] is None and row["error_message"] is None
            forwarded = json.loads(row["forwarded_request_json"])
            _assert_observation(forwarded["messages"], json.loads(row["request_messages_json"]))
            _assert_observation(forwarded["messages"][0]["content"], json.loads(row["content_json"]))
        else:
            assert row["forwarded_request_json"] is None
            assert row["error_class"] == "ValueError"
            assert "Out of range float values are not JSON compliant" in row["error_message"]


def test_actual_agent_executes_safe_tool_before_nonfinite_caller_forwarding_failure(caller_observations):
    rows = caller_observations["agent_runs"]
    assert {row["name"] for row in rows} == {
        "root-mixed-list", "code-2025-id-nan", "unknown-extension-nonfinite",
    }
    action_ids = set()
    for row in rows:
        finite = row["name"] == "root-mixed-list"
        assert row["error_class"] is None and row["error_message"] is None
        assert row["transport_requests"] == (2 if finite else 1)
        assert row["trace"][:8] == ["transport", "tool_use", "before", "guard", "handler", "after", "result", "tool_result"]
        assert row["trace"][8:] == (["transport"] if finite else ["error"])
        assert len(row["handler_calls"]) == 1
        handled = row["handler_calls"][0]
        assert (handled["id"], handled["name"], handled["value"]) == ("actual-use", "safe_probe", "safe-value")
        assert handled["permission_mode"] == "auto"
        assert handled["run_context"]["authority"] == "peer_agent"
        assert handled["run_context"]["approved_capabilities"] == []
        action_ids.add(handled["action_id"])
        assert json.loads(row["result_json"]) == [{"id": "actual-use", "name": "safe_probe", "output": "safe output", "denied": False, "failed": False}]
        history = json.loads(row["history_json"])
        uses = [block for message in history if isinstance(message["content"], list)
                for block in message["content"] if block["type"] == "tool_use"]
        results = [block for message in history if isinstance(message["content"], list)
                   for block in message["content"] if block["type"] == "tool_result"]
        assert len(uses) == len(results) == 1
        assert uses[0]["id"] == results[0]["tool_use_id"] == "actual-use"
        _assert_observation(uses[0]["caller"], _expected_caller(json.loads(row["caller_json"])))
        assert results[0]["content"] == "safe output"
        errors = [event for event in json.loads(row["events_json"]) if event["type"] == "error"]
        if finite:
            assert row["final"] == "done" and errors == []
            assert uses[0]["caller"][-1] == [{}, {"type": "code_execution_20250825"}]
        else:
            assert row["final"].startswith("[Error] ValueError:")
            assert len(errors) == 1
            assert "Out of range float values are not JSON compliant" in errors[0]["error"]
    # The same trusted session/message and outer use ID produce the same action;
    # caller metadata, including NaN, is not part of execution identity.
    assert len(action_ids) == 1
