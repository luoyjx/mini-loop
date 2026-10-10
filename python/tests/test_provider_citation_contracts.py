"""Actual SDK transport/stream observations used to design the native adapter."""
import importlib.util
from pathlib import Path


def test_sdk_citation_capture_preserves_semantic_and_extension_data():
    path = Path(__file__).resolve().parents[1] / "tools" / "provider_citation_contracts.py"
    spec = importlib.util.spec_from_file_location("citation_contracts", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    capture = module.provider_citation_contracts()
    cases = {case["name"]: case for case in capture["completed"]}
    assert len(cases) == 17
    assert all(case["requests"] == 2 for case in cases.values())
    assert all(case["normalized"] == case["forwarded"] for case in cases.values())
    assert cases["absent"]["normalized"]["citations"] is None
    assert cases["empty"]["normalized"]["citations"] == []
    assert cases["unbounded_index"]["normalized"]["citations"][0]["document_index"] == 10**80
    assert cases["unknown_variant"]["normalized"]["citations"][0]["type"] == "future_location"
    assert cases["extra_members"]["normalized"]["citations"][0]["extension"]["secret-key"] == ["fixture-secret", 3]
    assert cases["wrong_known_field"]["normalized"]["citations"][0]["cited_text"] == 7
    assert cases["non_array"]["normalized"]["citations"] == "opaque fixture-secret"
    for stream in capture["streams"]:
        assert stream["content"][0]["text"] == "prefix tail"
        citations = stream["content"][0]["citations"]
        assert len(citations) == (6 if stream["name"] == "seed_plus_all_deltas" else 5)
        assert [event["citation"]["type"] for event in stream["observed"] if event["type"] == "citation"] == [
            "char_location", "page_location", "content_block_location",
            "web_search_result_location", "search_result_location"]
