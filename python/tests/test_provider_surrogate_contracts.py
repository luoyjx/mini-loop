"""Contract checks execute the installed SDK rather than reimplementing it."""

import json

import pytest

from tools.provider_surrogate_contracts import provider_surrogate_contracts


@pytest.fixture(scope="module")
def surrogate_contracts(tmp_path_factory):
    return provider_surrogate_contracts(tmp_path_factory.mktemp("provider-surrogates"))


def test_actual_fixed_sdk_surrogate_ingress_and_next_request(surrogate_contracts):
    fixture = surrogate_contracts
    assert fixture["sdk_version"] == "0.107.1"
    assert fixture["pydantic_version"] == "2.13.4"
    assert len(fixture["profile_names"]) == 17
    assert len(fixture["cases"]) == 34
    assert {(row["name"], row["mode"]) for row in fixture["cases"]} == {
        (name, mode) for name in fixture["profile_names"] for mode in ("direct", "stream")}
    controls = {"text-pair-control", "text-replacement-control", "text-unicode-control"}
    failures = {
        ("bash-command-high", "stream"): ("reply_ingress", "ValueError", False),
        ("bash-extra-value-low", "stream"): ("reply_ingress", "ValueError", False),
        ("bash-extra-key-high", "stream"): ("reply_ingress", "ValueError", False),
        ("server-input-low", "stream"): ("reply_ingress", "ValueError", False),
        ("caller-extension-key-low", "direct"): ("reply_snapshot", "UnicodeEncodeError", True),
        ("caller-extension-key-low", "stream"): ("reply_ingress", "UnicodeEncodeError", False),
    }
    for row in fixture["cases"]:
        assert row["requests"][0]["method"] == "POST"
        assert row["requests"][0]["path"] == "/v1/messages"
        assert json.loads(row["requests"][0]["body_json"])["messages"] == [
            {"role": "user", "content": "capture"}]
        if row["name"] in controls:
            assert row["ingress_succeeded"] is True
            assert row["forward_succeeded"] is True
            assert row["failure_phase"] is None
            assert row["transport_requests"] == 2
        if row["ingress_succeeded"]:
            assert (row["name"], row["mode"]) not in failures
            assert row["sdk_reply_obtained"] is True
            assert row["retained_content_json"] == row["content_json"]
            source_block = json.loads(row["response_json"])["content"][0]
            actual_block = json.loads(row["content_json"])[0]
            for key in ("text", "thinking", "signature", "data", "file_id", "input"):
                if key in source_block:
                    assert actual_block[key] == source_block[key]
            if "caller" in source_block:
                if isinstance(source_block["caller"], dict):
                    assert all(actual_block["caller"][key] == value
                               for key, value in source_block["caller"].items())
                else:
                    assert actual_block["caller"] == source_block["caller"]
            history = json.loads(row["forward_messages_json"])
            assert history[1]["content"] == json.loads(row["content_json"])
            if row["forward_succeeded"]:
                assert row["transport_requests"] == 2
                assert json.loads(row["requests"][1]["body_json"])["messages"] == history
            else:
                assert row["forward_error_class"] == "UnicodeEncodeError"
                assert "surrogates not allowed" in row["forward_error_message"]
                assert row["failure_phase"] == "request_encode"
                assert row["transport_requests"] == 1
        else:
            assert (row["failure_phase"], row["ingress_error_class"], row["sdk_reply_obtained"]) == failures[
                (row["name"], row["mode"])]
            assert row["transport_requests"] == 1
            assert row["forward_succeeded"] is None
            assert row["content_json"] is None
        assert row["transport_requests"] == len(row["requests"])
