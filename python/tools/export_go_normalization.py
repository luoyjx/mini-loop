#!/usr/bin/env python3
"""Export fixed CPython Unicode properties and actual normalization observations.

Only CPython 3.11.3's installed unicodedata 14.0.0 is an input. There is no
download, third-party Python import, Go module, or runtime Python requirement.
The Go algorithm is project code; generated data retains the installed CPython
license verbatim as line comments. --provenance records local binary/source
hashes separately, so machine-specific paths do not enter canonical outputs.
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
import platform
import subprocess
import sys
import unicodedata
from dataclasses import dataclass
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
NORMALIZATION_TARGET = ROOT / "go/internal/pytext/normalization_data.go"
BIDI_TARGET = ROOT / "go/internal/pytext/bidi_data.go"
CORPUS_TARGET = ROOT / "go/testdata/python-unicode-normalization.json"
PYTHON_VERSION = "3.11.3"
UNICODE_VERSION = "14.0.0"
LIMIT = 0x110000
BIDI_NAMES = (
    "L", "R", "AL", "AN", "EN", "ES", "CS", "ET", "ON", "BN", "NSM",
    "B", "S", "WS", "LRE", "LRO", "RLE", "RLO", "PDF", "LRI", "RLI", "FSI", "PDI",
)


@dataclass(frozen=True)
class PropertyRange:
    first: int
    last: int
    value: int | str


@dataclass(frozen=True)
class Decomposition:
    source: int
    parts: tuple[int, ...]


@dataclass(frozen=True, order=True)
class Composition:
    first: int
    second: int
    composite: int


@dataclass(frozen=True)
class Tables:
    decompositions: tuple[Decomposition, ...]
    combining: tuple[PropertyRange, ...]
    compositions: tuple[Composition, ...]
    bidi: tuple[PropertyRange, ...]


def require_profile() -> None:
    profile = (platform.python_implementation(), platform.python_version(), unicodedata.unidata_version)
    expected = ("CPython", PYTHON_VERSION, UNICODE_VERSION)
    if profile != expected:
        raise RuntimeError(f"fixed Unicode export requires {expected!r}, found {profile!r}")


def reference(path: Path) -> dict[str, str | int]:
    data = path.read_bytes()
    return {"path": str(path.resolve()), "bytes": len(data), "sha256": hashlib.sha256(data).hexdigest()}


def license_source() -> Path:
    path = Path(sys.base_prefix) / "lib/python3.11/LICENSE.txt"
    if not path.is_file():
        raise RuntimeError(f"installed CPython license is missing: {path}")
    text = path.read_text(encoding="utf-8")
    if "PYTHON SOFTWARE FOUNDATION LICENSE VERSION 2" not in text:
        raise RuntimeError("installed CPython license has an unexpected form")
    return path


def append_range(ranges: list[PropertyRange], point: int, value: int | str) -> None:
    if ranges and ranges[-1].last + 1 == point and ranges[-1].value == value:
        previous = ranges[-1]
        ranges[-1] = PropertyRange(previous.first, point, value)
    else:
        ranges.append(PropertyRange(point, point, value))


def collect_tables() -> Tables:
    decompositions: list[Decomposition] = []
    combining: list[PropertyRange] = []
    bidi: list[PropertyRange] = []
    compositions: list[Composition] = []
    for point in range(LIMIT):
        char = chr(point)
        class_value = unicodedata.combining(char)
        if class_value:
            append_range(combining, point, class_value)
        direction = unicodedata.bidirectional(char)
        if direction:
            if direction not in BIDI_NAMES:
                raise RuntimeError(f"unexpected bidi class {direction!r}")
            append_range(bidi, point, direction)
        raw = unicodedata.decomposition(char)
        if raw and not raw.startswith("<"):
            parts = tuple(int(part, 16) for part in raw.split())
            decompositions.append(Decomposition(point, parts))
            # An excluded pair does not compose back under the real Source NFC.
            # No compatibility mapping enters this table. Hangul is algorithmic.
            if (len(parts) == 2 and unicodedata.combining(chr(parts[0])) == 0
                    and unicodedata.normalize("NFC", "".join(map(chr, parts))) == char):
                compositions.append(Composition(parts[0], parts[1], point))
    tables = Tables(tuple(decompositions), tuple(combining), tuple(sorted(compositions)), tuple(bidi))
    validate_tables(tables)
    return tables


def validate_tables(tables: Tables) -> None:
    for ranges in (tables.combining, tables.bidi):
        previous = -1
        for entry in ranges:
            if not previous < entry.first <= entry.last < LIMIT:
                raise RuntimeError("property ranges must be valid, sorted and disjoint")
            previous = entry.last
    if {entry.value for entry in tables.bidi} != set(BIDI_NAMES):
        raise RuntimeError("Source bidi class inventory changed")
    if any(not 0 < int(entry.value) <= 255 for entry in tables.combining):
        raise RuntimeError("combining class does not fit the closed uint8 representation")
    mapping = {entry.source: entry.parts for entry in tables.decompositions}
    if len(mapping) != len(tables.decompositions) or list(mapping) != sorted(mapping):
        raise RuntimeError("canonical decomposition sources must be unique and sorted")
    for entry in tables.decompositions:
        if not 0 < len(entry.parts) <= 255 or any(not 0 <= part < LIMIT for part in entry.parts):
            raise RuntimeError("canonical decomposition does not fit its typed representation")

    def depth(point: int, active: frozenset[int]) -> int:
        if point in active:
            raise RuntimeError("canonical decomposition cycle")
        return 1 + max((depth(part, active | {point}) for part in mapping.get(point, ())), default=0)

    # Fixed data has shallow recursion; refuse accidental/untrusted unbounded data.
    if max(map(lambda point: depth(point, frozenset()), mapping), default=0) > 32:
        raise RuntimeError("unexpected canonical decomposition depth")
    keys = [(entry.first, entry.second) for entry in tables.compositions]
    if keys != sorted(set(keys)):
        raise RuntimeError("composition pairs must be sorted and unique")


def generated_header(exporter_sha256: str) -> list[str]:
    return [
        "// Code generated by python/tools/export_go_normalization.py; DO NOT EDIT.",
        f"// Source: CPython {PYTHON_VERSION}, unicodedata {UNICODE_VERSION}.",
        f"// Exporter SHA256: {exporter_sha256}.",
        "// Rebuild: .venv/bin/python python/tools/export_go_normalization.py",
        "// Local input hashes are recorded by the exporter's --provenance option.",
    ]


def format_go(lines: list[str]) -> bytes:
    result = subprocess.run(
        ["gofmt"], input=("\n".join(lines) + "\n").encode("utf-8"),
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False,
    )
    if result.returncode:
        raise RuntimeError(f"gofmt refused generated data: {result.stderr.decode('utf-8', 'replace')}")
    return result.stdout


def render_normalization(tables: Tables, exporter_sha256: str, license_text: str) -> bytes:
    lines = generated_header(exporter_sha256)
    lines += ["//", "// Installed CPython LICENSE.txt (verbatim content; Go line-comment prefixes added):"]
    lines += ["// " + line if line else "//" for line in license_text.splitlines()]
    lines += ["package pytext", "", f'const NormalizationPythonVersion = "{PYTHON_VERSION}"',
              f'const NormalizationUnicodeVersion = "{UNICODE_VERSION}"', ""]
    lines.append("var pythonCanonical14 = [...]canonicalDecomposition{")
    offset = 0
    for entry in tables.decompositions:
        lines.append(f"\t{{0x{entry.source:x}, {offset}, {len(entry.parts)}}},")
        offset += len(entry.parts)
    lines += ["}", "", "var pythonCanonicalRunes14 = [...]rune{"]
    for entry in tables.decompositions:
        lines.append("\t" + ", ".join(f"0x{part:x}" for part in entry.parts) + ",")
    lines += ["}", "", "var pythonCombining14 = [...]combiningClassRange{"]
    lines += [f"\t{{0x{entry.first:x}, 0x{entry.last:x}, {entry.value}}}," for entry in tables.combining]
    lines += ["}", "", "var pythonComposition14 = [...]canonicalComposition{"]
    lines += [f"\t{{0x{entry.first:x}, 0x{entry.second:x}, 0x{entry.composite:x}}}," for entry in tables.compositions]
    lines += ["}", ""]
    return format_go(lines)


def render_bidi(tables: Tables, exporter_sha256: str) -> bytes:
    lines = generated_header(exporter_sha256)
    lines += ["// The complete installed CPython license is retained in normalization_data.go.",
              "package pytext", "", "var pythonBidi14 = [...]bidiClassRange{"]
    lines += [f"\t{{0x{entry.first:x}, 0x{entry.last:x}, Bidi{entry.value}}}," for entry in tables.bidi]
    lines += ["}", ""]
    return format_go(lines)


def b64(value: str) -> str:
    return base64.b64encode(value.encode("utf-8")).decode("ascii")


def render_corpus(tables: Tables, exporter_sha256: str) -> bytes:
    cases: list[dict[str, str | bool]] = []
    names: set[str] = set()

    def capture(name: str, kind: str, source: str) -> None:
        if name in names:
            raise RuntimeError(f"duplicate normalization recipe {name}")
        names.add(name)
        normalized = unicodedata.normalize("NFC", source)
        is_nfc = unicodedata.is_normalized("NFC", source)
        if is_nfc != (source == normalized):
            raise RuntimeError("Source is_normalized and normalize disagree")
        cases.append({"name": name, "kind": kind, "source_base64": b64(source),
                      "nfc_base64": b64(normalized), "is_nfc": is_nfc})

    curated = (
        ("empty", ""), ("ascii", "hello\x00world\n"), ("composed-latin", "\u00c5"),
        ("decomposed-latin", "A\u030a"), ("recursive", "\u1e69"),
        ("recursive-decomposed", "s\u0323\u0307"), ("reordered", "a\u0315\u0300"),
        ("leading-reordered", "\u0315\u0300a"), ("equal-class-blocks", "A\u0305\u0301"),
        ("equal-class-stable", "A\u0301\u0305"), ("lower-class-does-not-block", "A\u0327\u030a"),
        ("starter-resets", "A\u030aB\u0301"), ("cgj-blocks", "A\u034f\u030a"),
        ("exclusion-diaeresis", "\u0344"), ("exclusion-nukta", "\u0958"),
        ("singleton-ohm", "\u2126"), ("singleton-kelvin", "\u212a"),
        ("singleton-angstrom", "\u212b"), ("compatibility-ligature-identity", "\ufb01"),
        ("compatibility-width-identity", "\uff21"), ("zero-class-pair", "\u09c7\u09be"),
        ("musical-recursion", "\U0001d160"), ("hangul-first", "\uac00"),
        ("hangul-last", "\ud7a3"), ("hangul-before", "\uabff"), ("hangul-after", "\ud7a4"),
        ("hangul-empty-trailer", "\u1100\u1161\u11a7"),
        ("hangul-trailer-blocked", "\u1100\u1161\u0301\u11a8"),
        ("hangul-leading-blocked", "\u1100\u0301\u1161"),
        ("replacement-is-valid", "\ufffd"), ("unknown-in-unicode14", "\U00011f04"),
        ("last-scalar", "\U0010ffff"),
    )
    for name, source in curated:
        capture(name, "curated", source)
    for entry in tables.decompositions:
        capture(f"canonical-scalar-{entry.source:06x}", "canonical-scalar", chr(entry.source))
        capture(f"canonical-parts-{entry.source:06x}", "canonical-parts", "".join(map(chr, entry.parts)))
    for entry in tables.compositions:
        capture(f"composition-{entry.composite:06x}", "composition", chr(entry.first) + chr(entry.second))
    # Cover every leading/vowel combination and both trailing boundaries, plus
    # every trailing Jamo. Expected strings always come from real Source NFC.
    for leading in range(19):
        for vowel in range(21):
            for trailing in (0, 1, 27):
                source = chr(0x1100 + leading) + chr(0x1161 + vowel)
                if trailing:
                    source += chr(0x11a7 + trailing)
                capture(f"hangul-{leading:02}-{vowel:02}-{trailing:02}", "hangul", source)
    for trailing in range(2, 27):
        capture(f"hangul-all-trailing-{trailing:02}", "hangul", "\u1100\u1161" + chr(0x11a7 + trailing))
    capture("long-inverted-marks", "long-marks", "A" + "\u0315\u0300\u0327\u0301" * 2048)
    capture("long-leading-marks", "long-marks", "\u0315\u0300\u0327\u0301" * 2048)
    capture("long-equal-marks", "long-marks", "A" + "\u0305\u0301" * 4096)

    bidi_points = {point for entry in tables.bidi for point in (entry.first, entry.last)}
    bidi_points.update((0, 0x378, 0xd800, 0xdfff, 0x11f04, 0x1fae8, 0x10ffff))
    bidi_cases = [{"codepoint": point, "class": unicodedata.bidirectional(chr(point)) or "Unknown"}
                  for point in sorted(bidi_points)]
    invalid = (
        ("continuation", b"\x80"), ("truncated-two", b"\xc2"), ("overlong", b"\xc0\xaf"),
        ("truncated-three", b"\xe2\x82"), ("surrogate-high", b"\xed\xa0\x80"),
        ("surrogate-low", b"\xed\xbf\xbf"), ("above-unicode", b"\xf4\x90\x80\x80"),
        ("invalid-after-ascii", b"a\xff"),
    )
    payload = {
        "schema_version": 1, "python_implementation": "CPython", "python_version": PYTHON_VERSION,
        "unicode_version": UNICODE_VERSION, "exporter_sha256": exporter_sha256,
        "scope": "Actual fixed unicodedata NFC and bidi observations; invalid UTF-8 is a separate Go admission boundary.",
        "normalization_cases": cases, "bidi_cases": bidi_cases,
        "invalid_utf8_cases": [{"name": name, "source_base64": base64.b64encode(value).decode("ascii")}
                               for name, value in invalid],
    }
    return (json.dumps(payload, ensure_ascii=True, indent=2) + "\n").encode("ascii")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="compare all outputs without writing")
    parser.add_argument("--provenance", type=Path, help="write local input/output hashes after generation")
    args = parser.parse_args()
    if args.check and args.provenance:
        parser.error("--check does not write provenance; omit --provenance")
    require_profile()
    exporter = reference(Path(__file__))
    license_path = license_source()
    tables = collect_tables()
    exporter_sha256 = str(exporter["sha256"])
    outputs = (
        (NORMALIZATION_TARGET, render_normalization(tables, exporter_sha256, license_path.read_text(encoding="utf-8"))),
        (BIDI_TARGET, render_bidi(tables, exporter_sha256)),
        (CORPUS_TARGET, render_corpus(tables, exporter_sha256)),
    )
    if args.check:
        stale = [str(path.relative_to(ROOT)) for path, data in outputs if not path.is_file() or path.read_bytes() != data]
        if stale:
            print("Fixed Unicode outputs are stale: " + ", ".join(stale))
            return 1
        print(f"Fixed CPython {PYTHON_VERSION} Unicode {UNICODE_VERSION} outputs are current")
        return 0
    for path, data in outputs:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
        print(f"Wrote {path.relative_to(ROOT)} ({len(data)} bytes)")
    if args.provenance:
        provenance = {
            "schema_version": 1,
            "source": {"python_implementation": "CPython", "python_version": PYTHON_VERSION,
                       "unicode_version": UNICODE_VERSION, "interpreter": reference(Path(sys.executable).resolve()),
                       "unicodedata": reference(Path(unicodedata.__file__)), "license": reference(license_path)},
            "exporter": exporter,
            "algorithm": "Project-owned canonical recursive decomposition, stable CCC reorder, excluded-pair-aware composition and algorithmic Hangul.",
            "counts": {"canonical_decompositions": len(tables.decompositions),
                       "combining_ranges": len(tables.combining), "composition_pairs": len(tables.compositions),
                       "bidi_ranges": len(tables.bidi), "bidi_classes": len(BIDI_NAMES)},
            "outputs": [reference(path) for path, _ in outputs],
            "rebuild_command": ".venv/bin/python python/tools/export_go_normalization.py",
            "check_command": ".venv/bin/python python/tools/export_go_normalization.py --check",
            "runtime_python_invocation": False, "external_go_modules": [], "downloads": [],
        }
        args.provenance.parent.mkdir(parents=True, exist_ok=True)
        args.provenance.write_text(json.dumps(provenance, indent=2) + "\n", encoding="utf-8")
        print(f"Wrote provenance {args.provenance}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
