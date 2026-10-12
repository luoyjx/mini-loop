"""Capture CPython JSON text bytes compactly for native large-span checks."""
import hashlib
import json


def json_text_span_contracts():
    seeds = [
        ("empty", "", 1),
        ("ascii-large", "m", 524288),
        ("escaped-large", "m" * 4096 + '"\\\n', 128),
        ("unicode-large", "中😀\u2028<>&", 32768),
        ("controls", "".join(map(chr, range(32))) + '"\\/', 16),
        ("del-ascii", "a\x7fb", 128),
        ("scalar-edges", "é\x7f\u0080\u07ff\u0800\uffff\U00010000\U0010ffff", 1),
    ]
    cases = []
    for name, seed, repeat in seeds:
        text = seed * repeat
        for object_key in (False, True):
            value = {text: text} if object_key else text
            projections = {}
            for profile, ascii_only, spaced in (
                ("utf8", False, False), ("spaced", False, True),
                ("archive", True, False),
                ("ascii_spaced", True, True),
                ("legacy_pretty", False, "pretty"),
            ):
                encoded = json.dumps(value, ensure_ascii=ascii_only, allow_nan=False,
                                     indent=2 if spaced == "pretty" else None,
                                     separators=None if spaced else (",", ":")).encode("utf-8")
                projections[profile] = {"bytes": len(encoded), "sha256": hashlib.sha256(encoded).hexdigest()}
            cases.append({"name": name, "seed": seed, "repeat": repeat,
                          "object_key": object_key, "projections": projections})
    legacy_cases = []
    for name, text in (("high", "\ud800"), ("low", "a\udfffz"),
                       ("pair-codepoints", "\ud800\udfff"), ("replacement", "\ufffd")):
        for object_key in (False, True):
            value = {text: text} if object_key else text
            projections = {}
            for profile, pretty in (("compact", False), ("pretty", True)):
                encoded = json.dumps(value, ensure_ascii=False,
                                     indent=2 if pretty else None,
                                     separators=None if pretty else (",", ":")).encode("utf-8", "surrogatepass")
                projections[profile] = {"bytes": len(encoded), "sha256": hashlib.sha256(encoded).hexdigest()}
            legacy_cases.append({"name": name, "input_utf8_hex": text.encode("utf-8", "surrogatepass").hex(),
                                 "object_key": object_key, "projections": projections})
    return {"source": "CPython json.dumps; UTF-8/surrogatepass encoding; allow_nan=False",
            "cases": cases, "legacy_cases": legacy_cases}
