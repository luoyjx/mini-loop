import json
from collections import Counter

import pytest

from tools.http_query_wire_contracts import http_query_wire_contracts


@pytest.fixture(scope="module")
def query_wire():
    return http_query_wire_contracts()


def _cases(fixture, group):
    return {row["name"].removeprefix(group + "/"): row
            for row in fixture["cases"] if row["group"] == group}


def test_actual_raw_query_matrix_preserves_bytes_and_leaves_no_effects(query_wire):
    assert set(query_wire["versions"]) == {
        "python", "fastapi", "starlette", "pydantic", "httpx", "sse-starlette",
    }
    assert all(isinstance(value, str) and value for value in query_wire["versions"].values())
    hashes = query_wire["source_sha256"]
    assert {
        "HTTPConnection.query_params", "URL.__init__", "QueryParams.__init__",
        "ImmutableMultiDict.__init__", "urllib.parse.parse_qsl", "urllib.parse.unquote",
        "urllib.parse.unquote_to_bytes", "capture_helper", "request_helper",
    } <= hashes.keys()
    assert all(len(value) == 64 and set(value) <= set("0123456789abcdef")
               for value in hashes.values())
    json.dumps(query_wire, allow_nan=False)
    rows = query_wire["cases"]
    assert len(rows) == len({row["name"] for row in rows}) == 24
    assert Counter(row["group"] for row in rows) == {"auth": 12, "limit": 12}
    assert {row["name"] for row in rows if "source_fault" in row} == {
        "auth/utf8-valid-last", "auth/utf8-truncated-last",
    }

    tails = {
        "percent-last": b"%", "bad-escape-last": b"%GG",
        "semicolon-last": b"bad;tail", "encoded-semicolon-last": b"bad%3Btail",
        "escaped-percent-last": b"%25", "utf8-valid-last": b"%E4%B8%AD",
        "utf8-truncated-last": b"%E2%82",
    }
    auth = {name: b"access_token=token-a&access_token=" + tail for name, tail in tails.items()}
    auth.update({
        "percent-first": b"access_token=%&access_token=token-a",
        "header-valid": b"access_token=token-a&access_token=%",
        "header-invalid": b"access_token=token-a",
        "encoded-key": b"access%5Ftoken=token-a",
        "foreign": b"access_token=token-b&ignored=%",
    })
    limits = {name: b"limit=2&limit=" + tail for name, tail in tails.items()}
    limits.update({
        "utf8-invalid-percent-last": b"limit=2&limit=%FF",
        "valid-literal-utf8-last": b"limit=2&limit=" + "中".encode("utf-8"),
        "percent-first": b"limit=%&limit=2",
        "encoded-key": b"limit=1&l%69mit=2",
        "foreign-invalid": b"limit=2&limit=%",
    })
    for group, expected in (("auth", auth), ("limit", limits)):
        cases = _cases(query_wire, group)
        assert set(cases) == set(expected)
        for name, raw in expected.items():
            row = cases[name]
            assert row["method"] == "GET"
            assert row["query"] == []
            assert bytes.fromhex(row["raw_query_hex"]) == raw
            assert row["effects"]["before"] == row["effects"]["after"] == {
                "sessions": 3, "events": 6, "subscribers": 0, "trajectory_files": 3,
            }, row["name"]
            assert row["request_headers"] == (
                [{"name": "Last-Event-ID", "value": "1"}] if group == "auth" else []
            )


def test_raw_auth_last_value_and_header_precedence_keep_owner_scope(query_wire):
    rows = _cases(query_wire, "auth")
    denied = {
        "percent-last", "bad-escape-last", "semicolon-last", "encoded-semicolon-last",
        "escaped-percent-last", "header-invalid",
    }
    for name in denied:
        row = rows[name]
        assert row["status"] == 401, row["name"]
        assert row["body_kind"] == "json"
        assert row["body_json"] == {"detail": "a bearer token is required"}
        assert row["response_headers"]["www-authenticate"] == "Bearer"
        assert row["events"] == []
    for name in ("utf8-valid-last", "utf8-truncated-last"):
        row = rows[name]
        assert row["status"] == 500, row["name"]
        assert row["body_kind"] == "text"
        assert row["body_text"] == "Internal Server Error"
        assert row["body_json"] is None and row["events"] == []
        assert row["source_fault"] == {
            "exception_class": "TypeError",
            "message": "comparing strings with non-ASCII characters is not supported",
            "stage": "after_response_start",
        }
    assert rows["header-valid"]["authorization"] == "Bearer token-a"
    assert rows["header-invalid"]["authorization"] == "Bearer invalid"
    assert rows["foreign"]["status"] == 404
    assert rows["foreign"]["events"] == []

    seeds = query_wire["seeds"]["events"]
    assert len(seeds) == 6
    for session in ("alice_old", "bob", "alice_new"):
        assert [row["event"]["seq"] for row in seeds if row["session"] == session] == [1, 2]
    expected = next(row["event"] for row in seeds
                    if row["session"] == "alice_old" and row["event"]["seq"] == 2)
    for name in ("percent-first", "header-valid", "encoded-key"):
        row = rows[name]
        assert row["status"] == 200, row["name"]
        assert row["body_kind"] == "sse"
        assert row["events"] == [{"id": "2", "event": "status", "data": expected}]
        assert row["effects"]["after"]["subscribers"] == 0


def test_raw_integer_errors_retain_exact_decoded_last_input(query_wire):
    rows = _cases(query_wire, "limit")
    inputs = {
        "percent-last": "%", "bad-escape-last": "%GG",
        "semicolon-last": "bad;tail", "encoded-semicolon-last": "bad;tail",
        "escaped-percent-last": "%", "utf8-valid-last": "中",
        "utf8-truncated-last": "\ufffd", "utf8-invalid-percent-last": "\ufffd",
        # QueryParams decodes raw bytes as Latin-1 before parsing percent escapes.
        "valid-literal-utf8-last": "\u00e4\u00b8\u00ad",
        "foreign-invalid": "%",
    }
    for name, value in inputs.items():
        row = rows[name]
        assert row["status"] == 422, row["name"]
        assert row["body_kind"] == "json"
        assert row["body_json"] == {"detail": [{
            "type": "int_parsing", "loc": ["query", "limit"],
            "msg": "Input should be a valid integer, unable to parse string as an integer",
            "input": value,
        }]}
        assert row["events"] == []
    # Validation runs before the route's session-owner lookup.
    assert rows["foreign-invalid"]["path"] == "/sessions/{bob}/trajectories"


def test_raw_encoded_key_and_final_valid_integer_select_owned_sessions(query_wire):
    rows = _cases(query_wire, "limit")
    for name in ("percent-first", "encoded-key"):
        row = rows[name]
        assert row["status"] == 200, row["name"]
        assert row["body_kind"] == "json"
        assert [session["id"] for session in row["body_json"]] == ["alice_new", "alice_old"]
        assert row["events"] == []
    assert rows["percent-first"]["body_json"] == rows["encoded-key"]["body_json"]
