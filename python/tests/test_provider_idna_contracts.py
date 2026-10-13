import json
import socket
from pathlib import Path

from tools.provider_idna_contracts import provider_idna_contracts
import tools.provider_url_contracts as url_contracts


def test_actual_provider_idna_requests_match_frozen_contract(monkeypatch):
    network_attempts = []

    def deny_network(*args, **kwargs):
        network_attempts.append((args, kwargs))
        raise AssertionError("hostname contracts must use MockTransport without live network")

    monkeypatch.setattr(socket.socket, "connect", deny_network)
    monkeypatch.setattr(socket.socket, "connect_ex", deny_network)
    original_urls = url_contracts.URLS
    observed = provider_idna_contracts()
    assert url_contracts.URLS is original_urls
    assert network_attempts == []
    root = Path(__file__).resolve().parents[2]
    assert observed == json.loads((root / "go/testdata/python-provider-idna.json").read_text())
    assert observed["sdk_version"] == "0.107.1"
    assert observed["httpx_version"] == "0.28.1"
    assert observed["idna_version"] == "3.18"
    assert observed["idna_table_unicode_version"] == "17.0.0"
    assert observed["unicode_version"] == "14.0.0"
    assert observed["python_implementation"] == "CPython"
    assert len(observed["cases"]) == 84
    rows = {(row["name"], row["mode"]): row for row in observed["cases"]}
    assert len(rows) == 84

    for mode in ("direct", "stream"):
        for name in ("unicode-host", "punycode-host"):
            assert rows[name, mode]["stage"] == "complete"
            assert rows[name, mode]["error"] is None
        assert rows["unicode-host", mode]["calls"] == rows["punycode-host", mode]["calls"]
        for name, stage in {
            "ascii-underscore": "complete",
            "unicode-with-ascii-underscore": "constructor",
            "ascii-long-label": "complete",
            "label-64-with-unicode": "constructor",
            "nfc-composed": "complete",
            "nfc-decomposed": "constructor",
            "lower-becomes-ascii-with-invalid-label": "constructor",
            "numeric-label-before-rtl": "complete",
            "rtl-mixed-numeral-directions": "constructor",
            "middle-dot-context": "complete",
            "middle-dot-outside-context": "constructor",
            "newer-unicode-table-codepoint": "constructor",
        }.items():
            assert rows[name, mode]["stage"] == stage, (name, mode)
        malformed = rows["ascii-malformed-alabel", mode]
        assert malformed["stage"] == "request"
        assert malformed["error"] == {
            "type": "InvalidCodepoint",
            "text": "Codepoint U+0080 at position 1 of '\\x80' not allowed",
        }
        assert malformed["calls"] == []

    stage_counts = {"complete": 0, "constructor": 0, "request": 0}
    for row in observed["cases"]:
        stage_counts[row["stage"]] += 1
        assert row["identity"]["endpoint"] == row["base_url"]
        if row["error"] is not None:
            if row["name"] != "ascii-malformed-alabel":
                assert row["stage"] == "constructor"
                assert row["error"]["type"] == "InvalidURL"
            assert row["calls"] == []
        else:
            assert row["stage"] == "complete"
            assert row["text"] == "ok"
            assert len(row["calls"]) == 1
            call = row["calls"][0]
            assert call["method"] == "POST"
            assert call["api_key"] == "fixture-key"
            assert call["authorization"] is None
            assert call["raw_path"] == "/tenant/v1/messages"
    assert stage_counts == {"complete": 48, "constructor": 34, "request": 2}
    assert set(observed["source_sha256"]) == {
        "mini_loop/providers.py", "tools/provider_endpoint_contracts.py",
        "tools/provider_url_contracts.py", "tools/provider_idna_contracts.py",
        "anthropic/_base_client.py", "httpx/_urlparse.py", "idna/core.py",
        "idna/idnadata.py", "idna/intranges.py",
    }
    assert all(len(value) == 64 for value in observed["source_sha256"].values())
