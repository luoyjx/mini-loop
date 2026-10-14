"""Export raw-codec observations from fixed, offline CPython 3.11.3.

This does not generate Go implementation code. The native implementation is
independently derived from RFC 3492; these observations distinguish its scalar
and finite-work contract from Python's unrestricted str/raw-codec contract.
The reference implementation is PSF-licensed CPython, Lib/encodings/punycode.py.
"""

from __future__ import annotations

import argparse
import encodings.punycode
import hashlib
import json
import platform
import random
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
TARGET = ROOT / "go/testdata/python-punycode.json"
SOURCE_SHA256 = "34edc8fb1c50e4d1cbaa1e008bb491cd7c12116c316e51974f333fe7b628eb7c"
MAX_CODE_POINTS = 4096
MAX_PAYLOAD_BYTES = 32768


def scalar_text(text: str) -> bool:
    return all(not 0xD800 <= ord(char) <= 0xDFFF for char in text)


def encode_inputs() -> list[tuple[str, str]]:
    cases = [
        ("empty", ""),
        ("ascii", "abc"),
        ("ascii-case", "AbC"),
        ("delimiter", "-"),
        ("multiple-delimiters", "--a--"),
        ("ascii-controls", "\x00A\t\r\n\x7f"),
        ("all-ascii", "".join(map(chr, range(128)))),
        ("bucher", "b\u00fccher"),
        ("manana", "ma\u00f1ana"),
        ("combining", "a\u0308\u00e4"),
        ("chinese", "\u4ed6\u4eec\u4e3a\u4ec0\u4e48\u4e0d\u8bf4\u4e2d\u6587"),
        ("emoji", "a\U0001f600\U0001f680z"),
        ("basic-case-retained", "A\u00fcB\u00fc-"),
        ("repeated", "\u0080" * 254),
    ]
    extremes = [0, 0x7F, 0x80, 0xFF, 0x100, 0x7FF, 0x800, 0xD7FF,
                0xE000, 0xFDD0, 0xFFFE, 0xFFFF, 0x10000, 0x10FFFE, 0x10FFFF]
    cases.extend((f"scalar-{point:06x}", chr(point)) for point in extremes)
    cases.append(("extremes-ascending", "".join(map(chr, extremes))))
    cases.append(("extremes-descending", "".join(map(chr, reversed(extremes)))))
    randomizer = random.Random(3492)
    for number in range(96):
        points = []
        for index in range(1 + number % 48):
            point = randomizer.randrange(0x110000)
            if 0xD800 <= point <= 0xDFFF:
                point += 0x800
            if index % 5 == 0:
                point = randomizer.randrange(128)
            points.append(point)
        cases.append((f"seed-3492-{number:02d}", "".join(map(chr, points))))
    for name, points in [
        ("surrogate-high", [0xD800]),
        ("surrogate-high-end", [0xDBFF]),
        ("surrogate-low", [0xDC00]),
        ("surrogateescape-80", [0xDC80]),
        ("surrogateescape-ff", [0xDCFF]),
        ("surrogate-low-end", [0xDFFF]),
        ("surrogate-pair", [0xD800, 0xDC00]),
        ("surrogate-mixed", [ord("a"), 0xD800, ord("Z")]),
    ]:
        cases.append((name, "".join(map(chr, points))))
    return cases


def decode_observation(name: str, payload: bytes) -> dict:
    try:
        result = payload.decode("punycode", errors="strict")
    except UnicodeError as error:
        return {
            "name": name,
            "payload_hex": payload.hex(),
            "source_code_points": [],
            "source_error_class": type(error).__name__,
            "source_error_message": str(error),
            "native_accept": False,
        }
    return {
        "name": name,
        "payload_hex": payload.hex(),
        "source_code_points": list(map(ord, result)),
        "source_error_class": "",
        "source_error_message": "",
        "native_accept": scalar_text(result) and len(result) <= MAX_CODE_POINTS
            and len(payload) <= MAX_PAYLOAD_BYTES,
    }


def render() -> str:
    if platform.python_implementation() != "CPython" or sys.version_info[:3] != (3, 11, 3):
        raise RuntimeError("Punycode reference export requires CPython 3.11.3")
    source = Path(encodings.punycode.__file__)
    source_hash = hashlib.sha256(source.read_bytes()).hexdigest()
    if source_hash != SOURCE_SHA256:
        raise RuntimeError("CPython 3.11.3 Punycode reference source hash differs")
    encode_rows = []
    decodings = []
    for index, (name, text) in enumerate(encode_inputs()):
        payload = text.encode("punycode", errors="strict")
        encode_rows.append({
            "name": name,
            "source_code_points": list(map(ord, text)),
            "source_payload": payload.decode("ascii"),
            "native_accept": scalar_text(text) and len(text) <= MAX_CODE_POINTS
                and len(payload) <= MAX_PAYLOAD_BYTES,
        })
        decodings.append(decode_observation("encoded-" + name, payload))
        if index < 32:
            split = payload.rfind(b"-") + 1
            prefix, extended = payload[:split], payload[split:]
            decodings.append(decode_observation("upper-digits-" + name, prefix + extended.upper()))
            mixed = bytes(char - 32 if 97 <= char <= 122 and offset % 2 else char
                          for offset, char in enumerate(extended))
            decodings.append(decode_observation("mixed-digits-" + name, prefix + mixed))
    for name, payload in [
        ("delimiter-only", b"-"),
        ("two-delimiters", b"--"),
        ("empty-prefix-a", b"-a"),
        ("ascii-prefix-case", b"AbC-"),
        ("basic-punctuation", b"!.\x00\x7f-"),
        ("digit-a", b"a"),
        ("truncated-z", b"z"),
        ("truncated-9", b"9"),
        ("non-digit", b"!"),
        ("invalid-suffix", b"a-!"),
        ("non-ascii-prefix", b"\x80-"),
        ("non-ascii-suffix", b"a-\xff"),
        ("overflow-continuation", b"z" * 256),
        ("out-of-range", b"9" * 32 + b"a"),
        ("early-nul", b"\x00"),
        ("late-nul", b"a\x00"),
    ]:
        decodings.append(decode_observation(name, payload))
    data = {
        "schema_version": 1,
        "reference": {
            "implementation": "CPython",
            "version": "3.11.3",
            "source_path": "Lib/encodings/punycode.py",
            "source_sha256": source_hash,
            "license": "Python Software Foundation License Version 2",
        },
        "generator_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        "native_contract": {
            "scalar_only": True,
            "max_code_points": MAX_CODE_POINTS,
            "max_payload_bytes": MAX_PAYLOAD_BYTES,
        },
        "encodings": encode_rows,
        "decodings": decodings,
    }
    return json.dumps(data, ensure_ascii=True, indent=2) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    output = render()
    if args.check:
        if not TARGET.exists() or TARGET.read_text(encoding="ascii") != output:
            print("CPython raw Punycode fixture is stale")
            return 1
        print("CPython 3.11.3 raw Punycode fixture is current")
    else:
        TARGET.write_text(output, encoding="ascii")
        print(f"Wrote {TARGET}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
