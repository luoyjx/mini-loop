"""Pinned jiter edge contracts for the SDK's partial_mode=True call."""
import json
from importlib.metadata import version

from jiter import from_json


def provider_partial_json_contracts() -> dict:
    if version("jiter") != "0.15.0":
        raise RuntimeError("partial JSON contracts require jiter 0.15.0")
    profiles = [("case-" + str(i), raw) for i, raw in enumerate([
        '{"a":1]', '{"a":1 x', '{"a":1 ?', '{"a":1:2}', '{"a":1 "b":2}',
        '{"a":1,}', '{"a":1,,', '{"a":1,?', '{"a":1,"b"?', '{"a":1,"b":x',
        '[1}', '[1 x', '[1,]', '[1,,', '[1,?', '1x', 'trueX', 'tru', '-',
        '{"a":"\\ud800"}', '{"a":"\\ud800', '{"a":"\\ud800\\u',
        '{"a":"\\ud800\\u0041"}', '{"a":"\\udc00"}', '{"a":"\\ud83d\\ude00"}',
        '{"a":"\\u12x', '{"a":"\\u12', '{"a":"\\x',
        '{"a":"名称🙂"}', '{"a":"名称🙂', '{"a":"\\n\\t\\\\\\\""}',
        '{"a":-Inf', '{"a":1e400}', '{"a":-Infinity}', '{"a":0.01}', '{"a":-01}',
    ])]
    profiles += [("depth-" + str(n) + suffix, '[' * n + tail) for n in (199, 200, 201) for suffix, tail in (("-open", ""), ("-complete", '0' + ']' * n))]
    rows = []
    for name, raw in profiles:
        row = {"name": name, "raw": raw}
        try:
            value = from_json(raw.encode(), partial_mode=True)
            row["expected_json"] = json.dumps(value, ensure_ascii=True, sort_keys=True)
        except ValueError as error:
            row["error"] = {"class": type(error).__name__, "message": str(error)}
        rows.append(row)
    return {"jiter_version": version("jiter"), "cases": rows}
