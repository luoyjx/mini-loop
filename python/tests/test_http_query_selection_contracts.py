import json
from collections import Counter

import pytest

from tools.http_query_selection_contracts import http_query_selection_contracts


@pytest.fixture(scope="module")
def query_selection():
    return http_query_selection_contracts()


def _query_values(row, name):
    return [item["value"] for item in row["query"] if item["name"] == name]


def _cases(fixture, group):
    return {row["name"].removeprefix(group + "/"): row
            for row in fixture["cases"] if row["group"] == group}


def _assert_query_error(row, field, kind):
    assert row["status"] == 422, row["name"]
    assert row["body_kind"] == "json"
    values = _query_values(row, field)
    assert values
    assert row["body_json"] == {
        "detail": [{
            "type": kind,
            "loc": ["query", field],
            "msg": {
                "int_parsing": "Input should be a valid integer, unable to parse string as an integer",
                "bool_parsing": "Input should be a valid boolean, unable to interpret input",
            }[kind],
            "input": values[-1],
        }],
    }


def test_actual_query_capture_is_finite_and_leaves_no_effects(query_selection):
    fixture = query_selection
    assert fixture["versions"]
    assert all(isinstance(version, str) and version for version in fixture["versions"].values())
    assert fixture["source_sha256"]
    assert all(len(value) == 64 and set(value) <= set("0123456789abcdef")
               for value in fixture["source_sha256"].values())
    json.dumps(fixture, allow_nan=False)
    rows = fixture["cases"]
    assert len(rows) == 99
    assert Counter(row["group"] for row in rows) == {
        "auth": 15, "sessions": 18, "session-trajectories": 12,
        "trajectories": 11, "envelope": 17, "trajectory-session": 8,
        "format": 9, "epoch": 9,
    }
    assert len({(row["group"], row["name"]) for row in rows}) == len(rows)
    events = fixture["seeds"]["events"]
    assert len(events) == 6
    for session in ("alice_old", "bob", "alice_new"):
        stored = [row["event"] for row in events if row["session"] == session]
        assert [event["seq"] for event in stored] == [1, 2]
        assert all(event["session"] == session and event["type"] == "status" for event in stored)
    for seed in fixture["seeds"]["trajectories"]:
        records = [json.loads(line) for line in seed["raw_jsonl"].splitlines()]
        assert [record["record_type"] for record in records] == ["trajectory_start", "event", "trajectory_end"]
        assert records[0]["trajectory_id"] == records[2]["trajectory_id"] == seed["id"]
        assert all(record["session"] == seed["session"] for record in records)
    for row in rows:
        assert row["effects"]["before"] == row["effects"]["after"], row["name"]
        assert row["effects"]["after"]["subscribers"] == 0, row["name"]
        assert row["effects"]["after"]["events"] == 6, row["name"]
        assert set(row["effects"]["after"]) == {"sessions", "events", "subscribers", "trajectory_files"}
        if row["method"] == "GET" and row["path"].endswith("/events"):
            assert row["request_headers"] == [{"name": "Last-Event-ID", "value": "1"}]
        elif row["method"] == "TRACE":
            assert row["request_headers"] == []


def test_integer_queries_select_last_value_before_clamp_or_epoch_lookup(query_selection):
    limits = {
        "default": 100, "one-then-two": 2, "two-then-one": 1,
        "empty-first": 2, "invalid-first": 2, "zero": 1,
        "negative": 1, "large": 500,
    }
    for group in ("sessions", "session-trajectories", "trajectories"):
        rows = _cases(query_selection, group)
        for name, limit in limits.items():
            row = rows[name]
            assert row["status"] == 200, row["name"]
            assert row["body_kind"] == "json"
            if group == "sessions":
                expected = ["alice_new", "alice_old"][:limit]
                assert [item["id"] for item in row["body_json"]] == expected
            else:
                ordered = ["trace_alice_new", "trace_alice_old"]
                if group == "trajectories":
                    # The global store limit precedes the route's owner filter.
                    ordered = ["trace_alice_new", "trace_bob", "trace_alice_old"]
                expected = [name for name in ordered[:limit] if name != "trace_bob"]
                assert [item["trajectory_id"] for item in row["body_json"]] == expected
                assert all(item["owner"] == "alice" for item in row["body_json"])
        for name in ("empty-last", "invalid-last"):
            _assert_query_error(rows[name], "limit", "int_parsing")
        for name in ("foreign-invalid", "missing-invalid"):
            if name in rows:
                _assert_query_error(rows[name], "limit", "int_parsing")

    # These spellings are observations from the pinned Pydantic runtime; do not
    # predict its Rust parser with Python int() or a second parser in the test.
    sessions = _cases(query_selection, "sessions")
    spellings = {"plus", "space", "float", "exponent", "underscore", "unicode", "large"}
    assert {name.removeprefix("integer-") for name in sessions if name.startswith("integer-")} == spellings
    for name in spellings:
        row = sessions["integer-" + name]
        assert row["status"] in (200, 422)
        if row["status"] == 200:
            ids = [item["id"] for item in row["body_json"]]
            assert ids in (["alice_new"], ["alice_new", "alice_old"])

    epochs = _cases(query_selection, "epoch")
    for name, epoch in {"default": 2, "two-last": 2, "one-last": 1, "invalid-first": 1}.items():
        row = epochs[name]
        assert row["status"] == 200
        assert row["body_json"] == {
            "session": "alice_old", "epoch": epoch, "epochs": 2,
            "messages": [{"role": "user", "content": f"epoch {epoch}"}],
        }
    for name in ("empty-last", "invalid-last", "foreign-invalid"):
        _assert_query_error(epochs[name], "epoch", "int_parsing")
    for name, epoch in (("zero", 0), ("unavailable", 3)):
        assert epochs[name]["status"] == 404
        assert epochs[name]["body_json"] == {"detail": f"no epoch {epoch} (current: 2)"}


def test_boolean_queries_drive_real_sse_and_disconnect_cleanup(query_selection):
    rows = _cases(query_selection, "envelope")
    true_names = {"true-last", "empty-first", "uppercase", "one", "yes", "on"}
    false_names = {"default", "false-last", "invalid-first", "zero", "off"}
    for name in true_names | false_names:
        row = rows[name]
        assert row["status"] == 200, row["name"]
        assert row["body_kind"] == "sse"
        assert len(row["events"]) == 1
        frame = row["events"][0]
        assert frame["id"] == "2"
        assert frame["event"] == ("agent_event" if name in true_names else "status")
        assert frame["data"]["seq"] == 2
        assert frame["data"]["session"] == "alice_old"
        assert frame["data"]["type"] == "status"
        assert frame["data"]["status"] == "idle"
        assert row["effects"]["after"]["subscribers"] == 0
    for name in ("empty-last", "invalid-last", "whitespace", "foreign-invalid", "missing-invalid"):
        _assert_query_error(rows[name], "envelope", "bool_parsing")
    assert rows["foreign-valid"]["status"] == 404


def test_query_authentication_and_owner_selection_preserve_admission_order(query_selection):
    auth = _cases(query_selection, "auth")
    statuses = {
        "valid-last": 405, "invalid-last": 401, "empty-last": 401,
        "empty-first": 405, "header-valid": 405, "header-invalid": 401,
        "header-empty": 405, "unknown-events-valid": 404,
        "unknown-events-empty": 401, "non-events-query": 401,
        "trailing-events-query": 401, "missing-query": 401,
        "owner-query-last": 200, "foreign-query-last": 404, "header-foreign": 404,
    }
    assert set(auth) == set(statuses)
    for name, status in statuses.items():
        row = auth[name]
        assert row["status"] == status, row["name"]
        if status == 401:
            assert row["response_headers"]["www-authenticate"] == "Bearer"
        if status == 405:
            assert row["response_headers"]["allow"] == "GET"
    owner_stream = auth["owner-query-last"]
    assert owner_stream["body_kind"] == "sse"
    assert owner_stream["events"][0]["data"]["session"] == "alice_old"
    assert owner_stream["events"][0]["event"] == "status"
    unauthenticated = _cases(query_selection, "sessions")["unauthenticated-invalid"]
    assert unauthenticated["status"] == 401
    assert "detail" in unauthenticated["body_json"]
    assert not isinstance(unauthenticated["body_json"]["detail"], list)

    selected = _cases(query_selection, "trajectory-session")
    for name in ("owned", "owned-last", "empty-first"):
        row = selected[name]
        assert row["status"] == 200
        assert [item["trajectory_id"] for item in row["body_json"]] == ["trace_alice_new", "trace_alice_old"]
        assert all(item["owner"] == "alice" and item["session"] == "alice_old"
                   for item in row["body_json"])
    for name in ("foreign", "empty", "foreign-last", "empty-last", "missing"):
        assert selected[name]["status"] == 404


def test_export_format_selects_last_value_before_ownership(query_selection):
    rows = _cases(query_selection, "format")
    baseline = rows["default"]["body_json"]
    for name in ("default", "json-last", "empty-first", "invalid-first"):
        row = rows[name]
        assert row["status"] == 200
        assert row["body_kind"] == "json"
        assert row["body_json"] == baseline
        assert row["response_headers"]["content-disposition"] == 'attachment; filename="trace_alice_new.json"'
    assert baseline["trajectory_id"] == "trace_alice_new"
    assert baseline["owner"] == "alice" and baseline["session"] == "alice_old"
    jsonl = rows["jsonl-last"]
    assert jsonl["status"] == 200 and jsonl["body_kind"] == "jsonl"
    assert jsonl["response_headers"]["content-disposition"] == 'attachment; filename="trace_alice_new.jsonl"'
    records = jsonl["body_json"]
    assert [record["record_type"] for record in records] == ["trajectory_start", "event", "trajectory_end"]
    assert records[0]["trajectory_id"] == records[2]["trajectory_id"] == "trace_alice_new"
    assert records[0]["owner"] == records[2]["owner"] == "alice"
    assert records[1] == baseline["events"][0]
    for name in ("empty-last", "invalid-last", "foreign-invalid"):
        assert rows[name]["status"] == 400
        assert rows[name]["body_json"] == {"detail": "format must be 'json' or 'jsonl'"}
    assert rows["foreign-valid"]["status"] == 404
